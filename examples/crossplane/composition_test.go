// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package crossplane

import (
	c "github.com/sttts/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
)

func TestSelectReadinessRule(t *testing.T) {
	rules, err := c.Compose([]c.Aggregation{ReadinessRule()})
	if err != nil {
		t.Fatal(err)
	}
	parent := resource()
	if _, err := c.Aggregate(parent, nil, rules); err != nil {
		t.Fatal(err)
	}
	value, _ := c.Lookup(parent, "Ready")
	if value.Status != metav1.ConditionTrue {
		t.Fatalf("unexpected readiness: %#v", value)
	}
	if _, exists := c.Lookup(parent, "Synced"); exists {
		t.Fatal("selected readiness invented reconciliation result")
	}
}
