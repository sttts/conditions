// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package openshift demonstrates OpenShift operator-status aggregation, not operand observation.
package openshift

import (
	"sort"
	"strings"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	c "github.com/sttts/conditions"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Operator adapts the native status of an existing OpenShift operator configuration object.
type Operator struct {
	metav1.Object
	Status *operatorv1.OperatorStatus
}

// GetConditions translates operator assertions without assigning status-level generation to them.
func (o Operator) GetConditions() []metav1.Condition {
	result := make([]metav1.Condition, 0, len(o.Status.Conditions))
	for _, value := range o.Status.Conditions {
		result = append(result, metav1.Condition{Type: value.Type, Status: metav1.ConditionStatus(value.Status),
			Reason: value.Reason, Message: value.Message, LastTransitionTime: value.LastTransitionTime})
	}
	return result
}

// ClusterOperator adapts the native ClusterOperator output without inventing per-condition generations.
type ClusterOperator struct{ *configv1.ClusterOperator }

// GetConditions reads native ClusterOperator Conditions through the common accessor.
func (o ClusterOperator) GetConditions() []metav1.Condition {
	result := make([]metav1.Condition, 0, len(o.Status.Conditions))
	for _, value := range o.Status.Conditions {
		result = append(result, metav1.Condition{Type: string(value.Type), Status: metav1.ConditionStatus(value.Status),
			Reason: value.Reason, Message: value.Message, LastTransitionTime: value.LastTransitionTime})
	}
	return result
}

// SetConditions reports changes after converting the desired slice to the native representation.
func (o ClusterOperator) SetConditions(values []metav1.Condition) bool {
	desired := make([]configv1.ClusterOperatorStatusCondition, 0, len(values))
	for _, value := range values {
		desired = append(desired, configv1.ClusterOperatorStatusCondition{
			Type: configv1.ClusterStatusConditionType(value.Type), Status: configv1.ConditionStatus(value.Status),
			Reason: value.Reason, Message: value.Message, LastTransitionTime: value.LastTransitionTime})
	}
	if equality.Semantic.DeepEqual(o.Status.Conditions, desired) {
		return false
	}
	o.Status.Conditions = desired
	return true
}

// OperatorConditions selects every matching controller assertion from native operator status.
type OperatorConditions struct{ Condition c.ConditionType }

// Select accepts native operator status and retains every assertion matching the configured suffix.
func (p OperatorConditions) Select(_ c.Object, child c.Object) []c.Contribution {
	if _, ok := child.(Operator); !ok {
		return nil
	}
	var result []c.Contribution
	for _, value := range child.GetConditions() {
		if strings.HasSuffix(value.Type, p.Condition.Type) {
			result = append(result, c.Contribution{State: c.State{Assessment: p.Condition.Assessment(value)},
				Condition: value, Source: child})
		}
	}
	return result
}

// OperatorUnion expresses library-go's suffix union, including optional bad-state inertia.
type OperatorUnion struct {
	Condition c.ConditionType
	Inertia   func(metav1.Condition) time.Duration
	Now       func() time.Time
}

// Reduce gives known adverse statuses priority over Unknown and retains the native union diagnostics.
func (p OperatorUnion) Reduce(_ metav1.Condition, _ map[string]c.Object, _, selected []c.Contribution) c.Outcome {
	if len(selected) == 0 {
		return c.Outcome{State: c.State{Assessment: c.Unknown}, Reason: "NoData"}
	}
	var bad []c.Contribution
	assessment := c.Unknown
	for _, value := range selected {
		if value.State.Assessment != c.Good {
			bad = append(bad, value)
			if value.State.Assessment == c.Bad {
				assessment = c.Bad
			}
		}
	}
	sort.SliceStable(bad, func(i, j int) bool { return bad[i].Condition.Type < bad[j].Condition.Type })

	// One old bad assertion opens the union for every bad assertion, as in library-go.
	eligible := len(bad) > 0 && p.Inertia == nil
	if p.Inertia != nil {
		now := time.Now()
		if p.Now != nil {
			now = p.Now()
		}
		for _, value := range bad {
			if value.Condition.LastTransitionTime.Time.Before(now.Add(-p.Inertia(value.Condition))) {
				eligible = true
			}
		}
	}
	if !eligible {
		return c.Outcome{State: c.State{Assessment: c.Good}, Reason: "AsExpected",
			Summary: unionMessage(selected, "All is well"), LastTransitionTime: latestTransition(selected)}
	}
	var reasons []string
	for _, value := range bad {
		reason := strings.TrimSuffix(value.Condition.Type, p.Condition.Type)
		if value.Condition.Reason != "" {
			reason += "_" + value.Condition.Reason
		}
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	return c.Outcome{State: c.State{Assessment: assessment}, Reason: strings.Join(reasons, "::"),
		Summary: unionMessage(bad, ""), LastTransitionTime: latestTransition(bad)}
}

// unionMessage retains upstream ordering and deduplicates lines within each assertion only.
func unionMessage(selected []c.Contribution, fallback string) string {
	var lines []string
	for _, value := range selected {
		if value.Condition.Message == "" {
			continue
		}
		seen := map[string]bool{}
		for _, line := range strings.Split(value.Condition.Message, "\n") {
			if !seen[line] {
				lines = append(lines, value.Condition.Type+": "+line)
				seen[line] = true
			}
		}
	}
	if len(lines) == 0 {
		return fallback
	}
	return strings.Join(lines, "\n")
}

func latestTransition(selected []c.Contribution) metav1.Time {
	latest := metav1.Time{}
	for _, value := range selected {
		if latest.Before(&value.Condition.LastTransitionTime) {
			latest = value.Condition.LastTransitionTime
		}
	}
	return latest
}

// UnionMessages preserves the already-rendered native union message without interpreting its contents.
type UnionMessages struct{}

func (UnionMessages) Format(summary string, _ []c.Cause) string { return summary }
func (UnionMessages) Inline(value metav1.Condition) string      { return value.Message }

var (
	available   = c.ConditionType{Type: "Available", Polarity: c.TrueIsGood}
	progressing = c.ConditionType{Type: "Progressing", Polarity: c.FalseIsGood}
	degraded    = c.ConditionType{Type: "Degraded", Polarity: c.FalseIsGood}
	upgradeable = c.ConditionType{Type: "Upgradeable", Polarity: c.TrueIsGood}
)

// OpenShiftRules projects four independent operator-status unions without enabling inertia by default.
var OpenShiftRules = c.Rules{Outputs: []c.Aggregation{
	{Output: available, DefaultBase: &metav1.Condition{Type: "Available"}, Parent: c.ChildrenOnly{},
		Children: OperatorConditions{available}, Reduction: OperatorUnion{Condition: available},
		Messages: UnionMessages{}, Generation: c.NoObservedGeneration},
	{Output: progressing, DefaultBase: &metav1.Condition{Type: "Progressing"}, Parent: c.ChildrenOnly{},
		Children: OperatorConditions{progressing}, Reduction: OperatorUnion{Condition: progressing},
		Messages: UnionMessages{}, Generation: c.NoObservedGeneration},
	{Output: degraded, DefaultBase: &metav1.Condition{Type: "Degraded"}, Parent: c.ChildrenOnly{},
		Children: OperatorConditions{degraded}, Reduction: OperatorUnion{Condition: degraded},
		Messages: UnionMessages{}, Generation: c.NoObservedGeneration},
	{Output: upgradeable, DefaultBase: &metav1.Condition{Type: "Upgradeable"}, Parent: c.ChildrenOnly{},
		Children: OperatorConditions{upgradeable}, Reduction: OperatorUnion{Condition: upgradeable},
		Messages: UnionMessages{}, Generation: c.NoObservedGeneration},
}}

var (
	_ c.Object      = Operator{}
	_ c.Conditioned = ClusterOperator{}
)
