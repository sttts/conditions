// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package crossplane_test

import (
	"fmt"
	"testing"

	xpv1 "github.com/crossplane/crossplane-runtime/v2/apis/common/v1"
	c "github.com/sttts/conditions"
	"github.com/sttts/conditions/examples/crossplane"
	"github.com/sttts/conditions/examples/infra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Neither provider knows about this application or imports the other provider.
// Their published schemas are the complete consumer-side integration surface.
var application = c.Define(c.Definition{
	Resources: []c.ResourceMatch{c.G("apps.example.io")},
	Outputs: []c.OutputDefinition{
		c.Output("Ready", c.Spec{Polarity: c.TrueIsGood}),
		c.Output("Synced", c.Spec{Polarity: c.TrueIsGood}).Local(),
	},
	Children: []c.ChildBinding{
		c.Child(infra.Schema, c.Classify(c.Map("Available", "Ready"))).For(c.GK("infra.example.io", "Worker")),
		c.Child(crossplane.Schema, c.Classify(c.Map("Ready", "Ready"))).For(c.G("database.example.io")),
	},
})
var applicationSchema = application.Schema()

func ExampleDefine() {
	parent := crossplane.Adapter{Resource: &crossplane.Resource{}}
	database := crossplane.Adapter{Resource: &crossplane.Resource{}}
	database.Status.SetConditions(xpv1.Creating().WithMessage("waiting for endpoint"))
	worker := crossplane.Adapter{Resource: &crossplane.Resource{}}
	worker.SetConditions([]metav1.Condition{{Type: "Available", Status: metav1.ConditionTrue, Reason: "Serving"}})
	changed := c.Aggregate(parent, map[string]c.Object{
		"database": c.WithGVK(database, schema.GroupVersionKind{Group: "database.example.io", Version: "v1", Kind: "Database"}),
		"worker":   c.WithGVK(worker, schema.GroupVersionKind{Group: "infra.example.io", Version: "v1", Kind: "Worker"}),
	}, application, crossplane.ToCondition(xpv1.ReconcileSuccess()))
	value, _ := c.Lookup(parent, "Ready")
	fmt.Println(changed, value.Status)
	fmt.Println(value.Message)
	// Output:
	// true False
	// Conditions are not satisfied:
	//   database: waiting for endpoint
}

func TestPublishedSchemasAcrossModulesAndLevels(t *testing.T) {
	parent := crossplane.Adapter{Resource: &crossplane.Resource{}}
	database := crossplane.Adapter{Resource: &crossplane.Resource{}}
	database.Status.SetConditions(xpv1.Available())
	worker := crossplane.Adapter{Resource: &crossplane.Resource{}}
	worker.SetConditions([]metav1.Condition{{Type: "Available", Status: metav1.ConditionFalse, Reason: "ProvisioningFailed", Message: "capacity exhausted"}})
	children := map[string]c.Object{
		"database": c.WithGVK(database, schema.GroupVersionKind{Group: "database.example.io", Version: "v1", Kind: "Database"}),
		"worker":   c.WithGVK(worker, schema.GroupVersionKind{Group: "infra.example.io", Version: "v2", Kind: "Worker"}),
	}
	c.Aggregate(parent, children, application, crossplane.ToCondition(xpv1.ReconcileSuccess()))
	if value, _ := c.Lookup(parent, "Ready"); value.Reason != "ConditionsFailed" {
		t.Fatalf("lost provider's escalation: %#v", value)
	}
	next := c.Define(c.Definition{
		Outputs:  []c.OutputDefinition{c.Output("Ready", applicationSchema.Conditions["Ready"]), c.Output("Synced", applicationSchema.Conditions["Synced"]).Local()},
		Children: []c.ChildBinding{c.Child(applicationSchema, c.Identity)},
	})
	root := crossplane.Adapter{Resource: &crossplane.Resource{}}
	typedParent := c.WithGVK(parent, schema.GroupVersionKind{Group: "apps.example.io", Version: "v1", Kind: "Application"})
	c.AggregateSlice(root, []c.Object{typedParent}, next)
	if value, _ := c.Lookup(root, "Ready"); value.Status != metav1.ConditionFalse || value.Reason != "ConditionsFailed" {
		t.Fatalf("publication/identity lost escalation: %#v", value)
	}
	if _, found := c.Lookup(root, "Synced"); found {
		t.Fatal("child reconciliation promoted to parent")
	}
	worker.SetConditions([]metav1.Condition{{Type: "Available", Status: metav1.ConditionTrue, Reason: "Serving"}})
	c.Aggregate(parent, children, application)
	c.AggregateSlice(root, []c.Object{typedParent}, next)
	if value, _ := c.Lookup(root, "Ready"); value.Status != metav1.ConditionTrue {
		t.Fatal(value)
	}
	if c.AggregateSlice(root, []c.Object{typedParent}, next) {
		t.Fatal("repeat changed output")
	}
}
