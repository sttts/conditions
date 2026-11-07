// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package conditions_test

import (
	c "github.com/sttts/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
	"time"
)

func TestAllAndLocalAbsence(t *testing.T) {
	for _, polarity := range []c.Polarity{c.TrueIsGood, c.FalseIsGood} {
		good, bad := metav1.ConditionTrue, metav1.ConditionFalse
		if polarity == c.FalseIsGood {
			good, bad = bad, good
		}
		for _, tc := range []struct {
			name                   string
			localOnly              bool
			local, child, existing metav1.ConditionStatus
			want                   metav1.ConditionStatus
			reason                 string
		}{
			{name: "all without observations", want: metav1.ConditionUnknown, reason: "ConditionsUnknown"},
			{name: "all optional local", child: metav1.ConditionTrue, want: good, reason: "ConditionsSatisfied"},
			{name: "all bad child", child: metav1.ConditionFalse, want: bad, reason: "ConditionsNotSatisfied"},
			{name: "all local good needs contributions", local: good, want: metav1.ConditionUnknown, reason: "ConditionsUnknown"},
			{name: "all local bad wins", local: bad, child: metav1.ConditionTrue, want: bad, reason: "LocalAssertion"},
			{name: "all local unknown wins", local: metav1.ConditionUnknown, child: metav1.ConditionTrue, want: metav1.ConditionUnknown, reason: "LocalAssertion"},
			{name: "all local good permits bad child", local: good, child: metav1.ConditionFalse, want: bad, reason: "ConditionsNotSatisfied"},
			{name: "all no children replaces persisted good", existing: good, want: metav1.ConditionUnknown, reason: "ConditionsUnknown"},
			{name: "local without assertion creates nothing", localOnly: true, child: metav1.ConditionTrue},
			{name: "local without assertion retains old", localOnly: true, existing: bad, child: metav1.ConditionTrue, want: bad, reason: "Persisted"},
			{name: "local good ignores bad child", localOnly: true, local: good, child: metav1.ConditionFalse, want: good, reason: "LocalAssertion"},
			{name: "local unknown retained", localOnly: true, local: metav1.ConditionUnknown, child: metav1.ConditionTrue, want: metav1.ConditionUnknown, reason: "LocalAssertion"},
		} {
			t.Run(tc.name+"/"+string(good), func(t *testing.T) {
				spec := c.Spec{Polarity: polarity}
				output := c.All("Ready", spec)
				if tc.localOnly {
					output = c.Local("Ready", spec)
				}
				a := c.Define(c.Definition{
					Outputs:  []c.OutputDefinition{output},
					Children: []c.ChildBinding{c.Child(contract("Ready", c.TrueIsGood), c.Identity)},
					Now:      func() time.Time { return time.Unix(100, 0) },
				})
				parent := &object{ObjectMeta: metav1.ObjectMeta{Generation: 7}}
				if tc.existing != "" {
					parent.conditions = []metav1.Condition{{Type: "Ready", Status: tc.existing, Reason: "Persisted", ObservedGeneration: 2, LastTransitionTime: metav1.NewTime(time.Unix(1, 0))}}
				}
				var children []c.Object
				if tc.child != "" {
					children = []c.Object{observation("Ready", tc.child, "ChildObservation")}
				}
				var local []metav1.Condition
				if tc.local != "" {
					local = []metav1.Condition{{Type: "Ready", Status: tc.local, Reason: "LocalAssertion", ObservedGeneration: 3}}
				}
				c.AggregateSlice(parent, children, a, local...)
				value, found := c.Lookup(parent, "Ready")
				if tc.want == "" {
					if found {
						t.Fatal("invented local assertion", value)
					}
					return
				}
				if !found || value.Status != tc.want || value.Reason != tc.reason {
					t.Fatalf("got %#v, want %s/%s", value, tc.want, tc.reason)
				}
				if tc.localOnly {
					generation := int64(3)
					if tc.local == "" {
						generation = 2
					}
					if value.ObservedGeneration != generation {
						t.Fatalf("lost assertion generation: %#v", value)
					}
				}
				if c.AggregateSlice(parent, children, a, local...) {
					t.Fatal("repeat changed output")
				}
			})
		}
	}
}
