// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package infra expresses the Infra reference behavior through the shared conditions package.
package infra

import (
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"reflect"
	"sort"
	"strings"
	"time"
)

// ServingInputs declares the reference's Condition vocabulary and every source polarity.
type ServingInputs struct{ Available, Initialized, NonDegraded, UpToDate, Synced, Running, Succeeded, Failed, Deleting ConditionType }

// ServingChildren selects one diagnosis per child with separately configured propagation rules.
type ServingChildren struct {
	Inputs                                            ServingInputs
	Condition                                         ConditionType
	Classifier                                        Classifier
	False, Failure, Human, Unknown, SyncError         Propagation
	Uninitialized, Unavailable, JobRunning, JobFailed Propagation
	BadDescription                                    string
}

func (p ServingChildren) Select(parent, child Object) []Contribution {
	result, ok := p.selectOne(parent, child)
	if !ok {
		return nil
	}
	return []Contribution{result}
}

func (p ServingChildren) selectOne(parent, child Object) (Contribution, bool) {
	v := p.Inputs
	if isDeleting(parent) && isDeleting(child) {
		return Contribution{}, false
	}
	synced, running, failed := v.Synced.Read(child), v.Running.Read(child), v.Failed.Read(child)
	syncBad := v.Synced.Assessment(synced) == Bad
	syncHuman := syncBad && classify(p.Classifier, synced) == WaitingForHuman
	allows := func(rule Propagation) bool { return rule != nil && rule.Allows(parent, child) }
	pick := func(check ConditionType, escalation Escalation, fallback string) (Contribution, bool) {
		return Contribution{State: State{Bad, escalation}, Condition: check.Read(child), Source: child, Fallback: fallback}, true
	}

	// A job-like child is completely handled here, including its no-contribution cases.
	if running.Status != metav1.ConditionUnknown {
		switch {
		case v.Failed.Assessment(failed) == Bad && allows(p.Failure):
			return pick(v.Failed, Failed, "failed")
		case syncHuman && allows(p.Human):
			return pick(v.Synced, WaitingForHuman, "waiting for human")
		case syncBad && allows(p.SyncError):
			return pick(v.Synced, None, "not synced")
		case v.Running.Assessment(running) == Bad && allows(p.JobRunning):
			return pick(v.Running, None, "is running")
		case v.Failed.Assessment(failed) == Bad && allows(p.JobFailed):
			return pick(v.Failed, None, "failed")
		case v.Succeeded.IsGood(child):
			return Contribution{}, false
		case v.Running.Assessment(running) == Good && allows(p.JobRunning):
			return pick(v.Running, None, "is pending")
		default:
			return Contribution{}, false
		}
	}
	available, current := v.Available.Read(child), p.Condition.Read(child)
	availableBad := v.Available.Assessment(available) == Bad
	currentBad := p.Condition.Assessment(current) == Bad

	// Case order preserves failed availability before ordinary sync failure on the same child.
	switch {
	case availableBad && classify(p.Classifier, available) == Failed && allows(p.Unavailable) && allows(p.Failure):
		return pick(v.Available, Failed, "failed")
	case currentBad && classify(p.Classifier, current) == Failed && allows(p.False) && allows(p.Failure):
		return pick(p.Condition, Failed, "failed")
	case availableBad && classify(p.Classifier, available) == WaitingForHuman && allows(p.Unavailable) && allows(p.Human):
		return pick(v.Available, WaitingForHuman, "waiting for human")
	case currentBad && classify(p.Classifier, current) == WaitingForHuman && allows(p.False) && allows(p.Human):
		return pick(p.Condition, WaitingForHuman, "waiting for human")
	case syncHuman && allows(p.SyncError) && allows(p.Human):
		return pick(v.Synced, WaitingForHuman, "waiting for human")
	case syncHuman && v.Initialized.IsGood(parent):
		return Contribution{}, false
	case syncHuman && v.Initialized.IsGood(child):
		return pick(v.Synced, WaitingForHuman, "waiting for human")
	case syncBad && allows(p.SyncError):
		return pick(v.Synced, None, "not synced")
	case availableBad && allows(p.Unavailable):
		result, _ := pick(v.Available, None, "not available")
		result.Uninitialized = !v.Initialized.IsGood(child)
		if result.Uninitialized {
			result.Fallback = "not yet initialized"
		}
		return result, true
	case currentBad && allows(p.False):
		return pick(p.Condition, None, p.BadDescription)
	case v.Initialized.IsBad(child) && allows(p.Uninitialized):
		result, _ := pick(v.Available, None, "not yet initialized")
		result.Uninitialized = true
		return result, true
	case p.Condition.Assessment(current) == Unknown && allows(p.Unknown):
		text := p.BadDescription + ": state is still unknown"
		return Contribution{State: State{Bad, None}, Condition: current, Source: child, Detail: &text, FromUnknown: true}, true
	default:
		return Contribution{}, false
	}
}

