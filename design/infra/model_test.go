// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package conditionsproposal

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Metadata is supplied fixture data, not a resource lookup.
type fixtureMetadata struct {
	Reference string     `json:"reference"`
	CreatedAt time.Time  `json:"createdAt"`
	DeletedAt *time.Time `json:"deletedAt"`
	Deleting  bool       `json:"deleting"`
}

type fixtureChild struct {
	fixtureMetadata
	Conditions map[string]metav1.Condition `json:"conditions"`
}

type semanticFixture struct {
	Name           string             `json:"name"`
	Previous       []metav1.Condition `json:"previous"`
	Bases          []metav1.Condition `json:"bases"`
	Children       []*fixtureChild    `json:"children"`
	Steps          []string           `json:"steps"`
	ParentMetadata fixtureMetadata    `json:"parentMetadata"`
	Expected       map[string]struct {
		Status  metav1.ConditionStatus `json:"status"`
		Reason  *string                `json:"reason"`
		Message *string                `json:"message"`
	} `json:"expected"`
}

type presentationFixture struct {
	Name            string             `json:"name"`
	Conditions      []metav1.Condition `json:"conditions"`
	ParentMetadata  fixtureMetadata    `json:"parentMetadata"`
	ExpectedPhase   string             `json:"expectedPhase"`
	ExpectedMessage string             `json:"expectedMessage"`
}

func loadFixtures(t *testing.T) ([]semanticFixture, []presentationFixture) {
	t.Helper()
	data, err := os.ReadFile("fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Fixtures     []semanticFixture     `json:"fixtures"`
		Presentation []presentationFixture `json:"presentationFixtures"`
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus.Fixtures) != 27 || len(corpus.Presentation) != 17 {
		t.Fatalf("unexpected fixture counts: %d/%d", len(corpus.Fixtures), len(corpus.Presentation))
	}
	return corpus.Fixtures, corpus.Presentation
}

// BEGIN OBJECT_EXAMPLE
// exampleResource stands in for an existing typed resource with Kubernetes metadata and status.
type exampleResource struct {
	metav1.ObjectMeta
	Status exampleStatus
}

// exampleStatus uses the normal Conditions field of the example resource's status.
type exampleStatus struct{ Conditions []metav1.Condition }

func (r *exampleResource) GetConditions() []metav1.Condition { return r.Status.Conditions }
func (r *exampleResource) SetConditions(conditions []metav1.Condition) {
	r.Status.Conditions = conditions
}

// setConditions only prepares or updates the example API objects' supplied observations.
func setConditions(resource Setter, conditions ...metav1.Condition) bool {
	current := conditionOverlay{conditions: append([]metav1.Condition(nil), resource.GetConditions()...)}
	changed := current.SetConditions(conditions...)
	if changed {
		resource.SetConditions(current.conditions)
	}
	return changed
}

// END OBJECT_EXAMPLE

func fixtureResource(metadata fixtureMetadata, conditions []metav1.Condition) *exampleResource {
	deletedAt := metadata.DeletedAt
	if metadata.Deleting && deletedAt == nil {
		value := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
		deletedAt = &value
	}
	resource := &exampleResource{ObjectMeta: metav1.ObjectMeta{Name: metadata.Reference, CreationTimestamp: metav1.NewTime(metadata.CreatedAt)}, Status: exampleStatus{Conditions: append([]metav1.Condition(nil), conditions...)}}
	if deletedAt != nil {
		value := metav1.NewTime(*deletedAt)
		resource.DeletionTimestamp = &value
	}
	return resource
}

func fixtureObjects(f semanticFixture) (*exampleResource, map[string]Object) {
	parent := fixtureResource(f.ParentMetadata, f.Previous)
	children := map[string]Object{}
	for i, child := range f.Children {
		if child == nil {
			children[fmt.Sprintf("Deleted%d", i)] = nil
			continue
		}
		var types []string
		for typ := range child.Conditions {
			types = append(types, typ)
		}
		sort.Strings(types)
		var conditions []metav1.Condition
		for _, typ := range types {
			conditions = append(conditions, child.Conditions[typ])
		}
		children[child.Reference] = fixtureResource(child.fixtureMetadata, conditions)
	}
	return parent, children
}

