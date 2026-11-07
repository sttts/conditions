// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package conditions_test

import (
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	c "github.com/sttts/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func contract(name string, polarity c.Polarity) c.Schema {
	return c.Schema{Conditions: map[string]c.Spec{name: {Polarity: polarity}}}
}
func observation(name string, status metav1.ConditionStatus, reason string) *object {
	return &object{conditions: []metav1.Condition{{Type: name, Status: status, Reason: reason, Message: reason}}}
}
func scopeObject(group, version, kind string, obj c.Object) c.Object {
	return c.WithGVK(obj, schema.GroupVersionKind{Group: group, Version: version, Kind: kind})
}
func assertPanic(t *testing.T, contains string, f func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Errorf("expected panic containing %q", contains)
		} else if !strings.Contains(r.(string), contains) {
			t.Errorf("panic %v, want %q", r, contains)
		}
	}()
	f()
}

func TestSchemaTranslationAndPublication(t *testing.T) {
	foreign := contract("Available", c.TrueIsGood)
	middle := define(contract("Faulted", c.FalseIsGood), c.Child(foreign, c.Classify(
		c.Map("Available", "Faulted").Escalate(c.WaitingForHuman, c.Reason("InvalidConfiguration")),
	)))
	top := define(contract("Healthy", c.TrueIsGood), c.Child(middle.Schema(), c.Classify(c.Map("Faulted", "Healthy"))))
	child := observation("Available", metav1.ConditionFalse, "InvalidConfiguration")
	parent := &object{ObjectMeta: metav1.ObjectMeta{Generation: 7}}
	root := &object{}
	if !c.Aggregate(parent, map[string]c.Object{"database": child}, middle) {
		t.Fatal("first call unchanged")
	}
	fault, _ := c.Lookup(parent, "Faulted")
	if fault.Status != metav1.ConditionTrue || fault.Reason != "ConditionsWaitingForHuman" || fault.ObservedGeneration != 7 {
		t.Fatalf("wrong output: %#v", fault)
	}
	if !strings.Contains(fault.Message, "database: InvalidConfiguration") {
		t.Fatal(fault.Message)
	}
	c.AggregateSlice(root, []c.Object{parent}, top)
	healthy, _ := c.Lookup(root, "Healthy")
	if healthy.Status != metav1.ConditionFalse || healthy.Reason != "ConditionsWaitingForHuman" {
		t.Fatalf("published contract lost escalation/polarity: %#v", healthy)
	}
	if c.Aggregate(parent, map[string]c.Object{"database": child}, middle) {
		t.Fatal("not idempotent")
	}
	child.conditions[0].Status = metav1.ConditionTrue
	c.AggregateSlice(parent, []c.Object{child}, middle)
	c.AggregateSlice(root, []c.Object{parent}, top)
	healthy, _ = c.Lookup(root, "Healthy")
	if healthy.Status != metav1.ConditionTrue {
		t.Fatalf("did not recover: %#v", healthy)
	}
}

func TestIdentityNormalizesBothSchemas(t *testing.T) {
	a := define(contract("Ready", c.FalseIsGood), c.Child(contract("Ready", c.TrueIsGood), c.Identity))
	p := &object{}
	c.AggregateSlice(p, []c.Object{observation("Ready", metav1.ConditionTrue, "Fine")}, a)
	if value, _ := c.Lookup(p, "Ready"); value.Status != metav1.ConditionFalse {
		t.Fatalf("identity copied raw encoding: %#v", value)
	}
}

