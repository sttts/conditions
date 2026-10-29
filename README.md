# Conditions

A Go experiment in reusable Kubernetes Condition aggregation and diagnostic promotion.

Start with [DESIGN.md](DESIGN.md). The root `conditions` package implements the experimental evaluator shared by all examples. Its API is under review; native message encoding, general validity/freshness policies, and scale validation remain design work.

The public entry point is `conditions.Aggregate(parent, children, rules, localConditions...)`, using `metav1.Condition` and ordinary resource accessors. Rules are flat Go data; application policy and representation adapters stay in the examples.

| Example | What it demonstrates |
| --- | --- |
| [Infra](examples/infra) | Initialization, escalation, deletion, and legacy multiline diagnostics; all 117 reference cases plus the original 44 design fixtures. |
| [OpenShift operator](examples/openshift) | Controller assertions in **operator status**, suffix unions, four independent polarities, and native `ClusterOperator` output. |
| [Crossplane](examples/crossplane) | Native Crossplane Conditions, child readiness, and independent local reconciliation status. |

Each example is a separate Go module consuming the root package. OpenShift and Crossplane API dependencies do not enter the core module.

```sh
go test -race ./...
go -C examples/infra test -race ./...
go -C examples/openshift test -race ./...
go -C examples/crossplane test -race ./...
```

GitHub Actions runs all four modules. The root command alone does not recurse into the example modules.

Licensed under [Apache-2.0](LICENSE).