// ParentChildReduction preserves local/child origin instead of applying one global severity sort.
type ParentChildReduction struct {
	Inputs                    ServingInputs
	Output                    ConditionType
	Classifier                Classifier
	BadSuffix, BadDescription string
	LocalOnly                 bool
}

func (r ParentChildReduction) Reduce(base metav1.Condition, objects map[string]Object, local, children []Contribution) Outcome {
	var lf, lh, lb, cf, ch, cb, cs, cu []Contribution
	for _, c := range local {
		switch c.State.Escalation {
		case Failed:
			lf = append(lf, c)
		case WaitingForHuman:
			lh = append(lh, c)
		default:
			lb = append(lb, c)
		}
	}
	for _, c := range children {
		switch {
		case c.State.Escalation == Failed:
			cf = append(cf, c)
		case c.State.Escalation == WaitingForHuman:
			ch = append(ch, c)
		case c.Condition.Type == r.Inputs.Synced.Type:
			cs = append(cs, c)
		default:
			cb = append(cb, c)
		}
		if c.FromUnknown {
			cu = append(cu, c)
		}
	}

	// Unknown local bases allow local checks but never child aggregation.
	if r.LocalOnly || r.Output.Assessment(base) == Unknown {
		for _, group := range []struct {
			items               []Contribution
			multiple, one, many string
			escalation          Escalation
		}{
			{lf, "FailedChecks", "failed %s check: %s", "failed checks: ", Failed},
			{lh, "WaitingForHuman", "%s check waiting for human: %s", "checks waiting for human: ", WaitingForHuman},
			{lb, "FalseChecks", "false %s check: %s", "false checks: ", None},
		} {
			if len(group.items) == 1 {
				c := group.items[0]
				return Outcome{State: State{Bad, group.escalation}, Reason: c.Condition.Reason, Summary: fmt.Sprintf(group.one, c.CheckName, c.Condition.Message)}
			}
			if len(group.items) > 1 {
				return Outcome{State: State{Bad, group.escalation}, Reason: group.multiple, Summary: group.many + limitedNames(group.items, true)}
			}
		}
		return copyOutcome(base, r.Output, r.Classifier)
	}
	makeOutcome := func(reason string, escalation Escalation, heads []string, groups ...[]Contribution) Outcome {
		var causes []Cause
		for _, group := range groups {
			for _, c := range group {
				if !c.SuppressDetail {
					causes = append(causes, cause(c))
				}
			}
		}
		return Outcome{State: State{Bad, escalation}, Reason: reason, Summary: joinHeads(heads...), Causes: causes}
	}
	localHead := func(group []Contribution, singular, plural string) string {
		if len(group) == 1 {
			return fmt.Sprintf(singular, group[0].CheckName)
		}
		return plural + limitedNames(group, true)
	}
	badDescription := r.BadDescription
	allUninitialized := len(cb) > 0
	for _, c := range cb {
		allUninitialized = allUninitialized && c.Uninitialized
	}
	if allUninitialized {
		badDescription = "not yet initialized children"
	}

	// These ordinary Go branches are the reference's domain precedence, not interpreter actions.
	switch {
	case len(lf) > 0:
		reason := "FailedChecks"
		if len(lf) == 1 {
			reason = lf[0].Condition.Reason
		}
		return makeOutcome(reason, Failed, []string{localHead(lf, "failed %s check", "failed checks: "), groupHead("children failed", cf), groupHead("not synced", cs)}, lf, cf, cs)
	case len(cf) > 0:
		return makeOutcome(childReason(objects, cf, "Failed"), Failed, []string{groupHead("children failed", cf)}, cf)
	case len(lh) > 0:
		reason := "WaitingForHuman"
		if len(lh) == 1 {
			reason = lh[0].Condition.Reason
		}
		return makeOutcome(reason, WaitingForHuman, []string{localHead(lh, "%s check waiting for human", "checks waiting for human: "), groupHead("children waiting for human", ch), groupHead("not synced", cs)}, lh, ch, cs)
	case len(ch) > 0:
		return makeOutcome(childReason(objects, ch, "WaitingForHuman"), WaitingForHuman, []string{groupHead("children waiting for human", ch)}, ch)
	case len(cb) == 0 && len(cs) > 0:
		return makeOutcome(childReason(objects, cs, "NotSynced"), None, []string{groupHead("children are not synced", cs)}, cs)
	case len(cb) == 1 && len(cs) == 0:
		return makeOutcome(childReason(objects, cb, r.BadSuffix), None, []string{groupHead(badDescription, cb), localGroupHead("false checks", lb)}, cb, lb)
	case len(cb) > 0:
		return makeOutcome("Children"+r.BadSuffix, None, []string{groupHead(badDescription, cb), groupHead("not synced", cs), localGroupHead("false checks", lb)}, cb, cs, lb)
	case len(lb) > 0:
		reason := "FalseChecks"
		head := "false checks: " + limitedNames(lb, true)
		if len(lb) == 1 {
			reason = lb[0].Condition.Reason
			head = lb[0].CheckName + " check false"
		}
		return makeOutcome(reason, None, []string{head}, lb)
	case len(cu) > 0:
		out := makeOutcome(childReason(objects, cu, "StateUnknown"), None, []string{groupHead("children with unknown state", cu)}, cu)
		out.State.Assessment = Unknown
		return out
	default:
		return copyOutcome(base, r.Output, r.Classifier)
	}
}

