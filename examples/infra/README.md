# Lifecycle profile and conformance tests

This example accompanies [DESIGN.md](../../DESIGN.md). Its [flat rules and lifecycle policies](infra.go) exercise initialization, availability, degradation, updates, deletion, and escalation through the shared root `conditions` package. The optional profile facade only injects rules into generic `Aggregate`. [Type aliases](conditions_aliases.go) retain the conformance tests' local names for the shared API types.

## Condition setup

### Boundary and inputs

One call combines existing parent Conditions, independently computed local assertions, and Conditions of the children supplied by the controller. Objects expose `GetConditions`, a boolean-returning `SetConditions`, and standard metadata. This profile consumes `metav1.Condition` directly.

| Input | Where it comes from | How the profile uses it |
| --- | --- | --- |
| Local Available, NonDegraded, UpToDate, and Synced | The `Aggregate` call's local arguments | Bases for the corresponding outputs; a bad local base takes precedence. |
| Local checks such as AvailableHealth or SyncedConfig | Existing parent Conditions | Bad entries under an output's type prefix contribute local diagnostics. The exact base type is excluded from prefix selection. |
| Parent Initialized | Existing parent Conditions, then the availability rule's derivation | A persistent bring-up latch; once True, it stays True. Supplying it as a local argument does not replace the derivation. |
| Child Available, Initialized, NonDegraded, UpToDate, and Synced | Each supplied child's Conditions | Serving-state diagnoses, with separate propagation gates for each output. |
| Child Running, Succeeded, and Failed | Already-computed job-like assertions | Select pending/running, success, or failure behavior for setup/teardown work. |
| Deletion timestamps and child Deleting | Supplied object metadata and Conditions | Enable the deletion path and distinguish existing children from teardown work. |

Available, Initialized, NonDegraded, UpToDate, Synced, and Succeeded use `TrueIsGood`. Running, Failed, and Deleting use `FalseIsGood`: an active job, a reported job failure, or ongoing deletion can contribute an adverse result. Job completion still depends on the job-like policy and its Succeeded assertion.

### Rule data and order

The [profile definitions](infra.go) declare the vocabulary, classifiers, propagation gates, and output aggregations as Go data. In the snippets below, `c` imports the root library, `metav1` imports API machinery's metadata package, and `lifecycle` is an import alias for this example module.

The complete setup supplied to the generic evaluator is:

```go
rules := lifecycle.InfraRules
```

Its output list is Available, NonDegraded, UpToDate, Synced, then Deleting. `LocalFirst` also evaluates supplied local-only bases and bases whose policy excludes child input during the local pass. Availability explicitly derives Initialized before the later deferred outputs run. There is no resource discovery or ownership traversal in this rule data.

| Output | Local and child relationship |
| --- | --- |
| Available | Preserves a bad local base and includes bad Available-prefixed checks. During bring-up, child diagnoses can block availability. After parent initialization, availability failures propagate from initialized children, while failures of newly added, uninitialized children are suppressed. Ordinary sync errors propagate only before parent initialization; human-wait sync cases have explicit initialization-dependent exceptions. |
| Initialized | Derived after availability: becomes True once Available is good and then remains True. Before that, it reports NotYetAvailable or Deleting. It is not a separate child roll-up. |
| NonDegraded | Preserves a bad local base, evaluates NonDegraded-prefixed checks, and reports eligible child failure, human-wait, sync, availability, or degradation diagnoses. A good local base is additionally gated by parent availability; unavailable/unknown availability has explicit handling. |
| UpToDate | Preserves a bad local base and evaluates UpToDate-prefixed checks. A good base is gated by initialization and deletion, then includes ordinary update, sync, human-wait, and job-progress diagnoses. It does not propagate failure escalation through the general failure gate. |
| Synced | Local-only base and Synced-prefixed checks. Child Synced is not copied into the parent's reconciliation result, although other outputs can use child sync diagnoses. |
| Deleting | Runs from its declared default base, but emits a result only while the parent has a deletion timestamp. Deleting children take precedence over teardown-job diagnoses. This negative-polarity output uses True for ongoing deletion. |

The caller supplies each positive output's local base when it should be evaluated; the profile does not default a missing local assertion to success. Deleting has the explicit default needed for metadata-driven evaluation. Other Condition types can use the profile's local-only prefix evaluator. Existing unowned Conditions are retained.

For example, the classification used by these rules is equivalent to:

```go
classification := c.ReasonClassifier{Suffixes: []c.ReasonSuffix{
    {Suffix: "Failed", Escalation: c.Failed},
    {Suffix: "WaitingForHuman", Escalation: c.WaitingForHuman},
}}
```

The Available entry uses that classification and the two initialization gates explicitly:

