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

// readReady deliberately reads another output, without declaring a current-call dependency.
type readReady struct{}

func (readReady) Reduce(_ metav1.Condition, _ map[string]c.Object, local, _ []c.Contribution) c.Outcome {
	return c.Outcome{State: local[0].State, Reason: "Observed"}
}

type selectReady struct{}

func (selectReady) Select(parent c.Object) []c.Contribution {
	value, _ := c.Lookup(parent, "Ready")
	return []c.Contribution{{State: c.State{Assessment: (c.ConditionType{Type: "Ready"}).Assessment(value)}}}
}

type initialization struct{}

func (initialization) Outputs() []string { return []string{"Initialized"} }
func (initialization) Derive(parent c.Object, now time.Time) []metav1.Condition {
	value, _ := c.Lookup(parent, "Ready")
	return []metav1.Condition{{Type: "Initialized", Status: value.Status, Reason: "Initialized", LastTransitionTime: metav1.NewTime(now)}}
}

type dishonestDerivation struct{}

func (dishonestDerivation) Outputs() []string { return []string{"Initialized"} }
func (dishonestDerivation) Derive(c.Object, time.Time) []metav1.Condition {
	return []metav1.Condition{{Type: "Ready", Status: metav1.ConditionFalse, Reason: "Undeclared"}}
}
func TestUndeclaredDerivationWriteIsAtomic(t *testing.T) {
	producer := rule(c.Outcome{State: c.State{Assessment: c.Good}, Reason: "Ready"})
	producer.Derived = []c.Derivation{dishonestDerivation{}}
	parent := &object{}
	if _, err := c.Aggregate(parent, nil, c.Rules{Outputs: []c.Aggregation{producer}}); err == nil || parent.sets != 0 {
		t.Fatalf("undeclared derivation write was not rejected atomically: err=%v sets=%d", err, parent.sets)
	}
}

type derivedFrom struct{ output, input string }

func (d derivedFrom) Outputs() []string { return []string{d.output} }
func (d derivedFrom) Derive(parent c.Object, now time.Time) []metav1.Condition {
	value, _ := c.Lookup(parent, d.input)
	value.Type = d.output
	value.LastTransitionTime = metav1.NewTime(now)
	return []metav1.Condition{value}
}
func TestSiblingDerivationsAreIndependent(t *testing.T) {
	for _, derived := range [][]c.Derivation{{derivedFrom{"A", "Ready"}, derivedFrom{"B", "A"}}, {derivedFrom{"B", "A"}, derivedFrom{"A", "Ready"}}} {
		producer := rule(c.Outcome{State: c.State{Assessment: c.Good}, Reason: "Ready"})
		producer.Derived = derived
		parent := &object{}
		if _, err := c.Aggregate(parent, nil, c.Rules{Outputs: []c.Aggregation{producer}}); err != nil {
			t.Fatal(err)
		}
		value, _ := c.Lookup(parent, "B")
		if value.Status != metav1.ConditionUnknown {
			t.Fatalf("sibling derivation leaked into another: %#v", value)
		}
	}
}

func TestDerivedOwnershipAndDependencies(t *testing.T) {
	producer := rule(c.Outcome{State: c.State{Assessment: c.Good}, Reason: "Ready"})
	producer.Derived = []c.Derivation{initialization{}}
	consumer := rule(c.Outcome{State: c.State{Assessment: c.Good}, Reason: "Observed"})
	consumer.Output.Type = "Observed"
	consumer.DefaultBase = &metav1.Condition{Type: "Observed"}
	consumer.Requires = []string{"Initialized"}
	rules, err := c.Compose([]c.Aggregation{consumer, producer})
	if err != nil {
		t.Fatal(err)
	}
	parent := &object{}
	// Derived ownership ignores conflicting caller assertions, instead of a global IgnoreLocal list.
	if _, err := c.Aggregate(parent, nil, rules, metav1.Condition{Type: "Initialized", Status: metav1.ConditionFalse, Reason: "Caller"}); err != nil {
		t.Fatal(err)
	}
	value, _ := c.Lookup(parent, "Initialized")
	if value.Status != metav1.ConditionTrue {
		t.Fatalf("derivation was overridden: %#v", value)
	}
	duplicate := producer
	duplicate.Output.Type = "Initialized"
	duplicate.DefaultBase = &metav1.Condition{Type: "Initialized"}
	duplicate.Derived = nil
	if _, err := c.Compose([]c.Aggregation{producer, duplicate}); err == nil {
		t.Fatal("accepted competing derived owner")
	}
}

