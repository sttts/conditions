// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package crossplane

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	xpv1 "github.com/crossplane/crossplane-runtime/v2/apis/common/v1"
	c "github.com/sttts/conditions"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var epoch = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func resource(values ...xpv1.Condition) Adapter {
	result := &Resource{ObjectMeta: metav1.ObjectMeta{Generation: 7}}
	result.Status.SetConditions(values...)
	return Adapter{result}
}

func fixedRules() c.Rules {
	rules := CrossplaneRules
	rules.Now = func() time.Time { return epoch }
	return rules
}

func TestNativeReadinessReasons(t *testing.T) {
	for _, test := range []struct {
		name   string
		values []xpv1.Condition
		status metav1.ConditionStatus
		reason string
	}{
		{"available", []xpv1.Condition{xpv1.Available()}, metav1.ConditionTrue, "Available"},
		{"creating", []xpv1.Condition{xpv1.Creating()}, metav1.ConditionFalse, "Creating"},
		{"deleting", []xpv1.Condition{xpv1.Deleting()}, metav1.ConditionFalse, "Creating"},
		{"unavailable", []xpv1.Condition{xpv1.Unavailable()}, metav1.ConditionFalse, "Creating"},
		{"missing", nil, metav1.ConditionUnknown, "ReadinessUnknown"},
		{"unknown", []xpv1.Condition{{Type: xpv1.TypeReady, Status: corev1.ConditionUnknown, Reason: "Observing"}}, metav1.ConditionUnknown, "ReadinessUnknown"},
	} {
		t.Run(test.name, func(t *testing.T) {
			parent, child := resource(), resource(test.values...)
			before := append([]xpv1.Condition(nil), child.Status.Conditions...)
			changed, err := c.Aggregate(parent, map[string]c.Object{"database": child}, fixedRules())
			if err != nil || !changed {
				t.Fatalf("aggregate: %v, %v", changed, err)
			}
			value, _ := c.Lookup(parent, "Ready")
			if value.Status != test.status || value.Reason != test.reason || value.ObservedGeneration != 7 {
				t.Fatalf("unexpected Ready: %#v", value)
			}
			out := (Readiness{}).Reduce(*CrossplaneRules.Outputs[0].DefaultBase, nil, nil, (ReadyChildren{}).Select(parent, child))
			if out.State.Escalation != c.None {
				t.Fatalf("inferred terminal failure from native reason: %#v", out)
			}
			if !reflect.DeepEqual(before, child.Status.Conditions) {
				t.Fatal("mutated child assertions")
			}
			if changed, err = c.Aggregate(parent, map[string]c.Object{"database": child}, fixedRules()); err != nil || changed {
				t.Fatalf("not idempotent: %v, %v", changed, err)
			}
		})
	}
}

func TestSyncedDoesNotInvalidateReady(t *testing.T) {
	for _, sync := range []xpv1.Condition{xpv1.ReconcileError(errors.New("update denied")), xpv1.ReconcilePaused(), xpv1.ReconcileSuccess()} {
		t.Run(string(sync.Reason), func(t *testing.T) {
			parent := resource()
			child := resource(xpv1.Available(), sync)
			_, err := c.Aggregate(parent, map[string]c.Object{"database": child}, fixedRules(), ToCondition(sync))
			if err != nil {
				t.Fatal(err)
			}
			if value, _ := c.Lookup(parent, "Ready"); value.Status != metav1.ConditionTrue {
				t.Fatalf("Synced invalidated Ready: %#v", value)
			}
			if value, _ := c.Lookup(parent, "Synced"); value.Status != metav1.ConditionStatus(sync.Status) || value.Reason != string(sync.Reason) {
				t.Fatalf("lost local Synced: %#v", value)
			}
			parentWithoutSync := resource()
			if _, err = c.Aggregate(parentWithoutSync, map[string]c.Object{"database": child}, fixedRules()); err != nil {
				t.Fatal(err)
			}
			if _, exists := c.Lookup(parentWithoutSync, "Synced"); exists {
				t.Fatal("promoted child's Synced as parent reconciliation status")
			}
		})
	}
}