func fixtureNow() time.Time { return time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC) }

// The corpus supplies caller steps explicitly; each pure evaluation receives the resource objects.
func TestInfraSemanticCorpus(t *testing.T) {
	fixtures, _ := loadFixtures(t)
	aggregations := map[string]Aggregation{"available": InfraAvailability, "nonDegraded": InfraNonDegraded, "upToDate": InfraUpToDate, "synced": InfraSynced, "deleting": InfraDeleting}
	for _, f := range fixtures {
		t.Run(f.Name, func(t *testing.T) {
			parent, children := fixtureObjects(f)
			for _, step := range f.Steps {
				if step == "initialized" {
					setConditions(parent, DeriveInitialized(parent, fixtureNow()))
					continue
				}
				aggregation, ok := aggregations[step]
				if !ok {
					t.Fatalf("unknown step %q", step)
				}
				var base *metav1.Condition
				for i := range f.Bases {
					if f.Bases[i].Type == aggregation.Output.Type {
						base = &f.Bases[i]
						break
					}
				}
				if base == nil && step == "deleting" {
					base = &metav1.Condition{Type: "Deleting", Status: metav1.ConditionTrue, Reason: "Deleting", Message: "resource is being deleted"}
				}
				if base == nil {
					continue
				}
				result, err := aggregation.Evaluate(parent, children, *base, fixtureNow())
				if err != nil {
					t.Fatal(err)
				}
				if result != nil {
					setConditions(parent, *result)
				}
			}
			assertExpected(t, f, parent)
		})
	}
}

func assertExpected(t *testing.T, f semanticFixture, parent Getter) {
	t.Helper()
	for typ, want := range f.Expected {
		got, ok := Lookup(parent, typ)
		if !ok {
			t.Errorf("missing %s", typ)
			continue
		}
		if got.Status != want.Status {
			t.Errorf("%s status: got %q, want %q", typ, got.Status, want.Status)
		}
		if want.Reason != nil && got.Reason != *want.Reason {
			t.Errorf("%s reason: got %q, want %q", typ, got.Reason, *want.Reason)
		}
		if want.Message != nil && got.Message != *want.Message {
			t.Errorf("%s message: got %q, want %q", typ, got.Message, *want.Message)
		}
	}
}

// The facade applies Conditions in memory and reports no change on repeated identical input.
func TestInfraCallerCorpus(t *testing.T) {
	fixtures, _ := loadFixtures(t)
	profile := Infra
	profile.Rules.Now = fixtureNow
	for _, f := range fixtures {
		t.Run(f.Name, func(t *testing.T) {
			parent, children := fixtureObjects(f)
			if _, err := Aggregate(parent, children, profile.Rules, f.Bases...); err != nil {
				t.Fatal(err)
			}
			assertExpected(t, f, parent)
			if changed, err := profile.Aggregate(parent, children, f.Bases...); err != nil || changed {
				t.Fatalf("second call: changed=%v err=%v", changed, err)
			}
		})
	}
}

func TestInfraPresentationCorpus(t *testing.T) {
	_, fixtures := loadFixtures(t)
	var presenter Presenter = InfraSummary{Inputs: InfraConditions, Classifier: InfraClassification, Messages: infraMessages}
	for _, f := range fixtures {
		t.Run(f.Name, func(t *testing.T) {
			phase, message := presenter.Present(fixtureResource(f.ParentMetadata, f.Conditions))
			if phase != f.ExpectedPhase || message != f.ExpectedMessage {
				t.Fatalf("got (%q,%q), want (%q,%q)", phase, message, f.ExpectedPhase, f.ExpectedMessage)
			}
		})
	}
}

