// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package conditions

import (
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"strconv"
	"time"
)

// Definition is ordinary Go configuration. Helpers construct these same public values;
// loops, functions and struct literals can be freely mixed with them.
// Empty Resources publishes a global schema.
type Definition struct {
	// Scheme resolves typed children with empty TypeMeta. Register types before Define;
	// do not mutate the scheme while aggregations use it. Explicit object GVK wins.
	Scheme    *runtime.Scheme
	Resources []ResourceMatch
	Outputs   []OutputDefinition
	Children  []ChildBinding
	Now       func() time.Time
}

// OutputDefinition declares both a published contract and its derivation.
// Nil Parent, Reduction and Formatter select the standard local-precedence/All behavior.
// Nil Children uses classified child bindings. A custom Children selector reads the
// original objects and owns their interpretation, subject to the same binding scopes.
// Callbacks and interface implementations must be read-only and concurrency-safe.
type OutputDefinition struct {
	Name              string
	Spec              Spec
	LocalOnly         bool
	Requires          []string
	DefaultBase       *metav1.Condition
	Parent            ParentPolicy
	LocalChecks       LocalSelector
	Children          ChildSelector
	Reduction         Reducer
	Formatter         Formatter
	Generation        GenerationPolicy
	Derived           []Derivation
	DerivedConditions map[string]Spec
}

// Output declares an output with its published interpretation, exactly once.
func Output(name string, spec Spec) OutputDefinition {
	return OutputDefinition{Name: name, Spec: spec}
}
func (o OutputDefinition) Local() OutputDefinition { o.LocalOnly = true; return o }
func (o OutputDefinition) Reduce(reducer Reducer) OutputDefinition {
	if isNil(reducer) {
		panic("conditions: nil reducer")
	}
	o.Reduction = reducer
	return o
}
func (o OutputDefinition) Format(formatter Formatter) OutputDefinition {
	if isNil(formatter) {
		panic("conditions: nil formatter")
	}
	o.Formatter = formatter
	return o
}

// Aggregation is a compiled definition and its published output contract.
// Construct it once, usually in a package variable, and share it across reconciles.
type Aggregation struct {
	scheme   *runtime.Scheme
	schema   Schema
	rules    evaluationPlan
	children []compiledChild
}

// Schema returns an independent copy of the contract produced by this aggregation.
func (a *Aggregation) Schema() Schema { return cloneSchema(a.schema) }

