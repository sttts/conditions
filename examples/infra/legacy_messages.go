// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package infra

import (
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DefaultConditionMessageMaxLines controls the default max lines for formatted
// aggregate condition messages, including the summary line.
const DefaultConditionMessageMaxLines = 7

// ChildStatus represents a child’s display label and its message.
// Display is a concise identifier (e.g., Kind/Name, or Kind) used in the
// details block. Message may be multi-line and may end with a trailing ':'
// to indicate nested details in subsequent lines.
type ChildStatus struct {
	Display string
	Message string
}

// FormatAggregateMessage renders a summary with an optional details block of
// children. When children are present, the expanded form ends the summary line
// with a trailing ':' and places each child on its own line, followed by any
// nested lines from the child message, indented by two additional spaces.
//
// The result respects a line budget (maxLines). The formatter first attempts a
// full rendering. If the full message exceeds the budget, the formatter
// iteratively condenses the deepest nodes by rewriting their blocks into single
// lines (using chaining for single-child branches and ellipses for detail-only
// branches) while re-evaluating the overall line count after each condensation.
// When no further condensation is possible, it falls back to a single-line
// summary that mirrors the chaining rules (branch-degree aware inline output).
func FormatAggregateMessage(summaryHeads []string, children []ChildStatus, maxLines int) string {
	heads := normalizeHeads(summaryHeads)
	if maxLines <= 0 {
		maxLines = DefaultConditionMessageMaxLines
	}

	nodes := buildChildNodes(children)
	if len(nodes) == 0 {
		return limitMessageSize(joinSummaryHeads(heads, false))
	}

	if maxLines == 1 {
		return limitMessageSize(renderInline(heads, nodes))
	}

	working := cloneNodes(nodes)
	childBudget := maxLines - 1
	if childBudget < 0 {
		childBudget = 0
	}

	condenseNodes(working, childBudget)
	total := totalLines(working)
	if total > childBudget {
		inline := renderInline(heads, working)
		if lineCount(inline) <= maxLines {
			return limitMessageSize(inline)
		}
		return limitMessageSize(joinSummaryHeads(heads, false))
	}

	return limitMessageSize(renderMultiline(heads, working))
}

func normalizeHeads(heads []string) []string {
	normalized := make([]string, 0, len(heads))
	for _, head := range heads {
		trimmed := strings.TrimSpace(head)
		trimmed = strings.TrimSuffix(trimmed, ":")
		trimmed = strings.TrimSpace(trimmed)
		if trimmed != "" {
			normalized = append(normalized, trimmed)
		}
	}
	if len(normalized) == 0 {
		normalized = append(normalized, "")
	}
	return normalized
}

func joinSummaryHeads(heads []string, ensureColon bool) string {
	summary := strings.TrimSpace(strings.Join(heads, ": "))
	if !ensureColon {
		return summary
	}
	if summary == "" {
		return summary
	}
	if strings.HasSuffix(summary, ":") {
		return summary
	}
	return summary + ":"
}

func renderMultiline(heads []string, nodes []*messageNode) string {
	summary := joinSummaryHeads(heads, len(nodes) > 0)
	if len(nodes) == 0 {
		return strings.TrimSuffix(summary, ":")
	}

	var b strings.Builder
	b.WriteString(summary)
	for _, node := range nodes {
		writeNode(&b, node, 1)
	}
	return b.String()
}

func renderInline(heads []string, nodes []*messageNode) string {
	summary := joinSummaryHeads(heads, false)
	summary = strings.TrimSuffix(summary, ":")
	summary = strings.TrimSpace(summary)

	if len(nodes) == 0 {
		return summary
	}

	if len(nodes) == 1 {
		chain := nodes[0].inlineChain()
		if summary == "" {
			return chain
		}
		if chain == "" {
			return summary
		}
		return summary + ": " + chain
	}

	names := make([]string, 0, len(nodes))
	summaryLower := strings.ToLower(summary)
	allContained := true
	for _, node := range nodes {
		name := strings.TrimSpace(node.Display)
		if name == "" {
			allContained = false
			continue
		}
		names = append(names, name)
		if !strings.Contains(summaryLower, strings.ToLower(name)) {
			allContained = false
		}
	}

	if len(names) == 0 {
		return summary
	}
	if allContained {
		return summary
	}
	return summary + ": " + strings.Join(names, ", ")
}

// condenseNodes mutates nodes in-place so that their total line usage fits
// within childBudget lines (excluding the summary line). It condenses deepest
// nodes first and stops early when the budget is met or no further condensation
// is possible.
func condenseNodes(nodes []*messageNode, childBudget int) {
	if childBudget < 0 {
		childBudget = 0
	}
	for totalLines(nodes) > childBudget {
		candidate := selectCondenseCandidate(nodes)
		if candidate == nil {
			break
		}
		candidate.condenseToSingleLine()
	}
}

func selectCondenseCandidate(nodes []*messageNode) *messageNode {
	var best *messageNode
	bestDepth := -1
	bestSavings := -1

	var walk func(n *messageNode, depth int)
	walk = func(n *messageNode, depth int) {
		savings := n.lineCount() - 1
		if savings > 0 {
			if depth > bestDepth || (depth == bestDepth && savings > bestSavings) {
				best = n
				bestDepth = depth
				bestSavings = savings
			}
		}
		for _, child := range n.Children {
			walk(child, depth+1)
		}
	}

	for _, node := range nodes {
		walk(node, 1)
	}
	return best
}

func totalLines(nodes []*messageNode) int {
	total := 0
	for _, node := range nodes {
		total += node.lineCount()
	}
	return total
}

func cloneNodes(nodes []*messageNode) []*messageNode {
	if len(nodes) == 0 {
		return nil
	}
	cloned := make([]*messageNode, len(nodes))
	for i, node := range nodes {
		cloned[i] = node.clone()
	}
	return cloned
}

func writeNode(b *strings.Builder, node *messageNode, depth int) {
	b.WriteString("\n")
	b.WriteString(strings.Repeat(" ", depth*2))
	display := strings.TrimSpace(node.Display)
	summary := strings.TrimSpace(node.Summary)

	if display != "" {
		b.WriteString(display)
		if summary != "" {
			b.WriteString(": ")
			b.WriteString(summary)
		} else {
			b.WriteString(":")
		}
	} else {
		b.WriteString(summary)
	}

	for _, child := range node.Children {
		writeNode(b, child, depth+1)
	}
}

func buildChildNodes(children []ChildStatus) []*messageNode {
	nodes := make([]*messageNode, 0, len(children))
	for _, child := range children {
		lines := normalizeLines(child.Message)
		first := ""
		rest := []string{}
		if len(lines) > 0 {
			first = strings.TrimSpace(lines[0])
			if len(lines) > 1 {
				rest = lines[1:]
			}
		}
		node := &messageNode{
			Display:  strings.TrimSpace(child.Display),
			Summary:  first,
			Children: parseMessageChildren(rest),
		}
		nodes = append(nodes, node)
	}
	return nodes
}

func normalizeLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	return lines
}