func TestInfraOutputPolarity(t *testing.T) {
	fixtures, _ := loadFixtures(t)
	for _, polarity := range []Polarity{TrueIsGood, FalseIsGood} {
		for _, healthy := range []bool{false, true} {
			parent, children := fixtureObjects(fixtures[0])
			if healthy {
				setConditions(children["Worker"].(Conditioned), metav1.Condition{Type: "Available", Status: metav1.ConditionTrue, Reason: "Available"})
			}
			aggregation := InfraAvailability
			aggregation.Output = ConditionType{"Result", polarity}
			got, err := aggregation.Evaluate(parent, children, fixtures[0].Bases[0], fixtureNow())
			if err != nil {
				t.Fatal(err)
			}
			want := metav1.ConditionFalse
			if healthy == (polarity == TrueIsGood) {
				want = metav1.ConditionTrue
			}
			if got.Status != want {
				t.Errorf("healthy=%v polarity=%v: got %s, want %s", healthy, polarity, got.Status, want)
			}
		}
	}
}

// BEGIN INVOCATION
// Example_infra passes existing resource objects, named children, and local Conditions directly.
func Example_infra() {
	parent := &exampleResource{ObjectMeta: metav1.ObjectMeta{Name: "parent"}}
	setConditions(parent,
		metav1.Condition{Type: "Initialized", Status: metav1.ConditionTrue, Reason: "Initialized"},
		metav1.Condition{Type: "AvailableHealth", Status: metav1.ConditionTrue, Reason: "HealthReady"},
	)
	worker := &exampleResource{ObjectMeta: metav1.ObjectMeta{Name: "worker"}}
	setConditions(worker,
		metav1.Condition{Type: "Initialized", Status: metav1.ConditionFalse, Reason: "NotYetAvailable"},
		metav1.Condition{Type: "Available", Status: metav1.ConditionFalse, Reason: "DependencyFailed", Message: "Required dependency could not initialize"},
		metav1.Condition{Type: "Synced", Status: metav1.ConditionFalse, Reason: "ProviderError", Message: "Provider unavailable"},
		metav1.Condition{Type: "UpToDate", Status: metav1.ConditionFalse, Reason: "Updating"},
	)
	synced := metav1.Condition{Type: "Synced", Status: metav1.ConditionTrue, Reason: "Synced"}
	available := metav1.Condition{Type: "Available", Status: metav1.ConditionTrue, Reason: "Available"}
	nonDegraded := metav1.Condition{Type: "NonDegraded", Status: metav1.ConditionTrue, Reason: "NonDegraded"}
	upToDate := metav1.Condition{Type: "UpToDate", Status: metav1.ConditionTrue, Reason: "UpToDate"}
	rules := InfraRules
	rules.Now = func() time.Time { return time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC) }

	// The call shape mirrors Infra; only the caller persists the changed object's status.
	changed, err := Aggregate(parent, map[string]Object{"Worker": worker}, rules, synced, available, nonDegraded, upToDate)
	if err != nil {
		panic(err)
	}
	fmt.Println("changed:", changed)
	for _, typ := range []string{"Available", "Initialized", "NonDegraded", "UpToDate", "Synced"} {
		condition, _ := Lookup(parent, typ)
		fmt.Printf("%s=%s, %s\n", typ, condition.Status, condition.Reason)
	}
	var presentation Presenter = InfraSummary{Inputs: InfraConditions, Classifier: InfraClassification, Messages: infraMessages}
	phase, _ := presentation.Present(parent)
	fmt.Println("phase:", phase)

	// Recovery replaces the child's supplied Conditions and keeps the parent's previous Conditions.
	setConditions(worker,
		metav1.Condition{Type: "Initialized", Status: metav1.ConditionTrue, Reason: "Initialized"},
		metav1.Condition{Type: "Available", Status: metav1.ConditionTrue, Reason: "Available"},
		metav1.Condition{Type: "Synced", Status: metav1.ConditionTrue, Reason: "Synced"},
		metav1.Condition{Type: "UpToDate", Status: metav1.ConditionTrue, Reason: "UpToDate"},
	)
	if _, err := Aggregate(parent, map[string]Object{"Worker": worker}, rules, synced, available, nonDegraded, upToDate); err != nil {
		panic(err)
	}
	phase, _ = presentation.Present(parent)
	fmt.Println("recovered:", phase)

	// Output:
	// changed: true
	// Available=True, Available
	// Initialized=True, Initialized
	// NonDegraded=False, WorkerFailed
	// UpToDate=False, WorkerNotSynced
	// Synced=True, Synced
	// phase: Degraded and updating
	// recovered: Ready
}

