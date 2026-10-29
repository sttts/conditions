// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package infra

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
	"time"
)

// Reference fixtures are already-computed assertions; these helpers do not implement aggregation policy.
const (
	ConditionTypeAvailable   = "Available"
	ConditionTypeInitialized = "Initialized"
	ConditionTypeNonDegraded = "NonDegraded"
	ConditionTypeUpToDate    = "UpToDate"
	ConditionTypeSynced      = "Synced"
	ConditionTypeDeleting    = "Deleting"
	ConditionReasonDeleting  = "Deleting"
)

func referenceParent(t *testing.T, conditions ...metav1.Condition) *exampleResource {
	t.Helper()
	for i := range conditions {
		conditions[i].LastTransitionTime = parentTime
	}
	return &exampleResource{ObjectMeta: metav1.ObjectMeta{Name: "parent", Generation: 42}, Status: exampleStatus{Conditions: conditions}}
}
func referenceChild(t *testing.T, name string, conditions ...metav1.Condition) *exampleResource {
	t.Helper()
	resource := referenceParent(t, conditions...)
	resource.Name = name
	resource.CreationTimestamp = referenceLifecycleTime()
	return resource
}
func referenceLifecycleTime() metav1.Time {
	return metav1.NewTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
}
func referenceDeleting(resource *exampleResource) *exampleResource {
	timestamp := referenceLifecycleTime()
	resource.SetDeletionTimestamp(&timestamp)
	return resource
}
func referenceCondition(typ string, status metav1.ConditionStatus, reason, message string) metav1.Condition {
	return metav1.Condition{Type: typ, Status: status, Reason: reason, Message: message}
}
func Available() metav1.Condition {
	return referenceCondition("Available", metav1.ConditionTrue, "Available", "")
}
func Synced() metav1.Condition {
	return referenceCondition("Synced", metav1.ConditionTrue, "Synced", "")
}
func NonDegraded() metav1.Condition {
	return referenceCondition("NonDegraded", metav1.ConditionTrue, "FullyAvailable", "")
}
func RawUpToDate() metav1.Condition {
	return referenceCondition("UpToDate", metav1.ConditionTrue, "UpToDate", "")
}
func initialized() metav1.Condition {
	return referenceCondition("Initialized", metav1.ConditionTrue, "Initialized", "")
}
func notInitializedYet() metav1.Condition {
	return referenceCondition("Initialized", metav1.ConditionFalse, "NotYetAvailable", "waiting for resource to be available")
}
func NotSynced(reason string, err error) metav1.Condition {
	return referenceCondition("Synced", metav1.ConditionFalse, reason, err.Error())
}
func NotAvailable(reason string, err error) metav1.Condition {
	return referenceCondition("Available", metav1.ConditionFalse, reason, err.Error())
}
func Degraded(reason string, err error) metav1.Condition {
	return referenceCondition("NonDegraded", metav1.ConditionFalse, reason, err.Error())
}
func NotUpToDate(reason string, err error) metav1.Condition {
	return referenceCondition("UpToDate", metav1.ConditionFalse, reason, err.Error())
}
func UnknownAvailable(reason string, err error) metav1.Condition {
	return referenceCondition("Available", metav1.ConditionUnknown, reason, err.Error())
}
func UnknownNonDegraded(reason string, err error) metav1.Condition {
	return referenceCondition("NonDegraded", metav1.ConditionUnknown, reason, err.Error())
}
func Deleting(message string) metav1.Condition {
	if message == "" {
		message = "resource is being deleted"
	}
	return referenceCondition("Deleting", metav1.ConditionTrue, "Deleting", message)
}
func DeletionError(reason string, err error) metav1.Condition {
	return referenceCondition("Deleting", metav1.ConditionTrue, reason, err.Error())
}
func referenceJobRunning(reason, message string) metav1.Condition {
	return referenceCondition("Running", metav1.ConditionTrue, reason, message)
}
func referenceJobNotRunning(reason string, err error) metav1.Condition {
	return referenceCondition("Running", metav1.ConditionFalse, reason, err.Error())
}
func referenceJobSucceeded() metav1.Condition {
	return referenceCondition("Succeeded", metav1.ConditionTrue, "Succeeded", "")
}
func referenceJobNotSucceeded(reason string, err error) metav1.Condition {
	return referenceCondition("Succeeded", metav1.ConditionFalse, reason, err.Error())
}
func referenceJobFailed(reason string, err error) metav1.Condition {
	return referenceCondition("Failed", metav1.ConditionTrue, reason, err.Error())
}
func referenceJobNotFailed(reason string, err error) metav1.Condition {
	return referenceCondition("Failed", metav1.ConditionFalse, reason, err.Error())
}
