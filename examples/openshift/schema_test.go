// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package openshift

import (
	c "github.com/sttts/conditions"
	ref "github.com/sttts/conditions/internal/testreference"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"reflect"
	"testing"
	"time"
)

func applyDefined(t *testing.T, parent c.Conditioned, children map[string]c.Object, rules ref.Rules, local ...metav1.Condition) (bool, error) {
	t.Helper()
	if rules.Now == nil {
		rules.Now = func() time.Time { return epoch }
	}
	want, err := ref.Evaluate(parent, children, rules, local...)
	if err != nil {
		return false, err
	}
	d := c.Definition{Resources: Schema.Resources, Now: rules.Now, Children: []c.ChildBinding{c.Child(OperatorSchema, c.Identity)}}
	for _, rule := range rules.Outputs {
		output := unionOutput(rule.Output.Type, Schema.Conditions[rule.Output.Type])
		output.Reduction = rule.Reduction // Preserve per-test inertia/clock choices.
		d.Outputs = append(d.Outputs, output)
	}
	changed := c.Aggregate(parent, children, c.Define(d), local...)
	if !reflect.DeepEqual(parent.GetConditions(), want) {
		t.Fatalf("Define changed native union:\ngot  %#v\nwant %#v", parent.GetConditions(), want)
	}
	return changed, nil
}
