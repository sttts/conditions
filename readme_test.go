// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package conditions_test

import (
	"os"
	"strings"
	"testing"

	c "github.com/sttts/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Keep the README's diagnostic examples as actual output fixtures.
func TestReadmeFormatting(t *testing.T) {
	data, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	_, section, ok := strings.Cut(string(data), "## Formatting with examples\n")
	if !ok {
		t.Fatal("missing formatting section")
	}
	section, _, _ = strings.Cut(section, "## Custom Go policies\n")
	var documented []string
	for {
		_, rest, ok := strings.Cut(section, "```text\n")
		if !ok {
			break
		}
		block, tail, ok := strings.Cut(rest, "\n```")
		if !ok {
			t.Fatal("unclosed text block")
		}
		documented = append(documented, block)
		section = tail
	}
	summary := "service unavailable"
	causes := []c.Cause{{Reference: "Worker", Message: "bootstrap failed:\n  Job/init: image pull failed"}}
	multi := c.MultilineMessages{MaxLines: 7, InlineChars: 40}
	bounded := c.BoundedMessages{MaxLines: 7, InlineChars: 40}
	actual := []string{
		multi.Format(summary, causes),
		(c.MultilineMessages{MaxLines: 2}).Format(summary, causes),
		(c.MultilineMessages{MaxLines: 1}).Format(summary, causes),
		multi.Inline(metav1.Condition{Message: multi.Format(summary, causes)}),
		bounded.Format(summary, causes),
		(c.BoundedMessages{MaxLines: 2}).Format(summary, causes),
		(c.BoundedMessages{MaxLines: 1}).Format(summary, causes),
		bounded.Inline(metav1.Condition{Message: bounded.Format(summary, causes)}),
	}
	if len(actual) != len(documented) {
		t.Fatalf("got %d README blocks; want %d", len(documented), len(actual))
	}
	for i := range actual {
		if actual[i] != documented[i] {
			t.Errorf("format example %d:\ngot:\n%s\nREADME:\n%s", i, actual[i], documented[i])
		}
	}
}
