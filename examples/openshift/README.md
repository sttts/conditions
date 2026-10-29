# OpenShift operator Conditions

This example aggregates **operator status**, using native OpenShift API types. Several controllers publish assertions such as `ConfigDegraded` and `RouteDegraded` in one operator configuration object's `status.conditions`. The caller supplies that operator to `Aggregate`; the rules produce the four independent Conditions of a `config.openshift.io/v1 ClusterOperator`.

The inputs are the operator's already-computed assertions. This package does not inspect Pods, Deployments, or operand/component Conditions. The negative `Degraded` flag describes the operator's inability to fulfill intent; it is not an aggregation of every operand's readiness.

## Rules and upstream semantics

[`OpenShiftRules`](openshift.go) expresses the suffix-union behavior of library-go's [`UnionCondition` and `UnionClusterCondition`](https://github.com/openshift/library-go/blob/release-4.19/pkg/operator/status/condition.go). The upstream [`StatusSyncer`](https://github.com/openshift/library-go/blob/release-4.19/pkg/operator/status/status_controller.go) uses these unions to project operator status to ClusterOperator status.

| Output | Matching operator Conditions | Good status |
| --- | --- | --- |
| Available | Every type ending in `Available` | True |
| Progressing | Every type ending in `Progressing` | False |
| Degraded | Every type ending in `Degraded` | False |
| Upgradeable | Every type ending in `Upgradeable` | True |

Each output is independent. Missing matching inputs produce `Unknown/NoData`, including for Upgradeable. A known opposite status outranks Unknown. No flag implicitly maps to terminal `Failed`, invalidates another flag, or prevents later recovery.

Bad assertions are sorted by type. Reasons strip the suffix, append the source reason, and join with `::`; message lines retain their complete source Condition type. Duplicate lines are removed within one assertion, not across different assertions. Good unions preserve the input message order. These are native OpenShift presentation rules, implemented by this profile, rather than a new generic native-message codec.

`OperatorUnion.Inertia` optionally delays an adverse union. As upstream, an assertion must be strictly older than its configured duration; once any adverse assertion qualifies, all adverse assertions participate. `OpenShiftRules` leaves inertia disabled. Inertia never changes the producer's supplied Condition. Tests inject a clock and cover the threshold and mixed old/young case.

The example pins `openshift/api` at the release-4.19-compatible revision in [go.mod](go.mod); it does not depend on library-go or a Kubernetes client. [`OperatorCondition`](https://github.com/openshift/api/blob/1ebdd7bba8e6/operator/v1/types.go) and [`ClusterOperatorStatusCondition`](https://github.com/openshift/api/blob/1ebdd7bba8e6/config/v1/types_cluster_operator.go) have no per-condition observed generation. The adapter preserves status-level `observedGeneration` separately. `NoObservedGeneration` avoids manufacturing a field the output schema cannot retain. New output transitions use the union's latest source time when supplied, otherwise the evaluation clock; message-only updates preserve the previous output transition time.

## Invocation

`operator` is an existing `*operatorv1.Console`; `cluster` is an existing `*configv1.ClusterOperator`. The explicit adapter reads its native `OperatorStatus`:

```go
changed, err := c.Aggregate(
    openshift.ClusterOperator{ClusterOperator: cluster},
    map[string]c.Object{
        "Operator": openshift.Operator{
            Object: operator,
            Status: &operator.Status.OperatorStatus,
        },
    },
    openshift.OpenShiftRules,
)
```

Here `c` imports `github.com/sttts/conditions`; `openshift` imports this example module. The [executable example](openshift_test.go) constructs the actual API objects and exercises this call. The caller owns resource selection and status persistence.

## Status example

These are status observations, not instructions to create operand workloads. The operator input can report availability while two of its controllers cannot apply configuration:

```yaml
apiVersion: operator.openshift.io/v1
kind: Console
metadata:
  name: cluster
  generation: 17
spec:
  managementState: Managed
status:
  observedGeneration: 17
  conditions:
    - type: ConsoleAvailable
      status: "True"
      lastTransitionTime: "2026-10-01T12:00:00Z"
      reason: AsExpected
    - type: ConsoleProgressing
      status: "False"
      lastTransitionTime: "2026-10-01T12:00:00Z"
      reason: AsExpected
    - type: ConfigDegraded
      status: "True"
      lastTransitionTime: "2026-10-01T12:00:00Z"
      reason: Invalid
      message: invalid operator configuration
    - type: RouteDegraded
      status: "True"
      lastTransitionTime: "2026-10-01T12:00:00Z"
      reason: UpdateDenied
      message: route update denied
    - type: ConfigUpgradeable
      status: "False"
      lastTransitionTime: "2026-10-01T12:00:00Z"
      reason: Unsupported
      message: unsupported operator configuration
```

The resulting status includes the following Conditions:

```yaml
apiVersion: config.openshift.io/v1
kind: ClusterOperator
metadata:
  name: console
status:
  conditions:
    - type: Available
      status: "True"
      lastTransitionTime: "2026-10-01T12:00:00Z"
      reason: AsExpected
      message: All is well
    - type: Progressing
      status: "False"
      lastTransitionTime: "2026-10-01T12:00:00Z"
      reason: AsExpected
      message: All is well
    - type: Degraded
      status: "True"
      lastTransitionTime: "2026-10-01T12:00:00Z"
      reason: Config_Invalid::Route_UpdateDenied
      message: |-
        ConfigDegraded: invalid operator configuration
        RouteDegraded: route update denied
    - type: Upgradeable
      status: "False"
      lastTransitionTime: "2026-10-01T12:00:00Z"
      reason: Config_Unsupported
      message: 'ConfigUpgradeable: unsupported operator configuration'
```

Other native ClusterOperator status fields and unowned Condition types are retained. This demonstrates the four unions; it does not replace the full StatusSyncer, its management-state handling, versions/relatedObjects logic, or controller-specific inertia configuration.

The output adapter implements `SetConditions([]metav1.Condition) bool`. It compares the converted native Conditions before assignment and reports changes to stored status. Differences in fields absent from the native schema, such as per-condition observed generation, do not produce a change flag.

## Tests

```sh
go test -race ./...
```

The suite covers all four truth tables, missing/unknown inputs, precedence, suffix matching, exact-type matches, multiple assertions in one operator, reason/message ordering, deduplication, inertia, metadata, idempotence, retained status, and recovery. A separate test verifies that raw component Conditions cannot drive operator status through this profile.
