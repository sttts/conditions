// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package conditions_test

import (
	"reflect"
	"testing"
	"time"

	c "github.com/sttts/conditions"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type object struct {
	metav1.ObjectMeta
	conditions []metav1.Condition
	sets       int
}

func (o *object) GetConditions() []metav1.Condition { return o.conditions }
func (o *object) SetConditions(values []metav1.Condition) bool {
	o.sets++
	if equality.Semantic.DeepEqual(o.conditions, values) {
		return false
	}
	o.conditions = values
	return true
}

type fixedOutcome struct{ outcome c.Outcome }

func (r fixedOutcome) Reduce(metav1.Condition, map[string]c.Object, []c.Contribution, []c.Contribution) c.Outcome {
	return r.outcome
}

func rule(outcome c.Outcome) c.Aggregation {
	return c.Aggregation{Output: c.ConditionType{Type: "Ready", Polarity: c.TrueIsGood},
		Parent: c.ChildrenOnly{}, Reduction: fixedOutcome{outcome}, Messages: c.BoundedMessages{},
		DefaultBase: &metav1.Condition{Type: "Ready"}}
}

func TestOutputMetadataPolicies(t *testing.T) {
	epoch := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name   string
		policy c.GenerationPolicy
		copied *metav1.Condition
		want   int64
	}{
		{"parent generation", c.ParentGeneration, nil, 7},
		{"preserve copied generation", c.PreserveBaseGeneration, &metav1.Condition{ObservedGeneration: 3}, 3},
		{"legacy derived generation", c.PreserveBaseGeneration, nil, 0},
		{"schema without generation", c.NoObservedGeneration, &metav1.Condition{ObservedGeneration: 3}, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			parent := &object{ObjectMeta: metav1.ObjectMeta{Generation: 7}}
			out := c.Outcome{State: c.State{Assessment: c.Good}, Reason: "Available", Summary: "ready", Copied: test.copied, LastTransitionTime: metav1.NewTime(epoch.Add(-time.Hour))}
			r := rule(out)
			r.Generation = test.policy
			rules := c.Rules{Outputs: []c.Aggregation{r}, Now: func() time.Time { return epoch }}
			changed, err := c.Aggregate(parent, nil, rules)
			if err != nil || !changed {
				t.Fatalf("aggregate: %v, %v", changed, err)
			}
			value, _ := c.Lookup(parent, "Ready")
			if value.ObservedGeneration != test.want || value.LastTransitionTime != out.LastTransitionTime {
				t.Fatalf("incorrect metadata: %#v", value)
			}
			rules.Now = func() time.Time { return epoch.Add(time.Hour) }
			if changed, err = c.Aggregate(parent, nil, rules); err != nil || changed || parent.sets != 2 {
				t.Fatalf("unstable metadata: %v, %v, sets=%d", changed, err, parent.sets)
			}
		})
	}
}

func TestPolarityAndRenderingDoNotChangeClassification(t *testing.T) {
	for _, polarity := range []c.Polarity{c.TrueIsGood, c.FalseIsGood} {
		for _, format := range []c.Formatter{
			c.BoundedMessages{MaxLines: 1}, c.BoundedMessages{MaxLines: 7},
			c.MultilineMessages{MaxLines: 1}, c.MultilineMessages{MaxLines: 7},
		} {
			out := c.Outcome{State: c.State{Assessment: c.Bad, Escalation: c.Failed}, Reason: "ChildrenFailed", Summary: "dependency failed", Causes: []c.Cause{{Reference: "child", Message: "many\nlines\nof\ntext"}}}
			r := rule(out)
			r.Output.Polarity = polarity
			r.Messages = format
			parent := &object{}
			_, err := c.Aggregate(parent, nil, c.Rules{Outputs: []c.Aggregation{r}})
			if err != nil {
				t.Fatal(err)
			}
			value, _ := c.Lookup(parent, "Ready")
			want := metav1.ConditionFalse
			if polarity == c.FalseIsGood {
				want = metav1.ConditionTrue
			}
			if value.Status != want || value.Reason != "ChildrenFailed" {
				t.Fatalf("rendering or polarity changed failure: %#v", value)
			}
		}
	}
}

func TestInvalidRulesDoNotAssignPartialResults(t *testing.T) {
	parent := &object{conditions: []metav1.Condition{{Type: "Custom", Status: metav1.ConditionTrue, Reason: "Keep"}}}
	before := append([]metav1.Condition(nil), parent.conditions...)
	r := rule(c.Outcome{State: c.State{Assessment: c.Good}, Reason: "Available"})
	_, err := c.Aggregate(parent, nil, c.Rules{Outputs: []c.Aggregation{r, r}})
	if err == nil || parent.sets != 0 || !reflect.DeepEqual(before, parent.conditions) {
		t.Fatalf("invalid rules mutated parent: err=%v, object=%#v", err, parent)
	}
}
