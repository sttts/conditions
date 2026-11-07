<!--
SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
SPDX-License-Identifier: Apache-2.0
-->

# Condition aggregation and composable policy

Status: experimental design for `github.com/sttts/conditions`. The API is pre-v1.
Implementation details live in the linked Go sources, rather than copied implementations
in this document. [FORMATS.md](FORMATS.md) specifies the existing message contracts.

## Schema-oriented public API

The controller boundary is now `Aggregate(parent, children, aggregation, local...) bool`,
with `AggregateSlice` for `[]Object`. `Define` builds an immutable aggregation during startup
and `Aggregation.Schema()` publishes its output interpretation. The README contains the
complete user-facing API and executable examples.

A `Schema` declares condition names, polarities, reason classifiers and optional suffix
families. API scopes are GVK/GV/G/GK, or global when unspecified. Consumer bindings may narrow
them with `For`; narrower matching bindings replace broader fallbacks. Equal/incomparable
intersecting bindings are construction errors. Unmatched children do not participate. No
global registry is introduced. `Definition.Scheme` optionally resolves Go types with empty
TypeMeta; `Types(scheme)` constructs G/GV/GK/GVK scopes from `runtime.Object`. Explicit GVK
takes precedence. Projection must be unambiguous, and runtime candidates must all select
the same binding. Resolution errors precede mutation. No GVR or RESTMapper is involved.

`Classify(Map(...))` translates provider vocabulary to consumer outputs.
`Identity` explicitly asserts same-name semantic equivalence within a shared vocabulary.
Classification normalizes source polarity, retains diagnostics, and optionally adjusts
escalation using first-match Reason/Status predicates. Reduction combines contributions and
output encoding uses the destination schema's polarity. There are no universal semantic Kinds.

The default `All` reducer exports its own escalation reason interpretation with its output
schema. Therefore a subsequent consumer can reference that schema without reproducing the
original reducer. Local-only outputs and contextual Go policies remain explicit. `Definition` and all helper results are open Go structs. Each output declares its contract
and derivation together; the output schema is generated from these definitions. There is
no separate schema declaration or rule-reassembly bridge. `Spec.Reasons` interprets
bad-condition reasons; `Classification` translates names at the consumer boundary.

Unmapped sources are ignored, allowing additive provider evolution. `RequireAll` is an
optional exhaustive check. `Identity` maps the intersection of source and output names.
Domain-specific selectors and reducers can be ordinary Go functions using the function
adapters, or reusable interface implementations. Output-level `Children` owns contextual
selection over the admitted child objects; mapping-level `Select` specializes one binding.
These callbacks are native extension points, not actions in a DSL interpreter.

Configuration errors panic during `Define`. The bool-only application call also panics on
caller-contract errors, such as duplicate local assertions, before mutation. Observational
absence/Unknown is represented in conditions. `EvaluateAggregation` exposes pure evaluation
with returned runtime errors. This choice is explicit: a false change flag never hides an
error. Custom policy implementations are required to be pure and concurrency-safe.

## One definition and one evaluation path

`Definition` is an ordinary Go value containing output definitions, child bindings, API scopes,
an optional Scheme and clock. `Define` validates and snapshots it. Each `OutputDefinition`
contains its published `Spec` and its derivation. `Aggregation.Schema()` is generated from
those outputs, including contracts for owned derived conditions.

Providers publish output values or slices; consumers combine them using normal Go append or
struct literals. There is no public Rule/Rules configuration or separate Compose operation.
The compiled execution plan is private. `Aggregate` and `AggregateSlice` apply results through
the native setter; `EvaluateAggregation` returns desired conditions without mutation.

## Dependencies and ownership

Each output exclusively owns its named condition and the types declared by its `Derived`
implementations. `Derivation.Outputs()` returns `map[string]Spec`, declaring ownership and
the published contract once. Define snapshots these maps and their reason interpretations.
Duplicate ownership, invalid Specs, missing dependencies and dependency cycles
are construction errors, even if no local assertion would activate the output.

