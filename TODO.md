# TODO — Roadmap

Alcatraz's core is **pattern-based** (regex + checksum/format validators).
Statistical NER shipped as the opt-in **`alcatraz/ner`** module; LLM-backed
detection is still to come. This file tracks the remaining work.

The architecture seam: every detector implements `analyzer.Recognizer`
(`analyzer/recognizer.go`), and detectors that consume a shared NLP pass
additionally implement `analyzer.ArtifactRecognizer` (`analyzer/nlp.go`). An
`analyzer.NlpEngine` attached via `Engine.SetNlpEngine` runs **once per
Analyze call** and its `NlpArtifacts` are shared by every artifact-aware
recognizer — no duplicate inference, zero cost on the pattern-only path.

Guiding principle (same as the `lookaround` module): **keep the core
dependency-free and linear-time.** Any heavy runtime (ONNX, an LLM SDK, etc.)
lives in a **separate module** so importers of the core never pull the dep.

## Machine-learning / NER engine

- [x] NLP seam in the core: `NlpEngine`, `NlpArtifacts`, `ArtifactRecognizer`
      and `Engine.SetNlpEngine` — single shared inference pass per Analyze,
      lazy (skipped entirely when no artifact recognizer applies), degrades
      to pattern-only on inference failure.
- [x] `ner` module (separate module, mirrors `lookaround`): hugot-backed
      ONNX token-classification engine, pure-Go backend by default (no cgo),
      `-tags ORT` for ONNX Runtime throughput.
- [x] Emits `PERSON`, `LOCATION`, `NRP`, `DATE_TIME` with Presidio-compatible
      label mapping (`PER`→`PERSON`, `GPE`→`LOCATION`, `NORP`→`NRP`, …);
      `ORGANIZATION` and CoNLL `MISC` ignored by default, low-score
      multiplier and ignore list configurable via `ner.Config`.
- [x] Byte-offset guarantee: model spans are remapped so
      `text[r.Start:r.End] == r.Text` holds on multi-byte input. (The pure-Go
      tokenizer's span tracking miscounts multi-byte runes — see the upstream
      note below — so the model runs on an ASCII-folded rendering and spans
      are mapped back through the fold table; `ner/offsets.go`.)
- [ ] Upstream: go-huggingface `hftokenizer` BertNormalizer builds its
      normalized→original offset table with one entry per **rune** instead of
      per **byte**, shifting all subsequent token spans on multi-byte input.
      File an issue/PR; once fixed upstream the ASCII-fold workaround can be
      retired.
- [x] Word-boundary guarantee: spans are grown to the word they sit inside and
      same-type spans that then touch are unioned, so a subword split never
      reaches a caller as half a name (`ner/words.go`).
