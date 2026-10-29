// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package openshift

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	c "github.com/sttts/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var epoch = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func assertion(typ string, status operatorv1.ConditionStatus, reason, message string, age time.Duration) operatorv1.OperatorCondition {
	return operatorv1.OperatorCondition{Type: typ, Status: status, Reason: reason, Message: message,
		LastTransitionTime: metav1.NewTime(epoch.Add(-age))}
}

func source(values ...operatorv1.OperatorCondition) Operator {
	operator := &operatorv1.Console{ObjectMeta: metav1.ObjectMeta{Name: "cluster", Generation: 17}}
	operator.Status.Conditions = values
	operator.Status.ObservedGeneration = 16
	return Operator{Object: operator, Status: &operator.Status.OperatorStatus}
}

// TestUnion checks the same truth table for the two positive and two negative operator flags.
func TestUnion(t *testing.T) {
	for _, typ := range []c.ConditionType{available, progressing, degraded, upgradeable} {
		t.Run(typ.Type, func(t *testing.T) {
			good, bad := operatorv1.ConditionTrue, operatorv1.ConditionFalse
			if typ.Polarity == c.FalseIsGood {
				good, bad = bad, good
			}
			cases := []struct {
				name            string
				values          []operatorv1.OperatorCondition
				assessment      c.Assessment
				reason, message string
				age             time.Duration
			}{
				{name: "no data", assessment: c.Unknown, reason: "NoData"},
				{name: "unrelated suffix", values: []operatorv1.OperatorCondition{assertion("Other", bad, "Ignored", "ignored", time.Hour)}, assessment: c.Unknown, reason: "NoData"},
				{name: "good", values: []operatorv1.OperatorCondition{assertion("A"+typ.Type, good, "Fine", "", time.Hour)}, assessment: c.Good, reason: "AsExpected", message: "All is well", age: time.Hour},
				{name: "good preserves input message order", values: []operatorv1.OperatorCondition{assertion("Z"+typ.Type, good, "", "z", time.Hour), assertion("A"+typ.Type, good, "", "a", time.Minute)}, assessment: c.Good, reason: "AsExpected", message: "Z" + typ.Type + ": z\nA" + typ.Type + ": a", age: time.Minute},
				{name: "unknown", values: []operatorv1.OperatorCondition{assertion("A"+typ.Type, operatorv1.ConditionUnknown, "Pending", "pending", time.Minute)}, assessment: c.Unknown, reason: "A_Pending", message: "A" + typ.Type + ": pending", age: time.Minute},
				{name: "opposite beats unknown", values: []operatorv1.OperatorCondition{assertion("Z"+typ.Type, operatorv1.ConditionUnknown, "Pending", "pending", time.Minute), assertion("A"+typ.Type, bad, "Broken", "broken", time.Hour)}, assessment: c.Bad, reason: "A_Broken::Z_Pending", message: "A" + typ.Type + ": broken\nZ" + typ.Type + ": pending", age: time.Minute},
				{name: "good does not hide bad", values: []operatorv1.OperatorCondition{assertion("Z"+typ.Type, good, "Fine", "fine", time.Second), assertion("A"+typ.Type, bad, "Broken", "broken", time.Hour)}, assessment: c.Bad, reason: "A_Broken", message: "A" + typ.Type + ": broken", age: time.Hour},
				{name: "sort and deduplicate per assertion", values: []operatorv1.OperatorCondition{assertion("Z"+typ.Type, bad, "Z", "same\nsame\nlast", time.Minute), assertion("A"+typ.Type, bad, "A", "same", time.Hour)}, assessment: c.Bad, reason: "A_A::Z_Z", message: "A" + typ.Type + ": same\nZ" + typ.Type + ": same\nZ" + typ.Type + ": last", age: time.Minute},
				{name: "exact type matches", values: []operatorv1.OperatorCondition{assertion(typ.Type, bad, "Blocked", "", time.Hour)}, assessment: c.Bad, reason: "_Blocked", age: time.Hour},
				{name: "no reason or message", values: []operatorv1.OperatorCondition{assertion("A"+typ.Type, bad, "", "", time.Hour)}, assessment: c.Bad, reason: "A", age: time.Hour},
			}
			for _, test := range cases {
				t.Run(test.name, func(t *testing.T) {
					operator := source(test.values...)
					before := append([]operatorv1.OperatorCondition(nil), operator.Status.Conditions...)
					selected := (OperatorConditions{typ}).Select(nil, operator)
					out := (OperatorUnion{Condition: typ}).Reduce(metav1.Condition{}, nil, nil, selected)
					transition := metav1.Time{}
					if test.age != 0 {
						transition = metav1.NewTime(epoch.Add(-test.age))
					}
					want := c.Outcome{State: c.State{Assessment: test.assessment}, Reason: test.reason, Summary: test.message, LastTransitionTime: transition}
					if !reflect.DeepEqual(out, want) {
						t.Fatalf("got %#v, want %#v", out, want)
					}
					if !reflect.DeepEqual(before, operator.Status.Conditions) {
						t.Fatal("mutated native input")
					}
				})
			}
		})
	}
}