`Requires` exposes named dependencies' results from the current evaluation. Every output reads
a snapshot of prior conditions and supplied local observations plus only those declared current
results. Unrelated outputs remain isolated, independent of declaration order. Derivations run
after their owning output and see its current result. Undeclared or duplicate derived results
are errors before mutation.

If a dependency produces no current result, its persisted value remains visible. `Requires`
expresses dataflow, not freshness. Output ownership prevents caller-supplied local conditions
from replacing an independently derived condition such as Infra's Initialized latch.

## Semantics and extension points

Source polarity normalizes True/False into Good/Bad; other statuses remain Unknown.
`Spec.Reasons` interprets escalation only for bad observations. Classification translates
source names into consumer outputs and can adjust participation or escalation. No universal
semantic kinds are imposed on independent APIs.

The default reducer is All, with Bad > Unknown > Good and Failed > WaitingForHuman > None.
No contributions means Unknown. Non-good local bases take precedence over children.
Default All outputs publish their generated escalation-reason interpretation automatically.
Custom reducers declare their reason interpretation in the output's Spec.

Domain code remains ordinary Go. Selector, reducer, parent-policy, derivation and formatter
implementations can be reused directly; function adapters cover simple callbacks. A custom
output child selector owns interpretation of admitted child objects. It need not decompose
its multi-condition policy into mappings. Custom parent policies own their missing-base
convention; DefaultBase supplies an explicit base when needed. LocalOnly excludes children.

No global LocalFirst, UnmappedLocal, IgnoreLocal or LocalEvaluator settings exist. Additional
local-prefix checks are explicit selectors, and unowned local assertions pass through.

## Formatting and presentation

Each output selects a `Formatter` through its `Formatter` field or the `Format` helper.
The interface has one method: Format(summary, causes). It renders the reducer's diagnostic
result without influencing status, reason, escalation or metadata. The default BoundedMessages
preserves opaque foreign text; MultilineMessages parses the explicitly selected nested format.
See [FORMATS.md](FORMATS.md) for formatting contracts.

`InlineFormatter` is separate and optional. It renders a short view of an existing condition;
aggregation does not call it. Both built-in formatters support it. Presenter implementations
such as InfraSummary may use an InlineFormatter without requiring the aggregation formatter.

## Native profiles and adapters

- Infra's outputs directly configure lifecycle selectors and reducers, local prefix checks,
  initialization derivation, availability/degradation/update dependencies and deletion behavior.
- Crossplane publishes ReadyOutput for composition, local Synced, and a native readiness reducer
  whose good default permits child-only reconciliation. Child Synced is not promoted to parent.
- OpenShift uses scoped suffix families and native operator-status union reduction and diagnostics.
  Operand conditions do not drive operator status. Optional inertia stays ordinary domain code.

Adapters translate native condition representations and preserve metadata. A native boolean
setter reports actual changes; WithChangeDetection adapts void setters. No Kubernetes reads,
status writes or persistence occur inside aggregation.

## Atomicity, metadata and verification

The evaluation clock is sampled once per call. Existing slice order is retained; newly
produced types are appended in canonical order. Existing unrelated conditions are preserved.
Unchanged status preserves LastTransitionTime even when diagnostics change. Generation policy
selects parent generation, preserved base generation or no observed generation.

Definitions snapshot owned maps and slices. Custom implementations and the optional Scheme
must be immutable during use. Caller-contract and resolution errors precede the final setter.
Aggregate's bool-only signature panics on those errors; EvaluateAggregation returns errors.
Unknown and missing conditions remain ordinary observations, not evaluation errors.

Core tests exercise both public definitions and the internal evaluator. Example tests check
the original semantic and message fixtures directly through the public API. There is no
parallel legacy configuration or second evaluator. Cross-module tests compose published
output values and consume published schemas. Derived-contract tests cover publication,
snapshot isolation, invalid ownership and atomic rejection of undeclared results.

Provenance-aware mixed-format promotion, freshness policies and large-fan-out performance
validation remain future work.
