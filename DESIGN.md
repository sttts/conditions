<!--
SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
SPDX-License-Identifier: Apache-2.0
-->

# Condition aggregation and structured diagnostic promotion

Status: draft design for [`github.com/sttts/conditions`](https://github.com/sttts/conditions), accompanied by an experimental root package and executable profiles. The public API and native diagnostic format are under review.

## Summary

Build a reusable Go library for aggregating Kubernetes Conditions and promoting structured diagnostics. Applications configure flat `Rules` containing output aggregations, local and child mappings, polarities, optional validity rules, escalation, propagation, and presentation. The generic call is `Aggregate(parent, children, rules, localConditions...)`; application helpers may bind the rule data. The caller supplies the relevant current observations; the library derives Conditions and bounded diagnostic messages without discovering resources or persisting status.

Separate three concerns: whether an observation is good or bad, how strongly a bad observation escalates, and how its causes are presented. Support both native messages, whose documented structure survives repeated promotion, and foreign messages, which remain opaque text. The configuration model must be able to express the reference Infrastructure conditions implementation as a profile, rather than embedding that implementation's condition names and lifecycle assumptions in the aggregation engine.

**Non-goal:** Define how Conditions come to be, beyond deriving them from already-computed local Conditions and those of children.

The mechanism starts with those Conditions and their metadata. Resource adapters can translate Condition representations. Establishing the assertions, resource discovery, and controller package refactoring are outside this proposal.

## Motivation

Kubernetes controllers routinely combine local assertions and Conditions from dependent resources into a parent's status. These resources can use different Condition types, polarities, reason conventions, and generation metadata. Hand-written readiness checks often discard useful reasons and messages, while ad-hoc string concatenation obscures which failure dominates and allows diagnostics to grow without a consistent budget.

A shared library should make that interpretation explicit. Assessment, escalation, validity, and diagnostic rendering are separate concerns. Profiles configure mappings and priority; callers supply already-observed objects and keep resource selection, reconciliation, and API-server writes.

The Infrastructure profile below is the worked expressibility example. It includes initialization-aware availability, human-action waits, local precedence, job-like sources, deletion, and summary ordering. Additional OpenShift operator and Crossplane profiles exercise different native representations and aggregation semantics; an unrelated rule-set fixture also uses different names and negative output polarity. Dynamo is a potential consuming integration, alongside other controllers.

## Project and package conventions

The initial module path is `github.com/sttts/conditions`, with public package `conditions` at the module root. Keep exported APIs independent of repository ownership and application names. The package follows established Kubernetes API and Go conventions; repository-wide contribution files should use the [Kubernetes template project](https://github.com/kubernetes/kubernetes-template-project) as their baseline while preserving existing attribution and license notices.

Use `metav1.Condition` and its standard fields directly. Adopt the structural `GetConditions() []metav1.Condition` and `SetConditions([]metav1.Condition)` interfaces used by [Cluster API's Getter](https://github.com/kubernetes-sigs/cluster-api/blob/main/util/conditions/getter.go) and [Setter](https://github.com/kubernetes-sigs/cluster-api/blob/main/util/conditions/setter.go). The setter replaces the supplied slice; it is not responsible for merging, transition timestamps, change detection, or persistence. The library computes the returned `changed` flag. Existing variadic or boolean-returning setters use an adapter in their integration package.

Follow [Kubernetes Condition conventions](https://github.com/kubernetes/community/blob/main/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties) and the behavior of [API machinery's Condition helpers](https://github.com/kubernetes/apimachinery/blob/master/pkg/api/meta/conditions.go): Condition type identifies an entry, status is `True`, `False`, or `Unknown`, and `lastTransitionTime` changes with status. Preserve reasons and observed-generation contracts explicitly. Canonicalize timestamps to their API serialization precision before change comparison. Omission and staleness remain distinct inputs to profile policy; an absent Condition must not silently assert success.

The intended production layout is small:

```text
conditions/
  go.mod                 # module github.com/sttts/conditions
  types.go               # public accessors and normalized result types
  rules.go               # flat rules and validation
  aggregate.go           # generic Aggregate and Evaluate
  messages.go            # native/foreign diagnostics and bounded rendering
  adapters.go            # representation adapters built on API machinery
  internal/              # implementation details, without application policy
  examples/infra/        # reference application profile and conformance tests
  examples/openshift/    # operator-status unions and native OpenShift adapters
  examples/crossplane/   # native Crossplane Conditions and readiness policy
  testdata/              # public, self-contained conformance fixtures
  DESIGN.md
```

This is the target production layout. The experimental evaluator currently lives in `conditions.go`; all three examples are separate modules consuming that same root package. Infra retains its reference conformance tests. Infra vocabulary and compatibility behavior stay in examples or consuming applications. Framework-specific adapters remain optional, and neither clients nor application API modules become dependencies of basic aggregation.

Use ordinary Go interfaces for real Conditions concepts, Go values for configuration, and Go implementations for exceptional behavior. Document exported symbols and provide external-package examples and tests that use only the public API. Keep development versions pre-v1 until semantics, metadata behavior, and the native format are settled; publish compatibility expectations before v1. Build and test with the supported Go/API-machinery versions, formatting, static checks, and race tests where appropriate. The existing repository Apache-2.0 license applies; retained source notices remain intact.

## Requirements

1. Rules are Go data values composing output aggregations and implementations of domain interfaces. Standard implementations expose natural configuration for common cases; custom behavior is ordinary Go behind the same contracts. There are no aggregation generators, resource-instance selectors, ownership traversal, or embedded policy language. The caller chooses the aggregation for each invocation.
2. Both input and output conditions declare their polarity.
3. Ordinary bad state, `WaitingForHuman`, and `Failed` remain distinct. Compatible aggregation configurations can preserve failure escalation across multiple aggregation calls through explicit output reasons and input classifiers.
4. Unavailability takes precedence over ordinary degradation when constructing an overall summary. Propagation and priority remain configurable per condition and profile.
5. Escalation does not imply a terminal lifecycle or a sticky parent failure. Evaluation derives desired Conditions from the supplied Conditions and explicitly supplied prior state.
6. Native and foreign messages are selected through separate constructors, not inferred from arbitrary message text.
7. Diagnostic messages retain nested causes, have useful first lines, and obey line, size, and work limits.
8. Evaluation depends on the supplied aggregation, Conditions, metadata, prior Conditions, and time. Equivalent inputs produce equivalent results; intentionally changing the aggregation may change the interpretation.
9. Aggregation supports large input sets without building unbounded diagnostic messages or repeatedly traversing a complete message tree merely to produce a short summary.
10. The documented Infra fixture corpus is the canonical expressibility test. Its behavior must compose through domain interfaces and their implementations, without Infrastructure-specific branches in the aggregation core. Go implementations are permitted and expected; translating every control-flow branch into data is not a requirement.
11. Callers retain resource selection, reconciliation order, lifecycle policy, and status persistence ownership.
12. Condition evaluation and message rendering depend on ordinary values and Kubernetes API machinery. Application profiles, resource APIs, and controller integrations remain outside the evaluation package.
13. Static aggregations can optionally declare that a condition controls the validity of other condition inputs and supplies a replacement contribution when they are invalid. No resource must publish a sync condition, and no condition name has implicit invalidation semantics.

## Proposal

### Boundaries and data flow

The data flow is:

```text
Current observations selected by the caller
  -> resource adapters and statically selected condition aggregation
  -> normalized contributions
  -> optional validity rules and replacement contributions
  -> propagation rules and ordered reduction
  -> output conditions and structured causes
  -> bounded native message rendering
  -> caller-owned desired status
  -> existing controller status persistence
```

One evaluation combines already-supplied local and immediate-child Conditions into parent Conditions. Repeating that operation at another resource boundary creates the hierarchy implicitly. The configuration contains no resource nodes, edges, or ownership traversal, and the library does not locate children.

The proposed package boundaries are:

| Boundary | Responsibility |
| --- | --- |
| `conditions` library | Pure evaluation, optional in-memory Condition application, normalization, escalation, diagnostic trees, and message codecs. No application resource APIs, Kubernetes clients, or API-server writes. |
| Application profiles and resource adapters | Define static aggregations, source mappings, reason classifiers, lifecycle gates, summary ordering, and output reasons. Supply scoped observations and translate resource-specific status into declared input semantics. |
| Existing reconcilers | Retain lifecycle decisions, reconciliation sequencing, status persistence, and event emission. |

The condition library must not import controller packages, own reconciliation callbacks, or require exporting a controller's private reconciliation contract. This design does not prescribe additional domain-specific helper packages.

### Library API and dependency boundary

Keep condition evaluation and message formatting in a cohesive `conditions` package with a small exported API. Profiles and adapters use this API to supply observations and interpret results; they do not reach into evaluator or codec internals. This keeps resource-specific policy separate from the shared mechanism and makes both testable in isolation.

The core's dependency budget is the Go standard library and `k8s.io/apimachinery`, including `metav1.Condition` and `schema.GroupVersionKind`. It must not import application APIs, private integration implementations, `controller-runtime`, Kubernetes clients, or workload-specific API packages. A caller can use typed resource APIs when declaring its mappings; those imports belong to its integration package. Optional framework integrations, if needed later, remain separate from the core and cannot become dependencies of basic evaluation or formatting.

Inputs are already-computed local and child Conditions, resource identity and generation metadata, and previous persisted Conditions when a profile uses them. Public interfaces must not require an object to implement `client.Object`, embed a reconciler interface, register application types in a Kubernetes scheme, or expose application-specific resource methods. Pure evaluation returns desired Conditions. Generic `Aggregate` applies them to the supplied parent through a setter and reports changes; callers retain representation adapters and status persistence. Time is an explicit dependency: rule sets configure their clock, sample it once per call, and pass that value to single-output evaluation. Tests inject a fixed clock.

The core has no built-in DCD, DGD, DGDR, or Infrastructure aggregation. Condition names such as `Available`, `NonDegraded`, `Initialized`, and `Synced`, reason conventions, and lifecycle-specific precedence live in profiles. The library supplies the assessment, validity, escalation, propagation, and rendering primitives with which those profiles are expressed. Default escalation and rendering policies may be provided, but an application's condition vocabulary and lifecycle cannot be prerequisites for using them.

Document the native message grammar, its format evolution, and fallback rules before publishing the format. Native decoding is used to retain supported diagnostic structure. Semantic classification comes from the aggregation and Condition fields and does not depend on the message codec.

### Assessment, escalation, and polarity

The following experimental public types keep assessment, escalation, and polarity independent:

```go
// Polarity declares which known boolean status represents a good observation.
type Polarity uint8

const (
    TrueIsGood Polarity = iota
    FalseIsGood
)

// Assessment represents normalized goodness independently of escalation.
type Assessment uint8

const (
    Good Assessment = iota
    Bad
    Unknown
)

// Escalation classifies a bad result without imposing a terminal lifecycle.
type Escalation uint8

const (
    None Escalation = iota
    WaitingForHuman
    Failed
)
```

Assessment and escalation are separate dimensions. The numeric values above do not define priority; a profile defines the order explicitly.

Normalize each source condition using its own polarity:

| Source status | `TrueIsGood` | `FalseIsGood` |
| --- | --- | --- |
| `True` | `Good` | `Bad` |
| `False` | `Bad` | `Good` |
| `Unknown` | `Unknown` | `Unknown` |

Classify an accepted bad observation as ordinary, waiting for human action, or failed. A condition type or boolean alone does not imply escalation. For example, ordinary `Degraded=True` may be soft, while a configured reason such as `DependencyFailed` may be classified as `Failed`.

After aggregation, encode the result using the output condition's polarity:

| Aggregated assessment | Output `TrueIsGood` | Output `FalseIsGood` |
| --- | --- | --- |
| `Good` | `True` | `False` |
| `Bad`, including escalated contributions | `False` | `True` |
| `Unknown` | `Unknown` | `Unknown` |

The same failed contribution can therefore produce `Available=False` or `Degraded=True`. Escalation must never be detected by assuming that every failed condition has status `False`.

### Public API

The generic entry point itself has the Infra call shape with one additional argument: `Aggregate(parent, children, rules, localConditions...)`. It accepts existing resource objects, a map of already-selected children, and flat rule data. Application-specific helpers may bind `rules`; they do not own a separate evaluator. Callers never copy Conditions or metadata into an input bundle.

`Rules.Outputs` is an ordered flat list of output definitions for one boundary. `LocalFirst` optionally gives local-only and bad/unknown local bases precedence before deferred child aggregation. Defaults and local derivations are attached directly to their output rules. These are domain configuration values; exceptional derivation and local-condition behavior use the declared Go interfaces. The evaluator does not infer resource edges or schedule an ownership graph.

`Getter` uses the common `GetConditions() []metav1.Condition` convention. `Object` adds only standard Kubernetes metadata getters, without `client.Object` or scheme registration. `Setter` matches the slice-based Kubernetes ecosystem accessor; `Conditioned` combines it with object metadata. The library computes the change flag independently of the setter. Other representations use an accessor adapter.

| Concept | Contract | Implementations exercised below |
| --- | --- | --- |
| Resource-level aggregation | Apply supplied flat rules and report changes, or return a pure desired result. | Generic `Aggregate` and `Evaluate`; the Infra wrapper only binds `InfraRules`. |
| Condition access | Read existing objects and optionally apply desired Conditions in memory. | `Getter`, `Object`, `Conditioned`, and caller-bound adapters. |
| Classification | Escalation of an already-bad observation. | `ReasonClassifier` with exact mappings, ordered suffixes, and a default. |
| Propagation | Eligibility of a diagnosis for an output. | `Always`, `Never`, `BeforeInitialized`, `ExistingChildren`. |
| Selection | Local checks and zero or more diagnoses per supplied child. | `PrefixedChecks`, Infra’s single-diagnosis selectors, OpenShift’s multi-assertion suffix selector. |
| Local precedence and reduction | Preserve selected contributions and their origin. | `LocalBaseFirst`, `LocalOnly`, `ChildrenOnly`, and profile-specific reducers. |
| Local derivation | Derive additional Conditions from computed Conditions and metadata. | `InfraInitialization`; ordinary Go behind `Derivation`. |
| Unmapped local Conditions | Interpret additional supplied local Conditions when configured. | `InfraLocalConditions`; otherwise preserve the supplied local Condition. |
| Presentation | Format retained causes and select a parent summary. | `BoundedMessages`, `InfraSummary`. |

API declarations have one-line comments. Configured rule data, concrete calls, and implementation bodies appear in the separate example section.

```go
// State is the normalized semantic result before encoding output polarity.
type State struct {
	Assessment Assessment
	Escalation Escalation
}

// Getter is the conventional read-only accessor implemented by condition-bearing resources.
type Getter interface {
	GetConditions() []metav1.Condition
}

// Object combines condition access with standard Kubernetes metadata getters.
type Object interface {
	Getter
	GetGeneration() int64
	GetCreationTimestamp() metav1.Time
	GetDeletionTimestamp() *metav1.Time
}

// Setter is the slice-based Conditions setter used by Kubernetes ecosystem APIs.
type Setter interface {
	Getter
	SetConditions([]metav1.Condition)
}

// Conditioned combines object access and in-memory assignment without requiring a Kubernetes client.
type Conditioned interface {
	Object
	Setter
}

// ConditionType names a Condition and explicitly declares its source or output polarity.
type ConditionType struct {
	Type     string
	Polarity Polarity
}

// Rules is flat configuration for one resource boundary, in the caller's declared output order.
type Rules struct {
	Outputs       []Aggregation
	LocalFirst    bool
	UnmappedLocal LocalEvaluator
	IgnoreLocal   []string
	Now           func() time.Time
}

// Derivation derives additional local Conditions from already-computed Conditions and metadata.
type Derivation interface {
	Derive(parent Object, now time.Time) []metav1.Condition
}

// LocalEvaluator aggregates supplied local Conditions that have no configured output rule.
type LocalEvaluator interface {
	Evaluate(parent Object, local metav1.Condition, now time.Time) (*metav1.Condition, error)
}

// Classifier applies only after a source has been established as bad.
type Classifier interface {
	Classify(metav1.Condition) Escalation
}

// Propagation decides whether a particular child diagnosis participates in an output.
type Propagation interface {
	Allows(parent, child Object) bool
}

// ChildSelector selects the relevant contributions from one supplied child.
type ChildSelector interface {
	Select(parent, child Object) []Contribution
}

// LocalSelector selects the parent's independently supplied local checks.
type LocalSelector interface{ Select(Object) []Contribution }

// ParentPolicy handles local precedence and decides whether children may contribute.
type ParentPolicy interface {
	Before(parent Object, children map[string]Object, base metav1.Condition) (*Outcome, bool)
	IncludeChildren(base metav1.Condition) bool
}

// Reducer combines selected contributions, retaining their origin and diagnostics.
type Reducer interface {
	Reduce(base metav1.Condition, children map[string]Object, local, selected []Contribution) Outcome
}

// Formatter renders diagnostics separately from semantic selection and reduction.
type Formatter interface {
	Format(string, []Cause) string
	Inline(metav1.Condition) string
}

// Presenter selects a human summary from already-computed parent Conditions.
type Presenter interface {
	Present(Object) (phase, message string)
}

// Cause retains source text and its diagnostic provenance.
type Cause struct {
	Reference string
	Message   string
}

// Contribution retains a selected observation and its profile-specific diagnostic annotations.
type Contribution struct {
	State          State
	Condition      metav1.Condition
	Reference      string
	Source         Object
	CheckName      string
	Fallback       string
	Detail         *string
	Uninitialized  bool
	FromUnknown    bool
	SuppressDetail bool
}

// Outcome combines a normalized result with its retained diagnostic causes.
type Outcome struct {
	State              State
	Reason             string
	Summary            string
	Causes             []Cause
	Copied             *metav1.Condition
	LastTransitionTime metav1.Time
}

// GenerationPolicy declares which observed-generation representation an output supports.
type GenerationPolicy uint8

const (
	ParentGeneration GenerationPolicy = iota
	PreserveBaseGeneration
	NoObservedGeneration
)

// Aggregation is a flat value composing implementations of Conditions concepts.
type Aggregation struct {
	DefaultBase *metav1.Condition
	Derived     []Derivation
	Output      ConditionType
	Parent      ParentPolicy
	Local       LocalSelector
	Children    ChildSelector
	Reduction   Reducer
	Messages    Formatter
	Generation  GenerationPolicy
}

// ReasonSuffix assigns escalation to a matching reason suffix.
type ReasonSuffix struct {
	Suffix     string
	Escalation Escalation
}

// ReasonClassifier matches exact reasons, ordered suffixes, and a default in normal Go.
type ReasonClassifier struct {
	Reasons  map[string]Escalation
	Suffixes []ReasonSuffix
	Default  Escalation
}

// Aggregate applies supplied rules to existing objects and reports in-memory Condition changes.
func Aggregate(parent Conditioned, children map[string]Object, rules Rules, local ...metav1.Condition) (changed bool, err error)

// Evaluate returns desired Conditions through the same generic call without modifying objects.
func Evaluate(parent Object, children map[string]Object, rules Rules, local ...metav1.Condition) ([]metav1.Condition, error)

// Lookup distinguishes missing Conditions through the ordinary GetConditions accessor.
func Lookup(object Getter, typ string) (metav1.Condition, bool)

// Assessment normalizes one input using the configured source polarity.
func (c ConditionType) Assessment(value metav1.Condition) Assessment

// Evaluate derives one output using the supplied resource objects and explicit time.
func (a Aggregation) Evaluate(parent Object, children map[string]Object, base metav1.Condition, now time.Time) (*metav1.Condition, error)
```

The generic `Aggregate` stages all evaluations, compares desired and current Conditions itself, and calls the slice-based setter only when they differ. Invalid configuration returns an error without partially changing that parent. `Evaluate` returns the desired Conditions without requiring a setter. The original Infra function returns only `changed`; this proposed API also returns configuration errors. Neither path performs API-server writes.

A child's map key supplies a diagnostic alias and a compatible reason prefix; it never selects policy. Nil children, including typed nil pointers, are skipped as inputs. Original map cardinality can still participate in an explicitly compatible reason implementation.

Compatibility annotations in `Contribution` remain prototype choices. `GenerationPolicy` explicitly distinguishes parent generation, Infra-compatible copied/derived generations, and representations with no per-condition generation. General validity/freshness, native/foreign constructors, and the native codec still require their separate contracts. None has implicit Infra Condition names.

### Design guidelines

- Introduce an interface for a real Conditions concept with a meaningful contract, not for every helper or scalar value.
- Provide natural standard implementations whose fields describe that concept directly. A common aggregation should be readable from those configured values.
- Keep the aggregation itself flat data: inputs, outputs, polarities, and concrete policy values. Referencing Go implementations through interfaces does not turn that configuration into a generator.
- Implement exceptional behavior in ordinary Go behind the appropriate interface. Go is the extension language; the library does not supply a second programming language.
- Do not introduce generic expression trees, indirect value-addressing languages, action opcodes, or if/then interpreters. This exclusion applies even to a language that is technically bounded rather than Turing-complete.
- Keep policy implementations deterministic and limited to supplied Conditions, metadata, and explicit time. Clients, discovery, reconciliation, and status persistence stay with the caller.
- Test standard and custom implementations against the same concept contracts. The Infrastructure profile must exercise those concepts individually; a monolithic wrapper around its old `Aggregate` function is not a satisfactory design proof.

The executable example below is an expressibility proof and a reviewable API model. The production package and remaining contracts are still implementation work. An embedded policy language is outside this design.

### Propagation and ordering

Propagation answers whether a contribution is relevant to a particular output. Ordering answers which relevant contribution dominates. They must be separate operations.

A useful configured negative-contribution order is:

```text
Failed > WaitingForHuman > ordinary Bad
```

The profile separately positions `Unknown` relative to known bad and good observations. It also controls local-condition precedence, deletion behavior, initialization gates, and which child condition types contribute. There is no universal worst-value function that overrides these policies.

Overall presentation is a separate reduction over the resulting conditions. A service-oriented summary should distinguish unavailable service from a service that is available but degraded:

```text
ordinary Unavailable > ordinary Degraded
```

These refer to normalized semantics. In the reference positive-polarity profile this is `Available=False` taking precedence over `NonDegraded=False`. In a negative-polarity profile the latter may be represented by `Degraded=True`.

`WaitingForHuman` must fit both layers. A pending approval can make desired state not up to date while an existing service remains available. It must not automatically turn every availability or degradation condition bad. A failed branch still dominates an unrelated waiting-for-human branch:

| Branch A | Branch B | Dominating contribution |
| --- | --- | --- |
| `Bad/Failed` | `Bad/WaitingForHuman` | `Failed` |
| `Bad/Failed` | `Good/None` | `Failed` |
| `Good/None` | `Bad/WaitingForHuman` | `WaitingForHuman`, where propagation permits it |

Resolving branch B does not repair branch A. If branch A recovers, its failed contribution disappears on the next evaluation. The aggregator does not create an additional failure latch.

### Failure escalation is not terminality

`Failed` is an escalation category, not an instruction to stop reconciliation or permanently freeze the parent.

| Current observation | Assessment and escalation |
| --- | --- |
| A recoverable dependency is not ready | `Bad/None` |
| An authoritative source classifies its current failure as escalated | `Bad/Failed` |
| Healthy Conditions are supplied instead | `Good/None` |
| A Job reports terminal failure | `Bad/Failed`; terminality belongs to the Job lifecycle |

A source can be terminal for one attempt, require explicit human recovery, or recover automatically. The corresponding controller owns that behavior. The aggregation sees the current accepted observation and retains no independent historical severity.

The example's recovery is represented by replacing supplied Conditions with healthy Conditions. How the source establishes that replacement is outside the mechanism.

### Compose compatible aggregation configurations

`metav1.Condition` has no severity field. Each aggregation explicitly classifies its supplied Condition fields. To preserve escalation across calls, configure the first aggregation's output reason and the second aggregation's input classifier compatibly.

For example, aggregation A can classify `DependencyFailed` as `Failed` and publish `Ready=False, Reason=ChildrenFailed`. Aggregation B can explicitly classify `ChildrenFailed` as `Failed`. Passing A's returned Conditions directly into B is sufficient. No serialization, native message parsing, or additional cross-call protocol is required for this semantic composition.

Preservation is a property of those two compatible configurations, not a guarantee for arbitrary aggregations. A different aggregation may intentionally interpret `ChildrenFailed` as ordinary `Bad` or choose not to propagate it. The reference profile's reason-suffix conventions are one configurable policy, not a universal requirement.

No consumer derives severity from message wording, indentation, punctuation, or truncation markers. If a reason's escalation is unspecified, the profile's explicit fallback applies; the engine does not guess from English words.

### Native and foreign diagnostic messages

Internally, preserve a diagnostic tree until rendering. Native nodes carry a summary, ordered child causes, and omission counts. Foreign nodes carry an opaque text block. A foreign block remains foreign even after it is included in a native parent's message and that parent is later promoted again.

The constructor in the supplied aggregation chooses the decoder. Only a `Native` source is decoded as the native message format. `Foreign` text is never interpreted as structure even if it contains colons, indentation, URLs, stack traces, or strings resembling condition reasons. A malformed or legacy native message can be preserved as opaque text without affecting condition classification.

The codec must make foreign blocks unambiguous across serialization. A proposed v1 representation uses a literal-block marker for an opaque child, with escaping for reserved header forms:

```text
Resource is not ready: 1 failed dependency:
  Dependency/worker: Initialization failed:
    Job/initialize: |
      Initialization failed:
        Unable to retrieve the required artifact
        Check the configured location
```

The codec treats the complete literal block as text and never turns its apparent inner headings into native children. Exact escaping and legacy-decoding fixtures must be settled before the native format is published. The reference implementation's generic colon/indentation parsing must not be applied to arbitrary third-party messages.

Adopt the following behavior from the referenced multiline design:

1. The first line is useful on its own. It summarizes the result and the number or classes of affected inputs.
2. Use two spaces per native nesting level and one separate line per displayed child in an expanded details block.
3. The default total line budget is seven, including the summary. A caller can select another fixed budget.
4. Condense deepest native branches first. A single-child branch can become an inline chain. A branch with multiple children keeps an adequate standalone summary instead of concatenating all leaf detail.
5. Shorten an opaque multiline block by retaining its first line and explicitly marking omitted text. Do not infer a native subtree from that text.
6. When even the retained child headers do not fit, fall back to the summary and omission counts rather than hiding the dominant failure.
7. Provide a separate inline rendering for concise UI or log summaries; do not overwrite the stored detailed message merely to fit a printer column.

Normalize line endings and preserve meaningful text. Enforce both line and encoded-size limits. Truncation must be UTF-8 safe, stay within the Kubernetes condition-message limit, and explicitly indicate omission. A line budget alone does not bound a single exceptionally long child message.

### Published condition examples

These are illustrative status excerpts after native-message adoption. They use the existing `status.conditions` schema; no user-authored aggregation or new spec fields are proposed.

Positive output polarity:

```yaml
status:
  conditions:
    - type: Ready
      status: "False"
      observedGeneration: 12
      lastTransitionTime: "2026-10-27T09:00:00Z"
      reason: ChildrenFailed
      message: |-
        Resource is not ready: 1 failed dependency, 1 updating:
          Dependency/worker: Initialization failed:
            Job/initialize: Required initialization failed
          ExternalDependency/storage: |
            Waiting for capacity
```

Negative output polarity, using the same assessment and escalation:

```yaml
status:
  conditions:
    - type: Degraded
      status: "True"
      observedGeneration: 12
      lastTransitionTime: "2026-10-27T09:00:00Z"
      reason: ChildrenFailed
      message: |-
        Resource is degraded: 1 failed dependency:
          Dependency/worker: Initialization failed:
            Job/initialize: Required initialization failed
```

The resource labels above are illustrative display identities, not aggregation configuration or proposed new resource kinds. The `Degraded` condition demonstrates encoding; adding it to an API would require a separate, explicit migration decision. It is not implicitly added by using this library.

It is invalid for a positive `Ready` output to encode a normalized bad result as `status: "True"`, or for a negative `Degraded` output to encode that same result as `status: "False"`. Existing API field validation cannot detect such semantic mismatches; aggregation validation and polarity tests must cover them.

### Freshness, ownership, and persistence

Freshness policies interpret the supplied Conditions and generation metadata. Some APIs provide per-condition `observedGeneration`, while others only provide status-level generation metadata or no such guarantee. Representation adapters preserve that distinction. They do not invent observed generations or establish new assertions. A profile must explicitly choose the freshness contract appropriate for its inputs.

By default the output Condition's `observedGeneration` is the parent's supplied generation, not a copied child generation. `PreserveBaseGeneration` explicitly preserves a copied base generation or uses zero for a derived Condition; `NoObservedGeneration` uses zero for representations such as OpenShift operator Conditions; it cannot silently become the default. A future configured freshness check must prevent a stale supplied Condition from being presented as a current diagnosis; the prototype does not yet supply a general freshness policy. Missing inputs follow the aggregation's declared missing policy.

Pure evaluation returns desired Conditions and diagnostics without mutating supplied objects. Generic `Aggregate` applies Conditions in memory through the parent’s setter. Neither path writes to the API server, emits Kubernetes Events, or advances the caller’s status-level observed generation. Existing reconciler error handling and persistence ownership remain authoritative.

`lastTransitionTime` changes when the condition status changes, not on every message or reason update. Any initialized/ever-ready latch required by a profile derives from explicitly supplied previous Conditions.

The reference implementation reads the parent's previous `Initialized` Condition while aggregating availability, then derives the current initialized latch. `InfraRules` declares that derivation on availability and explicitly orders the later output rules. The generic evaluator follows that data and passes current working Conditions to each rule; it infers no output dependencies or ownership traversal.

### Deterministic evaluation and bounded aggregation

The evaluator computes its result from the supplied aggregation, local and child Conditions, metadata, prior Conditions, and time. It keeps no hidden state between calls. This requirement concerns evaluation only; it makes no requirement about how callers obtain inputs or establish their assertions. Resource discovery is outside this design.

Message generation must support large fan-out. Count all relevant contributions, retain a bounded set of representative causes ordered by severity and stable identity, and format those causes within the configured budgets. Do not construct or repeatedly traverse a complete large text tree merely to emit seven lines. Document the complexity and measure evaluation, rendering, and allocation costs independently of any particular resource-observation mechanism.

### Infrastructure as the canonical reference

Infra is the canonical worked example for expressibility. Its call style passes the actual parent object and a named child map, followed by already-computed local Conditions. The generic API preserves this shape with the rules as one additional argument; an optional application wrapper binds those rules.

[The public fixture corpus](examples/infra/fixtures.json) contains 27 aggregation cases and 17 human-summary cases. The [shared evaluator](conditions.go) and [Infra profile](examples/infra/infra.go) and [checks](examples/infra/infra_test.go) exercise all 44 cases. The 27 aggregation cases run through both explicit single-output fixture steps and generic `Aggregate(..., InfraRules, ...)`, including repeated-call change reporting and comparison with the injection-only wrapper. Additional tests cover pure evaluation, error atomicity, optional children, resources without sync Conditions, both output polarities, two-call composition/recovery, and an unrelated rule set. The example also runs all 117 cases from the reference aggregation and multiline suites; see [source provenance and scope](examples/infra/reference_tests.json).

The adapted reference suites check 74 aggregation cases, 16 local-condition cases, 10 literal aggregate-message cases, the multiline examples A-F, and 11 inline cases. Aggregate cases also check the original change flag, metadata, and repeated-call idempotence. Reading or running these tests requires no private repository, client, or application API module. They do not constitute execution of the private package or prove the still-undecided new native format or scale.

The example retains compatibility details deliberately: `FailedChecks` does not end in `Failed`; an unknown child can enter the ordinary false branch; ignored nil map entries can affect singular/plural reason selection; and failed availability can outrank an ordinary sync error on the same child. Runtime aliases affect compatible diagnostics and reasons, not configured policy selection.

### Complete Infrastructure example

This section shows the configuration, the actual implementations behind its contracts, and the call. Each `Aggregation` is flat data for one parent output. `ServingInputs` declares the reference vocabulary; it is an application profile type, not vocabulary built into the aggregation engine. The generic implementations below live in the root package; Infra declarations live in its example module. [Type aliases](examples/infra/conditions_aliases.go) keep the profile and its reference tests readable without copying the evaluator. Forward references are ordinary Go declarations.

#### Profile values and polarities

`InfraRules` is a `Rules` data value for the generic evaluator. Its ordered `Outputs`, optional local precedence, defaults, derivations, and clock describe Infra behavior without a separate top-level evaluation implementation. `Infra` is only an optional wrapper binding those exact rules.

| Output | Failed child contribution | Human wait | Ordinary sync error | Output polarity |
| --- | --- | --- | --- | --- |
| `Available` | Before parent initialization, or from an initialized child. | Suppressed by the normal rule, with explicit sync-human exceptions. | Only before parent initialization. | `TrueIsGood`. |
| `NonDegraded` | Included; failed availability can supply the diagnosis. | Suppressed by the normal rule, with the same exceptions. | Included. | `TrueIsGood`. |
| `UpToDate` | Failure amplification disabled; ordinary bad state can propagate. | Included. | Included. | `TrueIsGood`. |
| `Synced` | Local checks only. | Local checks only. | Local checks only; child sync is not a parent sync assertion. | `TrueIsGood`. |
| `Deleting` | Deletion-specific teardown reduction. | Deletion-specific teardown precedence. | Deleting-child sync errors are included. | `FalseIsGood`. |

A nil propagation field disables that candidate category. `BeforeInitialized` and `ExistingChildren` consult explicitly configured Condition types and polarities. The rule set's `Now` dependency is sampled once per call; tests replace it with a fixed function. Single-output evaluation receives that time explicitly.

```go
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
var infraMessages Formatter = InfraMessages{MaxLines: 7, InlineChars: 80}

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

```

#### Invocation and resulting Conditions

The generic call receives the rules explicitly. `parent` and `worker` are existing objects; the local Conditions have already been computed:

```go
changed, err := Aggregate(parent,
    map[string]Object{"Worker": worker},
    InfraRules,
    synced, available, nonDegraded, upToDate,
)
```

An application convenience wrapper may bind `InfraRules`:

```go
changed, err := Infra.Aggregate(parent, children,
    synced, available, nonDegraded, upToDate,
)
```

That wrapper delegates directly to the generic function. `changed` reports in-memory Condition changes; the controller owns persistence. The pure counterpart has the same generic shape: `Evaluate(parent, children, rules, localConditions...)`.

The executable example below uses the generic function directly. Availability of the serving parent stays good while a new child fails initialization; degradation and update status report the child's current problems. Replacing those child Conditions lets the parent recover.

```go
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
```

`exampleResource` stands in for an existing typed API resource; its definition only makes the example executable:

<details>
<summary>Example API resource and its Conditions accessor</summary>

```go
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
```

</details>

The resulting parent Conditions include:

```yaml
status:
  conditions:
    - type: Available
      status: "True"
      reason: Available
    - type: Initialized
      status: "True"
      reason: Initialized
    - type: NonDegraded
      status: "False"
      reason: WorkerFailed
      message: |-
        children failed:
          Worker: Required dependency could not initialize (DependencyFailed)
    - type: UpToDate
      status: "False"
      reason: WorkerNotSynced
      message: |-
        children are not synced:
          Worker: Provider unavailable (ProviderError)
    - type: Synced
      status: "True"
      reason: Synced
```

The excerpt omits timestamp/generation fields. Negative outputs such as `Deleting` encode a normalized bad result as `status: "True"`.

#### Generic resource-level aggregation

`Aggregate` and `Evaluate` below are the root-package implementation used by Infra, OpenShift, and Crossplane. They consume `Rules` directly. There is no branch on Infra names or a profile type, and no delegation to an opaque application-wide aggregation function.

Output rules are matched by Condition type. With `LocalFirst`, rules whose parent policy excludes child contributions for a supplied local base are evaluated during the local input pass. Other supplied bases are deferred to `Rules.Outputs` order. A declared default runs only if no base of that type was supplied. A derivation runs after its output rule produces a result, so the initialized latch can be attached explicitly to availability. The generic code knows no initialization or sync Condition name.

```go
// Aggregate applies the configured rules to existing objects and reports in-memory changes.
func Aggregate(parent Conditioned, children map[string]Object, rules Rules, local ...metav1.Condition) (bool, error) {
	desired, err := Evaluate(parent, children, rules, local...)
	if err != nil {
		return false, err
	}
	if reflect.DeepEqual(parent.GetConditions(), desired) {
		return false, nil
	}
	parent.SetConditions(desired)
	return true, nil
}

// Evaluate computes all desired Conditions through the same generic resource-oriented call.
func Evaluate(parent Object, children map[string]Object, rules Rules, local ...metav1.Condition) ([]metav1.Condition, error) {
	if isNilObject(parent) {
		return nil, fmt.Errorf("parent is nil")
	}
	outputs := make(map[string]Aggregation, len(rules.Outputs))
	for _, rule := range rules.Outputs {
		if rule.Output.Type == "" || rule.Parent == nil || rule.Reduction == nil || rule.Messages == nil {
			return nil, fmt.Errorf("incomplete aggregation for %q", rule.Output.Type)
		}
		if _, exists := outputs[rule.Output.Type]; exists {
			return nil, fmt.Errorf("duplicate output %q", rule.Output.Type)
		}
		if rule.DefaultBase != nil && rule.DefaultBase.Type != rule.Output.Type {
			return nil, fmt.Errorf("default base %q does not match output %q", rule.DefaultBase.Type, rule.Output.Type)
		}
		outputs[rule.Output.Type] = rule
	}
	ignored := make(map[string]bool, len(rules.IgnoreLocal))
	for _, typ := range rules.IgnoreLocal {
		ignored[typ] = true
	}
	nowFunc := rules.Now
	if nowFunc == nil {
		nowFunc = time.Now
	}
	now := nowFunc()
	working := &conditionOverlay{Object: parent, conditions: append([]metav1.Condition(nil), parent.GetConditions()...)}
	apply := func(rule Aggregation, base metav1.Condition) error {
		condition, err := rule.Evaluate(working, children, base, now)
		if err != nil {
			return err
		}
		if condition != nil {
			working.SetConditions(*condition)
			for _, derivation := range rule.Derived {
				working.SetConditions(derivation.Derive(working, now)...)
			}
		}
		return nil
	}
	provided := map[string]bool{}
	pending := map[string]metav1.Condition{}
	for _, condition := range local {
		if ignored[condition.Type] {
			continue
		}
		provided[condition.Type] = true
		if rule, exists := outputs[condition.Type]; exists {
			if rules.LocalFirst && !rule.Parent.IncludeChildren(condition) {
				delete(pending, condition.Type)
				if err := apply(rule, condition); err != nil {
					return nil, err
				}
			} else {
				pending[condition.Type] = condition
			}
			continue
		}
		if rules.UnmappedLocal == nil {
			working.SetConditions(stamp(working, condition, now))
			continue
		}
		result, err := rules.UnmappedLocal.Evaluate(working, condition, now)
		if err != nil {
			return nil, err
		}
		if result != nil {
			working.SetConditions(*result)
		}
	}
	for _, rule := range rules.Outputs {
		if base, exists := pending[rule.Output.Type]; exists {
			if err := apply(rule, base); err != nil {
				return nil, err
			}
		} else if !provided[rule.Output.Type] && rule.DefaultBase != nil {
			if err := apply(rule, *rule.DefaultBase); err != nil {
				return nil, err
			}
		}
	}
	return working.GetConditions(), nil
}

```

#### Single-output evaluation

`Aggregation.Evaluate` applies one output rule to supplied objects and a local base. It selects and reduces contributions and encodes polarity without modifying caller objects. The generic resource-level operation above this primitive supplies the application rules; it contains no Infra vocabulary.

```go
// Evaluate derives one desired Condition without modifying the supplied objects.
func (a Aggregation) Evaluate(parent Object, children map[string]Object, base metav1.Condition, now time.Time) (*metav1.Condition, error) {
	if isNilObject(parent) {
		return nil, fmt.Errorf("parent is nil")
	}
	if a.Output.Type == "" || a.Parent == nil || a.Reduction == nil || a.Messages == nil {
		return nil, fmt.Errorf("incomplete aggregation for %q", a.Output.Type)
	}
	outcome, done := a.Parent.Before(parent, children, base)
	if done && outcome == nil {
		return nil, nil
	}
	if !done {
		var local, selected []Contribution
		if a.Local != nil {
			local = a.Local.Select(parent)
		}
		if a.Children != nil && a.Parent.IncludeChildren(base) {
			for _, child := range sortedChildren(children) {
				for _, contribution := range a.Children.Select(parent, child.Object) {
					contribution.Reference = child.Name
					selected = append(selected, contribution)
				}
			}
		}
		reduced := a.Reduction.Reduce(base, children, local, selected)
		outcome = &reduced
	}
	status := metav1.ConditionUnknown
	if outcome.State.Assessment != Unknown {
		good := outcome.State.Assessment == Good
		if a.Output.Polarity == FalseIsGood {
			good = !good
		}
		status = metav1.ConditionFalse
		if good {
			status = metav1.ConditionTrue
		}
	}
	generation := parent.GetGeneration()
	switch a.Generation {
	case PreserveBaseGeneration:
		generation = 0
		if outcome.Copied != nil {
			generation = outcome.Copied.ObservedGeneration
		}
	case NoObservedGeneration:
		generation = 0
	}

	transition := outcome.LastTransitionTime
	if transition.IsZero() {
		transition = metav1.NewTime(now)
	}
	if old, ok := Lookup(parent, a.Output.Type); ok && old.Status == status {
		transition = old.LastTransitionTime
	}
	return &metav1.Condition{Type: a.Output.Type, Status: status, Reason: outcome.Reason,
		Message: a.Messages.Format(outcome.Summary, outcome.Causes), ObservedGeneration: generation,
		LastTransitionTime: transition}, nil
}

```

#### Classifier, propagation, and local-check implementations

These implementations have direct, domain-specific fields. Exact reason mappings precede ordered suffixes. Local checks use the configured prefix and source polarity. Local bad bases take precedence; unknown bases admit local checks while excluding children.

```go
// ReasonSuffix assigns escalation to a matching reason suffix.
type ReasonSuffix struct {
	Suffix     string
	Escalation Escalation
}

// ReasonClassifier matches exact reasons, ordered suffixes, and a default in normal Go.
type ReasonClassifier struct {
	Reasons  map[string]Escalation
	Suffixes []ReasonSuffix
	Default  Escalation
}

func (r ReasonClassifier) Classify(c metav1.Condition) Escalation {
	if value, ok := r.Reasons[c.Reason]; ok {
		return value
	}
	for _, rule := range r.Suffixes {
		if strings.HasSuffix(c.Reason, rule.Suffix) {
			return rule.Escalation
		}
	}
	return r.Default
}

// Always and Never implement the two unconditional propagation cases.
type Always struct{}

func (Always) Allows(Object, Object) bool { return true }

type Never struct{}

func (Never) Allows(Object, Object) bool { return false }

// BeforeInitialized propagates only before the configured parent Condition is good.
type BeforeInitialized struct{ Parent ConditionType }

func (p BeforeInitialized) Allows(parent, _ Object) bool { return !p.Parent.IsGood(parent) }

// ExistingChildren includes new children during bring-up and initialized children thereafter.
type ExistingChildren struct{ Parent, Child ConditionType }

func (p ExistingChildren) Allows(parent, child Object) bool {
	return !p.Parent.IsGood(parent) || p.Child.IsGood(child)
}

// LocalBaseFirst preserves a bad local base and optionally overrides it during deletion.
type LocalBaseFirst struct {
	Base               ConditionType
	Classifier         Classifier
	OverrideOnDeletion bool
}

func (p LocalBaseFirst) Before(parent Object, children map[string]Object, base metav1.Condition) (*Outcome, bool) {
	if p.OverrideOnDeletion && isDeleting(parent) {
		out := badOutcome("Deleting", "resource is being deleted")
		return &out, true
	}
	if p.Base.Assessment(base) == Bad {
		out := copyOutcome(base, p.Base, p.Classifier)
		return &out, true
	}
	return nil, false
}
func (p LocalBaseFirst) IncludeChildren(base metav1.Condition) bool {
	return p.Base.Assessment(base) == Good
}

// LocalOnly applies the same base precedence while accepting no child inputs.
type LocalOnly struct{ LocalBaseFirst }

func (LocalOnly) IncludeChildren(base metav1.Condition) bool { return false }

// ChildrenOnly always reduces supplied children without interpreting a local base.
type ChildrenOnly struct{}

func (ChildrenOnly) Before(Object, map[string]Object, metav1.Condition) (*Outcome, bool) {
	return nil, false
}
func (ChildrenOnly) IncludeChildren(metav1.Condition) bool { return true }

// PrefixedChecks selects bad local Conditions under one explicitly configured type prefix.
type PrefixedChecks struct {
	Prefix     string
	Polarity   Polarity
	Classifier Classifier
}

func (p PrefixedChecks) Select(parent Object) []Contribution {
	conditions := append([]metav1.Condition(nil), parent.GetConditions()...)
	sort.Slice(conditions, func(i, j int) bool { return conditions[i].Type < conditions[j].Type })
	var result []Contribution
	for _, c := range conditions {
		if c.Type == p.Prefix || !strings.HasPrefix(c.Type, p.Prefix) {
			continue
		}
		if (ConditionType{Type: c.Type, Polarity: p.Polarity}).Assessment(c) != Bad {
			continue
		}
		result = append(result, Contribution{State: State{Bad, classify(p.Classifier, c)}, Condition: c,
			Source: parent, CheckName: strings.TrimPrefix(c.Type, p.Prefix)})
	}
	return result
}

```

#### Infra selects one diagnosis per child

The first matching Go branch wins for a child. Job-like conditions have their own priority and terminate selection even when the result contributes nothing. Regular conditions preserve the source's initialization and sync-human exceptions. In particular, this reference profile does not blanket-invalidate every other Condition whenever `Synced=False`: failed availability can be selected first. A strict validity gate is a different explicitly configured policy, not a correction silently imposed on this profile.

```go
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

```

#### Origin-aware reduction and configured output reasons

The reference reduction order is local failure, child failure, local human wait, child human wait, then ordinary bad/sync results. The ordinary branches and local-only path have additional ordering shown in the Go code. A single global numeric severity sort would lose these distinctions. `childReason` uses original child-map cardinality for legacy reason selection; it does not use resource names to choose rules.

```go
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

```

#### Availability-dependent local policies

`NonDegraded` and `UpToDate` consume Conditions that the caller has already computed. They do not cause the evaluator to discover dependencies or schedule another output. Their exceptional behavior is ordinary Go behind `ParentPolicy`.

```go
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

```

#### Deletion and teardown policies

Deleting children take precedence over teardown jobs. With no deleting children remaining, failed teardown precedes human wait, then running teardown. On one teardown job, however, human wait precedes running, and running precedes failure. The two separate implementations make this difference explicit. The output ConditionType declares negative polarity.

```go
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

```

#### Infra derivation, local checks, and the optional facade

Evaluation order is data in `InfraRules.Outputs`; `LocalFirst` preserves the Infra local-base precedence. Availability declares `InfraInitialization` as a local derivation. Deletion declares its default base. Additional local Conditions use `InfraLocalConditions`, which implements the original local-only prefix behavior through the same selector and reducer concepts.

The optional Infra facade contains only calls to generic `Aggregate` and `Evaluate` with its bound `Rules`. Initialization and summary are separate ordinary Go implementations. Failed escalation acquires no independent historical latch.

```go
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

```

#### Diagnostic helpers and proof boundary

The companion implementation includes stable ordering of named children, explicit nil handling, compatible reason selection, transition timestamps, and an opaque cause formatter. Internal helpers adapt getter access and stage intermediate Conditions; callers do not create an evaluation bundle.

`BoundedMessages` demonstrates opaque cause rendering, bounded output, and UTF-8-safe shortening. Infra explicitly selects `InfraMessages`, an application-specific implementation of the existing Infra grammar in [legacy_messages.go](examples/infra/legacy_messages.go). The reference multiline tests run against that legacy implementation. It is not the proposed native codec and is never inferred for arbitrary foreign messages. Both implementations still materialize diagnostic detail; bounded working memory and large-fan-out performance remain implementation work.

<details>
<summary>Diagnostic, object-access, and metadata helper implementations</summary>

```go
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

// conditionOverlay stages new Conditions while forwarding metadata to the original object.
type conditionOverlay struct {
	Object
	conditions []metav1.Condition
}

func (o *conditionOverlay) GetConditions() []metav1.Condition { return o.conditions }
func (o *conditionOverlay) SetConditions(conditions ...metav1.Condition) bool {
	changed := false
	for _, condition := range conditions {
		found := false
		for i := range o.conditions {
			if o.conditions[i].Type != condition.Type {
				continue
			}
			found = true
			if !reflect.DeepEqual(o.conditions[i], condition) {
				o.conditions[i] = condition
				changed = true
			}
			break
		}
		if !found {
			o.conditions = append(o.conditions, condition)
			changed = true
		}
	}
	return changed
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

// BoundedMessages demonstrates opaque cause rendering; a native codec is a separate implementation.
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

```

</details>

The independent two-call composition check classifies `DependencyFailed` on the first input and `ChildrenFailed` on the second. The first returned Condition is supplied as a Condition of the next child resource. Replacing the original supplied input with a good Condition lets both outputs recover despite retained previous failures. Neither classifier reads messages.

The general validity/freshness API, reusable adapter contracts, bounded working memory, and native/foreign codec remain open. The OpenShift and Crossplane examples below exercise concrete native adapters without making their API modules core dependencies. They do not require changing the resource-oriented call style.

### OpenShift operator status example

The [OpenShift profile](examples/openshift) consumes native `operatorv1.OperatorStatus` from an operator configuration object and publishes native `configv1.ClusterOperator` Conditions. Its inputs are **the operator’s assertions**, such as `ConfigDegraded` and `RouteDegraded`, rather than the Conditions of its operand workloads. It expresses library-go’s [suffix union](https://github.com/openshift/library-go/blob/release-4.19/pkg/operator/status/condition.go) used by the [operator StatusSyncer](https://github.com/openshift/library-go/blob/release-4.19/pkg/operator/status/status_controller.go).

A single supplied operator contributes every matching controller Condition to each output. This requires `ChildSelector.Select` to return a slice: Infra’s choice of at most one diagnosis per child is profile policy, not a core restriction. Available and Upgradeable use `TrueIsGood`; Progressing and Degraded use `FalseIsGood`. Missing data is Unknown, a known opposite status wins over Unknown, and all four outputs remain independent. Optional inertia is an ordinary Go implementation in the profile. Reasons and multiline messages retain the native OpenShift union conventions.

```go
changed, err := c.Aggregate(
    openshift.ClusterOperator{ClusterOperator: cluster},
    map[string]c.Object{
        "Operator": openshift.Operator{Object: operator, Status: &operator.Status.OperatorStatus},
    },
    openshift.OpenShiftRules,
)
```

The native schemas have no per-condition observed generation. Their adapters preserve that distinction, with `NoObservedGeneration` selected explicitly for outputs. Existing unrelated status fields and Conditions remain owned by their caller. [Executable tests](examples/openshift/openshift_test.go) cover union semantics, inertia, native metadata, recovery, and rejection of operand Conditions as operator input. The [README](examples/openshift/README.md) contains complete status YAML and the scope relative to the full upstream StatusSyncer.

### Crossplane Conditions example

The [Crossplane profile](examples/crossplane) uses the actual `ConditionedStatus`, Condition type, and constructors from `crossplane-runtime/v2` v2.0.0. Its adapter preserves every native Condition field while exposing the root package’s slice-based accessors. Upstream API modules remain dependencies of the example alone.

This example chooses to roll up child Ready into parent Ready while keeping the parent’s Synced assertion local. Ready False for Creating, Deleting, or Unavailable is ordinary bad state, not implicit failure escalation. Missing readiness is Unknown. A supplied local bad/unknown Ready base keeps precedence; otherwise the declared good base is reduced with child readiness. Configured output reasons are Available, Creating, and ReadinessUnknown.

```go
changed, err := c.Aggregate(
    crossplane.Adapter{Resource: parent},
    map[string]c.Object{"database": crossplane.Adapter{Resource: database}},
    crossplane.CrossplaneRules,
    crossplane.ToCondition(xpv1.ReconcileSuccess()),
)
```

A supplied `Ready=True` can coexist with `Synced=False/ReconcileError` or ReconcilePaused. Neither the core nor this profile universally invalidates readiness on a sync error. This demonstrates why sync validity is an optional semantic policy, not a built-in Condition name. It does not implement every Crossplane composition or function-readiness mode. [Tests](examples/crossplane/crossplane_test.go) and [status YAML](examples/crossplane/README.md) make the configured boundary explicit. Reserved, not-yet-implemented Healthy in this pinned API is not treated as an available readiness mechanism.

## Compatibility and adoption

Aggregation rules and policies are ordinary Go configuration in each consuming application. The generic package requires no new resource spec fields or Condition severity fields. Each integration owns its published Condition names, reasons, polarity, lifecycle policy, and observed-generation contract.

Adoption can proceed incrementally:

1. Implement and characterize the generic evaluator and message codecs without changing a consuming controller's published behavior.
2. Express that controller's current mappings and preserve its reasons, readiness gates, output polarity, and metadata conventions. Compare with its existing tests.
3. Adopt native messages on selected producer/consumer paths. Legacy and foreign messages remain opaque unless explicitly adapted.
4. Enable bounded multiline promotion and document the human-facing message changes. Evaluate lifecycle, phase, or summary changes separately.

Existing JSON/YAML clients continue to see `metav1.Condition`. Machine consumers use Condition fields rather than parse diagnostic messages. Mixed-version and downgrade cases must cover native, foreign, malformed, and legacy messages. Formatting does not change the resource specification or introduce a new lifecycle policy.

Stable reason strings should be retained where possible. New aggregate reasons, different failure classification, or changed summary precedence require explicit compatibility decisions. The library does not silently reinterpret an application's terminal states through message formatting.

## Validation

The root package and all three example modules are executable design material. Infra includes generic resource-level calls and the demonstrated two-call semantic composition and recovery. The remaining fixtures below define production-library acceptance requirements. Semantic composition and message-format validation remain separate test groups.

The example is checked with Go 1.25 and `k8s.io/apimachinery` v0.34.2 in an isolated module. All 27 semantic fixtures, their 27 resource-level caller replays and idempotence checks, the 17 presentation fixtures, output-polarity checks, compatible two-call composition/recovery, pure and failed-evaluation nonmutation, optional children without sync, an unrelated rule set using different names and negative output polarity, and the printed generic invocation passed. In addition, all 117 ported reference cases run without skips. This is execution of the adapted public cases, not a build of the private package. [reference_tests.json](examples/infra/reference_tests.json) records the source revision, blob identities, case counts, and adaptations. GitHub Actions runs formatting checks and race tests in a four-module matrix: core, Infra, OpenShift operator, and Crossplane. OpenShift tests cover four union truth tables, multi-assertion selection, optional inertia, native metadata, and recovery. Crossplane tests cover the native adapter, child readiness, local Synced independence, recovery, and bounded foreign diagnostics.

To reproduce the model checks from the repository root:

```sh
go test -race ./...
go -C examples/infra test -race ./...
go -C examples/openshift test -race ./...
go -C examples/crossplane test -race ./...
```

### Semantic composition fixture

Configure two aggregations to preserve failure through their classifier and output-reason implementations. Pass the returned Conditions directly from the first call into the second; message serialization is not needed for this test.

| Supplied input | First output | Second output |
| --- | --- | --- |
| `Available=False`, `DependencyFailed` | `Ready=False`, `ChildrenFailed`, "One dependency failed". | Explicitly classify `ChildrenFailed` as failed and publish the configured failed Condition. |
| Replace with `Available=True`, `DependencyReady` | `Ready=True` with the configured good reason. | The failed contribution disappears even when previous Conditions report failure. |

Test negative output polarity with the same normalized result. This checks compatible implementations and configurations, not a guarantee that every classifier preserves every reason's escalation.

### Semantic tests

- Evaluate the Infrastructure profile through the concept interfaces and the complete fixture corpus. Differential conformance against each reference integration remains a release gate; the public corpus runs independently.
- Validate early local copies, unknown-base local-only aggregation, first-match termination, origin-specific reduction order, group membership without duplicate children, and empty-versus-missing behavior.
- Test the common contracts of standard and custom Go implementations; semantic classification must not depend on message rendering or resource-instance selectors.
- Both source polarities combined with both output polarities, including `Unknown`.
- Missing required inputs, absent optional diagnoses, and declared freshness handling for stale Conditions.
- An aggregation without validity rules works with resources that have no sync condition; an undeclared `Synced` condition has no implicit effect.
- Configured validity rules exclude both old good and old bad guarded conditions, contribute only the declared replacement for that input path, preserve provenance, and leave unrelated resource branches unaffected.
- Optional missing controlling inputs, required missing inputs, `Unknown`, staleness, both controlling polarities, invalid validity-scope rejection, and recovery when a controlling condition becomes usable again.
- A configured child sync failure yields `Bad/Failed` in the consuming aggregation without requiring parent `Synced=False`, and is encoded correctly for both parent output polarities.
- Ordinary negative state, human-action waits, and failed escalation; the compatible-aggregation fixture above preserves failure through two calls independently of message format.
- `Failed` dominates an unrelated `WaitingForHuman` contribution; resolving the waiting branch does not repair the failed branch.
- Replacing the supplied child Conditions with healthy Conditions removes the failed contribution when the configured rules permit it, even when previous parent Conditions report failure.
- Local/base precedence, deletion, initialization-dependent propagation, job-like sources, and explicit caller sequencing.
- Equivalent observations produce equivalent semantic results regardless of input ordering.

### Message-format fixtures

These fixtures use the same supplied Conditions and explicit semantic mappings, while varying only rendering or message representation. Native-format assertions must be finalized with the grammar before the format is published; until then, tree expectations describe the required supported structure rather than committing to undecided byte syntax.

| Fixture | Configuration and input | Required result |
| --- | --- | --- |
| Shortened diagnostic | Evaluate the failed fixture with cause rendering and line budgets of 7 and 1; also replace only its child message with shorter text. | `Ready=False`, `Degraded=True`, and `ChildrenFailed` remain unchanged. Rendering never changes the winning contribution. |
| Both output polarities | Evaluate the same failed and healthy snapshots with paired positive and negative outputs. | The failed snapshot encodes `Ready=False` and `Degraded=True`; the healthy snapshot encodes `Ready=True` and `Degraded=False`. |
| Native promotion | Supply a native cause tree, render an aggregate with enough budget to retain it, and let another aggregation explicitly select `Native` for that output. | Supported retained causes remain structurally nested, subject to the documented condensation rules. Classification still uses that aggregation's explicit Condition mappings. |
| Opaque foreign message | Select `Foreign` for text such as `Looks native:\n  Worker/a: failed`, including reserved native markers once defined. Promote it through a native output and decode that output in another call. | The foreign text remains an opaque leaf; its indentation, colons, and words create neither new causes nor escalation. |
| Truncated causes | Supply more cause detail than the seven-line budget can retain, including Unicode and opaque messages. | Keep a useful summary and report omitted detail within the same line/size budget. If only a summary fits, include omission information there. Semantic reduction and counts still consider all eligible contributions. |

The first two rows can be checked against `Status` and `Reason` expectations without accepting or decoding any native grammar. The last three exercise the message codec and renderer separately. The composition fixture intentionally requests no detail; truncation tests request causes and verify omission reporting when requested detail does not fit.

Additional message cases:

- Port the reference design's examples A-F as behavior fixtures, distinguishing native and opaque inputs explicitly.
- Summary-only output, nested native details, single-child inline chains, branching, deepest-first condensation, and omission counts.
- Opaque messages containing colons, nested indentation, reserved header text, literal markers, URLs, long lines, Unicode, CRLF, and trailing whitespace.
- Native render/decode/render stability within the retained information; opaque blocks remain opaque after repeated promotions.
- Classification of the same Condition fields is invariant under message truncation, formatting changes, and malformed native structure.
- Line and encoded-size limits are enforced with UTF-8-safe output.

### Integration and scale tests

- Independently configured producer and consumer aggregations agree on the intended reason mappings; DCD -> DGD -> DGDR is a Dynamo integration example. Native cause preservation is checked separately from semantic composition.
- Native and foreign resource adapters preserve their declared source semantics.
- Equivalent aggregation definitions, Conditions, metadata, prior Conditions, and time produce equivalent results; an intentionally changed aggregation can change interpretation.
- Large input sets exercise classification, aggregation, and omission accounting independently of Kubernetes discovery.
- Aggregation and rendering retain bounded diagnostic memory and respect message budgets when many inputs contribute failures.
- Status persistence remains owned by the calling controller.

### Package boundary tests

- Test evaluation and formatting using resource getters and metadata, without a Kubernetes client, controller setup, or application resource types.
- Exercise aggregation definitions through exported APIs only, using synthetic GVKs, condition names, polarities, and reason classifiers.
- Exercise both the Infrastructure profile and an unrelated application profile through the same core. Profile-specific vocabulary, lifecycle gates, and summary order require no core branches.
- Verify the documented native message fixtures across producer and consumer profiles, including compatible format versions and legacy fallback.
- Check the core's import aggregation so an integration cannot accidentally introduce controller, client, or application dependencies into it.

The public Infrastructure behavior matrix is the self-contained conformance corpus. Consuming projects should additionally compare the profile with their existing integration suites. Preserve applicable attribution and keep application modules outside the library dependency graph.

## Implementation plan

1. Review and refine the executable concept interfaces and Infrastructure example above. Finalize the general validity/freshness and adapter contracts, remove prototype-only compatibility leaks from the shared API, and validate against the reference integration suites. Keep orchestration in Go and settle the native format before publishing it.
2. Refine the experimental root evaluator and bounded formatting, adding general validity/freshness, codec, and package boundary tests. Preserve the existing polarity, escalation, composition, and reference-profile tests.
3. Retain the executable Infra, OpenShift operator, and Crossplane profiles as expressibility tests without resource-specific branches in the engine; keep Dynamo mappings in its integration layer.
4. Integrate a representative native -> native condition promotion chain and a foreign-resource adapter with existing conditions and reasons. A DCD -> DGD -> DGDR chain is one possible Dynamo example, not a constraint on the library.
5. Validate resource-specific interpretation, mixed-version behavior, and preservation of caller-owned lifecycle policy.
6. Measure aggregation and formatting across small and large input sets, document the externally visible message changes, and roll out incrementally.

## Alternate solutions

### Continue hand-written string promotion in each controller

This keeps each change local but repeats classification, escalation, freshness, and formatting logic. Nested diagnostics continue to be lost or expanded without a consistent budget.

### Copy the complete reference aggregator

Its formatting and tests are useful references, but its hard-coded vocabulary, initialization rules, reason conventions, and dependencies do not fit every controller. Expressing the behavior as a profile tests the abstraction without coupling the shared package to one application.

### One global severity order

It cannot represent a serving resource that remains available while an update waits for human approval, nor local/base precedence or non-transitive sync semantics. Ordering is necessary but insufficient without configured propagation and separate summary policy.

### Discover and traverse resource hierarchies in the evaluator

This moves resource discovery and lifecycle policy into another reconciliation system. Static aggregations over kinds and condition types are sufficient; callers already own the relevant resource sets.

### Parse every message as a hierarchy

Foreign messages can contain arbitrary colons and indentation. Parsing them as native structure invents dependencies and can change the meaning of diagnostics. Explicit constructors and a codec preserving opaque boundaries avoid this.

### Treat every failed escalation as terminal

This misrepresents recoverable failures and introduces a parent lifecycle latch unrelated to the source's behavior. Terminality remains owned by the source; aggregation reports current escalation.

## Open decisions

1. Finalize the concept interfaces and standard implementations after reviewing the executable Infrastructure example. Settle validity/freshness, adapters, bounded working memory, and placement of compatibility metadata. The shared root package and its three profiles remain experimental; the Infra corpus includes 44 original fixtures and all 117 ported reference cases.
2. Finalize the native message grammar, reserved-header escaping, format evolution, and legacy fallback. The literal-block notation is a proposed encoding, not a committed wire contract.
3. Choose initial example and integration reason-classification/output-reason policies while preserving consumer expectations. Configure the Infrastructure profile's suffix conventions independently.
4. Set the default diagnostic-detail size/depth limits and select performance acceptance thresholds after baseline measurements. Seven lines is a presentation default, not a latency guarantee.
5. Decide whether any consuming integration needs new Conditions or different lifecycle/summary precedence. They are separate from adopting the mechanism.

## References

- [Kubernetes API conventions: typical status properties and Conditions](https://github.com/kubernetes/community/blob/main/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties).
- [API machinery Condition helpers](https://github.com/kubernetes/apimachinery/blob/master/pkg/api/meta/conditions.go).
- [Cluster API Conditions Getter](https://github.com/kubernetes-sigs/cluster-api/blob/main/util/conditions/getter.go) and [Setter](https://github.com/kubernetes-sigs/cluster-api/blob/main/util/conditions/setter.go).
- [Kubernetes template project](https://github.com/kubernetes/kubernetes-template-project).
- [Public Infra model](examples/infra/infra.go), [checks](examples/infra/infra_test.go), and [fixtures](examples/infra/fixtures.json).
