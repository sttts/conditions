# Conditions

Compose Kubernetes Conditions across Go packages and controller boundaries. A database can
publish `Available`; your application can consume it as `Ready`, add a local `Synced`, and
publish its own schema for the next controller. Requires Go 1.25+.

Define the relationships once, usually in global variables. During reconciliation, pass the
parent, observed children, and local condition assertions. The library computes status,
reason, and diagnostic messages; your controller persists the result.

| Concept | Responsibility |
| --- | --- |
| `Schema` / `Spec` | The producer's condition names, polarity, reason interpretation, and resource scopes. |
| `Classification` | The consumer's translation, such as `Available` → `Ready`. |
| `Define(Definition)` | Validate an aggregation and produce its reusable output schema. |
| `All` / `Local` | Common policies, represented as ordinary `OutputDefinition` values. |

[Quick start](#quick-start) · [Choosing an operation](#choosing-an-operation) ·
[Recipes](#recipes) · [Resource scopes](#resource-scopes-and-filtering) ·
[Formatting](#formatting-with-examples) · [Custom Go policies](#custom-go-policies) ·
[Contracts](#contracts-and-errors) · [Development](#development)

## Quick start

This complete program uses a global child schema, so no scheme or TypeMeta is needed.
`WithChangeDetection` adapts a setter that does not return a change flag. If your setter
already returns `bool`, pass the parent directly.

Run it from this repository with `go run ./cmd/quickstart`, or copy the program into a Go
module with `github.com/sttts/conditions` as a dependency.

<!-- example:quickstart -->
```go
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/sttts/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// A dependency publishes its vocabulary. This schema is global for simplicity.
var databaseSchema = conditions.Schema{
	Conditions: map[string]conditions.Spec{
		"Available": {Polarity: conditions.TrueIsGood},
	},
}

// Define once, usually in a package variable.
var aggregation = conditions.Define(conditions.Definition{
	Outputs: []conditions.OutputDefinition{
		conditions.All("Ready", conditions.Spec{Polarity: conditions.TrueIsGood}),
		conditions.Local("Synced", conditions.Spec{Polarity: conditions.TrueIsGood}),
	},
	Children: []conditions.ChildBinding{
		conditions.Child(databaseSchema, conditions.Classify(conditions.Map("Available", "Ready"))),
	},
})

// Publish the result for the next consumer.
var Schema = aggregation.Schema()

// Embed metadata and adapt your resource's condition storage.
type resource struct {
	metav1.ObjectMeta
	Conditions []metav1.Condition
}

func (r *resource) GetConditions() []metav1.Condition       { return r.Conditions }
func (r *resource) SetConditions(values []metav1.Condition) { r.Conditions = values }

func run(out io.Writer) error {
	parent := &resource{ObjectMeta: metav1.ObjectMeta{Generation: 7}}
	database := &resource{Conditions: []metav1.Condition{{
		Type: "Available", Status: metav1.ConditionFalse,
		Reason: "Provisioning", Message: "waiting for endpoint",
	}}}
	children := map[string]conditions.Object{"database": database}
	synced := metav1.Condition{
		Type: "Synced", Status: metav1.ConditionTrue, Reason: "ReconcileSuccess",
		ObservedGeneration: parent.Generation,
	}

	changed, err := conditions.Aggregate(conditions.WithChangeDetection(parent), children, aggregation, synced)
	if err != nil {
		return err
	}
	ready, _ := conditions.Lookup(parent, "Ready")
	fmt.Fprintln(out, "changed:", changed)
	fmt.Fprintln(out, "Ready:", ready.Status, ready.Reason)
	fmt.Fprintln(out, ready.Message)

	changed, err = conditions.Aggregate(conditions.WithChangeDetection(parent), children, aggregation, synced)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "same observations:", changed)
	return nil
}

func main() {
	if err := run(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```
<!-- /example:quickstart -->

Output:

<!-- example:quickstart-output -->
```text
changed: true
Ready: False ConditionsNotSatisfied
Conditions are not satisfied:
  database: waiting for endpoint
same observations: false
```
<!-- /example:quickstart-output -->

The database's `Available=False` becomes the application's `Ready=False`. The local
`Synced=True` says this reconciliation succeeded; it does not override child readiness.
No Kubernetes API calls are made. `Aggregate` updates in-memory conditions; the controller
still needs to persist status. The second identical call reports no change.

## Choosing an operation

| Operation | Result | Changes the parent? |
| --- | --- | --- |
| `Aggregate(parent, children, aggregation, local...)` | `(bool, error)`; the setter's change flag | Once, on success |
| `AggregateSlice(parent, children, aggregation, local...)` | `(bool, error)`; the setter's change flag | Once, on success |
| `Evaluate("Ready", parent, children, aggregation, local...)` | `(*metav1.Condition, error)`; one newly computed output | No |
| `EvaluateAggregation(parent, children, aggregation, local...)` | `([]metav1.Condition, error)`; the complete desired slice | No |

`Aggregate`, `Evaluate`, and `EvaluateAggregation` take `map[string]conditions.Object`.
Keys name children in diagnostics; they do not choose schemas. `AggregateSlice` takes
`[]conditions.Object` and uses positional names `"0"`, `"1"`, … without renumbering nil entries.
`local...` contains ordinary `metav1.Condition` assertions, not configuration.

For a controller that already has a slice:

<!-- example:slice -->
```go
changed, err := conditions.AggregateSlice(parent, childSlice, Aggregation, localConditions...)
if err != nil {
    return err
}
if changed {
    // Persist the parent's status through your controller's client.
}
```
<!-- /example:slice -->

To compute only readiness and decide yourself what to do with it:

<!-- example:evaluate -->
```go
ready, err := conditions.Evaluate("Ready", parent, children, Aggregation, localConditions...)
if err != nil {
    return err
}
if ready != nil {
    // Inspect or store *ready; Evaluate has not modified parent.
}
```
<!-- /example:evaluate -->

`Evaluate` runs the requested output's owner and its transitive `Requires` dependencies.
Derived output names work too; an owner's derivations run together. An unknown name is an
error. A nil result means **no new result**, even if a condition of that type already exists.
`EvaluateAggregation` includes retained conditions in its complete desired slice.

To inspect the whole desired condition slice before applying it:

<!-- example:evaluate-all -->
```go
desired, err := conditions.EvaluateAggregation(parent, children, Aggregation, localConditions...)
if err != nil {
    return err
}
// Evaluation did not mutate parent. Apply only when your controller is ready.
changed := parent.SetConditions(desired)
if changed {
    // Persist the updated status.
}
```
<!-- /example:evaluate-all -->

**Error boundary:** invalid definitions panic during construction. Runtime input errors are
returned before the parent's setter runs. `false, nil` means unchanged; `false, err` means
failure. Missing observations and `Unknown` are ordinary condition states, not errors.

## Recipes

The following snippets are pieces of application configuration; identifiers such as
`database.Schema`, `parent`, and `scheme` refer to your project's packages and objects.
All helpers produce public Go values. You can mix helpers, struct literals, loops, and functions.

### Publish a schema; translate it at the consumer

The dependency describes what it means. It need not know that your application calls health
`Ready` or that another consumer calls it `Healthy`:

```go
// package database
var Schema = conditions.Schema{
    Resources: []conditions.ResourceMatch{conditions.G("database.example.io")},
    Conditions: map[string]conditions.Spec{
        "Available": {Polarity: conditions.TrueIsGood},
        "Degraded":  {Polarity: conditions.FalseIsGood},
        "Synced":    {Polarity: conditions.TrueIsGood},
    },
}
```

The consumer chooses which observations contribute:

```go
conditions.Child(database.Schema, conditions.Classify(
    conditions.Map("Available", "Ready"),
    conditions.Map("Degraded", "Ready"),
))
```

Here `Available=True` and `Degraded=False` are both good. Either bad observation makes an
`All("Ready", ...)` output bad. Polarity comes from the producer's schema; the mapping does
not repeat it. Unmapped `Synced` does not participate.

A source may contribute to multiple outputs:

```go
conditions.Classify(
    conditions.Map("Available", "Ready"),
    conditions.Map("Available", "Healthy"),
)
```

Both target outputs must be declared. Duplicate source/target pairs are rejected. Foreign
schema additions are harmless by default; for exhaustive integration checks use
`Classification{RequireAll: true, Mappings: ...}` and explicitly `Ignore` unused sources.

### Publish the result and compose another level

```go
// package application
var Aggregation = conditions.Define(conditions.Definition{
    Resources: []conditions.ResourceMatch{conditions.GK("apps.example.io", "Application")},
    Outputs: []conditions.OutputDefinition{
        conditions.All("Ready", conditions.Spec{Polarity: conditions.TrueIsGood}),
        conditions.Local("Synced", conditions.Spec{Polarity: conditions.TrueIsGood}),
    },
    Children: []conditions.ChildBinding{
        conditions.Child(database.Schema, conditions.Classify(
            conditions.Map("Available", "Ready"),
        )),
    },
})
var Schema = Aggregation.Schema()
```

A controller above the application references that published contract:

```go
conditions.Child(application.Schema, conditions.Identity)
```

`Identity` maps the intersection of source and output names using each schema's polarity.
Use it for a shared project vocabulary: it asserts compatible meanings, not just equal
spelling. Outputs declared `Local` still ignore children. For an unrelated vocabulary,
classify explicitly, for example `Map("Ready", "Healthy")`.

### Combine child health with local assertions

```go
conditions.All("Ready", conditions.Spec{Polarity: conditions.TrueIsGood})
conditions.Local("Synced", conditions.Spec{Polarity: conditions.TrueIsGood})
```

| Input | `All("Ready", …)` | `Local("Synced", …)` |
| --- | --- | --- |
| No local assertion | Compute from child contributions | Produce nothing; retain an existing condition |
| Good local assertion | Continue with child contributions | Use the assertion |
| Bad or Unknown local assertion | Use it before considering children | Use the assertion |
| No contributions, no overriding local assertion | Produce Unknown | Children never contribute |

A missing local assertion is not an assertion of Unknown. A good local assertion alone
cannot make `All` healthy when no child or local-check contributions exist. `Local`
preserves the assertion's observed generation; standard `All` outputs use the parent's.

For example, a local validation failure can override otherwise healthy children:

```go
invalid := metav1.Condition{
    Type: "Ready", Status: metav1.ConditionFalse,
    Reason: "InvalidConfiguration", Message: "endpoint is required",
}
changed, err := conditions.Aggregate(parent, children, Aggregation, invalid)
```

`All` uses `AllReducer`: Bad dominates Unknown, which dominates Good. Among bad contributions,
Failed dominates WaitingForHuman, then ordinary bad. Its generated reason interpretation is
published in `Aggregation.Schema()`, so escalation survives another aggregation level.

The helpers above are equivalent to these Go literals:

```go
conditions.OutputDefinition{Name: "Ready", Spec: readySpec, Reduction: conditions.AllReducer{}}
conditions.OutputDefinition{Name: "Synced", Spec: syncedSpec, LocalOnly: true}
```

### Interpret reasons and specialize a binding

A producer can publish which bad reasons are terminal:

```go
conditions.Spec{
    Polarity: conditions.TrueIsGood,
    Reasons: conditions.ReasonClassifier{Reasons: map[string]conditions.Escalation{
        "ProvisioningFailed": conditions.Failed,
    }},
}
```

A consumer can additionally recognize when a human needs to act:

```go
conditions.Map("Available", "Ready").
    Escalate(conditions.WaitingForHuman, conditions.Reason("InvalidConfiguration"))
```

Overrides apply only to bad observations, first match wins; otherwise the source schema's
classifier applies. Good and Unknown observations never acquire a failure escalation.
A `Classifier` interprets reasons; `Classification` translates condition names.

To restrict participation, use `When`:

```go
conditions.Map("Available", "Ready").When(conditions.Reason("Serving"))
```

Nonmatching observations are excluded, not made healthy. If nothing contributes, `All`
produces Unknown. Ordinary Go works equally well:

```go
conditions.Mapping{
    Source: "Available", Target: "Ready",
    Predicate: func(value metav1.Condition) bool { return value.Reason == "Serving" },
}
```

## Resource scopes and filtering

Schemas can declare GVK, GV, G, or GK scopes, or be global. The aggregation can restrict a
particular use of a global schema:

```go
conditions.Child(project.Schema, conditions.Identity).
    For(conditions.GK("workers.example.io", "Worker"))
```

| Scope | Matches |
| --- | --- |
| `GVK("example.io", "v1", "Worker")` | One group/version/kind |
| `GV("example.io", "v1")` | Every kind in one group/version |
| `GK("example.io", "Worker")` | Every version of one group/kind |
| `G("example.io")` | Every version/kind in one group |
| `G("")` | The core API group, **not** a wildcard |
| Omitted `Resources`, `ResourceMatch{}`, or `GVK("", "", "")` | Global |

`For` must narrow the published scope. The most specific matching binding wins as a whole;
fallback mappings are not merged. Equal or incomparable overlapping bindings (for example
GV and GK) are rejected at definition time. Children outside all bindings are filtered out.

Scoped matching reads the object's GVK when set. For typed objects with empty TypeMeta,
provide a Kubernetes scheme. The same scheme can construct scopes from object types:

```go
var types = conditions.Types(scheme) // scheme registration is already complete

var Aggregation = conditions.Define(conditions.Definition{
    Scheme: scheme,
    Outputs: []conditions.OutputDefinition{
        conditions.All("Ready", conditions.Spec{Polarity: conditions.TrueIsGood}),
    },
    Children: []conditions.ChildBinding{
        conditions.Child(database.Schema, conditions.Classify(
            conditions.Map("Available", "Ready"),
        )).For(types.GK(&databasev1.Database{})),
    },
})
```

`types.G`, `types.GV`, `types.GK`, and `types.GVK` take `runtime.Object` and return the same
`ResourceMatch` values as the string helpers. `Types(nil)` works for objects with a populated
GVK, including unstructured objects. `ObjectTypes{Scheme: scheme}` is the equivalent Go literal.
There is no GVR, RESTMapper, discovery, or generic resolver API.

A populated GVK takes precedence; an incomplete one is an error. Otherwise the scheme supplies
the registered GVKs. Helpers check ambiguity **after projection**: several versions of one
GroupKind are valid for `GK`, but ambiguous for `GVK`. Helpers panic on configuration errors.
`Define` snapshots scheme registrations for condition-bearing types and checks that every
candidate GVK selects the same binding (or all are unmatched). Ambiguity is a construction
error. `Aggregate` and `AggregateSlice` return `(bool, error)`; runtime resolution errors
return `false, err` before the setter runs.
Missing identity is never silently treated as a scope mismatch.

Finish scheme registration before `Define`. Later registrations do not affect an existing
aggregation; construct a new one to use them. Do not mutate a scheme concurrently with construction.
Neither helpers nor aggregation modify object TypeMeta. For adapters that are not
`runtime.Object`, use `conditions.WithGVK(child, gvk)`. Global-only definitions require no
API identity or scheme. Map keys never influence schema selection.

## Formatting with examples

An aggregation selects its message format through the `Formatter` interface:

```go
type Formatter interface {
    Format(summary string, causes []Cause) string
}
```

Set `OutputDefinition.Formatter`, or use `All(name, spec).Format(formatter)`.
The reducer produces an `Outcome`; `Formatter.Format(outcome.Summary, outcome.Causes)` produces
only the message. Status, reason, escalation, generation, and transition time are independent.
The default is `BoundedMessages{}`. `FormatterFunc` accepts a Go function with this signature.

Short presentation is a separate, optional one-method `InlineFormatter` interface:

```go
type InlineFormatter interface {
    Inline(condition metav1.Condition) string
}
```

Aggregation never calls `Inline`. The built-in formatters implement both interfaces;
custom formatters need only `Format`. `InlineFormatterFunc` accepts a presentation function.
Applications can provide another formatter, as OpenShift does for its native union messages.

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
output.Formatter = multiline
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

`MaxLines` includes the summary and defaults to seven. Use this format when child messages
follow the same structured format. Foreign messages with colons or indentation may be
interpreted as structure; use `BoundedMessages` for opaque diagnostics.

### BoundedMessages

```go
bounded := conditions.BoundedMessages{MaxLines: 7, InlineChars: 40}
output.Formatter = bounded
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

`MaxLines` defaults to seven, `MaxBytes` to 32,768, and `InlineChars` to 80.
The inline view leaves the stored detailed message untouched.

See [FORMATS.md](FORMATS.md) for the complete syntax, promotion rules, and input-format assumptions, and [multiline_test.go](multiline_test.go) for executable format examples.

### Supply a small Go formatter

```go
conditions.All("Ready", conditions.Spec{Polarity: conditions.TrueIsGood}).
    Format(conditions.FormatterFunc(func(summary string, causes []conditions.Cause) string {
        return fmt.Sprintf("%s; causes=%d", summary, len(causes))
    }))
```

For the summary and cause above, this renders `service unavailable; causes=1`.
A custom formatter implements only `Format`; it needs no `Inline` method. This changes
presentation, not reduction: the condition retains the same status and reason.

## Custom Go policies

`Definition`, `OutputDefinition`, `ChildBinding`, `Classification`, `Mapping`, and
`ResourceMatch` are public Go structs. Helpers construct these exact values.

An output owns its name, `Spec`, and derivation. `Define` derives the published schema
from these outputs.
Ordinary loops and functions can assemble a definition or share output values across consumers.
`Definition.Now` supplies an optional clock.

For advanced policies, use `OutputDefinition.Reduction`, `Parent`, `LocalChecks`, `Formatter`, and `Generation` for
custom behavior. `ChildSelectorFunc`, `LocalSelectorFunc`, `ReducerFunc`, and `ClassifierFunc`
accept ordinary Go snippets wherever defining a separate type would add little value:

```go
output := conditions.OutputDefinition{
    Name: "Ready",
    Spec: conditions.Spec{Polarity: conditions.TrueIsGood},
    Children: conditions.ChildSelectorFunc(func(parent, child conditions.Object) []conditions.Contribution {
        if parent.GetDeletionTimestamp() != nil { return nil }
        return selectDatabaseHealth(parent, child) // ordinary project Go code
    }),
    Reduction: databaseReadiness{}, // or conditions.ReducerFunc(...)
}
```

By default, child selection uses the declared schema bindings. An output's custom `Children`
selector instead reads the original objects and owns their interpretation; it still receives
only children admitted by the binding scopes. Use this for domain logic reading several
conditions or parent lifecycle state. `Map(...).Select(selector)` offers the same Go extension
for a particular mapping, with its `When` and escalation overrides applied afterward.
A global `Child(schema, Classify())` can admit objects exclusively for custom selectors.

`Derived` attaches Go derivations to the output that produces them. Each derivation declares
its names and published Specs together; no separate output-contract map is needed:

```go
func (Initialization) Outputs() map[string]conditions.Spec {
    return map[string]conditions.Spec{
        "Initialized": {Polarity: conditions.TrueIsGood},
    }
}
```

`Derive(parent, now)` returns the actual conditions. `Define` snapshots the declared Specs,
adds them to the published schema, and validates exclusive ownership. A derivation may omit
an output in a particular call, but may not write an undeclared type. Dependencies remain
explicit in `Requires`.

For example, an output whose Go selector reads the freshly computed `Ready` declares:

```go
healthy := conditions.All("Healthy", conditions.Spec{Polarity: conditions.TrueIsGood})
healthy.Requires = []string{"Ready"}
healthy.LocalChecks = conditions.LocalSelectorFunc(func(parent conditions.Object) []conditions.Contribution {
    typ := conditions.ConditionType{Type: "Ready", Polarity: conditions.TrueIsGood}
    value := typ.Read(parent)
    return []conditions.Contribution{{
        Condition: value,
        State: conditions.State{Assessment: typ.Assessment(value)},
    }}
})
```

Include both outputs in the same `Definition`. `Requires` makes the current dependency
result visible; unrelated outputs see the shared input snapshot. If the dependency produces
no new result, its persisted value remains visible. It expresses dataflow, not freshness.

Nil `Parent`, `Reduction`, and `Formatter` select the standard behavior. A custom parent policy
owns its missing-base convention; supply `DefaultBase` if it should run without a local base.
`LocalOnly` disables child contributions and, by default, requires a local assertion.

The three example modules expose `Aggregation` and `Schema` globals:

- **Crossplane:** declarative Ready mapping, local Synced, native Go reducer and message policy.
- **OpenShift:** suffix-family input schema, identity classification, native union reducer/messages.
- **Infra:** direct Go lifecycle selectors and reducers, including derived Initialized.

The [cross-module example](examples/crossplane/schema_external_test.go) consumes Infra and
Crossplane schemas, filters by resource identity, maps both to application Ready, then uses
that application's published schema in a further aggregation with Identity.

## Contracts and errors

`Define` validates programmer configuration and panics on errors; use it in package variables
or during startup. `Aggregate`/`AggregateSlice` return `(bool, error)`: invalid caller input such as
duplicate local assertions returns `false, err` **before the setter runs**. On success,
the boolean is the native setter's change flag. Missing conditions and Unknown are normal observations. For returned
runtime errors and pure evaluation, use `Evaluate` for one output or `EvaluateAggregation`
for the complete desired slice.

| Situation | Behavior |
| --- | --- |
| Invalid mapping, duplicate output, missing dependency, or cycle | `Define` panics at construction. |
| Registered GVKs of a Go type route to different bindings | `Define` panics at construction, even if future objects would carry explicit GVKs. |
| Missing or incomplete identity on a concrete child in scoped aggregation | Return an error; do not invoke the setter. |
| Unknown output passed to `Evaluate`, duplicate local assertions, or undeclared derived result | Return an error; do not invoke the setter. |
| Child outside all binding scopes | Filter it out. |
| Declared exact child condition missing | Contribute Unknown, unless its `Spec.Missing` supplies a fallback; a suffix family with no matches contributes nothing. |
| No new result from a local-only output | Keep the old condition in full aggregation; single-output `Evaluate` returns nil. |

On successful aggregation, existing unrelated conditions are preserved. Equivalent inputs
are idempotent; unchanged status retains `LastTransitionTime` even when diagnostics change.

Definitions snapshot schema maps, scopes, mappings, bases and rule slices. Custom classifiers,
selectors, reducers, derivations, formatters and clock callbacks must be pure/read-only and
safe to share. Their internal mutable state cannot be deep-cloned by the library.
`Schema()` returns a separate map/slice copy. Treat published global definitions as immutable.

### Resource adapters

Parents implement `Conditioned`: metadata getters, `GetConditions() []metav1.Condition`, and
`SetConditions([]metav1.Condition) bool`. Children implement the read-only `Object` interface.
For native setters without a change flag, use `conditions.WithChangeDetection(parent)`.
The examples include native Crossplane and OpenShift representation adapters.

## Examples

Each example contains output definitions, executable calls, and tests. The framework examples also include representation adapters and status YAML.

| Example | What it demonstrates |
| --- | --- |
| [Lifecycle](examples/infra) | Initialization-aware availability, degradation, updates, deletion, escalation, and multiline diagnostics, with conformance fixtures. |
| [OpenShift operator](examples/openshift) | Controller assertions in **operator status**, suffix unions, independent Available/Progressing/Degraded/Upgradeable outputs, and native `ClusterOperator` Conditions. |
| [Crossplane](examples/crossplane) | Native Crossplane Conditions, child readiness, and independent local reconciliation status. |

All examples consume the same root package. Framework API dependencies stay in separate example modules; the core depends on the Go standard library and Kubernetes API machinery.

## Project status

The package is experimental and its API is under review. The current implementation includes the shared evaluator and selectable multiline and opaque message formats. Explicit provenance and escaping for mixed-format promotion, reusable validity/freshness policies, and large-fan-out performance validation remain design work.

## Development

The quick-start source and its printed output are checked against this README by
`cmd/quickstart/main_test.go`. The operation snippets are compiled and compared by `readme_operations_test.go`; formatting
output blocks are checked by `readme_test.go`.
Update the executable examples and documentation together when changing these contracts.

Run the core tests (including these documentation checks) from the repository root:

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
