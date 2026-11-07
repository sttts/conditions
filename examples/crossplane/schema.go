// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package crossplane

import (
	c "github.com/sttts/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"time"
)

// NativeSchema is global: Crossplane providers use independent API groups.
// Consumers can restrict an individual Child binding with For(G/GV/GK/GVK(...)).
var NativeSchema = c.Schema{Conditions: map[string]c.Spec{
	"Ready":  {Polarity: c.TrueIsGood, Missing: &metav1.Condition{Reason: "ConditionMissing", Message: "Ready condition has not been reported"}},
	"Synced": {Polarity: c.TrueIsGood},
}}

// Aggregation preserves Crossplane's native reasons and opaque diagnostics.
// Synced is a local assertion and never promoted from composed resources.
var Aggregation = defineAggregation()
var Schema = Aggregation.Schema()

var ReadyOutput = c.OutputDefinition{
	Name: "Ready", Spec: NativeSchema.Conditions["Ready"],
	DefaultBase: &metav1.Condition{Status: metav1.ConditionTrue, Reason: "Available"},
	Reduction:   Readiness{}, Formatter: c.BoundedMessages{MaxLines: 7},
}

func defineAggregation(clocks ...func() time.Time) *c.Aggregation {
	d := c.Definition{
		Outputs:  []c.OutputDefinition{ReadyOutput, c.Output("Synced", NativeSchema.Conditions["Synced"]).Local()},
		Children: []c.ChildBinding{c.Child(NativeSchema, c.Identity)},
	}
	if len(clocks) > 0 {
		d.Now = clocks[0]
	}
	return c.Define(d)
}
