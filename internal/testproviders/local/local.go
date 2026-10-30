// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package local is an independent rule provider fixture using only public APIs.
package local

import (
	c "github.com/sttts/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Rules supplies one local-only readiness assertion; callers supply the base.
func Rules() []c.Aggregation {
	ready := c.ConditionType{Type: "Ready"}
	return []c.Aggregation{{Output: ready, Parent: c.LocalOnly{LocalBaseFirst: c.LocalBaseFirst{Base: ready}}, Reduction: copyBase{}, Messages: c.MultilineMessages{}}}
}

type copyBase struct{}

func (copyBase) Reduce(base metav1.Condition, _ map[string]c.Object, _, _ []c.Contribution) c.Outcome {
	return c.Outcome{State: c.State{Assessment: (c.ConditionType{Type: base.Type}).Assessment(base)}, Reason: base.Reason, Summary: base.Message, Copied: &base}
}