// Define validates and snapshots ordinary Go configuration. Invalid programmer
// configuration panics at construction; missing observations are ordinary data.
func Define(d Definition) *Aggregation {
	s := Schema{Resources: append([]ResourceMatch(nil), d.Resources...), Conditions: map[string]Spec{}}
	add := func(name string, spec Spec) {
		if _, exists := s.Conditions[name]; exists {
			panic(fmt.Sprintf("conditions: duplicate output %q", name))
		}
		if spec.Suffix {
			panic(fmt.Sprintf("conditions: output %q cannot be a suffix family", name))
		}
		s.Conditions[name] = spec
	}
	for _, o := range d.Outputs {
		if o.Children != nil && isNil(o.Children) || o.LocalChecks != nil && isNil(o.LocalChecks) {
			panic("conditions: nil output selector")
		}
		if o.Generation > NoObservedGeneration {
			panic("conditions: invalid generation policy")
		}
		spec := o.Spec
		_, all := o.Reduction.(All)
		if !o.LocalOnly && (o.Reduction == nil || all) {
			// Clone before wrapping so exported schemas do not share mutable reason maps.
			spec = cloneSchema(Schema{Conditions: map[string]Spec{o.Name: spec}}).Conditions[o.Name]
			spec.Reasons = aggregateClassifier{fallback: spec.Reasons}
		}
		add(o.Name, spec)
		derived := map[string]bool{}
		for _, derivation := range o.Derived {
			if isNil(derivation) {
				panic("conditions: nil derivation")
			}
			for _, name := range derivation.Outputs() {
				spec, ok := o.DerivedConditions[name]
				if !ok {
					panic(fmt.Sprintf("conditions: undeclared derived output %q", name))
				}
				add(name, spec)
				derived[name] = true
			}
		}
		for name := range o.DerivedConditions {
			if !derived[name] {
				panic(fmt.Sprintf("conditions: condition %q has no derivation", name))
			}
		}
	}
	s = cloneSchema(s)
	validateSchema(s)
	a := &Aggregation{scheme: d.Scheme, schema: s, rules: evaluationPlan{Now: d.Now}}
	for _, binding := range d.Children {
		a.children = append(a.children, compileChild(binding, s))
	}
	for i, left := range a.children {
		for _, right := range a.children[i+1:] {
			for _, l := range left.schema.Resources {
				for _, r := range right.schema.Resources {
					if l.overlaps(r) && (l.contains(r) == r.contains(l)) {
						panic(fmt.Sprintf("conditions: ambiguous child scopes %s and %s", l, r))
					}
				}
			}
		}
	}
	for _, o := range d.Outputs {
		spec := s.Conditions[o.Name]
		typ := ConditionType{Type: o.Name, Polarity: spec.Polarity}
		rule := compiledRule{
			Output: typ, Requires: append([]string(nil), o.Requires...), Derived: append([]Derivation(nil), o.Derived...),
			Parent: o.Parent, Local: o.LocalChecks, Children: o.Children,
			Reduction: o.Reduction, Formatter: o.Formatter, Generation: o.Generation,
		}
		if rule.Parent == nil {
			rule.Parent = LocalBaseFirst{Base: typ, Classifier: spec.Reasons}
		}
		if rule.Formatter == nil {
			rule.Formatter = BoundedMessages{}
		}
		if rule.Children == nil {
			rule.Children = classifiedSelector{aggregation: a, target: o.Name}
		}
		_, all := rule.Reduction.(All)
		if rule.Reduction == nil || all {
			rule.Reduction = configuredAll{Condition: typ, Classifier: spec.Reasons}
		}
		// A custom parent policy owns the missing-base convention. Otherwise a healthy
		// default lets the standard reduction operate without a local assertion.
		if o.DefaultBase != nil {
			base := *o.DefaultBase
			if base.Type != "" && base.Type != o.Name {
				panic(fmt.Sprintf("conditions: default base disagrees with output %q", o.Name))
			}
			base.Type = o.Name
			rule.DefaultBase = &base
		} else if o.Parent == nil && !o.LocalOnly {
			rule.DefaultBase = &metav1.Condition{Type: o.Name, Status: encodeStatus(Good, spec.Polarity), Reason: "ConditionsSatisfied"}
		}
		if o.LocalOnly {
			if o.Children != nil {
				panic("conditions: local output cannot select children")
			}
			rule.Children = nil
			if o.Reduction == nil {
				rule.Reduction = localAssertion{typ, spec.Reasons}
				rule.Generation = PreserveBaseGeneration
			}
		}
		a.rules.Outputs = append(a.rules.Outputs, rule)
	}
	if _, _, err := plan(a.rules); err != nil {
		panic("conditions: " + err.Error())
	}
	return a
}

func (a *Aggregation) child(object Object) (*compiledChild, error) {
	// Global-only definitions need no API identity (including non-runtime adapters).
	scoped := false
	for _, binding := range a.children {
		for _, scope := range binding.schema.Resources {
			scoped = scoped || scope.GroupSet
		}
	}
	if !scoped {
		if len(a.children) == 0 {
			return nil, nil
		}
		return &a.children[0], nil
	}
	typed, ok := object.(interface{ GetObjectKind() schema.ObjectKind })
	if !ok {
		return nil, fmt.Errorf("missing API identity for %T; supply WithGVK", object)
	}
	kinds, err := resolveKinds(typed, a.scheme)
	if err != nil {
		return nil, err
	}
	// Several registered GVKs are harmless only if all select the same binding.
	// Never choose a preferred version or silently drop an unresolved child.
	result := a.childKind(kinds[0])
	for _, kind := range kinds[1:] {
		if a.childKind(kind) != result {
			return nil, fmt.Errorf("ambiguous child identity for %T: %v", object, kinds)
		}
	}
	return result, nil
}

func (a *Aggregation) childKind(gvk schema.GroupVersionKind) *compiledChild {
	var best *compiledChild
	var scope ResourceMatch
	for i := range a.children {
		for _, candidate := range a.children[i].schema.Resources {
			if candidate.matches(gvk) && (best == nil || scope.contains(candidate)) {
				best = &a.children[i]
				scope = candidate
			}
		}
	}
	return best
}

type classifiedSelector struct {
	aggregation *Aggregation
	target      string
}

func (s classifiedSelector) Select(parent, child Object) []Contribution {
	binding, err := s.aggregation.child(child)
	if err != nil {
		panic("conditions: " + err.Error())
	}
	if binding == nil {
		return nil
	}
	return binding.selectFor(s.target, parent, child)
}

