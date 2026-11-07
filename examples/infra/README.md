# Lifecycle profile and conformance tests

This example defines Infra's lifecycle semantics using ordinary Go selectors, reducers,
derivations and formatters. [schema.go](schema.go) declares contracts and derivations together;
[infra.go](infra.go) implements the domain behavior. `Aggregation` and its generated `Schema`
are reusable global values.

## Select and compose outputs

```go
aggregation := c.Define(c.Definition{
    Outputs: []c.OutputDefinition{
        infra.AvailabilityOutput,
        infra.NonDegradedOutput,
        infra.UpToDateOutput,
        infra.SyncedOutput,
        infra.DeletingOutput,
    },
    Children: []c.ChildBinding{c.Child(infra.InputSchema, c.Identity)},
})
changed := c.Aggregate(parent, children, aggregation, localConditions...)
```

Outputs can be selected individually or combined with another provider's values. Required
providers must be included explicitly. Availability owns derived Initialized; NonDegraded
requires Available and Initialized; UpToDate requires Initialized. Missing dependencies,
duplicate ownership and cycles fail at definition time. Current outputs are visible only to
declared dependents. Availability reads the persisted initialization latch before deriving
its next value.

The [cross-module example](../crossplane/compose_external_test.go) combines only SyncedOutput
with Crossplane's ReadyOutput, without activating other Infra axes.

`InputSchema` is the shared project vocabulary, including job-like resources. `Schema` publishes
only this aggregation's outputs, including derived Initialized. Foreign consumers can map
Available into their own Ready without copying lifecycle logic or reason interpretation.

## Input and precedence

The controller supplies existing parent/child objects and local `metav1.Condition` assertions.
No resource discovery or API writes happen inside aggregation.

| Input | Interpretation |
| --- | --- |
| Local Available, NonDegraded, UpToDate, Synced | Bases for their outputs, with explicit local precedence |
| Parent prefix conditions such as AvailableHealth | Independent local checks; the exact base type is excluded |
| Persisted Initialized | Bring-up latch; once True, stays True |
| Child Available, Initialized, NonDegraded, UpToDate, Synced | Serving diagnoses with per-output propagation gates |
| Child Running, Succeeded, Failed | Setup/teardown job behavior |
| Deletion timestamps and Deleting | Lifecycle and teardown handling |

Available, Initialized, NonDegraded, UpToDate, Synced and Succeeded are TrueIsGood. Running,
Failed and Deleting are FalseIsGood. Reason suffixes classify Failed and WaitingForHuman;
boolean status alone does not imply escalation. Recovery remains possible after either.

ServingChildren reads multiple axes and preserves first-diagnosis precedence. ParentChildReduction
keeps local and child origins distinct instead of applying one global severity sort. Deletion
uses its own selector and reduction. These policies are direct Go implementations attached to
the output, not interpreted DSL instructions.

Additional local prefix checks are explicit output values:

```go
custom := infra.LocalChecks("Custom") // conditions.OutputDefinition
```

## Diagnostics and presentation

Outputs use MultilineMessages for the native nested message format. Set an output's Formatter
field to choose another renderer. Formatting affects only diagnostic text.

InfraSummary independently consumes an InlineFormatter to produce a short phase/message view:

```go
summary := infra.InfraSummary{
    Inputs: infra.InfraConditions,
    Classifier: infra.InfraClassification,
    Formatter: c.MultilineMessages{InlineChars: 80},
}
phase, message := summary.Present(parent)
```

Summary presentation does not modify status or select child contributions. The detailed
format contract is in [FORMATS.md](../../FORMATS.md).

## Tests

```sh
go test -race ./...
```

The caller corpus and reference fixtures compare native status, reasons, messages and metadata
with the new definitions, including recovery and repeat calls. Historical rule configurations
and their facade exist only in `legacy_test.go`. The frozen evaluator lives in
`internal/testreference`; production code does not import it. The executable example uses
only Define and Aggregate.