// END INVOCATION

func TestCompatibleCompositionAndRecovery(t *testing.T) {
	ready := ConditionType{"Ready", TrueIsGood}
	first := Aggregation{
		Output: ready,
		Parent: LocalBaseFirst{Base: ready},
		Children: ServingChildren{Inputs: InfraConditions, Condition: InfraConditions.Available,
			Classifier: ReasonClassifier{Reasons: map[string]Escalation{"DependencyFailed": Failed}}, False: Always{}, Failure: Always{}},
		Reduction: ParentChildReduction{Output: ready, BadSuffix: "NotReady"},
		Messages:  BoundedMessages{MaxLines: 7},
	}
	second := Aggregation{
		Output: ready,
		Parent: LocalBaseFirst{Base: ready},
		Children: ServingChildren{Inputs: InfraConditions, Condition: ready,
			Classifier: ReasonClassifier{Reasons: map[string]Escalation{"ChildrenFailed": Failed}}, False: Always{}, Failure: Always{}},
		Reduction: ParentChildReduction{Output: ready, BadSuffix: "NotReady"},
		Messages:  BoundedMessages{MaxLines: 1},
	}
	rulesA := Rules{Outputs: []Aggregation{first}, Now: fixtureNow}
	rulesB := Rules{Outputs: []Aggregation{second}, Now: fixtureNow}
	previous, previousGrandparent := &exampleResource{}, &exampleResource{}
	for _, healthy := range []bool{false, true} {
		input := metav1.Condition{Type: "Available", Status: metav1.ConditionFalse, Reason: "DependencyFailed", Message: "Required dependency could not initialize"}
		if healthy {
			input.Status, input.Reason, input.Message = metav1.ConditionTrue, "DependencyReady", ""
		}
		dependency := &exampleResource{Status: exampleStatus{Conditions: []metav1.Condition{input}}}
		other := &exampleResource{Status: exampleStatus{Conditions: []metav1.Condition{{Type: "Available", Status: metav1.ConditionTrue}, {Type: "Ready", Status: metav1.ConditionTrue}}}}
		base := metav1.Condition{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Ready"}
		desiredA, err := Evaluate(previous, map[string]Object{"Dependency": dependency, "Other": other}, rulesA, base)
		if err != nil {
			t.Fatal(err)
		}
		parent := &exampleResource{Status: exampleStatus{Conditions: desiredA}}
		desiredB, err := Evaluate(previousGrandparent, map[string]Object{"Parent": parent, "Other": other}, rulesB, base)
		if err != nil {
			t.Fatal(err)
		}
		wantStatus, wantReason := metav1.ConditionFalse, "ChildrenFailed"
		if healthy {
			wantStatus, wantReason = metav1.ConditionTrue, "Ready"
		}
		for _, conditions := range [][]metav1.Condition{desiredA, desiredB} {
			result := &exampleResource{Status: exampleStatus{Conditions: conditions}}
			out, ok := Lookup(result, "Ready")
			if !ok || out.Status != wantStatus || out.Reason != wantReason {
				t.Fatalf("healthy=%v: got %+v", healthy, out)
			}
		}
		setConditions(previous, desiredA...)
		setConditions(previousGrandparent, desiredB...)
	}
}

// Profile evaluation stages all outputs before applying any changes to the actual parent.
func TestInfraEvaluationAndFailedConfigurationDoNotMutate(t *testing.T) {
	fixtures, _ := loadFixtures(t)
	parent, children := fixtureObjects(fixtures[0])
	previous := append([]metav1.Condition(nil), parent.GetConditions()...)
	profile := Infra
	profile.Rules.Now = fixtureNow
	if _, err := profile.Evaluate(parent, children, fixtures[0].Bases...); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(previous, parent.GetConditions()) {
		t.Fatal("pure evaluation mutated parent")
	}
	profile.Rules.Outputs = append([]Aggregation(nil), profile.Rules.Outputs...)
	profile.Rules.Outputs[0] = Aggregation{}
	if changed, err := profile.Aggregate(parent, children, metav1.Condition{Type: "Synced", Status: metav1.ConditionTrue, Reason: "Synced"}, fixtures[0].Bases[0]); err == nil || changed {
		t.Fatalf("invalid configuration: changed=%v err=%v", changed, err)
	}
	if !reflect.DeepEqual(previous, parent.GetConditions()) {
		t.Fatal("failed evaluation partially changed parent")
	}
}

func TestInfraNilChildrenAndNoSync(t *testing.T) {
	parent := &exampleResource{}
	var absent *exampleResource
	children := map[string]Object{"Absent": absent, "Worker": &exampleResource{Status: exampleStatus{Conditions: []metav1.Condition{{Type: "Available", Status: metav1.ConditionTrue}}}}}
	result, err := InfraAvailability.Evaluate(parent, children, metav1.Condition{Type: "Available", Status: metav1.ConditionTrue, Reason: "Available"}, fixtureNow())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != metav1.ConditionTrue {
		t.Fatalf("typed nil or absent sync incorrectly blocked availability: %+v", result)
	}
}

// The generic entry point also evaluates unrelated names and a negative parent polarity.
func TestGenericAggregateUsesOnlySuppliedRules(t *testing.T) {
	output := ConditionType{"Faulted", FalseIsGood}
	input := ConditionType{"Accepting", TrueIsGood}
	rules := Rules{
		Outputs: []Aggregation{{
			Output: output,
			Parent: LocalBaseFirst{Base: output},
			Children: ServingChildren{Condition: input,
				Classifier: ReasonClassifier{Reasons: map[string]Escalation{"DependencyFailed": Failed}},
				False:      Always{}, Failure: Always{}},
			Reduction: ParentChildReduction{Output: output, BadSuffix: "Unavailable"},
			Messages:  BoundedMessages{MaxLines: 1},
		}},
		Now: fixtureNow,
	}
	parent := &exampleResource{}
	worker := &exampleResource{Status: exampleStatus{Conditions: []metav1.Condition{
		{Type: "Accepting", Status: metav1.ConditionFalse, Reason: "DependencyFailed"},
	}}}
	local := metav1.Condition{Type: "Faulted", Status: metav1.ConditionFalse, Reason: "Accepting"}
	children := map[string]Object{"Worker": worker}
	changed, err := Aggregate(parent, children, rules, local)
	if err != nil || !changed {
		t.Fatalf("failed input: changed=%v err=%v", changed, err)
	}
	condition, _ := Lookup(parent, "Faulted")
	if condition.Status != metav1.ConditionTrue || condition.Reason != "WorkerFailed" {
		t.Fatalf("failed input: %+v", condition)
	}
	if changed, err := Aggregate(parent, children, rules, local); err != nil || changed {
		t.Fatalf("identical input: changed=%v err=%v", changed, err)
	}
	setConditions(worker, metav1.Condition{Type: "Accepting", Status: metav1.ConditionTrue, Reason: "Accepting"})
	if changed, err := Aggregate(parent, children, rules, local); err != nil || !changed {
		t.Fatalf("healthy input: changed=%v err=%v", changed, err)
	}
	condition, _ = Lookup(parent, "Faulted")
	if condition.Status != metav1.ConditionFalse || condition.Reason != "Accepting" {
		t.Fatalf("healthy input: %+v", condition)
	}
	for _, typ := range []string{"Available", "Initialized", "Synced", "Deleting"} {
		if _, present := Lookup(parent, typ); present {
			t.Fatalf("generic call unexpectedly published Infra Condition %q", typ)
		}
	}
}
