# Statistical NER

Free-text entities (`PERSON`, `LOCATION`, `NRP`, `DATE_TIME`) need a model,
not a regex. The **`alcatraz/ner`** module runs an ONNX token-classification
model in-process via [hugot](https://github.com/knights-analytics/hugot). Like
[`lookaround`](lookaround.md), it is a *separate module*: importing it is the
only way to pull in the model runtime, and the default backend is pure Go with
no cgo and no shared libraries. (For maximum throughput, including GPU, see
[Inference backends](ner-backends.md).)

```bash
go get github.com/hoophq/alcatraz/ner   # requires Go 1.26.5+ (the core needs only 1.24)
```

```go
import "github.com/hoophq/alcatraz/ner"

nlp, err := ner.New(ctx, ner.DefaultConfig()) // downloads the model on first use
if err != nil { ... }
defer nlp.Close()

reg := analyzer.NewRegistry("en")
recognizers.LoadDefaults(reg, "en")   // the 45 pattern recognizers
reg.Add("en", nlp.Recognizer("en"))   // + statistical NER

eng := analyzer.NewEngine(reg, []string{"en"})
eng.SetNlpEngine(nlp) // model runs once per Analyze, shared with all recognizers

results := eng.Analyze("My name is John Smith, email john@example.com", alcatraz.Options{})
// PERSON "John Smith" (model) + EMAIL_ADDRESS "john@example.com" (pattern)
```

> [!TIP]
> The first `ner.New` downloads ~250 MiB. In production, fetch the model as a
> build or deploy step instead: see [Offline models](ner-offline.md).

## Design notes

- **One inference pass per `Analyze` call.** `SetNlpEngine` makes the engine
  run the model once and share the resulting artifacts with every recognizer
  that consumes them (`analyzer.ArtifactRecognizer`). Without it, the NER
  recognizer still works, running its own inference pass.
- **Zero cost when unused.** The pattern-only path never touches the model;
  an inference failure degrades to pattern-only results.
- **Presidio-compatible entity names.** The module maps model labels through
  `ner.Config.LabelMapping` (defaults mirror Presidio: `PER`→`PERSON`,
  `LOC`/`GPE`→`LOCATION`, `NORP`→`NRP`, `DATE`/`TIME`→`DATE_TIME`; it drops
  `ORGANIZATION` and CoNLL `MISC` by default as false-positive prone). Point
  `Config.Model` at any ONNX token-classification export on Hugging Face, or
  `Config.ModelPath` at a local directory.
- **Byte offsets, guaranteed.** The module maps model spans back to byte
  offsets in the original text, so `text[r.Start:r.End] == r.Text` holds for
  NER results too, including multi-byte input.
- **Whole words, guaranteed.** The model classifies subword tokens and can
  tag only part of one: `Luan` comes back as `Lu` + `an`, each with its own
  score, so a threshold can accept one half and leak the other. The module
  grows spans to the word they sit inside and unions same-type spans that
  then touch, so a caller never has to mask half a name.

## Tabular and log output: `Config.Segmentation`

Transformer NER is context-sensitive by construction: a token's label depends
on every other token in the sequence. That sensitivity makes it good at prose
and erratic on machine output. Feed it a 28-column `psql` row and the name
sits among UUIDs, timestamps and JSON that no news-trained model has seen, and
the whole row can come back empty:

```
id      org_id  connection  ...  user_email       user_name  user_email     ...
8aa71f6d-…  8aa73e5c-…  postgres-demo  ...  luan@hoop.dev  Luan  luan@hoop.dev  ...
```

The model finds `PERSON "Luan"` instantly on its own. As one blob, three rows
of it yield **nothing**. Ablating that input shows where the damage comes
from:

| Input | Names found |
|---|---|
| header + 3 rows | 0/3 |
| 3 rows, no header | 2/3 |
| 1 row alone | 1/1 |

A tab-separated header is the worst single thing in there: 28 column names in
a row is a sequence the model has no reading of, and it drags the rows after
it down with it. Cross-row context costs the rest.

The fix is to stop asking the model to read the table as a sentence.
`ner.Config.Segmentation` chooses what counts as one sequence:

| Value | Cuts after | Recall | Cost |
|---|---|---|---|
| `SegmentWhole` (default) | nothing; one sequence per text | 82% | 1.0x |
| `SegmentLines` | `\n`, so each row or log line stands alone | 88% | 1.8x |
| `SegmentFields` | `\n` and `\t`, so each cell stands alone | 94% | 4.3x |

```go
cfg := ner.DefaultConfig()
cfg.Segmentation = ner.SegmentFields // tab-delimited query output
```

Measured on a corpus of generated `psql`/log output plus a real 28-column
capture (286 planted names). Cost is inference calls: finer segmentation means
more, shorter sequences.

The default stays `SegmentWhole` because prose is the common case and finer
segmentation cannot help it. A segment boundary is a hard context boundary, so
an entity is never read across one, and `"Alice Johnson met Bob Miller in
Paris"` scores the same either way. Callers streaming query results or logs
should set `SegmentLines`, and `SegmentFields` when the output is
tab-delimited and recall matters more than throughput. Only tab counts as a
field delimiter: comma and pipe occur inside names and prose, so cutting on
them would fragment the free text this engine mostly sees.

Segmentation composes with windowing rather than replacing it. The module
still splits a segment longer than the token budget into overlapping windows,
so no setting truncates input, and byte offsets survive the extra rebase
(window → segment → text).
