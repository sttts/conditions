// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package conditions

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"time"
)

type TestRule = compiledRule
type TestRules = evaluationPlan

func EngineCompose(groups ...[]compiledRule) (evaluationPlan, error) {
	rules := evaluationPlan{}
	for _, group := range groups {
		rules.Outputs = append(rules.Outputs, group...)
	}
	if _, _, err := plan(rules); err != nil {
		return evaluationPlan{}, err
	}
	return rules, nil
}

func EngineApplyRules(parent Conditioned, children map[string]Object, rules evaluationPlan, local ...metav1.Condition) (bool, error) {
	desired, err := evaluate(parent, children, rules, local...)
	if err != nil {
		return false, err
	}
	return parent.SetConditions(desired), nil
}

// Evaluate exposes the internal single-output evaluator only to tests.
func (a compiledRule) Evaluate(parent Object, children map[string]Object, base metav1.Condition, now time.Time) (*metav1.Condition, error) {
	return a.evaluate(parent, children, base, now)
}
