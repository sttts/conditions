// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package conditions_test

import (
	c "github.com/sttts/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"reflect"
	"testing"
	"time"
)

func TestHelpersAndPlainGoHaveSameBehavior(t *testing.T) {
	input := contract("Available", c.FalseIsGood)
	// New upstream conditions are additive; consumers need no new Ignore entries.
	input.Conditions["NewCondition"] = c.Spec{Polarity: c.TrueIsGood}
	clock := func() time.Time { return time.Unix(123, 0) }
	helpers := c.Define(c.Definition{
		Now:      clock,
		Outputs:  []c.OutputDefinition{c.Output("Ready", c.Spec{Polarity: c.TrueIsGood}).Format(c.BoundedMessages{}), c.Output("Synced", c.Spec{}).Local()},
		Children: []c.ChildBinding{c.Child(input, c.Classify(c.Map("Available", "Ready").When(c.Status(metav1.ConditionTrue)).Escalate(c.Failed, c.Reason("Broken")))).For(c.G("db.io"))},
	})
	plain := c.Define(c.Definition{
		Now: clock,
		Outputs: []c.OutputDefinition{
			{Name: "Ready", Spec: c.Spec{Polarity: c.TrueIsGood}, Formatter: c.BoundedMessages{}},
			{Name: "Synced", Spec: c.Spec{}, LocalOnly: true},
		},
		Children: []c.ChildBinding{{
			Schema:    input,
			Resources: []c.ResourceMatch{{Group: "db.io", GroupSet: true}},
			Classification: c.Classification{Mappings: []c.Mapping{{
				Source: "Available", Target: "Ready",
				Predicate: func(value metav1.Condition) bool { return value.Status == metav1.ConditionTrue },
				Overrides: []c.EscalationOverride{{Level: c.Failed, Predicate: func(value metav1.Condition) bool { return value.Reason == "Broken" }}},
			}}},
		}},
	})
	for _, child := range []*object{observation("Available", metav1.ConditionTrue, "Broken"), observation("Available", metav1.ConditionFalse, "Fine"), {}} {
		p, q := &object{}, &object{}
		children := []c.Object{scopeObject("db.io", "v1", "DB", child)}
		c.AggregateSlice(p, children, helpers)
		c.AggregateSlice(q, children, plain)
		if !reflect.DeepEqual(p.conditions, q.conditions) {
			t.Fatalf("helpers and literals differ: %#v / %#v", p.conditions, q.conditions)
		}
	}
}

func TestGoCallbacksAndPublishedReasons(t *testing.T) {
	calls := 0
	spec := c.Spec{Polarity: c.TrueIsGood, Reasons: c.ClassifierFunc(func(value metav1.Condition) c.Escalation {
		if value.Reason == "CapacityExhausted" {
			return c.Failed
		}
		return c.None
	})}
	a := c.Define(c.Definition{
		Outputs: []c.OutputDefinition{{
			Name: "Healthy", Spec: spec,
			Children: c.ChildSelectorFunc(func(parent, child c.Object) []c.Contribution {
				calls++
				// Ordinary Go may inspect metadata and several conditions at once.
				if parent.GetDeletionTimestamp() != nil {
					return nil
				}
				available, _ := c.Lookup(child, "Available")
				return []c.Contribution{{State: c.State{Assessment: c.Bad}, Condition: available, Source: child}}
			}),
			Reduction: c.ReducerFunc(func(_ metav1.Condition, _ map[string]c.Object, _, selected []c.Contribution) c.Outcome {
				if len(selected) == 0 {
					return c.Outcome{State: c.State{Assessment: c.Unknown}, Reason: "NoData"}
				}
				return c.Outcome{State: c.State{Assessment: c.Bad, Escalation: c.Failed}, Reason: "CapacityExhausted"}
			}),
		}},
		Children: []c.ChildBinding{c.Child(contract("Available", c.TrueIsGood), c.Classify()).For(c.G("db.io"))},
	})
	parent := &object{}
	child := observation("Available", metav1.ConditionFalse, "NoCapacity")
	c.AggregateSlice(parent, []c.Object{scopeObject("ignored.io", "v1", "DB", child), scopeObject("db.io", "v1", "DB", child)}, a)
	if calls != 1 {
		t.Fatalf("custom selector escaped binding scope: %d calls", calls)
	}
	top := c.Define(c.Definition{
		Outputs:  []c.OutputDefinition{c.Output("Ready", c.Spec{})},
		Children: []c.ChildBinding{c.Child(a.Schema(), c.Classify(c.Map("Healthy", "Ready")))},
	})
	root := &object{}
	c.AggregateSlice(root, []c.Object{parent}, top)
	if v, _ := c.Lookup(root, "Ready"); v.Reason != "ConditionsFailed" {
		t.Fatalf("lost callback's published reason semantics: %#v", v)
	}
}

