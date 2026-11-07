// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package conditions

import (
	"fmt"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// ResourceMatch identifies a schema's API scope. Construct it with G, GV, GK, or GVK.
// G("") denotes the core API group. The zero value matches every resource.
// Set flags distinguish wildcards from explicit empty values. For example,
// ResourceMatch{Group: "db.io", GroupSet: true} is identical to G("db.io").
type ResourceMatch struct {
	Group, Version, Kind          string
	GroupSet, VersionSet, KindSet bool
}

func G(group string) ResourceMatch { return ResourceMatch{Group: group, GroupSet: true} }
func GV(group, version string) ResourceMatch {
	return ResourceMatch{Group: group, GroupSet: true, Version: version, VersionSet: true}
}
func GK(group, kind string) ResourceMatch {
	return ResourceMatch{Group: group, GroupSet: true, Kind: kind, KindSet: true}
}
func GVK(group, version, kind string) ResourceMatch {
	if group == "" && version == "" && kind == "" {
		return ResourceMatch{}
	}
	return ResourceMatch{Group: group, GroupSet: true, Version: version, Kind: kind, VersionSet: true, KindSet: true}
}
func (r ResourceMatch) matches(gvk schema.GroupVersionKind) bool {
	return (!r.GroupSet || (gvk.Version != "" && gvk.Kind != "" && r.Group == gvk.Group)) && (!r.VersionSet || r.Version == gvk.Version) && (!r.KindSet || r.Kind == gvk.Kind)
}
func (r ResourceMatch) contains(s ResourceMatch) bool {
	return (!r.GroupSet || s.GroupSet && r.Group == s.Group) && (!r.VersionSet || s.VersionSet && r.Version == s.Version) && (!r.KindSet || s.KindSet && r.Kind == s.Kind)
}
func (r ResourceMatch) overlaps(s ResourceMatch) bool {
	return (!r.GroupSet || !s.GroupSet || r.Group == s.Group) && (!r.VersionSet || !s.VersionSet || r.Version == s.Version) && (!r.KindSet || !s.KindSet || r.Kind == s.Kind)
}
func (r ResourceMatch) String() string {
	if !r.GroupSet {
		return "<global>"
	}
	v, k := "*", "*"
	if r.VersionSet {
		v = r.Version
	}
	if r.KindSet {
		k = r.Kind
	}
	return fmt.Sprintf("%q/%s, Kind=%s", r.Group, v, k)
}

// Spec is the published interpretation of a condition, independent of its consumers.
// Suffix describes a family such as OpenShift's *Available operator conditions.
// Missing supplies diagnostics for a missing exact condition (status is always Unknown).
// A suffix family contributes nothing when no matching conditions are present.
type Spec struct {
	Polarity Polarity
	Reasons  Classifier
	Suffix   bool
	Missing  *metav1.Condition
}

// Schema is an ordinary, publishable Go value. Empty Resources means global.
// A zero ResourceMatch (or GVK("", "", "")) also matches every resource.
// G("") matches only the core API group. Define snapshots its maps and slices.
// Custom classifiers must be immutable and safe to share across evaluations.
type Schema struct {
	Resources  []ResourceMatch
	Conditions map[string]Spec
}

func cloneSchema(s Schema) Schema {
	out := Schema{Resources: append([]ResourceMatch(nil), s.Resources...), Conditions: map[string]Spec{}}
	for name, spec := range s.Conditions {
		if spec.Missing != nil {
			copy := *spec.Missing
			spec.Missing = &copy
		}
		spec.Reasons = cloneReasons(spec.Reasons)
		out.Conditions[name] = spec
	}
	return out
}
func cloneReasons(value Classifier) Classifier {
	switch classifier := value.(type) {
	case ReasonClassifier:
		classifier.Suffixes = append([]ReasonSuffix(nil), classifier.Suffixes...)
		reasons := map[string]Escalation{}
		for k, v := range classifier.Reasons {
			reasons[k] = v
		}
		classifier.Reasons = reasons
		return classifier
	case aggregateClassifier:
		return aggregateClassifier{fallback: cloneReasons(classifier.fallback)}
	default:
		return value
	}
}

func validateSchema(s Schema) {
	for _, r := range s.Resources {
		if (!r.GroupSet && (r.Group != "" || r.VersionSet || r.KindSet)) || (!r.VersionSet && r.Version != "") || (!r.KindSet && r.Kind != "") {
			panic("conditions: inconsistent resource scope flags")
		}
		if r.VersionSet && r.Version == "" || r.KindSet && r.Kind == "" {
			panic("conditions: empty version or kind in schema scope")
		}
	}
	for name, spec := range s.Conditions {
		if spec.Reasons != nil && isNil(spec.Reasons) {
			panic("conditions: nil reason interpreter")
		}
		if name == "" || spec.Polarity > FalseIsGood {
			panic(fmt.Sprintf("conditions: invalid condition %q", name))
		}
	}
}
func schemaNames(s Schema) []string {
	names := make([]string, 0, len(s.Conditions))
	for name := range s.Conditions {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// WithGVK supplies type information when a client returned a typed object without TypeMeta.
// It changes neither the original object nor its conditions. There is no scheme/registry lookup.
func WithGVK(object Object, gvk schema.GroupVersionKind) Object {
	if isNil(object) {
		return nil
	}
	return typedObject{Object: object, gvk: gvk}
}

type typedObject struct {
	Object
	gvk schema.GroupVersionKind
}

func (o typedObject) GetObjectKind() schema.ObjectKind { return fixedKind{o.gvk} }

type fixedKind struct{ gvk schema.GroupVersionKind }

func (k fixedKind) GroupVersionKind() schema.GroupVersionKind { return k.gvk }
func (fixedKind) SetGroupVersionKind(schema.GroupVersionKind) {
	panic("conditions: WithGVK is read-only")
}