// DegradationBase handles availability-dependent local gates, independently of child selection.
type DegradationBase struct {
	LocalBaseFirst
	Inputs ServingInputs
}

func (p DegradationBase) Before(parent Object, children map[string]Object, base metav1.Condition) (*Outcome, bool) {
	if result, done := p.LocalBaseFirst.Before(parent, children, base); done {
		return result, true
	}
	if !p.IncludeChildren(base) {
		return nil, false
	}
	switch p.Inputs.Available.Assessment(p.Inputs.Available.Read(parent)) {
	case Bad:
		hasFailure := false
		for _, child := range children {
			if isNilObject(child) {
				continue
			}
			hasFailure = hasFailure || p.Inputs.Failed.IsBad(child) ||
				(p.Inputs.Available.IsBad(child) && classify(p.Classifier, p.Inputs.Available.Read(child)) == Failed) ||
				(p.Inputs.NonDegraded.IsBad(child) && classify(p.Classifier, p.Inputs.NonDegraded.Read(child)) == Failed)
		}
		if p.Inputs.Initialized.IsGood(parent) || !hasFailure {
			out := badOutcome("NotAvailable", "resource is not available")
			return &out, true
		}
	case Unknown:
		out := badOutcome("UnknownAvailability", "availability of resource is unknown")
		out.State.Assessment = Unknown
		return &out, true
	}
	return nil, false
}

// UpdateBase gates updates on the already-computed local initialization Condition.
type UpdateBase struct {
	LocalBaseFirst
	Initialized ConditionType
}

