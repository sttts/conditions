// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package conditions aggregates supplied Kubernetes Conditions with flat, caller-defined rules.
package conditions

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Polarity declares which known boolean status represents a good observation.
type Polarity uint8

const (
	TrueIsGood Polarity = iota
	FalseIsGood
)

// Assessment is independent of both escalation and output polarity.
type Assessment uint8

const (
	Good Assessment = iota
	Bad
	Unknown
)

// Escalation describes a bad result without imposing a terminal lifecycle.
type Escalation uint8

const (
	None Escalation = iota
	WaitingForHuman
	Failed
)

// State is the normalized semantic result before encoding output polarity.
type State struct {
	Assessment Assessment
	Escalation Escalation
}

// Getter is the conventional read-only accessor implemented by condition-bearing resources.
type Getter interface {
	GetConditions() []metav1.Condition
}

// Object combines condition access with standard Kubernetes metadata getters.
type Object interface {
	Getter
	GetGeneration() int64
	GetCreationTimestamp() metav1.Time
	GetDeletionTimestamp() *metav1.Time
}

// Setter reports whether applying the desired slice changed the stored Conditions.
type Setter interface {
	Getter
	SetConditions([]metav1.Condition) bool
}

// Conditioned combines object access and in-memory assignment without requiring a Kubernetes client.
type Conditioned interface {
	Object
	Setter
}

// Derivation derives additional local Conditions from already-computed Conditions and metadata.
type Derivation interface {
	Derive(parent Object, now time.Time) []metav1.Condition
}

// LocalEvaluator aggregates supplied local Conditions that have no configured output rule.
type LocalEvaluator interface {
	Evaluate(parent Object, local metav1.Condition, now time.Time) (*metav1.Condition, error)
}

// Rules is flat configuration for one resource boundary, in the caller's declared output order.
type Rules struct {
	Outputs       []Aggregation
	LocalFirst    bool
	UnmappedLocal LocalEvaluator
	IgnoreLocal   []string
	Now           func() time.Time
}

// Lookup distinguishes a missing Condition from an explicitly unknown Condition.
func Lookup(object Getter, typ string) (metav1.Condition, bool) {
	for _, condition := range object.GetConditions() {
		if condition.Type == typ {
			return condition, true
		}
	}
	return metav1.Condition{Type: typ, Status: metav1.ConditionUnknown, Reason: "Unknown", Message: "Unknown"}, false
}

// ConditionType names a Condition and explicitly declares its source or output polarity.
type ConditionType struct {
	Type     string
	Polarity Polarity
}

// Read uses the reference profile's explicit missing-as-unknown convention.
func (c ConditionType) Read(s Object) metav1.Condition { value, _ := Lookup(s, c.Type); return value }

// Assessment normalizes a raw status without guessing escalation from its boolean.
func (c ConditionType) Assessment(value metav1.Condition) Assessment {
	if value.Status == metav1.ConditionUnknown {
		return Unknown
	}
	good := value.Status == metav1.ConditionTrue
	if c.Polarity == FalseIsGood {
		good = !good
	}
	if good {
		return Good
	}
	return Bad
}

// IsGood and IsBad preserve the distinction between unknown and known bad state.
func (c ConditionType) IsGood(s Object) bool { return c.Assessment(c.Read(s)) == Good }
func (c ConditionType) IsBad(s Object) bool  { return c.Assessment(c.Read(s)) == Bad }

// Classifier applies only after a source has been established as bad.
type Classifier interface {
	Classify(metav1.Condition) Escalation
}

// Propagation decides whether a particular child diagnosis participates in an output.
type Propagation interface {
	Allows(parent, child Object) bool
}

// ChildSelector selects the relevant contributions from one supplied child.
type ChildSelector interface {
	Select(parent, child Object) []Contribution
}

// LocalSelector selects the parent's independently supplied local checks.
type LocalSelector interface{ Select(Object) []Contribution }

