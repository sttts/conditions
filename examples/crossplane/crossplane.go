// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package crossplane demonstrates readiness aggregation over native Crossplane Conditions.
package crossplane

import (
	xpv1 "github.com/crossplane/crossplane-runtime/v2/apis/common/v1"
	c "github.com/sttts/conditions"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Resource is an example CRD object with Crossplane's actual ConditionedStatus representation.
type Resource struct {
	metav1.ObjectMeta `json:"metadata"`
	Status            xpv1.ConditionedStatus `json:"status"`
}

// Adapter translates Condition representations while retaining all native fields.
type Adapter struct{ *Resource }

// GetConditions returns a converted copy of the native Conditions slice.
func (o Adapter) GetConditions() []metav1.Condition {
	result := make([]metav1.Condition, 0, len(o.Status.Conditions))
	for _, value := range o.Status.Conditions {
		result = append(result, ToCondition(value))
	}
	return result
}

// SetConditions reports native changes without recomputing timestamps or generations.
func (o Adapter) SetConditions(values []metav1.Condition) bool {
	desired := make([]xpv1.Condition, 0, len(values))
	for _, value := range values {
		desired = append(desired, xpv1.Condition{Type: xpv1.ConditionType(value.Type),
			Status: corev1.ConditionStatus(value.Status), Reason: xpv1.ConditionReason(value.Reason),
			Message: value.Message, LastTransitionTime: value.LastTransitionTime, ObservedGeneration: value.ObservedGeneration})
	}
	if equality.Semantic.DeepEqual(o.Status.Conditions, desired) {
		return false
	}
	o.Status.Conditions = desired
	return true
}

// ToCondition converts an existing local assertion without reclassifying it.
func ToCondition(value xpv1.Condition) metav1.Condition {
	return metav1.Condition{Type: string(value.Type), Status: metav1.ConditionStatus(value.Status),
		Reason: string(value.Reason), Message: value.Message, LastTransitionTime: value.LastTransitionTime,
		ObservedGeneration: value.ObservedGeneration}
}

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

// Readiness combines readiness assertions without inventing failure escalation from Crossplane reasons.
type Readiness struct{}

// Reduce preserves local precedence and combines child readiness independently of reconciliation status.
func (Readiness) Reduce(base metav1.Condition, _ map[string]c.Object, _, selected []c.Contribution) c.Outcome {
	if ready.Assessment(base) != c.Good {
		return c.Outcome{State: c.State{Assessment: ready.Assessment(base)}, Reason: base.Reason,
			Summary: base.Message, Copied: &base}
	}
	worst := c.Good
	for _, value := range selected {
		if value.State.Assessment == c.Bad {
			worst = c.Bad
		} else if value.State.Assessment == c.Unknown && worst == c.Good {
			worst = c.Unknown
		}
	}
	if worst == c.Good {
		return c.Outcome{State: c.State{Assessment: c.Good}, Reason: string(xpv1.ReasonAvailable),
			Summary: "All composed resources are ready"}
	}
	result := c.Outcome{State: c.State{Assessment: worst}, Reason: "ReadinessUnknown",
		Summary: "Readiness of composed resources is unknown"}
	if worst == c.Bad {
		result.Reason, result.Summary = string(xpv1.ReasonCreating), "Waiting for composed resources"
	}
	for _, value := range selected {
		if value.State.Assessment == worst {
			message := value.Condition.Message
			if message == "" {
				message = value.Condition.Reason
			}
			result.Causes = append(result.Causes, c.Cause{Reference: value.Reference, Message: message})
		}
	}
	return result
}

var ready = c.ConditionType{Type: string(xpv1.TypeReady), Polarity: c.TrueIsGood}

// ReadinessRule supplies independently selectable readiness policy and opaque diagnostics.
// Synced remains an independent local assertion; it is neither required nor derived.
func ReadinessRule() c.Aggregation {
	return c.Aggregation{
		Output: ready, Parent: c.LocalBaseFirst{Base: ready}, Children: ReadyChildren{}, Reduction: Readiness{},
		DefaultBase: &metav1.Condition{Type: string(xpv1.TypeReady), Status: metav1.ConditionTrue, Reason: string(xpv1.ReasonAvailable)},
		Messages:    c.BoundedMessages{MaxLines: 7},
	}
}

// CrossplaneRules selects the example's readiness rule without a separate evaluation path.
var CrossplaneRules = c.Rules{Outputs: []c.Aggregation{ReadinessRule()}}

var _ c.Conditioned = Adapter{}