func (p UpdateBase) Before(parent Object, children map[string]Object, base metav1.Condition) (*Outcome, bool) {
	if result, done := p.LocalBaseFirst.Before(parent, children, base); done {
		return result, true
	}
	if !p.IncludeChildren(base) {
		return nil, false
	}
	if isDeleting(parent) {
		out := badOutcome("Deleting", "resource is being deleted")
		return &out, true
	}
	if !p.Initialized.IsGood(parent) {
		out := badOutcome("NotYetInitialized", "waiting for resource to be initialized")
		return &out, true
	}
	return nil, false
}

// DeletionBase preserves a local failed deletion without forcing other outputs terminal.
type DeletionBase struct {
	Classifier Classifier
	Output     ConditionType
}

func (p DeletionBase) Before(parent Object, children map[string]Object, base metav1.Condition) (*Outcome, bool) {
	if !isDeleting(parent) {
		return nil, true
	}
	if classify(p.Classifier, base) == Failed {
		out := copyOutcome(base, p.Output, p.Classifier)
		return &out, true
	}
	return nil, false
}
func (DeletionBase) IncludeChildren(base metav1.Condition) bool { return true }

// DeletingChildren selects one deletion or teardown diagnosis from each supplied child.
type DeletingChildren struct {
	Inputs     ServingInputs
	Classifier Classifier
}

func (p DeletingChildren) Select(parent, child Object) []Contribution {
	result, ok := p.selectOne(parent, child)
	if !ok {
		return nil
	}
	return []Contribution{result}
}

func (p DeletingChildren) selectOne(parent, child Object) (Contribution, bool) {
	pick := func(check ConditionType, escalation Escalation, fallback string) (Contribution, bool) {
		return Contribution{State: State{Bad, escalation}, Condition: check.Read(child), Source: child, Fallback: fallback}, true
	}
	if isDeleting(child) {
		deleting := p.Inputs.Deleting.Read(child)
		if p.Inputs.Deleting.Assessment(deleting) == Bad && deleting.Reason != "Deleting" {
			return pick(p.Inputs.Deleting, None, "fails to delete")
		}
		if p.Inputs.Synced.IsBad(child) {
			return pick(p.Inputs.Synced, None, "fails to sync")
		}
		if deleting.Status != metav1.ConditionUnknown {
			return pick(p.Inputs.Deleting, None, "is being deleted")
		}
		return Contribution{State: State{Bad, None}, Source: child, SuppressDetail: true}, true
	}
	if child.GetCreationTimestamp().Time.Before(parent.GetDeletionTimestamp().Time) || p.Inputs.Running.Read(child).Status == metav1.ConditionUnknown {
		return Contribution{}, false
	}
	switch {
	case p.Inputs.Synced.IsBad(child) && classify(p.Classifier, p.Inputs.Synced.Read(child)) == WaitingForHuman:
		return pick(p.Inputs.Synced, WaitingForHuman, "is waiting for human")
	case p.Inputs.Running.IsBad(child):
		return pick(p.Inputs.Running, None, "is running")
	case p.Inputs.Failed.IsBad(child):
		return pick(p.Inputs.Failed, Failed, "failed")
	default:
		return Contribution{}, false
	}
}

// DeletionReduction keeps deleting children ahead of teardown jobs in normal Go.
type DeletionReduction struct {
	Inputs     ServingInputs
	Classifier Classifier
}

