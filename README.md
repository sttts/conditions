# Conditions

Composable Kubernetes Condition schemas and aggregation for Go. Requires Go 1.25+.

A provider publishes a **Schema** describing its Conditions and their polarities and reason
classification. A consumer references that schema and translates its observations using a
set of optional Go helpers. `Define` constructs the aggregation once and publishes its output schema for
the next consumer. There is no registry or universal vocabulary of semantic kinds.

```go
// In the dependency's package; Resources may be omitted for a global schema.
var Schema = conditions.Schema{
    Resources: []conditions.ResourceMatch{conditions.G("database.example.io")},
    Conditions: map[string]conditions.Spec{
        "Available": {Polarity: conditions.TrueIsGood},
        "Synced":    {Polarity: conditions.TrueIsGood},
    },
}
```

In the consuming application:

```go
var Aggregation = conditions.Define(conditions.Definition{
    Resources: []conditions.ResourceMatch{conditions.GK("apps.example.io", "Application")},
    Outputs: []conditions.OutputDefinition{
        conditions.All("Ready", conditions.Spec{Polarity: conditions.TrueIsGood}),
        conditions.Local("Synced", conditions.Spec{Polarity: conditions.TrueIsGood}),
    },
    Children: []conditions.ChildBinding{
        conditions.Child(database.Schema, conditions.Classify(
            conditions.Map("Available", "Ready").
                Escalate(conditions.WaitingForHuman, conditions.Reason("InvalidConfiguration")),
        )),
    },
})

// Publish exactly the contract produced by this aggregation.
var Schema = Aggregation.Schema()
```

`All` and `Local` express the common output policies directly:

| Policy | No local assertion | With a local assertion | No child contributions |
| --- | --- | --- | --- |
| `All(name, spec)` | Aggregate child contributions | Bad/Unknown local assertions take precedence; good permits aggregation | Unknown, unless a bad/Unknown local assertion takes precedence |
| `Local(name, spec)` | Create nothing; retain an existing condition | Use the local assertion, preserving its observed generation | Children never contribute |

A missing assertion is not an assertion of Unknown. Both helpers return public
`OutputDefinition` values; the equivalent literals are:

```go
conditions.OutputDefinition{Name: "Ready", Spec: readySpec, Reduction: conditions.AllReducer{}}
conditions.OutputDefinition{Name: "Synced", Spec: syncedSpec, LocalOnly: true}
```

For custom domain policies, use `OutputDefinition` directly or start with `Output(name, spec)`
and supply the appropriate selector/reducer. No additional absence-policy options are needed
for the common cases.

The controller's invocation is fixed:

```go
changed := conditions.Aggregate(parent, children, Aggregation, localConditions...)
// children: map[string]conditions.Object; keys are diagnostic names.

changed = conditions.AggregateSlice(parent, childSlice, Aggregation, localConditions...)
// childSlice: []conditions.Object; diagnostics use stable positional names "0", "1", ...
```

`localConditions` are ordinary `metav1.Condition` assertions, not rule configuration.
Both calls stage the complete result, apply it once through the parent's setter, and return
its change flag. They do not query Kubernetes or persist status. Existing unrelated
Conditions remain intact. Repeated equivalent observations do not update transition times.

The [executable example](define_example_test.go) includes resource adapters and prints:

```text
changed: true
False ConditionsNotSatisfied
Conditions are not satisfied:
  database: waiting for endpoint
same observations: false
```

## Classification and reuse

| DSL | Meaning |
| --- | --- |
| `Classify(Map("Available", "Ready"))` | Translate a foreign schema explicitly. Only mapped sources participate; additions to the provider schema are harmless. |
| `Identity` | Map the intersection of names with equal semantic meaning; each schema supplies its own polarity. |
| `Map(...).When(Status(metav1.ConditionFalse))` | Include only matching observations. |
| `Map(...).Escalate(WaitingForHuman, Reason("InvalidConfiguration"))` | Override escalation for matching **bad** observations. |
| `Local("Synced", spec)` | Require an explicit local assertion; children cannot set it. |
| `All("Ready", spec).Format(MultilineMessages{})` | Select diagnostic formatting independently of semantics. |

A source may map to several targets. Duplicate source/target mappings are rejected: put
reason-specific overrides on one mapping. Overrides are first-match, in declaration order;
without an override, the source schema's classifier applies. Good/Unknown observations
never acquire a failure escalation. `When` controls participation, not output status.
Unmapped conditions do not participate, whether or not the source schema declares them.
For exhaustive integration checks, set `Classification.RequireAll`; then every declared
source must be mapped or explicitly ignored. `Identity` maps names shared with the output
schema and asserts their semantic compatibility.

