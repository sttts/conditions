// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package crossplane_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	xpv1 "github.com/crossplane/crossplane-runtime/v2/apis/common/v1"
	c "github.com/sttts/conditions"
	"github.com/sttts/conditions/examples/crossplane"
	lifecycle "github.com/sttts/conditions/examples/infra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// updateConditions is the entire controller boundary: choose rules, supply objects,
// handle the change flag/error, and leave API-server persistence to the reconciler.
func updateConditions(parent c.Conditioned, children map[string]c.Object, synced metav1.Condition) (bool, error) {
	rules, err := c.Compose([]c.Aggregation{
		lifecycle.InfraSynced,      // local reconciliation + Infra's prefix-check policy
		crossplane.ReadinessRule(), // child readiness + opaque diagnostics
	})
	if err != nil {
		return false, err
	}
	return c.Aggregate(parent, children, rules, synced)
}

func Example_composition() {
	parent := crossplane.Adapter{Resource: &crossplane.Resource{}}
	child := crossplane.Adapter{Resource: &crossplane.Resource{}}
	child.Status.SetConditions(xpv1.Available(), xpv1.ReconcileError(errors.New("child update denied")))
	changed, err := updateConditions(parent, map[string]c.Object{"database": child}, crossplane.ToCondition(xpv1.ReconcileSuccess()))
	if err != nil {
		panic(err)
	}
	fmt.Println("changed:", changed)
	for _, typ := range []string{"Ready", "Synced"} {
		value, _ := c.Lookup(parent, typ)
		fmt.Println(typ, value.Status)
	}
	// Output:
	// changed: true
	// Ready True
	// Synced True
}

func TestIndependentExampleModulesCompose(t *testing.T) {
	rules, err := c.Compose([]c.Aggregation{lifecycle.InfraSynced, crossplane.ReadinessRule()})
	if err != nil {
		t.Fatal(err)
	}
	rules.Now = func() time.Time { return time.Unix(100, 0) }
	parent := crossplane.Adapter{Resource: &crossplane.Resource{ObjectMeta: metav1.ObjectMeta{Generation: 7}}}
	child := crossplane.Adapter{Resource: &crossplane.Resource{}}
	child.Status.SetConditions(xpv1.Creating().WithMessage("waiting for database"), xpv1.ReconcileSuccess())
	children := map[string]c.Object{"database": child}
	local := crossplane.ToCondition(xpv1.ReconcileError(errors.New("parent update denied")))
	if changed, err := c.Aggregate(parent, children, rules, local); err != nil || !changed {
		t.Fatalf("aggregate: %v %v", changed, err)
	}
	if value, _ := c.Lookup(parent, "Ready"); value.Status != metav1.ConditionFalse || value.Reason != "Creating" || value.Message != "Waiting for composed resources:\n  database: waiting for database" {
		t.Fatalf("lost child readiness/opaque diagnostics: %#v", value)
	}
	if value, _ := c.Lookup(parent, "Synced"); value.Status != metav1.ConditionFalse || value.Reason != "ReconcileError" {
		t.Fatalf("child success hid local failure: %#v", value)
	}
	if changed, err := c.Aggregate(parent, children, rules, local); err != nil || changed {
		t.Fatalf("repeat: %v %v", changed, err)
	}
	if len(parent.GetConditions()) != 2 {
		t.Fatal("unselected Infra axes were enabled")
	}
	child.Status.SetConditions(xpv1.Available(), xpv1.ReconcileError(errors.New("child update denied")))
	local = crossplane.ToCondition(xpv1.ReconcileSuccess())
	if _, err := c.Aggregate(parent, children, rules, local); err != nil {
		t.Fatal(err)
	}
	for _, typ := range []string{"Ready", "Synced"} {
		if value, _ := c.Lookup(parent, typ); value.Status != metav1.ConditionTrue {
			t.Fatalf("failed to recover %s: %#v", typ, value)
		}
	}
}
