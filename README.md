<div align="center">

# 🪨 Alcatraz

### PII detection for Go. In-process, dependency-free.

Emails, credit cards, national IDs: **51 entity types across 12 countries**,
detected with a function call. No service, no network, no models to download.

[![CI](https://github.com/hoophq/alcatraz/actions/workflows/test.yml/badge.svg)](https://github.com/hoophq/alcatraz/actions/workflows/test.yml)
&nbsp;·&nbsp; [![Go Reference](https://pkg.go.dev/badge/github.com/hoophq/alcatraz.svg)](https://pkg.go.dev/github.com/hoophq/alcatraz)
&nbsp;·&nbsp; [![Docs](https://img.shields.io/badge/docs-hoophq.github.io%2Falcatraz-blue)](https://hoophq.github.io/alcatraz/)
&nbsp;·&nbsp; Go 1.24+ &nbsp;·&nbsp; stdlib only

**[Documentation](https://hoophq.github.io/alcatraz/)** &nbsp;·&nbsp;
[API reference](https://pkg.go.dev/github.com/hoophq/alcatraz) &nbsp;·&nbsp;
[CLI](docs/cli.md) &nbsp;·&nbsp;
[Entity types](docs/entities.md)

</div>

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

---

## Why Alcatraz

- ✅ **Checksum-verified.** 29 of the 52 recognizers carry a real checksum
  validator: Luhn (credit cards), ISO 7064 mod-97 (IBAN), Verhoeff (Aadhaar),
  the Brazilian mod-11 schemes (CPF, CNPJ, CNH, PIS), and more. A 16-digit
  number that fails Luhn is *dropped*, not flagged.
- 🪶 **Zero dependencies.** The core imports nothing outside the Go standard
  library. Your dependency tree stays as it was.
- ⚡ **In-process.** No sidecar to deploy, no HTTP round-trip, no serialization.
  Detection is a function call on a `string`.
- ⏱️ **Linear-time by construction.** Built on Go's RE2 `regexp`: no
  backtracking, no catastrophic-ReDoS surface. An
  [opt-in module](docs/lookaround.md) adds lookaround, keeping the core clean.
- 🧩 **Extensible.** Every detector implements one interface,
  `analyzer.Recognizer`. Plug in your own patterns today, ML/NER backends
  tomorrow.

> [!NOTE]
> **The core is pattern-based.** The optional
> **[`alcatraz/ner`](docs/ner.md)** module detects the entities that need a
> statistical model (`PERSON`, `LOCATION`, `NRP`, free-text `DATE_TIME`). It
> runs an ONNX NER model in-process, pure Go by default, no cgo. The core
> stays dependency-free whether or not you use it.

---

## Install

```bash
go get github.com/hoophq/alcatraz
```

Requires Go 1.24+. The standard library is the only dependency.

```go
// Build an engine with the full built-in recognizer set (English by default).
eng := alcatraz.NewEngine()

results := eng.Analyze(text, alcatraz.Options{
    Entities:       []string{entities.CreditCard}, // optional: restrict types
    Threshold:      ptr(0.4),                      // optional: drop low scores
    AllowList:      []string{"4111111111111111"},  // optional: ignore values
    AllowListRegex: false,                         // treat AllowList as regex
})

for _, r := range results {
    // r.EntityType, r.Start, r.End, r.Score, r.Text, r.RecognizerName
}
```

`Options{}` (the zero value) analyzes with every recognizer and no threshold.
`Result` offsets are byte indices, so `text[r.Start:r.End] == r.Text`.

## The CLI

Same engine, same zero-network scan. Scan files, stdin, or a unified diff;
detected values are always masked in the output.

```bash
brew install hoophq/tap/alcatraz
# or: go install github.com/hoophq/alcatraz/cmd/alcatraz@latest

alcatraz scan secrets.log app.log      # scan files line by line
git diff | alcatraz diff               # scan only the lines a diff adds
pbpaste | alcatraz scan                # scan pasted text from stdin
alcatraz scan -json report.log         # machine-readable output (masked too)
```

Exit codes are grep-style: `0` clean, `1` findings, `2` error. Full flag
reference and Claude Code hook setup: **[docs/cli.md](docs/cli.md)**.

---

## Documentation

| Topic | |
|---|---|
| Install, quickstart, engine reuse | [docs/install.md](docs/install.md) |
| CLI flags, exit codes, hooks | [docs/cli.md](docs/cli.md) |
| The 51 entity types | [docs/entities.md](docs/entities.md) |
| The detection pipeline | [docs/how-it-works.md](docs/how-it-works.md) |
| Why a bare email scores 0.5 | [docs/context-scoring.md](docs/context-scoring.md) |
| Mask, replace, redact | [docs/anonymize.md](docs/anonymize.md) |
| Writing your own recognizer | [docs/custom-recognizers.md](docs/custom-recognizers.md) |
| `PERSON` / `LOCATION` via ONNX NER | [docs/ner.md](docs/ner.md) |
| Air-gapped and pinned model setup | [docs/ner-offline.md](docs/ner-offline.md) |
| ORT, XLA and GPU inference | [docs/ner-backends.md](docs/ner-backends.md) |
| privacy-filter.cpp (GGUF) backend | [docs/pfilter.md](docs/pfilter.md) |
| `(?<=…)` lookbehind rules | [docs/lookaround.md](docs/lookaround.md) |
| What this is and isn't, roadmap | [docs/design.md](docs/design.md) |
| Speed & parity vs. Presidio | [docs/benchmarks.md](docs/benchmarks.md) |
| Running the tests | [docs/testing.md](docs/testing.md) |

Read these with search and navigation at
**[hoophq.github.io/alcatraz](https://hoophq.github.io/alcatraz/)**.
[pkg.go.dev](https://pkg.go.dev/github.com/hoophq/alcatraz) generates the API
reference from the doc comments.

---

## Layout

```
alcatraz.go        Public entry point: NewEngine + re-exported types.
entities/          Canonical entity-type identifier constants.
analyzer/          Framework: Result, dedup, Recognizer, Pattern, Matcher,
                   PatternRecognizer, Registry, Engine, allow list,
                   context-aware scoring (ContextEnhancer), and the NLP seam
                   (NlpEngine, NlpArtifacts, ArtifactRecognizer).
anonymizer/        Mask/replace/redact detected spans (Operator, Config).
recognizers/       The 51 built-in recognizers, checksum helpers, loader.
models/            Pinned model manifests and checksum-verified downloads for
                   the optional NER backends. In the root module, stdlib only,
                   so the CLI fetches models without the model runtime.
lookaround/        Optional, separate module: regexp2-backed Matcher for
                   lookahead/lookbehind in user-configured patterns.
ner/               Optional, separate module: statistical NER (PERSON,
                   LOCATION, NRP, DATE_TIME) via an in-process ONNX model.
pfilter/           Optional, separate module: PII-specialized NER via
                   privacy-filter.cpp (GGUF models, purego FFI, no cgo).
bench/             Separate module: reproducible speed + parity benchmarks
                   against Presidio's Python analyzer (shared corpus, uv).
docs/              Documentation site sources (MkDocs Material).
```

## Contributing

```bash
go test ./...                    # core (incl. the models pin table, no network)
cd ner && go test ./...          # optional modules have their own go.mod
```

Full matrix, live model tests and the docs preview:
[docs/testing.md](docs/testing.md). Roadmap and detailed plan:
[docs/design.md](docs/design.md) and [TODO.md](TODO.md).

---

Built by the team behind [hoop.dev](https://hoop.dev/start?utm_source=alcatraz&utm_medium=github&utm_campaign=att-launch-072026).