type messageNode struct {
	Display  string
	Summary  string
	Children []*messageNode
}

func (n *messageNode) clone() *messageNode {
	if n == nil {
		return nil
	}
	clone := &messageNode{
		Display: n.Display,
		Summary: n.Summary,
	}
	if len(n.Children) > 0 {
		clone.Children = make([]*messageNode, len(n.Children))
		for i, child := range n.Children {
			clone.Children[i] = child.clone()
		}
	}
	return clone
}

func (n *messageNode) lineCount() int {
	if n == nil {
		return 0
	}
	total := 1
	for _, child := range n.Children {
		total += child.lineCount()
	}
	return total
}

func (n *messageNode) structChildren() []*messageNode {
	var result []*messageNode
	for _, child := range n.Children {
		if strings.TrimSpace(child.Display) != "" {
			result = append(result, child)
		}
	}
	return result
}

func (n *messageNode) detailChildren() []*messageNode {
	var result []*messageNode
	for _, child := range n.Children {
		if strings.TrimSpace(child.Display) == "" {
			result = append(result, child)
		}
	}
	return result
}

func (n *messageNode) condenseToSingleLine() {
	structChildren := n.structChildren()
	detailChildren := n.detailChildren()
	summary := strings.TrimSpace(n.Summary)

	switch len(structChildren) {
	case 0:
		if len(detailChildren) > 0 {
			summary = addEllipsis(summary)
		} else if strings.HasSuffix(summary, ":") {
			summary = strings.TrimSuffix(summary, ":")
		}
	case 1:
		child := structChildren[0]
		child.condenseToSingleLine()
		childText := strings.TrimSpace(child.singleLineText())
		summary = ensureTrailingColon(summary)
		summary = strings.TrimSuffix(summary, ":")
		if childText != "" {
			summary = summary + ": " + childText
		}
	default:
		summary = strings.TrimSuffix(summary, ":")
	}

	n.Summary = summary
	n.Children = nil
}

