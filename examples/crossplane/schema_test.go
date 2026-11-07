// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package crossplane

import (
	c "github.com/sttts/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"reflect"
	"sort"
	"testing"
)

func applyDefined(t *testing.T, parent c.Conditioned, children map[string]c.Object, rules c.Rules, local ...metav1.Condition) (bool, error) {
	t.Helper()
	want, err := c.Evaluate(parent, children, rules, local...)
	if err != nil {
		return false, err
	}
	changed := c.Aggregate(parent, children, defineAggregation(rules.Now), local...)
	got := parent.GetConditions()
	sort.Slice(got, func(i, j int) bool { return got[i].Type < got[j].Type })
	sort.Slice(want, func(i, j int) bool { return want[i].Type < want[j].Type })
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Define changed native result:\ngot  %#v\nwant %#v", parent.GetConditions(), want)
	}
	return changed, nil
}