// ParentPolicy handles local precedence and decides whether children may contribute.
type ParentPolicy interface {
	Before(parent Object, children map[string]Object, base metav1.Condition) (*Outcome, bool)
	IncludeChildren(base metav1.Condition) bool
}

// Reducer combines selected contributions, retaining their origin and diagnostics.
type Reducer interface {
	Reduce(base metav1.Condition, children map[string]Object, local, selected []Contribution) Outcome
}

// Formatter renders diagnostics separately from semantic selection and reduction.
type Formatter interface {
	Format(string, []Cause) string
	Inline(metav1.Condition) string
}

// Presenter selects a human summary from already-computed parent Conditions.
type Presenter interface {
	Present(Object) (phase, message string)
}

// Cause retains source text and its diagnostic provenance.
type Cause struct {
	Reference string
	Message   string
}

// Contribution retains a selected observation and its profile-specific diagnostic annotations.
type Contribution struct {
	State          State
	Condition      metav1.Condition
	Reference      string
	Source         Object
	CheckName      string
	Fallback       string
	Detail         *string
	Uninitialized  bool
	FromUnknown    bool
	SuppressDetail bool
}

// Outcome combines a normalized result with its retained diagnostic causes.
type Outcome struct {
	State              State
	Reason             string
	Summary            string
	Causes             []Cause
	Copied             *metav1.Condition
	LastTransitionTime metav1.Time
}

// GenerationPolicy declares which observed-generation representation an output supports.
type GenerationPolicy uint8

const (
	ParentGeneration GenerationPolicy = iota
	PreserveBaseGeneration
	NoObservedGeneration
)

// Aggregation is a flat value composing implementations of Conditions concepts.
type Aggregation struct {
	DefaultBase *metav1.Condition
	Derived     []Derivation
	Output      ConditionType
	Parent      ParentPolicy
	Local       LocalSelector
	Children    ChildSelector
	Reduction   Reducer
	Messages    Formatter
	Generation  GenerationPolicy
}

// Evaluate derives one desired Condition without modifying the supplied objects.
func (a Aggregation) Evaluate(parent Object, children map[string]Object, base metav1.Condition, now time.Time) (*metav1.Condition, error) {
	if isNilObject(parent) {
		return nil, fmt.Errorf("parent is nil")
	}
	if a.Output.Type == "" || a.Parent == nil || a.Reduction == nil || a.Messages == nil {
		return nil, fmt.Errorf("incomplete aggregation for %q", a.Output.Type)
	}
	outcome, done := a.Parent.Before(parent, children, base)
	if done && outcome == nil {
		return nil, nil
	}
	if !done {
		var local, selected []Contribution
		if a.Local != nil {
			local = a.Local.Select(parent)
		}
		if a.Children != nil && a.Parent.IncludeChildren(base) {
			for _, child := range sortedChildren(children) {
				for _, contribution := range a.Children.Select(parent, child.Object) {
					contribution.Reference = child.Name
					selected = append(selected, contribution)
				}
			}
		}
		reduced := a.Reduction.Reduce(base, children, local, selected)
		outcome = &reduced
	}
	status := metav1.ConditionUnknown
	if outcome.State.Assessment != Unknown {
		good := outcome.State.Assessment == Good
		if a.Output.Polarity == FalseIsGood {
			good = !good
		}
		status = metav1.ConditionFalse
		if good {
			status = metav1.ConditionTrue
		}
	}
	generation := parent.GetGeneration()
	switch a.Generation {
	case PreserveBaseGeneration:
		generation = 0
		if outcome.Copied != nil {
			generation = outcome.Copied.ObservedGeneration
		}
	case NoObservedGeneration:
		generation = 0
	}

	transition := outcome.LastTransitionTime
	if transition.IsZero() {
		transition = metav1.NewTime(now)
	}
	if old, ok := Lookup(parent, a.Output.Type); ok && old.Status == status {
		transition = old.LastTransitionTime
	}
	return &metav1.Condition{Type: a.Output.Type, Status: status, Reason: outcome.Reason,
		Message: a.Messages.Format(outcome.Summary, outcome.Causes), ObservedGeneration: generation,
		LastTransitionTime: transition}, nil
}

