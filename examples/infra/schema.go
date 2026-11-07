// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package infra

import (
	c "github.com/sttts/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// InputSchema is the shared, global project vocabulary, including job-like resources.
var InputSchema = c.Schema{Conditions: map[string]c.Spec{
	"Available":   {Polarity: TrueIsGood, Reasons: InfraClassification},
	"Initialized": {Polarity: TrueIsGood},
	"NonDegraded": {Polarity: TrueIsGood, Reasons: InfraClassification},
	"UpToDate":    {Polarity: TrueIsGood, Reasons: InfraClassification},
	"Synced":      {Polarity: TrueIsGood, Reasons: InfraClassification},
	"Deleting":    {Polarity: FalseIsGood, Reasons: InfraClassification},
	"Running":     {Polarity: FalseIsGood, Reasons: InfraClassification},
	"Succeeded":   {Polarity: TrueIsGood, Reasons: InfraClassification},
	"Failed":      {Polarity: FalseIsGood, Reasons: InfraClassification},
}}

// Domain-specific selection stays ordinary Go. The output owns its contract and
// lifecycle logic; no synthetic identity mapping is needed for a multi-axis selector.
var AvailabilityOutput = c.OutputDefinition{
	Name: "Available", Spec: InputSchema.Conditions["Available"],
	Derived:           []c.Derivation{InfraInitialization{}},
	DerivedConditions: map[string]c.Spec{"Initialized": InputSchema.Conditions["Initialized"]},
	Parent:            LocalBaseFirst{Base: InfraConditions.Available, Classifier: InfraClassification, OverrideOnDeletion: true},
	LocalChecks:       PrefixedChecks{Prefix: "Available", Polarity: TrueIsGood, Classifier: InfraClassification},
	Children: ServingChildren{Inputs: InfraConditions, Condition: InfraConditions.Available, Classifier: InfraClassification,
		False: existingChildren, Failure: existingChildren, Human: Never{}, Unknown: beforeInitialization,
		SyncError: beforeInitialization, JobRunning: beforeInitialization, JobFailed: beforeInitialization,
		BadDescription: "is not available"},
	Reduction: ParentChildReduction{Inputs: InfraConditions, Output: InfraConditions.Available, Classifier: InfraClassification, BadSuffix: "NotAvailable", BadDescription: "unavailable children"},
	Format:    infraMessages, Generation: PreserveBaseGeneration,
}
var NonDegradedOutput = c.OutputDefinition{
	Name: "NonDegraded", Spec: InputSchema.Conditions["NonDegraded"], Requires: []string{"Available", "Initialized"},
	Parent:      DegradationBase{LocalBaseFirst: LocalBaseFirst{Base: InfraConditions.NonDegraded, Classifier: InfraClassification, OverrideOnDeletion: true}, Inputs: InfraConditions},
	LocalChecks: PrefixedChecks{Prefix: "NonDegraded", Polarity: TrueIsGood, Classifier: InfraClassification},
	Children: ServingChildren{Inputs: InfraConditions, Condition: InfraConditions.NonDegraded, Classifier: InfraClassification,
		False: Always{}, Failure: Always{}, Human: Never{}, SyncError: Always{}, Unavailable: Always{}, JobFailed: Always{}, BadDescription: "is degraded"},
	Reduction: ParentChildReduction{Inputs: InfraConditions, Output: InfraConditions.NonDegraded, Classifier: InfraClassification, BadSuffix: "Degraded", BadDescription: "degraded children"},
	Format:    infraMessages, Generation: PreserveBaseGeneration,
}
var UpToDateOutput = c.OutputDefinition{
	Name: "UpToDate", Spec: InputSchema.Conditions["UpToDate"], Requires: []string{"Initialized"},
	Parent:      UpdateBase{LocalBaseFirst: LocalBaseFirst{Base: InfraConditions.UpToDate, Classifier: InfraClassification}, Initialized: InfraConditions.Initialized},
	LocalChecks: PrefixedChecks{Prefix: "UpToDate", Polarity: TrueIsGood, Classifier: InfraClassification},
	Children: ServingChildren{Inputs: InfraConditions, Condition: InfraConditions.UpToDate, Classifier: InfraClassification,
		False: Always{}, Failure: Never{}, Human: Always{}, SyncError: Always{}, Uninitialized: Always{}, JobRunning: Always{}, JobFailed: Always{}, BadDescription: "is updating"},
	Reduction: ParentChildReduction{Inputs: InfraConditions, Output: InfraConditions.UpToDate, Classifier: InfraClassification, BadSuffix: "Updating", BadDescription: "updating children"},
	Format:    infraMessages, Generation: PreserveBaseGeneration,
}
var SyncedOutput = c.OutputDefinition{
	Name: "Synced", Spec: InputSchema.Conditions["Synced"], LocalOnly: true,
	Parent:      LocalOnly{LocalBaseFirst: LocalBaseFirst{Base: InfraConditions.Synced, Classifier: InfraClassification}},
	LocalChecks: PrefixedChecks{Prefix: "Synced", Polarity: TrueIsGood, Classifier: InfraClassification},
	Reduction:   ParentChildReduction{Inputs: InfraConditions, Output: InfraConditions.Synced, Classifier: InfraClassification, LocalOnly: true},
	Format:      infraMessages, Generation: PreserveBaseGeneration,
}
var DeletingOutput = c.OutputDefinition{
	Name: "Deleting", Spec: InputSchema.Conditions["Deleting"],
	DefaultBase: &metav1.Condition{Status: metav1.ConditionTrue, Reason: "Deleting", Message: "resource is being deleted"},
	Parent:      DeletionBase{Classifier: InfraClassification, Output: InfraConditions.Deleting},
	Children:    DeletingChildren{Inputs: InfraConditions, Classifier: InfraClassification},
	Reduction:   DeletionReduction{Inputs: InfraConditions, Classifier: InfraClassification},
	Format:      infraMessages, Generation: PreserveBaseGeneration,
}

var Aggregation = c.Define(c.Definition{
	Outputs:  []c.OutputDefinition{AvailabilityOutput, NonDegradedOutput, UpToDateOutput, SyncedOutput, DeletingOutput},
	Children: []c.ChildBinding{c.Child(InputSchema, c.Identity)},
})
var Schema = Aggregation.Schema()