func TestDeclaredDependencyUsesCurrentResult(t *testing.T) {
	producer := rule(c.Outcome{State: c.State{Assessment: c.Good}, Reason: "Ready"})
	consumer := c.Aggregation{Output: c.ConditionType{Type: "Observed"}, DefaultBase: &metav1.Condition{Type: "Observed"}, Parent: c.ChildrenOnly{}, Local: selectReady{}, Reduction: readReady{}, Messages: c.BoundedMessages{}}
	consumer.Requires = []string{"Ready"}
	for _, outputs := range [][]c.Aggregation{{producer, consumer}, {consumer, producer}} {
		rules, err := c.Compose(outputs)
		if err != nil {
			t.Fatal(err)
		}
		parent := &object{conditions: []metav1.Condition{{Type: "Ready", Status: metav1.ConditionFalse, Reason: "Previous"}}}
		if _, err := c.Aggregate(parent, nil, rules); err != nil {
			t.Fatal(err)
		}
		got, _ := c.Lookup(parent, "Observed")
		if got.Status != metav1.ConditionTrue {
			t.Fatalf("dependency did not see current result: %#v", got)
		}
	}
}

func TestUnownedLocalOrderDoesNotMatter(t *testing.T) {
	a, b := &object{}, &object{}
	first := metav1.Condition{Type: "A", Status: metav1.ConditionTrue, Reason: "A"}
	last := metav1.Condition{Type: "Z", Status: metav1.ConditionFalse, Reason: "Z"}
	rules := c.Rules{Now: func() time.Time { return time.Unix(100, 0) }}
	if _, err := c.Aggregate(a, nil, rules, first, last); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Aggregate(b, nil, rules, last, first); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a.GetConditions(), b.GetConditions()) {
		t.Fatal("local order changed desired slice")
	}
}

func TestDuplicateLocalAssertionsAreAtomic(t *testing.T) {
	parent := &object{}
	a := metav1.Condition{Type: "Synced", Status: metav1.ConditionTrue, Reason: "Success"}
	b := a
	b.Status = metav1.ConditionFalse
	b.Reason = "Error"
	if _, err := c.Aggregate(parent, nil, c.Rules{}, a, b); err == nil || parent.sets != 0 {
		t.Fatalf("duplicate local assertions accepted: err=%v sets=%d", err, parent.sets)
	}
}

func TestTypedNilPolicyIsRejected(t *testing.T) {
	producer := rule(c.Outcome{State: c.State{Assessment: c.Good}, Reason: "Ready"})
	var reducer *fixedOutcome
	producer.Reduction = reducer
	if _, err := c.Compose([]c.Aggregation{producer}); err == nil {
		t.Fatal("typed nil policy accepted as complete")
	}
}

func TestIndependentRulesReadSameSnapshot(t *testing.T) {
	producer := rule(c.Outcome{State: c.State{Assessment: c.Good}, Reason: "Ready"})
	consumer := c.Aggregation{Output: c.ConditionType{Type: "Observed"}, DefaultBase: &metav1.Condition{Type: "Observed"}, Parent: c.ChildrenOnly{}, Local: selectReady{}, Reduction: readReady{}, Messages: c.BoundedMessages{}}
	for _, outputs := range [][]c.Aggregation{{producer, consumer}, {consumer, producer}} {
		parent := &object{conditions: []metav1.Condition{{Type: "Ready", Status: metav1.ConditionFalse, Reason: "Previous"}}}
		if _, err := c.Aggregate(parent, nil, c.Rules{Outputs: outputs}); err != nil {
			t.Fatal(err)
		}
		got, _ := c.Lookup(parent, "Observed")
		if got.Status != metav1.ConditionFalse {
			t.Fatalf("undeclared output leaked into independent rule: %#v", got)
		}
	}
}
