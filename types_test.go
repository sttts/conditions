// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package conditions_test

import (
	"fmt"
	c "github.com/sttts/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"strings"
	"testing"
)

type apiObject struct {
	metav1.TypeMeta
	object
}

func (o *apiObject) DeepCopyObject() runtime.Object {
	if o == nil {
		return nil
	}
	out := *o
	out.ObjectMeta = *o.ObjectMeta.DeepCopy()
	out.conditions = append([]metav1.Condition(nil), o.conditions...)
	return &out
}
func registeredTypes(kinds ...schema.GroupVersionKind) *runtime.Scheme {
	s := runtime.NewScheme()
	for _, k := range kinds {
		s.AddKnownTypeWithName(k, &apiObject{})
	}
	return s
}

func TestObjectScopeHelpers(t *testing.T) {
	kind := schema.GroupVersionKind{Group: "db.io", Version: "v1", Kind: "Database"}
	s := registeredTypes(kind)
	types := c.Types(s)
	obj := &apiObject{}
	for _, tc := range []struct {
		name string
		get  func(runtime.Object) c.ResourceMatch
		want c.ResourceMatch
	}{
		{"G", types.G, c.G("db.io")}, {"GV", types.GV, c.GV("db.io", "v1")},
		{"GK", types.GK, c.GK("db.io", "Database")}, {"GVK", types.GVK, c.GVK("db.io", "v1", "Database")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.get(obj); got != tc.want {
				t.Fatalf("%v != %v", got, tc.want)
			}
		})
	}
	if !obj.GroupVersionKind().Empty() {
		t.Fatal("helper mutated TypeMeta")
	}
	// Explicit TypeMeta works without a scheme, including the core group.
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(schema.GroupVersionKind{Version: "v1", Kind: "Pod"})
	if got := c.Types(nil).G(u); got != c.G("") {
		t.Fatal(got)
	}
	obj.SetGroupVersionKind(kind.GroupKind().WithVersion("v2"))
	if got := types.GVK(obj); got != c.GVK("db.io", "v2", "Database") {
		t.Fatal("explicit GVK must win", got)
	}
	assertPanic(t, "nil type object", func() { types.GK((*apiObject)(nil)) })
	assertPanic(t, "missing GVK", func() { c.Types(nil).GK(&apiObject{}) })
	assertPanic(t, "no kind is registered", func() { c.Types(runtime.NewScheme()).GK(&apiObject{}) })
	obj.SetGroupVersionKind(schema.GroupVersionKind{Kind: "Database"})
	assertPanic(t, "incomplete GVK", func() { types.GK(obj) })
}

func TestScopeAmbiguityAfterProjection(t *testing.T) {
	v1 := schema.GroupVersionKind{Group: "db.io", Version: "v1", Kind: "Database"}
	types := c.Types(registeredTypes(v1, v1.GroupKind().WithVersion("v2")))
	if types.GK(&apiObject{}) != c.GK("db.io", "Database") || types.G(&apiObject{}) != c.G("db.io") {
		t.Fatal("unambiguous projection rejected")
	}
	assertPanic(t, "ambiguous scope", func() { types.GVK(&apiObject{}) })
	assertPanic(t, "ambiguous scope", func() { types.GV(&apiObject{}) })
	otherKind := v1
	otherKind.Kind = "Replica"
	types = c.Types(registeredTypes(v1, otherKind))
	if types.GV(&apiObject{}) != c.GV("db.io", "v1") {
		t.Fatal("GV should be unambiguous")
	}
	assertPanic(t, "ambiguous scope", func() { types.GK(&apiObject{}) })
	otherGroup := v1
	otherGroup.Group = "other.io"
	types = c.Types(registeredTypes(v1, otherGroup))
	assertPanic(t, "ambiguous scope", func() { types.G(&apiObject{}) })
}

