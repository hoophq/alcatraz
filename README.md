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
> **Detection is pattern-based only — there is no machine-learning engine yet.**
> Entities that need a statistical model (`PERSON`, `LOCATION`, `NRP`) are not
> detected today. This is planned, not fundamental — see the
> [roadmap](#roadmap).

---

## Install

```bash
go get github.com/hoophq/alcatraz
```

Requires Go 1.24+. The standard library is the only dependency.

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
language key is retained for future language-specific recognizers such as
ML/NER.)

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

The `Recognizer` interface is the seam for a future ML/NER backend; nothing in
the framework assumes regex.

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

Alcatraz is a **pattern engine**: regexes plus checksum validators, verified
against the schemes each identifier actually uses. That makes it precise on
structured identifiers and honest about the rest:

- **No ML engine yet.** Free-text entities (`PERSON`, `LOCATION`, `NRP`) need
  a statistical model and are not emitted, even though constants exist for
  them. The `analyzer.Recognizer` interface is the integration seam — see the
  roadmap below.
- **The default threshold is 0.** Some recognizers are intentionally
  low-confidence (e.g. `US_BANK_NUMBER` at 0.05 for any 8–17 digit run). Set
  `Options.Threshold` to trade recall for precision.
- **Recall over locale-perfection.** Patterns favor catching real identifiers
  over locale-perfect validation of every edge case.

---

## Roadmap

- [x] 45 pattern recognizers, 25 checksum-validated
- [x] Opt-in `lookaround` module — true lookaround without polluting the core
- [ ] Context-word score boosting (raise a match's confidence when related
      words appear near the span)
- [ ] ML/NER backend for `PERSON`, `LOCATION`, `NRP` — shipped as a separate
      module, same pattern as `lookaround`
- [ ] Optional LLM-backed detection/validation — separate module, explicit
      opt-in
- [ ] Precision/recall benchmark suite against a labeled corpus

See [TODO.md](TODO.md) for the detailed plan.

## Layout

```
alcatraz.go        Public entry point: NewEngine + re-exported types.
entities/          Canonical entity-type identifier constants.
analyzer/          Framework: Result, dedup, Recognizer, Pattern, Matcher,
                   PatternRecognizer, Registry, Engine, allow list.
recognizers/       The 45 built-in recognizers, checksum helpers, loader.
lookaround/        Optional, separate module: regexp2-backed Matcher for
                   lookahead/lookbehind in user-configured patterns.
```

## Tests

```bash
go test ./...                    # core
cd lookaround && go test ./...   # lookaround module
```

---

Built by the team behind [hoop.dev](https://hoop.dev).
