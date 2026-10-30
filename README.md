# Conditions

Kubernetes Condition aggregation and diagnostic promotion for Go.

Controllers often need to combine their own Conditions with those of dependent resources. This package provides a shared evaluator with explicit rules for selecting observations, interpreting status and reasons, choosing an aggregate result, and presenting its causes.

The caller supplies existing resource objects and already-computed local Conditions. Resource selection, reconciliation, and status persistence stay with the caller.

## Usage

The module is `github.com/sttts/conditions` and currently requires Go 1.25 or newer.

```sh
go get github.com/sttts/conditions
```

The main entry point applies Conditions to the supplied parent in memory:

```go
changed, err := conditions.Aggregate(parent, children, rules, localConditions...)
```

- `parent` implements `conditions.Conditioned`: standard metadata getters, `GetConditions() []metav1.Condition`, and `SetConditions([]metav1.Condition) bool`.
- `children` is a `map[string]conditions.Object` of resources selected by the caller. Map keys identify sources in diagnostics.
- `rules` is a `conditions.Rules` value describing the outputs and their policies.
- `localConditions` contains the caller's local `metav1.Condition` assertions.

The evaluator merges results with existing Conditions, then calls the native setter with the complete desired slice. The setter reports whether the stored Conditions changed; `Aggregate` returns that `bool` directly. Setters must accept identical inputs and report `false` for a no-op. Evaluation errors leave the setter uncalled. The package performs no API-server writes.

### A controller boundary

Suppose the controller has already read an application object (`parent`) and its database dependency (`database`). Its local checks pass, while the database reports `Ready=False`, reason `Provisioning`, and message `waiting for endpoint`.

The following is the controller's aggregation code. It defines one parent output and supplies the existing objects to the library:

```go
func updateReadiness(
	parent conditions.Conditioned,
	database conditions.Object,
	localReady metav1.Condition,
) (bool, error) {
	ready := conditions.ConditionType{Type: "Ready", Polarity: conditions.TrueIsGood}

	// Application policy: select child Ready and require all selected children to be ready.
	policy := readiness{Condition: ready}
	rules := conditions.Rules{Outputs: []conditions.Aggregation{{
		Output:    ready,
		Parent:    conditions.LocalBaseFirst{Base: ready},
		Children:  policy,
		Reduction: policy,
		Messages:  conditions.MultilineMessages{MaxLines: 7},
	}}}

	// The controller supplies existing objects and its already-computed local Ready assertion.
	return conditions.Aggregate(parent,
		map[string]conditions.Object{"database": database},
		rules,
		localReady,
	)
}
```

`readiness` is **application code**, not a library type. This partial example omits its implementation and the resource accessors; both are included in the [complete executable example](example_test.go). The policy selects each child's Ready Condition, treats a missing Ready as Unknown, and combines the selected observations with known bad taking precedence over Unknown and good. It chooses the output reasons and diagnostic summary.

The library supplies `LocalBaseFirst`, polarity normalization, deterministic child ordering, output metadata, message formatting, and the final setter call. Here `LocalBaseFirst` preserves a bad local Ready assertion; when the local assertion is good, child readiness participates. The local Ready argument supplies the base for this output on each call.

For the already-computed local result:

```go
localReady := metav1.Condition{
    Type: "Ready", Status: metav1.ConditionTrue, Reason: "LocalChecksPassed",
}
changed, err := updateReadiness(parent, database, localReady)
```

The complete example prints:

```text
changed=true, Ready=False, reason=ChildrenNotReady
Required children are not ready:
  database: waiting for endpoint
same observations: changed=false
```

The parent now contains the derived Ready Condition. Its other Condition types are retained, and the output's observed generation comes from the parent. The controller handles `err` and persists the parent's status when `changed` is true. Repeating the call with the same observations reports no change. Discovery and persistence are outside the library.

### Pure evaluation

For a pure evaluation, use the same arguments with `Evaluate`:

```go
desiredConditions, err := conditions.Evaluate(parent, children, rules, localConditions...)
```

Resources with another Condition representation can use an adapter. The examples include adapters for native OpenShift and Crossplane types.

### Setters without a change flag

For objects with `SetConditions([]metav1.Condition)` returning nothing, such as Cluster API resources, wrap the parent:

```go
changed, err := conditions.Aggregate(
    conditions.WithChangeDetection(parent),
    children, rules, localConditions...,
)
```

`WithChangeDetection` copies the current Conditions before invoking the wrapped setter and compares them with the stored Conditions afterward using Kubernetes semantic equality. This handles setters that update a slice in place or normalize their input. It tracks changes to Conditions only. Native boolean setters provide their own change detection.

## Rules and semantics

`Rules.Outputs` explicitly selects output aggregations for one boundary from children to parent. Each aggregation composes selectors, local precedence, reduction, and message formatting through Go interfaces. Rules are ordinary Go data; custom policies are ordinary Go implementations. Output-list order is not evaluation order.

