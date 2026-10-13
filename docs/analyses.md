# Complexity & allocation analysis

A function-by-function pass over every component in the `Analyze` flow and
its satellites, with time complexity and heap-allocation behaviour per call,
so the places worth optimizing are visible at a glance. Numbers in the
"Measured" section come from the benchmark corpus in `bench/`; everything
else is read from the code. The tables describe the current tree; the
[ledger](#where-to-optimize) at the end records what was changed from the
first version of this analysis and what is deliberately left.

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

| Size / density | ns/op (before → after) | MB/s | B/op (before → after) | allocs/op (before → after) | results/op |
|---|---:|---:|---:|---:|---:|
| 100B / none | 110 k → 2.8 k | 0.91 → 35.6 | 1 470 → 10 | 53 → 0 | 0 |
| 100B / dense | 139 k → 34 k | 0.72 → 2.9 | 2 216 → 591 | 64 → 5 | 0.6 |
| 1KB / none | 991 k → 25 k | 1.03 → 40.2 | 1 470 → 195 | 53 → 0 | 0 |
| 1KB / dense | 1.05 M → 761 k | 0.97 → 1.35 | 16 417 → 3 529 | 153 → 36 | 8.8 |
| 10KB / none | 10.3 M → 277 k | 0.99 → 37.0 | 1 613 → 421 | 53 → 0 | 0 |
| 10KB / sparse | 10.7 M → 8.2 M | 0.95 → 1.25 | 22 084 → 4 375 | 191 → 48 | 12.3 |
| 10KB / dense | 11.2 M → 8.5 M | 0.91 → 1.20 | 153 919 → 36 012 | 596 → 171 | 94 |
| 1MB / none | 1.10 s → 28.5 ms | 0.96 → 36.8 | 1 338 → 40 | 53 → 0 | 0 |
| 1MB / sparse | 1.20 s → 846 ms | 0.88 → 1.24 | 1.8 M → 462 k | 4 671 → 1 269 | — |
| 1MB / dense | 1.65 s → 899 ms | 0.64 → 1.17 | 15.5 M → 4.67 M | 36 834 → 9 776 | 9 575 |
| OCR line / prose (60 B, 51-entity filter) | — → 2.0 k | 29.6 | 195 | 0 | 0 |
| OCR line / digits | — → 50.6 k | 1.19 | 666 | 10 | — |
| `NewEngine()` | 169 k → 97 k | — | 459 224 → 177 672 | 3 797 → 2 362 | — |

"Before" is the first version of this analysis; "after" is the current
tree, which the per-function tables describe. The parity run in `bench/`
(`benchgo -mode detect`, 23 851 detections) is byte-identical across the
two.

What the table shows:

1. **Prose is ~37 MB/s and allocation-free.** A text with no digit skips
   every numeric pattern before any regex runs; what remains is one byte
   pass for the class mask and a handful of literal-prefixed patterns.
2. **Dense text is bounded by the stdlib `regexp` scan.** At 10KB/dense,
   80 % of CPU is `regexp.(*Regexp).find` for the ~55 patterns that are not
   bare digit runs, and 80 % of the remaining allocations are the one
   `[]int` per match that `FindAllStringSubmatchIndex` returns. Everything
   on this side of that API is gone.
3. **Match density no longer has a quadratic term.** 1MB/dense is 0.90 s
   against 0.85 s for 1MB/sparse; the pre-change gap (1.65 s vs 1.20 s) was
   the O(M²) de-duplication.

## Flow and per-stage cost

```
Engine.Analyze(text, o)
│  Registry.Recognizers(lang, o.Entities)         O(1) map hit; O(R·E) on first use of a filter
│  [NlpEngine.ProcessText]                         C(n)     only with ner/pfilter attached
└─ analyzeWithArtifacts
   │  byteClasses(text)                            O(n)     0 allocs; one table lookup per byte
   ├─ for each recognizer: PatternRecognizer.appendResults
   │    for each pattern:
   │       skip if text lacks a required class        O(1)
   │       \b\d{n}\b pattern → shared digit runs       O(n) once per call, then O(runs)
   │       shared regex source → scanCache hit         O(1)
   │       else stdMatcher.FindAll                     O(n·|re|)  1 + m allocs     ← 80 % CPU on dense
   │       validator(match)                            O(s)       0 allocs
   │       contextValidator(text, start, end)          O(1)       0 allocs
   ├─ WordContextEnhancer.Enhance                   O(M·k)     1 map + 1 window slice per call
   ├─ removeDuplicates (all results)                O(M log M) in place; exported RemoveDuplicates copies first
   ├─ threshold filter                              O(M)       in place
   ├─ allowList.apply                               O(M)       prepared list reused across calls
   └─ fill Result.Text                              O(M)       substrings, no copy
```

---

## `alcatraz` (root)

| Function | Path | Time | Allocations | Note |
|---|---|---|---|---|
| `NewEngine(languages...)` `alcatraz.go:30` | setup | O(langs · (R + P)) + one `regexp.Compile` per distinct regex source, process-wide | measured 178 KB / 2 362 allocs for `en` | `compileProgram` caches programs by source, so a second language reuses the compiled regexes; the `Pattern` and recognizer structs are still rebuilt per language. |

## `analyzer`

### engine.go

| Function | Path | Time | Allocations | Note |
|---|---|---|---|---|
| `NewEngine(registry, languages)` :47 | setup | O(langs) | `Engine`, copy of `languages`, `WordContextEnhancer` | - |
| `SetThreshold` / `SetNlpEngine` / `SetContextEnhancer` :57-71 | setup | O(1) | 0 | - |
| `Languages()` :74 | - | O(langs) | 1 slice copy | - |
| `SupportedEntities(language)` :77 | - | O(R + S log S) | map + slice (see `Registry.SupportedEntities`) | - |
| `Analyze(text, o)` :88 | hot | `Recognizers` + optional `ProcessText` + `analyzeWithArtifacts` | 0 own | Entry point. |
| `AnalyzeBatch(texts, o)` :114 | hot | T × `analyzeWithArtifacts` + one `ProcessTexts` when batchable | `artifacts []*NlpArtifacts` (T), `results [][]Result` (T) | - |
| `wantsArtifacts(recs)` :146 | hot (only when an NLP engine is set) | O(R) type assertions | 0 | - |
| `analyzeWithArtifacts(text, o, recs, artifacts)` :158 | hot | `byteClasses` O(n) + Σ `appendResults` + `Enhance` + `removeDuplicates` O(M log M) + O(M) threshold + O(M) allow list | one `scanCache` on the stack (escape analysis verified); `all` grows by `append` as recognizers write into it (~log₂ M reallocs); non-pattern recognizers' slices are still copied in | The engine-level dedup subsumes the per-recognizer one, which is skipped on this path. |
| `prepareAllowList(o)` :226 | hot when `AllowList` set | O(A) element-wise compare on a hit; O(A) + `regexp.Compile` on a miss | hit: 0; miss: `slices.Clone` of the list + compiled regexp or `map[string]struct{}` | Single cached entry (`atomic.Pointer`): the common case is one list on every call. Alternating lists rebuild each time, as before. |
| `allowList.apply(results, text)` :245 | hot when `AllowList` set | O(M·\|re\|) regex mode, O(M) exact | 0 (in place) | - |

### registry.go

| Function | Path | Time | Allocations | Note |
|---|---|---|---|---|
| `NewRegistry(languages...)` :25 | setup | O(langs) | map + slice copy | - |
| `Add(language, rec)` :43 | setup | O(1) amortized | slice growth; clears the filter cache | - |
| `Recognizers(language, entities)` :57 | hot | `entities == nil`: O(1). Filtered: O(E) key build in a 1 KiB stack buffer + map hit; O(R·E) on first use of a filter | hit: 0 (key exceeding the stack buffer spills once per call); miss: `out` slice + cache entry. Cache dropped (not evicted) past 64 entries | Key elements are length-prefixed (`appendKeyed`), so an entity name containing the separator cannot alias another list. |
| `SupportedEntities(language)` :94 | - | O(R + S log S) | `seen` map, `out` slice, sort | - |
| `Languages()` :110 | - | O(langs) | 1 slice copy | - |

### recognizer.go

| Function | Path | Time | Allocations | Note |
|---|---|---|---|---|
| `supportsAny(have, want)` :22 | hot (filtered calls) | O(\|have\|·\|want\|) | 0 | Fine for \|have\| = 1. |

### pattern.go

| Function | Path | Time | Allocations | Note |
|---|---|---|---|---|
| `NewPattern(name, regex, score)` :34 | setup | `compileProgram` (cache hit O(1), else `regexp.Compile`) + one `syntax.Parse` for the class mask and digit-run shape | `*Pattern`; program only on first use of a source | `requires` and `run` are derived here, once. |
| `MustPattern(...)` :51 | setup | = `NewPattern` | + string concat on panic | - |
| `NewPatternMatcher(name, m, score)` :63 | setup | O(1) | `*Pattern` | Custom matchers get no class mask and no digit-run shortcut. |
| `WithGroup(group)` :69 | setup | O(1) | 0 | - |

### classes.go

| Function | Path | Time | Allocations | Note |
|---|---|---|---|---|
| `byteClasses(text)` :54 | hot, once per `Analyze` | O(n), one `[256]` table lookup per byte | 0 | Bits for digit, upper, lower, letter, space and each printable ASCII punctuation byte. |
| `patternRequires(regex)` / `requiredClasses(re)` :72-86 | setup | O(\|regex tree\|) | parse tree | Conservative: optional parts, alternation branches that disagree, case-fold orbits with non-ASCII members and any non-ASCII rune require nothing. `TestPrefilterNeverSkipsCorpusMatch` checks every built-in pattern against the bench corpus. |
| `literalClasses` / `charClassClasses` / `runeClasses` / `buildByteClass` | setup | O(1) each | 0 | - |

### matcher.go

| Function | Path | Time | Allocations | Note |
|---|---|---|---|---|
| `NewMatch(loc)` :24 / `Match.NumGroups()` :27 / `Match.Span(g)` :31 | hot | O(1) | 0 | `Match` holds the stdlib start/end pair slice; group g is `loc[2g], loc[2g+1]`. **Breaking for custom `Matcher`s**: the exported `Groups [][2]int` field is gone; build matches with `NewMatch` (see `lookaround`). |
| `compileProgram(regex)` :75 | setup | map hit O(1), else `regexp.Compile` | program on first use; the process-wide map stops growing at `programCacheSize` (1 024) | Patterns past the bound still compile and work, they just do not share a scan. |
| `stdMatcher.FindAll(text)` :100 | **hot, per non-shared regex pattern** | **O(n·\|re\|)**; bitstate backtracker under Go's size cap, NFA above. `\b`-prefixed patterns get no literal-prefix skip. | `FindAllStringSubmatchIndex`: outer slice (1) + one `[]int` per match (m); `[]Match` (1) adopting them. **m + 2 allocs**, no re-copy. The per-match `[]int` is stdlib-internal. | 80 % of CPU at 10KB/dense. Unused capture groups are now non-capturing, so each row is 2 ints. |
| `parseDigitRun(re)` :126 | setup | O(1) on the parse tree | 0 | Accepts exactly `Concat[\b, Repeat{min≥1,max}([0-9]), \b]` or `\b\d+\b`. |
| `digitRuns(buf, text)` :162 / `countDigitRuns(text)` :189 | hot, once per `Analyze` when any digit-run pattern survives the class filter | O(n) byte walk | 0 into the stack buffer (32 runs); heap slice only beyond that | Maximal digit runs with no ASCII word byte on either side — exactly `\b\d+\b`. `TestDigitRunsMatchRegex` checks equivalence against `regexp` on 2 000 random texts. |
| `matchDigitRuns(runs, r)` :210 | hot, per digit-run pattern | O(runs) | 2 (one flat offset buffer + `[]Match`), 0 when nothing matches | Replaces the regex for 16 built-in patterns. |
| `scanCache.findAll(p, text)` :257 | hot, per pattern | digit run → above; shared program → O(8) linear probe; else `FindAll` | 0 own | Lives on the engine's stack frame (`-gcflags=-m`: `scans does not escape`). 8 shared-program slots; the built-in set uses 4. |

### pattern_recognizer.go

| Function | Path | Time | Allocations | Note |
|---|---|---|---|---|
| `NewPatternRecognizer(...)` :38 | setup | O(1) | struct + one-element `entities` slice | - |
| `WithContext(words...)` :60 | setup | O(Σ\|entry\|) `splitContext` | copy of `words` + phrases + lower-cased words, once | A snapshot: later changes to the caller's slice do not reach the recognizer. Was split per `Enhance`. |
| `WithValidator` / `WithContextValidator` :70-78 | setup | O(1) | 0 | - |
| `Context()` / `SupportedEntities()` :84-92 | public | O(1) | 1 copy each | Fresh slices; the engine and registry read the recognizer's own fields, so neither is on the hot path. |
| `Name()` / `SupportedLanguage()` :87-95 | hot | O(1) | 0 | - |
| `Analyze(text, entities)` :107 | hot only when called directly (not via the engine) | `byteClasses` + `appendResults` + `removeDuplicates` | own `scanCache` on the stack; `results` growth | Public contract keeps the per-recognizer dedup. |
| `appendResults(dst, text, classes, scans)` :121 | **hot, ×R** | per pattern: class check O(1); `scans.findAll`; per match validator O(s) + contextValidator | appends `Result` (72 B) into the caller's slice; validators allocate nothing | Engine path. |

### result.go

| Function | Path | Time | Allocations | Note |
|---|---|---|---|---|
| `Result.Len` / `Intersects` / `ContainedIn` / `Contains` / `EqualIndices` :33-55 | hot | O(1) | 0 | `Result` is 72 bytes; sorts move all of it. |
| `RemoveDuplicates(results)` :66 | public | `slices.Clone` + `removeDuplicates` | one copy of the input | The input is left untouched. |
| `removeDuplicates(results)` :80 | **hot, once per `Analyze`** | `< 2` results: O(1). Else **O(M log M)**: stable sort by `(Start, −End)`, one sweep with a per-type last-kept table (linear, T types), stable sort by `(−Score, Start, −Len)` | **0**: in place on the input slice (`slices.SortStableFunc`, no reflection); the per-type table is a 16-entry stack array, heap only beyond | Types are not part of the sort key, so results tied on score, start and length keep their input order across types — the anonymizer resolves such overlaps by order. Kept spans of a type have strictly increasing ends, so containment in any kept span of that type implies containment in its last one. Identical spans keep the higher score, earlier on a tie. |

### context.go

| Function | Path | Time | Allocations | Note |
|---|---|---|---|---|
| `NewWordContextEnhancer()` :100 | setup | O(1) | 1 | - |
| `Enhance(text, results, recs, _)` :111 | hot (when `M > 0`) | `contextWords` + Σ over boostable results of (`appendWordsBefore` + `appendWordsAfter` + `supported`) = O(R + M·(k·w̄ + C·k)); a huge token before a match makes one window O(n) | one `window` slice (k strings) + `contextWords` + `foldWord` lower-casing only for words with upper-case bytes and ≤ `maxWord` | Bounded against the base64-blob case (see comment in source). |
| `contextWords(recs, results)` :172 | hot (when `M > 0`) | O(M + R) | `byRecognizer` map (1 + growth) | `*PatternRecognizer` supplies pre-split phrases; any other `ContextualRecognizer` is split per call via `splitContext`. |
| `splitContext(entries)` :213 | setup (via `WithContext`) | O(Σ\|entry\|) | phrases + lower-cased words | - |
| `supported(window, phrases)` :229 | hot | O(C · k · \|phrase\|) | 0 | - |
| `containsPhrase(window, phrase)` :240 | hot | O(k · \|phrase\|) | 0 | - |
| `wordMatches(word, context)` :270 | hot | O(\|context\|) | 0 | - |
| `foldWord(s, maxWord)` :297 | hot | O(\|s\|) if folded, O(1) otherwise | 0–1 (`ToLower`, only when needed) | - |
| `appendWordsBefore(dst, text, end, n, maxWord)` :307 | hot | O(bytes walked back to the k-th word boundary); rune-decoded | 0 own + `foldWord` | ASCII fast path for `isWordRune` would cut the per-rune `unicode` lookups (undone, minor). |
| `appendWordsAfter(...)` :367 | hot (only if `WordsAfter > 0`; default 0) | same shape forward | same | - |
| `splitWords(s)` :413 | setup via `splitContext` | O(\|s\|) | `words` slice + one `ToLower` string per word | - |
| `isWordRune(r)` :442 | hot (per rune walked) | O(1) — three `unicode` table lookups | 0 | - |

### nlp.go

Interfaces and plain structs only (`Token`, `NerSpan`, `NlpArtifacts`,
`NlpEngine`, `BatchNlpEngine`, `ArtifactRecognizer`). No runtime cost; the
implementations are in `ner` and `pfilter` below.

## `recognizers`

All 52 constructors are **setup**: each calls `analyzer.MustPattern`, so the
patterns are built at `All()`/`LoadDefaults` time, not at package init, once
per language loaded; the compiled regex behind each source is cached
process-wide, so a second language pays only the `Pattern` and recognizer
structs. The table below lists what matters at runtime: pattern count, base
score, and the validator that runs per match. Capture groups not consumed by
`WithGroup` are non-capturing.

### Constructors

| Constructor | File | Patterns (score) | Hot per-match code |
|---|---|---|---|
| `Email` | generic.go:17 | 1 (0.5) — largest RE2 program in the set | none |
| `Phone` | generic.go:31 | 3 (0.5 each), US/BR use `WithGroup(1)` | none |
| `CreditCard` | generic.go:57 | 1 (0.3) | `luhnValid` |
| `Crypto` | generic.go:67 | 2 (0.5) | none |
| `IP` | generic.go:78 | 2 (0.6) | none |
| `URL` | generic.go:90 | 1 (0.5) | none |
| `DateTime` | generic.go:102 | 4 (0.6–0.8) | none |
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
| `INAadhaar` | india.go:9 | 1 (0.6) | `validateAadhaar` |
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

**Shared digit runs.** Sixteen patterns are exactly `\b\d{n}\b` or
`\b\d{a,b}\b` (`\b\d{11}\b` ×6, `\b\d{9}\b` ×3, and one each of 8, 8–17,
12, 13, 14, 15). None of them runs a regex: the engine finds every bare
digit run once per `Analyze` and each pattern selects by length
(`analyzer/matcher.go`). The remaining digit-bearing patterns that share a
source (AUTFN = AUACN) share one compiled program and one scan per call.

### defaults.go

| Function | Path | Time | Allocations | Note |
|---|---|---|---|---|
| `LoadDefaults(reg, language)` :13 | setup | O(R + P·compile) | everything in `All()` + 52 `Registry.Add` | Called once per language by `alcatraz.NewEngine`. |
| `All()` :20 | setup | O(R + P·compile) | 10 group slices, `recs` grown by `append`, 52 recognizers, 74 patterns + regexps | - |
| `generic` … `other` :32-80 | setup | per group | 1 slice each | - |

### checksum.go (hot helpers)

| Function | Path | Time | Allocations | Note |
|---|---|---|---|---|
| `digitValues(dst, s)` :7 | hot (most validators) | O(s) | 0: appends into the caller's stack array (`var buf [n]int; digitValues(buf[:0], s)`) | Grows only if the match has more digits than the buffer, which the regexes bound. |
| `digitsExactly(dst, s, n)` :19 | hot | O(s) | 0; never grows `dst` past `n` | - |
| `digitsInt(s)` :38 | hot (ES, FI) | O(\|s\|) | 0 | Sign-free, error-free `Atoi` for a digit run. |
| `verhoeffValid(s)` / `verhoeffDigits(ds)` :80-86 | hot (Aadhaar) | O(s) + O(d) | 0 | Tables are package arrays. |
| `luhnValid(s)` :96 | hot (CreditCard) | O(s) + O(d) | 0 | Separators are skipped by `digitValues`; the former `stripSeparators` is gone. |

### Validators (hot, once per match)

Every row is `testing.AllocsPerRun == 0`, pinned by
`TestValidatorsDoNotAllocate`; the rewrites were differential-fuzzed against
the previous implementations on every regex-reachable input.

| Function | File | Time | Allocations | Note |
|---|---|---|---|---|
| `validateIBAN` / `mod97Push` | generic.go:135-183 | O(s) | 0 | Streams mod-97 over the original bytes, `s[4:]` then `s[:4]`, skipping spaces; letters expand to two digits inline. |
| `ssnDigits` / `validateSSN` / `validateITIN` | us.go:23-76 | O(s) | 0 | Byte walk into a `[9]int`. |
| `validateABA` | us.go:132 | O(s) | 0 | - |
| `validateNHS` | uk.go:19 | O(s) | 0 | - |
| `validateNINO` | uk.go:46 | O(s) | 0 | Rune walk skipping `unicode.IsSpace` into a `[9]byte`. |
| `validateTFN` / `validateABN` / `validateACN` / `validateMedicare` | au.go | O(s) | 0 | `validateABN` mutates `ds[0]`; the slice is private to the call. |
| `validateAadhaar` | india.go:16 | O(s) | 0 | One `digitsExactly`, then `verhoeffDigits` on the same slice. |
| `validateITFiscalCode` | italy.go:20 | O(15·26) `IndexByte` over const tables | 0 | A `[256]int8` table would cut the compares (undone, minor). |
| `validateITVAT` | italy.go:61 | O(s) | 0 | - |
| `validateESNIF` / `validateESNIE` | spain.go:21-41 | O(8) | 0 | `digitsInt(s[1:8])` plus the lead digit arithmetically. |
| `validateSGFIN` | singapore.go:17 | O(7) | 0 | Indexes `s[1..7]` directly; the regex guarantees digits. |
| `validateBRCPF` / `BRCNPJ` / `BRCNH` / `BRPIS` / `BRCNS` / `BRTitulo` / `BRRenavam` | brazil.go | O(s) + O(d) | 0 | - |
| `mod11Weighted` / `allEqual` / `validDefinitiveBRCNS` / `weightedSum15` | brazil.go | O(d) | 0 | - |
| `labelBefore` / `labelAt` / `wordEqualsLower` / `isWordRune` | brazil.go:396-457 | O(w + L·F), w = 24-byte window, F ≤ 13 words | 0: word spans in a `[13][2]int` stack array; comparison lower-cases the text rune by rune into a `[4]byte` | Runs on every `\b\d{8}\b` match and every UUID. 300 k fuzzed windows, 0 mismatches against the old `ToLower`/`FieldsFunc`/`Join` version. |
| `validatePESEL` / `validateKRRRN` / `validateTHTNIN` | others.go | O(s) | 0 | - |
| `validateFIPersonalCode` | others.go:65 | O(1) | 0 | `digitsInt` on each run. |

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
the evidence, the change, and whether it has landed.

### Time

1. **74 full-text regex passes — `stdMatcher.FindAll` via `PatternRecognizer.Analyze`.**
   91 % of CPU at 10KB/dense, 69 % at 1MB/dense; throughput was a flat
   ~1 MB/s. Options, cheapest first:
   - ✅ Duplicate digit-run patterns share one compiled program and one scan
     per `Analyze` (`program`, `scanCache` in `analyzer/matcher.go`).
   - ✅ Unused capture groups are non-capturing (`Email`, `CreditCard`,
     `DateTime`, `USITIN`, `IBAN (spaces)`).
   - ✅ Per-text byte-class prefilter (`analyzer/classes.go`): a pattern
     whose matches must contain a class the text lacks is not scanned. This
     is the 35× on prose.
   - ✅ Shared digit-run scan: the 16 patterns of the exact form
     `\b\d{min,max}\b` are served from one byte pass per `Analyze`
     (`digitRuns`, `matchDigitRuns`), no regex at all.
     `TestDigitRunsMatchRegex` proves equivalence against `regexp` on
     2 000 random texts. A general combined scan for the other patterns
     stays undone: it would change boundary semantics.
   - ⬜ What is left is the stdlib `regexp` scan for the ~55 remaining
     patterns: 80 % of CPU at 10KB/dense. Further gains need fewer or
     narrower patterns, not engine work.
2. ✅ **`RemoveDuplicates` is O(M log M)** — sort by `(Start, −End)`, one
   sweep with a per-type last-kept table, in place; was 27 % of CPU at
   1MB/dense. The type is not in the sort key so ties across types keep
   input order (the anonymizer depends on it). Per-recognizer dedup is
   skipped on the engine path since the engine's pass subsumes it.
3. ✅ **`labelBefore`** — allocation-free byte walk; differential-fuzzed
   against the old implementation (300 k windows, 0 mismatches).
4. ✅ **Allow list** — prepared once (`prepareAllowList`) and reused while
   `Options.AllowList` is unchanged; 22 allocs/call → 1.

### Allocations

1. ✅ `RemoveDuplicates` closure on empty input — early return.
2. ✅ `stdMatcher.FindAll` double copy — `Match` adopts the stdlib `[]int`.
   **Breaking**: the exported `Match.Groups` field is gone; custom
   `Matcher`s build matches with `NewMatch(loc []int)` (`lookaround` is
   the reference). The exported `RemoveDuplicates` still copies its
   input; the engine uses the in-place `removeDuplicates`.
3. ✅ `stripSeparators` / `strings.Replacer` per credit-card match — deleted.
4. ✅ `digitValues` — append-style into a caller's stack array; every
   validator is `AllocsPerRun == 0` (`TestValidatorsDoNotAllocate`).
5. ✅ `contextWords` — phrases split once in `WithContext`.
6. ✅ `PatternRecognizer.SupportedEntities` — slice stored on the struct;
   `Registry.Recognizers` memoized per filter.
7. ✅ `analyzeWithArtifacts` — recognizers append into one engine-owned
   slice (`appendResults`).
8. ✅ Validator string building — `validateIBAN` streams mod-97 over the
   original bytes; `validateNINO`, `validateSSN`/`validateITIN`,
   `INAadhaar`, `validateESNIE`, `validateFIPersonalCode`, `validateSGFIN`
   index bytes directly.
9. ⬜ The one `[]int` per match inside `regexp.FindAllStringSubmatchIndex`
   (80 % of remaining allocations on dense text) — stdlib-internal; no
   public API returns matches into a caller-owned buffer.

### Setup

- ◐ `NewEngine()` = 97 µs, 178 KB, 2 362 allocs (was 169 µs / 459 KB /
  3 797): `compileProgram` caches compiled regexes by source, so a second
  language reuses them; the `Pattern`/recognizer structs are still rebuilt
  per language.

### Optional modules

- **ner**: ✅ `windows()` short-circuits a segment whose byte count plus the
  special tokens fits the budget, so short lines and cells skip the sizing
  tokenizer pass; ✅ `SupportedEntities` is computed once per `Recognizer`.
  ⬜ Rows that do need windowing are still tokenized twice (sizing, then
  inside hugot) and windowed rows three times; ⬜ `foldASCII` on non-ASCII
  text costs 8 bytes per input byte plus two allocations per non-ASCII
  rune; ⬜ reflect-based sorts in `mergeSpans`/`snapToWords`.
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