func (n *messageNode) inlineChain() string {
	if n == nil {
		return ""
	}
	text := strings.TrimSpace(n.singleLineText())
	structChildren := n.structChildren()
	if len(structChildren) == 1 {
		child := structChildren[0].clone()
		child.condenseToSingleLine()
		childText := strings.TrimSpace(child.singleLineText())
		base := strings.TrimSuffix(text, ":")
		base = strings.TrimSpace(base)
		if childText == "" {
			return base
		}
		if base == "" {
			return childText
		}
		return base + ": " + childText
	}
	return text
}

func (n *messageNode) singleLineText() string {
	summary := strings.TrimSpace(n.Summary)
	display := strings.TrimSpace(n.Display)
	if display == "" {
		return summary
	}
	if summary == "" {
		return display
	}
	return display + ": " + summary
}

func parseMessageChildren(lines []string) []*messageNode {
	if len(lines) == 0 {
		return nil
	}
	type entry struct {
		indent int
		text   string
	}

	entries := make([]entry, 0, len(lines))
	for _, raw := range lines {
		if raw == "" {
			continue
		}
		indent := countLeadingSpaces(raw)
		text := strings.TrimSpace(raw)
		if text == "" {
			continue
		}
		entries = append(entries, entry{indent: indent, text: text})
	}

	if len(entries) == 0 {
		return nil
	}

	root := &messageNode{}
	stack := []struct {
		indent int
		node   *messageNode
	}{{indent: -1, node: root}}

	for _, entry := range entries {
		display, summary, hasDisplay := splitDisplayAndSummary(entry.text)
		child := &messageNode{Summary: summary}
		if hasDisplay {
			child.Display = display
		}

		for len(stack) > 0 && entry.indent <= stack[len(stack)-1].indent {
			stack = stack[:len(stack)-1]
		}
		parent := stack[len(stack)-1].node
		parent.Children = append(parent.Children, child)
		stack = append(stack, struct {
			indent int
			node   *messageNode
		}{indent: entry.indent, node: child})
	}

	return root.Children
}

func splitDisplayAndSummary(text string) (string, string, bool) {
	idx := strings.Index(text, ":")
	if idx <= 0 {
		return "", text, false
	}
	display := strings.TrimSpace(text[:idx])
	summary := strings.TrimSpace(text[idx+1:])
	if display == "" {
		return "", text, false
	}
	return display, summary, true
}

func countLeadingSpaces(s string) int {
	count := 0
	for _, r := range s {
		if r != ' ' {
			break
		}
		count++
	}
	return count
}

func ensureTrailingColon(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return trimmed
	}
	if strings.HasSuffix(trimmed, ":") {
		return trimmed
	}
	return trimmed + ":"
}

func addEllipsis(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "…"
	}
	if strings.HasSuffix(trimmed, "…") {
		return trimmed
	}
	return trimmed + " …"
}

func lineCount(s string) int {
	if s == "" {
		return 0
	}
	return 1 + strings.Count(s, "\n")
}

