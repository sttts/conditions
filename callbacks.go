// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package conditions

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// ClassifierFunc lets a Go function interpret bad-condition reasons.
type ClassifierFunc func(metav1.Condition) Escalation

func (f ClassifierFunc) Classify(c metav1.Condition) Escalation { return f(c) }

// ChildSelectorFunc lets a Go function select contextual child contributions.
type ChildSelectorFunc func(parent, child Object) []Contribution

func (f ChildSelectorFunc) Select(parent, child Object) []Contribution { return f(parent, child) }

// LocalSelectorFunc lets a Go function select independent local checks.
type LocalSelectorFunc func(Object) []Contribution

func (f LocalSelectorFunc) Select(parent Object) []Contribution { return f(parent) }

// ReducerFunc lets a Go function combine contributions without defining a new type.
type ReducerFunc func(base metav1.Condition, children map[string]Object, local, selected []Contribution) Outcome

func (f ReducerFunc) Reduce(base metav1.Condition, children map[string]Object, local, selected []Contribution) Outcome {
	return f(base, children, local, selected)
}

// FormatterFunc renders messages with an ordinary Go function.
type FormatterFunc func(summary string, causes []Cause) string

func (f FormatterFunc) Format(summary string, causes []Cause) string { return f(summary, causes) }

// InlineFormatterFunc supplies a short presentation independently of aggregation.
type InlineFormatterFunc func(metav1.Condition) string

func (f InlineFormatterFunc) Inline(condition metav1.Condition) string { return f(condition) }
