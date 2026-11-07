// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package openshift

import (
	c "github.com/sttts/conditions"
	ref "github.com/sttts/conditions/internal/testreference"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strings"
)

// UnionRule supplies one independently selectable operator-status union.
// It enables no other axes, inertia, local-input handling, or evaluation-wide policy.
func UnionRule(condition c.ConditionType) ref.Rule {
	return ref.Rule{
		Output: condition, DefaultBase: &metav1.Condition{Type: condition.Type}, Parent: c.ChildrenOnly{},
		Children: OperatorConditions{condition}, Reduction: OperatorUnion{Condition: condition},
		Formatter: UnionMessages{}, Generation: c.NoObservedGeneration,
	}
}

// OpenShiftRules selects four independent unions, without enabling inertia by default.
var OpenShiftRules = ref.Rules{Outputs: []ref.Rule{
	UnionRule(available), UnionRule(progressing), UnionRule(degraded), UnionRule(upgradeable),
}}

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
