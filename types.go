// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package conditions

import (
	"fmt"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"reflect"
)

// ObjectTypes constructs resource scopes from API objects. Scheme may be nil when
// objects carry their GVK. Finish scheme registration before sharing these helpers.
type ObjectTypes struct{ Scheme *runtime.Scheme }

func Types(scheme *runtime.Scheme) ObjectTypes { return ObjectTypes{Scheme: scheme} }

func (t ObjectTypes) G(object runtime.Object) ResourceMatch {
	return t.scope(object, func(k schema.GroupVersionKind) ResourceMatch { return G(k.Group) })
}
func (t ObjectTypes) GV(object runtime.Object) ResourceMatch {
	return t.scope(object, func(k schema.GroupVersionKind) ResourceMatch { return GV(k.Group, k.Version) })
}
func (t ObjectTypes) GK(object runtime.Object) ResourceMatch {
	return t.scope(object, func(k schema.GroupVersionKind) ResourceMatch { return GK(k.Group, k.Kind) })
}
func (t ObjectTypes) GVK(object runtime.Object) ResourceMatch {
	return t.scope(object, func(k schema.GroupVersionKind) ResourceMatch { return GVK(k.Group, k.Version, k.Kind) })
}

// Helpers panic on configuration errors, like Define. Ambiguity is checked after
// projection: several versions of one GK are valid for GK, but not for GVK.
func (t ObjectTypes) scope(object runtime.Object, project func(schema.GroupVersionKind) ResourceMatch) ResourceMatch {
	if isNil(object) {
		panic("conditions: nil type object")
	}
	kinds, err := resolveKinds(object, t.Scheme)
	if err != nil {
		panic("conditions: " + err.Error())
	}
	result := project(kinds[0])
	for _, kind := range kinds[1:] {
		if project(kind) != result {
			panic(fmt.Sprintf("conditions: ambiguous scope for %T: %v", object, kinds))
		}
	}
	return result
}

func resolveKinds(object interface{ GetObjectKind() schema.ObjectKind }, scheme *runtime.Scheme) ([]schema.GroupVersionKind, error) {
	kind := object.GetObjectKind().GroupVersionKind()
	if !kind.Empty() {
		if kind.Version == "" || kind.Kind == "" {
			return nil, fmt.Errorf("incomplete GVK for %T: %v", object, kind)
		}
		return []schema.GroupVersionKind{kind}, nil
	}
	typed, ok := object.(runtime.Object)
	if !ok || scheme == nil {
		return nil, fmt.Errorf("missing GVK for %T; supply TypeMeta, a Scheme, or WithGVK", object)
	}
	kinds, _, err := scheme.ObjectKinds(typed)
	if err != nil {
		return nil, err
	}
	if len(kinds) == 0 {
		return nil, fmt.Errorf("no GVK registered for %T", object)
	}
	for _, kind := range kinds {
		if kind.Version == "" || kind.Kind == "" {
			return nil, fmt.Errorf("incomplete registered GVK for %T: %v", object, kind)
		}
	}
	return kinds, nil
}

// compileTypes freezes scheme registrations and checks routing ambiguity before
// any reconcile. Only condition-bearing types participate in child routing.
func (a *Aggregation) compileTypes(s *runtime.Scheme) {
	if s == nil {
		return
	}
	a.types = map[reflect.Type]*compiledChild{}
	kindsByType := map[reflect.Type][]schema.GroupVersionKind{}
	for kind, typ := range s.AllKnownTypes() {
		if _, ok := reflect.New(typ).Interface().(Object); !ok {
			continue
		}
		ptr := reflect.PointerTo(typ)
		kindsByType[ptr] = append(kindsByType[ptr], kind)
	}
	for typ, kinds := range kindsByType {
		var selected *compiledChild
		for i, kind := range kinds {
			if kind.Version == "" || kind.Kind == "" {
				panic(fmt.Sprintf("conditions: incomplete registered GVK for %v: %v", typ, kind))
			}
			binding := a.childKind(kind)
			if i > 0 && binding != selected {
				panic(fmt.Sprintf("conditions: ambiguous child identity for %v: %v", typ, kinds))
			}
			selected = binding
		}
		a.types[typ] = selected
	}
}
