# Crossplane Conditions

This example uses Crossplane's actual `Condition`, `ConditionedStatus`, and constructors from `crossplane-runtime/v2` v2.0.0. It supplies a representation adapter and an explicit example readiness policy to the shared root `conditions.Aggregate` implementation.

## Condition setup

### Boundary and native assertions

| Role | Conditions supplied to the evaluator |
| --- | --- |
| Parent/output | An existing resource's native ConditionedStatus, exposed through Adapter. Ready is the only configured aggregate output. |
| Children | Native child Conditions exposed through Adapter. The selector reads only Ready; missing Ready contributes Unknown. |
| Local Ready | Optional caller assertion of the parent's own readiness. A supplied assertion replaces the rule's default base. |
| Local Synced | Optional caller assertion of the parent's reconciliation result, converted with ToCondition and passed as a local argument. It is stored independently. |

Resource metadata and prior Conditions stay on the existing objects. The caller supplies the relevant child set; the library does not locate composed resources.

Crossplane's [Condition definitions](https://github.com/crossplane/crossplane-runtime/blob/v2.0.0/apis/common/condition.go) distinguish readiness from reconciliation:

| Native Condition | Supplied assertions |
| --- | --- |
| Ready | True/Available; False/Creating, Deleting, or Unavailable |
| Synced | True/ReconcileSuccess; False/ReconcileError or ReconcilePaused |

`ReconcileError` may mean an update failed or an observation failed. It does not universally invalidate a supplied `Ready=True` assertion. `Healthy` is reserved and not implemented in this pinned API version; it is not used as an input here.

### Rule data and precedence

The complete Ready setup is equivalent to the following data. Here `c` imports the root library, `crossplane` imports this example module, and `metav1` imports API machinery's metadata package:

```go
ready := c.ConditionType{Type: "Ready", Polarity: c.TrueIsGood}
rules := c.Rules{Outputs: []c.Aggregation{{
    Output:      ready,
    DefaultBase: &metav1.Condition{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Available"},
    Parent:      c.LocalBaseFirst{Base: ready},
    Children:    crossplane.ReadyChildren{},
    Reduction:   crossplane.Readiness{},
    Messages:    c.BoundedMessages{MaxLines: 7},
}}}
```

Ready uses `TrueIsGood` on both input and output. `ReadyChildren` and `Readiness` are this example's selector and reducer; `LocalBaseFirst` and the formatter come from the library. The good default base is an explicit policy assumption for omitted local Ready, not a readiness observation inferred by the library. It schedules the output even when the call supplies only local Synced. Missing Ready on a supplied child still contributes Unknown. An empty child set leaves the otherwise-good base sufficient.

[`CrossplaneRules`](crossplane.go) selects `ReadinessRule()`. The constructor returns an independently composable `c.Aggregation`, not evaluation-wide settings:

```go
rules, err := c.Compose([]c.Aggregation{crossplane.ReadinessRule()})
```

The [cross-module controller call](compose_external_test.go) additionally selects `lifecycle.InfraSynced` from the separate Infra module, without enabling its other axes. It tests local-vs-child sync independence, unchanged opaque diagnostics, recovery, and idempotence. This example module's additional Infra dependency is only for that external-package composition test; neither the core nor readiness policy imports Infra.

The selected readiness policy is:

- Aggregate child Ready assertions into parent Ready. Known bad outranks Unknown; all supplied children must be good for a good result. No children means that an otherwise-good local base is sufficient.
- Preserve a supplied local bad or unknown Ready base. Otherwise use the declared good default and the child results.
- Publish `Available` for success, `Creating` while a child is not ready, and the explicitly configured `ReadinessUnknown` for missing/unknown readiness. The diagnostic summary identifies this as waiting for composed resources.
- Preserve local Synced independently. A child's Synced is not promoted into the parent's reconciliation status; Synced is optional and is not an automatic validity gate.
- Do not infer `Failed` or `WaitingForHuman` escalation from Creating, Deleting, Unavailable, ReconcileError, or ReconcilePaused.

