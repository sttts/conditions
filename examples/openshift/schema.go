// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package openshift

import (
	c "github.com/sttts/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// OperatorSchema describes families of controller assertions, not operand health.
var OperatorSchema = c.Schema{
	Resources: []c.ResourceMatch{c.G("operator.openshift.io")},
	Conditions: map[string]c.Spec{
		"Available":   {Polarity: c.TrueIsGood, Suffix: true},
		"Progressing": {Polarity: c.FalseIsGood, Suffix: true},
		"Degraded":    {Polarity: c.FalseIsGood, Suffix: true},
		"Upgradeable": {Polarity: c.TrueIsGood, Suffix: true},
	},
}

// unionOutput combines the public contract with the native Go union implementation.
func unionOutput(name string, spec c.Spec) c.OutputDefinition {
	return c.OutputDefinition{
		Name: name, Spec: spec,
		DefaultBase: &metav1.Condition{}, Parent: c.ChildrenOnly{},
		Reduction: OperatorUnion{Condition: c.ConditionType{Type: name, Polarity: spec.Polarity}},
		Formatter: UnionMessages{}, Generation: c.NoObservedGeneration,
	}
}

var Aggregation = c.Define(definition())

func definition() c.Definition {
	return c.Definition{
		Resources: []c.ResourceMatch{c.GK("config.openshift.io", "ClusterOperator")},
		Outputs: []c.OutputDefinition{
			unionOutput("Available", c.Spec{Polarity: c.TrueIsGood}),
			unionOutput("Progressing", c.Spec{Polarity: c.FalseIsGood}),
			unionOutput("Degraded", c.Spec{Polarity: c.FalseIsGood}),
			unionOutput("Upgradeable", c.Spec{Polarity: c.TrueIsGood}),
		},
		Children: []c.ChildBinding{c.Child(OperatorSchema, c.Identity)},
	}
}

var Schema = Aggregation.Schema()
