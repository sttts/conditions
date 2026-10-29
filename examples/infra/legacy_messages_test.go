// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package infra

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormatAggregateMessage_ExampleA(t *testing.T) {
	summary := []string{"Cluster is not available", "unavailable children: EKSAcceleratedCluster, and not synced: EKSManagementCluster"}
	children := []ChildStatus{
		{
			Display: "EKSAcceleratedCluster",
			Message: "something something, this could be\na multiline message too, one that\nis very long (SomeReason)",
		},
		{
			Display: "EKSManagementCluster",
			Message: "not synced (AnotherReason):\nsome more message that could be cut off because there is the colon in the line above.",
		},
	}

	expectedFull := "Cluster is not available: unavailable children: EKSAcceleratedCluster, and not synced: EKSManagementCluster:\n" +
		"  EKSAcceleratedCluster: something something, this could be\n" +
		"    a multiline message too, one that\n" +
		"    is very long (SomeReason)\n" +
		"  EKSManagementCluster: not synced (AnotherReason):\n" +
		"    some more message that could be cut off because there is the colon in the line above."

	actualFull := FormatAggregateMessage(summary, children, 0)
	require.Equal(t, expectedFull, actualFull)

	expectedInline := "Cluster is not available: unavailable children: EKSAcceleratedCluster, and not synced: EKSManagementCluster"
	actualInline := FormatAggregateMessage(summary, children, 1)
	require.Equal(t, expectedInline, actualInline)
}

func TestFormatAggregateMessage_ExampleB(t *testing.T) {
	summary := []string{"Storage health degraded"}
	children := []ChildStatus{
		{
			Display: "StorageCluster",
			Message: "capacity low, investigating\nqueue length high\ndisks nearing wearout",
		},
	}

	expectedExpanded := "Storage health degraded:\n" +
		"  StorageCluster: capacity low, investigating\n" +
		"    queue length high\n" +
		"    disks nearing wearout"
	actualExpanded := FormatAggregateMessage(summary, children, 0)
	require.Equal(t, expectedExpanded, actualExpanded)

	expectedCondensed := "Storage health degraded:\n" +
		"  StorageCluster: capacity low, investigating …"
	actualCondensed := FormatAggregateMessage(summary, children, 2)
	require.Equal(t, expectedCondensed, actualCondensed)
}

func TestFormatAggregateMessage_ExampleC(t *testing.T) {
	summary := []string{"Cluster is not available", "children degraded"}
	children := []ChildStatus{
		{
			Display: "StorageCluster",
			Message: "rebuilding pool:\nDiskPoolA: waiting for replacement",
		},
		{
			Display: "NetworkCluster",
			Message: "not synced:\nRouterA: pending reboot",
		},
		{
			Display: "ComputeCluster",
			Message: "updating nodes:\nNodeGroupA: drained\nNodeGroupB: waiting",
		},
	}

	expectedExpanded := "Cluster is not available: children degraded:\n" +
		"  StorageCluster: rebuilding pool:\n" +
		"    DiskPoolA: waiting for replacement\n" +
		"  NetworkCluster: not synced:\n" +
		"    RouterA: pending reboot\n" +
		"  ComputeCluster: updating nodes:\n" +
		"    NodeGroupA: drained\n" +
		"    NodeGroupB: waiting"
	actualExpanded := FormatAggregateMessage(summary, children, 8)
	require.Equal(t, expectedExpanded, actualExpanded)

	expectedCondensed := "Cluster is not available: children degraded:\n" +
		"  StorageCluster: rebuilding pool:\n" +
		"    DiskPoolA: waiting for replacement\n" +
		"  NetworkCluster: not synced:\n" +
		"    RouterA: pending reboot\n" +
		"  ComputeCluster: updating nodes"
	actualCondensed := FormatAggregateMessage(summary, children, 7)
	require.Equal(t, expectedCondensed, actualCondensed)
}

func TestFormatAggregateMessage_ExampleD(t *testing.T) {
	summary := []string{"Root is unhealthy", "child cluster not available"}
	children := []ChildStatus{
		{
			Display: "LitmusCluster",
			Message: "waiting on bootstrap:\nWardenCluster: draining nodes (PendingInit)",
		},
	}

	expectedExpanded := "Root is unhealthy: child cluster not available:\n" +
		"  LitmusCluster: waiting on bootstrap:\n" +
		"    WardenCluster: draining nodes (PendingInit)"
	actualExpanded := FormatAggregateMessage(summary, children, 0)
	require.Equal(t, expectedExpanded, actualExpanded)

	expectedInline := "Root is unhealthy: child cluster not available: LitmusCluster: waiting on bootstrap: WardenCluster: draining nodes (PendingInit)"
	actualInline := FormatAggregateMessage(summary, children, 1)
	require.Equal(t, expectedInline, actualInline)
}

func TestFormatAggregateMessage_ExampleE(t *testing.T) {
	summary := []string{"Level1 is unhealthy, children are broken"}
	children := []ChildStatus{
		{
			Display: "Level2ObjectA",
			Message: "children are broken:\nLevel3ObjectA: unhealthy (ReasonX)",
		},
		{
			Display: "Level2ObjectB",
			Message: "something something (ReasonY)",
		},
	}

	expectedExpanded := "Level1 is unhealthy, children are broken:\n" +
		"  Level2ObjectA: children are broken:\n" +
		"    Level3ObjectA: unhealthy (ReasonX)\n" +
		"  Level2ObjectB: something something (ReasonY)"
	actualExpanded := FormatAggregateMessage(summary, children, 0)
	require.Equal(t, expectedExpanded, actualExpanded)

	expectedThreeLines := "Level1 is unhealthy, children are broken:\n" +
		"  Level2ObjectA: children are broken: Level3ObjectA: unhealthy (ReasonX)\n" +
		"  Level2ObjectB: something something (ReasonY)"
	actualThreeLines := FormatAggregateMessage(summary, children, 3)
	require.Equal(t, expectedThreeLines, actualThreeLines)

	expectedInline := "Level1 is unhealthy, children are broken: Level2ObjectA, Level2ObjectB"
	actualInline := FormatAggregateMessage(summary, children, 1)
	require.Equal(t, expectedInline, actualInline)
}

