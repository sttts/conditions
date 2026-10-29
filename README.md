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

`Rules.Outputs` is an ordered, flat list of output aggregations for one boundary from children to parent. Each aggregation composes selectors, local precedence, reduction, and message formatting through Go interfaces. Rules are ordinary Go data; custom policies are ordinary Go implementations.

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

## Examples

Each example contains rules, representation adapters, executable calls, status YAML, and tests.

| Example | What it demonstrates |
| --- | --- |
| [OpenShift operator](examples/openshift) | Controller assertions in **operator status**, suffix unions, independent Available/Progressing/Degraded/Upgradeable outputs, and native `ClusterOperator` Conditions. |
| [Crossplane](examples/crossplane) | Native Crossplane Conditions, child readiness, and independent local reconciliation status. |

All examples consume the same root package. Framework API dependencies stay in separate example modules; the core depends on the Go standard library and Kubernetes API machinery.

## Project status

The package is experimental and its API is under review. The current implementation includes the shared evaluator, opaque bounded diagnostics, and profile-specific formatters. A general native diagnostic codec, reusable validity/freshness policies, and large-fan-out performance validation remain design work.

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
