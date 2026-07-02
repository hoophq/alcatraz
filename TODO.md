# TODO — Roadmap

Alcatraz today is **pattern-based only** (regex + checksum/format validators).
It has **no machine-learning engine, no NER, and no LLM** integration yet. This
file tracks the work to add them.

The architecture already anticipates this: every detector implements the
`analyzer.Recognizer` interface (`analyzer/recognizer.go`), which is the single
seam where statistical and LLM-backed backends plug in — no engine rewrite
required. The unsupported entity constants (`entities.Person`,
`entities.Location`, `entities.NRP`) already exist; they just have no recognizer
emitting them.

Guiding principle (same as the `lookaround` module): **keep the core
dependency-free and linear-time.** Any heavy runtime (ONNX, an LLM SDK, etc.)
lives in a **separate module** so importers of the core never pull the dep.

## Machine-learning / NER engine

- [ ] Define an `NERecognizer` implementing `analyzer.Recognizer` that emits
      `PERSON`, `LOCATION`, `NRP` (and other span-based entities).
- [ ] Pick an inference path that keeps the core clean:
  - [ ] In-process ONNX (e.g. `onnxruntime_go`) loading a quantized NER model, or
  - [ ] An out-of-process model server (gRPC/HTTP sidecar) so the runtime stays
        optional. Either way, ship it as a **separate module**, mirroring
        `alcatraz/lookaround`.
- [ ] Tokenization + offset mapping: convert model token spans back to **byte**
      offsets into the original text (the whole library is byte-indexed — reuse
      the rune→byte approach from `lookaround`).
- [ ] Map model labels → `entities.*` and calibrate per-label scores onto the
      0.0–1.0 scale the engine uses.
- [ ] Per-language models wired into the `Registry` via `SupportedLanguage()`
      (today every built-in is language-independent and registered under `en`).
- [ ] Tests + a small labeled fixture corpus with expected spans/scores.

## Context-aware scoring (precision prerequisite for NER)

- [ ] Activate the currently-inert context words: `PatternRecognizer.WithContext`
      stores words but does nothing. Implement context-based score enhancement
      (boost a match when context words appear near the span).
- [ ] Define cross-recognizer overlap handling: `analyzer.RemoveDuplicates` only
      de-duplicates **same-entity** overlaps today. Decide how an NER `PERSON`
      span and a pattern `EMAIL_ADDRESS` span interact when they overlap.

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
      contribution is measurable and regressions are caught.
- [ ] `NewEngine`/`Options` plumbing to enable/disable the ML and LLM backends
      without code changes.
- [ ] Extend the README "Extending" section with an ML/NER example once the first
      backend lands, and update the "Limitations" note.
