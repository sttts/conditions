<!--
SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
SPDX-License-Identifier: Apache-2.0
-->

# Condition aggregation and composable policy

Status: experimental design for `github.com/sttts/conditions`. The API is pre-v1.
Implementation details live in the linked Go sources, rather than copied implementations
in this document. [FORMATS.md](FORMATS.md) specifies the existing message contracts.

## Boundary

A controller supplies existing parent and child objects, local Conditions, and explicitly
selected rules. The library derives desired Conditions at **one resource boundary**:

```go
changed, err := conditions.Aggregate(parent, children, rules, localConditions...)
```

`Evaluate` has the same arguments and returns the desired slice without a setter.
`Aggregate` stages evaluation, invokes the native setter once on success, and returns
its change flag. Neither operation discovers resources, queries Kubernetes, persists
status, emits Events, advances status-level observed generation, or owns reconciliation.

The root package depends only on the Go standard library and Kubernetes API machinery.
Resource APIs, classifiers, lifecycle gates, selection, reduction, and presentation
policies belong in independent consuming packages. The three examples are separate
modules. A consuming controller need not import any example.

## Selection and composition

A provider exports `Aggregation` values or ordinary constructors returning them. It may
also export a `[]Aggregation` bundle. Providers implement the public policy interfaces;
they do not register with the core or import one another.

The caller selects individual values and/or bundles:

```go
rules, err := conditions.Compose(
    []conditions.Aggregation{lifecycle.InfraSynced},
    []conditions.Aggregation{crossplane.ReadinessRule()},
)
if err != nil {
    return false, err
}
return conditions.Aggregate(parent, children, rules, localSynced)
```

This is an executable, cross-module controller boundary in
[compose_external_test.go](examples/crossplane/compose_external_test.go). It combines
Infra's **local** reconciliation/prefix-check policy and Crossplane's child-readiness
policy. Neither enables the unselected Infra availability, initialization, degradation,
update, or deletion rules. Child reconciliation success cannot repair a local failure;
child reconciliation failure does not automatically invalidate readiness.

`Compose(groups ...[]Aggregation) (Rules, error)` allocates a new output slice and validates
the combined selection. It does not add dependencies automatically. Callers can instead
construct `Rules{Outputs: ...}`; `Evaluate` repeats validation so that this path cannot
bypass conflicts. `Rules.Now` is invocation-wide clock injection, sampled once per call,
not a provider policy. Set it on the assembled `Rules`, not on provider bundles.

`Aggregation` still composes the existing concepts:

- `Output`: type and polarity of one primary Condition.
- `DefaultBase`: an explicit base used only when the caller omitted that local type.
- `Requires`: selected Condition outputs whose current-call values this rule reads.
- `Derived`: post-output derivations with declared output ownership.
- `Parent`, `Local`, `Children`, `Reduction`: ordinary Go policies.
- `Messages`: an independently selected formatter.
- `Generation`: the output's explicit metadata contract.

See [conditions.go](conditions.go) for the public declarations and single-output evaluator,
and [composition.go](composition.go) for validation, scheduling, and resource-level evaluation.
There is no registry, resource graph, selector language, or embedded policy DSL.

## Read model and dependencies

Incidental list order must not become lifecycle policy. Each rule reads:

1. The parent's **previous persisted Conditions and metadata**.
2. All supplied **unowned local Conditions**, stamped using the shared clock.
3. Only the current-call results explicitly named in its `Requires` list.

A local base for a selected primary output is an argument to that rule, not an
unevaluated current assertion visible to every other rule. A local value for an owned
derived output is ignored: its derivation owns that assertion. Omitted primary bases do
not imply success. Without a default, that rule and its derivations do not run.

`Requires` names Condition types, including derived types. Each dependency must have
exactly one selected provider. Providers run before consumers using a deterministic
topological order. A missing provider or any cycle, including self-dependency, is an
error. The library never guesses dependencies from names or hides missing selections
by automatically importing a whole profile.

Only the named dependency values become visible, not every Condition produced by their
providers or transitive dependencies. A rule needing both Available and Initialized
must declare both. Previous persisted values remain available even without a dependency;
this deliberately supports latches and prior-state policies. For example, availability
reads **previous** Initialized, then derives the current Initialized. Requiring its own
Initialized would be a cycle, not a valid way to implement that latch.

If a selected dependency produces no new value (missing base or policy abstention), its
previous persisted value remains readable, or `Lookup` reports missing/Unknown. A
`Requires` edge guarantees selection, scheduling, and visibility, **not freshness or a
new observation**. Domain policy must decide whether that missing/prior value is usable.
A general freshness/validity framework remains separate work.

