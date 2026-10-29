// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package infra is an executable design example, not operator production code.
package infra

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BEGIN API
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

// Setter is the slice-based Conditions setter used by Kubernetes ecosystem APIs.
type Setter interface {
	Getter
	SetConditions([]metav1.Condition)
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

// ChildSelector selects at most one relevant contribution from one supplied child.
type ChildSelector interface {
	Select(parent, child Object) (Contribution, bool)
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

// Contribution records one selected diagnosis; compatibility flags belong to this example.
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
	State   State
	Reason  string
	Summary string
	Causes  []Cause
	Copied  *metav1.Condition
}

// Aggregation is a flat value composing implementations of Conditions concepts.
type Aggregation struct {
	DefaultBase      *metav1.Condition
	Derived          []Derivation
	Output           ConditionType
	Parent           ParentPolicy
	Local            LocalSelector
	Children         ChildSelector
	Reduction        Reducer
	Messages         Formatter
	LegacyGeneration bool
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
				if contribution, ok := a.Children.Select(parent, child.Object); ok {
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
	if a.LegacyGeneration {
		generation = 0
		if outcome.Copied != nil {
			generation = outcome.Copied.ObservedGeneration
		}
	}
	transition := metav1.NewTime(now)
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
	if reflect.DeepEqual(parent.GetConditions(), desired) {
		return false, nil
	}
	parent.SetConditions(desired)
	return true, nil
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

// END API

// BEGIN STANDARD
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

// END STANDARD

// BEGIN CHILD
// ServingInputs declares the reference's Condition vocabulary and every source polarity.
type ServingInputs struct{ Available, Initialized, NonDegraded, UpToDate, Synced, Running, Succeeded, Failed, Deleting ConditionType }

// ServingChildren selects one diagnosis per child with separately configured propagation rules.
type ServingChildren struct {
	Inputs                                            ServingInputs
	Condition                                         ConditionType
	Classifier                                        Classifier
	False, Failure, Human, Unknown, SyncError         Propagation
	Uninitialized, Unavailable, JobRunning, JobFailed Propagation
	BadDescription                                    string
}

func (p ServingChildren) Select(parent, child Object) (Contribution, bool) {
	v := p.Inputs
	if isDeleting(parent) && isDeleting(child) {
		return Contribution{}, false
	}
	synced, running, failed := v.Synced.Read(child), v.Running.Read(child), v.Failed.Read(child)
	syncBad := v.Synced.Assessment(synced) == Bad
	syncHuman := syncBad && classify(p.Classifier, synced) == WaitingForHuman
	allows := func(rule Propagation) bool { return rule != nil && rule.Allows(parent, child) }
	pick := func(check ConditionType, escalation Escalation, fallback string) (Contribution, bool) {
		return Contribution{State: State{Bad, escalation}, Condition: check.Read(child), Source: child, Fallback: fallback}, true
	}

	// A job-like child is completely handled here, including its no-contribution cases.
	if running.Status != metav1.ConditionUnknown {
		switch {
		case v.Failed.Assessment(failed) == Bad && allows(p.Failure):
			return pick(v.Failed, Failed, "failed")
		case syncHuman && allows(p.Human):
			return pick(v.Synced, WaitingForHuman, "waiting for human")
		case syncBad && allows(p.SyncError):
			return pick(v.Synced, None, "not synced")
		case v.Running.Assessment(running) == Bad && allows(p.JobRunning):
			return pick(v.Running, None, "is running")
		case v.Failed.Assessment(failed) == Bad && allows(p.JobFailed):
			return pick(v.Failed, None, "failed")
		case v.Succeeded.IsGood(child):
			return Contribution{}, false
		case v.Running.Assessment(running) == Good && allows(p.JobRunning):
			return pick(v.Running, None, "is pending")
		default:
			return Contribution{}, false
		}
	}
	available, current := v.Available.Read(child), p.Condition.Read(child)
	availableBad := v.Available.Assessment(available) == Bad
	currentBad := p.Condition.Assessment(current) == Bad

	// Case order preserves failed availability before ordinary sync failure on the same child.
	switch {
	case availableBad && classify(p.Classifier, available) == Failed && allows(p.Unavailable) && allows(p.Failure):
		return pick(v.Available, Failed, "failed")
	case currentBad && classify(p.Classifier, current) == Failed && allows(p.False) && allows(p.Failure):
		return pick(p.Condition, Failed, "failed")
	case availableBad && classify(p.Classifier, available) == WaitingForHuman && allows(p.Unavailable) && allows(p.Human):
		return pick(v.Available, WaitingForHuman, "waiting for human")
	case currentBad && classify(p.Classifier, current) == WaitingForHuman && allows(p.False) && allows(p.Human):
		return pick(p.Condition, WaitingForHuman, "waiting for human")
	case syncHuman && allows(p.SyncError) && allows(p.Human):
		return pick(v.Synced, WaitingForHuman, "waiting for human")
	case syncHuman && v.Initialized.IsGood(parent):
		return Contribution{}, false
	case syncHuman && v.Initialized.IsGood(child):
		return pick(v.Synced, WaitingForHuman, "waiting for human")
	case syncBad && allows(p.SyncError):
		return pick(v.Synced, None, "not synced")
	case availableBad && allows(p.Unavailable):
		result, _ := pick(v.Available, None, "not available")
		result.Uninitialized = !v.Initialized.IsGood(child)
		if result.Uninitialized {
			result.Fallback = "not yet initialized"
		}
		return result, true
	case currentBad && allows(p.False):
		return pick(p.Condition, None, p.BadDescription)
	case v.Initialized.IsBad(child) && allows(p.Uninitialized):
		result, _ := pick(v.Available, None, "not yet initialized")
		result.Uninitialized = true
		return result, true
	case p.Condition.Assessment(current) == Unknown && allows(p.Unknown):
		text := p.BadDescription + ": state is still unknown"
		return Contribution{State: State{Bad, None}, Condition: current, Source: child, Detail: &text, FromUnknown: true}, true
	default:
		return Contribution{}, false
	}
}

// END CHILD

// BEGIN REDUCTION
// ParentChildReduction preserves local/child origin instead of applying one global severity sort.
type ParentChildReduction struct {
	Inputs                    ServingInputs
	Output                    ConditionType
	Classifier                Classifier
	BadSuffix, BadDescription string
	LocalOnly                 bool
}

func (r ParentChildReduction) Reduce(base metav1.Condition, objects map[string]Object, local, children []Contribution) Outcome {
	var lf, lh, lb, cf, ch, cb, cs, cu []Contribution
	for _, c := range local {
		switch c.State.Escalation {
		case Failed:
			lf = append(lf, c)
		case WaitingForHuman:
			lh = append(lh, c)
		default:
			lb = append(lb, c)
		}
	}
	for _, c := range children {
		switch {
		case c.State.Escalation == Failed:
			cf = append(cf, c)
		case c.State.Escalation == WaitingForHuman:
			ch = append(ch, c)
		case c.Condition.Type == r.Inputs.Synced.Type:
			cs = append(cs, c)
		default:
			cb = append(cb, c)
		}
		if c.FromUnknown {
			cu = append(cu, c)
		}
	}

	// Unknown local bases allow local checks but never child aggregation.
	if r.LocalOnly || r.Output.Assessment(base) == Unknown {
		for _, group := range []struct {
			items               []Contribution
			multiple, one, many string
			escalation          Escalation
		}{
			{lf, "FailedChecks", "failed %s check: %s", "failed checks: ", Failed},
			{lh, "WaitingForHuman", "%s check waiting for human: %s", "checks waiting for human: ", WaitingForHuman},
			{lb, "FalseChecks", "false %s check: %s", "false checks: ", None},
		} {
			if len(group.items) == 1 {
				c := group.items[0]
				return Outcome{State: State{Bad, group.escalation}, Reason: c.Condition.Reason, Summary: fmt.Sprintf(group.one, c.CheckName, c.Condition.Message)}
			}
			if len(group.items) > 1 {
				return Outcome{State: State{Bad, group.escalation}, Reason: group.multiple, Summary: group.many + limitedNames(group.items, true)}
			}
		}
		return copyOutcome(base, r.Output, r.Classifier)
	}
	makeOutcome := func(reason string, escalation Escalation, heads []string, groups ...[]Contribution) Outcome {
		var causes []Cause
		for _, group := range groups {
			for _, c := range group {
				if !c.SuppressDetail {
					causes = append(causes, cause(c))
				}
			}
		}
		return Outcome{State: State{Bad, escalation}, Reason: reason, Summary: joinHeads(heads...), Causes: causes}
	}
	localHead := func(group []Contribution, singular, plural string) string {
		if len(group) == 1 {
			return fmt.Sprintf(singular, group[0].CheckName)
		}
		return plural + limitedNames(group, true)
	}
	badDescription := r.BadDescription
	allUninitialized := len(cb) > 0
	for _, c := range cb {
		allUninitialized = allUninitialized && c.Uninitialized
	}
	if allUninitialized {
		badDescription = "not yet initialized children"
	}

	// These ordinary Go branches are the reference's domain precedence, not interpreter actions.
	switch {
	case len(lf) > 0:
		reason := "FailedChecks"
		if len(lf) == 1 {
			reason = lf[0].Condition.Reason
		}
		return makeOutcome(reason, Failed, []string{localHead(lf, "failed %s check", "failed checks: "), groupHead("children failed", cf), groupHead("not synced", cs)}, lf, cf, cs)
	case len(cf) > 0:
		return makeOutcome(childReason(objects, cf, "Failed"), Failed, []string{groupHead("children failed", cf)}, cf)
	case len(lh) > 0:
		reason := "WaitingForHuman"
		if len(lh) == 1 {
			reason = lh[0].Condition.Reason
		}
		return makeOutcome(reason, WaitingForHuman, []string{localHead(lh, "%s check waiting for human", "checks waiting for human: "), groupHead("children waiting for human", ch), groupHead("not synced", cs)}, lh, ch, cs)
	case len(ch) > 0:
		return makeOutcome(childReason(objects, ch, "WaitingForHuman"), WaitingForHuman, []string{groupHead("children waiting for human", ch)}, ch)
	case len(cb) == 0 && len(cs) > 0:
		return makeOutcome(childReason(objects, cs, "NotSynced"), None, []string{groupHead("children are not synced", cs)}, cs)
	case len(cb) == 1 && len(cs) == 0:
		return makeOutcome(childReason(objects, cb, r.BadSuffix), None, []string{groupHead(badDescription, cb), localGroupHead("false checks", lb)}, cb, lb)
	case len(cb) > 0:
		return makeOutcome("Children"+r.BadSuffix, None, []string{groupHead(badDescription, cb), groupHead("not synced", cs), localGroupHead("false checks", lb)}, cb, cs, lb)
	case len(lb) > 0:
		reason := "FalseChecks"
		head := "false checks: " + limitedNames(lb, true)
		if len(lb) == 1 {
			reason = lb[0].Condition.Reason
			head = lb[0].CheckName + " check false"
		}
		return makeOutcome(reason, None, []string{head}, lb)
	case len(cu) > 0:
		out := makeOutcome(childReason(objects, cu, "StateUnknown"), None, []string{groupHead("children with unknown state", cu)}, cu)
		out.State.Assessment = Unknown
		return out
	default:
		return copyOutcome(base, r.Output, r.Classifier)
	}
}

// END REDUCTION

// BEGIN LOCAL
// DegradationBase handles availability-dependent local gates, independently of child selection.
type DegradationBase struct {
	LocalBaseFirst
	Inputs ServingInputs
}

func (p DegradationBase) Before(parent Object, children map[string]Object, base metav1.Condition) (*Outcome, bool) {
	if result, done := p.LocalBaseFirst.Before(parent, children, base); done {
		return result, true
	}
	if !p.IncludeChildren(base) {
		return nil, false
	}
	switch p.Inputs.Available.Assessment(p.Inputs.Available.Read(parent)) {
	case Bad:
		hasFailure := false
		for _, child := range children {
			if isNilObject(child) {
				continue
			}
			hasFailure = hasFailure || p.Inputs.Failed.IsBad(child) ||
				(p.Inputs.Available.IsBad(child) && classify(p.Classifier, p.Inputs.Available.Read(child)) == Failed) ||
				(p.Inputs.NonDegraded.IsBad(child) && classify(p.Classifier, p.Inputs.NonDegraded.Read(child)) == Failed)
		}
		if p.Inputs.Initialized.IsGood(parent) || !hasFailure {
			out := badOutcome("NotAvailable", "resource is not available")
			return &out, true
		}
	case Unknown:
		out := badOutcome("UnknownAvailability", "availability of resource is unknown")
		out.State.Assessment = Unknown
		return &out, true
	}
	return nil, false
}

// UpdateBase gates updates on the already-computed local initialization Condition.
type UpdateBase struct {
	LocalBaseFirst
	Initialized ConditionType
}

func (p UpdateBase) Before(parent Object, children map[string]Object, base metav1.Condition) (*Outcome, bool) {
	if result, done := p.LocalBaseFirst.Before(parent, children, base); done {
		return result, true
	}
	if !p.IncludeChildren(base) {
		return nil, false
	}
	if isDeleting(parent) {
		out := badOutcome("Deleting", "resource is being deleted")
		return &out, true
	}
	if !p.Initialized.IsGood(parent) {
		out := badOutcome("NotYetInitialized", "waiting for resource to be initialized")
		return &out, true
	}
	return nil, false
}

// END LOCAL

// BEGIN DELETION
// DeletionBase preserves a local failed deletion without forcing other outputs terminal.
type DeletionBase struct {
	Classifier Classifier
	Output     ConditionType
}

func (p DeletionBase) Before(parent Object, children map[string]Object, base metav1.Condition) (*Outcome, bool) {
	if !isDeleting(parent) {
		return nil, true
	}
	if classify(p.Classifier, base) == Failed {
		out := copyOutcome(base, p.Output, p.Classifier)
		return &out, true
	}
	return nil, false
}
func (DeletionBase) IncludeChildren(base metav1.Condition) bool { return true }

// DeletingChildren selects one deletion or teardown diagnosis from each supplied child.
type DeletingChildren struct {
	Inputs     ServingInputs
	Classifier Classifier
}

func (p DeletingChildren) Select(parent, child Object) (Contribution, bool) {
	pick := func(check ConditionType, escalation Escalation, fallback string) (Contribution, bool) {
		return Contribution{State: State{Bad, escalation}, Condition: check.Read(child), Source: child, Fallback: fallback}, true
	}
	if isDeleting(child) {
		deleting := p.Inputs.Deleting.Read(child)
		if p.Inputs.Deleting.Assessment(deleting) == Bad && deleting.Reason != "Deleting" {
			return pick(p.Inputs.Deleting, None, "fails to delete")
		}
		if p.Inputs.Synced.IsBad(child) {
			return pick(p.Inputs.Synced, None, "fails to sync")
		}
		if deleting.Status != metav1.ConditionUnknown {
			return pick(p.Inputs.Deleting, None, "is being deleted")
		}
		return Contribution{State: State{Bad, None}, Source: child, SuppressDetail: true}, true
	}
	if child.GetCreationTimestamp().Time.Before(parent.GetDeletionTimestamp().Time) || p.Inputs.Running.Read(child).Status == metav1.ConditionUnknown {
		return Contribution{}, false
	}
	switch {
	case p.Inputs.Synced.IsBad(child) && classify(p.Classifier, p.Inputs.Synced.Read(child)) == WaitingForHuman:
		return pick(p.Inputs.Synced, WaitingForHuman, "is waiting for human")
	case p.Inputs.Running.IsBad(child):
		return pick(p.Inputs.Running, None, "is running")
	case p.Inputs.Failed.IsBad(child):
		return pick(p.Inputs.Failed, Failed, "failed")
	default:
		return Contribution{}, false
	}
}

// DeletionReduction keeps deleting children ahead of teardown jobs in normal Go.
type DeletionReduction struct {
	Inputs     ServingInputs
	Classifier Classifier
}

func (p DeletionReduction) Reduce(base metav1.Condition, objects map[string]Object, _ []Contribution, children []Contribution) Outcome {
	var deleting, errors, failed, human, running []Contribution
	for _, c := range children {
		if isDeleting(c.Source) {
			deleting = append(deleting, c)
			if c.Condition.Type == p.Inputs.Synced.Type ||
				(c.Condition.Type == p.Inputs.Deleting.Type && p.Inputs.Deleting.Assessment(c.Condition) == Bad && c.Condition.Reason != "Deleting") {
				errors = append(errors, c)
			}
			continue
		}
		switch c.State.Escalation {
		case Failed:
			failed = append(failed, c)
		case WaitingForHuman:
			human = append(human, c)
		default:
			running = append(running, c)
		}
	}
	outcome := func(reason string, escalation Escalation, summary string, group []Contribution) Outcome {
		var causes []Cause
		for _, c := range group {
			if !c.SuppressDetail {
				causes = append(causes, cause(c))
			}
		}
		return Outcome{State: State{Bad, escalation}, Reason: reason, Summary: summary, Causes: causes}
	}
	if len(deleting) == 0 {
		switch {
		case len(failed) > 0:
			return outcome("TearDownFailed", Failed, groupHead("tear-down jobs failed", failed), failed)
		case len(human) > 0:
			return outcome("WaitingForHuman", WaitingForHuman, groupHead("tear-down jobs waiting for human", human), human)
		case len(running) > 0:
			return outcome("Deleting", None, groupHead("tear-down jobs running", running), running)
		case len(sortedChildren(objects)) > 0:
			var refs []string
			for _, c := range sortedChildren(objects) {
				refs = append(refs, c.Name)
			}
			return outcome("Deleting", None, "resource is being deleted, remaining: "+limitNames(refs), nil)
		default:
			return copyOutcome(base, p.Inputs.Deleting, p.Classifier)
		}
	}
	if len(errors) > 0 {
		prefix := ""
		if n := len(deleting) - len(errors); n > 0 {
			prefix = fmt.Sprintf("deleting %d resources", n)
		}
		return outcome("ChildrenDeletionError", None, joinHeads(prefix, groupHead("with errors", errors)), errors)
	}
	return outcome("Deleting", None, groupHead("deleting children", deleting), deleting)
}

// END DELETION

// BEGIN PROFILE
// The reference profile binds Condition names and polarities explicitly as ordinary data.
var InfraConditions = ServingInputs{
	Available: ConditionType{"Available", TrueIsGood}, Initialized: ConditionType{"Initialized", TrueIsGood},
	NonDegraded: ConditionType{"NonDegraded", TrueIsGood}, Synced: ConditionType{"Synced", TrueIsGood},
	UpToDate: ConditionType{"UpToDate", TrueIsGood},
	Running:  ConditionType{"Running", FalseIsGood}, Succeeded: ConditionType{"Succeeded", TrueIsGood},
	Failed: ConditionType{"Failed", FalseIsGood}, Deleting: ConditionType{"Deleting", FalseIsGood},
}
var InfraClassification Classifier = ReasonClassifier{Suffixes: []ReasonSuffix{{"Failed", Failed}, {"WaitingForHuman", WaitingForHuman}}}
var beforeInitialization Propagation = BeforeInitialized{Parent: InfraConditions.Initialized}
var existingChildren Propagation = ExistingChildren{Parent: InfraConditions.Initialized, Child: InfraConditions.Initialized}
var infraMessages Formatter = InfraMessages{MaxLines: 7, InlineChars: 80}

// Each flat value composes configured implementations of the domain interfaces.
var InfraAvailability = Aggregation{
	Derived: []Derivation{InfraInitialization{}},
	Output:  InfraConditions.Available,
	Parent:  LocalBaseFirst{Base: InfraConditions.Available, Classifier: InfraClassification, OverrideOnDeletion: true},
	Local:   PrefixedChecks{Prefix: "Available", Polarity: TrueIsGood, Classifier: InfraClassification},
	Children: ServingChildren{Inputs: InfraConditions, Condition: InfraConditions.Available, Classifier: InfraClassification,
		False: existingChildren, Failure: existingChildren, Human: Never{}, Unknown: beforeInitialization,
		SyncError: beforeInitialization, JobRunning: beforeInitialization, JobFailed: beforeInitialization,
		BadDescription: "is not available"},
	Reduction: ParentChildReduction{Inputs: InfraConditions, Output: InfraConditions.Available, Classifier: InfraClassification, BadSuffix: "NotAvailable", BadDescription: "unavailable children"},
	Messages:  infraMessages, LegacyGeneration: true,
}
var InfraNonDegraded = Aggregation{
	Output: InfraConditions.NonDegraded,
	Parent: DegradationBase{LocalBaseFirst: LocalBaseFirst{Base: InfraConditions.NonDegraded, Classifier: InfraClassification, OverrideOnDeletion: true}, Inputs: InfraConditions},
	Local:  PrefixedChecks{Prefix: "NonDegraded", Polarity: TrueIsGood, Classifier: InfraClassification},
	Children: ServingChildren{Inputs: InfraConditions, Condition: InfraConditions.NonDegraded, Classifier: InfraClassification,
		False: Always{}, Failure: Always{}, Human: Never{}, SyncError: Always{}, Unavailable: Always{}, JobFailed: Always{}, BadDescription: "is degraded"},
	Reduction: ParentChildReduction{Inputs: InfraConditions, Output: InfraConditions.NonDegraded, Classifier: InfraClassification, BadSuffix: "Degraded", BadDescription: "degraded children"},
	Messages:  infraMessages, LegacyGeneration: true,
}
var InfraUpToDate = Aggregation{
	Output: InfraConditions.UpToDate,
	Parent: UpdateBase{LocalBaseFirst: LocalBaseFirst{Base: InfraConditions.UpToDate, Classifier: InfraClassification}, Initialized: InfraConditions.Initialized},
	Local:  PrefixedChecks{Prefix: "UpToDate", Polarity: TrueIsGood, Classifier: InfraClassification},
	Children: ServingChildren{Inputs: InfraConditions, Condition: InfraConditions.UpToDate, Classifier: InfraClassification,
		False: Always{}, Failure: Never{}, Human: Always{}, SyncError: Always{}, Uninitialized: Always{}, JobRunning: Always{}, JobFailed: Always{}, BadDescription: "is updating"},
	Reduction: ParentChildReduction{Inputs: InfraConditions, Output: InfraConditions.UpToDate, Classifier: InfraClassification, BadSuffix: "Updating", BadDescription: "updating children"},
	Messages:  infraMessages, LegacyGeneration: true,
}
var InfraSynced = Aggregation{
	Output:    InfraConditions.Synced,
	Parent:    LocalOnly{LocalBaseFirst{Base: InfraConditions.Synced, Classifier: InfraClassification}},
	Local:     PrefixedChecks{Prefix: "Synced", Polarity: TrueIsGood, Classifier: InfraClassification},
	Reduction: ParentChildReduction{Inputs: InfraConditions, Output: InfraConditions.Synced, Classifier: InfraClassification, LocalOnly: true},
	Messages:  infraMessages, LegacyGeneration: true,
}
var InfraDeleting = Aggregation{
	DefaultBase: &metav1.Condition{Type: "Deleting", Status: metav1.ConditionTrue, Reason: "Deleting", Message: "resource is being deleted"},
	Output:      InfraConditions.Deleting,
	Parent:      DeletionBase{Classifier: InfraClassification, Output: InfraConditions.Deleting},
	Children:    DeletingChildren{Inputs: InfraConditions, Classifier: InfraClassification},
	Reduction:   DeletionReduction{Inputs: InfraConditions, Classifier: InfraClassification},
	Messages:    infraMessages, LegacyGeneration: true,
}

// InfraRules expresses the Infra behavior as data consumed by the generic Aggregate function.
var InfraRules = Rules{
	Outputs:       []Aggregation{InfraAvailability, InfraNonDegraded, InfraUpToDate, InfraSynced, InfraDeleting},
	LocalFirst:    true,
	UnmappedLocal: InfraLocalConditions{},
	IgnoreLocal:   []string{InfraConditions.Initialized.Type},
	Now:           time.Now,
}

// InfraProfile optionally binds rules for callers that prefer a short application-specific facade.
type InfraProfile struct{ Rules Rules }

// Infra injects InfraRules; the facade has no evaluation or sequencing implementation of its own.
var Infra = InfraProfile{Rules: InfraRules}

// END PROFILE

// BEGIN CALLER
// InfraSummary is a presentation implementation; it neither selects children nor mutates status.
type InfraSummary struct {
	Inputs     ServingInputs
	Classifier Classifier
	Messages   Formatter
}

func (p InfraSummary) Present(parent Object) (string, string) {
	v := p.Inputs
	if len(parent.GetConditions()) == 0 {
		return "Initializing", ""
	}
	selected := func(check ConditionType, ordinary string) (string, string) {
		c := check.Read(parent)
		phase := ordinary
		if classify(p.Classifier, c) == WaitingForHuman {
			phase = c.Reason
		}
		return phase, p.Messages.Inline(c)
	}
	if v.Synced.IsBad(parent) {
		return selected(v.Synced, "Error")
	}
	if v.Deleting.IsBad(parent) {
		return selected(v.Deleting, "Deleting")
	}
	if isDeleting(parent) {
		return "Deleting", ""
	}
	available := v.Available.Read(parent)
	if v.Available.Assessment(available) == Bad && classify(p.Classifier, available) == Failed {
		return "Failed", p.Messages.Inline(available)
	}
	if v.Initialized.IsBad(parent) {
		if v.Available.Assessment(available) == Bad {
			return "Initializing", p.Messages.Inline(available)
		}
		return "Initializing", p.Messages.Inline(v.Initialized.Read(parent))
	}
	if v.Available.Assessment(available) == Bad {
		return selected(v.Available, "Unavailable")
	}
	if v.NonDegraded.IsBad(parent) {
		if classify(p.Classifier, v.NonDegraded.Read(parent)) == WaitingForHuman {
			return selected(v.NonDegraded, "Degraded")
		}
		if v.UpToDate.IsBad(parent) {
			return "Degraded and updating", p.Messages.Inline(v.UpToDate.Read(parent))
		}
		return "Degraded", p.Messages.Inline(v.NonDegraded.Read(parent))
	}
	if v.UpToDate.IsBad(parent) {
		return selected(v.UpToDate, "Updating")
	}
	return "Ready", ""
}

// DeriveInitialized is a local Go helper consuming supplied current Conditions and a persisted latch.
func DeriveInitialized(parent Object, now time.Time) metav1.Condition {
	v := InfraConditions
	if v.Initialized.IsGood(parent) {
		return v.Initialized.Read(parent)
	}
	reason, message, status := "NotYetAvailable", "waiting for resource to be available", metav1.ConditionFalse
	if isDeleting(parent) {
		reason, message = "Deleting", "resource is being deleted"
	} else if v.Available.IsGood(parent) {
		reason, message, status = "Initialized", "", metav1.ConditionTrue
	}
	return stamp(parent, metav1.Condition{Type: v.Initialized.Type, Status: status, Reason: reason, Message: message}, now)
}

// InfraInitialization derives the explicit Infra initialized latch after availability evaluation.
type InfraInitialization struct{}

func (InfraInitialization) Derive(parent Object, now time.Time) []metav1.Condition {
	return []metav1.Condition{DeriveInitialized(parent, now)}
}

// InfraLocalConditions preserves local-only prefix aggregation for additional Infra Condition types.
type InfraLocalConditions struct{}

func (InfraLocalConditions) Evaluate(parent Object, local metav1.Condition, now time.Time) (*metav1.Condition, error) {
	output := ConditionType{local.Type, TrueIsGood}
	rule := Aggregation{
		Output:           output,
		Parent:           LocalOnly{LocalBaseFirst{Base: output, Classifier: InfraClassification}},
		Local:            PrefixedChecks{Prefix: local.Type, Polarity: TrueIsGood, Classifier: InfraClassification},
		Reduction:        ParentChildReduction{Output: output, Classifier: InfraClassification, LocalOnly: true},
		Messages:         infraMessages,
		LegacyGeneration: true,
	}
	return rule.Evaluate(parent, nil, local, now)
}

// Aggregate only injects the facade's rules into the generic resource-level operation.
func (p InfraProfile) Aggregate(parent Conditioned, children map[string]Object, local ...metav1.Condition) (bool, error) {
	return Aggregate(parent, children, p.Rules, local...)
}

// Evaluate only injects the facade's rules into generic pure evaluation.
func (p InfraProfile) Evaluate(parent Object, children map[string]Object, local ...metav1.Condition) ([]metav1.Condition, error) {
	return Evaluate(parent, children, p.Rules, local...)
}

// END CALLER

// BEGIN HELPERS
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
func cause(c Contribution) Cause {
	label := c.Reference
	if c.CheckName != "" {
		label = c.CheckName
	}
	if c.Detail != nil {
		return Cause{label, *c.Detail}
	}
	message, reason := strings.TrimSpace(c.Condition.Message), strings.TrimSpace(c.Condition.Reason)
	switch {
	case message == "" && reason == "":
		message = c.Fallback
	case message == "":
		message = reason
	case reason != "" && !strings.Contains(message, reason):
		message += " (" + reason + ")"
	}
	return Cause{label, message}
}
func limitNames(names []string) string {
	if len(names) <= 3 {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s, and %d more", strings.Join(names[:3], ", "), len(names)-3)
}
func limitedNames(group []Contribution, local bool) string {
	names := make([]string, 0, len(group))
	for _, c := range group {
		name := c.Reference
		if local {
			name = c.CheckName
		}
		names = append(names, name)
	}
	return limitNames(names)
}
func groupHead(prefix string, group []Contribution) string {
	if len(group) == 0 {
		return ""
	}
	if len(group) == 1 {
		return prefix
	}
	return prefix + ": " + limitedNames(group, false)
}
func localGroupHead(prefix string, group []Contribution) string {
	if len(group) == 0 {
		return ""
	}
	if len(group) == 1 {
		return prefix
	}
	return prefix + ": " + limitedNames(group, true)
}
func joinHeads(heads ...string) string {
	var nonempty []string
	for _, s := range heads {
		if s = strings.TrimSpace(s); s != "" {
			nonempty = append(nonempty, s)
		}
	}
	return strings.Join(nonempty, ", ")
}
func childReason(children map[string]Object, group []Contribution, suffix string) string {
	if len(children) == 1 && len(group) == 1 {
		return group[0].Reference + suffix
	}
	return "Children" + suffix
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

// END HELPERS