```go
v := lifecycle.InfraConditions
existing := c.ExistingChildren{Parent: v.Initialized, Child: v.Initialized}
beforeInit := c.BeforeInitialized{Parent: v.Initialized}
availability := c.Aggregation{
    Output:  v.Available,
    Derived: []c.Derivation{lifecycle.InfraInitialization{}},
    Parent: c.LocalBaseFirst{
        Base: v.Available, Classifier: classification, OverrideOnDeletion: true,
    },
    Local: c.PrefixedChecks{
        Prefix: v.Available.Type, Polarity: c.TrueIsGood, Classifier: classification,
    },
    Children: lifecycle.ServingChildren{
        Inputs: v, Condition: v.Available, Classifier: classification,
        False: existing, Failure: existing, Human: c.Never{}, Unknown: beforeInit,
        SyncError: beforeInit, JobRunning: beforeInit, JobFailed: beforeInit,
        BadDescription: "is not available",
    },
    Reduction: lifecycle.ParentChildReduction{
        Inputs: v, Output: v.Available, Classifier: classification,
        BadSuffix: "NotAvailable", BadDescription: "unavailable children",
    },
    Messages:   c.MultilineMessages{MaxLines: 7, InlineChars: 80},
    Generation: c.PreserveBaseGeneration,
}
```

Classification applies to bad observations. Local failed checks outrank selected child failures; those failures outrank human waits, followed by ordinary sync/bad contributions according to the output's reducer. This is a configured, origin-aware order. Selected unknown child observations can also block a positive output; the profile does not use a universal worst-status reducer. A reason such as FailedChecks does not match the `Failed` suffix. Failure escalation does not create a permanent failure latch; replacing supplied observations can recover the outputs.

The profile selects the root `MultilineMessages` format. Its explicit `PreserveBaseGeneration` metadata policy retains a copied local base's observed generation and uses zero for newly aggregated outputs. That choice differs from the library's default parent-generation policy and does not perform a freshness check.

### Invocation and worked scenario

The [executable invocation](infra_test.go) starts with an initialized parent and a good AvailableHealth check. A newly added child is not initialized, reports Available=False/DependencyFailed, Synced=False/ProviderError, and UpToDate=False/Updating. The controller's own bases are good:

```go
changed, err := c.Aggregate(parent,
    map[string]c.Object{"Worker": worker},
    lifecycle.InfraRules,
    metav1.Condition{Type: "Synced", Status: metav1.ConditionTrue, Reason: "Synced"},
    metav1.Condition{Type: "Available", Status: metav1.ConditionTrue, Reason: "Available"},
    metav1.Condition{Type: "NonDegraded", Status: metav1.ConditionTrue, Reason: "NonDegraded"},
    metav1.Condition{Type: "UpToDate", Status: metav1.ConditionTrue, Reason: "UpToDate"},
)
```

The resulting setup behaves as follows:

| Parent Condition | Result | Why |
| --- | --- | --- |
| Available | True / Available | The initialized parent excludes the new, uninitialized child's availability failure. |
| Initialized | True / Initialized | The existing latch remains True. |
| NonDegraded | False / WorkerFailed | This output admits the child's failed availability diagnosis. |
| UpToDate | False / WorkerNotSynced | The selected ordinary sync error precedes the child's update diagnosis here. |
| Synced | True / Synced | The parent's local reconciliation assertion is independent of the child's Synced. |

Worker is the caller's diagnostic alias; it is not a selector for a resource name or kind. Replacing the child assertions with initialized/available/synced/up-to-date observations lets the serving outputs recover, and the example's summary presenter reports phase Ready. Ready is a presentation value here, not another Condition type. The controller owns persistence. The presenter reports unavailability before ordinary degradation/update state; it does not alter the Conditions.

## Tests

Run this module's tests from this directory:

```sh
go test -race ./...
```

The tests include the existing 27 semantic fixtures and 17 summary fixtures, plus generic rules, composition, recovery, output polarity, error atomicity, and change reporting. They also port every case in the reference `aggregate_test.go` and `multiline_test.go`:

| Reference coverage | Cases |
| --- | ---: |
| Aggregation, including metadata, change reporting, and idempotence | 74 |
| Local sub-condition reduction | 16 |
| Literal aggregate messages | 10 |
| Multiline examples A-F | 6 |
| Inline presentation | 11 |
| Total ported reference cases | 117 |

See [reference_tests.json](reference_tests.json) for provenance and file hashes. Test case names and expected behavior remain intact. The aggregate suite separates semantic checks from message checks. Aggregate cases assert all metadata. Local-only cases check type, status, reason, message, and the timestamp supplied to the public evaluator. Runtime object fixtures use standard metadata/getter/setter interfaces and a fixed clock. URL fixtures use `example.invalid` consistently in inputs and expected results.

The profile selects the root package's `MultilineMessages` through `Formatter`. [multiline_test.go](multiline_test.go) checks its hierarchical, condensed, and inline output through the public interface. The implementation and [format contract](../../FORMATS.md) belong to the library. `BoundedMessages` is the separate opaque-text format.

Producer and wrapper/unstructured-accessor suites exercise APIs this example does not implement; they are outside the aggregation/presentation suites. The conformance corpus is self-contained, and none of its imported cases are skipped.

The generic evaluator and message formats are shared with the [OpenShift operator](../openshift) and [Crossplane](../crossplane) examples. Lifecycle policy stays here. Generic freshness/validity, mixed-format provenance and escaping, bounded diagnostic working memory, and scale remain separate work.
