// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package conditions_test

import (
	"bytes"
	"go/format"
	"os"
	"strings"
	"testing"

	"github.com/sttts/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func readmeSlice(parent conditions.Conditioned, childSlice []conditions.Object, Aggregation *conditions.Aggregation, localConditions ...metav1.Condition) error {
	// BEGIN slice
	changed, err := conditions.AggregateSlice(parent, childSlice, Aggregation, localConditions...)
	if err != nil {
		return err
	}
	if changed {
		// Persist the parent's status through your controller's client.
	}
	// END slice
	return nil
}

func readmeEvaluate(parent conditions.Conditioned, children map[string]conditions.Object, Aggregation *conditions.Aggregation, localConditions ...metav1.Condition) error {
	// BEGIN evaluate
	ready, err := conditions.Evaluate("Ready", parent, children, Aggregation, localConditions...)
	if err != nil {
		return err
	}
	if ready != nil {
		// Inspect or store *ready; Evaluate has not modified parent.
	}
	// END evaluate
	return nil
}

func readmeEvaluateAll(parent conditions.Conditioned, children map[string]conditions.Object, Aggregation *conditions.Aggregation, localConditions ...metav1.Condition) error {
	// BEGIN evaluate-all
	desired, err := conditions.EvaluateAggregation(parent, children, Aggregation, localConditions...)
	if err != nil {
		return err
	}
	// Evaluation did not mutate parent. Apply only when your controller is ready.
	changed := parent.SetConditions(desired)
	if changed {
		// Persist the updated status.
	}
	// END evaluate-all
	return nil
}

func TestReadmeOperations(t *testing.T) {
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("readme_operations_test.go")
	if err != nil {
		t.Fatal(err)
	}
	extract := func(text, start, end string) string {
		t.Helper()
		_, rest, ok := strings.Cut(text, start)
		if !ok {
			t.Fatalf("missing marker %q", start)
		}
		value, _, ok := strings.Cut(rest, end)
		if !ok {
			t.Fatalf("missing marker %q", end)
		}
		return value
	}
	normalize := func(body string) []byte {
		t.Helper()
		formatted, err := format.Source([]byte("package example\nfunc example() error {\n" + strings.TrimSpace(body) + "\nreturn nil\n}"))
		if err != nil {
			t.Fatal(err)
		}
		return formatted
	}
	for _, name := range []string{"slice", "evaluate", "evaluate-all"} {
		documented := extract(string(readme), "<!-- example:"+name+" -->\n```go\n", "\n```\n<!-- /example:"+name+" -->")
		compiled := extract(string(source), "// BEGIN "+name+"\n", "// END "+name+"\n")
		if !bytes.Equal(normalize(documented), normalize(compiled)) {
			t.Errorf("README %s differs from compiled example", name)
		}
	}
	a := conditions.Define(conditions.Definition{Outputs: []conditions.OutputDefinition{conditions.All("Ready", conditions.Spec{Polarity: conditions.TrueIsGood})}})
	parent := &object{}
	if err := readmeSlice(parent, nil, a); err != nil {
		t.Fatal(err)
	}
	if err := readmeEvaluate(parent, nil, a); err != nil {
		t.Fatal(err)
	}
	if err := readmeEvaluateAll(parent, nil, a); err != nil {
		t.Fatal(err)
	}
	for _, f := range []func() error{
		func() error { return readmeSlice(parent, nil, nil) },
		func() error { return readmeEvaluate(parent, nil, nil) },
		func() error { return readmeEvaluateAll(parent, nil, nil) },
	} {
		if err := f(); err == nil {
			t.Error("example swallowed error")
		}
	}
}