// Aggregate applies the configured rules to existing objects and reports in-memory changes.
func Aggregate(parent Conditioned, children map[string]Object, rules Rules, local ...metav1.Condition) (bool, error) {
	desired, err := Evaluate(parent, children, rules, local...)
	if err != nil {
		return false, err
	}
	return parent.SetConditions(desired), nil
}

// Evaluate computes all desired Conditions through the same generic resource-oriented call.
func Evaluate(parent Object, children map[string]Object, rules Rules, local ...metav1.Condition) ([]metav1.Condition, error) {
	if isNilObject(parent) {
		return nil, fmt.Errorf("parent is nil")
	}
	outputs := make(map[string]Aggregation, len(rules.Outputs))
	for _, rule := range rules.Outputs {
		if rule.Output.Type == "" || rule.Parent == nil || rule.Reduction == nil || rule.Messages == nil {
			return nil, fmt.Errorf("incomplete aggregation for %q", rule.Output.Type)
		}
		if _, exists := outputs[rule.Output.Type]; exists {
			return nil, fmt.Errorf("duplicate output %q", rule.Output.Type)
		}
		if rule.DefaultBase != nil && rule.DefaultBase.Type != rule.Output.Type {
			return nil, fmt.Errorf("default base %q does not match output %q", rule.DefaultBase.Type, rule.Output.Type)
		}
		outputs[rule.Output.Type] = rule
	}
	ignored := make(map[string]bool, len(rules.IgnoreLocal))
	for _, typ := range rules.IgnoreLocal {
		ignored[typ] = true
	}
	nowFunc := rules.Now
	if nowFunc == nil {
		nowFunc = time.Now
	}
	now := nowFunc()
	working := &conditionOverlay{Object: parent, conditions: append([]metav1.Condition(nil), parent.GetConditions()...)}
	apply := func(rule Aggregation, base metav1.Condition) error {
		condition, err := rule.Evaluate(working, children, base, now)
		if err != nil {
			return err
		}
		if condition != nil {
			working.SetConditions(*condition)
			for _, derivation := range rule.Derived {
				working.SetConditions(derivation.Derive(working, now)...)
			}
		}
		return nil
	}
	provided := map[string]bool{}
	pending := map[string]metav1.Condition{}
	for _, condition := range local {
		if ignored[condition.Type] {
			continue
		}
		provided[condition.Type] = true
		if rule, exists := outputs[condition.Type]; exists {
			if rules.LocalFirst && !rule.Parent.IncludeChildren(condition) {
				delete(pending, condition.Type)
				if err := apply(rule, condition); err != nil {
					return nil, err
				}
			} else {
				pending[condition.Type] = condition
			}
			continue
		}
		if rules.UnmappedLocal == nil {
			working.SetConditions(stamp(working, condition, now))
			continue
		}
		result, err := rules.UnmappedLocal.Evaluate(working, condition, now)
		if err != nil {
			return nil, err
		}
		if result != nil {
			working.SetConditions(*result)
		}
	}
	for _, rule := range rules.Outputs {
		if base, exists := pending[rule.Output.Type]; exists {
			if err := apply(rule, base); err != nil {
				return nil, err
			}
		} else if !provided[rule.Output.Type] && rule.DefaultBase != nil {
			if err := apply(rule, *rule.DefaultBase); err != nil {
				return nil, err
			}
		}
	}
	return working.GetConditions(), nil
}

// ReasonSuffix assigns escalation to a matching reason suffix.
type ReasonSuffix struct {
	Suffix     string
	Escalation Escalation
}