func TestOrderingAndRecovery(t *testing.T) {
	parent := resource(xpv1.Condition{Type: "Custom", Status: corev1.ConditionTrue, Reason: "Retained"})
	a := resource(xpv1.Creating().WithMessage("creating database"))
	z := resource(xpv1.Unavailable().WithMessage("endpoint unavailable"))
	children := map[string]c.Object{"z": z, "a": a, "unknown": resource()}
	_, err := c.Aggregate(parent, children, fixedRules())
	if err != nil {
		t.Fatal(err)
	}
	value, _ := c.Lookup(parent, "Ready")
	if value.Status != metav1.ConditionFalse || value.Message != "Waiting for composed resources:\n  a: creating database\n  z: endpoint unavailable" {
		t.Fatalf("bad must outrank unknown with stable diagnostic order: %#v", value)
	}
	before := value.LastTransitionTime
	a.Status.SetConditions(xpv1.Available())
	z.Status.SetConditions(xpv1.Available())
	children["unknown"] = resource(xpv1.Available())
	rules := fixedRules()
	rules.Now = func() time.Time { return epoch.Add(time.Minute) }
	changed, err := c.Aggregate(parent, children, rules)
	if err != nil || !changed {
		t.Fatalf("recovery: %v, %v", changed, err)
	}
	value, _ = c.Lookup(parent, "Ready")
	if value.Status != metav1.ConditionTrue || value.Reason != "Available" || !strings.Contains(value.Message, "All composed resources are ready") || value.LastTransitionTime == before {
		t.Fatalf("failed to recover: %#v", value)
	}
	if other, _ := c.Lookup(parent, "Custom"); other.Reason != "Retained" {
		t.Fatal("lost unrelated Condition")
	}
}

func TestLocalReadinessPrecedence(t *testing.T) {
	for _, local := range []xpv1.Condition{xpv1.Deleting(), {Type: xpv1.TypeReady, Status: corev1.ConditionUnknown, Reason: "Observing", Message: "local observation incomplete"}} {
		parent := resource()
		_, err := c.Aggregate(parent, map[string]c.Object{"database": resource(xpv1.Available())}, fixedRules(), ToCondition(local))
		if err != nil {
			t.Fatal(err)
		}
		value, _ := c.Lookup(parent, "Ready")
		if value.Status != metav1.ConditionStatus(local.Status) || value.Reason != string(local.Reason) {
			t.Fatalf("child hid local readiness: %#v", value)
		}
	}
}

func TestAdapterRoundTripAndOwnership(t *testing.T) {
	native := xpv1.Available().WithMessage("All is well\n  foreign: text").WithObservedGeneration(5)
	native.LastTransitionTime = metav1.NewTime(epoch)
	object := resource(native)
	values := object.GetConditions()
	if values[0].ObservedGeneration != 5 || values[0].LastTransitionTime != native.LastTransitionTime || values[0].Message != native.Message {
		t.Fatal("lossy read adapter")
	}
	values[0].Message = "changed"
	if object.Status.Conditions[0].Message != native.Message {
		t.Fatal("getter exposed a mutable native slice")
	}
	if object.SetConditions([]metav1.Condition{ToCondition(native)}) {
		t.Fatal("reported a change for identical native Conditions")
	}
	if !reflect.DeepEqual(object.Status.Conditions, []xpv1.Condition{native}) {
		t.Fatalf("lossy write adapter: %#v", object.Status)
	}
	updated := ToCondition(native)
	updated.ObservedGeneration++
	if !object.SetConditions([]metav1.Condition{updated}) || object.Status.Conditions[0].ObservedGeneration != updated.ObservedGeneration {
		t.Fatal("missed a native generation change with unchanged status")
	}
}

func TestForeignMessagesStayOpaqueAndBounded(t *testing.T) {
	message := "failed children:\n  native-looking: do not parse\n" + strings.Repeat("more detail\n", 12)
	child := resource(xpv1.Unavailable().WithMessage(message))
	parent := resource()
	_, err := c.Aggregate(parent, map[string]c.Object{"database": child}, fixedRules())
	if err != nil {
		t.Fatal(err)
	}
	value, _ := c.Lookup(parent, "Ready")
	if value.Status != metav1.ConditionFalse || value.Reason != "Creating" || !strings.Contains(value.Message, "    native-looking: do not parse") || !strings.Contains(value.Message, "detail lines omitted") || len(strings.Split(value.Message, "\n")) != 7 {
		t.Fatalf("invalid opaque rendering: %#v", value)
	}
}

func TestReservedHealthyIsNotAnInput(t *testing.T) {
	parent := resource()
	child := resource(xpv1.Available(), xpv1.Condition{Type: xpv1.TypeHealthy, Status: corev1.ConditionFalse, Reason: "Reserved"})
	_, err := c.Aggregate(parent, map[string]c.Object{"database": child}, fixedRules())
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := c.Lookup(parent, "Ready"); value.Status != metav1.ConditionTrue {
		t.Fatal("used reserved Healthy as implemented readiness")
	}
}

func Example() {
	parent := resource()
	child := resource(xpv1.Creating().WithMessage("waiting for database"))
	_, err := c.Aggregate(parent, map[string]c.Object{"database": child}, CrossplaneRules,
		ToCondition(xpv1.ReconcileSuccess()))
	if err != nil {
		panic(err)
	}
	for _, typ := range []string{"Ready", "Synced"} {
		value, _ := c.Lookup(parent, typ)
		fmt.Println(value.Type, value.Status, value.Reason)
	}
	// Output:
	// Ready False Creating
	// Synced True ReconcileSuccess
}