Rule definitions and local argument permutations yield the same desired Conditions for
pure policies and equivalent inputs. Existing parent slice order is retained; newly
copied local types and newly computed types are appended in deterministic type order
within their respective groups. Condition type, not incidental insertion order, is the
identity. The reference suite compares map entries by type while retaining all field
and repeated-call change assertions.

## Ownership and conflicts

One selection has exactly one writer for each primary or derived Condition type.

- Duplicate primary outputs are errors, even if the definitions appear identical.
- Derived-vs-primary and derived-vs-derived collisions are errors.
- `Derivation.Outputs() []string` declares stable, exclusive output ownership.
- Nil derivations, empty derived output names, incomplete primary rules, and mismatched
  default-base types are errors before policy evaluation.
- Duplicate local assertions of the same type are errors, not last-writer-wins.
- A derivation returning an undeclared type or repeated result fails evaluation without
  calling the setter. It cannot overwrite another provider's output.
- Unowned local Conditions are copied, not interpreted by a profile-wide fallback.
  Existing unowned parent Conditions are retained.

Derivations run only after their primary output exists. Each sees that primary result
and the same dependency/input view; sibling derivations do not see one another's new
results. A policy requiring additional sequencing should expose another Aggregation
with `Requires`, rather than rely on derivation list order.

Validation cannot prove arbitrary Go implementations correct or prevent them from
using mutable globals, clocks, I/O, or mutating objects. Policy implementations must be
pure, read-only, deterministic for supplied inputs, and safe to share when used
concurrently. Configuration is ordinary Go data with ordinary shallow-copy semantics;
`Compose` does not deep-clone policy objects, slices inside rules, or base pointers.
Treat shared definitions as immutable and copy mutable configuration before editing it.
No concurrent mutation of rules or supplied objects during evaluation is supported.

## Removing the old global couplings

The earlier prototype used `Rules.LocalFirst`, `UnmappedLocal`, and `IgnoreLocal`, with
rules observing an incrementally mutated parent in output-list order. Those fields and
the root `LocalEvaluator` contract are removed, not retained behind another facade.

- **Local precedence** stays in each rule's `ParentPolicy` and `Reducer`. All unowned
  local checks are available before evaluation; dependency edges select current results.
  An independent rule cannot silently see an unrelated provider's newly computed output.
- **Ignored local Initialized** becomes ownership declared by `InfraInitialization`.
- **Availability before gates** becomes `InfraNonDegraded.Requires = {Available,
  Initialized}` and `InfraUpToDate.Requires = {Initialized}`.
- **Additional local-only prefix outputs** require explicit selection with
  `lifecycle.LocalChecks("Custom")`. A profile cannot silently reinterpret arbitrary local
  Conditions supplied by another package. Unselected types use ordinary copy semantics.

This is an intentional experimental-API migration. Callers depending on catch-all local
prefix aggregation must select each such output. Configurations relying on incidental
current-output reads must declare them. Newly appended Condition ordering is canonical,
not the old local-first insertion order. Status/reason/message/metadata semantics of
the reference profile are retained; list insertion order is not a lifecycle contract.

## Semantic axes remain independent

Polarity declares which known boolean is good; Assessment normalizes to Good, Bad, or
Unknown. Escalation distinguishes ordinary bad state, WaitingForHuman, and Failed.
These are separate dimensions, not one global severity number.

A bad contribution can encode as `Available=False` or `Degraded=True`. Only an explicit
classifier interprets source reasons. Output reason and the next consumer's classifier
must agree to preserve escalation across resource boundaries. There is no universal
reason vocabulary or implicit sync validity gate. A child Synced assertion is not a
parent reconciliation assertion.

Failed escalation imposes no terminal lifecycle or historical failure latch. Replacing
supplied observations with healthy values permits recovery according to the selected
policies. Whether the source stops reconciling, retries, or requires human action stays
with that source. Overall phase/UI presentation is separate from Condition evaluation;
Infra's presenter puts ordinary unavailability ahead of degradation without changing
those independent Conditions.

## Profiles and representation adapters

### Infra

[infra.go](examples/infra/infra.go) is the canonical lifecycle expressibility example.
It retains the independently interpreted availability, initialization, degradation,
update, reconciliation, and deletion axes. Initialization-aware propagation, local-vs-child
precedence, job diagnoses, human-wait exceptions, failure classification, recovery, and
summary policy remain ordinary Go in this example, not core branches.

