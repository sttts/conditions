// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package infra

import (
	ref "github.com/sttts/conditions/internal/testreference"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Rule = ref.Rule
type Rules = ref.Rules

var Evaluate = ref.Evaluate
var ApplyRules = ref.ApplyRules

// Each flat value composes configured implementations of the domain interfaces.
var InfraAvailability = Rule{
	Derived: []Derivation{InfraInitialization{}},
	Output:  InfraConditions.Available,
	Parent:  LocalBaseFirst{Base: InfraConditions.Available, Classifier: InfraClassification, OverrideOnDeletion: true},
	Local:   PrefixedChecks{Prefix: "Available", Polarity: TrueIsGood, Classifier: InfraClassification},
	Children: ServingChildren{Inputs: InfraConditions, Condition: InfraConditions.Available, Classifier: InfraClassification,
		False: existingChildren, Failure: existingChildren, Human: Never{}, Unknown: beforeInitialization,
		SyncError: beforeInitialization, JobRunning: beforeInitialization, JobFailed: beforeInitialization,
		BadDescription: "is not available"},
	Reduction: ParentChildReduction{Inputs: InfraConditions, Output: InfraConditions.Available, Classifier: InfraClassification, BadSuffix: "NotAvailable", BadDescription: "unavailable children"},
	Formatter: infraMessages, Generation: PreserveBaseGeneration,
}
var InfraNonDegraded = Rule{
	Requires: []string{"Available", "Initialized"},
	Output:   InfraConditions.NonDegraded,
	Parent:   DegradationBase{LocalBaseFirst: LocalBaseFirst{Base: InfraConditions.NonDegraded, Classifier: InfraClassification, OverrideOnDeletion: true}, Inputs: InfraConditions},
	Local:    PrefixedChecks{Prefix: "NonDegraded", Polarity: TrueIsGood, Classifier: InfraClassification},
	Children: ServingChildren{Inputs: InfraConditions, Condition: InfraConditions.NonDegraded, Classifier: InfraClassification,
		False: Always{}, Failure: Always{}, Human: Never{}, SyncError: Always{}, Unavailable: Always{}, JobFailed: Always{}, BadDescription: "is degraded"},
	Reduction: ParentChildReduction{Inputs: InfraConditions, Output: InfraConditions.NonDegraded, Classifier: InfraClassification, BadSuffix: "Degraded", BadDescription: "degraded children"},
	Formatter: infraMessages, Generation: PreserveBaseGeneration,
}
var InfraUpToDate = Rule{
	Requires: []string{"Initialized"},
	Output:   InfraConditions.UpToDate,
	Parent:   UpdateBase{LocalBaseFirst: LocalBaseFirst{Base: InfraConditions.UpToDate, Classifier: InfraClassification}, Initialized: InfraConditions.Initialized},
	Local:    PrefixedChecks{Prefix: "UpToDate", Polarity: TrueIsGood, Classifier: InfraClassification},
	Children: ServingChildren{Inputs: InfraConditions, Condition: InfraConditions.UpToDate, Classifier: InfraClassification,
		False: Always{}, Failure: Never{}, Human: Always{}, SyncError: Always{}, Uninitialized: Always{}, JobRunning: Always{}, JobFailed: Always{}, BadDescription: "is updating"},
	Reduction: ParentChildReduction{Inputs: InfraConditions, Output: InfraConditions.UpToDate, Classifier: InfraClassification, BadSuffix: "Updating", BadDescription: "updating children"},
	Formatter: infraMessages, Generation: PreserveBaseGeneration,
}
var InfraSynced = Rule{
	Output:    InfraConditions.Synced,
	Parent:    LocalOnly{LocalBaseFirst: LocalBaseFirst{Base: InfraConditions.Synced, Classifier: InfraClassification}},
	Local:     PrefixedChecks{Prefix: "Synced", Polarity: TrueIsGood, Classifier: InfraClassification},
	Reduction: ParentChildReduction{Inputs: InfraConditions, Output: InfraConditions.Synced, Classifier: InfraClassification, LocalOnly: true},
	Formatter: infraMessages, Generation: PreserveBaseGeneration,
}
var InfraDeleting = Rule{
	DefaultBase: &metav1.Condition{Type: "Deleting", Status: metav1.ConditionTrue, Reason: "Deleting", Message: "resource is being deleted"},
	Output:      InfraConditions.Deleting,
	Parent:      DeletionBase{Classifier: InfraClassification, Output: InfraConditions.Deleting},
	Children:    DeletingChildren{Inputs: InfraConditions, Classifier: InfraClassification},
	Reduction:   DeletionReduction{Inputs: InfraConditions, Classifier: InfraClassification},
	Formatter:   infraMessages, Generation: PreserveBaseGeneration,
}

// InfraRules selects the complete Infra behavior. Individual values remain independently selectable.
var InfraRules = Rules{
	Outputs: []Rule{InfraAvailability, InfraNonDegraded, InfraUpToDate, InfraSynced, InfraDeleting},
}

// InfraProfile optionally binds rules for callers that prefer a short application-specific facade.
type InfraProfile struct{ Rules Rules }

// Infra injects InfraRules; the facade has no evaluation or sequencing implementation of its own.
var Infra = InfraProfile{Rules: InfraRules}

// LocalChecks selects Infra's local-only prefix policy for exactly one named output.
// Additional local types require explicit selection; they do not change other packages' rules.
func legacyLocalChecks(typ string) Rule {
	output := ConditionType{Type: typ, Polarity: TrueIsGood}
	return Rule{
		Output:     output,
		Parent:     LocalOnly{LocalBaseFirst: LocalBaseFirst{Base: output, Classifier: InfraClassification}},
		Local:      PrefixedChecks{Prefix: typ, Polarity: TrueIsGood, Classifier: InfraClassification},
		Reduction:  ParentChildReduction{Output: output, Classifier: InfraClassification, LocalOnly: true},
		Formatter:  infraMessages,
		Generation: PreserveBaseGeneration,
	}
}

// ApplyRules only injects the facade's rules into the generic resource-level operation.
func (p InfraProfile) ApplyRules(parent Conditioned, children map[string]Object, local ...metav1.Condition) (bool, error) {
	return ApplyRules(parent, children, p.Rules, local...)
}

// Evaluate only injects the facade's rules into generic pure evaluation.
func (p InfraProfile) Evaluate(parent Object, children map[string]Object, local ...metav1.Condition) ([]metav1.Condition, error) {
	return Evaluate(parent, children, p.Rules, local...)
}
