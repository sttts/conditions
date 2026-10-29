# Conditions

A design for reusable Kubernetes Condition aggregation and structured diagnostic promotion.

Start with [DESIGN.md](DESIGN.md). The [executable Infra example](examples/infra) accompanies the design and is not a production library implementation.

The proposed public entry point is `conditions.Aggregate(parent, children, rules, localConditions...)`, using `metav1.Condition` and ordinary resource accessors.

Licensed under [Apache-2.0](LICENSE).

The [Infra example](examples/infra) includes 117 ported aggregation and presentation reference cases, alongside the original 44 design fixtures. Run `go test -race ./...` from `examples/infra`.