### Choose rules from independent packages

Providers export `Aggregation` values, constructors, or `[]Aggregation` bundles. Callers select only what they need; there is no registration or central profile extension. For example, combine Infra's **local reconciliation** policy with Crossplane's **child readiness** policy:

```go
// c = github.com/sttts/conditions
// lifecycle = github.com/sttts/conditions/examples/infra
// crossplane = github.com/sttts/conditions/examples/crossplane
rules, err := c.Compose([]c.Aggregation{
    lifecycle.InfraSynced,
    crossplane.ReadinessRule(),
})
if err != nil {
    return false, err
}
return c.Aggregate(parent, children, rules, localSynced)
```

This exact controller boundary and a recovery/idempotence test run in the [cross-module example](examples/crossplane/compose_external_test.go). No unselected Infra axes are enabled. A third-party package supplies the same public Go values and interfaces; it does not need to modify the core. [Two independent provider fixtures](provider_contract_test.go) also exercise selection without importing one another.

`Compose(groups ...[]Aggregation)` returns validated `Rules`. It allocates its own output slice; embedded policy values/configuration still follow ordinary Go shallow-copy semantics and should be treated as immutable. Set `rules.Now` on the assembled rules to inject a clock. Direct `Rules{Outputs: ...}` construction remains supported and is validated on every evaluation.

Composition has explicit contracts:

- **One owner per type**, including derived outputs. Duplicate definitions, missing providers, dependency cycles, incomplete rules, and mismatched defaults fail before evaluation. There is no silent override order.
- **Explicit current-result reads.** `Aggregation.Requires` names selected primary or derived Condition types. Each rule sees previous persisted Conditions, supplied unowned local checks, and only its named dependencies' newly computed values. Unrelated new outputs and transitive dependencies do not leak into its view.
- **Explicit derivation ownership.** `Derivation.Outputs()` reserves its types. Local arguments cannot overwrite them. Undeclared/repeated derived writes fail without calling the setter. Sibling derivations do not see one another's new results.
- **Explicit activation.** An output runs only with a supplied local base or declared default. A dependency whose provider abstains or has no base leaves the previous value readable (or missing/Unknown); dependencies do not assert freshness or success.
- **Local assertions are unambiguous.** Duplicate local types are errors. Unowned local Conditions are copied, not interpreted by a profile-wide fallback. Existing unowned parent Conditions remain intact.

Policies remain ordinary read-only Go implementations, not sandboxed code. They must be pure and deterministic; shared rules/objects must not be mutated concurrently. Single-output `Aggregation.Evaluate` remains a low-level primitive: its caller supplies the view/sequencing and it does not run the composition plan or derivations.

### Migrating the experimental API

`Rules.LocalFirst`, `UnmappedLocal`, `IgnoreLocal`, and the root `LocalEvaluator` interface are removed. Local precedence is each rule's policy; current-output ordering uses `Requires`; ignored derived inputs use declared ownership. To use Infra's local-only prefix policy for another type, select `lifecycle.LocalChecks("Custom")` explicitly instead of installing a catch-all evaluator. `InfraNonDegraded` requires the selected availability/initialization provider; `InfraUpToDate` requires initialization. Missing selections are errors, not automatically enabled policies.

Existing Condition entries retain their slice order; new entries have deterministic ordering, not the old local-first insertion order. Condition identity is its type. The Infra semantic/message/metadata fixtures and repeated-call change assertions remain exercised.

The evaluator keeps these concepts separate:

| Concept | Purpose |
| --- | --- |
| Assessment | Normalize an observation to `Good`, `Bad`, or `Unknown`. |
| Polarity | Declare whether True or False means good, independently for inputs and outputs. |
| Escalation | Distinguish ordinary bad state, `WaitingForHuman`, and `Failed` according to configured reason classification. |
| Propagation and reduction | Decide which observations participate and which contribution takes precedence. |
| Presentation | Render causes without letting diagnostic text determine semantic classification. |

For example, the same normalized bad result can be encoded as `Ready=False` or `Degraded=True`. A sync Condition has no special meaning unless a policy assigns it one. Failure escalation does not itself impose a terminal lifecycle.

One call evaluates one resource boundary. Passing its resulting Conditions into another call composes multiple levels. The package does not discover or traverse an ownership hierarchy.

See [DESIGN.md](DESIGN.md) for the full API, worked configurations, composition tests, and open decisions.

## Message formats

An aggregation selects its message format through the `Formatter` interface:

```go
type Formatter interface {
    Format(summary string, causes []Cause) string
    Inline(condition metav1.Condition) string
}
```

Set `rule.Messages` to a format implementation. Applications can provide another implementation, as the OpenShift operator example does for its native union messages.