func TestSchemaScopesAndFilters(t *testing.T) {
	cases := []struct {
		name                 string
		scope                c.ResourceMatch
		group, version, kind string
	}{
		{"GVK", c.GVK("db.io", "v1", "Database"), "db.io", "v1", "Database"},
		{"GV", c.GV("db.io", "v2"), "db.io", "v2", "Other"},
		{"GK", c.GK("db.io", "Database"), "db.io", "v9", "Database"},
		{"G", c.G("db.io"), "db.io", "v3", "Other"},
		{"core group", c.G(""), "", "v1", "Pod"},
		{"global zero", c.ResourceMatch{}, "anything.io", "v3", "Anything"},
		{"empty GVK", c.GVK("", "", ""), "anything.io", "v3", "Anything"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := contract("Available", c.TrueIsGood)
			a := define(contract("Ready", c.TrueIsGood), c.Child(s, c.Classify(c.Map("Available", "Ready"))).For(tc.scope))
			p := &object{}
			c.AggregateSlice(p, []c.Object{scopeObject(tc.group, tc.version, tc.kind, observation("Available", metav1.ConditionTrue, "Fine"))}, a)
			if value, _ := c.Lookup(p, "Ready"); value.Status != metav1.ConditionTrue {
				t.Fatalf("scope not routed: %#v", value)
			}
		})
	}
	// Each narrower scope replaces the fallback's complete classification; the result is
	// independent of binding order, not a union of all matching bindings.
	bindings := []c.ChildBinding{
		c.Child(contract("Global", c.TrueIsGood), c.Classify(c.Map("Global", "Ready"))),
		c.Child(contract("Group", c.TrueIsGood), c.Classify(c.Map("Group", "Ready"))).For(c.G("db.io")),
		c.Child(contract("Version", c.TrueIsGood), c.Classify(c.Map("Version", "Ready"))).For(c.GV("db.io", "v1")),
		c.Child(contract("Exact", c.TrueIsGood), c.Classify(c.Map("Exact", "Ready"))).For(c.GVK("db.io", "v1", "Database")),
	}
	for iteration := 0; iteration < 2; iteration++ {
		a := define(contract("Ready", c.TrueIsGood), bindings...)
		for _, tc := range []struct{ g, v, k, name string }{{"db.io", "v1", "Database", "Exact"}, {"db.io", "v1", "Other", "Version"}, {"db.io", "v2", "Other", "Group"}, {"other.io", "v1", "Other", "Global"}} {
			p := &object{}
			c.AggregateSlice(p, []c.Object{scopeObject(tc.g, tc.v, tc.k, observation(tc.name, metav1.ConditionTrue, "Fine"))}, a)
			if value, _ := c.Lookup(p, "Ready"); value.Status != metav1.ConditionTrue {
				t.Fatalf("wrong specificity for %#v: %#v", tc, value)
			}
		}
		for l, r := 0, len(bindings)-1; l < r; l, r = l+1, r-1 {
			bindings[l], bindings[r] = bindings[r], bindings[l]
		}
	}
}

func TestDefinitionFailures(t *testing.T) {
	s := contract("Ready", c.TrueIsGood)
	for _, tc := range []struct {
		name, want string
		build      func()
	}{
		{"missing mapping", "mapped or ignored", func() { define(s, c.Child(s, c.Classification{RequireAll: true})) }},
		{"unknown source", "unknown source", func() { define(s, c.Child(s, c.Classify(c.Map("Missing", "Ready")))) }},
		{"unknown target", "unknown target", func() { define(s, c.Child(s, c.Classify(c.Map("Ready", "Missing")))) }},
		{"duplicate mapping", "duplicate mapping", func() { define(s, c.Child(s, c.Classify(c.Map("Ready", "Ready"), c.Map("Ready", "Ready")))) }},
		{"ignored mapping", "ignored source", func() { define(s, c.Child(s, c.Classify(c.Ignore("Ready"), c.Map("Ready", "Ready")))) }},
		{"two global schemas", "ambiguous", func() { define(s, c.Child(s, c.Identity), c.Child(s, c.Identity)) }},
		{"GV vs GK", "ambiguous", func() {
			define(s, c.Child(s, c.Identity).For(c.GV("db.io", "v1")), c.Child(s, c.Identity).For(c.GK("db.io", "Database")))
		}},
		{"scope expansion", "outside", func() {
			scoped := s
			scoped.Resources = []c.ResourceMatch{c.G("db.io")}
			define(s, c.Child(scoped, c.Identity).For(c.G("other.io")))
		}},
		{"empty explicit version", "empty version", func() { scoped := s; scoped.Resources = []c.ResourceMatch{c.GV("db.io", "")}; define(scoped) }},
	} {
		t.Run(tc.name, func(t *testing.T) { assertPanic(t, tc.want, tc.build) })
	}
}

