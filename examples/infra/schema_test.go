// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package infra

import (
	c "github.com/sttts/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"reflect"
	"testing"
)

// Every reference aggregate case checks both paths, including exact message and metadata
// equality before the reference suite applies its historical message exclusions.
func applyDefined(t *testing.T, parent Conditioned, children map[string]Object, rules Rules, local ...metav1.Condition) (bool, error) {
	t.Helper()
	want, err := Evaluate(parent, children, rules, local...)
	if err != nil {
		return false, err
	}
	changed := c.Aggregate(parent, children, defineAggregation(rules), local...)
	if !reflect.DeepEqual(parent.GetConditions(), want) {
		t.Fatalf("Define changed reference result:\ngot  %#v\nwant %#v", parent.GetConditions(), want)
	}
	return changed, nil
}

func defineAggregation(rules Rules) *c.Aggregation {
	d := c.Definition{Now: rules.Now, Children: []c.ChildBinding{c.Child(InputSchema, c.Identity)}}
	available := map[string]c.OutputDefinition{"Available": AvailabilityOutput, "NonDegraded": NonDegradedOutput, "UpToDate": UpToDateOutput, "Synced": SyncedOutput, "Deleting": DeletingOutput}
	for _, rule := range rules.Outputs {
		output, ok := available[rule.Output.Type]
		if !ok {
			panic("unexpected test output: " + rule.Output.Type)
		}
		d.Outputs = append(d.Outputs, output)
	}
	return c.Define(d)
}
