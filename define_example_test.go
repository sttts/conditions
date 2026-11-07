// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package conditions_test

import (
	"fmt"
	c "github.com/sttts/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// A dependency package publishes this; it knows nothing about its consumers.
var databaseSchema = c.Schema{
	Resources: []c.ResourceMatch{c.G("database.example.io")},
	Conditions: map[string]c.Spec{
		"Available": {Polarity: c.TrueIsGood},
		"Synced":    {Polarity: c.TrueIsGood},
	},
}

// The application defines its outputs and translates foreign observations once.
var applicationAggregation = c.Define(c.Definition{
	Resources: []c.ResourceMatch{c.GK("apps.example.io", "Application")},
	Outputs: []c.OutputDefinition{
		c.All("Ready", c.Spec{Polarity: c.TrueIsGood}),
		c.Local("Synced", c.Spec{Polarity: c.TrueIsGood}),
	},
	Children: []c.ChildBinding{
		c.Child(databaseSchema, c.Classify(
			c.Map("Available", "Ready").Escalate(c.WaitingForHuman, c.Reason("InvalidConfiguration")),
		)),
	},
})

// Another package can consume this without importing the derivation's implementation.
var applicationSchema = applicationAggregation.Schema()

func ExampleAggregate() {
	parent := &exampleResource{ObjectMeta: metav1.ObjectMeta{Generation: 7}}
	database := &exampleResource{Conditions: []metav1.Condition{{Type: "Available", Status: metav1.ConditionFalse, Reason: "Provisioning", Message: "waiting for endpoint"}}}
	children := map[string]c.Object{"database": c.WithGVK(database, schema.GroupVersionKind{Group: "database.example.io", Version: "v1", Kind: "Database"})}
	local := metav1.Condition{Type: "Synced", Status: metav1.ConditionTrue, Reason: "ReconcileSuccess"}
	changed, err := c.Aggregate(parent, children, applicationAggregation, local)
	if err != nil {
		fmt.Println(err)
		return
	}
	ready, _ := c.Lookup(parent, "Ready")
	fmt.Println("changed:", changed)
	fmt.Println(ready.Status, ready.Reason)
	fmt.Println(ready.Message)
	changed, err = c.Aggregate(parent, children, applicationAggregation, local)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("same observations:", changed)
	// Output:
	// changed: true
	// False ConditionsNotSatisfied
	// Conditions are not satisfied:
	//   database: waiting for endpoint
	// same observations: false
}
