// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package infra

import (
	c "github.com/sttts/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
	"time"
)

// Apply the public API directly; fixture expectations are the independent oracle.
func applyDefined(t *testing.T, parent c.Conditioned, children map[string]c.Object, d c.Definition, local ...metav1.Condition) (bool, error) {
	t.Helper()
	desired, err := c.EvaluateAggregation(parent, children, c.Define(d), local...)
	if err != nil {
		return false, err
	}
	return parent.SetConditions(desired), nil
}

func evaluateOutput(output c.OutputDefinition, parent c.Object, children map[string]c.Object, base metav1.Condition, now time.Time) (*metav1.Condition, error) {
	output.Requires = nil // The corpus explicitly orders steps and supplies persisted inputs.
	output.Derived = nil  // Initialized has its own explicit corpus step.
	base.Type = output.Name
	d := c.Definition{Outputs: []c.OutputDefinition{output}, Children: []c.ChildBinding{c.Child(InputSchema, c.Classify())}, Now: func() time.Time { return now }}
	return c.Evaluate(output.Name, parent, children, c.Define(d), base)
}
