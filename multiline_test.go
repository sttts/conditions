// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package conditions_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	c "github.com/sttts/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func ExampleMultilineMessages() {
	var format c.Formatter = c.MultilineMessages{MaxLines: 7}
	child := format.Format("bootstrap failed", []c.Cause{{Reference: "Job/init", Message: "image pull failed"}})
	message := format.Format("service unavailable", []c.Cause{{Reference: "Worker", Message: child}})
	fmt.Println(message)
	fmt.Println(format.Inline(metav1.Condition{Message: message}))
	// Output:
	// service unavailable:
	//   Worker: bootstrap failed:
	//     Job/init: image pull failed
	// service unavailable: Worker: bootstrap failed: Job/init: image pull failed
}

func TestMultilineFormatPromotionAndCondensation(t *testing.T) {
	causes := []c.Cause{{Reference: "Worker", Message: "bootstrap failed:\n  Job/init: image pull failed"}}
	before := append([]c.Cause(nil), causes...)
	for _, test := range []struct {
		name  string
		lines int
		want  string
	}{
		{"expanded", 7, "service unavailable:\n  Worker: bootstrap failed:\n    Job/init: image pull failed"},
		{"condensed", 2, "service unavailable:\n  Worker: bootstrap failed: Job/init: image pull failed"},
		{"inline", 1, "service unavailable: Worker: bootstrap failed: Job/init: image pull failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var format c.Formatter = c.MultilineMessages{MaxLines: test.lines}
			if got := format.Format("service unavailable", causes); got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
			if !reflect.DeepEqual(causes, before) {
				t.Fatal("formatting changed the supplied causes")
			}
		})
	}
}

func TestMultilineFormatMessageSizeAndLineEndings(t *testing.T) {
	format := c.MultilineMessages{}
	want := "service unavailable:\n  Worker: bootstrap failed:\n    Job/init: image pull failed"
	got := format.Format("service unavailable", []c.Cause{{Reference: "Worker", Message: "bootstrap failed:\r\n  Job/init: image pull failed"}})
	if got != want {
		t.Fatalf("CRLF promotion: got %q, want %q", got, want)
	}
	message := format.Format("service unavailable", []c.Cause{{Reference: "Worker", Message: strings.Repeat("故", 12000)}})
	if len(message) > 32768 || !utf8.ValidString(message) || !strings.HasSuffix(message, "...") {
		t.Fatal("message truncation exceeded the byte budget, split UTF-8, or lost its omission marker")
	}
}

func TestMessageFormatsAreExplicit(t *testing.T) {
	causes := []c.Cause{{Reference: "Worker", Message: "bootstrap failed:\n  Job/init: image pull failed"}}
	opaque := (c.BoundedMessages{MaxLines: 1}).Format("service unavailable", causes)
	structured := (c.MultilineMessages{MaxLines: 1}).Format("service unavailable", causes)
	if opaque != "service unavailable (2 detail lines omitted)" {
		t.Fatalf("opaque format interpreted the source text: %q", opaque)
	}
	if structured != "service unavailable: Worker: bootstrap failed: Job/init: image pull failed" {
		t.Fatalf("multiline format did not retain the structured cause: %q", structured)
	}
}

func TestMultilineInlineBudgetCountsCharacters(t *testing.T) {
	format := c.MultilineMessages{InlineChars: 13}
	message := metav1.Condition{Message: "不可用:\n  节点: 启动失败"}
	if got := format.Inline(message); got != "不可用: 节点: 启动失败" {
		t.Fatalf("counted UTF-8 bytes instead of characters: %q", got)
	}
}