func (p DeletionReduction) Reduce(base metav1.Condition, objects map[string]Object, _ []Contribution, children []Contribution) Outcome {
	var deleting, errors, failed, human, running []Contribution
	for _, c := range children {
		if isDeleting(c.Source) {
			deleting = append(deleting, c)
			if c.Condition.Type == p.Inputs.Synced.Type ||
				(c.Condition.Type == p.Inputs.Deleting.Type && p.Inputs.Deleting.Assessment(c.Condition) == Bad && c.Condition.Reason != "Deleting") {
				errors = append(errors, c)
			}
			continue
		}
		switch c.State.Escalation {
		case Failed:
			failed = append(failed, c)
		case WaitingForHuman:
			human = append(human, c)
		default:
			running = append(running, c)
		}
	}
	outcome := func(reason string, escalation Escalation, summary string, group []Contribution) Outcome {
		var causes []Cause
		for _, c := range group {
			if !c.SuppressDetail {
				causes = append(causes, cause(c))
			}
		}
		return Outcome{State: State{Bad, escalation}, Reason: reason, Summary: summary, Causes: causes}
	}
	if len(deleting) == 0 {
		switch {
		case len(failed) > 0:
			return outcome("TearDownFailed", Failed, groupHead("tear-down jobs failed", failed), failed)
		case len(human) > 0:
			return outcome("WaitingForHuman", WaitingForHuman, groupHead("tear-down jobs waiting for human", human), human)
		case len(running) > 0:
			return outcome("Deleting", None, groupHead("tear-down jobs running", running), running)
		case len(sortedChildren(objects)) > 0:
			var refs []string
			for _, c := range sortedChildren(objects) {
				refs = append(refs, c.Name)
			}
			return outcome("Deleting", None, "resource is being deleted, remaining: "+limitNames(refs), nil)
		default:
			return copyOutcome(base, p.Inputs.Deleting, p.Classifier)
		}
	}
	if len(errors) > 0 {
		prefix := ""
		if n := len(deleting) - len(errors); n > 0 {
			prefix = fmt.Sprintf("deleting %d resources", n)
		}
		return outcome("ChildrenDeletionError", None, joinHeads(prefix, groupHead("with errors", errors)), errors)
	}
	return outcome("Deleting", None, groupHead("deleting children", deleting), deleting)
}

// The reference profile binds Condition names and polarities explicitly as ordinary data.
var InfraConditions = ServingInputs{
	Available: ConditionType{"Available", TrueIsGood}, Initialized: ConditionType{"Initialized", TrueIsGood},
	NonDegraded: ConditionType{"NonDegraded", TrueIsGood}, Synced: ConditionType{"Synced", TrueIsGood},
	UpToDate: ConditionType{"UpToDate", TrueIsGood},
	Running:  ConditionType{"Running", FalseIsGood}, Succeeded: ConditionType{"Succeeded", TrueIsGood},
	Failed: ConditionType{"Failed", FalseIsGood}, Deleting: ConditionType{"Deleting", FalseIsGood},
}
var InfraClassification Classifier = ReasonClassifier{Suffixes: []ReasonSuffix{{"Failed", Failed}, {"WaitingForHuman", WaitingForHuman}}}
var beforeInitialization Propagation = BeforeInitialized{Parent: InfraConditions.Initialized}
var existingChildren Propagation = ExistingChildren{Parent: InfraConditions.Initialized, Child: InfraConditions.Initialized}
var infraMessages Formatter = MultilineMessages{MaxLines: 7, InlineChars: 80}

