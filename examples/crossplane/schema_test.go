// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package crossplane

import (
	c "github.com/sttts/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
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