- [ ] Upstream: hugot `getGoTokens` (`backends/tokenizer_go.go`) decodes each
      token id on its own, so the WordPiece decoder strips the `##` marker
      before `gatherPreEntities` compares token length to the source slice.
      `isSubword` is therefore always false on `BackendGo` and every
      word-aggregating strategy (FIRST/MAX/AVERAGE) is inert there — all four
      strategies return byte-identical output, silently. The Rust tokenizer
      path does not go through `getGoTokens` and keeps the marker. Reported as
      [knights-analytics/hugot#136](https://github.com/knights-analytics/hugot/issues/136)
      (fix: decode the batch in one call, or keep the raw tokens alongside the
      ids); nothing here waits on it. Independently, SIMPLE aggregation never
      consults `isSubword` at all and splits on every `B-` tag, so
      `ner/words.go` stays either way.
- [x] `pfilter` module (separate module): privacy-filter.cpp backend over
      purego FFI (no cgo — cross-compiles everywhere; the dlopen path builds
      on 64-bit darwin/linux, everything else gets a clean "unsupported" stub).
      PII-specialized GGUF models (8 categories base, 54 multilingual), byte
      offsets straight from the runtime, GPU via `Config.Device`, long inputs
      via `Config.WindowTokens`. Live test gated by `ALCATRAZ_PF_LIVE=1` +
      `PF_LIBRARY` + `PF_MODEL`.
- [x] `pfilter`: binding validated end to end against a real `libpf`
      (built via the `pfilter/dist` CMake wrapper) + the q8 GGUF —
      `TestLivePrivacyFilter` passes, offsets hold on multi-byte input.
- [x] `pfilter` distribution: `EnsureLibrary` (prebuilt, sha256-pinned
      `libpf` from the `libpf-vN` GitHub release; built by the manual
      `libpf-release.yml` workflow) and `EnsureModel` (GGUF from Hugging
      Face, verified against the published LFS sha256) — users need neither
      cmake nor a manual download. `pfilter/dist` builds one self-contained
      shared library (ggml linked statically, force-loaded pf archive).
- [x] `pfilter`: `libpf-v1` published (all four platforms built + smoke-
      tested by `libpf-release.yml`) and its checksums pinned in
      `pfilter/download.go:libraryChecksums` — `EnsureLibrary` verified end
      to end against the release (download, hash check, dlopen, inference).
- [x] `pfilter`: ctx pool instead of one mutex-guarded pf_ctx —
      `Config.PoolSize` (default 1) contexts behind a channel pool; classify
      calls run in parallel up to the pool size, Close waits for in-flight
      calls. Memory scales with PoolSize (each pf_ctx loads the model).
- [ ] Per-language model routing: one `ner.Engine` per language wired through
      `SupportedLanguage()` (today: one model per engine, registered per
      language by the caller).
- [ ] Zero-shot PII models (GLiNER-class, e.g. `knowledgator/gliner-pii-edge`):
      runtime-defined entity types without retraining. Needs custom span
      decoding against the ONNX graph (not a plain token-classification
      pipeline).

## Context-aware scoring (precision follow-up)

- [ ] Activate the currently-inert context words: `PatternRecognizer.WithContext`
      stores words but does nothing. Implement context-based score boosting
      fed from the shared `NlpArtifacts.Tokens` (Presidio boosts +0.35 when a
      recognizer's context words appear near the span; plain lowercase token
      matching is a fine v1, lemmas later).
- [ ] Populate `NlpArtifacts.Tokens` in the `ner` module (hugot's aggregated
      output currently yields entity spans only).
- [ ] Cross-recognizer overlap handling: `analyzer.RemoveDuplicates` only
      de-duplicates **same-entity** overlaps today. Decide how an NER `PERSON`
      span and a pattern `EMAIL_ADDRESS` span interact when they overlap
      (proposed rule: a checksum-validated pattern result suppresses an
      overlapping statistical span).

## LLM-based detection / validation

- [ ] Optional LLM recognizer (separate module) behind a provider interface
      (OpenAI / Anthropic / local) — never a core dependency.
- [ ] Use cases: (a) catch free-text PII patterns/NER miss; (b) a second-opinion
      validator on ambiguous spans (an LLM-backed `ContextValidator`).
- [ ] Structured output (JSON spans) → map back to byte offsets and `Result`.
- [ ] Guardrails: timeout, retries, token/cost budget, response caching,
      deterministic decoding settings.
- [ ] **Privacy:** sending text to a third-party LLM for PII detection is itself
      a data-exposure decision. Make it explicit opt-in, document it clearly, and
      support local-only models.

## Cross-cutting

- [ ] Precision/recall benchmark suite against a labeled dataset so each backend's
      contribution is measurable and regressions are caught (the `ner` live test
      fixture is a seed, not a benchmark).
- [x] CI: unit tests for all four modules on every push (`test.yml` matrix);
      live model tests (`ALCATRAZ_NER_LIVE=1`, `ALCATRAZ_PF_LIVE=1`) in
      `ml-live.yml` — PRs touching ML modules, weekly schedule, manual
      dispatch — with `libpf` build and GGUF/ONNX model caches.
- [ ] Extend the README "Extending" section with a custom-model `ner.Config`
      example once model routing lands.