The `All` helper uses `AllReducer`: Bad dominates Unknown, which dominates Good. No contributing
observations produces Unknown. Among bad observations, Failed dominates WaitingForHuman,
then ordinary bad. A bad or unknown local base takes precedence over children. The generated
reasons are published in `Aggregation.Schema()` so escalation survives another aggregation
level. Custom reducers publish their reason interpretation in the same output
definition through `Spec.Reasons`. A `Classifier` interprets bad-condition reasons;
`Classification` translates child vocabulary into parent outputs.

Within a project, reference the same published schema with `Child(project.Schema, Identity)`.
The aggregation still decides which outputs are local and which consume child contributions.
Identity is an explicit assertion of semantic compatibility; matching names alone do not
establish that compatibility across unrelated providers.

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
types := conditions.Types(scheme) // scheme registration is already complete

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
During aggregation, every candidate GVK must select the same binding (or all be unmatched).
`EvaluateAggregation` returns resolution errors; `Aggregate` panics before the setter runs.
Missing identity is never silently treated as a scope mismatch.

Finish scheme registration before use and do not mutate it concurrently with evaluations.
Neither helpers nor aggregation modify object TypeMeta. For adapters that are not
`runtime.Object`, use `conditions.WithGVK(child, gvk)`. Global-only definitions require no
API identity or scheme. Map keys never influence schema selection.

## Domain-specific aggregation

`Definition`, `OutputDefinition`, `ChildBinding`, `Classification`, `Mapping`, and
`ResourceMatch` are public Go structs. Helpers construct these exact values; no closed
option interface or DSL interpreter sits between Go and the evaluator. For example:

```go
// These declare the same mapping.
mapping := conditions.Map("Available", "Ready").When(conditions.Reason("Serving"))
mapping = conditions.Mapping{
    Source: "Available", Target: "Ready",
    Predicate: func(value metav1.Condition) bool { return value.Reason == "Serving" },
}
```

An output owns its name, `Spec`, and derivation. There is no separate output schema to keep
in sync, and no `.Using(rule)` bridge. `Define` derives the published schema from the outputs.
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

## Validation and extension contracts

`Define` validates programmer configuration and panics on errors; use it in package variables
or during startup. `Aggregate`/`AggregateSlice` return only `bool`: invalid caller input such as
duplicate local assertions also panics **before the setter runs**, never masquerading as
`changed=false`. Missing conditions and Unknown are normal observations. For returned
runtime errors and pure evaluation, use `EvaluateAggregation`.

Definitions snapshot schema maps, scopes, mappings, bases and rule slices. Custom classifiers,
selectors, reducers, derivations, formatters and clock callbacks must be pure/read-only and
safe to share. Their internal mutable state cannot be deep-cloned by the library.
`Schema()` returns a separate map/slice copy. Treat published global definitions as immutable.

There is one public configuration path: `Definition` → `Define` → `Aggregation`.
Compose independent providers by appending their `OutputDefinition` values to `Definition.Outputs`.
The evaluator and its compiled rules are internal. The former public `Rule`, `Rules`, `Compose`,
`Evaluate`, and `ApplyRules` APIs have been removed; use `EvaluateAggregation` for pure evaluation.
The original semantic and message fixtures are checked directly through the public API;
there is no duplicate reference evaluator or parallel legacy configuration.

## Resource adapters

Parents implement `Conditioned`: metadata getters, `GetConditions() []metav1.Condition`, and
`SetConditions([]metav1.Condition) bool`. Children implement the read-only `Object` interface.
For native setters without a change flag, use `conditions.WithChangeDetection(parent)`.
The examples include native Crossplane and OpenShift representation adapters.

## Message formats

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

`MaxLines` includes the summary and defaults to seven. Condensation visits deepest branches first; a branching cause can retain its summary while its children are omitted. `InlineChars` defaults to 80 and limits added detail; negative values allow unlimited detail. The inline view preserves an existing single-line summary even when it exceeds that target budget. Formatted messages are capped at 32,768 bytes, with UTF-8-safe truncation and a trailing `...` when the byte cap is reached.

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

`MaxLines` includes the summary and defaults to seven. `MaxBytes` defaults to 32,768; byte truncation appends `...` at a UTF-8 boundary. `InlineChars` defaults to 80; an overlong first line is shortened with `…`. The inline view leaves the stored detailed message untouched.

See [FORMATS.md](FORMATS.md) for the complete syntax, promotion rules, and input-format assumptions, and [multiline_test.go](multiline_test.go) for executable format examples.

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