func TestInertia(t *testing.T) {
	for _, test := range []struct {
		name   string
		ages   []time.Duration
		want   c.Assessment
		reason string
	}{
		{"young", []time.Duration{time.Second}, c.Good, "AsExpected"},
		{"exact threshold remains suppressed", []time.Duration{time.Minute}, c.Good, "AsExpected"},
		{"old", []time.Duration{time.Minute + time.Second}, c.Bad, "A_Blocked"},
		{"one old includes young", []time.Duration{time.Hour, time.Second}, c.Bad, "A_Blocked::B_Blocked"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var values []operatorv1.OperatorCondition
			for i, age := range test.ages {
				values = append(values, assertion(fmt.Sprintf("%cDegraded", 'A'+i), operatorv1.ConditionTrue, "Blocked", "detail", age))
			}
			selected := (OperatorConditions{degraded}).Select(nil, source(values...))
			out := (OperatorUnion{Condition: degraded, Inertia: func(metav1.Condition) time.Duration { return time.Minute }, Now: func() time.Time { return epoch }}).Reduce(metav1.Condition{}, nil, nil, selected)
			if out.State.Assessment != test.want || out.Reason != test.reason {
				t.Fatalf("unexpected union: %#v", out)
			}
		})
	}
}

func TestOperatorProjectionAndRecovery(t *testing.T) {
	operator := source(
		assertion("ConsoleAvailable", operatorv1.ConditionTrue, "AsExpected", "", time.Hour),
		assertion("ConsoleProgressing", operatorv1.ConditionFalse, "AsExpected", "", time.Hour),
		assertion("ConfigDegraded", operatorv1.ConditionTrue, "Invalid", "configuration is invalid", time.Minute),
		assertion("RouteDegraded", operatorv1.ConditionTrue, "Blocked", "route update denied", time.Second),
		assertion("ConfigUpgradeable", operatorv1.ConditionFalse, "Unsupported", "unsupported operator configuration", time.Hour))
	cluster := &configv1.ClusterOperator{ObjectMeta: metav1.ObjectMeta{Name: "console", Generation: 29}}
	cluster.Status.Versions = []configv1.OperandVersion{{Name: "operator", Version: "1.2.3"}}
	cluster.Status.Conditions = []configv1.ClusterOperatorStatusCondition{{Type: "Custom", Status: configv1.ConditionTrue, Reason: "Retained"}}
	parent := ClusterOperator{cluster}
	children := map[string]c.Object{"Operator": operator}
	rules := OpenShiftRules
	rules.Now = func() time.Time { return epoch }
	changed, err := c.Aggregate(parent, children, rules)
	if err != nil || !changed {
		t.Fatalf("aggregate: %v, %v", changed, err)
	}
	for typ, status := range map[string]metav1.ConditionStatus{"Available": metav1.ConditionTrue, "Progressing": metav1.ConditionFalse, "Degraded": metav1.ConditionTrue, "Upgradeable": metav1.ConditionFalse} {
		value, _ := c.Lookup(parent, typ)
		if value.Status != status || value.ObservedGeneration != 0 {
			t.Fatalf("%s: %#v", typ, value)
		}
	}
	value, _ := c.Lookup(parent, "Degraded")
	if value.Reason != "Config_Invalid::Route_Blocked" || value.Message != "ConfigDegraded: configuration is invalid\nRouteDegraded: route update denied" {
		t.Fatalf("lost controller union: %#v", value)
	}
	if value.LastTransitionTime != metav1.NewTime(epoch.Add(-time.Second)) {
		t.Fatal("lost union transition")
	}
	if operator.Status.ObservedGeneration != 16 || len(cluster.Status.Versions) != 1 {
		t.Fatal("adapter changed other status fields")
	}
	if custom, _ := c.Lookup(parent, "Custom"); custom.Reason != "Retained" {
		t.Fatal("unowned Condition changed")
	}
	if changed, err = c.Aggregate(parent, children, rules); err != nil || changed {
		t.Fatalf("not idempotent with native generation-less schema: %v, %v", changed, err)
	}

	// Replacement assertions recover automatically; Degraded is never made a terminal Failed state.
	for i := range operator.Status.Conditions {
		if operator.Status.Conditions[i].Type == "ConfigDegraded" || operator.Status.Conditions[i].Type == "RouteDegraded" {
			operator.Status.Conditions[i].Status = operatorv1.ConditionFalse
			operator.Status.Conditions[i].LastTransitionTime = metav1.NewTime(epoch)
		}
	}
	if changed, err = c.Aggregate(parent, children, rules); err != nil || !changed {
		t.Fatalf("recovery: %v, %v", changed, err)
	}
	value, _ = c.Lookup(parent, "Degraded")
	if value.Status != metav1.ConditionFalse || value.Reason != "AsExpected" || value.LastTransitionTime != metav1.NewTime(epoch) {
		t.Fatalf("failed recovery: %#v", value)
	}
	operator.Status.Conditions[2].Message = "new diagnostic only"
	operator.Status.Conditions[2].LastTransitionTime = metav1.NewTime(epoch.Add(time.Hour))
	if _, err = c.Aggregate(parent, children, rules); err != nil {
		t.Fatal(err)
	}
	value, _ = c.Lookup(parent, "Degraded")
	if value.LastTransitionTime != metav1.NewTime(epoch) {
		t.Fatal("message update changed output transition time")
	}
}

