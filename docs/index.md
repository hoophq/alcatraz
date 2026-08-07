# 🪨 Alcatraz

**PII detection for Go. In-process, dependency-free.**

Emails, credit cards, national IDs: **45 entity types across 12 countries**,
detected with a function call. No service, no network, no models to download.

```go
eng := alcatraz.NewEngine()
for _, hit := range eng.Analyze("email me at jane@example.com", alcatraz.Options{}) {
    fmt.Println(hit.EntityType, hit.Text, hit.Score)
}
// EMAIL_ADDRESS jane@example.com 0.5
```

Most PII analyzers are services you deploy and call over HTTP. Alcatraz is a
library you `go get` and invoke in-process.

> [!WARNING]
> **Experimental, under active development.** Until `v1.0.0` the public API may
> change between releases, including breaking changes. Pin a version and read
> the release notes before upgrading.

## Why Alcatraz

- ✅ **Checksum-verified.** 25 of the 45 recognizers carry a real checksum
  validator: Luhn (credit cards), ISO 7064 mod-97 (IBAN), Verhoeff (Aadhaar),
  the Brazilian mod-11 schemes (CPF, CNPJ, CNH, PIS), and more. A 16-digit
  number that fails Luhn is *dropped*, not flagged.
- 🪶 **Zero dependencies.** The core imports nothing outside the Go standard
  library. Your dependency tree stays as it was.
- ⚡ **In-process.** No sidecar to deploy, no HTTP round-trip, no serialization.
  Detection is a function call on a `string`.
- ⏱️ **Linear-time by construction.** Built on Go's RE2 `regexp`: no
  backtracking, no catastrophic-ReDoS surface. An
  [opt-in module](lookaround.md) adds lookaround, keeping the core clean.
- 🧩 **Extensible.** Every detector implements one interface,
  `analyzer.Recognizer`. Plug in your own patterns today, ML/NER backends
  tomorrow.

> [!NOTE]
> **The core is pattern-based.** The optional **[`alcatraz/ner`](ner.md)**
> module detects the entities that need a statistical model (`PERSON`,
> `LOCATION`, `NRP`, free-text `DATE_TIME`). It runs an ONNX NER model
> in-process, pure Go by default, no cgo. The core stays dependency-free
> whether or not you use it.

## Where to go next

| If you want to… | Read |
|---|---|
| Add the library to a Go program | [Install & quickstart](install.md) |
| Scan files or a diff from your shell | [The CLI](cli.md) |
| Know which entity types exist | [What it detects](entities.md) |
| Understand scores and the pipeline | [How it works](how-it-works.md) · [Context-aware scoring](context-scoring.md) |
| Mask, replace or redact detections | [Anonymize](anonymize.md) |
| Add your own detector | [Custom recognizers](custom-recognizers.md) |
| Detect `PERSON` / `LOCATION` | [Statistical NER](ner.md) |
| Run NER in an air-gapped network | [Offline models](ner-offline.md) |
| Go faster with ORT, XLA or a GPU | [Inference backends](ner-backends.md) |
| Use a PII-specialized GGUF model | [pfilter](pfilter.md) |
| Write `(?<=…)` lookbehind rules | [Lookaround](lookaround.md) |
| Judge whether this fits your problem | [Design & limits](design.md) |

The full API reference lives on
[pkg.go.dev](https://pkg.go.dev/github.com/hoophq/alcatraz).
