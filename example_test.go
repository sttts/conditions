// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package conditions_test

import (
	"fmt"

	conditions "github.com/sttts/conditions"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func ExampleAggregate() {
	parent := &exampleResource{ObjectMeta: metav1.ObjectMeta{Name: "application", Generation: 7}}
	database := &exampleResource{ObjectMeta: metav1.ObjectMeta{Name: "database", Generation: 3},
		Conditions: []metav1.Condition{{Type: "Ready", Status: metav1.ConditionFalse,
			Reason: "Provisioning", Message: "waiting for endpoint", ObservedGeneration: 3}}}
	localReady := metav1.Condition{Type: "Ready", Status: metav1.ConditionTrue, Reason: "LocalChecksPassed"}
	changed, err := updateReadiness(parent, database, localReady)
	if err != nil {
		panic(err)
	}
	ready, _ := conditions.Lookup(parent, "Ready")
	fmt.Printf("changed=%t, Ready=%s, reason=%s\n", changed, ready.Status, ready.Reason)
	fmt.Println(ready.Message)
	changed, err = updateReadiness(parent, database, localReady)
	if err != nil {
		panic(err)
	}
	fmt.Printf("same observations: changed=%t\n", changed)
	// Output:
	// changed=true, Ready=False, reason=ChildrenNotReady
	// Required children are not ready:
	//   database: waiting for endpoint
	// same observations: changed=false
}

// BEGIN README_USAGE
func updateReadiness(
	parent conditions.Conditioned,
	database conditions.Object,
	localReady metav1.Condition,
) (bool, error) {
	ready := conditions.ConditionType{Type: "Ready", Polarity: conditions.TrueIsGood}

	// Application policy: select child Ready and require all selected children to be ready.
	policy := readiness{Condition: ready}
	rules := conditions.Rules{Outputs: []conditions.Aggregation{{
		Output:    ready,
		Parent:    conditions.LocalBaseFirst{Base: ready},
		Children:  policy,
		Reduction: policy,
		Messages:  conditions.MultilineMessages{MaxLines: 7},
	}}}

	// The controller supplies existing objects and its already-computed local Ready assertion.
	return conditions.Aggregate(parent,
		map[string]conditions.Object{"database": database},
		rules,
		localReady,
	)
}

// END README_USAGE

// readiness is application code implementing the ChildSelector and Reducer contracts.
type readiness struct{ Condition conditions.ConditionType }

func (p readiness) Select(_ conditions.Object, child conditions.Object) []conditions.Contribution {
	value := p.Condition.Read(child)
	return []conditions.Contribution{{Condition: value, Source: child,
		State: conditions.State{Assessment: p.Condition.Assessment(value)}}}
}

func (p readiness) Reduce(base metav1.Condition, _ map[string]conditions.Object, _, selected []conditions.Contribution) conditions.Outcome {
	if assessment := p.Condition.Assessment(base); assessment != conditions.Good {
		return conditions.Outcome{State: conditions.State{Assessment: assessment}, Reason: base.Reason,
			Summary: base.Message, Copied: &base}
	}
	state := conditions.Good
	for _, value := range selected {
		if value.State.Assessment == conditions.Bad {
			state = conditions.Bad
		} else if value.State.Assessment == conditions.Unknown && state == conditions.Good {
			state = conditions.Unknown
		}
	}
	result := conditions.Outcome{State: conditions.State{Assessment: state}, Reason: "ChildrenReady",
		Summary: "All required children are ready"}
	if state == conditions.Good {
		return result
	}
	result.Reason, result.Summary = "ChildrenUnknown", "Child readiness is unknown"
	if state == conditions.Bad {
		result.Reason, result.Summary = "ChildrenNotReady", "Required children are not ready"
	}
	for _, value := range selected {
		if value.State.Assessment == state {
			message := value.Condition.Message
			if message == "" {
				message = value.Condition.Reason
			}
			result.Causes = append(result.Causes, conditions.Cause{Reference: value.Reference, Message: message})
		}
	}
	return result
}

// exampleResource is a minimal resource fixture with metadata and native accessors.
type exampleResource struct {
	metav1.ObjectMeta
	Conditions []metav1.Condition
}

func (o *exampleResource) GetConditions() []metav1.Condition { return o.Conditions }
func (o *exampleResource) SetConditions(values []metav1.Condition) bool {
	if equality.Semantic.DeepEqual(o.Conditions, values) {
		return false
	}
	o.Conditions = values
	return true
}