func TestSetterReportsNativeChanges(t *testing.T) {
	parent := ClusterOperator{&configv1.ClusterOperator{}}
	initial := []metav1.Condition{{Type: "Available", Status: metav1.ConditionTrue, Reason: "AsExpected"}}
	if !parent.SetConditions(initial) {
		t.Fatal("did not report adding a native Condition")
	}
	initial[0].ObservedGeneration = 42
	if parent.SetConditions(initial) {
		t.Fatal("reported a change for a field absent from the native schema")
	}
	initial[0].Message = "updated diagnostic"
	if !parent.SetConditions(initial) || parent.Status.Conditions[0].Message != "updated diagnostic" {
		t.Fatal("missed a native message change with unchanged status")
	}
}

// componentObject deliberately resembles an operand but is not native operator status.
type componentObject struct{ metav1.ObjectMeta }

func (componentObject) GetConditions() []metav1.Condition {
	return []metav1.Condition{{Type: "Available", Status: metav1.ConditionFalse}}
}

func TestOperandConditionsDoNotDriveOperatorStatus(t *testing.T) {
	parent := ClusterOperator{&configv1.ClusterOperator{}}
	_, err := c.Aggregate(parent, map[string]c.Object{"operand": &componentObject{}}, OpenShiftRules)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range parent.GetConditions() {
		if value.Status != metav1.ConditionUnknown || value.Reason != "NoData" {
			t.Fatalf("operand drove operator status: %#v", value)
		}
	}
}

func Example() {
	operator := &operatorv1.Console{ObjectMeta: metav1.ObjectMeta{Name: "cluster"}}
	operator.Status.Conditions = []operatorv1.OperatorCondition{
		{Type: "ConfigDegraded", Status: operatorv1.ConditionTrue, Reason: "Invalid", Message: "invalid operator configuration"},
		{Type: "RouteDegraded", Status: operatorv1.ConditionTrue, Reason: "UpdateDenied", Message: "route update denied"},
	}
	cluster := &configv1.ClusterOperator{ObjectMeta: metav1.ObjectMeta{Name: "console"}}
	_, err := c.Aggregate(ClusterOperator{cluster}, map[string]c.Object{
		"Operator": Operator{Object: operator, Status: &operator.Status.OperatorStatus},
	}, OpenShiftRules)
	if err != nil {
		panic(err)
	}
	value, _ := c.Lookup(ClusterOperator{cluster}, "Degraded")
	fmt.Println(value.Status, value.Reason)
	fmt.Println(value.Message)
	// Output:
	// True Config_Invalid::Route_UpdateDenied
	// ConfigDegraded: invalid operator configuration
	// RouteDegraded: route update denied
}
