// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package conditions_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	c "github.com/sttts/conditions"
	"github.com/sttts/conditions/internal/testproviders/dependent"
	"github.com/sttts/conditions/internal/testproviders/local"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestProvidersFromSeparatePackages(t *testing.T) {
	// Neither provider imports the other or needs changes/registration in the core.
	rules, err := c.Compose(dependent.Rules(), local.Rules())
	if err != nil {
		t.Fatal(err)
	}
	clockCalls := 0
	rules.Now = func() time.Time { clockCalls++; return time.Unix(100, 0) }
	parent := &object{ObjectMeta: metav1.ObjectMeta{Generation: 7}}
	for _, status := range []metav1.ConditionStatus{metav1.ConditionFalse, metav1.ConditionTrue} {
		base := metav1.Condition{Type: "Ready", Status: status, Reason: "LocalObservation", Message: "detail"}
		if changed, err := c.Aggregate(parent, nil, rules, base); err != nil || !changed {
			t.Fatalf("change: %v %v", changed, err)
		}
		for _, name := range []string{"Ready", "Observed"} {
			value, _ := c.Lookup(parent, name)
			if value.Status != status || value.Reason != base.Reason || value.ObservedGeneration != 7 {
				t.Fatalf("%s lost provider result: %#v", name, value)
			}
		}
		before := append([]metav1.Condition(nil), parent.GetConditions()...)
		if changed, err := c.Aggregate(parent, nil, rules, base); err != nil || changed {
			t.Fatalf("repeat: %v %v", changed, err)
		}
		if !reflect.DeepEqual(before, parent.GetConditions()) {
			t.Fatal("repeat changed metadata")
		}
	}
	if clockCalls != 4 {
		t.Fatalf("clock sampled %d times, want once per evaluation", clockCalls)
	}
}

func TestCompositionValidation(t *testing.T) {
	good := rule(c.Outcome{State: c.State{Assessment: c.Good}, Reason: "Ready"})
	for _, tc := range []struct {
		name    string
		outputs []c.Aggregation
		want    string
	}{
		{"duplicate", []c.Aggregation{good, good}, "duplicate output"},
		{"missing dependency", dependent.Rules(), "requires missing output"},
		{"incomplete", []c.Aggregation{{Output: c.ConditionType{Type: "Bad"}}}, "incomplete aggregation"},
		{"mismatched base", []c.Aggregation{func() c.Aggregation { r := good; r.DefaultBase = &metav1.Condition{Type: "Other"}; return r }()}, "does not match"},
		{"self cycle", []c.Aggregation{func() c.Aggregation { r := good; r.Requires = []string{"Ready"}; return r }()}, "dependency cycle"},
		{"mutual cycle", func() []c.Aggregation {
			a, b := good, good
			a.Requires = []string{"Other"}
			b.Output.Type = "Other"
			b.DefaultBase = &metav1.Condition{Type: "Other"}
			b.Requires = []string{"Ready"}
			return []c.Aggregation{a, b}
		}(), "dependency cycle"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := c.Compose(tc.outputs); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Compose: %v, want %q", err, tc.want)
			}
			parent := &object{}
			if _, err := c.Aggregate(parent, nil, c.Rules{Outputs: tc.outputs}); err == nil || parent.sets != 0 {
				t.Fatalf("direct Rules bypassed validation: err=%v sets=%d", err, parent.sets)
			}
		})
	}
}
