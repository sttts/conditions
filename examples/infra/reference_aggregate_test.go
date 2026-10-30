// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Reference cases are adapted from the Infra conditions suite at revision 309d39e1634937e13e1c920493606dfbc05e506d.
// Original case names and expected semantic/message values are retained; fixture objects use the standard accessors.
package infra

import (
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var parentTime = metav1.NewTime(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))

func TestReferenceAggregate(t *testing.T) {
	nowTime := metav1.NewTime(time.Date(2025, 5, 27, 0, 0, 0, 0, time.UTC))

	tests := []struct {
		name     string
		parent   *exampleResource
		children map[string]*exampleResource
		args     []metav1.Condition
		expected []metav1.Condition
		changed  bool
	}{
		{
			name:     "No children, no args - should not change anything",
			parent:   referenceParent(t),
			args:     []metav1.Condition{},
			children: map[string]*exampleResource{},
			expected: nil,
			changed:  false,
		},
		{
			name:     "No children - should set Synced, Available, Initialized and UpToDate to true",
			parent:   referenceParent(t),
			args:     []metav1.Condition{Synced(), Available(), RawUpToDate()},
			children: map[string]*exampleResource{},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: nowTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionTrue, Reason: "Available", LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", LastTransitionTime: nowTime},
				{Type: ConditionTypeUpToDate, Status: metav1.ConditionTrue, Reason: "UpToDate", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:     "No children - was Synced, Available, Initialized, UpToDate before, should keep all of them",
			parent:   referenceParent(t, Synced(), Available(), initialized(), RawUpToDate()),
			args:     []metav1.Condition{Synced(), Available(), initialized(), RawUpToDate()},
			children: map[string]*exampleResource{},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionTrue, Reason: "Available", LastTransitionTime: parentTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", LastTransitionTime: parentTime},
				{Type: ConditionTypeUpToDate, Status: metav1.ConditionTrue, Reason: "UpToDate", LastTransitionTime: parentTime},
			},
			changed: false,
		},
		{
			name:     "No children during deletion - should set Available, NonDegraded to false, leave Initialized and UpToDate unchanged",
			parent:   referenceDeleting(referenceParent(t, Synced(), Available(), initialized(), RawUpToDate(), NonDegraded())),
			children: map[string]*exampleResource{},
			args:     []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", LastTransitionTime: parentTime},
				{Type: ConditionTypeUpToDate, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "Deleting", LastTransitionTime: nowTime, Message: "resource is being deleted"},
				{Type: ConditionTypeDeleting, Status: metav1.ConditionTrue, Reason: ConditionReasonDeleting, Message: "resource is being deleted", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:     "No children during deletion with NonDegraded=False override - should keep NonDegraded type, not set Available",
			parent:   referenceDeleting(referenceParent(t, Synced(), Available(), initialized(), RawUpToDate(), NonDegraded())),
			children: map[string]*exampleResource{},
			args:     []metav1.Condition{Synced(), NotAvailable("SomeReason", errors.New("some reason")), Degraded("SomeReason", errors.New("some reason")), RawUpToDate()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", LastTransitionTime: parentTime},
				{Type: ConditionTypeUpToDate, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "Deleting", LastTransitionTime: nowTime, Message: "resource is being deleted"},
				{Type: ConditionTypeDeleting, Status: metav1.ConditionTrue, Reason: ConditionReasonDeleting, Message: "resource is being deleted", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Uninitialized parent: child with Synced=False condition should only influence Available and NonDegraded",
			parent: referenceParent(t, Synced(), NotAvailable("Deploying", errors.New("deploying")), notInitializedYet()),
			children: map[string]*exampleResource{
				"TestName": referenceChild(t, "testname", NotSynced("TestError", fmt.Errorf("sync error"))),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{
					Type:   ConditionTypeAvailable,
					Status: metav1.ConditionFalse,
					Reason: "TestNameNotSynced",
					Message: `children are not synced:
  TestName: sync error (TestError)`,
					LastTransitionTime: parentTime,
				},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionFalse, Reason: "NotYetAvailable", Message: "waiting for resource to be available", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "NotAvailable", Message: "resource is not available", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Initialized parent: child with Synced=False condition, should only influence NonDegraded",
			parent: referenceParent(t, Synced(), Available(), initialized()),
			children: map[string]*exampleResource{
				"TestName": referenceChild(t, "testname", NotSynced("TestError", fmt.Errorf("sync error"))),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionTrue, Reason: "Available", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", LastTransitionTime: parentTime},
				{
					Type:               ConditionTypeNonDegraded,
					Status:             metav1.ConditionFalse,
					LastTransitionTime: nowTime,
					Reason:             "TestNameNotSynced",
					Message: `children are not synced:
  TestName: sync error (TestError)`,
				},
			},
			changed: true,
		},
		{
			name:   "Initialized parent: child with Synced=False, false Available and false NonDegraded condition, Synced error should bubble up",
			parent: referenceParent(t, Synced(), Available(), initialized()),
			children: map[string]*exampleResource{
				"TestName": referenceChild(t, "testname", NotSynced("TestError", fmt.Errorf("sync error")), NotAvailable("TestError", fmt.Errorf("available error")), Degraded("NotAvailable", fmt.Errorf("resource is not available"))),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionTrue, Reason: "Available", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", LastTransitionTime: parentTime},
				{
					Type:               ConditionTypeNonDegraded,
					Status:             metav1.ConditionFalse,
					LastTransitionTime: nowTime,
					Reason:             "TestNameNotSynced",
					Message: `children are not synced:
  TestName: sync error (TestError)`,
				},
			},
			changed: true,
		},
		{
			name:   "Initialized parent: child with Synced=False, failed Available and false NonDegraded condition, failed availability should bubble up",
			parent: referenceParent(t, Synced(), Available(), initialized()),
			children: map[string]*exampleResource{
				"TestName": referenceChild(t, "testname", NotSynced("TestError", fmt.Errorf("sync error")), NotAvailable("TestFailed", fmt.Errorf("available failure")), Degraded("NotAvailable", fmt.Errorf("resource is not available"))),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionTrue, Reason: "Available", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", LastTransitionTime: parentTime},
				{
					Type:               ConditionTypeNonDegraded,
					Status:             metav1.ConditionFalse,
					LastTransitionTime: nowTime,
					Reason:             "TestNameFailed",
					Message: `children failed:
  TestName: available failure (TestFailed)`,
				},
			},
			changed: true,
		},
		{
			name:   "UpToDate parent: child with UpToDate=false should bubble up",
			parent: referenceParent(t, Synced(), Available(), NonDegraded(), initialized()),
			children: map[string]*exampleResource{
				"TestName1": referenceChild(t, "testname1", Synced(), Available(), NonDegraded(), NotUpToDate("Updating", errors.New("updating child"))),
				"TestName2": referenceChild(t, "testname2", Synced(), Available(), NonDegraded(), RawUpToDate()),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionTrue, Reason: "Available", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionTrue, Reason: "FullyAvailable", LastTransitionTime: parentTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", LastTransitionTime: parentTime},
				{
					Type:   ConditionTypeUpToDate,
					Status: metav1.ConditionFalse,
					Reason: "ChildrenUpdating",
					Message: `updating children:
  TestName1: updating child (Updating)`,
					LastTransitionTime: nowTime,
				},
			},
			changed: true,
		},
		{
			name:   "UpToDate parent: children with UpToDate=false should bubble up",
			parent: referenceParent(t, Synced(), Available(), NonDegraded(), initialized()),
			children: map[string]*exampleResource{
				"TestName1": referenceChild(t, "testname1", Synced(), Available(), NonDegraded(), NotUpToDate("Updating", errors.New("updating child 1"))),
				"TestName2": referenceChild(t, "testname2", Synced(), Available(), NonDegraded(), NotUpToDate("Updating", errors.New("updating child 2"))),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionTrue, Reason: "Available", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionTrue, Reason: "FullyAvailable", LastTransitionTime: parentTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", LastTransitionTime: parentTime},
				{
					Type:   ConditionTypeUpToDate,
					Status: metav1.ConditionFalse,
					Reason: "ChildrenUpdating",
					Message: `updating children: TestName1, TestName2:
  TestName1: updating child 1 (Updating)
  TestName2: updating child 2 (Updating)`,
					LastTransitionTime: nowTime,
				},
			},
			changed: true,
		},
		{
			name:   "UpToDate parent: children with UpToDate=false should bubble up",
			parent: referenceParent(t, Synced(), Available(), NonDegraded(), initialized()),
			children: map[string]*exampleResource{
				"TestName1": referenceChild(t, "testname1", Synced(), Available(), NonDegraded(), NotUpToDate("Updating", errors.New("updating child 1"))),
				"TestName2": referenceChild(t, "testname2", Synced(), Available(), NonDegraded(), NotUpToDate("Updating", errors.New("updating child 2"))),
				"TestName3": referenceChild(t, "testname1", Synced(), Available(), NonDegraded(), NotUpToDate("Updating", errors.New("updating child 3"))),
				"TestName4": referenceChild(t, "testname2", Synced(), Available(), NonDegraded(), NotUpToDate("Updating", errors.New("updating child 4"))),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionTrue, Reason: "Available", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionTrue, Reason: "FullyAvailable", LastTransitionTime: parentTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", LastTransitionTime: parentTime},
				{
					Type:   ConditionTypeUpToDate,
					Status: metav1.ConditionFalse,
					Reason: "ChildrenUpdating",
					Message: `updating children: TestName1, TestName2, TestName3, and 1 more:
  TestName1: updating child 1 (Updating)
  TestName2: updating child 2 (Updating)
  TestName3: updating child 3 (Updating)
  TestName4: updating child 4 (Updating)`,
					LastTransitionTime: nowTime,
				},
			},
			changed: true,
		},
		{
			name:   "Uninitialized parent: uninitialized child with false Available condition",
			parent: referenceParent(t, Synced(), NotAvailable("Deploying", errors.New("deploying")), notInitializedYet()),
			children: map[string]*exampleResource{
				"TestName": referenceChild(t, "testname", Synced(), NotAvailable("TestError", fmt.Errorf("available error")), notInitializedYet()),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{
					Type:   ConditionTypeAvailable,
					Status: metav1.ConditionFalse,
					Reason: "TestNameNotAvailable",
					Message: `unavailable children:
  TestName: available error (TestError)`,
					LastTransitionTime: parentTime,
				},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionFalse, Reason: "NotYetAvailable", Message: "waiting for resource to be available", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "NotAvailable", Message: "resource is not available", LastTransitionTime: nowTime},
				{Type: ConditionTypeUpToDate, Status: metav1.ConditionFalse, Reason: "NotYetInitialized", Message: "waiting for resource to be initialized", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Initialized parent: initialized child with false Available condition, should be Available=false, UpToDate=true",
			parent: referenceParent(t, Synced(), Available(), initialized()),
			children: map[string]*exampleResource{
				"TestName": referenceChild(t, "testname", Synced(), NotAvailable("TestError", fmt.Errorf("available error")), initialized()),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{
					Type:   ConditionTypeAvailable,
					Status: metav1.ConditionFalse,
					Reason: "TestNameNotAvailable",
					Message: `unavailable children:
  TestName: available error (TestError)`,
					LastTransitionTime: nowTime,
				},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "NotAvailable", Message: "resource is not available", LastTransitionTime: nowTime},
				{Type: ConditionTypeUpToDate, Status: metav1.ConditionTrue, Reason: "UpToDate", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Initialized parent: uninitialized child with false Available condition, should be Available=true, NonDegraded=false, UpToDate=false",
			parent: referenceParent(t, Synced(), Available(), initialized()),
			children: map[string]*exampleResource{
				"TestName": referenceChild(t, "testname", Synced(), NotAvailable("TestError", fmt.Errorf("available error")), notInitializedYet()),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionTrue, Reason: "Available", LastTransitionTime: parentTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", Message: "", LastTransitionTime: parentTime},
				{
					Type:   ConditionTypeNonDegraded,
					Status: metav1.ConditionFalse,
					Reason: "TestNameDegraded",
					Message: `not yet initialized children:
  TestName: available error (TestError)`,
					LastTransitionTime: nowTime,
				},
				{
					Type:   ConditionTypeUpToDate,
					Status: metav1.ConditionFalse,
					Reason: "TestNameUpdating",
					Message: `not yet initialized children:
  TestName: available error (TestError)`,
					LastTransitionTime: nowTime,
				},
			},
			changed: true,
		},
		{
			name:   "Uninitialized parent: uninitialized child with false Available condition and *Failed reason",
			parent: referenceParent(t, Synced(), NotAvailable("Deploying", errors.New("deploying")), notInitializedYet()),
			children: map[string]*exampleResource{
				"TestName": referenceChild(t, "testname", Synced(), NotAvailable("TestNameFailed", fmt.Errorf("available error")), notInitializedYet()),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{
					Type:   ConditionTypeAvailable,
					Status: metav1.ConditionFalse,
					Reason: "TestNameFailed",
					Message: `children failed:
  TestName: available error (TestNameFailed)`,
					LastTransitionTime: parentTime,
				},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionFalse, Reason: "NotYetAvailable", Message: "waiting for resource to be available", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "TestNameFailed", Message: "children failed:\n  TestName: available error (TestNameFailed)", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Uninitialized parent: multiple uninitialized childdren with false Available condition and *Failed reason",
			parent: referenceParent(t, Synced(), NotAvailable("Deploying", errors.New("deploying")), notInitializedYet()),
			children: map[string]*exampleResource{
				"TestName1": referenceChild(t, "testname1", Synced(), NotAvailable("TestName1Failed", fmt.Errorf("available error")), notInitializedYet()),
				"TestName2": referenceChild(t, "testname2", Synced(), NotAvailable("TestName2Failed", fmt.Errorf("available error")), notInitializedYet()),
				"TestName3": referenceChild(t, "testname3", Synced(), NotAvailable("TestError", fmt.Errorf("available error")), notInitializedYet()),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{
					Type:   ConditionTypeAvailable,
					Status: metav1.ConditionFalse,
					Reason: "ChildrenFailed",
					Message: `children failed: TestName1, TestName2:
  TestName1: available error 1 (TestError)
  TestName2: available error 2 (TestError)`,
					LastTransitionTime: parentTime,
				},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionFalse, Reason: "NotYetAvailable", Message: "waiting for resource to be available", LastTransitionTime: parentTime},
				{
					Type:   ConditionTypeNonDegraded,
					Status: metav1.ConditionFalse,
					Reason: "ChildrenFailed",
					Message: `children failed: TestName1, TestName2:
  TestName1: available error (TestName1Failed)
  TestName2: available error (TestName2Failed)`,
					LastTransitionTime: nowTime,
				},
			},
			changed: true,
		},
		{
			name:   "Initialized parent: initialized child with false Available condition and *Failed reason",
			parent: referenceParent(t, Synced(), Available(), initialized()),
			children: map[string]*exampleResource{
				"TestName": referenceChild(t, "testname", Synced(), NotAvailable("TestNameFailed", fmt.Errorf("available error")), initialized()),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionFalse, Reason: "TestNameFailed", Message: "TestName failed: available error", LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "NotAvailable", Message: "resource is not available", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Initialized parent: uninitialized child with false Available condition and *Failed reason",
			parent: referenceParent(t, Synced(), Available(), initialized()),
			children: map[string]*exampleResource{
				"TestName": referenceChild(t, "testname", Synced(), NotAvailable("TestNameFailed", fmt.Errorf("available error")), notInitializedYet()),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionTrue, Reason: "Available", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "TestNameFailed", Message: "children failed:\n  TestName: available error (TestNameFailed)", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Initialized parent: uninitialized child with false Available condition",
			parent: referenceParent(t, Synced(), Available(), initialized()),
			children: map[string]*exampleResource{
				"TestName": referenceChild(t, "testname", Synced(), NotAvailable("TestError", fmt.Errorf("available error")), notInitializedYet()),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionTrue, Reason: "Available", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "TestNameDegraded", Message: "not yet initialized children:\n  TestName: available error (TestError)", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Uninitialized parent: child with unknown Available condition",
			parent: referenceParent(t, Synced(), NotAvailable("Deploying", errors.New("deploying")), notInitializedYet()),
			children: map[string]*exampleResource{
				"TestName": referenceChild(t, "testname", Synced(), UnknownAvailable("TestError", fmt.Errorf("available error"))),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionFalse, Reason: "TestNameNotAvailable", Message: "TestName is not available: state is still unknown", LastTransitionTime: parentTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionFalse, Reason: "NotYetAvailable", Message: "waiting for resource to be available", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "NotAvailable", Message: "resource is not available", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Initialized parent: child with unknown Available condition, considered a new resource of an upgrade, not making the parent degraded or unavailable",
			parent: referenceParent(t, Synced(), Available(), initialized()),
			children: map[string]*exampleResource{
				"TestName": referenceChild(t, "testname", Synced(), UnknownAvailable("TestError", fmt.Errorf("available error")), notInitializedYet()),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionTrue, Reason: "Available", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionTrue, Reason: "FullyAvailable", Message: "", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Uninitialized parent: multiple children with different conditions",
			parent: referenceParent(t, Synced(), NotAvailable("Deploying", errors.New("deploying")), notInitializedYet()),
			children: map[string]*exampleResource{
				"TestName1": referenceChild(t, "testname1", Synced(), NotAvailable("AvailableError1", fmt.Errorf("available error 1")), Degraded("NotAvailable", fmt.Errorf("resource is not available")), notInitializedYet()),
				"TestName2": referenceChild(t, "testname2", Synced(), Available(), Degraded("DegradedError2", fmt.Errorf("degraded error 2")), notInitializedYet()),
				"TestName3": referenceChild(t, "testname3", Synced(), UnknownAvailable("AvailableError3", fmt.Errorf("available error 3")), UnknownNonDegraded("DegradedError3", fmt.Errorf("degraded error 3")), notInitializedYet()),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{
					Type:   ConditionTypeAvailable,
					Status: metav1.ConditionFalse,
					Reason: "ChildrenNotAvailable",
					Message: `unavailable children: TestName1, TestName3:
  TestName1: available error 1 (TestError)
  TestName3: available error 3 (TestError)`,
					LastTransitionTime: parentTime,
				},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionFalse, Reason: "NotYetAvailable", Message: "waiting for resource to be available", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "NotAvailable", Message: "resource is not available", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Initialized parent: Multiple uninitialized children with different conditions",
			parent: referenceParent(t, Synced(), Available(), initialized()),
			children: map[string]*exampleResource{
				"TestName1": referenceChild(t, "testname1", Synced(), NotAvailable("AvailableError1", fmt.Errorf("available error 1")), Degraded("NotAvailable", fmt.Errorf("resource is not available")), notInitializedYet()),
				"TestName2": referenceChild(t, "testname2", Synced(), Available(), Degraded("DegradedError2", fmt.Errorf("degraded error 2")), notInitializedYet()),
				"TestName3": referenceChild(t, "testname3", Synced(), UnknownAvailable("AvailableError3", fmt.Errorf("available error 3")), UnknownNonDegraded("DegradedError3", fmt.Errorf("degraded error 3")), notInitializedYet()),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionTrue, Reason: "Available", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "ChildrenDegraded", Message: "degraded children: TestName1, TestName2", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Initialized parent: initialized child with direct NonDegraded=False, should use degraded header not initialized header",
			parent: referenceParent(t, Synced(), Available(), initialized()),
			children: map[string]*exampleResource{
				"TestName": referenceChild(t, "testname", Synced(), Available(), Degraded("HealthCheckDegraded", fmt.Errorf("health check degraded")), initialized()),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionTrue, Reason: "Available", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", Message: "", LastTransitionTime: parentTime},
				{
					Type:   ConditionTypeNonDegraded,
					Status: metav1.ConditionFalse,
					Reason: "TestNameDegraded",
					Message: `degraded children:
  TestName: health check degraded (HealthCheckDegraded)`,
					LastTransitionTime: nowTime,
				},
			},
			changed: true,
		},
		{
			name:   "Initialized parent: initialized child with direct NonDegraded=False *Failed reason, should amplify to children failed",
			parent: referenceParent(t, Synced(), Available(), initialized()),
			children: map[string]*exampleResource{
				"TestName": referenceChild(t, "testname", Synced(), Available(), Degraded("HealthCheckFailed", fmt.Errorf("health check failed")), initialized()),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionTrue, Reason: "Available", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", Message: "", LastTransitionTime: parentTime},
				{
					Type:   ConditionTypeNonDegraded,
					Status: metav1.ConditionFalse,
					Reason: "TestNameFailed",
					Message: `children failed:
  TestName: health check failed (HealthCheckFailed)`,
					LastTransitionTime: nowTime,
				},
			},
			changed: true,
		},
		{
			name:   "Initialized parent: Multiple initialized children with different conditions",
			parent: referenceParent(t, Synced(), Available(), initialized()),
			children: map[string]*exampleResource{
				"TestName1": referenceChild(t, "testname1", Synced(), NotAvailable("AvailableError1", fmt.Errorf("available error 1")), Degraded("NotAvailable", fmt.Errorf("resource is not available")), initialized()),
				"TestName2": referenceChild(t, "testname2", Synced(), Available(), Degraded("DegradedError2", fmt.Errorf("degraded error 2")), initialized()),
				"TestName3": referenceChild(t, "testname3", Synced(), UnknownAvailable("AvailableError3", fmt.Errorf("available error 3")), UnknownNonDegraded("DegradedError3", fmt.Errorf("degraded error 3")), initialized()),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionFalse, Reason: "ChildrenNotAvailable", Message: `TestName1: available error 1`, LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "NotAvailable", Message: "resource is not available", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Parent conditions override child conditions",
			parent: referenceParent(t),
			children: map[string]*exampleResource{
				"TestName": referenceChild(t, "testname", Synced(), NotAvailable("TestError", fmt.Errorf("available error")), NotUpToDate("Updating", errors.New("updating 1"))),
			},
			args: []metav1.Condition{Synced(), NotAvailable("AvailableError", fmt.Errorf("parent available error")), NotUpToDate("Updating", errors.New("updating parent"))},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: nowTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionFalse, Reason: "AvailableError", Message: "parent available error", LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionFalse, Reason: "NotYetAvailable", Message: "waiting for resource to be available", LastTransitionTime: nowTime},
				{Type: ConditionTypeUpToDate, Status: metav1.ConditionFalse, Reason: "Updating", Message: "updating parent", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Child with unknown conditions",
			parent: referenceParent(t, Synced(), Available(), RawUpToDate()),
			children: map[string]*exampleResource{
				"TestName": referenceChild(t, "testname"),
			},
			args: []metav1.Condition{Synced(), Available(), RawUpToDate()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionFalse, Reason: "TestNameNotAvailable", Message: "TestName is not available: state is still unknown", LastTransitionTime: nowTime},
				{Type: ConditionTypeUpToDate, Status: metav1.ConditionFalse, Reason: "NotYetInitialized", Message: "waiting for resource to be initialized", LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionFalse, Reason: "NotYetAvailable", Message: "waiting for resource to be available", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Multiple children with unknown conditions",
			parent: referenceParent(t, Synced(), Available(), RawUpToDate()),
			children: map[string]*exampleResource{
				"TestName1": referenceChild(t, "testname1"),
				"TestName2": referenceChild(t, "testname2"),
			},
			args: []metav1.Condition{Synced(), Available(), RawUpToDate()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionFalse, Reason: "ChildrenNotAvailable", Message: "unavailable children: TestName1, TestName2", LastTransitionTime: nowTime},
				{Type: ConditionTypeUpToDate, Status: metav1.ConditionFalse, Reason: "NotYetInitialized", Message: "waiting for resource to be initialized", LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionFalse, Reason: "NotYetAvailable", Message: "waiting for resource to be available", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Uninitialized parent: running job-like child",
			parent: referenceParent(t, Synced(), Available(), NonDegraded(), notInitializedYet()),
			children: map[string]*exampleResource{
				"TestName1": referenceChild(t, "testname1", Synced(), referenceJobRunning("Running", "resource is running"), referenceJobNotSucceeded("Running", errors.New("still running")), referenceJobNotFailed("Running", errors.New("still running"))),
				"TestName2": referenceChild(t, "testname2", Synced(), Available(), NonDegraded(), RawUpToDate()),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionFalse, Reason: "ChildrenNotAvailable", Message: "TestName1 is running: resource is running", LastTransitionTime: nowTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "NotAvailable", Message: "resource is not available", LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionFalse, Reason: "NotYetAvailable", Message: "waiting for resource to be available", LastTransitionTime: parentTime},
				{Type: ConditionTypeUpToDate, Status: metav1.ConditionFalse, Reason: "NotYetInitialized", Message: "waiting for resource to be initialized", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Uninitialized parent: pending job-like child",
			parent: referenceParent(t, Synced(), Available(), NonDegraded(), notInitializedYet()),
			children: map[string]*exampleResource{
				"TestName1": referenceChild(t, "testname1", Synced(), referenceJobNotRunning("Running", errors.New("resource is not yet running")), referenceJobNotSucceeded("Running", errors.New("still running")), referenceJobNotFailed("Running", errors.New("still running"))),
				"TestName2": referenceChild(t, "testname2", Synced(), Available(), NonDegraded(), RawUpToDate()),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionFalse, Reason: "ChildrenNotAvailable", Message: "TestName1 is pending: resource is not yet running", LastTransitionTime: nowTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "NotAvailable", Message: "resource is not available", LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionFalse, Reason: "NotYetAvailable", Message: "waiting for resource to be available", LastTransitionTime: parentTime},
				{Type: ConditionTypeUpToDate, Status: metav1.ConditionFalse, Reason: "NotYetInitialized", Message: "waiting for resource to be initialized", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Uninitialized parent: succeeded job-like child",
			parent: referenceParent(t, Synced(), Available(), NonDegraded(), notInitializedYet()),
			children: map[string]*exampleResource{
				"TestName1": referenceChild(t, "testname1", Synced(), referenceJobNotRunning("Succeeded", errors.New("resource succeeded")), referenceJobSucceeded(), referenceJobNotFailed("Succeeded", errors.New("resource succeeded"))),
				"TestName2": referenceChild(t, "testname2", Synced(), Available(), NonDegraded(), RawUpToDate()),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionTrue, Reason: "Available", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionTrue, Reason: "FullyAvailable", LastTransitionTime: parentTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", LastTransitionTime: nowTime},
				{Type: ConditionTypeUpToDate, Status: metav1.ConditionTrue, Reason: "UpToDate", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Uninitialized parent: failed job-like child",
			parent: referenceParent(t, Synced(), Available(), NonDegraded(), notInitializedYet()),
			children: map[string]*exampleResource{
				"TestName1": referenceChild(t, "testname1", Synced(), referenceJobNotRunning("Failed", errors.New("resource failed")), referenceJobNotSucceeded("Failed", errors.New("resource failed")), referenceJobFailed("Failed", errors.New("resource failed"))),
				"TestName2": referenceChild(t, "testname2", Synced(), Available(), NonDegraded(), RawUpToDate()),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionFalse, Reason: "ChildrenFailed", Message: "TestName1 failed: resource failed", LastTransitionTime: nowTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "ChildrenFailed", Message: "TestName1 failed: resource failed", LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionFalse, Reason: "NotYetAvailable", Message: "waiting for resource to be available", LastTransitionTime: parentTime},
				{Type: ConditionTypeUpToDate, Status: metav1.ConditionFalse, Reason: "NotYetInitialized", Message: "waiting for resource to be initialized", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Uninitialized parent: Synced=false job-like child",
			parent: referenceParent(t, Synced(), Available(), NonDegraded(), notInitializedYet()),
			children: map[string]*exampleResource{
				"TestName1": referenceChild(t, "testname1", NotSynced("SyncError", errors.New("sync error")), referenceJobRunning("Running", "resource is running"), referenceJobNotSucceeded("Running", errors.New("still running")), referenceJobNotFailed("Running", errors.New("still running"))),
				"TestName2": referenceChild(t, "testname2", Synced(), Available(), NonDegraded(), RawUpToDate()),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionFalse, Reason: "ChildrenNotSynced", Message: "TestName1: sync error", LastTransitionTime: nowTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "NotAvailable", Message: "resource is not available", LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionFalse, Reason: "NotYetAvailable", Message: "waiting for resource to be available", LastTransitionTime: parentTime},
				{Type: ConditionTypeUpToDate, Status: metav1.ConditionFalse, Reason: "NotYetInitialized", Message: "waiting for resource to be initialized", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Initialized parent: running job-like child",
			parent: referenceParent(t, Synced(), Available(), NonDegraded(), initialized()),
			children: map[string]*exampleResource{
				"TestName1": referenceChild(t, "testname1", Synced(), referenceJobRunning("Running", "resource is running"), referenceJobNotSucceeded("Running", errors.New("still running")), referenceJobNotFailed("Running", errors.New("still running"))),
				"TestName2": referenceChild(t, "testname2", Synced(), Available(), NonDegraded(), RawUpToDate()),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionTrue, Reason: "Available", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionTrue, Reason: "FullyAvailable", LastTransitionTime: parentTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", LastTransitionTime: parentTime},
				{Type: ConditionTypeUpToDate, Status: metav1.ConditionFalse, Reason: "ChildrenUpdating", Message: "TestName1 is running: resource is running", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Initialized parent: succeeded job-like child",
			parent: referenceParent(t, Synced(), Available(), NonDegraded(), initialized()),
			children: map[string]*exampleResource{
				"TestName1": referenceChild(t, "testname1", Synced(), referenceJobNotRunning("Succeeded", errors.New("resource succeeded")), referenceJobSucceeded(), referenceJobNotFailed("Succeeded", errors.New("resource succeeded"))),
				"TestName2": referenceChild(t, "testname2", Synced(), Available(), NonDegraded(), RawUpToDate()),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionTrue, Reason: "Available", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionTrue, Reason: "FullyAvailable", LastTransitionTime: parentTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", LastTransitionTime: parentTime},
				{Type: ConditionTypeUpToDate, Status: metav1.ConditionTrue, Reason: "UpToDate", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Initialized parent: failed job-like child",
			parent: referenceParent(t, Synced(), Available(), NonDegraded(), initialized()),
			children: map[string]*exampleResource{
				"TestName1": referenceChild(t, "testname1", Synced(), referenceJobNotRunning("Failed", errors.New("resource failed")), referenceJobNotSucceeded("Failed", errors.New("resource failed")), referenceJobFailed("Failed", errors.New("resource failed"))),
				"TestName2": referenceChild(t, "testname2", Synced(), Available(), NonDegraded(), RawUpToDate()),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionTrue, Reason: "Available", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "ChildrenFailed", Message: "TestName1 failed: resource failed", LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", LastTransitionTime: parentTime},
				{Type: ConditionTypeUpToDate, Status: metav1.ConditionFalse, Reason: "ChildrenUpdating", Message: "TestName1 failed: resource failed", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Initialized parent: succeeded job-like child, running again (retry)",
			parent: referenceParent(t, Synced(), Available(), NonDegraded(), initialized()),
			children: map[string]*exampleResource{
				"TestName1": referenceChild(t, "testname1", Synced(), referenceJobRunning("Retried", "resource was retried"), referenceJobSucceeded(), referenceJobNotFailed("Succeeded", errors.New("resource succeeded"))),
				"TestName2": referenceChild(t, "testname2", Synced(), Available(), NonDegraded(), RawUpToDate()),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionTrue, Reason: "Available", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionTrue, Reason: "FullyAvailable", LastTransitionTime: parentTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", LastTransitionTime: parentTime},
				{Type: ConditionTypeUpToDate, Status: metav1.ConditionFalse, Reason: "ChildrenUpdating", Message: "TestName1 is running: resource was retried", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Initialized parent: failed job-like child, running again (retry)",
			parent: referenceParent(t, Synced(), Available(), NonDegraded(), initialized()),
			children: map[string]*exampleResource{
				"TestName1": referenceChild(t, "testname1", Synced(), referenceJobRunning("Retried", "resource was retried"), referenceJobNotSucceeded("Failed", errors.New("resource failed")), referenceJobFailed("Failed", errors.New("resource failed"))),
				"TestName2": referenceChild(t, "testname2", Synced(), Available(), NonDegraded(), RawUpToDate()),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionTrue, Reason: "Available", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "ChildrenFailed", Message: "TestName1 failed: resource failed", LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", LastTransitionTime: parentTime},
				{Type: ConditionTypeUpToDate, Status: metav1.ConditionFalse, Reason: "ChildrenUpdating", Message: "TestName1 is running: resource was retried", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Non-true parent conditions override all child conditions",
			parent: referenceParent(t, Synced(), Available(), initialized(), RawUpToDate()),
			children: map[string]*exampleResource{
				"TestName1": referenceChild(t, "testname1", NotAvailable("TestError1", errors.New("available error 1")), NotUpToDate("Updating", errors.New("updating 1"))),
				"TestName2": referenceChild(t, "testname2", NotAvailable("TestError2", errors.New("sync error 2")), NotUpToDate("Updating", errors.New("updating 2"))),
			},
			args: []metav1.Condition{NotSynced("SyncError", errors.New("parent sync error")), NotAvailable("AvailableError", errors.New("parent available error")), NotUpToDate("Updating", errors.New("updating parent"))},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionFalse, Reason: "SyncError", Message: "parent sync error", LastTransitionTime: nowTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionFalse, Reason: "AvailableError", Message: "parent available error", LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", LastTransitionTime: parentTime},
				{Type: ConditionTypeUpToDate, Status: metav1.ConditionFalse, Reason: "Updating", Message: "updating parent", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Deleting parent: multiple but less than 3 children being deleted",
			parent: referenceDeleting(referenceParent(t, Synced(), Available(), initialized(), NonDegraded(), RawUpToDate())),
			children: map[string]*exampleResource{
				"TestName1": referenceDeleting(referenceChild(t, "testname1")),
				"TestName2": referenceDeleting(referenceChild(t, "testname2")),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeUpToDate, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeDeleting, Status: metav1.ConditionTrue, Reason: "Deleting", Message: "deleting children: TestName1, TestName2", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Deleting parent: one child being deleted",
			parent: referenceDeleting(referenceParent(t, Synced(), Available(), initialized(), NonDegraded(), RawUpToDate())),
			children: map[string]*exampleResource{
				"TestName1": referenceDeleting(referenceChild(t, "testname1", Deleting("delete message"))),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeUpToDate, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeDeleting, Status: metav1.ConditionTrue, Reason: "Deleting", Message: "deleting TestName1: delete message", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Parent deleting: multiple children being deleted with more than 3",
			parent: referenceDeleting(referenceParent(t, Synced(), Available(), initialized(), NonDegraded(), RawUpToDate())),
			children: map[string]*exampleResource{
				"TestName1": referenceDeleting(referenceChild(t, "testname1")),
				"TestName2": referenceDeleting(referenceChild(t, "testname2")),
				"TestName3": referenceDeleting(referenceChild(t, "testname3")),
				"TestName4": referenceDeleting(referenceChild(t, "testname4")),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeUpToDate, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeDeleting, Status: metav1.ConditionTrue, Reason: "Deleting", Message: "deleting children: TestName1, TestName2, TestName3, and 1 more", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Parent deleting: one child deleting",
			parent: referenceDeleting(referenceParent(t, Synced(), Available(), initialized(), NonDegraded(), RawUpToDate())),
			children: map[string]*exampleResource{
				"TestName1": referenceChild(t, "testname1", Synced(), NotAvailable(ConditionReasonDeleting, errors.New("resource is being deleted")), initialized(), DeletionError("DeletionError", errors.New("error during deletion"))),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeUpToDate, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeDeleting, Status: metav1.ConditionTrue, Reason: "Deleting", Message: "resource is being deleted, remaining: TestName1", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Parent deleting: multiple children deleting",
			parent: referenceDeleting(referenceParent(t, Synced(), Available(), initialized(), NonDegraded(), RawUpToDate())),
			children: map[string]*exampleResource{
				"TestName1": referenceDeleting(referenceChild(t, "testname1", Synced(), NotAvailable(ConditionReasonDeleting, errors.New("resource is being deleted")), initialized(), Deleting(""))),
				"TestName2": referenceDeleting(referenceChild(t, "testname2", Synced(), NotAvailable(ConditionReasonDeleting, errors.New("resource is being deleted")), initialized(), Deleting(""))),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeUpToDate, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeDeleting, Status: metav1.ConditionTrue, Reason: "Deleting", Message: "deleting children: TestName1, TestName2", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Parent deleting: child with delete error during deletion",
			parent: referenceDeleting(referenceParent(t, Synced(), Available(), initialized(), NonDegraded(), RawUpToDate())),
			children: map[string]*exampleResource{
				"TestName1": referenceDeleting(referenceChild(t, "testname1", Synced(), NotAvailable(ConditionReasonDeleting, errors.New("resource is being deleted")), initialized(), DeletionError("DeletionError", errors.New("error during deletion")))),
				"TestName2": referenceDeleting(referenceChild(t, "testname2", Synced(), NotAvailable(ConditionReasonDeleting, errors.New("resource is being deleted")), initialized(), Deleting(""))),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeUpToDate, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeDeleting, Status: metav1.ConditionTrue, Reason: "ChildrenDeletionError", Message: "deleting 2 resources, but: TestName1 fails to delete: error during deletion", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Parent deleting: multiple children with delete error during deletion",
			parent: referenceDeleting(referenceParent(t, Synced(), Available(), initialized(), NonDegraded(), RawUpToDate())),
			children: map[string]*exampleResource{
				"TestName1": referenceDeleting(referenceChild(t, "testname1", Synced(), NotAvailable(ConditionReasonDeleting, errors.New("resource is being deleted")), initialized(), DeletionError("DeletionError", errors.New("error during deletion")))),
				"TestName2": referenceDeleting(referenceChild(t, "testname2", Synced(), NotAvailable(ConditionReasonDeleting, errors.New("resource is being deleted")), initialized(), Deleting(""))),
				"TestName3": referenceDeleting(referenceChild(t, "testname3", Synced(), NotAvailable(ConditionReasonDeleting, errors.New("resource is being deleted")), initialized(), DeletionError("DeletionError", errors.New("error during deletion")))),
				"TestName4": referenceDeleting(referenceChild(t, "testname4", Synced(), NotAvailable(ConditionReasonDeleting, errors.New("resource is being deleted")), initialized(), Deleting(""))),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeUpToDate, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeDeleting, Status: metav1.ConditionTrue, Reason: "ChildrenDeletionError", Message: "deleting 4 resources, and 2 failed: TestName1, TestName3", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Parent deleting: child with sync error during deletion",
			parent: referenceDeleting(referenceParent(t, Synced(), Available(), initialized(), NonDegraded(), RawUpToDate())),
			children: map[string]*exampleResource{
				"TestName1": referenceDeleting(referenceChild(t, "testname1", NotSynced("SyncError", errors.New("sync error")), NotAvailable(ConditionReasonDeleting, errors.New("resource is being deleted")), initialized(), Deleting(""))),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeUpToDate, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeDeleting, Status: metav1.ConditionTrue, Reason: "ChildrenDeletionError", Message: "TestName1 fails to sync: sync error", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:     "Parent deleting: no children, parent custom condition",
			parent:   referenceDeleting(referenceParent(t, Synced(), Available(), initialized(), NonDegraded(), RawUpToDate())),
			children: map[string]*exampleResource{},
			args:     []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate(), DeletionError("Deleting", errors.New("some custom message"))},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeUpToDate, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeDeleting, Status: metav1.ConditionTrue, Reason: "Deleting", Message: "some custom message", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Parent deleting: parent custom Failed condition",
			parent: referenceDeleting(referenceParent(t, Synced(), Available(), initialized(), NonDegraded(), RawUpToDate())),
			children: map[string]*exampleResource{
				"TestName1": referenceDeleting(referenceChild(t, "testname1", NotSynced("SyncError", errors.New("sync error")), NotAvailable(ConditionReasonDeleting, errors.New("resource is being deleted")), initialized(), Deleting(""))),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate(), DeletionError("DeletionFailed", errors.New("some custom message"))},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeUpToDate, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeDeleting, Status: metav1.ConditionTrue, Reason: "DeletionFailed", Message: "some custom message", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Parent deleting: one tear-down job running",
			parent: referenceDeleting(referenceParent(t, Synced(), Available(), initialized(), NonDegraded(), RawUpToDate())),
			children: map[string]*exampleResource{
				"TestNameTearDown": referenceChild(t, "teardown1", Synced(), referenceJobRunning("Running", "tearing down"), referenceJobNotSucceeded("Running", errors.New("still running")), referenceJobNotFailed("Running", errors.New("still running"))),
				"TestName1":        referenceChild(t, "testname1", Synced(), NotAvailable(ConditionReasonDeleting, errors.New("resource is being deleted")), initialized(), DeletionError("DeletionError", errors.New("error during deletion"))),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeUpToDate, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeDeleting, Status: metav1.ConditionTrue, Reason: "Deleting", Message: "TestNameTearDown is running: tearing down", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Parent deleting: tear-down job running with multi-line message preserves nested detail",
			parent: referenceDeleting(referenceParent(t, Synced(), Available(), initialized(), NonDegraded(), RawUpToDate())),
			children: map[string]*exampleResource{
				"TeardownPipeline": referenceChild(t, "teardown1", Synced(),
					referenceJobRunning("Provisioning", "GitLabResource ns/name on the host is provisioning:\n  pipeline: https://git.example.invalid/project/-/merge_requests/123"),
					referenceJobNotSucceeded("Running", errors.New("still running")),
					referenceJobNotFailed("Running", errors.New("still running"))),
				"TestName1": referenceChild(t, "testname1", Synced(), NotAvailable(ConditionReasonDeleting, errors.New("resource is being deleted")), initialized(), DeletionError("DeletionError", errors.New("error during deletion"))),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeUpToDate, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeDeleting, Status: metav1.ConditionTrue, Reason: "Deleting", Message: "TeardownPipeline is running: GitLabResource ns/name on the host is provisioning:\n  pipeline: https://git.example.invalid/project/-/merge_requests/123", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Parent deleting: deleting child with multi-line message preserves nested detail",
			parent: referenceDeleting(referenceParent(t, Synced(), Available(), initialized(), NonDegraded(), RawUpToDate())),
			children: map[string]*exampleResource{
				"EKSCluster": referenceDeleting(referenceChild(t, "ekscluster1", Synced(), metav1.Condition{
					Type:    ConditionTypeDeleting,
					Status:  metav1.ConditionTrue,
					Reason:  ConditionReasonDeleting,
					Message: "TeardownPipeline is running: GitLabResource ns/name on the host is provisioning:\n  pipeline: https://git.example.invalid/project/-/merge_requests/123",
				})),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionFalse, Reason: "Deleting", LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "Deleting", LastTransitionTime: nowTime},
				{Type: ConditionTypeUpToDate, Status: metav1.ConditionFalse, Reason: "Deleting", LastTransitionTime: nowTime},
				{Type: ConditionTypeDeleting, Status: metav1.ConditionTrue, Reason: "Deleting", Message: "deleting EKSCluster: TeardownPipeline is running: GitLabResource ns/name on the host is provisioning:\n  pipeline: https://git.example.invalid/project/-/merge_requests/123", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Parent deleting: multiple tear-down jobs running",
			parent: referenceDeleting(referenceParent(t, Synced(), Available(), initialized(), NonDegraded(), RawUpToDate())),
			children: map[string]*exampleResource{
				"TestNameTearDown1": referenceChild(t, "teardown1", Synced(), referenceJobRunning("Running", "tearing down"), referenceJobNotSucceeded("Running", errors.New("still running")), referenceJobNotFailed("Running", errors.New("still running"))),
				"TestNameTearDown2": referenceChild(t, "teardown2", Synced(), referenceJobRunning("Running", "tearing down"), referenceJobNotSucceeded("Running", errors.New("still running")), referenceJobNotFailed("Running", errors.New("still running"))),
				"TestName1":         referenceChild(t, "testname1", Synced(), NotAvailable(ConditionReasonDeleting, errors.New("resource is being deleted")), initialized(), DeletionError("DeletionError", errors.New("error during deletion"))),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeUpToDate, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeDeleting, Status: metav1.ConditionTrue, Reason: "Deleting", Message: "tear-down jobs running: TestNameTearDown1, TestNameTearDown2", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Parent deleting: one tear-down job failed",
			parent: referenceDeleting(referenceParent(t, Synced(), Available(), initialized(), NonDegraded(), RawUpToDate())),
			children: map[string]*exampleResource{
				"TestNameTearDown1": referenceChild(t, "teardown1", Synced(), referenceJobNotRunning("Failed", errors.New("failed")), referenceJobNotSucceeded("Running", errors.New("still running")), referenceJobFailed("Running", errors.New("failed"))),
				"TestNameTearDown2": referenceChild(t, "teardown2", Synced(), referenceJobRunning("Running", "tearing down"), referenceJobNotSucceeded("Running", errors.New("still running")), referenceJobNotFailed("Running", errors.New("still running"))),
				"TestName1":         referenceChild(t, "testname1", Synced(), NotAvailable(ConditionReasonDeleting, errors.New("resource is being deleted")), initialized(), DeletionError("DeletionError", errors.New("error during deletion"))),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeUpToDate, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeDeleting, Status: metav1.ConditionTrue, Reason: "TearDownFailed", Message: "TestNameTearDown1 failed: failed", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Parent deleting: multiple tear-down jobs failed",
			parent: referenceDeleting(referenceParent(t, Synced(), Available(), initialized(), NonDegraded(), RawUpToDate())),
			children: map[string]*exampleResource{
				"TestNameTearDown1": referenceChild(t, "teardown1", Synced(), referenceJobNotRunning("Failed", errors.New("failed")), referenceJobNotSucceeded("Running", errors.New("still running")), referenceJobFailed("Running", errors.New("failed"))),
				"TestNameTearDown2": referenceChild(t, "teardown2", Synced(), referenceJobNotRunning("Failed", errors.New("failed")), referenceJobNotSucceeded("Running", errors.New("still running")), referenceJobFailed("Running", errors.New("failed"))),
				"TestName1":         referenceChild(t, "testname1", Synced(), NotAvailable(ConditionReasonDeleting, errors.New("resource is being deleted")), initialized(), DeletionError("DeletionError", errors.New("error during deletion"))),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeUpToDate, Status: metav1.ConditionFalse, Reason: "Deleting", Message: "resource is being deleted", LastTransitionTime: nowTime},
				{Type: ConditionTypeDeleting, Status: metav1.ConditionTrue, Reason: "TearDownFailed", Message: "tear-down jobs failed: TestNameTearDown1, TestNameTearDown2", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Nil child among valid children - nil should be ignored",
			parent: referenceParent(t, Synced(), Available(), initialized(), NonDegraded()),
			children: map[string]*exampleResource{
				"TestName1": referenceChild(t, "testname1", Synced(), NotAvailable("TestError1", errors.New("availability error 1")), initialized()),
				"TestName2": referenceChild(t, "testname2", Synced(), Available(), NonDegraded(), initialized()),
				"TestName3": nil,
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionFalse, Reason: "ChildrenNotAvailable", Message: "TestName1 is not available: availability error 1", LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "NotAvailable", Message: "resource is not available", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Multi line message",
			parent: referenceParent(t, Synced(), Available(), initialized(), NonDegraded()),
			children: map[string]*exampleResource{
				"TestName1": referenceChild(t, "testname1", Synced(), NotAvailable("TestError1", errors.New("availability error 1:\nsome\nmore\ncontext")), initialized()),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionFalse, Reason: "TestName1NotAvailable", Message: "TestName1 is not available: availability error 1", LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "NotAvailable", Message: "resource is not available", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Dot ending the message",
			parent: referenceParent(t, Synced(), Available(), initialized(), NonDegraded()),
			children: map[string]*exampleResource{
				"TestName1": referenceChild(t, "testname1", Synced(), NotAvailable("TestError1", errors.New("availability error 1.")), initialized()), //nolint:revive // it's for testing purposes.
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionFalse, Reason: "TestName1NotAvailable", Message: "TestName1 is not available: availability error 1", LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "NotAvailable", Message: "resource is not available", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Uninitialized parent: multiple children with false synced conditions should influence Available and NonDegraded",
			parent: referenceParent(t, Synced(), Available(), NonDegraded()),
			children: map[string]*exampleResource{
				"TestName":  referenceChild(t, "testname", NotSynced("TestError", fmt.Errorf("sync error"))),
				"TestName1": referenceChild(t, "testname1", NotSynced("TestError", fmt.Errorf("sync error"))),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionFalse, Reason: "ChildrenNotSynced", Message: "children are not synced: TestName, TestName1", LastTransitionTime: nowTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "NotAvailable", Message: "resource is not available", LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionFalse, Reason: "NotYetAvailable", Message: "waiting for resource to be available", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "Uninitialized parent: multiple children with false synced conditions and exactly 1 child that is unavailable",
			parent: referenceParent(t, Synced(), Available(), NonDegraded()),
			children: map[string]*exampleResource{
				"TestName1": referenceChild(t, "testname1", NotSynced("TestError", fmt.Errorf("sync error"))),
				"TestName2": referenceChild(t, "testname2", NotSynced("TestError", fmt.Errorf("sync error"))),
				"TestName3": referenceChild(t, "testname3", NotAvailable("TestError", fmt.Errorf("availability error"))),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionFalse, Reason: "ChildrenNotAvailable", Message: "unavailable children: TestName3, and not synced: TestName1, TestName2", LastTransitionTime: nowTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "NotAvailable", Message: "resource is not available", LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionFalse, Reason: "NotYetAvailable", Message: "waiting for resource to be available", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name: "Parent with true available subcondition",
			parent: referenceParent(t, Synced(), Available(), initialized(), NonDegraded(), metav1.Condition{
				Type:   "AvailableHealth",
				Status: metav1.ConditionTrue,
				Reason: "HealthCheckPassed",
			}),
			children: map[string]*exampleResource{
				"TestName1": referenceChild(t, "testname1", Synced(), Available(), NonDegraded(), initialized()),
				"TestName2": referenceChild(t, "testname2", Synced(), Available(), NonDegraded(), initialized()),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionTrue, Reason: "Available", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionTrue, Reason: "FullyAvailable", Message: "", LastTransitionTime: parentTime},
				{Type: "AvailableHealth", Status: metav1.ConditionTrue, Reason: "HealthCheckPassed", LastTransitionTime: parentTime},
			},
			changed: false,
		},
		{
			name: "Parent with false available subcondition",
			parent: referenceParent(t, Synced(), Available(), initialized(), NonDegraded(), metav1.Condition{
				Type:    "AvailableNodes",
				Status:  metav1.ConditionFalse,
				Reason:  "InsufficientHealthyNodes",
				Message: "11/200 nodes are unhealthy",
			}),
			children: map[string]*exampleResource{
				"TestName1": referenceChild(t, "testname1", Synced(), Available(), NonDegraded(), initialized()),
				"TestName2": referenceChild(t, "testname2", Synced(), Available(), NonDegraded(), initialized()),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionFalse, Reason: "InsufficientHealthyNodes", Message: "Nodes check false: 11/200 nodes are unhealthy", LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "NotAvailable", Message: "resource is not available", LastTransitionTime: nowTime},
				{Type: "AvailableNodes", Status: metav1.ConditionFalse, Reason: "InsufficientHealthyNodes", Message: "11/200 nodes are unhealthy", LastTransitionTime: parentTime},
			},
			changed: true,
		},
		{
			name: "Parent with multiple false available subcondition",
			parent: referenceParent(t, Synced(), Available(), initialized(), NonDegraded(), metav1.Condition{
				Type:    "AvailableHealth",
				Status:  metav1.ConditionFalse,
				Reason:  "HealthCheckFlaky",
				Message: "Health check is flaky",
			}, metav1.Condition{
				Type:    "AvailableNodes",
				Status:  metav1.ConditionFalse,
				Reason:  "InsufficientHealthyNodes",
				Message: "11/200 nodes are unhealthy",
			}),
			children: map[string]*exampleResource{
				"TestName1": referenceChild(t, "testname1", Synced(), Available(), NonDegraded(), initialized()),
				"TestName2": referenceChild(t, "testname2", Synced(), Available(), NonDegraded(), initialized()),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionFalse, Reason: "FalseChecks", Message: "false checks: Health, Nodes", LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "NotAvailable", Message: "resource is not available", LastTransitionTime: nowTime},
				{Type: "AvailableHealth", Status: metav1.ConditionFalse, Reason: "HealthCheckFlaky", Message: "Health check is flaky", LastTransitionTime: parentTime},
				{Type: "AvailableNodes", Status: metav1.ConditionFalse, Reason: "InsufficientHealthyNodes", Message: "11/200 nodes are unhealthy", LastTransitionTime: parentTime},
			},
			changed: true,
		},
		{
			name: "Parent with one failed available subcondition",
			parent: referenceParent(t, Synced(), Available(), initialized(), NonDegraded(), metav1.Condition{
				Type:    "AvailableHealth",
				Status:  metav1.ConditionFalse,
				Reason:  "HealthCheckFailed",
				Message: "Health check failed",
			}, metav1.Condition{
				Type:    "AvailableGPUOperators",
				Status:  metav1.ConditionFalse,
				Reason:  "GPUOperatorNotAvailable",
				Message: "GPU operator is not running",
			}),
			children: map[string]*exampleResource{
				"TestName1": referenceChild(t, "testname1", Synced(), NotAvailable("NotAvailable", errors.New("not available")), NonDegraded(), initialized()),
				"TestName2": referenceChild(t, "testname2", Synced(), Available(), NonDegraded(), initialized()),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionFalse, Reason: "HealthCheckFailed", Message: "failed Health check: Health check failed", LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "NotAvailable", Message: "resource is not available", LastTransitionTime: nowTime},
				{Type: "AvailableHealth", Status: metav1.ConditionFalse, Reason: "HealthCheckFailed", Message: "Health check failed", LastTransitionTime: parentTime},
				{Type: "AvailableGPUOperators", Status: metav1.ConditionFalse, Reason: "GPUOperatorNotAvailable", Message: "GPU operator is not running", LastTransitionTime: parentTime},
			},
			changed: true,
		},
		{
			name: "Parent with failed available subcondition",
			parent: referenceParent(t, Synced(), Available(), initialized(), NonDegraded(), metav1.Condition{
				Type:    "AvailableHealth",
				Status:  metav1.ConditionFalse,
				Reason:  "HealthCheckFailed",
				Message: "Health check failed",
			}, metav1.Condition{
				Type:    "AvailableNodes",
				Status:  metav1.ConditionFalse,
				Reason:  "GetNodesFailed",
				Message: "Could not get nodes",
			}, metav1.Condition{
				Type:    "AvailableGPUOperators",
				Status:  metav1.ConditionFalse,
				Reason:  "GPUOperatorNotAvailable",
				Message: "GPU operator is not running",
			}),
			children: map[string]*exampleResource{
				"TestName1": referenceChild(t, "testname1", Synced(), NotAvailable("NotAvailable", errors.New("not available")), NonDegraded(), initialized()),
				"TestName2": referenceChild(t, "testname2", Synced(), Available(), NonDegraded(), initialized()),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionFalse, Reason: "FailedChecks", Message: "failed checks: Health, Nodes", LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "NotAvailable", Message: "resource is not available", LastTransitionTime: nowTime},
				{Type: "AvailableHealth", Status: metav1.ConditionFalse, Reason: "HealthCheckFailed", Message: "Health check failed", LastTransitionTime: parentTime},
				{Type: "AvailableNodes", Status: metav1.ConditionFalse, Reason: "GetNodesFailed", Message: "Could not get nodes", LastTransitionTime: parentTime},
				{Type: "AvailableGPUOperators", Status: metav1.ConditionFalse, Reason: "GPUOperatorNotAvailable", Message: "GPU operator is not running", LastTransitionTime: parentTime},
			},
			changed: true,
		},
		{
			name: "Parent with failed available subcondition and failed children",
			parent: referenceParent(t, Synced(), Available(), initialized(), NonDegraded(), metav1.Condition{
				Type:    "AvailableHealth",
				Status:  metav1.ConditionFalse,
				Reason:  "HealthCheckFailed",
				Message: "Health check failed",
			}, metav1.Condition{
				Type:    "AvailableNodes",
				Status:  metav1.ConditionFalse,
				Reason:  "GetNodesFailed",
				Message: "Could not get nodes",
			}, metav1.Condition{
				Type:    "AvailableGPUOperators",
				Status:  metav1.ConditionFalse,
				Reason:  "GPUOperatorNotAvailable",
				Message: "GPU operator is not running",
			}),
			children: map[string]*exampleResource{
				"TestName1": referenceChild(t, "testname1", Synced(), NotAvailable("SomethingFailed", errors.New("something failed")), NonDegraded(), initialized()),
				"TestName2": referenceChild(t, "testname2", NotSynced("NotSynced", errors.New("cannot sync")), Available(), NonDegraded(), initialized()),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionFalse, Reason: "FailedChecks", Message: "failed checks: Health, Nodes, and children failed: TestName1", LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", Message: "", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "NotAvailable", Message: "resource is not available", LastTransitionTime: nowTime},
				{Type: "AvailableHealth", Status: metav1.ConditionFalse, Reason: "HealthCheckFailed", Message: "Health check failed", LastTransitionTime: parentTime},
				{Type: "AvailableNodes", Status: metav1.ConditionFalse, Reason: "GetNodesFailed", Message: "Could not get nodes", LastTransitionTime: parentTime},
				{Type: "AvailableGPUOperators", Status: metav1.ConditionFalse, Reason: "GPUOperatorNotAvailable", Message: "GPU operator is not running", LastTransitionTime: parentTime},
			},
			changed: true,
		},

		// amplifyHuman: WaitingForHuman reasons escalate through aggregation.
		// Case matrix for Synced=False/WaitingForHuman on Available:
		//   parent init=N, child init=N → Available=False/NotSynced (infra doesn't exist)
		//   parent init=N, child init=Y → Available=False/WaitingForHuman (initialized child blocked)
		//   parent init=Y, child init=N → Available unchanged (new child, service running)
		//   parent init=Y, child init=Y → Available unchanged (child works, service running)
		// UpToDate always amplifies WaitingForHuman; NonDegraded never does (see ADR-008).
		{
			name:   "WaitingForHuman: uninit parent, uninit child — Available=False/NotSynced, NonDegraded/UpToDate not yet initialized",
			parent: referenceParent(t),
			children: map[string]*exampleResource{
				"TestName": referenceChild(t, "testname", NotSynced("WaitingForHuman", errors.New("waiting for MR approval"))),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: nowTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionFalse, Reason: "TestNameNotSynced",
					Message:            "children are not synced:\n  TestName: waiting for MR approval (WaitingForHuman)",
					LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionFalse, Reason: "NotYetAvailable", Message: "waiting for resource to be available", LastTransitionTime: nowTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "NotAvailable", Message: "resource is not available", LastTransitionTime: nowTime},
				{Type: ConditionTypeUpToDate, Status: metav1.ConditionFalse, Reason: "NotYetInitialized", Message: "waiting for resource to be initialized", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "WaitingForHuman: uninit parent, init child — Available=False/WaitingForHuman escalated",
			parent: referenceParent(t),
			children: map[string]*exampleResource{
				"TestName": referenceChild(t, "testname", NotSynced("WaitingForHuman", errors.New("waiting for MR approval")), Available(), NonDegraded(), initialized()),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: nowTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionFalse, Reason: "TestNameWaitingForHuman",
					Message:            "children waiting for human:\n  TestName: waiting for MR approval (WaitingForHuman)",
					LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionFalse, Reason: "NotYetAvailable", Message: "waiting for resource to be available", LastTransitionTime: nowTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "NotAvailable", Message: "resource is not available", LastTransitionTime: nowTime},
				{Type: ConditionTypeUpToDate, Status: metav1.ConditionFalse, Reason: "NotYetInitialized", Message: "waiting for resource to be initialized", LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "WaitingForHuman: init parent, uninit child — Available unchanged, only UpToDate amplified",
			parent: referenceParent(t, Synced(), Available(), initialized()),
			children: map[string]*exampleResource{
				"TestName": referenceChild(t, "testname", NotSynced("WaitingForHuman", errors.New("waiting for human intervention"))),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionTrue, Reason: "Available", LastTransitionTime: parentTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionTrue, Reason: "FullyAvailable", LastTransitionTime: nowTime},
				{Type: ConditionTypeUpToDate, Status: metav1.ConditionFalse, Reason: "TestNameWaitingForHuman",
					Message:            "children waiting for human:\n  TestName: waiting for human intervention (WaitingForHuman)",
					LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "WaitingForHuman: init parent, init child — Available unchanged, only UpToDate amplified",
			parent: referenceParent(t, Synced(), Available(), initialized()),
			children: map[string]*exampleResource{
				"TestName": referenceChild(t, "testname", NotSynced("WaitingForHuman", errors.New("waiting for MR approval")), Available(), NonDegraded(), initialized()),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionTrue, Reason: "Available", LastTransitionTime: parentTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionTrue, Reason: "FullyAvailable", LastTransitionTime: nowTime},
				{Type: ConditionTypeUpToDate, Status: metav1.ConditionFalse, Reason: "TestNameWaitingForHuman",
					Message:            "children waiting for human:\n  TestName: waiting for MR approval (WaitingForHuman)",
					LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "WaitingForHuman: UpToDate=False/WaitingForHuman on child amplifies through UpToDate",
			parent: referenceParent(t, Synced(), Available(), initialized()),
			children: map[string]*exampleResource{
				"TestName": referenceChild(t, "testname", Synced(), Available(), NonDegraded(), NotUpToDate("WaitingForHuman", errors.New("waiting for manual approval")), initialized()),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionTrue, Reason: "Available", LastTransitionTime: parentTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionTrue, Reason: "FullyAvailable", LastTransitionTime: nowTime},
				{Type: ConditionTypeUpToDate, Status: metav1.ConditionFalse, Reason: "TestNameWaitingForHuman",
					Message:            "children waiting for human:\n  TestName: waiting for manual approval (WaitingForHuman)",
					LastTransitionTime: nowTime},
			},
			changed: true,
		},
		{
			name:   "WaitingForHuman: failed takes priority over WaitingForHuman",
			parent: referenceParent(t, Synced(), Available(), initialized()),
			children: map[string]*exampleResource{
				"TestName1": referenceChild(t, "testname1", NotSynced("WaitingForHuman", errors.New("waiting for human")), Available(), NonDegraded(), initialized()),
				"TestName2": referenceChild(t, "testname2", Synced(), NotAvailable("SomethingFailed", errors.New("something failed")), initialized()),
			},
			args: []metav1.Condition{Synced(), Available(), NonDegraded()},
			expected: []metav1.Condition{
				{Type: ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: "Synced", LastTransitionTime: parentTime},
				{Type: ConditionTypeAvailable, Status: metav1.ConditionFalse, Reason: "ChildrenFailed",
					Message:            "children failed:\n  TestName2: something failed (SomethingFailed)",
					LastTransitionTime: nowTime},
				{Type: ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized", LastTransitionTime: parentTime},
				{Type: ConditionTypeNonDegraded, Status: metav1.ConditionFalse, Reason: "NotAvailable", Message: "resource is not available", LastTransitionTime: nowTime},
			},
			changed: true,
		},
	}

	rules := InfraRules
	rules.Now = func() time.Time { return nowTime.Time }
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			children := make(map[string]Object, len(tt.children))
			for name, child := range tt.children {
				children[name] = child
			}
			changed, err := Aggregate(tt.parent, children, rules, tt.args...)
			if err != nil {
				t.Fatal(err)
			}
			if changed != tt.changed {
				t.Errorf("changed: got %v, want %v", changed, tt.changed)
			}

			// The source semantic suite excludes messages; the separate literal-message suite covers those.
			expected := cloneConditions(tt.expected)
			actual := cloneConditions(tt.parent.GetConditions())
			stripConditionMessages(expected)
			stripConditionMessages(actual)
			// Conditions are a map keyed by type; composition does not preserve incidental insertion order.
			sort.Slice(expected, func(i, j int) bool { return expected[i].Type < expected[j].Type })
			sort.Slice(actual, func(i, j int) bool { return actual[i].Type < actual[j].Type })
			if diff := cmp.Diff(expected, actual); diff != "" {
				t.Errorf("Conditions mismatch (-want +got):\n%s", diff)
			}
			changed, err = Aggregate(tt.parent, children, rules, tt.args...)
			if err != nil {
				t.Fatal(err)
			}
			if changed {
				t.Error("identical observations changed Conditions on the second evaluation")
			}
		})
	}
}

func TestReferenceAggregateSubConditions(t *testing.T) {
	tests := []struct {
		name     string
		parent   *exampleResource
		cond     metav1.Condition
		expected metav1.Condition
	}{
		{
			name:   "No sub-conditions - should return original condition",
			parent: referenceParent(t),
			cond: metav1.Condition{
				Type:   ConditionTypeAvailable,
				Status: metav1.ConditionTrue,
				Reason: "Available",
			},
			expected: metav1.Condition{
				Type:   ConditionTypeAvailable,
				Status: metav1.ConditionTrue,
				Reason: "Available",
			},
		},
		{
			name: "Original condition is false - should return original condition",
			parent: referenceParent(t, metav1.Condition{
				Type:    "AvailableHealth",
				Status:  metav1.ConditionFalse,
				Reason:  "HealthCheckFailed",
				Message: "Health check failed",
			}),
			cond: metav1.Condition{
				Type:    ConditionTypeAvailable,
				Status:  metav1.ConditionFalse,
				Reason:  "NotReady",
				Message: "Resource not ready",
			},
			expected: metav1.Condition{
				Type:    ConditionTypeAvailable,
				Status:  metav1.ConditionFalse,
				Reason:  "NotReady",
				Message: "Resource not ready",
			},
		},
		{
			name: "One failed sub-condition",
			parent: referenceParent(t, metav1.Condition{
				Type:    "AvailableHealth",
				Status:  metav1.ConditionFalse,
				Reason:  "HealthCheckFailed",
				Message: "Health check failed",
			}),
			cond: metav1.Condition{
				Type:   ConditionTypeAvailable,
				Status: metav1.ConditionTrue,
				Reason: "Available",
			},
			expected: metav1.Condition{
				Type:    ConditionTypeAvailable,
				Status:  metav1.ConditionFalse,
				Reason:  "HealthCheckFailed",
				Message: "failed Health check: Health check failed",
			},
		},
		{
			name: "Multiple failed sub-conditions",
			parent: referenceParent(t, metav1.Condition{
				Type:    "AvailableHealth",
				Status:  metav1.ConditionFalse,
				Reason:  "HealthCheckFailed",
				Message: "Health check failed",
			}, metav1.Condition{
				Type:    "AvailableNodes",
				Status:  metav1.ConditionFalse,
				Reason:  "NodeCheckFailed",
				Message: "Node check failed",
			}),
			cond: metav1.Condition{
				Type:   ConditionTypeAvailable,
				Status: metav1.ConditionTrue,
				Reason: "Available",
			},
			expected: metav1.Condition{
				Type:    ConditionTypeAvailable,
				Status:  metav1.ConditionFalse,
				Reason:  "FailedChecks",
				Message: "failed checks: Health, Nodes",
			},
		},
		{
			name: "One false sub-condition (non-failed reason)",
			parent: referenceParent(t, metav1.Condition{
				Type:    "AvailableHealth",
				Status:  metav1.ConditionFalse,
				Reason:  "HealthCheckFlaky",
				Message: "Health check is flaky",
			}),
			cond: metav1.Condition{
				Type:   ConditionTypeAvailable,
				Status: metav1.ConditionTrue,
				Reason: "Available",
			},
			expected: metav1.Condition{
				Type:    ConditionTypeAvailable,
				Status:  metav1.ConditionFalse,
				Reason:  "HealthCheckFlaky",
				Message: "false Health check: Health check is flaky",
			},
		},
		{
			name: "Multiple false sub-conditions (non-failed reasons)",
			parent: referenceParent(t, metav1.Condition{
				Type:    "AvailableHealth",
				Status:  metav1.ConditionFalse,
				Reason:  "HealthCheckFlaky",
				Message: "Health check is flaky",
			}, metav1.Condition{
				Type:    "AvailableNodes",
				Status:  metav1.ConditionFalse,
				Reason:  "InsufficientNodes",
				Message: "Not enough nodes",
			}),
			cond: metav1.Condition{
				Type:   ConditionTypeAvailable,
				Status: metav1.ConditionTrue,
				Reason: "Available",
			},
			expected: metav1.Condition{
				Type:    ConditionTypeAvailable,
				Status:  metav1.ConditionFalse,
				Reason:  "FalseChecks",
				Message: "false checks: Health, Nodes",
			},
		},
		{
			name: "Mixed failed and non-failed sub-conditions - failed takes precedence",
			parent: referenceParent(t, metav1.Condition{
				Type:    "AvailableHealth",
				Status:  metav1.ConditionFalse,
				Reason:  "HealthCheckFailed",
				Message: "Health check failed",
			}, metav1.Condition{
				Type:    "AvailableNodes",
				Status:  metav1.ConditionFalse,
				Reason:  "InsufficientNodes",
				Message: "Not enough nodes",
			}),
			cond: metav1.Condition{
				Type:   ConditionTypeAvailable,
				Status: metav1.ConditionTrue,
				Reason: "Available",
			},
			expected: metav1.Condition{
				Type:    ConditionTypeAvailable,
				Status:  metav1.ConditionFalse,
				Reason:  "HealthCheckFailed",
				Message: "failed Health check: Health check failed",
			},
		},
		{
			name: "Sub-conditions with true status should be ignored",
			parent: referenceParent(t, metav1.Condition{
				Type:   "AvailableHealth",
				Status: metav1.ConditionTrue,
				Reason: "HealthCheckPassed",
			}, metav1.Condition{
				Type:    "AvailableNodes",
				Status:  metav1.ConditionFalse,
				Reason:  "NodeCheckFailed",
				Message: "Node check failed",
			}),
			cond: metav1.Condition{
				Type:   ConditionTypeAvailable,
				Status: metav1.ConditionTrue,
				Reason: "Available",
			},
			expected: metav1.Condition{
				Type:    ConditionTypeAvailable,
				Status:  metav1.ConditionFalse,
				Reason:  "NodeCheckFailed",
				Message: "failed Nodes check: Node check failed",
			},
		},
		{
			name: "Sub-conditions with unknown status should be ignored",
			parent: referenceParent(t, metav1.Condition{
				Type:   "AvailableHealth",
				Status: metav1.ConditionUnknown,
				Reason: "HealthCheckUnknown",
			}, metav1.Condition{
				Type:    "AvailableNodes",
				Status:  metav1.ConditionFalse,
				Reason:  "NodeCheckFailed",
				Message: "Node check failed",
			}),
			cond: metav1.Condition{
				Type:   ConditionTypeAvailable,
				Status: metav1.ConditionTrue,
				Reason: "Available",
			},
			expected: metav1.Condition{
				Type:    ConditionTypeAvailable,
				Status:  metav1.ConditionFalse,
				Reason:  "NodeCheckFailed",
				Message: "failed Nodes check: Node check failed",
			},
		},
		{
			name: "Non-prefixed conditions should be ignored",
			parent: referenceParent(t, metav1.Condition{
				Type:    "Ready",
				Status:  metav1.ConditionFalse,
				Reason:  "NotReady",
				Message: "Resource not ready",
			}, metav1.Condition{
				Type:    "AvailableNodes",
				Status:  metav1.ConditionFalse,
				Reason:  "NodeCheckFailed",
				Message: "Node check failed",
			}),
			cond: metav1.Condition{
				Type:   ConditionTypeAvailable,
				Status: metav1.ConditionTrue,
				Reason: "Available",
			},
			expected: metav1.Condition{
				Type:    ConditionTypeAvailable,
				Status:  metav1.ConditionFalse,
				Reason:  "NodeCheckFailed",
				Message: "failed Nodes check: Node check failed",
			},
		},
		{
			name: "Base condition type should be ignored",
			parent: referenceParent(t, metav1.Condition{
				Type:    ConditionTypeAvailable,
				Status:  metav1.ConditionFalse,
				Reason:  "NotAvailable",
				Message: "Resource not available",
			}, metav1.Condition{
				Type:    "AvailableNodes",
				Status:  metav1.ConditionFalse,
				Reason:  "NodeCheckFailed",
				Message: "Node check failed",
			}),
			cond: metav1.Condition{
				Type:   ConditionTypeAvailable,
				Status: metav1.ConditionTrue,
				Reason: "Available",
			},
			expected: metav1.Condition{
				Type:    ConditionTypeAvailable,
				Status:  metav1.ConditionFalse,
				Reason:  "NodeCheckFailed",
				Message: "failed Nodes check: Node check failed",
			},
		},
		{
			name: "Sub-conditions sorted by type",
			parent: referenceParent(t, metav1.Condition{
				Type:    "AvailableZone",
				Status:  metav1.ConditionFalse,
				Reason:  "ZoneCheckFailed",
				Message: "Zone check failed",
			}, metav1.Condition{
				Type:    "AvailableHealth",
				Status:  metav1.ConditionFalse,
				Reason:  "HealthCheckFailed",
				Message: "Health check failed",
			}),
			cond: metav1.Condition{
				Type:   ConditionTypeAvailable,
				Status: metav1.ConditionTrue,
				Reason: "Available",
			},
			expected: metav1.Condition{
				Type:    ConditionTypeAvailable,
				Status:  metav1.ConditionFalse,
				Reason:  "FailedChecks",
				Message: "failed checks: Health, Zone",
			},
		},
		{
			name: "Works with different base condition types",
			parent: referenceParent(t, metav1.Condition{
				Type:    "SyncedDatabase",
				Status:  metav1.ConditionFalse,
				Reason:  "DatabaseSyncFailed",
				Message: "Database sync failed",
			}),
			cond: metav1.Condition{
				Type:   ConditionTypeSynced,
				Status: metav1.ConditionTrue,
				Reason: "Synced",
			},
			expected: metav1.Condition{
				Type:    ConditionTypeSynced,
				Status:  metav1.ConditionFalse,
				Reason:  "DatabaseSyncFailed",
				Message: "failed Database check: Database sync failed",
			},
		},
		{
			name: "More than 3 sub-conditions get truncated in message",
			parent: referenceParent(t, metav1.Condition{
				Type:    "AvailableHealth",
				Status:  metav1.ConditionFalse,
				Reason:  "HealthCheckFailed",
				Message: "Health check failed",
			}, metav1.Condition{
				Type:    "AvailableNodes",
				Status:  metav1.ConditionFalse,
				Reason:  "NodeCheckFailed",
				Message: "Node check failed",
			}, metav1.Condition{
				Type:    "AvailableStorage",
				Status:  metav1.ConditionFalse,
				Reason:  "StorageCheckFailed",
				Message: "Storage check failed",
			}, metav1.Condition{
				Type:    "AvailableNetwork",
				Status:  metav1.ConditionFalse,
				Reason:  "NetworkCheckFailed",
				Message: "Network check failed",
			}),
			cond: metav1.Condition{
				Type:   ConditionTypeAvailable,
				Status: metav1.ConditionTrue,
				Reason: "Available",
			},
			expected: metav1.Condition{
				Type:    ConditionTypeAvailable,
				Status:  metav1.ConditionFalse,
				Reason:  "FailedChecks",
				Message: "failed checks: Health, Network, Nodes, and 1 more",
			},
		},
		{
			name: "UpToDateNoDrift=False makes UpToDate=False via prefix matching",
			parent: referenceParent(t, metav1.Condition{
				Type:    "UpToDateNoDrift",
				Status:  metav1.ConditionFalse,
				Reason:  "DriftDetected",
				Message: "Drift Plan detected 1 changes",
			}),
			cond: metav1.Condition{
				Type:   ConditionTypeUpToDate,
				Status: metav1.ConditionTrue,
				Reason: "UpToDate",
			},
			expected: metav1.Condition{
				Type:    ConditionTypeUpToDate,
				Status:  metav1.ConditionFalse,
				Reason:  "DriftDetected",
				Message: "false NoDrift check: Drift Plan detected 1 changes",
			},
		},
		{
			name: "UpToDateNoDrift=True does not affect UpToDate=True",
			parent: referenceParent(t, metav1.Condition{
				Type:   "UpToDateNoDrift",
				Status: metav1.ConditionTrue,
				Reason: "NoDrift",
			}),
			cond: metav1.Condition{
				Type:   ConditionTypeUpToDate,
				Status: metav1.ConditionTrue,
				Reason: "UpToDate",
			},
			expected: metav1.Condition{
				Type:   ConditionTypeUpToDate,
				Status: metav1.ConditionTrue,
				Reason: "UpToDate",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := LocalChecks(tt.cond.Type).Evaluate(tt.parent, nil, tt.cond, parentTime.Time)
			if err != nil {
				t.Fatal(err)
			}

			// The public local evaluator also encodes timestamps, using the fixed clock supplied above.
			expected := tt.expected
			if expected.LastTransitionTime.IsZero() {
				expected.LastTransitionTime = parentTime
			}
			if diff := cmp.Diff(expected, *actual); diff != "" {
				t.Errorf("local Condition mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestReferenceAggregateMessageFormatting(t *testing.T) {
	testCases := []struct {
		name          string
		parent        *exampleResource
		children      map[string]*exampleResource
		args          []metav1.Condition
		conditionType string
		expected      string
	}{
		{
			name:   "NonDegraded aggregates not synced child",
			parent: referenceParent(t, Synced(), Available(), initialized()),
			children: map[string]*exampleResource{
				"TestName": referenceChild(t, "testname", NotSynced("SyncError", fmt.Errorf("sync error"))),
			},
			args:          []metav1.Condition{Synced(), Available(), NonDegraded()},
			conditionType: ConditionTypeNonDegraded,
			expected: `children are not synced:
  TestName: sync error (SyncError)`,
		},
		{
			name: "Multiple failed children with parent failed checks",
			parent: referenceParent(t, Synced(), Available(), initialized(), metav1.Condition{
				Type:    "AvailableHealth",
				Status:  metav1.ConditionFalse,
				Reason:  "HealthCheckFailed",
				Message: "Health check failed",
			}),
			children: map[string]*exampleResource{
				"ChildA": referenceChild(t, "childa", Synced(), NotAvailable("Failed", fmt.Errorf("failure a")), initialized()),
				"ChildB": referenceChild(t, "childb", Synced(), NotAvailable("Failed", fmt.Errorf("failure b")), initialized()),
			},
			args:          []metav1.Condition{Synced(), Available(), NonDegraded()},
			conditionType: ConditionTypeAvailable,
			expected: `failed Health check, children failed: ChildA, ChildB:
  Health: Health check failed (HealthCheckFailed)
  ChildA: failure a (Failed)
  ChildB: failure b (Failed)`,
		},
		{
			name:   "Deleting: single tear-down job running with multi-line detail preserves nested URL",
			parent: referenceDeleting(referenceParent(t, Synced(), Available(), initialized(), NonDegraded(), RawUpToDate())),
			children: map[string]*exampleResource{
				"TeardownPipeline": referenceChild(t, "teardown1", Synced(),
					referenceJobRunning("Provisioning", "GitLabResource ns/name on the host is provisioning:\n  pipeline: https://git.example.invalid/project/-/merge_requests/123"),
					referenceJobNotSucceeded("Running", fmt.Errorf("still running")),
					referenceJobNotFailed("Running", fmt.Errorf("still running"))),
			},
			args:          []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			conditionType: ConditionTypeDeleting,
			expected: `tear-down jobs running:
  TeardownPipeline: GitLabResource ns/name on the host is provisioning:
    pipeline: https://git.example.invalid/project/-/merge_requests/123 (Provisioning)`,
		},
		{
			name:   "Deleting: child with multi-line Deleting message preserves nested URL through the chain",
			parent: referenceDeleting(referenceParent(t, Synced(), Available(), initialized(), NonDegraded(), RawUpToDate())),
			children: map[string]*exampleResource{
				"EKSCluster": referenceDeleting(referenceChild(t, "ekscluster1", Synced(), metav1.Condition{
					Type:    ConditionTypeDeleting,
					Status:  metav1.ConditionTrue,
					Reason:  ConditionReasonDeleting,
					Message: "TeardownPipeline is running: GitLabResource ns/name on the host is provisioning:\n  pipeline: https://git.example.invalid/project/-/merge_requests/123",
				})),
			},
			args:          []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			conditionType: ConditionTypeDeleting,
			expected: `deleting children:
  EKSCluster: TeardownPipeline is running: GitLabResource ns/name on the host is provisioning:
    pipeline: https://git.example.invalid/project/-/merge_requests/123 (Deleting)`,
		},
		{
			name:   "Deleting: single tear-down job running with single-line detail",
			parent: referenceDeleting(referenceParent(t, Synced(), Available(), initialized(), NonDegraded(), RawUpToDate())),
			children: map[string]*exampleResource{
				"TestNameTearDown": referenceChild(t, "td", Synced(),
					referenceJobRunning("Running", "tearing down"),
					referenceJobNotSucceeded("Running", fmt.Errorf("still running")),
					referenceJobNotFailed("Running", fmt.Errorf("still running"))),
			},
			args:          []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			conditionType: ConditionTypeDeleting,
			expected: `tear-down jobs running:
  TestNameTearDown: tearing down (Running)`,
		},
		{
			name:   "Deleting: multiple tear-down jobs running",
			parent: referenceDeleting(referenceParent(t, Synced(), Available(), initialized(), NonDegraded(), RawUpToDate())),
			children: map[string]*exampleResource{
				"TD1": referenceChild(t, "td1", Synced(),
					referenceJobRunning("Running", "tearing down"),
					referenceJobNotSucceeded("Running", fmt.Errorf("still running")),
					referenceJobNotFailed("Running", fmt.Errorf("still running"))),
				"TD2": referenceChild(t, "td2", Synced(),
					referenceJobRunning("Running", "tearing down"),
					referenceJobNotSucceeded("Running", fmt.Errorf("still running")),
					referenceJobNotFailed("Running", fmt.Errorf("still running"))),
			},
			args:          []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			conditionType: ConditionTypeDeleting,
			expected: `tear-down jobs running: TD1, TD2:
  TD1: tearing down (Running)
  TD2: tearing down (Running)`,
		},
		{
			name:   "Deleting: many deleting children without per-child detail collapses to summary only",
			parent: referenceDeleting(referenceParent(t, Synced(), Available(), initialized(), NonDegraded(), RawUpToDate())),
			children: map[string]*exampleResource{
				"TestName1": referenceDeleting(referenceChild(t, "tn1", Synced())),
				"TestName2": referenceDeleting(referenceChild(t, "tn2", Synced())),
			},
			args:          []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			conditionType: ConditionTypeDeleting,
			expected:      `deleting children: TestName1, TestName2`,
		},
		{
			name:   "Deleting: deletion error among other deleting children surfaces nested detail",
			parent: referenceDeleting(referenceParent(t, Synced(), Available(), initialized(), NonDegraded(), RawUpToDate())),
			children: map[string]*exampleResource{
				"TestName1": referenceDeleting(referenceChild(t, "tn1", Synced(), DeletionError("DeletionError", fmt.Errorf("error during deletion")))),
				"TestName2": referenceDeleting(referenceChild(t, "tn2", Synced(), Deleting("delete msg"))),
			},
			args:          []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			conditionType: ConditionTypeDeleting,
			expected: `deleting 1 resources, with errors:
  TestName1: error during deletion (DeletionError)`,
		},
		{
			name:   "Deleting: multiple deletion errors list affected children and surface their detail",
			parent: referenceDeleting(referenceParent(t, Synced(), Available(), initialized(), NonDegraded(), RawUpToDate())),
			children: map[string]*exampleResource{
				"TestName1": referenceDeleting(referenceChild(t, "tn1", Synced(), DeletionError("DeletionError", fmt.Errorf("err 1")))),
				"TestName2": referenceDeleting(referenceChild(t, "tn2", Synced(), Deleting("delete msg"))),
				"TestName3": referenceDeleting(referenceChild(t, "tn3", Synced(), DeletionError("DeletionError", fmt.Errorf("err 3")))),
				"TestName4": referenceDeleting(referenceChild(t, "tn4", Synced(), Deleting("delete msg"))),
			},
			args:          []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			conditionType: ConditionTypeDeleting,
			expected: `deleting 2 resources, with errors: TestName1, TestName3:
  TestName1: err 1 (DeletionError)
  TestName3: err 3 (DeletionError)`,
		},
		{
			name:   "Deleting: sync failure on the only deleting child shows the sync error",
			parent: referenceDeleting(referenceParent(t, Synced(), Available(), initialized(), NonDegraded(), RawUpToDate())),
			children: map[string]*exampleResource{
				"TestName1": referenceDeleting(referenceChild(t, "tn1", NotSynced("SyncError", fmt.Errorf("sync error")))),
			},
			args:          []metav1.Condition{Synced(), Available(), NonDegraded(), RawUpToDate()},
			conditionType: ConditionTypeDeleting,
			expected: `with errors:
  TestName1: sync error (SyncError)`,
		},
	}

	rules := InfraRules
	rules.Now = func() time.Time { return time.Date(2025, 5, 27, 0, 0, 0, 0, time.UTC) }
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			children := make(map[string]Object, len(tc.children))
			for name, child := range tc.children {
				children[name] = child
			}
			if _, err := Aggregate(tc.parent, children, rules, tc.args...); err != nil {
				t.Fatal(err)
			}
			actual, _ := Lookup(tc.parent, tc.conditionType)
			if actual.Message != tc.expected {
				t.Errorf("message mismatch (-want +got):\n%s", cmp.Diff(tc.expected, actual.Message))
			}
		})
	}
}

func cloneConditions(in []metav1.Condition) []metav1.Condition {
	out := make([]metav1.Condition, len(in))
	copy(out, in)
	return out
}
func stripConditionMessages(conditions []metav1.Condition) {
	for i := range conditions {
		conditions[i].Message = ""
	}
}
