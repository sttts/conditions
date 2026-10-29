# Conditions

A design for reusable Kubernetes Condition aggregation and structured diagnostic promotion.

Start with [DESIGN.md](DESIGN.md). The [executable Infra API model](design/infra) accompanies the design and is not a production library implementation.

The proposed public entry point is `conditions.Aggregate(parent, children, rules, localConditions...)`, using `metav1.Condition` and ordinary resource accessors.

Licensed under [Apache-2.0](LICENSE).