// Each flat value composes configured implementations of the domain interfaces.
var InfraAvailability = Aggregation{
	Derived: []Derivation{InfraInitialization{}},
	Output:  InfraConditions.Available,
	Parent:  LocalBaseFirst{Base: InfraConditions.Available, Classifier: InfraClassification, OverrideOnDeletion: true},
	Local:   PrefixedChecks{Prefix: "Available", Polarity: TrueIsGood, Classifier: InfraClassification},
	Children: ServingChildren{Inputs: InfraConditions, Condition: InfraConditions.Available, Classifier: InfraClassification,
		False: existingChildren, Failure: existingChildren, Human: Never{}, Unknown: beforeInitialization,
		SyncError: beforeInitialization, JobRunning: beforeInitialization, JobFailed: beforeInitialization,
		BadDescription: "is not available"},
	Reduction: ParentChildReduction{Inputs: InfraConditions, Output: InfraConditions.Available, Classifier: InfraClassification, BadSuffix: "NotAvailable", BadDescription: "unavailable children"},
	Messages:  infraMessages, Generation: PreserveBaseGeneration,
}
var InfraNonDegraded = Aggregation{
	Output: InfraConditions.NonDegraded,
	Parent: DegradationBase{LocalBaseFirst: LocalBaseFirst{Base: InfraConditions.NonDegraded, Classifier: InfraClassification, OverrideOnDeletion: true}, Inputs: InfraConditions},
	Local:  PrefixedChecks{Prefix: "NonDegraded", Polarity: TrueIsGood, Classifier: InfraClassification},
	Children: ServingChildren{Inputs: InfraConditions, Condition: InfraConditions.NonDegraded, Classifier: InfraClassification,
		False: Always{}, Failure: Always{}, Human: Never{}, SyncError: Always{}, Unavailable: Always{}, JobFailed: Always{}, BadDescription: "is degraded"},
	Reduction: ParentChildReduction{Inputs: InfraConditions, Output: InfraConditions.NonDegraded, Classifier: InfraClassification, BadSuffix: "Degraded", BadDescription: "degraded children"},
	Messages:  infraMessages, Generation: PreserveBaseGeneration,
}
var InfraUpToDate = Aggregation{
	Output: InfraConditions.UpToDate,
	Parent: UpdateBase{LocalBaseFirst: LocalBaseFirst{Base: InfraConditions.UpToDate, Classifier: InfraClassification}, Initialized: InfraConditions.Initialized},
	Local:  PrefixedChecks{Prefix: "UpToDate", Polarity: TrueIsGood, Classifier: InfraClassification},
	Children: ServingChildren{Inputs: InfraConditions, Condition: InfraConditions.UpToDate, Classifier: InfraClassification,
		False: Always{}, Failure: Never{}, Human: Always{}, SyncError: Always{}, Uninitialized: Always{}, JobRunning: Always{}, JobFailed: Always{}, BadDescription: "is updating"},
	Reduction: ParentChildReduction{Inputs: InfraConditions, Output: InfraConditions.UpToDate, Classifier: InfraClassification, BadSuffix: "Updating", BadDescription: "updating children"},
	Messages:  infraMessages, Generation: PreserveBaseGeneration,
}
var InfraSynced = Aggregation{
	Output:    InfraConditions.Synced,
	Parent:    LocalOnly{LocalBaseFirst{Base: InfraConditions.Synced, Classifier: InfraClassification}},
	Local:     PrefixedChecks{Prefix: "Synced", Polarity: TrueIsGood, Classifier: InfraClassification},
	Reduction: ParentChildReduction{Inputs: InfraConditions, Output: InfraConditions.Synced, Classifier: InfraClassification, LocalOnly: true},
	Messages:  infraMessages, Generation: PreserveBaseGeneration,
}
var InfraDeleting = Aggregation{
	DefaultBase: &metav1.Condition{Type: "Deleting", Status: metav1.ConditionTrue, Reason: "Deleting", Message: "resource is being deleted"},
	Output:      InfraConditions.Deleting,
	Parent:      DeletionBase{Classifier: InfraClassification, Output: InfraConditions.Deleting},
	Children:    DeletingChildren{Inputs: InfraConditions, Classifier: InfraClassification},
	Reduction:   DeletionReduction{Inputs: InfraConditions, Classifier: InfraClassification},
	Messages:    infraMessages, Generation: PreserveBaseGeneration,
}

// InfraRules expresses the Infra behavior as data consumed by the generic Aggregate function.
var InfraRules = Rules{
	Outputs:       []Aggregation{InfraAvailability, InfraNonDegraded, InfraUpToDate, InfraSynced, InfraDeleting},
	LocalFirst:    true,
	UnmappedLocal: InfraLocalConditions{},
	IgnoreLocal:   []string{InfraConditions.Initialized.Type},
	Now:           time.Now,
}

// InfraProfile optionally binds rules for callers that prefer a short application-specific facade.
type InfraProfile struct{ Rules Rules }

// Infra injects InfraRules; the facade has no evaluation or sequencing implementation of its own.
var Infra = InfraProfile{Rules: InfraRules}

