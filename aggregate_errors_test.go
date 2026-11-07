// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package conditions_test

import (
	"reflect"
	"testing"

	c "github.com/sttts/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestAggregateErrorsAreAtomic(t *testing.T) {
	a := c.Define(c.Definition{Outputs: []c.OutputDefinition{c.All("Ready", c.Spec{})}, Children: []c.ChildBinding{c.Child(contract("Ready", c.TrueIsGood), c.Identity).For(c.G("db.io"))}})
	local := metav1.Condition{Type: "Ready", Status: metav1.ConditionTrue}
	for _, tc := range []struct {
		name        string
		aggregation *c.Aggregation
		children    map[string]c.Object
		local       []metav1.Condition
	}{
		{name: "nil aggregation"},
		{name: "missing identity", aggregation: a, children: map[string]c.Object{"db": &object{}}},
		{name: "missing GVK", aggregation: a, children: map[string]c.Object{"db": &apiObject{}}},
		{name: "incomplete GVK", aggregation: a, children: map[string]c.Object{"db": c.WithGVK(&object{}, schema.GroupVersionKind{Kind: "Database"})}},
		{name: "duplicate locals", aggregation: a, local: []metav1.Condition{local, local}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parent := observation("Ready", metav1.ConditionTrue, "Persisted")
			before := append([]metav1.Condition(nil), parent.GetConditions()...)
			changed, err := c.Aggregate(parent, tc.children, tc.aggregation, tc.local...)
			if err == nil || changed || parent.sets != 0 || !reflect.DeepEqual(before, parent.GetConditions()) {
				t.Fatalf("changed=%v err=%v sets=%d", changed, err, parent.sets)
			}
		})
	}
	if changed, err := c.Aggregate((*object)(nil), nil, a); err == nil || changed {
		t.Fatalf("nil parent: %v %v", changed, err)
	}
	if changed, err := c.AggregateSlice(&object{}, []c.Object{&object{}}, a); err == nil || changed {
		t.Fatalf("slice: %v %v", changed, err)
	}
}