// EvaluateAggregation computes the complete desired condition slice without mutation.
// Caller errors from the low-level evaluator are returned. Children outside all
// bindings are filtered out. Scoped schemas require TypeMeta, Definition.Scheme,
// or WithGVK. Resolution errors are returned before any mutation.
func EvaluateAggregation(parent Object, children map[string]Object, a *Aggregation, local ...metav1.Condition) ([]metav1.Condition, error) {
	if a == nil {
		return nil, fmt.Errorf("aggregation is nil")
	}
	selected := make(map[string]Object, len(children))
	for name, child := range children {
		// Preserve reserved nil slots for domain reducers that count expected children.
		// Selectors skip nil observations, as in the low-level evaluator.
		if isNil(child) {
			selected[name] = child
			continue
		}
		binding, err := a.child(child)
		if err != nil {
			return nil, fmt.Errorf("child %q: %w", name, err)
		}
		if binding != nil {
			selected[name] = child
		}
	}
	return evaluate(parent, selected, a.rules, local...)
}

// Aggregate applies the desired slice exactly once and returns the native setter's change flag.
// It panics on invalid caller input before calling the setter. It performs no API writes.
func Aggregate(parent Conditioned, children map[string]Object, a *Aggregation, local ...metav1.Condition) bool {
	desired, err := EvaluateAggregation(parent, children, a, local...)
	if err != nil {
		panic("conditions: " + err.Error())
	}
	return parent.SetConditions(desired)
}

// AggregateSlice is Aggregate with stable positional diagnostic names ("0", "1", ...).
// Nil children are skipped without renumbering. Use Aggregate for caller-defined names.
func AggregateSlice(parent Conditioned, children []Object, a *Aggregation, local ...metav1.Condition) bool {
	named := make(map[string]Object, len(children))
	for i, child := range children {
		named[strconv.Itoa(i)] = child
	}
	return Aggregate(parent, named, a, local...)
}

func encodeStatus(assessment Assessment, polarity Polarity) metav1.ConditionStatus {
	if assessment == Unknown {
		return metav1.ConditionUnknown
	}
	good := assessment == Good
	if polarity == FalseIsGood {
		good = !good
	}
	if good {
		return metav1.ConditionTrue
	}
	return metav1.ConditionFalse
}

type aggregateClassifier struct{ fallback Classifier }

func (c aggregateClassifier) Classify(value metav1.Condition) Escalation {
	switch value.Reason {
	case "ConditionsFailed":
		return Failed
	case "ConditionsWaitingForHuman":
		return WaitingForHuman
	}
	return classify(c.fallback, value)
}

// All combines normalized contributions: Bad > Unknown > Good. Among bad inputs,
// Failed > WaitingForHuman > None. No contributions is Unknown. A non-good local
// assertion takes precedence; healthy local assertions permit child aggregation.
// Output reasons encode escalation and are understood by the Schema returned by Define.
type All struct{}
type configuredAll struct {
	Condition  ConditionType
	Classifier Classifier
}
type localAssertion struct {
	condition  ConditionType
	classifier Classifier
}

func (r localAssertion) Reduce(base metav1.Condition, _ map[string]Object, _, _ []Contribution) Outcome {
	return copyOutcome(base, r.condition, r.classifier)
}
func (r configuredAll) Reduce(base metav1.Condition, objects map[string]Object, local, children []Contribution) Outcome {
	if r.Condition.Assessment(base) != Good {
		return copyOutcome(base, r.Condition, r.Classifier)
	}
	return (All{}).Reduce(base, objects, local, children)
}

func (All) Reduce(_ metav1.Condition, _ map[string]Object, local, children []Contribution) Outcome {
	values := append(append([]Contribution(nil), local...), children...)
	state := State{Assessment: Good}
	if len(values) == 0 {
		state.Assessment = Unknown
	}
	for _, value := range values {
		if value.State.Assessment == Bad {
			state.Assessment = Bad
			if value.State.Escalation > state.Escalation {
				state.Escalation = value.State.Escalation
			}
		} else if value.State.Assessment == Unknown && state.Assessment == Good {
			state.Assessment = Unknown
		}
	}
	out := Outcome{State: state, Reason: "ConditionsSatisfied", Summary: "All conditions are satisfied"}
	if state.Assessment == Unknown {
		out.Reason = "ConditionsUnknown"
		out.Summary = "Condition state is unknown"
	}
	if state.Assessment == Bad {
		out.Reason = "ConditionsNotSatisfied"
		out.Summary = "Conditions are not satisfied"
		switch state.Escalation {
		case Failed:
			out.Reason = "ConditionsFailed"
			out.Summary = "Conditions failed"
		case WaitingForHuman:
			out.Reason = "ConditionsWaitingForHuman"
			out.Summary = "Conditions require human intervention"
		}
	}
	for _, value := range values {
		if value.State.Assessment == state.Assessment && state.Assessment != Good {
			message := value.Condition.Message
			if message == "" {
				message = value.Condition.Reason
			}
			out.Causes = append(out.Causes, Cause{Reference: value.Reference, Message: message})
		}
	}
	return out
}