// InfraSummary is a presentation implementation; it neither selects children nor mutates status.
type InfraSummary struct {
	Inputs     ServingInputs
	Classifier Classifier
	Messages   Formatter
}

func (p InfraSummary) Present(parent Object) (string, string) {
	v := p.Inputs
	if len(parent.GetConditions()) == 0 {
		return "Initializing", ""
	}
	selected := func(check ConditionType, ordinary string) (string, string) {
		c := check.Read(parent)
		phase := ordinary
		if classify(p.Classifier, c) == WaitingForHuman {
			phase = c.Reason
		}
		return phase, p.Messages.Inline(c)
	}
	if v.Synced.IsBad(parent) {
		return selected(v.Synced, "Error")
	}
	if v.Deleting.IsBad(parent) {
		return selected(v.Deleting, "Deleting")
	}
	if isDeleting(parent) {
		return "Deleting", ""
	}
	available := v.Available.Read(parent)
	if v.Available.Assessment(available) == Bad && classify(p.Classifier, available) == Failed {
		return "Failed", p.Messages.Inline(available)
	}
	if v.Initialized.IsBad(parent) {
		if v.Available.Assessment(available) == Bad {
			return "Initializing", p.Messages.Inline(available)
		}
		return "Initializing", p.Messages.Inline(v.Initialized.Read(parent))
	}
	if v.Available.Assessment(available) == Bad {
		return selected(v.Available, "Unavailable")
	}
	if v.NonDegraded.IsBad(parent) {
		if classify(p.Classifier, v.NonDegraded.Read(parent)) == WaitingForHuman {
			return selected(v.NonDegraded, "Degraded")
		}
		if v.UpToDate.IsBad(parent) {
			return "Degraded and updating", p.Messages.Inline(v.UpToDate.Read(parent))
		}
		return "Degraded", p.Messages.Inline(v.NonDegraded.Read(parent))
	}
	if v.UpToDate.IsBad(parent) {
		return selected(v.UpToDate, "Updating")
	}
	return "Ready", ""
}

// DeriveInitialized is a local Go helper consuming supplied current Conditions and a persisted latch.
func DeriveInitialized(parent Object, now time.Time) metav1.Condition {
	v := InfraConditions
	if v.Initialized.IsGood(parent) {
		return v.Initialized.Read(parent)
	}
	reason, message, status := "NotYetAvailable", "waiting for resource to be available", metav1.ConditionFalse
	if isDeleting(parent) {
		reason, message = "Deleting", "resource is being deleted"
	} else if v.Available.IsGood(parent) {
		reason, message, status = "Initialized", "", metav1.ConditionTrue
	}
	return stamp(parent, metav1.Condition{Type: v.Initialized.Type, Status: status, Reason: reason, Message: message}, now)
}

// InfraInitialization derives the explicit Infra initialized latch after availability evaluation.
type InfraInitialization struct{}

func (InfraInitialization) Derive(parent Object, now time.Time) []metav1.Condition {
	return []metav1.Condition{DeriveInitialized(parent, now)}
}

// InfraLocalConditions preserves local-only prefix aggregation for additional Infra Condition types.
type InfraLocalConditions struct{}

func (InfraLocalConditions) Evaluate(parent Object, local metav1.Condition, now time.Time) (*metav1.Condition, error) {
	output := ConditionType{local.Type, TrueIsGood}
	rule := Aggregation{
		Output:     output,
		Parent:     LocalOnly{LocalBaseFirst{Base: output, Classifier: InfraClassification}},
		Local:      PrefixedChecks{Prefix: local.Type, Polarity: TrueIsGood, Classifier: InfraClassification},
		Reduction:  ParentChildReduction{Output: output, Classifier: InfraClassification, LocalOnly: true},
		Messages:   infraMessages,
		Generation: PreserveBaseGeneration,
	}
	return rule.Evaluate(parent, nil, local, now)
}