// ReasonClassifier matches exact reasons, ordered suffixes, and a default in normal Go.
type ReasonClassifier struct {
	Reasons  map[string]Escalation
	Suffixes []ReasonSuffix
	Default  Escalation
}

func (r ReasonClassifier) Classify(c metav1.Condition) Escalation {
	if value, ok := r.Reasons[c.Reason]; ok {
		return value
	}
	for _, rule := range r.Suffixes {
		if strings.HasSuffix(c.Reason, rule.Suffix) {
			return rule.Escalation
		}
	}
	return r.Default
}

// Always and Never implement the two unconditional propagation cases.
type Always struct{}

func (Always) Allows(Object, Object) bool { return true }

type Never struct{}

func (Never) Allows(Object, Object) bool { return false }

// BeforeInitialized propagates only before the configured parent Condition is good.
type BeforeInitialized struct{ Parent ConditionType }

func (p BeforeInitialized) Allows(parent, _ Object) bool { return !p.Parent.IsGood(parent) }

// ExistingChildren includes new children during bring-up and initialized children thereafter.
type ExistingChildren struct{ Parent, Child ConditionType }

func (p ExistingChildren) Allows(parent, child Object) bool {
	return !p.Parent.IsGood(parent) || p.Child.IsGood(child)
}

// LocalBaseFirst preserves a bad local base and optionally overrides it during deletion.
type LocalBaseFirst struct {
	Base               ConditionType
	Classifier         Classifier
	OverrideOnDeletion bool
}

func (p LocalBaseFirst) Before(parent Object, children map[string]Object, base metav1.Condition) (*Outcome, bool) {
	if p.OverrideOnDeletion && isDeleting(parent) {
		out := badOutcome("Deleting", "resource is being deleted")
		return &out, true
	}
	if p.Base.Assessment(base) == Bad {
		out := copyOutcome(base, p.Base, p.Classifier)
		return &out, true
	}
	return nil, false
}
func (p LocalBaseFirst) IncludeChildren(base metav1.Condition) bool {
	return p.Base.Assessment(base) == Good
}

// LocalOnly applies the same base precedence while accepting no child inputs.
type LocalOnly struct{ LocalBaseFirst }

func (LocalOnly) IncludeChildren(base metav1.Condition) bool { return false }

// ChildrenOnly always reduces supplied children without interpreting a local base.
type ChildrenOnly struct{}

func (ChildrenOnly) Before(Object, map[string]Object, metav1.Condition) (*Outcome, bool) {
	return nil, false
}
func (ChildrenOnly) IncludeChildren(metav1.Condition) bool { return true }

// PrefixedChecks selects bad local Conditions under one explicitly configured type prefix.
type PrefixedChecks struct {
	Prefix     string
	Polarity   Polarity
	Classifier Classifier
}

func (p PrefixedChecks) Select(parent Object) []Contribution {
	conditions := append([]metav1.Condition(nil), parent.GetConditions()...)
	sort.Slice(conditions, func(i, j int) bool { return conditions[i].Type < conditions[j].Type })
	var result []Contribution
	for _, c := range conditions {
		if c.Type == p.Prefix || !strings.HasPrefix(c.Type, p.Prefix) {
			continue
		}
		if (ConditionType{Type: c.Type, Polarity: p.Polarity}).Assessment(c) != Bad {
			continue
		}
		result = append(result, Contribution{State: State{Bad, classify(p.Classifier, c)}, Condition: c,
			Source: parent, CheckName: strings.TrimPrefix(c.Type, p.Prefix)})
	}
	return result
}

// classify supplies the natural no-escalation default for a missing classifier.
func classify(c Classifier, value metav1.Condition) Escalation {
	if c == nil {
		return None
	}
	return c.Classify(value)
}
func badOutcome(reason, message string) Outcome {
	return Outcome{State: State{Bad, None}, Reason: reason, Summary: message}
}
func copyOutcome(c metav1.Condition, check ConditionType, classifier Classifier) Outcome {
	assessment := check.Assessment(c)
	escalation := None
	if assessment == Bad {
		escalation = classify(classifier, c)
	}
	return Outcome{State: State{assessment, escalation}, Reason: c.Reason, Summary: c.Message, Copied: &c}
}

