// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package conditions_test

import (
	"reflect"
	"testing"
	"time"

	c "github.com/sttts/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestEvaluateMatchesAggregation(t *testing.T) {
	output := c.All("Ready", c.Spec{Polarity: c.TrueIsGood}).Format(c.FormatterFunc(func(string, []c.Cause) string { return "formatted" }))
	a := c.Define(c.Definition{Outputs: []c.OutputDefinition{output}, Children: []c.ChildBinding{c.Child(contract("Available", c.FalseIsGood), c.Classify(c.Map("Available", "Ready")))}, Now: func() time.Time { return time.Unix(100, 0) }})
	parent := observation("Ready", metav1.ConditionTrue, "Persisted")
	before := append([]metav1.Condition(nil), parent.GetConditions()...)
	child := observation("Available", metav1.ConditionTrue, "Broken")
	children := map[string]c.Object{"database": child}
	got, err := c.Evaluate("Ready", parent, children, a)
	if err != nil || got == nil {
		t.Fatalf("Evaluate: %v, %v", got, err)
	}
	all, err := c.EvaluateAggregation(parent, children, a)
	if err != nil || len(all) != 1 || !reflect.DeepEqual(*got, all[0]) {
		t.Fatalf("single %v, all %v, err %v", got, all, err)
	}
	if got.Status != metav1.ConditionFalse || got.Message != "formatted" {
		t.Fatalf("classification/formatting lost: %v", got)
	}
	if !reflect.DeepEqual(before, parent.GetConditions()) {
		t.Fatal("parent mutated")
	}
}

func TestEvaluateDependencyClosure(t *testing.T) {
	spec := c.Spec{Polarity: c.TrueIsGood}
	derived := &declaredDerivation{specs: map[string]c.Spec{"Initialized": spec}, values: []metav1.Condition{{Type: "Initialized", Status: metav1.ConditionTrue, Reason: "Fresh"}}}
	source := c.All("Source", spec)
	source.Derived = []c.Derivation{derived}
	ready := c.All("Ready", spec)
	ready.Requires = []string{"Initialized"}
	called := false
	ready.LocalChecks = c.LocalSelectorFunc(func(parent c.Object) []c.Contribution {
		called = true
		value, ok := c.Lookup(parent, "Initialized")
		if !ok || value.Reason != "Fresh" {
			t.Fatalf("missing current dependency: %v", value)
		}
		value, _ = c.Lookup(parent, "Unrelated")
		if value.Reason != "Persisted" {
			t.Fatalf("unrelated local assertion leaked: %v", value)
		}
		return nil
	})
	unrelated := c.All("Unrelated", spec).Format(c.FormatterFunc(func(string, []c.Cause) string { t.Fatal("unrelated output executed"); return "" }))
	a := c.Define(c.Definition{Outputs: []c.OutputDefinition{ready, unrelated, source}})
	parent := observation("Unrelated", metav1.ConditionTrue, "Persisted")
	value, err := c.Evaluate("Ready", parent, nil, a, metav1.Condition{Type: "Unrelated", Status: metav1.ConditionFalse, Reason: "Local"})
	if err != nil || value == nil || !called {
		t.Fatalf("%v, %v, called=%v", value, err, called)
	}
	called = false
	value, err = c.Evaluate("Initialized", parent, nil, a)
	if err != nil || value == nil || value.Reason != "Fresh" || called {
		t.Fatalf("derived result: %v, %v", value, err)
	}
}

func TestEvaluateAbsenceAndErrors(t *testing.T) {
	a := c.Define(c.Definition{Outputs: []c.OutputDefinition{c.Local("Ready", c.Spec{Polarity: c.TrueIsGood})}})
	parent := observation("Ready", metav1.ConditionTrue, "Persisted")
	if value, err := c.Evaluate("Ready", parent, nil, a); value != nil || err != nil {
		t.Fatalf("returned stale value: %v, %v", value, err)
	}
	for _, name := range []string{"", "Missing"} {
		if _, err := c.Evaluate(name, parent, nil, a); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
	if _, err := c.Evaluate("Ready", nil, nil, a); err == nil {
		t.Fatal("accepted nil parent")
	}
	if _, err := c.Evaluate("Ready", parent, nil, nil); err == nil {
		t.Fatal("accepted nil aggregation")
	}
	local := metav1.Condition{Type: "Ready", Status: metav1.ConditionTrue}
	if _, err := c.Evaluate("Ready", parent, nil, a, local, local); err == nil {
		t.Fatal("accepted duplicate local")
	}
}
