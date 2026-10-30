// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package conditions_test

import (
	"testing"
	"time"

	c "github.com/sttts/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type abstain struct{}

func (abstain) Before(c.Object, map[string]c.Object, metav1.Condition) (*c.Outcome, bool) {
	return nil, true
}
func (abstain) IncludeChildren(metav1.Condition) bool { return false }

func observing(name string, requires ...string) c.Aggregation {
	return c.Aggregation{Output: c.ConditionType{Type: name}, DefaultBase: &metav1.Condition{Type: name}, Requires: requires, Parent: c.ChildrenOnly{}, Local: selectReady{}, Reduction: readReady{}, Messages: c.BoundedMessages{}}
}

func TestDependenciesDoNotImplyFreshnessOrSuccess(t *testing.T) {
	for _, mode := range []string{"missing base", "abstention"} {
		for _, previous := range []bool{false, true} {
			t.Run(mode+"/"+map[bool]string{false: "missing", true: "persisted"}[previous], func(t *testing.T) {
				producer := rule(c.Outcome{State: c.State{Assessment: c.Good}, Reason: "Ready"})
				if mode == "missing base" {
					producer.DefaultBase = nil
				} else {
					producer.Parent = abstain{}
				}
				parent := &object{}
				want := metav1.ConditionUnknown
				if previous {
					parent.conditions = []metav1.Condition{{Type: "Ready", Status: metav1.ConditionFalse, Reason: "Previous"}}
					want = metav1.ConditionFalse
				}
				rules, err := c.Compose([]c.Aggregation{observing("Observed", "Ready"), producer})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := c.Aggregate(parent, nil, rules); err != nil {
					t.Fatal(err)
				}
				value, _ := c.Lookup(parent, "Observed")
				if value.Status != want {
					t.Fatalf("dependency invented a current assertion: %#v", value)
				}
			})
		}
	}
}

func TestTransitiveAndUnprocessedBaseResultsDoNotLeak(t *testing.T) {
	producer := rule(c.Outcome{State: c.State{Assessment: c.Good}, Reason: "Ready"})
	middle := observing("Stage", "Ready")
	final := observing("Observed", "Stage") // does NOT declare Ready
	parent := &object{conditions: []metav1.Condition{{Type: "Ready", Status: metav1.ConditionFalse, Reason: "Previous"}}}
	rules, err := c.Compose([]c.Aggregation{final, middle, producer})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Aggregate(parent, nil, rules, metav1.Condition{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Local"}); err != nil {
		t.Fatal(err)
	}
	if value, _ := c.Lookup(parent, "Stage"); value.Status != metav1.ConditionTrue {
		t.Fatalf("direct dependency did not see result: %#v", value)
	}
	if value, _ := c.Lookup(parent, "Observed"); value.Status != metav1.ConditionFalse {
		t.Fatalf("transitive result or unprocessed base leaked: %#v", value)
	}
}

type repeatedResult struct{}

func (repeatedResult) Outputs() []string { return []string{"Initialized"} }
func (repeatedResult) Derive(c.Object, time.Time) []metav1.Condition {
	return []metav1.Condition{{Type: "Initialized", Status: metav1.ConditionTrue, Reason: "First"}, {Type: "Initialized", Status: metav1.ConditionFalse, Reason: "Second"}}
}
func TestDerivedConflictsAreAtomic(t *testing.T) {
	for _, derivatives := range [][]c.Derivation{
		{initialization{}, initialization{}}, // colliding declarations
		{derivedFrom{output: "", input: "Ready"}},
		{nil},
		{repeatedResult{}}, // declaration is unique, runtime result isn't
	} {
		producer := rule(c.Outcome{State: c.State{Assessment: c.Good}, Reason: "Ready"})
		producer.Derived = derivatives
		parent := &object{}
		if _, err := c.Aggregate(parent, nil, c.Rules{Outputs: []c.Aggregation{producer}}); err == nil || parent.sets != 0 {
			t.Fatalf("derived conflict applied results: err=%v sets=%d", err, parent.sets)
		}
	}
}