// InlineMessage re-derives the single-line inline form from a (possibly
// multiline) condition message. When maxChars > 0 it builds the chain
// incrementally, only appending the next level if there is space. If the
// message is single-line and exceeds the budget, it is returned as-is
// (no hard truncation).
func InlineMessage(msg string, maxChars int) string {
	if msg == "" {
		return ""
	}

	// split into first line (summary head) and remainder
	head := msg
	var remainder []string
	if i := strings.Index(msg, "\n"); i >= 0 {
		head = strings.TrimSpace(msg[:i])
		remainder = strings.Split(msg[i+1:], "\n")
	}

	// strip trailing colon/dot from head
	headClean := strings.TrimRight(strings.TrimSpace(head), ".:")
	headClean = strings.TrimSpace(headClean)

	nodes := parseMessageChildren(remainder)
	if len(nodes) == 0 {
		return headClean
	}

	return inlineWithBudget(headClean, nodes, maxChars)
}

// inlineWithBudget builds the inline chain, only appending the next level
// if appending it (plus "...") still fits within maxChars. When maxChars <= 0,
// no budget is enforced.
func inlineWithBudget(prefix string, nodes []*messageNode, maxChars int) string {
	if len(nodes) == 0 {
		return prefix
	}

	// multiple children: list display names
	if len(nodes) > 1 {
		return namesWithBudget(prefix, nodes, maxChars)
	}

	// single child: walk the chain
	return chainWithBudget(prefix, nodes[0], maxChars)
}

func chainWithBudget(prefix string, node *messageNode, maxChars int) string {
	// split the node into ": "-separated segments (display, then summary parts)
	segments := nodeSegments(node)
	result := prefix
	for _, seg := range segments {
		candidate := result + ": " + seg
		if maxChars > 0 && len(candidate) > maxChars {
			return result
		}
		result = candidate
	}

	// all node segments fit, recurse into children
	structChildren := node.structChildren()
	switch len(structChildren) {
	case 0:
		return result
	case 1:
		return chainWithBudget(result, structChildren[0], maxChars)
	default:
		return namesWithBudget(result, structChildren, maxChars)
	}
}

func nodeSegments(node *messageNode) []string {
	display := strings.TrimSpace(node.Display)
	summary := strings.TrimRight(strings.TrimSpace(node.Summary), ":")
	summary = strings.TrimSpace(summary)

	var segments []string
	if display != "" {
		segments = append(segments, display)
	}
	if summary != "" {
		segments = append(segments, strings.Split(summary, ": ")...)
	}
	return segments
}

func namesWithBudget(prefix string, nodes []*messageNode, maxChars int) string {
	names := make([]string, 0, len(nodes))
	for _, n := range nodes {
		name := strings.TrimSpace(n.Display)
		if name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return prefix
	}

	// check if all names are already contained in the prefix
	prefixLower := strings.ToLower(prefix)
	allContained := true
	for _, name := range names {
		if !strings.Contains(prefixLower, strings.ToLower(name)) {
			allContained = false
			break
		}
	}
	if allContained {
		return prefix
	}

	// append names one by one while they fit
	result := prefix + ": " + names[0]
	if maxChars > 0 && len(result) > maxChars {
		return prefix
	}
	for _, name := range names[1:] {
		candidate := result + ", " + name
		if maxChars > 0 && len(candidate) > maxChars {
			return result + "..."
		}
		result = candidate
	}
	return result
}

// InfraMessages opts this reference profile into the pre-existing Infra message grammar.
type InfraMessages struct{ MaxLines, InlineChars int }

func (p InfraMessages) Format(summary string, causes []Cause) string {
	children := make([]ChildStatus, 0, len(causes))
	for _, cause := range causes {
		children = append(children, ChildStatus{Display: cause.Reference, Message: cause.Message})
	}
	return FormatAggregateMessage([]string{summary}, children, p.MaxLines)
}
func (p InfraMessages) Inline(condition metav1.Condition) string {
	message := condition.Message
	if message == "" {
		message = condition.Reason
	}
	limit := p.InlineChars
	if limit <= 0 {
		limit = 80
	}
	return InlineMessage(message, limit)
}

// limitMessageSize keeps the legacy ASCII behavior and preserves complete UTF-8 characters.
func limitMessageSize(message string) string {
	return (BoundedMessages{MaxBytes: 32768}).Format(message, nil)
}
