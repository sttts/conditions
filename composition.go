// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package conditions

import (
	"fmt"
	"sort"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Compose selects rule values from independent packages, without registration.
// It rejects invalid definitions and missing or cyclic dependencies before use.
// The returned slice is new; policy implementations must be pure and safe to share.
// Set the returned Rules.Now to inject a clock for the complete evaluation.
func Compose(groups ...[]Aggregation) (Rules, error) {
	rules := Rules{}
	for _, group := range groups {
		rules.Outputs = append(rules.Outputs, group...)
	}
	if _, _, err := plan(rules); err != nil {
		return Rules{}, err
	}
	return rules, nil
}

// plan validates definitions even when no local base will activate them.
func plan(rules Rules) ([]Aggregation, map[string]string, error) {
	definitions := make(map[string]Aggregation, len(rules.Outputs))
	owners := make(map[string]string, len(rules.Outputs))
	for _, rule := range rules.Outputs {
		name := rule.Output.Type
		if name == "" || isNil(rule.Parent) || isNil(rule.Reduction) || isNil(rule.Messages) {
			return nil, nil, fmt.Errorf("incomplete aggregation for %q", name)
		}
		if _, exists := owners[name]; exists {
			return nil, nil, fmt.Errorf("duplicate output %q", name)
		}
		if rule.DefaultBase != nil && rule.DefaultBase.Type != name {
			return nil, nil, fmt.Errorf("default base %q does not match output %q", rule.DefaultBase.Type, name)
		}
		owners[name] = name
		for _, derivation := range rule.Derived {
			if isNil(derivation) {
				return nil, nil, fmt.Errorf("nil derivation for %q", name)
			}
			for _, output := range derivation.Outputs() {
				if output == "" {
					return nil, nil, fmt.Errorf("empty derived output for %q", name)
				}
				if previous, exists := owners[output]; exists {
					return nil, nil, fmt.Errorf("duplicate output %q (owners %q and %q)", output, previous, name)
				}
				owners[output] = name
			}
		}
		definitions[name] = rule
	}
	names := make([]string, 0, len(definitions))
	for name := range definitions {
		names = append(names, name)
	}
	sort.Strings(names)
	var ordered []Aggregation
	state := map[string]uint8{}
	var visit func(string) error
	visit = func(name string) error {
		if state[name] == 1 {
			return fmt.Errorf("dependency cycle at %q", name)
		}
		if state[name] == 2 {
			return nil
		}
		state[name] = 1
		rule := definitions[name]
		dependencies := append([]string(nil), rule.Requires...)
		sort.Strings(dependencies)
		for _, dependency := range dependencies {
			owner, ok := owners[dependency]
			if !ok {
				return fmt.Errorf("output %q requires missing output %q", name, dependency)
			}
			if err := visit(owner); err != nil {
				return err
			}
		}
		state[name] = 2
		ordered = append(ordered, rule)
		return nil
	}
	for _, name := range names {
		if err := visit(name); err != nil {
			return nil, nil, err
		}
	}
	return ordered, owners, nil
}

// Evaluate computes desired Conditions without modifying the supplied objects.
// Rules read a shared input snapshot plus only their declared dependency results.
func Evaluate(parent Object, children map[string]Object, rules Rules, local ...metav1.Condition) ([]metav1.Condition, error) {
	if isNil(parent) {
		return nil, fmt.Errorf("parent is nil")
	}
	ordered, owners, err := plan(rules)
	if err != nil {
		return nil, err
	}
	nowFunc := rules.Now
	if nowFunc == nil {
		nowFunc = time.Now
	}
	now := nowFunc()
	input := &conditionOverlay{Object: parent, conditions: append([]metav1.Condition(nil), parent.GetConditions()...)}
	bases := map[string]metav1.Condition{}
	local = append([]metav1.Condition(nil), local...)
	sort.Slice(local, func(i, j int) bool { return local[i].Type < local[j].Type })
	for _, value := range local {
		if _, exists := bases[value.Type]; exists {
			return nil, fmt.Errorf("duplicate local condition %q", value.Type)
		}
		bases[value.Type] = value
		if _, owned := owners[value.Type]; !owned {
			input.SetConditions(stamp(parent, value, now))
		}
	}
	results := map[string]metav1.Condition{}
	for _, rule := range ordered {
		base, provided := bases[rule.Output.Type]
		if !provided {
			if rule.DefaultBase == nil {
				continue
			}
			base = *rule.DefaultBase
		}
		view := &conditionOverlay{Object: parent, conditions: append([]metav1.Condition(nil), input.GetConditions()...)}
		for _, dependency := range rule.Requires {
			if value, ok := results[dependency]; ok {
				view.SetConditions(value)
			}
		}
		value, err := rule.Evaluate(view, children, base, now)
		if err != nil {
			return nil, err
		}
		if value == nil {
			continue
		}
		results[value.Type] = *value
		view.SetConditions(*value)
		for _, derivation := range rule.Derived {
			declared := map[string]bool{}
			for _, name := range derivation.Outputs() {
				declared[name] = true
			}
			for _, derived := range derivation.Derive(view, now) {
				if !declared[derived.Type] {
					return nil, fmt.Errorf("output %q derivation wrote undeclared type %q", rule.Output.Type, derived.Type)
				}
				if _, exists := results[derived.Type]; exists {
					return nil, fmt.Errorf("duplicate derived result %q", derived.Type)
				}
				results[derived.Type] = derived
			}
		}
	}
	// Preserve existing slice order; append newly produced types in canonical order.
	working := &conditionOverlay{Object: parent, conditions: append([]metav1.Condition(nil), input.GetConditions()...)}
	names := make([]string, 0, len(results))
	for name := range results {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		working.SetConditions(results[name])
	}
	return working.GetConditions(), nil
}
