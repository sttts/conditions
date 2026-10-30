// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package dependent implements a second provider without importing the first provider.
package dependent

import (
	c "github.com/sttts/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Rules explicitly requires Ready from any selected provider, not a concrete package.
func Rules() []c.Aggregation {
	return []c.Aggregation{{Output: c.ConditionType{Type: "Observed"}, Requires: []string{"Ready"}, DefaultBase: &metav1.Condition{Type: "Observed"}, Parent: c.ChildrenOnly{}, Local: ready{}, Reduction: observation{}, Messages: c.BoundedMessages{}}}
}

type ready struct{}

func (ready) Select(parent c.Object) []c.Contribution {
	value, _ := c.Lookup(parent, "Ready")
	return []c.Contribution{{State: c.State{Assessment: (c.ConditionType{Type: "Ready"}).Assessment(value)}, Condition: value}}
}

type observation struct{}

func (observation) Reduce(_ metav1.Condition, _ map[string]c.Object, local, _ []c.Contribution) c.Outcome {
	return c.Outcome{State: local[0].State, Reason: local[0].Condition.Reason, Summary: local[0].Condition.Message}
}
