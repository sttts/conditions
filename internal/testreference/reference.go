// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package testreference is the frozen pre-Definition evaluator for conformance tests.
// Production code must not import this package.
package testreference

import (
	"fmt"
	c "github.com/sttts/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"reflect"
	"sort"
	"time"
)

type (
	ConditionType    = c.ConditionType
	Object           = c.Object
	Conditioned      = c.Conditioned
	Derivation       = c.Derivation
	ParentPolicy     = c.ParentPolicy
	LocalSelector    = c.LocalSelector
	ChildSelector    = c.ChildSelector
	Reducer          = c.Reducer
	Formatter        = c.Formatter
	GenerationPolicy = c.GenerationPolicy
	Outcome          = c.Outcome
	Contribution     = c.Contribution
)

const (
	Unknown                = c.Unknown
	Good                   = c.Good
	FalseIsGood            = c.FalseIsGood
	PreserveBaseGeneration = c.PreserveBaseGeneration
	NoObservedGeneration   = c.NoObservedGeneration
)

var Lookup = c.Lookup

type Rules struct {
	Outputs []Rule
	Now     func() time.Time
}

type Rule struct {
	// Requires names selected outputs whose current-call results are visible to this rule.
	// Other rules' new results are isolated; previous persisted Conditions remain readable.
	Requires    []string
	DefaultBase *metav1.Condition
	Derived     []Derivation
	Output      ConditionType
	Parent      ParentPolicy
	Local       LocalSelector
	Children    ChildSelector
	Reduction   Reducer
	Formatter   Formatter
	Generation  GenerationPolicy
}

// Evaluate derives one desired Condition without modifying the supplied objects.
func (a Rule) Evaluate(parent Object, children map[string]Object, base metav1.Condition, now time.Time) (*metav1.Condition, error) {
	if isNil(parent) {
		return nil, fmt.Errorf("parent is nil")
	}
	if a.Output.Type == "" || isNil(a.Parent) || isNil(a.Reduction) || isNil(a.Formatter) {
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
		Message: a.Formatter.Format(outcome.Summary, outcome.Causes), ObservedGeneration: generation,
		LastTransitionTime: transition}, nil
}

// ApplyRules applies the configured rules to existing objects and reports in-memory changes.
func ApplyRules(parent Conditioned, children map[string]Object, rules Rules, local ...metav1.Condition) (bool, error) {
	desired, err := Evaluate(parent, children, rules, local...)
	if err != nil {
		return false, err
	}
	return parent.SetConditions(desired), nil
}

// ReasonSuffix assigns escalation to a matching reason suffix.
// namedObject retains the caller's diagnostic alias beside the supplied resource.
type namedObject struct {
	Name   string
	Object Object
}