func TestTypedChildrenAndResolutionAtomicity(t *testing.T) {
	v1 := schema.GroupVersionKind{Group: "db.io", Version: "v1", Kind: "Database"}
	s := registeredTypes(v1, v1.GroupKind().WithVersion("v2"))
	input := contract("Available", c.TrueIsGood)
	d := c.Definition{
		Scheme:   s,
		Outputs:  []c.OutputDefinition{c.All("Ready", c.Spec{})},
		Children: []c.ChildBinding{c.Child(input, c.Classify(c.Map("Available", "Ready"))).For(c.Types(s).GK(&apiObject{}))},
	}
	child := &apiObject{object: *observation("Available", metav1.ConditionTrue, "Serving")}
	parent := &object{}
	a := c.Define(d)
	c.AggregateSlice(parent, []c.Object{child}, a)
	if value, _ := c.Lookup(parent, "Ready"); value.Status != metav1.ConditionTrue {
		t.Fatal(value)
	}
	if !child.GroupVersionKind().Empty() {
		t.Fatal("evaluation mutated TypeMeta")
	}
	// Narrowing to one registered version leaves the other unmatched: ambiguous.
	d.Children[0] = d.Children[0].For(c.GVK("db.io", "v1", "Database"))
	ambiguous := c.Define(d)
	before := parent.sets
	_, err := c.EvaluateAggregation(parent, map[string]c.Object{"db": child}, ambiguous)
	if err == nil || !strings.Contains(err.Error(), "ambiguous child identity") {
		t.Fatal(err)
	}
	assertPanic(t, "ambiguous child identity", func() { c.AggregateSlice(parent, []c.Object{child}, ambiguous) })
	if parent.sets != before {
		t.Fatal("mutation on identity failure")
	}
	// Explicit identity disambiguates, including adapters that are not runtime.Objects.
	c.AggregateSlice(parent, []c.Object{c.WithGVK(child, v1)}, ambiguous)
	d.Scheme = nil
	missing := c.Define(d)
	_, err = c.EvaluateAggregation(parent, map[string]c.Object{"db": child}, missing)
	if err == nil || !strings.Contains(err.Error(), "missing GVK") {
		t.Fatal(err)
	}
	_, err = c.EvaluateAggregation(parent, map[string]c.Object{"adapter": &object{}}, missing)
	if err == nil || !strings.Contains(err.Error(), "missing API identity") {
		t.Fatal(err)
	}
	d.Scheme = runtime.NewScheme()
	_, err = c.EvaluateAggregation(parent, map[string]c.Object{"db": child}, c.Define(d))
	if err == nil {
		t.Fatal("unregistered type silently filtered")
	}
	// A resolved foreign GVK is an ordinary non-match, not a resolution error.
	child.SetGroupVersionKind(schema.GroupVersionKind{Group: "other.io", Version: "v1", Kind: "Database"})
	if _, err := c.EvaluateAggregation(parent, map[string]c.Object{"db": child}, missing); err != nil {
		t.Fatal(err)
	}
	// Global definitions continue to support condition adapters without runtime.Object.
	global := c.Define(c.Definition{Outputs: d.Outputs, Children: []c.ChildBinding{c.Child(input, c.Classify(c.Map("Available", "Ready")))}})
	if _, err := c.EvaluateAggregation(parent, map[string]c.Object{"db": &object{}}, global); err != nil {
		t.Fatal(err)
	}
}

func ExampleTypes() {
	scheme := runtime.NewScheme()
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{Group: "db.io", Version: "v1", Kind: "Database"}, &apiObject{})
	types := c.Types(scheme)
	aggregation := c.Define(c.Definition{
		Scheme:  scheme,
		Outputs: []c.OutputDefinition{c.All("Ready", c.Spec{Polarity: c.TrueIsGood})},
		Children: []c.ChildBinding{
			c.Child(contract("Available", c.TrueIsGood), c.Classify(c.Map("Available", "Ready"))).For(types.GK(&apiObject{})),
		},
	})
	child := &apiObject{object: *observation("Available", metav1.ConditionTrue, "Serving")}
	parent := &object{}
	c.AggregateSlice(parent, []c.Object{child}, aggregation)
	ready, _ := c.Lookup(parent, "Ready")
	fmt.Println(ready.Status, child.GroupVersionKind().Empty())
	// Output: True true
}
