# Alcatraz

A pure-Go, dependency-free PII detection library. It is a pattern-based port of
the core of [Microsoft Presidio](https://github.com/microsoft/presidio)'s
analyzer, meant to be **imported and
invoked in-process** no service, no network, no models to download.

```go
eng := alcatraz.NewEngine()
for _, hit := range eng.Analyze("email me at jane@example.com", alcatraz.Options{}) {
    fmt.Println(hit.EntityType, hit.Text, hit.Score)
}
// EMAIL_ADDRESS jane@example.com 0.5
```

## Install

```bash
go get github.com/hoophq/alcatraz
```

Requires Go 1.24+. The standard library is the only dependency.

## How it works

Detection is purely **pattern-based**: each recognizer is one or more regular
expressions plus, for entities with a verifiable structure, a checksum/format
validator. The pipeline mirrors Presidio:

1. Every applicable recognizer runs its regexes over the text.
2. A matched span is scored at the pattern's base confidence; a validator then
   either promotes it to `1.0` (verified) or drops it (failed checksum).
3. Overlapping spans **of the same entity type** are de-duplicated (the
   enclosing/higher-scoring span wins). Different entity types never suppress
   each other.
4. An optional score threshold and allow list are applied.
5. Each surviving result is annotated with the matched substring (`Result.Text`).

21 of the 40 recognizers carry a real validator — Luhn (credit cards), ISO 7064
mod-97 (IBAN), Verhoeff (Aadhaar), and the various national weighted-modulus and
check-letter schemes — so structured identifiers are verified, not just
shape-matched.

## API

```go
// Build an engine with the full built-in recognizer set (English by default).
eng := alcatraz.NewEngine()

results := eng.Analyze(text, alcatraz.Options{
    Entities:       []string{entities.CreditCard}, // optional: restrict types
    Threshold:      ptr(0.4),                       // optional: drop low scores
    AllowList:      []string{"4111111111111111"},   // optional: ignore values
    AllowListRegex: false,                          // treat AllowList as regex
})

for _, r := range results {
    // r.EntityType, r.Start, r.End, r.Score, r.Text, r.RecognizerName
}
```

`Options{}` (the zero value) analyzes English text with every recognizer and no
threshold. `Result` offsets are byte indices, so `text[r.Start:r.End] == r.Text`.

## Supported entities (40)

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
| Other | `PL_PESEL`✓, `KR_RRN`✓, `FI_PERSONAL_IDENTITY_CODE`✓, `TH_TNIN`✓ |

✓ = checksum/format validated. Constants live in the `entities` package.

Because every built-in detects a **language-independent** structured identifier,
the complete set is registered under whichever language an engine is built with,
so a default English engine detects all of them. (The language key is retained
for future language-specific recognizers such as ML/NER.)

## Extending

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

Go's standard `regexp` (RE2) deliberately omits lookaround and backreferences to
guarantee linear-time matching and resist ReDoS. alcatraz keeps that guarantee
in its core and offers three escalating tools — the first two are pure-Go and
dependency-free, and cover essentially every real lookaround need:

**A — Context-aware validator.** For "match X only when surrounded by Y". The
validator sees the full text and the match's byte span, so it can inspect what
comes before/after:

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

None of the 40 built-ins need more than A + B — they lean on `\b` anchors plus
validators and same-entity dedup, which already handle "don't match a sub-span
of a longer token".

**C — True lookaround for user-configured patterns.** When a rule genuinely
needs `(?<=…)`, `(?=…)`, `(?!…)` or backreferences — e.g. regexes supplied in a
config file — use the optional **`alcatraz/lookaround`** module. It is a
*separate module*, so importing it is the only way to pull in the backtracking
engine ([`dlclark/regexp2`](https://github.com/dlclark/regexp2)); the alcatraz
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
`MatchTimeout` (default 1s) to bound catastrophic backtracking (ReDoS); set your
own with `CompileWithTimeout`. Matches report byte offsets just like the core
(regexp2 works in rune space internally — the module converts), so results
compose seamlessly through the same `Engine`.

## Limitations

- **No free-text NER.** Entities that need a statistical model — `PERSON`,
  `LOCATION`, `NRP` — are out of scope. Constants exist for them, but no
  built-in recognizer emits them.
- **Default threshold is 0.** Some recognizers are intentionally low-confidence
  (e.g. `US_BANK_NUMBER` at 0.05 for any 8–17 digit run). Set `Options.Threshold`
  to trade recall for precision.
- Patterns favor recall and faithfulness to the reference implementation over
  locale-perfect validation.

## Layout

```
alcatraz.go        Public entry point: NewEngine + re-exported types.
entities/          Canonical entity-type identifier constants.
analyzer/          Framework: Result, dedup, Recognizer, Pattern, Matcher,
                   PatternRecognizer, Registry, Engine, allow list.
recognizers/       The 40 built-in recognizers, checksum helpers, loader.
lookaround/        Optional, separate module: regexp2-backed Matcher for
                   lookahead/lookbehind in user-configured patterns (C).
```

## Tests

```bash
go test ./...
```
