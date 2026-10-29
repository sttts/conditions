// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package conditions_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	c "github.com/sttts/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type normalizingObject struct{ *object }

func (o normalizingObject) SetConditions(values []metav1.Condition) bool {
	values = slices.Clone(values)
	for i := range values {
		values[i].Message = strings.TrimSpace(values[i].Message)
	}
	return o.object.SetConditions(values)
}

func TestAggregateUsesNativeSetterResult(t *testing.T) {
	initial := metav1.Condition{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Available", Message: "ready"}
	parent := normalizingObject{&object{conditions: []metav1.Condition{initial}}}
	r := rule(c.Outcome{State: c.State{Assessment: c.Good}, Reason: "Available", Summary: " ready "})
	rules := c.Rules{Outputs: []c.Aggregation{r}}
	changed, err := c.Aggregate(parent, nil, rules)
	if err != nil || changed || parent.sets != 1 || parent.conditions[0] != initial {
		t.Fatalf("did not honor the setter's normalized no-op: changed=%v err=%v parent=%#v", changed, err, parent.object)
	}
	rules.Outputs[0].Reduction = fixedOutcome{c.Outcome{State: c.State{Assessment: c.Good}, Reason: "Available", Summary: " updated "}}
	changed, err = c.Aggregate(parent, nil, rules)
	if err != nil || !changed || parent.conditions[0].Message != "updated" {
		t.Fatalf("did not report a native change: changed=%v err=%v parent=%#v", changed, err, parent.object)
	}
}

// plainObject has Cluster API's accessor shape without a framework dependency.
type plainObject struct {
	metav1.ObjectMeta
	conditions []metav1.Condition
	apply      func([]metav1.Condition)
	sets       int
}

func (o *plainObject) GetConditions() []metav1.Condition { return o.conditions }
func (o *plainObject) SetConditions(values []metav1.Condition) {
	o.sets++
	if o.apply != nil {
		o.apply(values)
		return
	}
	o.conditions = values
}

func TestWithChangeDetectionSnapshotsBeforeInPlaceMutation(t *testing.T) {
	parent := &plainObject{conditions: []metav1.Condition{{Type: "Ready", Status: metav1.ConditionFalse, Reason: "Waiting"}}}
	parent.apply = func(values []metav1.Condition) { copy(parent.conditions, values) }
	tracked := c.WithChangeDetection(parent)
	desired := []metav1.Condition{{Type: "Ready", Status: metav1.ConditionFalse, Reason: "StillWaiting", ObservedGeneration: 4}}
	if !tracked.SetConditions(desired) {
		t.Fatal("missed an in-place change with unchanged status")
	}
	if parent.conditions[0] != desired[0] || tracked.SetConditions(desired) {
		t.Fatal("in-place assignment was not idempotent")
	}
}

func TestWithChangeDetectionComparesStoredConditions(t *testing.T) {
	parent := &plainObject{conditions: []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue, Message: "ready"}}}
	parent.apply = func(values []metav1.Condition) {
		parent.conditions = slices.Clone(values)
		for i := range parent.conditions {
			parent.conditions[i].Message = strings.TrimSpace(parent.conditions[i].Message)
		}
	}
	tracked := c.WithChangeDetection(parent)
	desired := []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue, Message: " ready "}}
	if tracked.SetConditions(desired) {
		t.Fatal("reported an input difference discarded by the setter")
	}
	if desired[0].Message != " ready " {
		t.Fatal("adapter mutated the supplied Conditions")
	}
}

func TestWithChangeDetectionUsesAPIEquality(t *testing.T) {
	t.Run("nil and empty Conditions", func(t *testing.T) {
		parent := &plainObject{}
		if c.WithChangeDetection(parent).SetConditions([]metav1.Condition{}) {
			t.Fatal("reported a change for equivalent empty Conditions")
		}
	})
	t.Run("equivalent transition times", func(t *testing.T) {
		epoch := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
		initial := metav1.Condition{Type: "Ready", Status: metav1.ConditionTrue, LastTransitionTime: metav1.NewTime(epoch)}
		parent := &plainObject{conditions: []metav1.Condition{initial}}
		desired := initial
		desired.LastTransitionTime = metav1.NewTime(epoch.In(time.FixedZone("offset", 3600)))
		if c.WithChangeDetection(parent).SetConditions([]metav1.Condition{desired}) {
			t.Fatal("reported a change for the same instant in a different time zone")
		}
	})
}

func TestWithChangeDetectionOnlyTracksConditions(t *testing.T) {
	parent := &plainObject{conditions: []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue}}}
	parent.apply = func([]metav1.Condition) { parent.Labels = map[string]string{"other": "updated"} }
	if c.WithChangeDetection(parent).SetConditions(parent.conditions) {
		t.Fatal("reported a non-Condition change")
	}
}

func TestAggregateWithPlainSetter(t *testing.T) {
	parent := &plainObject{ObjectMeta: metav1.ObjectMeta{Generation: 9},
		conditions: []metav1.Condition{{Type: "Custom", Status: metav1.ConditionTrue, Reason: "Keep"}}}
	tracked := c.WithChangeDetection(parent)
	r := rule(c.Outcome{State: c.State{Assessment: c.Good}, Reason: "Available"})
	rules := c.Rules{Outputs: []c.Aggregation{r}}
	changed, err := c.Aggregate(tracked, nil, rules)
	if err != nil || !changed {
		t.Fatalf("aggregate through adapter: changed=%v err=%v", changed, err)
	}
	if value, _ := c.Lookup(tracked, "Ready"); value.ObservedGeneration != 9 {
		t.Fatalf("adapter lost parent metadata: %#v", value)
	}
	if value, _ := c.Lookup(tracked, "Custom"); value.Reason != "Keep" {
		t.Fatal("lost an unowned Condition")
	}
	if changed, err = c.Aggregate(tracked, nil, rules); err != nil || changed {
		t.Fatalf("adapted setter is not idempotent: changed=%v err=%v", changed, err)
	}
	rules.Outputs = append(rules.Outputs, r)
	if changed, err = c.Aggregate(tracked, nil, rules); err == nil || changed || parent.sets != 2 {
		t.Fatalf("invalid rules reached the wrapped setter: changed=%v err=%v calls=%d", changed, err, parent.sets)
	}
}

func TestWithChangeDetectionNilObjects(t *testing.T) {
	for _, input := range []c.PlainSetter{nil, (*plainObject)(nil)} {
		changed, err := c.Aggregate(c.WithChangeDetection(input), nil, c.Rules{})
		if err == nil || changed {
			t.Fatalf("nil parent through adapter: changed=%v err=%v", changed, err)
		}
	}
}