`InfraAvailability` owns Available plus derived Initialized. NonDegraded and UpToDate
declare the current-output dependencies described above. `InfraSynced` and `InfraDeleting`
are independently selectable. Full `InfraRules` and its injection-only facade remain
conveniences. Additional local outputs use `LocalChecks(type)`. All select the same root
`MultilineMessages` implementation and preserve Infra's explicit base-generation policy.

The public corpus includes 27 semantic fixtures, 17 summary fixtures, and all 117 ported
reference aggregation/message cases; provenance is in
[reference_tests.json](examples/infra/reference_tests.json). These are public adapted
cases, not execution of a private integration. Compatibility details include failed
availability preceding ordinary sync failure on the same child, first-match job
selection, no automatic parent sync promotion, and the existing reason conventions.

### OpenShift

[openshift.go](examples/openshift/openshift.go) projects native **operator assertions**,
not operand workload readiness, into native ClusterOperator status. `UnionRule(type)`
selects one suffix union; `OpenShiftRules` selects the existing four independent unions.
Available/Upgradeable use TrueIsGood; Progressing/Degraded use FalseIsGood. Missing input
is Unknown, known adverse state precedes Unknown, and optional inertia stays a profile
policy. Native reasons, union diagnostics, source transition times, and the absence of
per-condition observed generation remain explicit. This is not a full StatusSyncer.

### Crossplane

[crossplane.go](examples/crossplane/crossplane.go) preserves native ConditionedStatus
fields through its adapter. `ReadinessRule()` supplies the existing readiness policy;
`CrossplaneRules` selects it. Local bad/unknown Ready precedes children, child missing
Ready is Unknown, and ordinary bad precedes Unknown. Synced remains independent and
local. No failure escalation is invented from Creating, ReconcileError, or ReconcilePaused.
The pinned API's reserved Healthy type remains unused. This is not a full composition
controller or an implementation of every function/readiness mode.

## Metadata and application

Object access uses `GetConditions()` and standard Kubernetes generation/creation/deletion
getters, not a Kubernetes client interface or scheme. The root setters apply the complete
desired slice. The native setter's boolean represents actual stored changes, including
message/reason/metadata changes with unchanged status. A normalized native no-op may
return false even if an unsupported incoming field differed.

`WithChangeDetection` supports setters returning nothing by copying before assignment
and comparing stored Conditions afterward with Kubernetes semantic equality. It handles
in-place writes and native normalization. Other status remains caller-owned.

Transitions change when status changes, not on every diagnostic update. Output generation
is the parent's generation by default. `PreserveBaseGeneration` preserves copied bases
and uses zero for new derived aggregates; `NoObservedGeneration` supports schemas without
that field. Adapters do not manufacture a child's generation as a parent's assertion.
Time is sampled once per evaluation and passed explicitly to derivations and single-output
execution. Custom policy clocks, such as optional inertia, remain explicit policy inputs.

## Diagnostics

`Formatter` receives a summary and retained causes, independently of semantic selection
and classification. Select `MultilineMessages` for its documented colon/indentation
structure or `BoundedMessages` to keep arbitrary source text opaque. The same rule can
choose either formatter without changing status, reason, escalation, or dependencies.
OpenShift's native union formatter is another independent implementation.

[FORMATS.md](FORMATS.md) defines promotion, line/byte/character budgets, UTF-8 behavior,
and explicit source-format assumptions. No classifier infers severity from indentation,
English words, truncation markers, or native-message parsing. Mixed-format provenance,
escaping, versioned opaque boundaries, bounded diagnostic working memory, and large-fan-out
performance validation remain open; this change does not claim to implement them.

## Verification and limits

[composition_test.go](composition_test.go) and [provider_contract_test.go](provider_contract_test.go)
exercise output isolation, declared current-result reads, missing/cyclic dependencies,
primary/derived conflicts, undeclared writes, duplicate locals, clock sampling,
metadata/idempotence, and independent provider packages. The real cross-module example
combines Infra and Crossplane through public APIs. Infra replays its semantic corpus with
reversed rules and local arguments. OpenShift and Crossplane test independently selected
rules as well as the existing full scenarios.

Run the root and **all separate example modules**:

```sh
test -z "$(gofmt -l .)"
go test -race ./...
go vet ./...
for module in examples/*; do
    if [ -f "$module/go.mod" ]; then
        go -C "$module" test -race ./... || exit 1
        go -C "$module" vet ./... || exit 1
    fi
done
```

Configuration validation and pure evaluation cannot establish source truth, current
freshness, or compatibility of arbitrary providers' reason vocabularies and polarities.
Consumers must select coherent policies and adapters. No automatic profile merging,
output override priority, public registry, production-scale performance guarantee,
new product Condition semantics, or Modelplane integration is part of this design.
