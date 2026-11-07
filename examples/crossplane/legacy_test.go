// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package crossplane

import (
	xpv1 "github.com/crossplane/crossplane-runtime/v2/apis/common/v1"
	c "github.com/sttts/conditions"
	ref "github.com/sttts/conditions/internal/testreference"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ReadinessRule supplies independently selectable readiness policy and opaque diagnostics.
// Synced remains an independent local assertion; it is neither required nor derived.
func ReadinessRule() ref.Rule {
	return ref.Rule{
		Output: ready, Parent: c.LocalBaseFirst{Base: ready}, Children: ReadyChildren{}, Reduction: Readiness{},
		DefaultBase: &metav1.Condition{Type: string(xpv1.TypeReady), Status: metav1.ConditionTrue, Reason: string(xpv1.ReasonAvailable)},
		Formatter:   c.BoundedMessages{MaxLines: 7},
	}
}

// CrossplaneRules selects the example's readiness rule without a separate evaluation path.
var CrossplaneRules = ref.Rules{Outputs: []ref.Rule{ReadinessRule()}}

// ReadyChildren selects readiness independently of each child's reconciliation status.
type ReadyChildren struct{}

// Select reads only Ready and supplies an explicit unknown contribution when it is missing.
func (ReadyChildren) Select(_ c.Object, child c.Object) []c.Contribution {
	value, found := c.Lookup(child, string(xpv1.TypeReady))
	if !found {
		value.Reason, value.Message = "ConditionMissing", "Ready condition has not been reported"
	}
	return []c.Contribution{{State: c.State{Assessment: ready.Assessment(value)}, Condition: value, Source: child}}
}