This is a chosen aggregation policy over native Crossplane assertions, not a claim to implement every Crossplane composition controller, readiness-check mode, function result, or claim propagation rule. The local Ready base remains a caller assertion; callers select an appropriate base for their resource lifecycle.

| Effective input | Parent output |
| --- | --- |
| Supplied local Ready is False or Unknown | Preserve that local status, reason, and message; child readiness does not replace it. |
| Good local/default base and any child Ready=False | Ready=False/Creating, with diagnostics for the not-ready children. |
| Good base, no false children, and missing/Unknown child Ready | Ready=Unknown/ReadinessUnknown. |
| Good base and all selected children ready | Ready=True/Available. |
| Supplied local Synced | Preserve it as the parent's local Synced; the child's Synced never substitutes for it. |

There is no Synced output aggregation or sync validity gate in this setup. Omitting local Synced creates no new Synced assertion; an existing one remains unless the caller replaces it. All child readiness contributions have no failure/human-wait escalation. Derived Ready uses the parent's generation, while supplied local Synced retains its own observed-generation value.

## Adapter and invocation

Crossplane has a variadic `SetConditions(...Condition)` and a single-type `GetCondition` accessor. Its native Condition type differs from `metav1.Condition`. `Adapter` converts type, status, reason, message, transition time, and observed generation without loss. Its slice setter returns `bool` by comparing the converted native Conditions with stored status before assignment. The core merges Conditions and computes transition times; the adapter reports changes to the actual native representation. Other Condition types are preserved by the core.

`Resource` is a small example CRD fixture embedding standard ObjectMeta and the real Crossplane `ConditionedStatus`, not a new input bundle required by the library. A consuming controller can bind the same adapter logic to its actual resource type.

```go
changed, err := c.Aggregate(
    crossplane.Adapter{Resource: parent},
    map[string]c.Object{
        "database": crossplane.Adapter{Resource: database},
    },
    crossplane.CrossplaneRules,
    crossplane.ToCondition(xpv1.ReconcileSuccess()),
)
```

Here `c` imports `github.com/sttts/conditions`, `crossplane` imports this example module, and `xpv1` imports `github.com/crossplane/crossplane-runtime/v2/apis/common/v1`. The [executable example](crossplane_test.go) constructs the native assertions. Resource discovery and persistence stay with the caller.

## Status example

A child resource's observed status can contain both of these assertions:

```yaml
status:
  conditions:
    - type: Ready
      status: "True"
      reason: Available
      observedGeneration: 5
    - type: Synced
      status: "False"
      reason: ReconcileError
      message: external API rejected an update
      observedGeneration: 5
```

With that child and a separately supplied local `ReconcileSuccess`, the example parent at generation 7 publishes:

```yaml
status:
  conditions:
    - type: Synced
      status: "True"
      reason: ReconcileSuccess
    - type: Ready
      status: "True"
      reason: Available
      message: All composed resources are ready
      observedGeneration: 7
```

The local Synced assertion keeps its supplied observed generation, if any. Derived Ready uses the parent's generation. These snippets omit transition timestamps; no field is synthesized from the child's generation or reconciliation result.

If the child instead supplies `Ready=False/Creating` with message `waiting for database`, the parent Ready becomes:

```yaml
type: Ready
status: "False"
reason: Creating
message: |-
  Waiting for composed resources:
    database: waiting for database
observedGeneration: 7
```

Messages are opaque foreign text rendered with the generic seven-line budget. Text resembling a native tree is not parsed. Shortening diagnostics never changes readiness classification or its reason.

## Tests

```sh
go test -race ./...
```

Tests cover native constructors/reasons, missing/unknown inputs, local precedence, bad-over-unknown reduction, optional Synced, ReconcileError/ReconcilePaused independence, automatic recovery from replaced assertions, metadata round trips, idempotence, other Condition ownership, bounded opaque diagnostics, and exclusion of reserved Healthy.