func TestDefinitionSnapshotsReasonMapsAndMappings(t *testing.T) {
	reasons := map[string]c.Escalation{"Broken": c.Failed}
	spec := c.Spec{Reasons: c.ReasonClassifier{Reasons: reasons}}
	mapping := c.Map("Available", "Ready").Escalate(c.WaitingForHuman, c.Reason("Manual"))
	binding := c.Child(c.Schema{Conditions: map[string]c.Spec{"Available": spec}}, c.Classify(mapping))
	d := c.Definition{Outputs: []c.OutputDefinition{c.Output("Ready", spec)}, Children: []c.ChildBinding{binding}}
	a := c.Define(d)
	reasons["Broken"] = c.None
	mapping.Overrides[0].Level = c.None
	d.Outputs[0].Name = "Changed"
	d.Children[0].Classification.Mappings[0].Target = "Changed"
	published := a.Schema()
	if published.Conditions["Ready"].Reasons.Classify(metav1.Condition{Reason: "Broken"}) != c.Failed {
		t.Fatal("published classifier shares caller's map")
	}
	for _, tc := range []struct{ reason, want string }{{"Broken", "ConditionsFailed"}, {"Manual", "ConditionsWaitingForHuman"}} {
		p := &object{}
		c.AggregateSlice(p, []c.Object{observation("Available", metav1.ConditionFalse, tc.reason)}, a)
		if v, _ := c.Lookup(p, "Ready"); v.Reason != tc.want {
			t.Fatal(v)
		}
	}
}

func TestPublicConfigurationValidation(t *testing.T) {
	output := c.Output("Ready", c.Spec{})
	for _, tc := range []struct {
		name, want string
		def        c.Definition
	}{
		{"duplicate output", "duplicate output", c.Definition{Outputs: []c.OutputDefinition{output, output}}},
		{"local selector conflict", "local output", c.Definition{Outputs: []c.OutputDefinition{{Name: "Ready", LocalOnly: true, Children: c.ChildSelectorFunc(func(c.Object, c.Object) []c.Contribution { return nil })}}}},
		{"wrong default", "default base", c.Definition{Outputs: []c.OutputDefinition{{Name: "Ready", DefaultBase: &metav1.Condition{Type: "Other"}}}}},
		{"identity plus mappings", "SameNames", c.Definition{Outputs: []c.OutputDefinition{output}, Children: []c.ChildBinding{{Schema: contract("Ready", c.TrueIsGood), Classification: c.Classification{SameNames: true, Mappings: []c.Mapping{c.Map("Ready", "Ready")}}}}}},
		{"bad literal escalation", "invalid escalation", c.Definition{Outputs: []c.OutputDefinition{output}, Children: []c.ChildBinding{c.Child(contract("Ready", c.TrueIsGood), c.Classify(c.Mapping{Source: "Ready", Target: "Ready", Overrides: []c.EscalationOverride{{Level: 99}}}))}}},
	} {
		t.Run(tc.name, func(t *testing.T) { assertPanic(t, tc.want, func() { c.Define(tc.def) }) })
	}
}

func TestFormatterDoesNotRequireInlineOrChangeSemantics(t *testing.T) {
	calls := 0
	format := c.FormatterFunc(func(summary string, causes []c.Cause) string {
		calls++
		if len(causes) != 1 || causes[0].Reference != "database" {
			t.Fatalf("lost provenance: %#v", causes)
		}
		return summary + " / " + causes[0].Message
	})
	output := c.Output("Ready", c.Spec{}).Format(format)
	// The helper and the public field install the same one-method interface.
	var _ c.Formatter = output.Formatter
	d := c.Definition{
		Outputs:  []c.OutputDefinition{output},
		Children: []c.ChildBinding{c.Child(contract("Ready", c.TrueIsGood), c.Identity)},
		Now:      func() time.Time { return time.Unix(123, 0) },
	}
	custom, standard := &object{}, &object{}
	children := map[string]c.Object{"database": observation("Ready", metav1.ConditionFalse, "Starting")}
	c.Aggregate(custom, children, c.Define(d))
	d.Outputs[0].Formatter = c.BoundedMessages{}
	c.Aggregate(standard, children, c.Define(d))
	got, _ := c.Lookup(custom, "Ready")
	want, _ := c.Lookup(standard, "Ready")
	if calls != 1 || got.Message != "Conditions are not satisfied / Starting" {
		t.Fatalf("formatter not applied: %#v", got)
	}
	got.Message = want.Message
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("formatting changed semantics or metadata: %#v / %#v", got, want)
	}
	var inline c.InlineFormatter = c.InlineFormatterFunc(func(value metav1.Condition) string { return value.Reason })
	if inline.Inline(want) != want.Reason {
		t.Fatal("independent inline presentation failed")
	}
}