func TestFormatAggregateMessage_ExampleF(t *testing.T) {
	summary := []string{"Level1 routing degraded"}
	regionMessage := "unavailable children: Level3ZoneA, and not synced: Level3ZoneB\n" +
		"  Level3ZoneA: zone does not respond (PingTimeout)\n" +
		"  Level3ZoneB: children unavailable:\n" +
		"    Level4ZoneA: failed to apply child configuration (Invalid)"
	children := []ChildStatus{
		{
			Display: "Level2Region",
			Message: regionMessage,
		},
	}

	expectedExpanded := "Level1 routing degraded:\n" +
		"  Level2Region: unavailable children: Level3ZoneA, and not synced: Level3ZoneB\n" +
		"    Level3ZoneA: zone does not respond (PingTimeout)\n" +
		"    Level3ZoneB: children unavailable:\n" +
		"      Level4ZoneA: failed to apply child configuration (Invalid)"
	actualExpanded := FormatAggregateMessage(summary, children, 0)
	require.Equal(t, expectedExpanded, actualExpanded)

	expectedFourLines := "Level1 routing degraded:\n" +
		"  Level2Region: unavailable children: Level3ZoneA, and not synced: Level3ZoneB\n" +
		"    Level3ZoneA: zone does not respond (PingTimeout)\n" +
		"    Level3ZoneB: children unavailable: Level4ZoneA: failed to apply child configuration (Invalid)"
	actualFourLines := FormatAggregateMessage(summary, children, 4)
	require.Equal(t, expectedFourLines, actualFourLines)

	expectedInline := "Level1 routing degraded: Level2Region: unavailable children: Level3ZoneA, and not synced: Level3ZoneB"
	actualInline := FormatAggregateMessage(summary, children, 1)
	require.Equal(t, expectedInline, actualInline)
}

func TestInlineMessage(t *testing.T) {
	tests := []struct {
		name     string
		msg      string
		maxChars int
		expected string
	}{
		{
			name:     "empty message",
			msg:      "",
			maxChars: 80,
			expected: "",
		},
		{
			name:     "single line, fits",
			msg:      "something went wrong",
			maxChars: 80,
			expected: "something went wrong",
		},
		{
			name:     "single line, returned as-is",
			msg:      "this is a very long message that exceeds the maximum character limit for the printer column output",
			maxChars: 40,
			expected: "this is a very long message that exceeds the maximum character limit for the printer column output",
		},
		{
			name: "multiline single child, chains inline",
			msg: "unavailable children:\n" +
				"  EKSAcceleratedCluster: is not available: state is still unknown",
			maxChars: 0,
			expected: "unavailable children: EKSAcceleratedCluster: is not available: state is still unknown",
		},
		{
			name: "multiline single child, stops at last fitting segment",
			msg: "unavailable children:\n" +
				"  EKSAcceleratedCluster: is not available: state is still unknown",
			maxChars: 63,
			expected: "unavailable children: EKSAcceleratedCluster: is not available",
		},
		{
			name: "multiline single child, tight budget stops at display",
			msg: "unavailable children:\n" +
				"  EKSAcceleratedCluster: is not available: state is still unknown",
			maxChars: 45,
			expected: "unavailable children: EKSAcceleratedCluster",
		},
		{
			name: "multiline multiple children, lists names",
			msg: "unavailable children:\n" +
				"  EKSAcceleratedCluster: is not available\n" +
				"  EKSManagementCluster: not synced",
			maxChars: 0,
			expected: "unavailable children: EKSAcceleratedCluster, EKSManagementCluster",
		},
		{
			name: "multiline multiple children, stops at last fitting name",
			msg: "unavailable children:\n" +
				"  EKSAcceleratedCluster: is not available\n" +
				"  EKSManagementCluster: not synced",
			maxChars: 50,
			expected: "unavailable children: EKSAcceleratedCluster...",
		},
		{
			name:     "trailing colon stripped",
			msg:      "something happened:",
			maxChars: 80,
			expected: "something happened",
		},
		{
			name: "deep chain flattened",
			msg: "child cluster not available:\n" +
				"  LitmusCluster: waiting on bootstrap:\n" +
				"    WardenCluster: draining nodes (PendingInit)",
			maxChars: 0,
			expected: "child cluster not available: LitmusCluster: waiting on bootstrap: WardenCluster: draining nodes (PendingInit)",
		},
		{
			name: "deep chain with budget stops at right level",
			msg: "child cluster not available:\n" +
				"  LitmusCluster: waiting on bootstrap:\n" +
				"    WardenCluster: draining nodes (PendingInit)",
			maxChars: 80,
			expected: "child cluster not available: LitmusCluster: waiting on bootstrap: WardenCluster",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := InlineMessage(tc.msg, tc.maxChars)
			require.Equal(t, tc.expected, actual)
		})
	}
}
