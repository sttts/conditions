// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package infra

import c "github.com/sttts/conditions"

// These aliases retain the reference example's vocabulary while all mechanisms live in the root package.
type (
	Polarity          = c.Polarity
	Assessment        = c.Assessment
	Escalation        = c.Escalation
	State             = c.State
	Getter            = c.Getter
	Object            = c.Object
	Setter            = c.Setter
	Conditioned       = c.Conditioned
	Derivation        = c.Derivation
	Rules             = c.Rules
	ConditionType     = c.ConditionType
	Classifier        = c.Classifier
	Propagation       = c.Propagation
	ChildSelector     = c.ChildSelector
	LocalSelector     = c.LocalSelector
	ParentPolicy      = c.ParentPolicy
	Reducer           = c.Reducer
	Formatter         = c.Formatter
	Presenter         = c.Presenter
	Cause             = c.Cause
	Contribution      = c.Contribution
	Outcome           = c.Outcome
	Aggregation       = c.Aggregation
	ReasonSuffix      = c.ReasonSuffix
	ReasonClassifier  = c.ReasonClassifier
	Always            = c.Always
	Never             = c.Never
	BeforeInitialized = c.BeforeInitialized
	ExistingChildren  = c.ExistingChildren
	LocalBaseFirst    = c.LocalBaseFirst
	LocalOnly         = c.LocalOnly
	PrefixedChecks    = c.PrefixedChecks
	BoundedMessages   = c.BoundedMessages
	MultilineMessages = c.MultilineMessages
	GenerationPolicy  = c.GenerationPolicy
)

const (
	TrueIsGood             = c.TrueIsGood
	FalseIsGood            = c.FalseIsGood
	Good                   = c.Good
	Bad                    = c.Bad
	Unknown                = c.Unknown
	None                   = c.None
	WaitingForHuman        = c.WaitingForHuman
	Failed                 = c.Failed
	ParentGeneration       = c.ParentGeneration
	PreserveBaseGeneration = c.PreserveBaseGeneration
	NoObservedGeneration   = c.NoObservedGeneration
)

var (
	Aggregate = c.Aggregate
	Evaluate  = c.Evaluate
	Lookup    = c.Lookup
)
