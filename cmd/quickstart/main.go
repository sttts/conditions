// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"io"
	"os"

	"github.com/sttts/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// A dependency publishes its vocabulary. This schema is global for simplicity.
var databaseSchema = conditions.Schema{
	Conditions: map[string]conditions.Spec{
		"Available": {Polarity: conditions.TrueIsGood},
	},
}

// Define once, usually in a package variable.
var aggregation = conditions.Define(conditions.Definition{
	Outputs: []conditions.OutputDefinition{
		conditions.All("Ready", conditions.Spec{Polarity: conditions.TrueIsGood}),
		conditions.Local("Synced", conditions.Spec{Polarity: conditions.TrueIsGood}),
	},
	Children: []conditions.ChildBinding{
		conditions.Child(databaseSchema, conditions.Classify(conditions.Map("Available", "Ready"))),
	},
})

// Publish the result for the next consumer.
var Schema = aggregation.Schema()

// Embed metadata and adapt your resource's condition storage.
type resource struct {
	metav1.ObjectMeta
	Conditions []metav1.Condition
}

func (r *resource) GetConditions() []metav1.Condition       { return r.Conditions }
func (r *resource) SetConditions(values []metav1.Condition) { r.Conditions = values }

func run(out io.Writer) error {
	parent := &resource{ObjectMeta: metav1.ObjectMeta{Generation: 7}}
	database := &resource{Conditions: []metav1.Condition{{
		Type: "Available", Status: metav1.ConditionFalse,
		Reason: "Provisioning", Message: "waiting for endpoint",
	}}}
	children := map[string]conditions.Object{"database": database}
	synced := metav1.Condition{
		Type: "Synced", Status: metav1.ConditionTrue, Reason: "ReconcileSuccess",
		ObservedGeneration: parent.Generation,
	}

	changed, err := conditions.Aggregate(conditions.WithChangeDetection(parent), children, aggregation, synced)
	if err != nil {
		return err
	}
	ready, _ := conditions.Lookup(parent, "Ready")
	fmt.Fprintln(out, "changed:", changed)
	fmt.Fprintln(out, "Ready:", ready.Status, ready.Reason)
	fmt.Fprintln(out, ready.Message)

	changed, err = conditions.Aggregate(conditions.WithChangeDetection(parent), children, aggregation, synced)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "same observations:", changed)
	return nil
}

func main() {
	if err := run(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
