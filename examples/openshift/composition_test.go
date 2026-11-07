// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package openshift

import (
	configv1 "github.com/openshift/api/config/v1"
	c "github.com/sttts/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
)

func TestSelectOneUnion(t *testing.T) {
	aggregation := c.Define(c.Definition{
		Outputs:  []c.OutputDefinition{unionOutput("Degraded", c.Spec{Polarity: c.FalseIsGood})},
		Children: []c.ChildBinding{c.Child(OperatorSchema, c.Identity)},
	})
	parent := ClusterOperator{&configv1.ClusterOperator{}}
	if _, err := c.Aggregate(parent, nil, aggregation); err != nil {
		t.Fatal(err)
	}
	if len(parent.GetConditions()) != 1 {
		t.Fatal("selecting one union enabled other axes")
	}
	value, _ := c.Lookup(parent, "Degraded")
	if value.Status != metav1.ConditionUnknown || value.Reason != "NoData" {
		t.Fatalf("lost no-data semantics: %#v", value)
	}
}
