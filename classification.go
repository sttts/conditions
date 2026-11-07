// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package conditions

import (
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Predicate inspects a source observation. Custom predicates must be pure.
type Predicate func(metav1.Condition) bool

func Reason(reason string) Predicate {
	return func(c metav1.Condition) bool { return c.Reason == reason }
}
func Status(status metav1.ConditionStatus) Predicate {
	return func(c metav1.Condition) bool { return c.Status == status }
}

// Mapping translates one published source condition into one parent output contribution.
// Methods return new values, so a global mapping can be safely reused.
type Mapping struct {
	Source, Target string
	Ignored        bool
	Predicate      Predicate
	Overrides      []EscalationOverride
	Selector       ChildSelector
}

// EscalationOverride replaces a bad observation's escalation when Predicate matches.
// A nil Predicate matches every bad observation.
type EscalationOverride struct {
	Predicate Predicate
	Level     Escalation
}

func Map(source, target string) Mapping { return Mapping{Source: source, Target: target} }
func Ignore(source string) Mapping      { return Mapping{Source: source, Ignored: true} }

// When limits participation. A missing exact input is tested as Unknown.
func (m Mapping) When(predicate Predicate) Mapping {
	if predicate == nil {
		panic("conditions: nil predicate")
	}
	m.Predicate = predicate
	return m
}

// Escalate overrides classification of bad observations only. First matching override wins.
// With no predicate it is unconditional; at most one predicate is accepted.
func (m Mapping) Escalate(value Escalation, predicates ...Predicate) Mapping {
	if len(predicates) > 1 || value > Failed {
		panic("conditions: invalid escalation override")
	}
	var when Predicate
	if len(predicates) == 1 {
		when = predicates[0]
		if when == nil {
			panic("conditions: nil escalation predicate")
		}
	}
	m.Overrides = append(append([]EscalationOverride(nil), m.Overrides...), EscalationOverride{Predicate: when, Level: value})
	return m
}

// Select is the escape hatch for contextual, multi-condition domain classification.
// Its normalized contributions replace the source selection and interpretation; When and
// Escalate still apply. The selector receives the original objects and must be read-only.
func (m Mapping) Select(selector ChildSelector) Mapping {
	if isNil(selector) {
		panic("conditions: nil selector")
	}
	m.Selector = selector
	return m
}

// Classification is a declarative translation between independently published schemas.
// Unmapped sources do not participate. RequireAll optionally enforces complete coverage.
// SameNames maps the intersection of source and target names (explicit semantic identity).
type Classification struct {
	SameNames  bool
	RequireAll bool
	Mappings   []Mapping
}

var Identity = Classification{SameNames: true}

func Classify(mappings ...Mapping) Classification {
	return Classification{Mappings: append([]Mapping(nil), mappings...)}
}

// ChildBinding binds a published child schema to its interpretation by this consumer.
type ChildBinding struct {
	Schema         Schema
	Classification Classification
	Resources      []ResourceMatch
}

func Child(s Schema, classification Classification) ChildBinding {
	return ChildBinding{Schema: s, Classification: classification}
}

// For restricts this use of a schema, without changing the published schema.
func (c ChildBinding) For(resources ...ResourceMatch) ChildBinding {
	if len(resources) == 0 {
		panic("conditions: For requires a scope")
	}
	c.Resources = append([]ResourceMatch(nil), resources...)
	return c
}

type compiledChild struct {
	schema   Schema
	mappings []Mapping
}

func compileChild(binding ChildBinding, target Schema) compiledChild {
	s := cloneSchema(binding.Schema)
	validateSchema(s)
	if len(s.Resources) == 0 {
		s.Resources = []ResourceMatch{{}}
	}
	if len(binding.Resources) > 0 {
		for _, filter := range binding.Resources {
			allowed := false
			for _, scope := range s.Resources {
				if scope.contains(filter) {
					allowed = true
					break
				}
			}
			if !allowed {
				panic(fmt.Sprintf("conditions: filter %s is outside child schema scope", filter))
			}
		}
		s.Resources = append([]ResourceMatch(nil), binding.Resources...)
		validateSchema(s)
	}
	mappings := binding.Classification.Mappings
	if binding.Classification.SameNames {
		if len(mappings) != 0 {
			panic("conditions: SameNames cannot be combined with explicit mappings")
		}
		mappings = nil
		for _, name := range schemaNames(s) {
			if _, ok := target.Conditions[name]; ok {
				mappings = append(mappings, Map(name, name))
			}
		}
	}
	seen := map[string]bool{}
	ignored := map[string]bool{}
	pairs := map[string]bool{}
	result := compiledChild{schema: s}
	for _, m := range mappings {
		if m.Selector != nil && isNil(m.Selector) {
			panic("conditions: nil mapping selector")
		}
		if m.Ignored && (m.Target != "" || m.Selector != nil || m.Predicate != nil || len(m.Overrides) != 0) {
			panic("conditions: Ignore cannot have mapping behavior")
		}
		for _, override := range m.Overrides {
			if override.Level > Failed {
				panic("conditions: invalid escalation override")
			}
		}
		if _, ok := s.Conditions[m.Source]; !ok {
			panic(fmt.Sprintf("conditions: unknown source %q", m.Source))
		}
		if m.Ignored {
			if seen[m.Source] {
				panic(fmt.Sprintf("conditions: conflicting Ignore(%q)", m.Source))
			}
			ignored[m.Source] = true
		} else {
			if ignored[m.Source] {
				panic(fmt.Sprintf("conditions: mapping ignored source %q", m.Source))
			}
			if _, ok := target.Conditions[m.Target]; !ok {
				panic(fmt.Sprintf("conditions: unknown target %q", m.Target))
			}
			pair := m.Source + "\x00" + m.Target
			if pairs[pair] {
				panic(fmt.Sprintf("conditions: duplicate mapping %q -> %q; put overrides on one Map", m.Source, m.Target))
			}
			pairs[pair] = true
		}
		seen[m.Source] = true
		m.Overrides = append([]EscalationOverride(nil), m.Overrides...)
		result.mappings = append(result.mappings, m)
	}
	for _, name := range schemaNames(s) {
		if binding.Classification.RequireAll && !seen[name] {
			panic(fmt.Sprintf("conditions: source %q must be mapped or ignored", name))
		}
	}
	return result
}
func (c compiledChild) selectFor(target string, parent, child Object) []Contribution {
	var result []Contribution
	for _, m := range c.mappings {
		if m.Ignored || m.Target != target {
			continue
		}
		spec := c.schema.Conditions[m.Source]
		var selected []Contribution
		if m.Selector != nil {
			selected = m.Selector.Select(parent, child)
		} else {
			var values []metav1.Condition
			if spec.Suffix {
				for _, value := range child.GetConditions() {
					if strings.HasSuffix(value.Type, m.Source) {
						values = append(values, value)
					}
				}
			} else {
				value, found := Lookup(child, m.Source)
				if !found && spec.Missing != nil {
					value = *spec.Missing
					value.Type = m.Source
					value.Status = metav1.ConditionUnknown
				}
				values = []metav1.Condition{value}
			}
			for _, value := range values {
				state := State{Assessment: (ConditionType{Type: m.Source, Polarity: spec.Polarity}).Assessment(value)}
				if state.Assessment == Bad {
					state.Escalation = classify(spec.Reasons, value)
				}
				selected = append(selected, Contribution{State: state, Condition: value, Source: child})
			}
		}
		for _, value := range selected {
			if m.Predicate != nil && !m.Predicate(value.Condition) {
				continue
			}
			if value.State.Assessment == Bad {
				for _, override := range m.Overrides {
					if override.Predicate == nil || override.Predicate(value.Condition) {
						value.State.Escalation = override.Level
						break
					}
				}
			}
			result = append(result, value)
		}
	}
	return result
}
