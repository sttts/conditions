// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package conditions

import (
	"fmt"
	"strings"
	"unicode/utf8"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Formatter renders diagnostics separately from semantic selection and reduction.
type Formatter interface {
	Format(summary string, causes []Cause) string
	Inline(condition metav1.Condition) string
}

// Cause retains source text and its diagnostic provenance.
type Cause struct {
	Reference string
	Message   string
}

// BoundedMessages renders causes as opaque text with explicit omission notices.
type BoundedMessages struct{ MaxLines, MaxBytes, InlineChars int }

func (p BoundedMessages) Format(summary string, causes []Cause) string {
	limit := p.MaxLines
	if limit < 1 {
		limit = 7
	}
	if len(causes) == 0 {
		return p.limit(summary)
	}
	lines := []string{strings.TrimSuffix(summary, ":") + ":"}
	for _, c := range causes {
		parts := strings.Split(strings.ReplaceAll(c.Message, "\r\n", "\n"), "\n")
		lines = append(lines, "  "+c.Reference+": "+parts[0])
		for _, part := range parts[1:] {
			lines = append(lines, "    "+part)
		}
	}
	if len(lines) > limit {
		omitted := len(lines) - limit + 1
		if limit == 1 {
			return p.limit(fmt.Sprintf("%s (%d detail lines omitted)", summary, len(lines)-1))
		}
		lines = append(lines[:limit-1], fmt.Sprintf("  … %d detail lines omitted", omitted))
	}
	return p.limit(strings.Join(lines, "\n"))
}
func (p BoundedMessages) Inline(c metav1.Condition) string {
	text := c.Message
	if text == "" {
		text = c.Reason
	}
	text = strings.SplitN(text, "\n", 2)[0]
	limit := p.InlineChars
	if limit < 1 {
		limit = 80
	}
	runes := []rune(text)
	if len(runes) > limit {
		return string(runes[:limit-1]) + "…"
	}
	return text
}
func (p BoundedMessages) limit(text string) string {
	limit := p.MaxBytes
	if limit < 4 {
		limit = 32768
	}
	if len(text) <= limit {
		return text
	}
	end := limit - 3
	for end > 0 && !utf8.ValidString(text[:end]) {
		end--
	}
	return text[:end] + "..."
}

var (
	_ Formatter = BoundedMessages{}
	_ Formatter = MultilineMessages{}
)
