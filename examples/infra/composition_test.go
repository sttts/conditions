// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package infra

import (
	"reflect"
	"slices"
	"testing"

	c "github.com/sttts/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestExplicitLocalChecks(t *testing.T) {
	rules, err := c.Compose([]c.Aggregation{LocalChecks("Custom")})
	if err != nil {
		t.Fatal(err)
	}
	parent := referenceParent(t, metav1.Condition{Type: "CustomHealth", Status: metav1.ConditionFalse, Reason: "HealthFailed", Message: "broken"})
	_, err = c.Aggregate(parent, nil, rules, metav1.Condition{Type: "Custom", Status: metav1.ConditionTrue, Reason: "Passed"})
	if err != nil {
		t.Fatal(err)
	}
	value, _ := c.Lookup(parent, "Custom")
	if value.Status != metav1.ConditionFalse || value.Reason != "HealthFailed" {
		t.Fatalf("lost local prefix semantics: %#v", value)
	}
}

func TestInfraCompositionIsOrderIndependent(t *testing.T) {
	fixtures, _ := loadFixtures(t)
	for _, f := range fixtures {
		t.Run(f.Name, func(t *testing.T) {
			a, children := fixtureObjects(f)
			b, _ := fixtureObjects(f)
			rules := InfraRules
			rules.Now = fixtureNow
			reversed := rules
			reversed.Outputs = slices.Clone(rules.Outputs)
			slices.Reverse(reversed.Outputs)
			local := slices.Clone(f.Bases)
			slices.Reverse(local)
			if _, err := Aggregate(a, children, rules, f.Bases...); err != nil {
				t.Fatal(err)
			}
			if _, err := Aggregate(b, children, reversed, local...); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(a.GetConditions(), b.GetConditions()) {
				t.Fatalf("rule/local permutation changed result: a=%#v b=%#v", a.GetConditions(), b.GetConditions())
			}
		})
	}
}