func TestOutputCompositionValidation(t *testing.T) {
	good := c.Output("Ready", c.Spec{})
	for _, tc := range []struct {
		name, want string
		outputs    []c.OutputDefinition
	}{
		{"duplicate", "duplicate output", []c.OutputDefinition{good, good}},
		{"missing dependency", "requires missing output", []c.OutputDefinition{{Name: "Observed", Requires: []string{"Ready"}}}},
		{"self cycle", "dependency cycle", []c.OutputDefinition{{Name: "Ready", Requires: []string{"Ready"}}}},
		{"mutual cycle", "dependency cycle", []c.OutputDefinition{{Name: "Ready", Requires: []string{"Observed"}}, {Name: "Observed", Requires: []string{"Ready"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) { assertPanic(t, tc.want, func() { c.Define(c.Definition{Outputs: tc.outputs}) }) })
	}
}

type declaredDerivation struct {
	specs  map[string]c.Spec
	values []metav1.Condition
}

func (d *declaredDerivation) Outputs() map[string]c.Spec { return d.specs }
func (d *declaredDerivation) Derive(c.Object, time.Time) []metav1.Condition {
	return append([]metav1.Condition(nil), d.values...)
}

func TestDerivedContractPublicationAndSnapshot(t *testing.T) {
	reasons := map[string]c.Escalation{"InitializationFailed": c.Failed}
	derivation := &declaredDerivation{
		specs:  map[string]c.Spec{"Initialized": {Polarity: c.TrueIsGood, Reasons: c.ReasonClassifier{Reasons: reasons}}},
		values: []metav1.Condition{{Type: "Initialized", Status: metav1.ConditionFalse, Reason: "InitializationFailed"}},
	}
	producer := c.Define(c.Definition{Outputs: []c.OutputDefinition{{Name: "Ready", Derived: []c.Derivation{derivation}}}})
	// The derivation's map is the only declaration. Define freezes it and nested reason maps.
	delete(derivation.specs, "Initialized")
	derivation.specs["Injected"] = c.Spec{}
	reasons["InitializationFailed"] = c.None
	published := producer.Schema()
	spec, found := published.Conditions["Initialized"]
	if !found || spec.Polarity != c.TrueIsGood || spec.Reasons.Classify(derivation.values[0]) != c.Failed {
		t.Fatalf("lost derived contract: %#v", published)
	}
	if _, found := published.Conditions["Injected"]; found {
		t.Fatal("caller mutated output ownership")
	}
	parent := &object{}
	c.Aggregate(parent, nil, producer)
	consumer := c.Define(c.Definition{
		Outputs:  []c.OutputDefinition{c.Output("Healthy", c.Spec{})},
		Children: []c.ChildBinding{c.Child(published, c.Classify(c.Map("Initialized", "Healthy")))},
	})
	root := &object{}
	c.AggregateSlice(root, []c.Object{parent}, consumer)
	if value, _ := c.Lookup(root, "Healthy"); value.Status != metav1.ConditionFalse || value.Reason != "ConditionsFailed" {
		t.Fatal(value)
	}
	before := append([]metav1.Condition(nil), parent.conditions...)
	sets := parent.sets
	derivation.values[0].Type = "Injected"
	assertPanic(t, "undeclared", func() { c.Aggregate(parent, nil, producer) })
	if parent.sets != sets || !reflect.DeepEqual(before, parent.conditions) {
		t.Fatal("undeclared derived output caused partial mutation")
	}
}

func TestDerivedContractValidation(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		specs      map[string]c.Spec
	}{
		{"duplicate owner", "duplicate output", map[string]c.Spec{"Ready": {}}},
		{"empty name", "invalid condition", map[string]c.Spec{"": {}}},
		{"invalid polarity", "invalid condition", map[string]c.Spec{"Initialized": {Polarity: 99}}},
		{"suffix output", "cannot be a suffix", map[string]c.Spec{"Initialized": {Suffix: true}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertPanic(t, tc.want, func() {
				c.Define(c.Definition{Outputs: []c.OutputDefinition{{Name: "Ready", Derived: []c.Derivation{&declaredDerivation{specs: tc.specs}}}}})
			})
		})
	}
}
