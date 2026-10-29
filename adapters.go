// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package conditions

import (
	"slices"

	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PlainSetter combines object access with a setter that does not report changes.
type PlainSetter interface {
	Object
	SetConditions([]metav1.Condition)
}

// WithChangeDetection adapts a plain setter to the native change-reporting contract.
func WithChangeDetection(object PlainSetter) Conditioned {
	if isNilObject(object) {
		return nil
	}
	return changeTracking{PlainSetter: object}
}

type changeTracking struct{ PlainSetter }

func (o changeTracking) SetConditions(values []metav1.Condition) bool {
	before := slices.Clone(o.GetConditions())
	o.PlainSetter.SetConditions(values)
	return !equality.Semantic.DeepEqual(before, o.GetConditions())
}