func TestAllUnknownLocalAndOverrides(t *testing.T) {
	s := contract("Ready", c.TrueIsGood)
	mapping := c.Map("Ready", "Ready").Escalate(c.WaitingForHuman, c.Reason("Manual")).Escalate(c.Failed)
	a := c.Define(c.Definition{Outputs: []c.OutputDefinition{c.All("Ready", s.Conditions["Ready"]).Reduce(c.AllReducer{})}, Children: []c.ChildBinding{c.Child(s, c.Classify(mapping))}})
	for _, tc := range []struct {
		name     string
		children []c.Object
		status   metav1.ConditionStatus
		reason   string
	}{
		{"empty", nil, metav1.ConditionUnknown, "ConditionsUnknown"},
		{"missing", []c.Object{&object{}}, metav1.ConditionUnknown, "ConditionsUnknown"},
		{"unknown", []c.Object{observation("Ready", metav1.ConditionUnknown, "Pending")}, metav1.ConditionUnknown, "ConditionsUnknown"},
		{"first override", []c.Object{observation("Ready", metav1.ConditionFalse, "Manual")}, metav1.ConditionFalse, "ConditionsWaitingForHuman"},
		{"bad beats unknown", []c.Object{&object{}, observation("Ready", metav1.ConditionFalse, "Broken")}, metav1.ConditionFalse, "ConditionsFailed"},
		{"good never escalates", []c.Object{observation("Ready", metav1.ConditionTrue, "Manual")}, metav1.ConditionTrue, "ConditionsSatisfied"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &object{}
			c.AggregateSlice(p, tc.children, a)
			v, _ := c.Lookup(p, "Ready")
			if v.Status != tc.status || v.Reason != tc.reason {
				t.Fatalf("%#v", v)
			}
		})
	}
	p := &object{}
	local := metav1.Condition{Type: "Ready", Status: metav1.ConditionUnknown, Reason: "LocalUnknown"}
	c.AggregateSlice(p, []c.Object{observation("Ready", metav1.ConditionTrue, "Fine")}, a, local)
	if v, _ := c.Lookup(p, "Ready"); v.Reason != "LocalUnknown" {
		t.Fatal(v)
	}
	localOnly := c.Define(c.Definition{Outputs: []c.OutputDefinition{c.Local("Ready", s.Conditions["Ready"])}})
	p = &object{}
	c.Aggregate(p, nil, localOnly)
	if len(p.conditions) != 0 {
		t.Fatal("local assertion invented")
	}
	local.Status = metav1.ConditionTrue
	local.ObservedGeneration = 3
	c.Aggregate(p, nil, localOnly, local)
	if v, _ := c.Lookup(p, "Ready"); v.Status != metav1.ConditionTrue || v.ObservedGeneration != 3 {
		t.Fatal(v)
	}
}

func TestAtomicityImmutabilityAndSlice(t *testing.T) {
	s := contract("Ready", c.TrueIsGood)
	scopes := []c.ResourceMatch{c.G("db.io")}
	binding := c.Child(s, c.Identity).For(scopes...)
	a := define(s, binding)
	s.Conditions["Ready"] = c.Spec{Polarity: c.FalseIsGood}
	scopes[0] = c.G("changed.io")
	exported := a.Schema()
	delete(exported.Conditions, "Ready")
	child := scopeObject("db.io", "v1", "Database", observation("Ready", metav1.ConditionFalse, "Broken"))
	p, q := &object{}, &object{}
	c.AggregateSlice(p, []c.Object{nil, child}, a)
	c.Aggregate(q, map[string]c.Object{"1": child}, a)
	if !reflect.DeepEqual(p.conditions, q.conditions) {
		t.Fatal("map/slice differ")
	}
	if p.conditions[0].Status != metav1.ConditionFalse || !strings.Contains(p.conditions[0].Message, "1: Broken") {
		t.Fatal(p.conditions)
	}
	before := append([]metav1.Condition(nil), p.conditions...)
	sets := p.sets
	local := metav1.Condition{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Local"}
	assertPanic(t, "duplicate local", func() { c.Aggregate(p, nil, a, local, local) })
	if p.sets != sets || !reflect.DeepEqual(before, p.conditions) {
		t.Fatal("partial mutation on failure")
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); c.AggregateSlice(&object{}, []c.Object{child}, a) }()
	}
	wg.Wait()
}

func TestFilteringAndConditionalParticipation(t *testing.T) {
	input := contract("Available", c.TrueIsGood)
	input.Conditions["Synced"] = c.Spec{Polarity: c.TrueIsGood}
	a := define(contract("Ready", c.TrueIsGood), c.Child(input, c.Classify(
		c.Map("Available", "Ready").When(c.Status(metav1.ConditionFalse)), c.Ignore("Synced"),
	)).For(c.G("db.io")))
	p := &object{}
	c.AggregateSlice(p, []c.Object{
		scopeObject("other.io", "v1", "Database", observation("Available", metav1.ConditionFalse, "Ignored")),
		scopeObject("db.io", "v1", "Database", observation("Available", metav1.ConditionTrue, "Filtered")),
	}, a)
	if v, _ := c.Lookup(p, "Ready"); v.Status != metav1.ConditionUnknown {
		t.Fatal(v)
	}
	c.AggregateSlice(p, []c.Object{scopeObject("db.io", "v1", "Database", observation("Available", metav1.ConditionFalse, "Broken"))}, a)
	if v, _ := c.Lookup(p, "Ready"); v.Status != metav1.ConditionFalse {
		t.Fatal(v)
	}
}

func define(s c.Schema, children ...c.ChildBinding) *c.Aggregation {
	d := c.Definition{Resources: s.Resources, Children: children, Now: func() time.Time { return time.Unix(100, 0) }}
	for name, spec := range s.Conditions {
		d.Outputs = append(d.Outputs, c.All(name, spec))
	}
	return c.Define(d)
}
