<div align="center">

# 🪨 Alcatraz

### PII detection for Go. In-process, dependency-free.

Emails, credit cards, national IDs — **45 entity types across 12 countries** —
detected with a function call. No service, no network, no models to download.

[![CI](https://github.com/hoophq/alcatraz/actions/workflows/test.yml/badge.svg)](https://github.com/hoophq/alcatraz/actions/workflows/test.yml)
&nbsp;·&nbsp; [![Go Reference](https://pkg.go.dev/badge/github.com/hoophq/alcatraz.svg)](https://pkg.go.dev/github.com/hoophq/alcatraz)
&nbsp;·&nbsp; Go 1.24+ &nbsp;·&nbsp; stdlib only

</div>

```go
eng := alcatraz.NewEngine()
for _, hit := range eng.Analyze("email me at jane@example.com", alcatraz.Options{}) {
    fmt.Println(hit.EntityType, hit.Text, hit.Score)
}
// EMAIL_ADDRESS jane@example.com 0.5
```

Where most PII analyzers are services you deploy and call over HTTP, Alcatraz
is a library you `go get` and invoke in-process.

> [!WARNING]
> **Experimental — under active development.** Until `v1.0.0` the public API may
> change between releases, including breaking changes. Pin a version and read
> the release notes before upgrading.

---

## Why Alcatraz

- ✅ **Verified, not just shape-matched.** 25 of the 45 recognizers carry a real
  checksum validator — Luhn (credit cards), ISO 7064 mod-97 (IBAN), Verhoeff
  (Aadhaar), the Brazilian mod-11 schemes (CPF, CNPJ, CNH, PIS), and more. A
  16-digit number that fails Luhn is *dropped*, not flagged.
- 🪶 **Zero dependencies.** The core imports nothing outside the Go standard
  library. Your dependency tree stays exactly as it was.
- ⚡ **In-process.** No sidecar to deploy, no HTTP round-trip, no serialization.
  Detection is a function call on a `string`.
- ⏱️ **Linear-time by construction.** Built on Go's RE2 `regexp` — no
  backtracking, no catastrophic-ReDoS surface. (Need lookaround? There's an
  [opt-in module](#advanced-matching-lookahead--lookbehind) that keeps the core clean.)
- 🧩 **Extensible.** Every detector implements one interface,
  `analyzer.Recognizer` — plug in your own patterns today, ML/NER backends
  tomorrow.

> [!NOTE]
> **The core is pattern-based.** Entities that need a statistical model
> (`PERSON`, `LOCATION`, `NRP`, free-text `DATE_TIME`) are detected by the
> optional **[`alcatraz/ner`](#statistical-ner-optional-module)** module,
> which runs an ONNX NER model in-process — pure Go by default, no cgo. The
> core stays dependency-free whether or not you use it.

---

## Install

```bash
go get github.com/hoophq/alcatraz
```

Requires Go 1.24+. The standard library is the only dependency.

## The CLI

The library also ships as a small command-line scanner — same engine, same
zero-network posture. Scan files, stdin, or a unified diff; detected values
are always masked in the output.

```bash
# Homebrew (macOS & Linux)
brew install hoophq/tap/alcatraz

# or grab a checksum-verified binary from GitHub releases, or build from source
go install github.com/hoophq/alcatraz/cmd/alcatraz@latest
```

```bash
alcatraz scan secrets.log app.log      # scan files line by line
git diff | alcatraz diff               # scan only the lines a diff adds
pbpaste | alcatraz scan                # scan pasted text from stdin
alcatraz scan -json report.log         # machine-readable output (masked too)
```

Exit codes are grep-style: `0` clean, `1` findings, `2` error. Flags:
`-threshold` (default 0.4), `-entities`, `-ignore` (default `DATE_TIME,URL`),
`-allowlist-file` (one allowed value per line, `#` comments), and `-exclude`
(diff mode, glob patterns of paths to skip).

The CLI powers the [Hoop plugin for Claude Code](https://github.com/hoophq/claude-marketplace)'s
`/hoop:pii-scan` command and pairs with
[alcatraz-action](https://github.com/hoophq/alcatraz-action) for CI.

## Quickstart

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

---

## What it detects

45 entity types. ✓ = checksum/format validated, so structured identifiers are
**verified**, not just shape-matched. Constants live in the `entities` package.

| Group | Entity types |
|-------|--------------|
| Generic | `EMAIL_ADDRESS`, `PHONE_NUMBER`, `CREDIT_CARD`✓, `CRYPTO`, `IP_ADDRESS`, `URL`, `DATE_TIME`, `IBAN_CODE`✓ |
| United States | `US_SSN`✓, `US_ITIN`✓, `US_PASSPORT`, `US_DRIVER_LICENSE`, `US_BANK_NUMBER`, `ABA_ROUTING`✓, `MEDICAL_LICENSE` |
| United Kingdom | `UK_NHS`✓, `UK_NINO`✓ |
| Australia | `AU_TFN`✓, `AU_ABN`✓, `AU_ACN`✓, `AU_MEDICARE`✓ |
| India | `IN_AADHAAR`✓, `IN_PAN`, `IN_PASSPORT`, `IN_VEHICLE_REGISTRATION`, `IN_VOTER`, `IN_GSTIN` |
| Italy | `IT_FISCAL_CODE`✓, `IT_VAT_CODE`✓, `IT_IDENTITY_CARD`, `IT_DRIVER_LICENSE`, `IT_PASSPORT` |
| Spain | `ES_NIF`✓, `ES_NIE`✓ |
| Singapore | `SG_FIN`✓, `SG_UEN` |
| Brazil | `BR_CPF`✓, `BR_CNPJ`✓, `BR_RG`, `BR_CNH`✓, `BR_PIS`✓ |
| Other | `PL_PESEL`✓, `KR_RRN`✓, `FI_PERSONAL_IDENTITY_CODE`✓, `TH_TNIN`✓ |

Every built-in detects a **language-independent** structured identifier — an
IBAN or a Thai national ID looks the same in any surrounding text — so the
complete set is active under whichever language an engine is built with. (The
language key exists for language-specific recognizers, such as the `ner`
module's model-backed recognizer.)

---

## How it works

```
text  →  recognizers (regex)  →  validators (checksum)  →  dedup  →  threshold + allow list  →  results
```

The pipeline:

1. Every applicable recognizer runs its regexes over the text.
2. A matched span is scored at the pattern's base confidence; a validator then
   either promotes it to `1.0` (verified) or drops it (failed checksum).
3. Overlapping spans **of the same entity type** are de-duplicated (the
   enclosing/higher-scoring span wins). Different entity types never suppress
   each other.
4. An optional score threshold and allow list are applied.
5. Each surviving result is annotated with the matched substring (`Result.Text`).

---

## Anonymize: mask, replace, redact

Detection gives you spans; the `anonymizer` package turns them into sanitized
text. Pick an operator — mask with the character of your choice (`#`, `*`, …),
keep a recognizable tail, replace with a placeholder, or redact — and apply it
to the results of an `Analyze` call:

```go
import "github.com/hoophq/alcatraz/anonymizer"

text := "Email jane@example.com, card 4532015112830366, ssn 536-90-4399."
results := eng.Analyze(text, alcatraz.Options{})

anonymizer.Anonymize(text, results, anonymizer.Mask('*'))
// Email ****************, card ****************, ssn ***********.

anonymizer.AnonymizeWith(text, results, anonymizer.Config{
    Default: anonymizer.Replace(), // <ENTITY_TYPE> placeholders
    PerEntity: map[string]anonymizer.Operator{
        entities.CreditCard: anonymizer.MaskKeepLast('#', 4),
    },
})
// Email <EMAIL_ADDRESS>, card ############0366, ssn <US_SSN>.
```

Built-in operators: `Mask(char)` (length-preserving, one mask rune per text
rune), `MaskKeepLast(char, n)`, `Replace()`, `ReplaceWith(s)`, `Redact()`. An
`Operator` is just a `func(entityType, match string) string`, so hashing,
tokenization or encryption plug in the same way. Overlapping spans of
different entity types are resolved before replacement — the higher-scoring
span wins and the rest is trimmed, never leaked. Pure Go, dependency-free,
part of the core module.

---

## Make it yours

Add your own detector by implementing `analyzer.Recognizer` (or reuse
`analyzer.PatternRecognizer`) and registering it:

```go
reg := analyzer.NewRegistry("en")
recognizers.LoadDefaults(reg, "en")          // built-ins (optional)
reg.Add("en", analyzer.NewPatternRecognizer(
    "InternalIDRecognizer", "INTERNAL_ID", "en",
    []*analyzer.Pattern{analyzer.MustPattern("internal-id", `\bEMP-\d{6}\b`, 0.9)},
).WithValidator(myChecksum))

eng := analyzer.NewEngine(reg, []string{"en"})
```

The `Recognizer` interface is the seam for statistical backends too; nothing
in the framework assumes regex. The `alcatraz/ner` module (below) plugs in
through the same interface.

## Statistical NER (optional module)

Free-text entities — `PERSON`, `LOCATION`, `NRP`, `DATE_TIME` — need a model,
not a regex. The **`alcatraz/ner`** module runs an ONNX token-classification
model in-process via [hugot](https://github.com/knights-analytics/hugot). Like
`lookaround`, it is a *separate module*: importing it is the only way to pull
in the model runtime, and the default backend is pure Go — no cgo, no shared
libraries. (For maximum throughput, including GPU, see
[Faster inference](#faster-inference-ort-xla-and-gpu) below.)

```bash
go get github.com/hoophq/alcatraz/ner   # requires Go 1.26+
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

Design notes:

- **One inference pass per `Analyze` call.** `SetNlpEngine` makes the engine
  run the model once and share the resulting artifacts with every recognizer
  that consumes them (`analyzer.ArtifactRecognizer`). Without it, the NER
  recognizer still works — it just runs the model itself.
- **Zero cost when unused.** The pattern-only path never touches the model;
  an inference failure degrades to pattern-only results.
- **Presidio-compatible entity names.** Model labels are mapped through
  `ner.Config.LabelMapping` (defaults mirror Presidio: `PER`→`PERSON`,
  `LOC`/`GPE`→`LOCATION`, `NORP`→`NRP`, `DATE`/`TIME`→`DATE_TIME`;
  `ORGANIZATION` and CoNLL `MISC` are dropped by default as false-positive
  prone). Point `Config.Model` at any ONNX token-classification export on
  Hugging Face, or `Config.ModelPath` at a local directory.
- **Byte offsets, guaranteed.** Model spans are mapped back to byte offsets
  in the original text, so `text[r.Start:r.End] == r.Text` holds for NER
  results too, including multi-byte input.

### Faster inference: ORT, XLA and GPU

The pure-Go backend is the zero-friction default, not the speed ceiling.
`ner.Config.Backend` selects one of hugot's faster backends, and
`ner.Config.Accelerator` adds a GPU execution provider on top:

| `Config.Backend` | Build tags | Runtime dependency | Accelerators |
|------------------|-----------|--------------------|--------------|
| `"go"` (default) | none — pure Go, cross-compiles | none | — |
| `"ort"` | `-tags ORT` (cgo + [libtokenizers.a](https://github.com/daulet/tokenizers/releases) at link time) | `libonnxruntime.{so,dylib}` ([releases](https://github.com/microsoft/onnxruntime/releases)) | `coreml` (Apple GPU/ANE), `cuda`, `directml` |
| `"xla"` | `-tags XLA` (cgo) | PJRT plugin | `cuda` |

```go
cfg := ner.DefaultConfig()
cfg.Backend = ner.BackendORT            // needs a -tags ORT build
cfg.Accelerator = ner.AcceleratorCoreML // optional: Apple GPU/Neural Engine
nlp, err := ner.New(ctx, cfg)
```

```bash
# macOS: brew install onnxruntime — found automatically. Otherwise point
# Config.ORTLibraryPath at the library (file or directory).
CGO_LDFLAGS="-L/path/to/libtokenizers" go build -tags ORT .
```

The whole pipeline — windowing, batching, span merging — behaves identically
on every backend, and a backend that is not compiled in fails `ner.New` with
an error naming the missing build tag rather than degrading silently.
Indicatively, ORT on CPU is ~5–10x faster than the pure-Go backend on batch
workloads; CoreML/CUDA go beyond that. To compare on your hardware:

```bash
cd ner && ALCATRAZ_NER_LIVE=1 go test -bench LiveProcessTexts -benchtime 1x -run xxx .
# and the same with ALCATRAZ_NER_BACKEND=ort under a -tags ORT build
```

### Alternative backend: privacy-filter.cpp (`pfilter`)

The **`alcatraz/pfilter`** module binds
[privacy-filter.cpp](https://github.com/localai-org/privacy-filter.cpp) — the
GGML runtime for the `openai-privacy-filter` PII model family — as a second
`analyzer.NlpEngine` implementation. Compared to the `ner` module it trades
setup effort for a **PII-specialized model** (8 categories in the base model,
54 across 16 languages in the multilingual fine-tune, vs. generic
person/location NER), **long-document support** (near-linear banded
attention; 131k-token inputs with halo windowing) and **GPU inference**
(CUDA/Vulkan).

The binding is FFI via [purego](https://github.com/ebitengine/purego) — no
cgo, the module cross-compiles like plain Go — but at runtime it needs the
`libpf` shared library and a GGUF model file. Neither requires a manual
build: `EnsureLibrary` downloads a prebuilt, sha256-pinned `libpf` for your
platform, and `EnsureModel` downloads a GGUF (pre-converted:
[LocalAI-io/privacy-filter-GGUF](https://huggingface.co/LocalAI-io/privacy-filter-GGUF))
verified against its published checksum. Both cache under the user cache dir.

```go
import "github.com/hoophq/alcatraz/pfilter"

// One-time setup, no cmake, no clone: fetch libpf + a model (verified).
if _, err := pfilter.EnsureLibrary(ctx); err != nil { ... }
model, err := pfilter.EnsureModel(ctx, pfilter.ModelQ8) // ~1.6 GB, cached
if err != nil { ... }

// Library resolution: Config.Library, else $PF_LIBRARY, else the
// EnsureLibrary cache, else system paths.
nlp, err := pfilter.New(pfilter.DefaultConfig(model))
if err != nil { ... }
defer nlp.Close()

reg.Add("en", nlp.Recognizer("en"))
eng.SetNlpEngine(nlp) // same seam, same one-pass sharing as the ner module
```

To build `libpf` from source instead (e.g. for CUDA/Vulkan), `pfilter/dist`
has a CMake wrapper that produces one self-contained shared library from a
privacy-filter.cpp checkout:

```bash
git clone --recursive https://github.com/localai-org/privacy-filter.cpp
cmake -S pfilter/dist -B build -DPF_SOURCE_DIR=$PWD/privacy-filter.cpp \
      -DCMAKE_BUILD_TYPE=Release && cmake --build build -j
# -> build/libpf.dylib (macOS) / build/libpf.so (Linux); point $PF_LIBRARY at it
```

Default label mapping: `private_person`→`PERSON`,
`private_address`→`LOCATION`, `private_email`→`EMAIL_ADDRESS`,
`private_phone`→`PHONE_NUMBER`, `private_date`→`DATE_TIME`,
`private_url`→`URL`, plus `ACCOUNT_NUMBER` and `SECRET`. Because the model
shares entity names with the pattern recognizers, overlapping detections
(e.g. an email found by both) collapse in the engine's same-type dedup.
Unmapped labels from the multilingual model surface as
SCREAMING_SNAKE_CASE of the model label; drop them via
`Config.LabelsToIgnore`.

## Advanced matching: lookahead & lookbehind

Go's RE2 `regexp` deliberately omits lookaround and backreferences to guarantee
linear-time matching. Alcatraz keeps that guarantee in its core and offers
three escalating tools — the first two are pure-Go and cover essentially every
real lookaround need:

**A — Context-aware validator.** For "match X only when surrounded by Y". The
validator sees the full text and the match's byte span:

```go
rec := analyzer.NewPatternRecognizer("PinRule", "PIN", "en",
    []*analyzer.Pattern{analyzer.MustPattern("pin", `\d{4}`, 0.5)},
).WithContextValidator(func(text string, start, end int) bool {
    return strings.HasSuffix(text[:start], "PIN ") // emulates (?<=PIN )
})
```

It is a *filter* (keep/drop) and never inflates the score the way a checksum
`WithValidator` does — the two compose if you need both.

**B — Capture-group span.** Match the surrounding context but report only the
captured entity. `WithGroup(n)` selects which group becomes the result span:

```go
// Emulates (?<=user=)\w+ : require the prefix, emit only the value.
p := analyzer.MustPattern("user", `user=(\w+)`, 0.9).WithGroup(1)
```

None of the 45 built-ins need more than A + B — they lean on `\b` anchors plus
validators and same-entity dedup.

**C — True lookaround for user-configured patterns.** When a rule genuinely
needs `(?<=…)`, `(?=…)`, `(?!…)` or backreferences — e.g. regexes supplied in a
config file — use the optional **`alcatraz/lookaround`** module. It is a
*separate module*, so importing it is the only way to pull in the backtracking
engine ([`dlclark/regexp2`](https://github.com/dlclark/regexp2)); the Alcatraz
core stays dependency-free and linear-time.

```go
import "github.com/hoophq/alcatraz/lookaround"

// One call turns user-configured regex rules into a recognizer.
rec, err := lookaround.NewRecognizer("Secret", "API_SECRET", "en",
    lookaround.Spec{Name: "bearer", Regex: `(?<=Bearer )[A-Za-z0-9._-]{8,}`, Score: 0.95},
    lookaround.Spec{Name: "domain", Regex: `(?<=@)(\w+)\.com`, Score: 0.6, Group: 1},
)
reg.Add("en", rec)
```

```bash
go get github.com/hoophq/alcatraz/lookaround   # regexp2 only for importers of this package
```

Backtracking has no linear-time guarantee, so every compiled matcher carries a
`MatchTimeout` (default 1s) to bound catastrophic backtracking (ReDoS); set
your own with `CompileWithTimeout`. Matches report byte offsets just like the
core, so results compose seamlessly through the same `Engine`.

---

## What Alcatraz is — and isn't

Alcatraz is a **pattern engine** at its core: regexes plus checksum
validators, verified against the schemes each identifier actually uses. That
makes it precise on structured identifiers and honest about the rest:

- **ML is opt-in, not built-in.** Free-text entities (`PERSON`, `LOCATION`,
  `NRP`) require the separate [`ner` module](#statistical-ner-optional-module);
  the core alone does not emit them. Statistical detection is probabilistic —
  treat NER scores as confidence, not verification.
- **The default threshold is 0.** Some recognizers are intentionally
  low-confidence (e.g. `US_BANK_NUMBER` at 0.05 for any 8–17 digit run). Set
  `Options.Threshold` to trade recall for precision.
- **Recall over locale-perfection.** Patterns favor catching real identifiers
  over locale-perfect validation of every edge case.

---

## Benchmarks

The [`bench/`](bench/) directory holds a reproducible speed comparison
against [Presidio](https://github.com/data-privacy-stack/presidio)'s Python
analyzer. Both engines read the same generated corpus — 186 documents across
a {100 B, 1 KB, 10 KB, 1 MB} × {no PII, sparse, dense} matrix, every seeded
value passing its real checksum — and emit the same JSON schema, so results
merge into one table.

Representative single-threaded run (Presidio configured with its slim
tokenization-only NLP engine — the closest apples-to-apples with Alcatraz's
pattern-only core; its default spaCy-NER pipeline is slower still):

| Corpus group | Alcatraz ms/doc | Presidio ms/doc | Speedup |
|--------------|----------------:|----------------:|--------:|
| 100 B, no PII |           0.09 |             8.5 |   ~100x |
| 1 KB, no PII  |           0.80 |            15.2 |    ~19x |
| 10 KB, no PII |           8.0  |            84.3 |    ~11x |
| 10 KB, dense  |           8.7  |           116.1 |    ~13x |
| 1 MB, no PII  |          840   |         7,999   |    ~10x |
| 1 MB, dense   |        1,521   |       159,237   |   ~105x |

Speed is only half the story: a parity check diffs the detections of both
engines on the same corpus. On the shared entity types the two agree
exactly (credit cards, IBANs, SSNs, IPs, bank numbers — span for span);
they diverge where recognizer sets differ (e.g. Presidio ships no Brazilian
recognizers).

Numbers vary by machine — reproduce them with two commands per engine; see
[`bench/README.md`](bench/README.md) for setup and methodology.

---

## Roadmap

- [x] 45 pattern recognizers, 25 checksum-validated
- [x] Opt-in `lookaround` module — true lookaround without polluting the core
- [x] ML/NER backend for `PERSON`, `LOCATION`, `NRP` — opt-in `ner` module,
      same pattern as `lookaround`; one shared inference pass per `Analyze`
- [x] `pfilter` module — privacy-filter.cpp (GGML) backend: PII-specialized
      models, long documents, GPU; purego FFI, no cgo
- [ ] Context-word score boosting (raise a match's confidence when related
      words appear near the span — the shared `NlpArtifacts` tokens are the
      input for this)
- [ ] Zero-shot PII models (GLiNER-class): user-defined entity types at
      runtime, no retraining
- [ ] Optional LLM-backed detection/validation — separate module, explicit
      opt-in
- [ ] Precision/recall benchmark suite against a labeled corpus

See [TODO.md](TODO.md) for the detailed plan.

## Layout

```
alcatraz.go        Public entry point: NewEngine + re-exported types.
entities/          Canonical entity-type identifier constants.
analyzer/          Framework: Result, dedup, Recognizer, Pattern, Matcher,
                   PatternRecognizer, Registry, Engine, allow list, and the
                   NLP seam (NlpEngine, NlpArtifacts, ArtifactRecognizer).
anonymizer/        Mask/replace/redact detected spans (Operator, Config).
recognizers/       The 45 built-in recognizers, checksum helpers, loader.
lookaround/        Optional, separate module: regexp2-backed Matcher for
                   lookahead/lookbehind in user-configured patterns.
ner/               Optional, separate module: statistical NER (PERSON,
                   LOCATION, NRP, DATE_TIME) via an in-process ONNX model.
pfilter/           Optional, separate module: PII-specialized NER via
                   privacy-filter.cpp (GGUF models, purego FFI, no cgo).
bench/             Separate module: reproducible speed + parity benchmarks
                   against Presidio's Python analyzer (shared corpus, uv).
```

## Tests

```bash
go test ./...                    # core
cd lookaround && go test ./...   # lookaround module
cd ner && go test ./...          # ner module (unit tests, no model needed)
cd ner && ALCATRAZ_NER_LIVE=1 go test ./...   # + end-to-end (downloads model)
cd pfilter && go test ./...      # pfilter module (unit tests, no lib needed)
cd pfilter && ALCATRAZ_PF_LIVE=1 PF_LIBRARY=/path/libpf.dylib \
  PF_MODEL=/path/privacy-filter-q8.gguf go test ./...   # + end-to-end
```

CI runs the unit tests of all four modules on every push (`test.yml`). The
live end-to-end model tests run in `ml-live.yml` — on PRs touching the ML
modules, weekly, and on demand — with the built `libpf` and the GGUF cached
between runs. Prebuilt `libpf` binaries are produced by the manual
`libpf-release.yml` workflow and published as `libpf-vN` GitHub releases,
which is what `pfilter.EnsureLibrary` downloads.

---

Built by the team behind [hoop.dev](https://hoop.dev).