func sortedChildren(children map[string]Object) []namedObject {
	result := make([]namedObject, 0, len(children))
	for name, object := range children {
		if !isNil(object) {
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

// isNil handles typed nil objects and policy implementations held in interfaces.
func isNil(object any) bool {
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

// Compose selects rule values from independent packages, without registration.
// It rejects invalid definitions and missing or cyclic dependencies before use.
// The returned slice is new; policy implementations must be pure and safe to share.
// Set the returned Rules.Now to inject a clock for the complete evaluation.
func Compose(groups ...[]Rule) (Rules, error) {
	rules := Rules{}
	for _, group := range groups {
		rules.Outputs = append(rules.Outputs, group...)
	}
	if _, _, err := plan(rules); err != nil {
		return Rules{}, err
	}
	return rules, nil
}

// plan validates definitions even when no local base will activate them.
func plan(rules Rules) ([]Rule, map[string]string, error) {
	definitions := make(map[string]Rule, len(rules.Outputs))
	owners := make(map[string]string, len(rules.Outputs))
	for _, rule := range rules.Outputs {
		name := rule.Output.Type
		if name == "" || isNil(rule.Parent) || isNil(rule.Reduction) || isNil(rule.Formatter) {
			return nil, nil, fmt.Errorf("incomplete aggregation for %q", name)
		}
		if _, exists := owners[name]; exists {
			return nil, nil, fmt.Errorf("duplicate output %q", name)
		}
		if rule.DefaultBase != nil && rule.DefaultBase.Type != name {
			return nil, nil, fmt.Errorf("default base %q does not match output %q", rule.DefaultBase.Type, name)
		}
		owners[name] = name
		for _, derivation := range rule.Derived {
			if isNil(derivation) {
				return nil, nil, fmt.Errorf("nil derivation for %q", name)
			}
			for _, output := range derivation.Outputs() {
				if output == "" {
					return nil, nil, fmt.Errorf("empty derived output for %q", name)
				}
				if previous, exists := owners[output]; exists {
					return nil, nil, fmt.Errorf("duplicate output %q (owners %q and %q)", output, previous, name)
				}
				owners[output] = name
			}
		}
		definitions[name] = rule
	}
	names := make([]string, 0, len(definitions))
	for name := range definitions {
		names = append(names, name)
	}
	sort.Strings(names)
	var ordered []Rule
	state := map[string]uint8{}
	var visit func(string) error
	visit = func(name string) error {
		if state[name] == 1 {
			return fmt.Errorf("dependency cycle at %q", name)
		}
		if state[name] == 2 {
			return nil
		}
		state[name] = 1
		rule := definitions[name]
		dependencies := append([]string(nil), rule.Requires...)
		sort.Strings(dependencies)
		for _, dependency := range dependencies {
			owner, ok := owners[dependency]
			if !ok {
				return fmt.Errorf("output %q requires missing output %q", name, dependency)
			}
			if err := visit(owner); err != nil {
				return err
			}
		}
		state[name] = 2
		ordered = append(ordered, rule)
		return nil
	}
	for _, name := range names {
		if err := visit(name); err != nil {
			return nil, nil, err
		}
	}
	return ordered, owners, nil
}

// Evaluate computes desired Conditions without modifying the supplied objects.
// Rules read a shared input snapshot plus only their declared dependency results.
func Evaluate(parent Object, children map[string]Object, rules Rules, local ...metav1.Condition) ([]metav1.Condition, error) {
	if isNil(parent) {
		return nil, fmt.Errorf("parent is nil")
	}
	ordered, owners, err := plan(rules)
	if err != nil {
		return nil, err
	}
	nowFunc := rules.Now
	if nowFunc == nil {
		nowFunc = time.Now
	}
	now := nowFunc()
	input := &conditionOverlay{Object: parent, conditions: append([]metav1.Condition(nil), parent.GetConditions()...)}
	bases := map[string]metav1.Condition{}
	local = append([]metav1.Condition(nil), local...)
	sort.Slice(local, func(i, j int) bool { return local[i].Type < local[j].Type })
	for _, value := range local {
		if _, exists := bases[value.Type]; exists {
			return nil, fmt.Errorf("duplicate local condition %q", value.Type)
		}
		bases[value.Type] = value
		if _, owned := owners[value.Type]; !owned {
			input.SetConditions(stamp(parent, value, now))
		}
	}
	results := map[string]metav1.Condition{}
	for _, rule := range ordered {
		base, provided := bases[rule.Output.Type]
		if !provided {
			if rule.DefaultBase == nil {
				continue
			}
			base = *rule.DefaultBase
		}
		view := &conditionOverlay{Object: parent, conditions: append([]metav1.Condition(nil), input.GetConditions()...)}
		for _, dependency := range rule.Requires {
			if value, ok := results[dependency]; ok {
				view.SetConditions(value)
			}
		}
		value, err := rule.Evaluate(view, children, base, now)
		if err != nil {
			return nil, err
		}
		if value == nil {
			continue
		}
		results[value.Type] = *value
		view.SetConditions(*value)
		for _, derivation := range rule.Derived {
			declared := map[string]bool{}
			for _, name := range derivation.Outputs() {
				declared[name] = true
			}
			for _, derived := range derivation.Derive(view, now) {
				if !declared[derived.Type] {
					return nil, fmt.Errorf("output %q derivation wrote undeclared type %q", rule.Output.Type, derived.Type)
				}
				if _, exists := results[derived.Type]; exists {
					return nil, fmt.Errorf("duplicate derived result %q", derived.Type)
				}
				results[derived.Type] = derived
			}
		}
	}
	// Preserve existing slice order; append newly produced types in canonical order.
	working := &conditionOverlay{Object: parent, conditions: append([]metav1.Condition(nil), input.GetConditions()...)}
	names := make([]string, 0, len(results))
	for name := range results {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		working.SetConditions(results[name])
	}
	return working.GetConditions(), nil
}