| Format | Use when | Behavior |
| --- | --- | --- |
| `MultilineMessages` | Source messages use the colon-and-indentation format. | Retains nested causes and condenses deepest branches to fit the line budget. |
| `BoundedMessages` | Source messages are opaque text, such as foreign diagnostics or logs. | Preserves source text without parsing its apparent structure and reports omitted detail lines. |

Both examples below use the same summary and cause:

```go
summary := "service unavailable"
causes := []conditions.Cause{{
    Reference: "Worker",
    Message:   "bootstrap failed:\n  Job/init: image pull failed",
}}
```

The output blocks show Condition message text. Format selection and budgets do not change the Condition's status, reason, or escalation.

### MultilineMessages

```go
multiline := conditions.MultilineMessages{MaxLines: 7, InlineChars: 40}
rule.Messages = multiline
multilineMessage := multiline.Format(summary, causes)
```

The cause's message is interpreted in this explicitly selected format. Promotion retains the nested `Job/init` cause, with two spaces per level:

```text
service unavailable:
  Worker: bootstrap failed:
    Job/init: image pull failed
```

With `MaxLines: 2`, the deepest branch becomes a chain while the parent summary remains on its own line:

```text
service unavailable:
  Worker: bootstrap failed: Job/init: image pull failed
```

With `MaxLines: 1`, the entire single-child branch becomes one line:

```text
service unavailable: Worker: bootstrap failed: Job/init: image pull failed
```

For a separate UI or log summary, call `multiline.Inline(metav1.Condition{Message: multilineMessage})`. With `InlineChars: 40`, it retains the last fitting part of the chain:

```text
service unavailable: Worker
```

`MaxLines` includes the summary and defaults to seven. Condensation visits deepest branches first; a branching cause can retain its summary while its children are omitted. `InlineChars` defaults to 80 and limits added detail; negative values allow unlimited detail. The inline view preserves an existing single-line summary even when it exceeds that target budget. Formatted messages are capped at 32,768 bytes, with UTF-8-safe truncation and a trailing `...` when the byte cap is reached.

### BoundedMessages

```go
bounded := conditions.BoundedMessages{MaxLines: 7, InlineChars: 40}
rule.Messages = bounded
boundedMessage := bounded.Format(summary, causes)
```

The source is opaque. Its existing indentation is retained inside the outer details block; the apparent `Job/init` heading is not decoded into a child node:

```text
service unavailable:
  Worker: bootstrap failed:
      Job/init: image pull failed
```

With `MaxLines: 2`, the budget retains the summary and an omission notice. Both detail lines are counted as omitted:

```text
service unavailable:
  … 2 detail lines omitted
```

With `MaxLines: 1`, the notice joins the summary:

```text
service unavailable (2 detail lines omitted)
```

`bounded.Inline(metav1.Condition{Message: boundedMessage})` uses only the first line, subject to the configured hard character limit:

```text
service unavailable:
```

`MaxLines` includes the summary and defaults to seven. `MaxBytes` defaults to 32,768; byte truncation appends `...` at a UTF-8 boundary. `InlineChars` defaults to 80; an overlong first line is shortened with `…`. The inline view leaves the stored detailed message untouched.

See [FORMATS.md](FORMATS.md) for the complete syntax, promotion rules, and input-format assumptions, and [multiline_test.go](multiline_test.go) for executable format examples.

## Examples

Each example contains rules, executable calls, and tests. The framework examples also include representation adapters and status YAML.

| Example | What it demonstrates |
| --- | --- |
| [Lifecycle](examples/infra) | Initialization-aware availability, degradation, updates, deletion, escalation, and multiline diagnostics, with conformance fixtures. |
| [OpenShift operator](examples/openshift) | Controller assertions in **operator status**, suffix unions, independent Available/Progressing/Degraded/Upgradeable outputs, and native `ClusterOperator` Conditions. |
| [Crossplane](examples/crossplane) | Native Crossplane Conditions, child readiness, and independent local reconciliation status. |

All examples consume the same root package. Framework API dependencies stay in separate example modules; the core depends on the Go standard library and Kubernetes API machinery.

## Project status

The package is experimental and its API is under review. The current implementation includes the shared evaluator and selectable multiline and opaque message formats. Explicit provenance and escaping for mixed-format promotion, reusable validity/freshness policies, and large-fan-out performance validation remain design work.

## Development

Run the core tests from the repository root:

```sh
go test -race ./...
```

Examples are separate modules, so test them individually or run all of them:

```sh
for module in examples/*; do
    if [ -f "$module/go.mod" ]; then
        go -C "$module" test -race ./... || exit 1
    fi
done
```

[GitHub Actions](https://github.com/sttts/conditions/actions/workflows/test.yml) checks formatting and runs the core and every example suite with the race detector.

## License

Licensed under [Apache-2.0](LICENSE).
