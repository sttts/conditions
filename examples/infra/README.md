# Lifecycle profile and conformance tests

This example accompanies [DESIGN.md](../../DESIGN.md). Its [flat rules and lifecycle policies](infra.go) exercise initialization, availability, degradation, updates, deletion, and escalation through the shared root `conditions` package. The optional profile facade only injects rules into generic `Aggregate`. [Type aliases](conditions_aliases.go) retain the conformance tests' local names for the shared API types.

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
