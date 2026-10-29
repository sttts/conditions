# Message formats

`Formatter` defines rendering independently of Condition selection, polarity, escalation, and reduction:

```go
type Formatter interface {
    Format(summary string, causes []Cause) string
    Inline(condition metav1.Condition) string
}
```

The reducer supplies a summary and named causes. The selected formatter produces the Condition message; `Inline` produces a separate concise presentation, using the Condition reason when its message is empty. An aggregation selects a formatter through `Aggregation.Messages`. Applications may implement additional formats through the same interface.

## MultilineMessages

[`MultilineMessages`](multiline.go) is a format provided by the root package. It uses a summary followed by indented causes:

```text
service unavailable:
  Worker: bootstrap failed:
    Job/init: image pull failed
```

The first line is the caller's summary. Each cause contributes its reference label and message. Nested messages in this format retain their hierarchy when promoted again. Expanded output uses two spaces per level. Within detail lines, the first colon separates a label from its summary; lines without a label remain detail text. Blank detail lines are omitted, trailing spaces are removed, and CRLF/CR line endings are normalized.

Use nonempty summaries and single-line reference labels without colons. Colons and indentation carry structure in this format. The formatter receives no Condition assessment or escalation and does not infer either from message text.

### Promotion

The same formatter can render a child and then include that child's message as a cause of its parent:

```go
var format conditions.Formatter = conditions.MultilineMessages{MaxLines: 7}

child := format.Format("bootstrap failed", []conditions.Cause{
    {Reference: "Job/init", Message: "image pull failed"},
})
parent := format.Format("service unavailable", []conditions.Cause{
    {Reference: "Worker", Message: child},
})
```

`parent` contains the three-line message above. The [executable example](multiline_test.go) also demonstrates the inline presentation:

```text
service unavailable: Worker: bootstrap failed: Job/init: image pull failed
```

### Budgets

`MaxLines` includes the summary; nonpositive values use seven lines. If expanded output exceeds the budget, the formatter condenses deepest branches first. A single-child branch becomes a chain. Detail-only branches can collapse with an ellipsis; branching causes retain their summary when their children are condensed. With a one-line budget, a single branch becomes an inline chain, while multiple branches are represented by their source labels or by a summary that already identifies them.

`InlineChars` is a target budget for inline detail: zero uses 80 characters, and negative values allow unlimited detail. The inline renderer stops adding detail at the last fitting segment or source label. It preserves an existing single-line summary even when that summary exceeds the detail budget. This preserves a useful diagnosis rather than enforcing a hard printer-column width.

Formatted Condition messages have a 32,768-byte limit. Byte truncation appends `...` without splitting a UTF-8 character. Line condensation and byte truncation affect the rendered message only; the aggregation result and output reason remain unchanged.

### Input format

Selecting `MultilineMessages` declares that promoted source messages use this colon-and-indentation format. The parser is part of this explicitly selected format; the library does not choose it by inspecting arbitrary message text.

Use `BoundedMessages` when source text must remain opaque. A general mechanism for mixing differently formatted sources and retaining opaque boundaries across repeated promotion still needs explicit source provenance and escaping; it is separate from this format's current homogeneous promotion contract.

## BoundedMessages

[`BoundedMessages`](messages.go) treats every source message as opaque text. It prefixes each cause with its reference and indents continuation lines, without parsing colons or apparent child headings. It enforces a total line budget, reports omitted detail lines, and truncates encoded output at a UTF-8 boundary.

`MaxLines` defaults to seven, `MaxBytes` defaults to 32,768, and `InlineChars` defaults to 80. Its inline view uses the first line and applies a hard character limit. The [format-selection tests](multiline_test.go) verify that the same source text is rendered according to the selected implementation.

## Validation

Generic tests cover promotion, condensation, explicit format selection, line endings, encoded-size limits, and UTF-8. Aggregation tests exercise both formats with positive and negative output polarities and different line budgets, verifying that presentation does not change failure classification.

The lifecycle example's [multiline conformance tests](examples/infra/multiline_test.go) retain all six expanded/condensed message cases and eleven inline cases, calling the public format implementation. The example contains no separate formatter implementation.
