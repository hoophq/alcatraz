# Complexity & allocation analysis

A function-by-function pass over every component in the `Analyze` flow and
its satellites, with time complexity and heap-allocation behaviour per call,
so the places worth optimizing are visible at a glance. Numbers in the
"Measured" section come from the benchmark corpus in `bench/`; everything
else is read from the code.

Jump to [Where to optimize](#where-to-optimize) for the ranked list.

> **Status (2026-10-12).** Landed from the ranked list: Allocations-1
> (`RemoveDuplicates` early return, `slices.SortStableFunc`), Allocations-6
> (each recognizer stores its entity slice; the registry caches the filtered
> recognizer set per `Options.Entities`), Time-1 bullets 1 to 3 as a
> byte-class prefilter (`analyzer/classes.go` computes a byte-class mask per
> call and derives a required-class mask per pattern from the
> `regexp/syntax` tree), exact-source regex program sharing with a per-call
> scan cache for the duplicate digit-run patterns (byte-identical results by
> construction; the semantic length-dispatch merge stays undone), and in
> `ner` the window short-circuit plus a memoized `SupportedEntities`.
> Remeasured on the same machine: 100B/none 118 µs and 53 allocs → 3.1 µs and
> 0; 100B/dense 118 µs → 49 µs and 10 allocs; 1MB/none 1.08 s → 30 ms (a text
> with no digit skips all numeric patterns); 1MB/dense 1.64 s → 1.49 s. The
> new `BenchmarkAnalyzeOCRLine` (60 bytes, 51 entities) runs prose at 2.2 µs
> and 0 allocs, down from 69 µs and 110, and digits at about 60 µs and 18
> allocs, down from 91 µs. `NewEngine` 169 → 78 µs. The parity corpus in
> `bench/` matches byte for byte before and after. The per-row tables below
> still describe the code before these changes.

## Variables

| Symbol | Meaning |
|---|---|
| `n` | `len(text)` in bytes |
| `R` | recognizers registered per language (52 built-in) |
| `P` | compiled patterns across all recognizers (74 built-in) |
| `m` | matches of one pattern in `text` |
| `M` | results across all recognizers for one `Analyze` (before dedup) |
| `s` | bytes in one matched substring (10–40 typical); `d` = decimal digits in it |
| `E` | `len(Options.Entities)`; `A` = `len(Options.AllowList)` |
| `C` | context words declared by one recognizer; `k` = `WordsBefore + WordsAfter` (5 by default) |
| `\|re\|` | compiled RE2 program size of one pattern |
| `t` | tokens, `w` windows, `L` token budget (NER); `C(n)` = model forward cost |

**Hot** = runs on every `Engine.Analyze`. **Setup** = once per engine /
recognizer construction.

## Measured

`go test -bench 'BenchmarkAnalyze|BenchmarkNewEngine' -benchmem` in `bench/`,
Apple M4 Max, Go 1.26, default English engine, no NLP backend:

| Size / density | ns/op | MB/s | B/op | allocs/op | results/op |
|---|---:|---:|---:|---:|---:|
| 100B / none | 110 k | 0.91 | 1 470 | 53 | 0 |
| 100B / dense | 139 k | 0.72 | 2 216 | 64 | 0.6 |
| 1KB / none | 991 k | 1.03 | 1 470 | 53 | 0 |
| 1KB / dense | 1.05 M | 0.97 | 16 417 | 153 | 8.8 |
| 10KB / none | 10.3 M | 0.99 | 1 613 | 53 | 0 |
| 10KB / sparse | 10.7 M | 0.95 | 22 084 | 191 | 12.3 |
| 10KB / dense | 11.2 M | 0.91 | 153 919 | 596 | 94 |
| 1MB / none | 1.10 s | 0.96 | 1 338 | 53 | 0 |
| 1MB / sparse | 1.20 s | 0.88 | 1.8 M | 4 671 | — |
| 1MB / dense | 1.65 s | 0.64 | 15.5 M | 36 834 | 9 575 |
| `NewEngine()` | 169 k | — | 459 224 | 3 797 | — |

Three facts fall out of the table:

1. **Time is linear in `n` and ~1 µs/byte regardless of density.** That is
   the cost of 74 full-text regex passes: CPU profile on 10KB/dense is 91 %
   inside `regexp` (`stdMatcher.FindAll`), of which `\b` word-boundary
   evaluation (`inputString.context` + `IsWordChar`) is ~12 %.
2. **A zero-result call still makes 53 allocations.** 52 of them are the
   `sort.SliceStable` closure in `RemoveDuplicates`, called once per
   recognizer plus once by the engine even when the slice is empty.
3. **Dense 1MB is 50 % slower than empty 1MB.** CPU profile: `RemoveDuplicates`
   is 27 % of the call at `M ≈ 9 600` — the quadratic span comparison, not the
   regexes, is what grows with match density.

## Flow and per-stage cost

```
Engine.Analyze(text, o)
│  Registry.Recognizers(lang, o.Entities)         O(R·E)   R small allocs when E set
│  [NlpEngine.ProcessText]                         C(n)     only with ner/pfilter attached
└─ analyzeWithArtifacts
   ├─ for each recognizer: PatternRecognizer.Analyze
   │    for each pattern: stdMatcher.FindAll        O(n·|re|)  1 + 2m allocs      ← 91 % CPU
   │       validator(match)                         O(s)       0–4 allocs/match
   │       contextValidator(text, start, end)       O(1)       0–3 allocs/match
   │    RemoveDuplicates (per recognizer)           O(m²)      1 closure alloc even if m = 0
   ├─ WordContextEnhancer.Enhance                   O(M·k + R·C)  map + ToLower per word
   ├─ RemoveDuplicates (all results)                O(M²)      ← 27 % CPU at 1MB/dense
   ├─ threshold filter                              O(M)       in place
   ├─ applyAllowList                                O(A + M)   regexp.Compile per call in regex mode
   └─ fill Result.Text                              O(M)       substrings, no copy
```

---

## `alcatraz` (root)

| Function | Path | Time | Allocations | Note |
|---|---|---|---|---|
| `NewEngine(languages...)` `alcatraz.go:30` | setup | O(langs · (R + P·compile)) | per language: 52 recognizers, 74 `regexp.Compile`; measured 459 KB / 3 797 allocs for `en` | `LoadDefaults` recompiles all 74 regexes for every language; the recognizers are language-independent, so one `All()` could be shared across languages. |

## `analyzer`

### engine.go

| Function | Path | Time | Allocations | Note |
|---|---|---|---|---|
| `NewEngine(registry, languages)` :40 | setup | O(langs) | `Engine`, copy of `languages`, `WordContextEnhancer` | - |
| `SetThreshold` / `SetNlpEngine` / `SetContextEnhancer` :50-64 | setup | O(1) | 0 | - |
| `Languages()` :67 | - | O(langs) | 1 slice copy | - |
| `SupportedEntities(language)` :70 | - | O(R + S log S) | map + slice (see `Registry.SupportedEntities`) | - |
| `Analyze(text, o)` :81 | hot | `Recognizers` + optional `ProcessText` + `analyzeWithArtifacts` | 0 own | Entry point. |
| `AnalyzeBatch(texts, o)` :107 | hot | T × `analyzeWithArtifacts` + one `ProcessTexts` when batchable | `artifacts []*NlpArtifacts` (T), `results [][]Result` (T) | - |
| `wantsArtifacts(recs)` :139 | hot (only when an NLP engine is set) | O(R) type assertions | 0 | - |
| `analyzeWithArtifacts(text, o, recs, artifacts)` :151 | hot | Σ recognizer cost + `Enhance` + **O(M²)** dedup + O(M) filter + allow list | `all` grows by `append` across 52 recognizer return slices (~log₂ M reallocs, each copying 72-byte `Result`s); every recognizer's own result slice is garbage after the copy | Pre-size `all` or let recognizers append into a caller-provided slice to remove one copy of every result. |
| `applyAllowList(results, allowList, text, regexMode)` :197 | hot (when `AllowList` set) | regex mode: **`regexp.Compile` of the joined list per call** + O(M·\|re\|); exact mode: O(A + M) | regex mode: `strings.Join` + full regexp compile (measured +21 allocs, +3.2 KB for a 2-entry list on a zero-result text); exact mode: `map[string]struct{}` of A entries per call | Cache the compiled allow-list regex / set on the engine or on `Options` identity; the list rarely changes between calls. |

### registry.go

| Function | Path | Time | Allocations | Note |
|---|---|---|---|---|
| `NewRegistry(languages...)` :12 | setup | O(langs) | map + slice copy | - |
| `Add(language, rec)` :30 | setup | O(1) amortized | slice growth | - |
| `Recognizers(language, entities)` :39 | hot | `entities == nil`: O(1), 0 allocs. Otherwise O(R · E) | `out` grown by `append` + **one `[]string` per recognizer from `SupportedEntities()`** (measured: 51 extra allocs with a one-entity filter) | Have `PatternRecognizer` hold a pre-built one-element slice, or add a `SupportsEntity(string) bool` fast path. |
| `SupportedEntities(language)` :55 | - | O(R + S log S) | `seen` map, `out` slice, sort | - |
| `Languages()` :71 | - | O(langs) | 1 slice copy | - |

### recognizer.go

| Function | Path | Time | Allocations | Note |
|---|---|---|---|---|
| `supportsAny(have, want)` :22 | hot (filtered calls) | O(\|have\|·\|want\|) | 0 | Fine for \|have\| = 1. |

### pattern.go

| Function | Path | Time | Allocations | Note |
|---|---|---|---|---|
| `NewPattern(name, regex, score)` :24 | setup | `regexp.Compile`: O(\|regex\|) parse + compile | `*Pattern`, `*regexp.Regexp` and its program (dozens of internal allocs) | - |
| `MustPattern(...)` :35 | setup | = `NewPattern` | + string concat on panic | - |
| `NewPatternMatcher(name, m, score)` :47 | setup | O(1) | `*Pattern` | - |
| `WithGroup(group)` :53 | setup | O(1) | 0 | - |

### matcher.go

| Function | Path | Time | Allocations | Note |
|---|---|---|---|---|
| `Match.Span(g)` :14 | hot | O(1) | 0 | - |
| `stdMatcher.String()` :42 | - | O(1) | 0 | - |
| `stdMatcher.FindAll(text)` :44 | **hot, ×P per Analyze** | **O(n·\|re\|)** per pattern; Go picks the bitstate backtracker for `len(text)·\|re\|` under its cap, NFA above it. Patterns starting with `\b` get no literal-prefix skip, so every byte is visited by every pattern. | `FindAllStringSubmatchIndex` → outer `[][]int` (1) + `slices.Clone` per match (m); then this function copies into `[]Match` (1) + `[][2]int` per match (m): **2 + 2m allocs, two copies of every offset**. Profile: 49 % of allocated objects on 10KB/dense. | Return the `[][]int` directly (or a flat `[]int` with stride), and have `Match.Span` index into it; drops to 1 + m allocs and one copy. Unused capture groups in the patterns (Email 6, CreditCard 8, DateTime 3 each, …) inflate every row by 2 ints per group. |

### pattern_recognizer.go

| Function | Path | Time | Allocations | Note |
|---|---|---|---|---|
| `NewPatternRecognizer(...)` :30 | setup | O(1) | 1 struct | - |
| `WithContext(words...)` :48 | setup | O(1) | 0 (stores the variadic slice) | Context phrases are re-split and lower-cased on **every** `Enhance` (see `contextWords`); splitting here once would remove that work. |
| `WithValidator` / `WithContextValidator` :55-63 | setup | O(1) | 0 | - |
| `Context()` / `Name()` / `SupportedLanguage()` | hot | O(1) | 0 | - |
| `SupportedEntities()` :75 | hot when `Entities` set | O(1) | **1 `[]string{entity}` per call** | Store the one-element slice on the struct at construction. |
| `Analyze(text, entities)` :86 | **hot, ×R** | Σ over patterns of `FindAll` + m × (validator O(s) + contextValidator) + `RemoveDuplicates` O(m²) | `results` grown by `append` (~log₂ m reallocs, 72 B/element); `text[start:end]` is a substring (0 allocs); + `RemoveDuplicates` closure **even when `results` is nil** | Early-return `nil` when no pattern matched; pre-size `results` from the match count. Per-recognizer dedup is redundant with the engine-level one for single-pattern recognizers (34 of 52): `FindAll` already returns non-overlapping matches. |

### result.go

| Function | Path | Time | Allocations | Note |
|---|---|---|---|---|
| `Result.Len` / `Intersects` / `ContainedIn` / `Contains` / `EqualIndices` :30-54 | hot | O(1) | 0 | `Result` is 72 bytes (3 strings + 2 ints + float); every slice copy / sort swap moves all of it. |
| `RemoveDuplicates(results)` :61 | **hot, ×(R + 1)** | **O(M²)**: for each result, scan `filtered` twice (containment check, then subsumption sweep); then `sort.SliceStable` O(M log² M) via reflection | `filtered` (1, exact size), **`sort.SliceStable` closure (1 — paid even for an empty input)**, `reflectlite.Swapper` (1–2 when non-empty). Measured: 52 of the 53 baseline allocs on a zero-result call. | (1) `if len(results) < 2 { return results }` before sorting kills the baseline. (2) Sort by `(EntityType, Start, -End)` first, then a single linear sweep keeps the outermost/highest-scoring span → O(M log M); the pairwise loop only ever compares same-type spans anyway. (3) `slices.SortStableFunc` removes the reflection swapper. At 1MB/dense this function is 27 % of CPU. |

### context.go

| Function | Path | Time | Allocations | Note |
|---|---|---|---|---|
| `NewWordContextEnhancer()` :100 | setup | O(1) | 1 | - |
| `Enhance(text, results, recs, _)` :111 | hot (when `M > 0`) | `contextWords` + Σ over boostable results of (`appendWordsBefore` + `appendWordsAfter` + `supported`) = O(R·C + M·(k·w̄ + C·k)) where w̄ = bytes walked per word; a huge token before a match makes one window O(n) | one `window` slice for the call (k strings); + `contextWords`; + per window word `foldWord` → `strings.ToLower` allocates only if the word has upper-case bytes and is ≤ `maxWord` | Already bounded against the base64-blob case (see comment in source). Remaining per-call cost is `contextWords`. |
| `contextWords(recs, results)` :170 | hot (when `M > 0`) | O(M + R + Σ_{hit recognizers} C · \|entry\|) | `byRecognizer` map (1 + growth); per hit recognizer: `phrases` slice + `splitWords` per entry (slice + one lower-cased string per word). Profile: 5.6 % of objects on 10KB/dense. | Pre-split and lower-case context entries once in `WithContext`; `contextWords` then only builds the small map (or a per-recognizer index assigned at registration, removing the map too). |
| `supported(window, phrases)` :215 | hot | O(C · k · \|phrase\|) | 0 | - |
| `containsPhrase(window, phrase)` :226 | hot | O(k · \|phrase\|) | 0 | - |
| `wordMatches(word, context)` :256 | hot | O(\|context\|) | 0 | - |
| `foldWord(s, maxWord)` :283 | hot | O(\|s\|) if folded, O(1) otherwise | 0–1 (`ToLower`, only when needed) | - |
| `appendWordsBefore(dst, text, end, n, maxWord)` :293 | hot | O(bytes walked back to the k-th word boundary); rune-decoded, not byte-indexed | 0 own (writes into `dst`) + `foldWord` | ASCII fast path for `isWordRune` would cut the per-rune `unicode` table lookups. |
| `appendWordsAfter(...)` :353 | hot (only if `WordsAfter > 0`; default 0) | same shape forward | same | - |
| `splitWords(s)` :399 | hot via `contextWords` | O(\|s\|) | `words` slice + one `ToLower` string per word | Hoist to setup (above). |
| `isWordRune(r)` :428 | hot (per rune walked) | O(1) — three `unicode` table lookups | 0 | `r < 0x80` fast path. |

### nlp.go

Interfaces and plain structs only (`Token`, `NerSpan`, `NlpArtifacts`,
`NlpEngine`, `BatchNlpEngine`, `ArtifactRecognizer`). No runtime cost; the
implementations are in `ner` and `pfilter` below.

## `recognizers`

All 52 constructors are **setup**: each calls `analyzer.MustPattern`, so the
regexes are compiled at `All()`/`LoadDefaults` time, not at package init, and
once per language loaded. Per constructor the cost is `p` × `regexp.Compile`
+ one `PatternRecognizer` + a context-word slice. The table below lists what
matters at runtime: pattern count, base score, and the validator that runs
per match.

### Constructors

| Constructor | File | Patterns (score) | Hot per-match code |
|---|---|---|---|
| `Email` | generic.go:17 | 1 (0.5) — largest RE2 program in the set, 6 unused capture groups | none |
| `Phone` | generic.go:31 | 3 (0.5 each), US/BR use `WithGroup(1)` | none |
| `CreditCard` | generic.go:57 | 1 (0.3), 8 unused groups | `luhnValid(stripSeparators(m))` |
| `Crypto` | generic.go:67 | 2 (0.5) | none |
| `IP` | generic.go:78 | 2 (0.6) | none |
| `URL` | generic.go:90 | 1 (0.5) | none |
| `DateTime` | generic.go:102 | 4 (0.6–0.8), 3 unused groups each | none |
| `IBAN` | generic.go:124 | 2 (0.3 / 0.4) | `validateIBAN` |
| `USSSN` | us.go:13 | 2 (0.85 / 0.3 `\b\d{9}\b`) | `validateSSN` |
| `USITIN` | us.go:60 | 2 (0.85 / 0.3) | `validateITIN` |
| `USPassport` | us.go:91 | 2 (0.3 `\b[0-9]{9}\b` / 0.4) | none |
| `USDriverLicense` | us.go:103 | 3 (0.3–0.5) | none |
| `USBank` | us.go:115 | 1 (0.05, `\b\d{8,17}\b`) | none — matches every 8–17 digit run, each becomes a `Result` that the threshold later drops |
| `ABARouting` | us.go:123 | 1 (0.5, `\b\d{9}\b`) | `validateABA` |
| `MedicalLicense` | us.go:140 | 1 (0.4) | none |
| `UKNHS` | uk.go:11 | 1 (0.6) | `validateNHS` |
| `UKNINO` | uk.go:36 | 1 (0.7) | `validateNINO` |
| `AUTFN` | au.go:10 | 1 (0.3) | `validateTFN` |
| `AUABN` | au.go:32 | 1 (0.3) | `validateABN` |
| `AUACN` | au.go:56 | 1 (0.3) — identical regex to `AUTFN` | `validateACN` |
| `AUMedicare` | au.go:78 | 1 (0.6) | `validateMedicare` |
| `INAadhaar` | india.go:9 | 1 (0.6) | inline: `digitValues` + `verhoeffValid` (2 allocs) |
| `INPAN` / `INPassport` / `INVehicle` / `INVoter` / `INGSTIN` | india.go | 1 each (0.6–0.85) | none |
| `ITFiscalCode` | italy.go:12 | 1 (0.85) | `validateITFiscalCode` |
| `ITVAT` | italy.go:54 | 1 (0.3, `\b\d{11}\b`) | `validateITVAT` |
| `ITIdentityCard` | italy.go:81 | 2 (0.7 / 0.3 `\b[A-Z0-9]{7}\b` — very high `m` on code-like text) | none |
| `ITDriverLicense` / `ITPassport` | italy.go | 1 each (0.6) | none |
| `ESNIF` | spain.go:14 | 1 (0.7) | `validateESNIF` |
| `ESNIE` | spain.go:34 | 1 (0.7) | `validateESNIE` |
| `SGFIN` | singapore.go:10 | 1 (0.7) | `validateSGFIN` |
| `SGUEN` | singapore.go:46 | 2 (0.5 / 0.7) | none |
| `BRCPF` | brazil.go:13 | 2 (0.4 / 0.3 `\b\d{11}\b`) | `validateBRCPF` |
| `BRCNPJ` | brazil.go:36 | 2 (0.4 / 0.3) | `validateBRCNPJ` |
| `BRRG` | brazil.go:62 | 1 (0.4) | none |
| `BRCNH` | brazil.go:73 | 1 (0.3 `\b\d{11}\b`) | `validateBRCNH` |
| `BRPIS` | brazil.go:113 | 2 (0.4 / 0.3) | `validateBRPIS` |
| `BRCNS` | brazil.go:167 | 2 (0.4 / 0.3) | `validateBRCNS` |
| `BRTitulo` | brazil.go:225 | 2 (0.4 / 0.3) | `validateBRTitulo` |
| `BRRenavam` | brazil.go:271 | 1 (0.3 `\b\d{11}\b`) | `validateBRRenavam` (same math as PIS) |
| `BRCEP` | brazil.go:305 | 1 (0.5) | none |
| `BRCEPUnformatted` | brazil.go:327 | 1 (0.8 `\b\d{8}\b`) | context validator `labelBefore` on **every** 8-digit run |
| `BRPlaca` | brazil.go:345 | 2 (0.6, `(?i)`) | none |
| `BRPixKey` | brazil.go:367 | 1 (0.8, UUID) | context validator `labelBefore` on every UUID |
| `PLPESEL` | others.go:11 | 1 (0.3 `\b\d{11}\b`) | `validatePESEL` |
| `KRRRN` | others.go:34 | 1 (0.7) | `validateKRRRN` |
| `FIPersonalCode` | others.go:57 | 1 (0.85) | `validateFIPersonalCode` |
| `THTNIN` | others.go:83 | 2 (0.3 `\b\d{13}\b` / 0.85) | `validateTHTNIN` |

**Duplicate scans.** `\b\d{11}\b` is compiled and run by six recognizers
(BRCPF, BRCNH, BRPIS, BRRenavam, ITVAT, PLPESEL); `\b\d{9}\b` by three
(USSSN, USPassport, ABARouting); AUTFN and AUACN share one regex; and
`\b\d{8}\b`, `\b\d{8,17}\b`, `\b\d{12}\b`, `\b\d{13}\b`, `\b\d{14}\b`,
`\b\d{15}\b` all re-find the same digit runs. One digit-run scan dispatching
by length to the validators would replace ~15 of the 74 passes.

### defaults.go

| Function | Path | Time | Allocations | Note |
|---|---|---|---|---|
| `LoadDefaults(reg, language)` :13 | setup | O(R + P·compile) | everything in `All()` + 52 `Registry.Add` | Called once per language by `alcatraz.NewEngine`. |
| `All()` :20 | setup | O(R + P·compile) | 10 group slices, `recs` grown by `append`, 52 recognizers, 74 patterns + regexps | - |
| `generic` … `other` :32-80 | setup | per group | 1 slice each | - |

### checksum.go (hot helpers)

| Function | Path | Time | Allocations | Note |
|---|---|---|---|---|
| `stripSeparators(s)` :7 | hot (CreditCard) | O(s) | **`strings.NewReplacer` built per call** (Replacer + lookup table) + result string. Profile: 24 % of bytes allocated by `Analyze` on 10KB/dense. | Delete it: `digitValues` already ignores non-digits, so `luhnValid(m)` is equivalent. |
| `digitValues(s)` :13 | hot (24 of 30 validators) | O(s) | 1 `[]int` of cap `len(s)` (8·s bytes) | A `[40]byte` stack buffer or validating over bytes directly removes the only allocation in most validators. |
| `digitsExactly(s, n)` :25 | hot | O(s) | = `digitValues` | Count digits before allocating; early-exit on `len(s) < n`. |
| `verhoeffValid(s)` :60 | hot (Aadhaar) | O(s) + O(d) | 1 | Tables are package arrays. |
| `luhnValid(s)` :70 | hot (CreditCard) | O(s) + O(d) | 1 | - |

### Validators (hot, once per match)

| Function | File | Time | Allocations | Fix |
|---|---|---|---|---|
| `validateIBAN` | generic.go:135 | O(s) | 3–4: `ReplaceAll`, 4-char rotation concat, `strings.Builder` growth | streaming mod-97 over the original bytes, skipping spaces: 0 allocs |
| `validateSSN` | us.go:24 | O(s) | `ReplaceAll` (when dashed) + `digitValues` | parse into `[9]int` on the stack |
| `validateITIN` | us.go:70 | O(s) | `ReplaceAll` (when dashed) | index bytes directly |
| `validateABA` | us.go:130 | O(s) | 1 (`digitValues`) | - |
| `validateNHS` | uk.go:18 | O(s) | 1 | - |
| `validateNINO` | uk.go:44 | O(s) | `strings.Fields` + `strings.Join` (1–2) | byte walk into `[9]byte` |
| `validateTFN` / `validateABN` / `validateACN` / `validateMedicare` | au.go | O(s) | 1 each (`digitsExactly`) | `validateABN` mutates `ds[0]` — only safe because the slice is fresh; a pooled buffer must copy. |
| `INAadhaar` closure | india.go:13 | O(s) ×2 | 2 (`digitValues` then `verhoeffValid` → `digitValues` again) | call `digitsExactly(m, 12)` once and pass the slice |
| `validateITFiscalCode` | italy.go:20 | O(15·26) `IndexByte` over const tables | **0** | `[256]int8` table (minor) |
| `validateITVAT` | italy.go:61 | O(s) | 1 | - |
| `validateESNIF` | spain.go:21 | O(8) | 0 | - |
| `validateESNIE` | spain.go:41 | O(8) | 1 (`string(lead) + s[1:8]` for `Atoi`) | `Atoi(s[1:8])` + arithmetic for the lead digit |
| `validateSGFIN` | singapore.go:17 | O(7) | 1 (`digitValues` on a guaranteed-digit slice) | index bytes directly |
| `validateBRCPF` / `BRCNPJ` / `BRCNH` / `BRPIS` / `BRCNS` / `BRTitulo` / `BRRenavam` | brazil.go | O(s) + O(d) | 1 each (`digitsExactly`) | - |
| `mod11Weighted` / `allEqual` / `validDefinitiveBRCNS` / `weightedSum15` | brazil.go:133-215 | O(d) | 0 | - |
| `labelBefore(text, start, labels...)` | brazil.go:386 | O(w + L·(F + w)), w = 24-byte window | `ToLower` (if upper-case present) + `FieldsFunc` slice + `Join` (when > 1 field) + small concats per multi-word label: up to 3+ allocs | Runs on every `\b\d{8}\b` match and every UUID. Rewrite as a lower-casing byte walk compared against labels in place. |
| `validatePESEL` / `validateKRRRN` / `validateTHTNIN` | others.go | O(s) | 1 each | - |
| `validateFIPersonalCode` | others.go:65 | O(1) | 1 (`s[0:6] + s[7:10]` for `Atoi`) | parse the two runs manually |

### separators.go

Only `const hsp = \p{Zs}`, concatenated into pattern literals at compile time.
No runtime cost; no input normalization happens anywhere in this package.

## `anonymizer`

Not on the `Analyze` path; runs once per `Anonymize` call. `m` = input
results, `K` = kept pieces after overlap resolution (≤ 2m).

| Function | Path | Time | Allocations | Note |
|---|---|---|---|---|
| `Mask(char)` :19 → closure | per span | O(\|match\|) | 1 (`strings.Repeat`) | - |
| `MaskKeepLast(char, keep)` :28 → closure | per span | O(\|match\|) | **4**: `[]rune(match)`, `Repeat`, tail `string(...)`, concat | walk back `keep` runes with `utf8.DecodeLastRuneInString`, one `Builder` → 1 alloc |
| `Replace()` :45 → closure | per span | O(\|type\|) | 1 (`"<"+type+">"`) | memoize per entity type (≤ 51 values) |
| `ReplaceWith(p)` / `Redact()` → closures | per span | O(1) | 0 | - |
| `Anonymize(text, results, op)` :71 | per call | = `AnonymizeWith` | 0 own | - |
| `AnonymizeWith(text, results, cfg)` :76 | per call | **O(m log m + m² + K·n)**: `resolve` + K splices, each `append(out[:Start], append([]byte(repl), out[End:]...)...)` copying the suffix twice | `[]byte(text)` (n), per span: operator result + `[]byte(repl)` + suffix temp (≈ 3), final `string(out)` (n) → **2 + 3K allocs, ≈ 2n + Σ(n − End_i) bytes moved** | Forward pass over ascending spans into one `strings.Builder` with `Grow(n)`: O(n + Σ\|repl\|), 1 alloc. |
| `resolve(results, textLen)` :101 | per call | clamp O(m); `sort.SliceStable` O(m log² m); overlap loop O(m·K) `subtract` calls (each allocating); `sort.Slice` O(K log K) | `spans` copy; 2 reflection sorts (closure + Swapper each); per (span, kept) pair `[]Result{s}` / `next` slices → **O(m²) small allocs** even when nothing overlaps | Check disjointness before allocating; `slices.SortStableFunc`; keep `kept` sorted by `Start` and binary-search candidates. |
| `subtract(p, k)` :148 | per pair | O(1) | 1–2 (always allocates, even for the no-overlap case) | return fragments by value |

## `entities`

51 untyped string constants. No functions, no runtime cost.

## `ner` (optional module)

Hot path per `Analyze`: `ProcessText` → `ProcessTexts` → fold → segment →
window → `hugot` pipeline → span conversion → merge → snap. The model forward
pass dominates everything else by orders of magnitude; Go-side work matters
for GC pressure and for short texts under `SegmentFields`.

| Function | File:line | Path | Time | Allocations | Note |
|---|---|---|---|---|---|
| `New(ctx, cfg)` | ner.go:54 | setup | model download/verify (sha256 over ~260 MB) + ONNX session load | session, pipeline, optional second tokenizer | - |
| `ensureModel` / `Close` / `Config` | ner.go:158-188 | setup | I/O / O(1) | - | - |
| `ProcessText(text, lang)` | ner.go:199 | hot | = `ProcessTexts` with T = 1 | `[]string{text}` | single-text fast path (minor) |
| `inferenceRows(texts, folded, offsets)` | ner.go:227 | hot | O(N) + tokenization of every segment (see `tokenWindows`) | `rows` (grows by doubling), `windowed []bool`, per segment a 1-elem `[]segment` and `[]textWindow` | pre-size `rows` from segment count |
| `ProcessTexts(texts, lang)` | ner.go:265 | hot | O(N) fold + rows + ⌈R/B⌉ × `RunPipeline` (**O(R·L̂²)** forward) + Σ `toNerSpan` + `mergeSpans` + `snapToWords` | `folded`, `foldOffsets`, `artifacts` (T each), `bodies` per sub-batch, hugot tensors/logits, `Ents` append growth | **Every row is tokenized twice** (here for windowing, again inside hugot); pass pre-tokenized input or skip windowing for bodies with `len+specials ≤ L`. |
| `toNerSpan(text, offsets, ent)` | ner.go:327 | hot ×m | O(g) `ignored` ×2 + O(q) + O(1) | 0 | sets instead of linear scans if the ignore lists grow |
| `ignored(label)` | ner.go:366 | hot ×2m | O(g) | 0 | - |
| `Recognizer(language)` | ner.go:379 | setup | O(1) | 1 | - |
| `Recognizer.SupportedEntities()` | ner.go:400 | hot when `Entities` set (via `Registry.Recognizers`) | O(g + M_labels + K log K) | **2 maps + slice + sort per call** (`Config.SupportedEntities`) | cache at construction |
| `Recognizer.Analyze(text, entities)` | ner.go:408 | hot only without `SetNlpEngine` | `supportsAny` + `ProcessText` + `AnalyzeWithArtifacts` | as above | - |
| `Recognizer.AnalyzeWithArtifacts(...)` | ner.go:420 | hot | O(m·E) | `results` append growth | `make(..., 0, len(Ents))` |
| `contains` / `supportsAny` | ner.go:437-446 | hot | O(list) / O(have·want) | 0 | - |
| `foldASCII(text)` | offsets.go:24 | hot per text | O(n) | ASCII: **0**. Non-ASCII: `Builder` (n) + **`[]int` of one entry per rune, cap `len(text)` → 8·n bytes** + `foldRune` allocs | sparse delta table (O(u)), `[]int32` |
| `foldRune(r)` | offsets.go:49 | hot per rune (non-ASCII text) | O(1) ASCII; O(decomp) otherwise | **2 allocs per non-ASCII rune** (`string(r)`, `norm.NFD.String`) | Latin-1/Latin-Ext lookup table or `norm.NFD.AppendString` into a stack buffer |
| `remapSpan` / `byteSpan` | offsets.go:64-85 | hot ×m | O(1) | 0 | - |
| `normalizeSegmentation(mode)` | segments.go:58 | setup | O(1) | ≤ 1 | - |
| `segments(folded)` | segments.go:74 | hot per text | whole: O(1); lines/fields: O(n) ×2 passes | whole: 1-elem slice; otherwise exact-size slice | - |
| `cutAfter(folded, sep)` | segments.go:93 | hot (lines/fields) | 2 × O(n) with an indirect `sep` call per byte | 1 | `[256]bool` table or `IndexByte` jumps |
| `isLineSep` / `isFieldSep` / `segmentText` / `hasWordRune` | segments.go | hot | O(1) / O(1) / O(seg) early-exit | 0 | - |
| `windows(folded)` | windows.go:53 | hot per segment | `tokenWindows` or `byteWindows` | 1 slice | **short-circuit when `len(folded)+specials ≤ tokenBudget`** — every content token covers ≥ 1 byte, so one window is provably enough and the tokenizer pass is skipped for every short cell/line |
| `windowTokenizer` / `loadWindowTokenizer` | windows.go:74-86 | setup | tokenizer.json parse | vocab maps (tens of MB) | - |
| `tokenWindows(tk, folded, budget)` | windows.go:112 | hot per segment | full `EncodeWithAnnotations` O(n) **always**, + per window a re-`Encode` with shrink retries ≈ 1.15·t tokens | encoding (IDs, token strings, spans, mask — ~5 slices + t strings), `contentSpans` copy (16·t bytes, built even when the result fits one window), `wins` growth, per-retry encodings | move the `contentSpans` copy after the budget check; count-only encode if the API allows |
| `contentSpans(enc)` | windows.go:153 | hot per segment | O(t) | 1 (16·t bytes) | build lazily |
| `byteWindows(n, size, overlap)` | windows.go:170 | fallback only | O(n/step) | growth | pre-size |
| `mergeSpans(spans)` | windows.go:195 | hot (windowed texts) | O(m log m) | `sort.Slice` closure + Swapper (2) + `widest` map | `slices.SortFunc`, linear type list |
| `wordRune(r)` | words.go:38 | hot per edge rune | O(1), three `unicode` lookups | 0 | ASCII fast path |
| `wordBounds(s, i)` | words.go:50 | hot ≤ 2m | O(min(word, 100)) | 0 | bounded by `maxWordChars` (benchmarked: 17 ms → < 1 µs on a 4 MiB blob) |
| `snapToWords(text, spans)` | words.go:106 | hot (m > 0) | O(m·100 + m log m) | `sort.SliceStable` closure + Swapper + `last` map | `slices.SortStableFunc`; skip sort when m = 1 |
| `DefaultConfig()` | config.go:200 | setup | O(1) | map + 3 slices | - |
| `Config.SupportedEntities()` | config.go:233 | hot when `Entities` set | O(g + M_labels + K log K) | 2 maps + slice + sort | memoize |
| `backend.go` (7 funcs) / `offline.go` (5) / `models.go` (5) | — | setup | session options, stat/open, sha256 forwarders | setup-only | - |

## `pfilter` (optional module)

Hot path: `ProcessText` → `classify` (one FFI call) → `toNerSpan` ×m →
`AnalyzeWithArtifacts`. `C(n)` (GGML inference) dominates.

| Function | File:line | Path | Time | Allocations | Note |
|---|---|---|---|---|---|
| `New(cfg)` | pfilter.go:23 | setup | P × `pf_load` (full GGUF per context: memory = P × model) | engine, config map, contexts, pool | - |
| `Close` / `Config` | pfilter.go:38-43 | — | O(P) / O(1) | 0 | - |
| `ProcessText(text, lang)` | pfilter.go:51 | hot | C(n) + O(m·I) | `NlpArtifacts` + `Ents` append growth + `classify` | `make(Ents, 0, len(raw))` |
| `toNerSpan(text, ent)` | pfilter.go:70 | hot ×m | O(I) ×2 + O(1) + `byteSpan` | 0 when the label is mapped; **2 string allocs via `normalizeLabel` when unmapped** (46 of 54 multilingual labels by default) | lazy per-label cache |
| `ignored(label)` | pfilter.go:100 | hot ×2m | O(I) | 0 | - |
| `Recognizer(language)` | pfilter.go:113 | setup | O(1) | 1 | - |
| `Recognizer.SupportedEntities()` | pfilter.go:131 | hot when `Entities` set | O(I + L + S log S) | **2 maps + slice + sort per call** | memoize at `New` |
| `Recognizer.Analyze` / `AnalyzeWithArtifacts` | pfilter.go:139-151 | hot | O(E·S) + `ProcessText` / O(m·E) | `results` growth | pre-size |
| `contains` / `supportsAny` | pfilter.go:168-177 | hot | O(list) / O(have·want) | 0 | - |
| `goString(p)` | ffi.go:57 | hot ×m | O(ℓ) strlen + copy | 1 string per label | zero-copy `unsafe.String` for the map lookup; copy only unmapped labels |
| `loadLibrary(path)` | ffi.go:80 | setup (cached per path) | dlopen + 7 `RegisterLibFunc` | reflect closures; `MkdirAll` under the cache dir even when nothing is downloaded | - |
| `newFFIClassifier(cfg)` | ffi.go:143 | setup | P × `pf_load` | contexts, C strings | - |
| `ffiClassifier.classify(text, threshold)` | ffi.go:180 | hot | C(n) + purego trampoline + pool wait | **`strings.CString(text)` copies the whole text (n+1 bytes)** although `pf_classify` takes an explicit length; reflect boxing of 6 args; `&out`, `&n` escape (2); `results` (48·m bytes); m × `goString` | pass `unsafe.StringData(text)` + `runtime.KeepAlive`; reuse an out-struct per context |
| `ctxPool.acquire` / `release` | binding.go:45-50 | hot | O(1) channel ops; `acquire` blocks unboundedly when all P contexts are busy | 0 | optional `context.Context` for bounded wait |
| `newCtxPool` / `closeAll` | binding.go:35-58 | setup/teardown | O(P) | chan | - |
| `DefaultConfig(path)` | config.go:58 | setup | O(1) | 1 map | - |
| `Config.SupportedEntities()` | config.go:79 | hot when `Entities` set | O(I + L + S log S) | ~4–6 allocs | memoize |
| `normalizeLabel(label)` | config.go:99 | hot ×m (unmapped) | O(ℓ) ×2 | `ReplaceAll` + `ToUpper` | cache |
| `byteSpan(text, start, end)` | offsets.go:12 | hot ×m | O(1) on valid UTF-8 | 0 | - |
| `EnsureModel` / `EnsureLibrary` / `cachedFileValid` / `download` / `cacheDir` | download.go | setup | **sha256 over the whole file on every call** (1.6–2.8 GB model ≈ 1 s) | hasher, 32 KB copy buffer, hex string | size+mtime stamp or sidecar marker makes the hit path O(1) |

## `lookaround` (optional module)

| Function | Path | Time | Allocations | Note |
|---|---|---|---|---|
| `Compile` / `CompileWithTimeout` / `MustCompile` :26-48 | setup | O(\|pattern\|) | regexp2 program | - |
| `Matcher.String()` :57 | — | O(1) | 0 | - |
| `Matcher.FindAll(text)` :63 | **hot, once per lookaround pattern per Analyze** | `runeToByteMapper` O(n) + regexp2 `[]rune(s)` O(n) + backtracking scan: O(r·\|p\|) typical, **exponential worst case**, bounded only by `MatchTimeout` (1 s default, **per match attempt**, so m matches can take m × 1 s) | byte table `[]int` (8·(n+1) bytes) + closure; regexp2 `[]rune` (4·r bytes); mutex per `run`; per match: fresh `*Match` (3 + g allocs), `Groups()` builds and then copies a `[]Group` (≈ 2g allocs, ~160·g bytes), `spans [][2]int` → **≈ 3g + 5 allocs per match** vs 1 for `stdMatcher` | ASCII fast path (identity mapping, no table); `[]int32`; iterate `GroupByNumber` instead of `Groups()`; consider a lower default timeout for untrusted patterns |
| `runeToByteMapper(text)` :95 | hot | O(n) | `[]int` of n+1 + closure | ASCII fast path |
| `Pattern` / `NewRecognizer` :116-137 | setup | O(Σ\|pattern\|) | per-spec compile | - |

## `models`

Setup-only. `F` = pinned files (6), `B` = their total bytes (~261 MB).

| Function | Path | Time | Allocations | Note |
|---|---|---|---|---|
| `PinnedModels` / `IsPinned` / `PinnedFiles` / `Revision` / `Origin` / `License` | models.go:99-176 | O(P log P) / O(1) / O(F) / O(1) | small slices | - |
| `originFor(art, override)` :191 | O(\|origin\|) | `url.Parse` | - |
| `EnsureModel` / `EnsureModelIn` / `EnsureModelFrom` :229-260 | **O(B)** sha256 on a cache hit; O(2B + network) on a miss (hash while streaming, then the post-download check) | path strings, hasher, 32 KB buffer per file | re-hashes 260 MB on every call; `ner.New` and the CLI may both call it in one process |
| `VerifyModelIn(model, dir)` :313 | O(B) | same | - |
| `ResolveDir` / `DefaultDir` / `Dir` :362-391 | O(1) + `MkdirAll` | path strings | - |
| `fileMatches(path, sha)` | download.go:26 | O(b) streaming sha256 | `*os.File`, `*digest`, 32 KB buffer, 32 B sum, 64 B hex | - |
| `cachedFileValid(path, sha)` | download.go:50 | = `fileMatches` | - | - |
| `download(ctx, url, dest, sha, size, mode)` | download.go:59 | O(b + network), streamed through `LimitReader(size+1)` into temp file + hasher | request/response, temp file, `MultiWriter`, 32 KB buffer | `io.CopyBuffer` with a larger buffer would cut syscalls on the 260 MB file (minor) |

## `cmd/alcatraz`

`L` = lines, `N` = total input bytes, `M` = findings.

| Function | File:line | Path | Time | Allocations | Note |
|---|---|---|---|---|---|
| `main` / `run(args)` | main.go:12-21 | setup | flag parse + one `NewEngine` per process + Σ scan cost + report | flag values, `[]Finding` aggregate; in multi-file mode each file's slice is appended into the aggregate → **every Finding copied twice** | thread the aggregate slice into `scanFile` |
| `readAllowlist(path)` | main.go:98 | setup | O(file) | `ReadFile` + `string(data)` second copy | - |
| `newScanner(...)` | scan.go:37 | setup | `NewEngine` | map, scanner struct | - |
| `disableContext` / `excluded(file)` | scan.go:60-66 | setup / per `+++` header | O(1) / O(x · \|path\|) `path.Match` | `dir+"/"` concat per pattern | precompute prefixes |
| `analyzeLine(file, line, content, out)` | scan.go:83 | **hot, once per line** | `Analyze(n)` + O(m) | the engine's `[]Result` per line; `Finding.Value` is a substring of the line, so a finding pins its whole line copy | - |
| `scanDiff(r)` | scan.go:108 | hot per diff line | O(D) prefix checks + **`hunkRe.MatchString` on every line inside an active file** + `FindStringSubmatch` again on headers + `Analyze` per `+` line | `sc.Text()` copies every line, including context/header lines never analyzed; `[]string` per hunk header | guard with `HasPrefix(l, "@@")`; `sc.Bytes()` for non-`+` lines |
| `scanFile(path)` / `scanText(r)` | scan.go:152-172 | hot per line | O(L) + Σ `Analyze` — streaming, never whole-file | `os.File`, 64 KiB scanner buffer per input (grows to 10 MiB max line), one string copy per line, `[]Finding` per file | reuse the scanner buffer across files |
| `newLineScanner(r)` | scan.go:182 | per input | O(1) | 64 KiB buffer | pool |
| `runHook(args)` | hook.go:75 | setup | **`io.ReadAll` of stdin ≤ 10 MiB** (doubling growth ≈ 2N copied) | - | pre-size |
| `runClaudePost(...)` | hook.go:127 | per event | `json.Unmarshal` O(N) into `any` tree + optional chain process + `maskAny` + `emit` O(N) re-encode | **~3 copies of the payload** (raw, decoded tree with every string re-allocated, encoded output); `ToolInput json.RawMessage` copied but never read | drop the unused field |
| `runClaudePrompt(...)` | hook.go:181 | per event | O(N) decode + `maskString` | - | - |
| `runChainCmd(...)` | hook.go:220 | per event (with `-chain`) | process spawn + O(N) pipe + O(N) parse | second decoded tree | I/O bound |
| `maskAny(s, v, key, counts)` | hook.go:249 | hot, recursive over JSON nodes | O(nodes) + Σ `maskString` | 0 own (in-place mutation) | - |
| `maskString(s, text, counts)` | hook.go:278 | hot per string leaf | `Analyze(n)` + `sort.Slice` O(m log m) + **O(m·n)**: each masked result rebuilds the whole string via 3-way concat | per result: `mask()` (5) + one n-byte string; `sort.Slice` closure + Swapper | single forward pass into a `strings.Builder` sized `len(text)` → O(n + Σk), 1 alloc |
| `summarizeCounts(counts)` | hook.go:305 | per event | O(c log c) | `sort.Slice`, `Fprintf` boxing | micro |
| `emit(out)` | hook.go:326 | per event | O(N) encode; `encoding/json` sorts map keys per object | encoder buffer | - |
| `mask(s)` | report.go:12 | per finding | O(k) | **5**: `[]rune(s)`, `Repeat`, two rune-slice→string, concat | `utf8` offsets + one `Builder` → 1 alloc |
| `Finding.location()` | report.go:27 | per finding | O(\|file\|) | `Sprintf` (boxing 2 args + result ≈ 3) | `strconv` + `Builder` |
| `writeFindings(w, findings)` | report.go:45 | report | O(M) | **≈ 12 allocs + 1 `write(2)` syscall per finding** (unbuffered stdout) | `bufio.Writer` + `strconv.Append*` into a reused buffer |
| `writeJSON(w, findings)` | report.go:65 | report | O(M·k) build + O(M) encode | exact-size slice, `mask()` ×M, encoder buffer; single write | `mask` is the per-finding cost |
| `parseAllowlist` / `splitList` | report.go:86-98 | setup | O(\|content\|) | `strings.Split` slice + growth | - |
| `runModels*` / `pinsList` / `pins` / `download` / `verify` / `printf` / hint helpers / `humanSize` / `modelsUsage` | models.go | setup | O(F) report lines + the `models` package costs above | `Sprintf` boxing per line; one syscall per `printf` on stdout | `bufio.Writer` (trivial, ~20 lines) |

---

## Where to optimize

Ranked by measured impact on the default (pattern-only) `Analyze` path, then
by the per-call allocation count they remove. Each line names the function,
the evidence, and the change.

### Time

1. **74 full-text regex passes — `stdMatcher.FindAll` via `PatternRecognizer.Analyze`.**
   91 % of CPU at 10KB/dense, 69 % at 1MB/dense; throughput is a flat
   ~1 MB/s. Options, cheapest first:
   - Merge the duplicate digit-run patterns (`\b\d{11}\b` ×6, `\b\d{9}\b`
     ×3, AUTFN = AUACN, and the `\b\d{8..17}\b` family): one scan, dispatch by
     length to the validators. Removes ~15 of 74 passes with no semantic
     change.
   - Make the unused capture groups non-capturing (`Email` 6, `CreditCard`
     8, `DateTime` 3 each, `USITIN` 3, `IBAN (spaces)` 2). Fewer ints per
     match and a cheaper submatch path inside `regexp`.
   - A per-text class prefilter (text has no digit → skip the ~55
     digit-only patterns; no `@` → skip Email; no `:` → skip IPv6/URL)
     costs one O(n) byte scan and skips whole patterns on prose. Safe because
     a pattern that requires a byte class cannot match text lacking it.
   - A single combined scan (one alternation, or a tokenizer feeding
     per-class patterns only to candidate windows) is the big-step win but
     changes match semantics at boundaries; needs the parity benchmark in
     `bench/` to prove equivalence.
2. **`RemoveDuplicates` is O(M²).** 27 % of CPU at 1MB/dense
   (M ≈ 9 600). Sort by `(EntityType, Start, −End)` and sweep: O(M log M),
   identical output since only same-type spans ever interact. Also called
   once per recognizer before the engine-level call; single-pattern
   recognizers (34 of 52) cannot produce overlaps, so skip it there.
3. **`labelBefore` on every 8-digit run and UUID** (`BRCEPUnformatted`,
   `BRPixKey`): `ToLower` + `FieldsFunc` + `Join` per match. Numeric dumps
   hit this once per 8-digit token.
4. **`applyAllowList` in regex mode compiles the joined pattern on every
   call.** Cache the compiled regexp / exact-match set.

### Allocations

1. **`RemoveDuplicates` closure on empty input** — 52 of the 53 allocations
   a zero-result `Analyze` makes. `if len(results) < 2 { return results }`.
2. **`stdMatcher.FindAll` double copy** — `FindAllStringSubmatchIndex`
   already allocates one `[]int` per match; the `[]Match`/`[][2]int` re-copy
   doubles it (49 % of allocated objects at 10KB/dense). Return the stdlib
   slices or a flat stride buffer.
3. **`stripSeparators` builds a `strings.Replacer` per credit-card match** —
   24 % of bytes allocated at 10KB/dense. Delete it; `digitValues` already
   skips separators.
4. **`digitValues` allocates an `[]int` (8·s bytes) in 24 of 30 validators.**
   Stack buffer or byte-wise validation. Watch `validateABN`, which mutates
   the slice.
5. **`contextWords` re-splits and lower-cases every context phrase per call**
   (5.6 % of objects). Do it once in `WithContext`.
6. **`PatternRecognizer.SupportedEntities` allocates a one-element slice per
   recognizer per call when `Options.Entities` is set** (+51 allocs
   measured). Store it on the struct.
7. **`analyzeWithArtifacts` copies every recognizer's result slice into
   `all`.** Pre-size or append in place.
8. **Validators with avoidable string building**: `validateIBAN` (3–4),
   `validateNINO` (1–2), `validateSSN`/`validateITIN` (`ReplaceAll`),
   `INAadhaar` (double `digitValues`), `validateESNIE`,
   `validateFIPersonalCode`, `validateSGFIN`.

### Setup

- `NewEngine()` = 169 µs, 459 KB, 3 797 allocs — all 74 regex compiles.
  `alcatraz.NewEngine("en", "pt")` compiles them twice; the recognizers are
  language-independent and could be built once and registered under each
  language.

### Optional modules

- **ner**: every row is tokenized twice (`tokenWindows` for sizing, then
  inside hugot) and windowed rows three times. A byte-length short-circuit
  in `windows` removes the sizing pass for every segment ≤ `tokenBudget`
  bytes. `foldASCII` on non-ASCII text costs 8 bytes per input byte plus two
  allocations per non-ASCII rune. `Config.SupportedEntities` is rebuilt on
  every filtered `Analyze`.
- **pfilter**: `classify` copies the whole text to a C string although the
  C API takes a length; `goString` allocates per label just to key a map;
  `Config.SupportedEntities` is rebuilt per filtered `Analyze`.
- **lookaround**: two O(n) decodes per pattern per call and ~3g + 5 allocs
  per match; worst-case exponential time with a 1 s timeout **per match
  attempt**. ASCII fast path and `GroupByNumber` iteration are the cheap
  fixes.
- **anonymizer**: `AnonymizeWith` moves O(K·n) bytes and `resolve` makes
  O(m²) allocations even when nothing overlaps; a forward `Builder` pass and
  a disjointness check before allocating fix both.
- **cmd/alcatraz**: `maskString` is O(m·n) in copying; `writeFindings`
  does ~12 allocs and one syscall per finding on unbuffered stdout;
  `scanDiff` runs a regexp on every line of an active file.

## Reproducing

```sh
cd bench && go test -run '^$' -bench 'BenchmarkAnalyze|BenchmarkNewEngine' -benchmem -benchtime=200x ./
```

CPU and allocation profiles were taken with a small harness that loads
`bench/corpus.jsonl`, warms the engine, then runs `Analyze` under
`runtime/pprof` with `MemProfileRate = 1`; `go tool pprof -top` and
`-list` on `analyzer.RemoveDuplicates`, `stdMatcher.FindAll` and
`PatternRecognizer.Analyze` give the per-line attributions quoted above.