// Aggregate only injects the facade's rules into the generic resource-level operation.
func (p InfraProfile) Aggregate(parent Conditioned, children map[string]Object, local ...metav1.Condition) (bool, error) {
	return Aggregate(parent, children, p.Rules, local...)
}

// Evaluate only injects the facade's rules into generic pure evaluation.
func (p InfraProfile) Evaluate(parent Object, children map[string]Object, local ...metav1.Condition) ([]metav1.Condition, error) {
	return Evaluate(parent, children, p.Rules, local...)
}

// classify supplies the natural no-escalation default for a missing classifier.
func classify(c Classifier, value metav1.Condition) Escalation {
	if c == nil {
		return None
	}
	return c.Classify(value)
}
func badOutcome(reason, message string) Outcome {
	return Outcome{State: State{Bad, None}, Reason: reason, Summary: message}
}
func copyOutcome(c metav1.Condition, check ConditionType, classifier Classifier) Outcome {
	assessment := check.Assessment(c)
	escalation := None
	if assessment == Bad {
		escalation = classify(classifier, c)
	}
	return Outcome{State: State{assessment, escalation}, Reason: c.Reason, Summary: c.Message, Copied: &c}
}

// namedObject retains the caller's diagnostic alias beside the supplied resource.
type namedObject struct {
	Name   string
	Object Object
}

func sortedChildren(children map[string]Object) []namedObject {
	result := make([]namedObject, 0, len(children))
	for name, object := range children {
		if !isNilObject(object) {
			result = append(result, namedObject{name, object})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}
func cause(c Contribution) Cause {
	label := c.Reference
	if c.CheckName != "" {
		label = c.CheckName
	}
	if c.Detail != nil {
		return Cause{label, *c.Detail}
	}
	message, reason := strings.TrimSpace(c.Condition.Message), strings.TrimSpace(c.Condition.Reason)
	switch {
	case message == "" && reason == "":
		message = c.Fallback
	case message == "":
		message = reason
	case reason != "" && !strings.Contains(message, reason):
		message += " (" + reason + ")"
	}
	return Cause{label, message}
}
func limitNames(names []string) string {
	if len(names) <= 3 {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s, and %d more", strings.Join(names[:3], ", "), len(names)-3)
}
func limitedNames(group []Contribution, local bool) string {
	names := make([]string, 0, len(group))
	for _, c := range group {
		name := c.Reference
		if local {
			name = c.CheckName
		}
		names = append(names, name)
	}
	return limitNames(names)
}
func groupHead(prefix string, group []Contribution) string {
	if len(group) == 0 {
		return ""
	}
	if len(group) == 1 {
		return prefix
	}
	return prefix + ": " + limitedNames(group, false)
}
func localGroupHead(prefix string, group []Contribution) string {
	if len(group) == 0 {
		return ""
	}
	if len(group) == 1 {
		return prefix
	}
	return prefix + ": " + limitedNames(group, true)
}
func joinHeads(heads ...string) string {
	var nonempty []string
	for _, s := range heads {
		if s = strings.TrimSpace(s); s != "" {
			nonempty = append(nonempty, s)
		}
	}
	return strings.Join(nonempty, ", ")
}
func childReason(children map[string]Object, group []Contribution, suffix string) string {
	if len(children) == 1 && len(group) == 1 {
		return group[0].Reference + suffix
	}
	return "Children" + suffix
}

func stamp(parent Object, c metav1.Condition, now time.Time) metav1.Condition {
	c.LastTransitionTime = metav1.NewTime(now)
	if old, ok := Lookup(parent, c.Type); ok && old.Status == c.Status {
		c.LastTransitionTime = old.LastTransitionTime
	}
	return c
}

// deleting reads already-supplied object metadata without querying Kubernetes.
func isDeleting(object Object) bool {
	timestamp := object.GetDeletionTimestamp()
	return timestamp != nil && !timestamp.IsZero()
}

// isNilObject handles optional resources, including typed nil pointers in interface values.
func isNilObject(object Object) bool {
	if object == nil {
		return true
	}
	value := reflect.ValueOf(object)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
