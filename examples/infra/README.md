# Infra example and conformance tests

This standalone example accompanies [DESIGN.md](../../DESIGN.md). `infra.go` contains the proposed generic API and a flat Infra rule set; `Infra.Aggregate` only injects those rules into generic `Aggregate`. The production root package remains implementation work.

```sh
cd examples/infra
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

See [reference_tests.json](reference_tests.json) for the source revision and file hashes. Test case names and expected behavior remain intact. The original aggregate suite separates semantic checks from message checks; this port retains that separation. Aggregate cases assert all metadata. Local-only cases retain the original type/status/reason/message expectations and additionally assert the timestamp supplied to the public evaluator. Runtime object fixtures use standard metadata/getter/setter interfaces and a fixed clock. A mock internal URL was replaced consistently with an `example.invalid` URL.

`legacy_messages.go` contains the explicitly selected existing Infra formatter. Its hierarchy parsing and inline behavior are compatibility semantics for this example, including the legacy unbounded single-line inline case. The generic `BoundedMessages` implementation remains opaque and bounded. These tests do not settle the proposed new native message grammar or authorize parsing foreign text.

Legacy provider-specific `SetAvailable` and old wrapper/unstructured-accessor suites exercise APIs this example does not implement; they are outside the imported aggregation/presentation suites. The private package is not an import or test dependency. No test cases in the imported suites are skipped.

The example currently keeps the generic evaluator and application implementations in one package for design review. The production layout in `DESIGN.md` will separate the root library from examples using its public API. Generic freshness/validity, native/foreign codec, bounded diagnostic working memory, and scale remain separate work.