// namedObject retains the caller's diagnostic alias beside the supplied resource.
type namedObject struct {
	Name   string
	Object Object
}

func sortedChildren(children map[string]Object) []namedObject {
	result := make([]namedObject, 0, len(children))
	for name, object := range children {
		if !isNilObject(object) {
			result = append(result, namedObject{name, object})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

// conditionOverlay stages new Conditions while forwarding metadata to the original object.
type conditionOverlay struct {
	Object
	conditions []metav1.Condition
}

func (o *conditionOverlay) GetConditions() []metav1.Condition { return o.conditions }
func (o *conditionOverlay) SetConditions(conditions ...metav1.Condition) bool {
	changed := false
	for _, condition := range conditions {
		found := false
		for i := range o.conditions {
			if o.conditions[i].Type != condition.Type {
				continue
			}
			found = true
			if !reflect.DeepEqual(o.conditions[i], condition) {
				o.conditions[i] = condition
				changed = true
			}
			break
		}
		if !found {
			o.conditions = append(o.conditions, condition)
			changed = true
		}
	}
	return changed
}
func stamp(parent Object, c metav1.Condition, now time.Time) metav1.Condition {
	c.LastTransitionTime = metav1.NewTime(now)
	if old, ok := Lookup(parent, c.Type); ok && old.Status == c.Status {
		c.LastTransitionTime = old.LastTransitionTime
	}
	return c
}

// deleting reads already-supplied object metadata without querying Kubernetes.
func isDeleting(object Object) bool {
	timestamp := object.GetDeletionTimestamp()
	return timestamp != nil && !timestamp.IsZero()
}

// isNilObject handles optional resources, including typed nil pointers in interface values.
func isNilObject(object Object) bool {
	if object == nil {
		return true
	}
	value := reflect.ValueOf(object)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

// BoundedMessages demonstrates opaque cause rendering; a native codec is a separate implementation.
type BoundedMessages struct{ MaxLines, MaxBytes, InlineChars int }

func (p BoundedMessages) Format(summary string, causes []Cause) string {
	limit := p.MaxLines
	if limit < 1 {
		limit = 7
	}
	if len(causes) == 0 {
		return p.limit(summary)
	}
	lines := []string{strings.TrimSuffix(summary, ":") + ":"}
	for _, c := range causes {
		parts := strings.Split(strings.ReplaceAll(c.Message, "\r\n", "\n"), "\n")
		lines = append(lines, "  "+c.Reference+": "+parts[0])
		for _, part := range parts[1:] {
			lines = append(lines, "    "+part)
		}
	}
	if len(lines) > limit {
		omitted := len(lines) - limit + 1
		if limit == 1 {
			return p.limit(fmt.Sprintf("%s (%d detail lines omitted)", summary, len(lines)-1))
		}
		lines = append(lines[:limit-1], fmt.Sprintf("  … %d detail lines omitted", omitted))
	}
	return p.limit(strings.Join(lines, "\n"))
}
func (p BoundedMessages) Inline(c metav1.Condition) string {
	text := c.Message
	if text == "" {
		text = c.Reason
	}
	text = strings.SplitN(text, "\n", 2)[0]
	limit := p.InlineChars
	if limit < 1 {
		limit = 80
	}
	runes := []rune(text)
	if len(runes) > limit {
		return string(runes[:limit-1]) + "…"
	}
	return text
}
func (p BoundedMessages) limit(text string) string {
	limit := p.MaxBytes
	if limit < 4 {
		limit = 32768
	}
	if len(text) <= limit {
		return text
	}
	end := limit - 3
	for end > 0 && !utf8.ValidString(text[:end]) {
		end--
	}
	return text[:end] + "..."
}
