# Design & limits

## Scope

Alcatraz is a **pattern engine**: regexes plus checksum validators, verified
against the real scheme behind each identifier. That makes it precise on
structured identifiers and honest about the rest:

- **ML ships as an opt-in module.** Free-text entities (`PERSON`, `LOCATION`,
  `NRP`) require the separate [`ner` module](ner.md); the core alone does not
  emit them. Statistical detection is probabilistic, so treat NER scores as
  confidence rather than verification.
- **The default threshold is 0.** Some recognizers are low-confidence by
  design (e.g. `US_BANK_NUMBER` at 0.05 for any 8–17 digit run). Set
  `Options.Threshold` to trade recall for precision.
- **Scores are relative.** A base score says how much the pattern alone is
  worth; [context](context-scoring.md) adds a fixed `0.35` when the
  surrounding words agree. Neither number is a probability, so pick a
  threshold by measuring on your own text rather than by reading the score.
- **Recall over locale-perfection.** Patterns favor catching real identifiers
  over locale-perfect validation of every edge case.

## Repository layout

```
alcatraz.go        Public entry point: NewEngine + re-exported types.
entities/          Canonical entity-type identifier constants.
analyzer/          Framework: Result, dedup, Recognizer, Pattern, Matcher,
                   PatternRecognizer, Registry, Engine, allow list,
                   context-aware scoring (ContextEnhancer), and the NLP seam
                   (NlpEngine, NlpArtifacts, ArtifactRecognizer).
anonymizer/        Mask/replace/redact detected spans (Operator, Config).
recognizers/       The 45 built-in recognizers, checksum helpers, loader.
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
docs/              This documentation site (MkDocs Material).
```

## Roadmap

- [x] 45 pattern recognizers, 25 checksum-validated
- [x] Opt-in `lookaround` module: true lookaround without polluting the core
- [x] ML/NER backend for `PERSON`, `LOCATION`, `NRP`: opt-in `ner` module,
      same pattern as `lookaround`; one shared inference pass per `Analyze`
- [x] `pfilter` module, a privacy-filter.cpp (GGML) backend: PII-specialized
      models, long documents, GPU; purego FFI, no cgo
- [x] Context-word score boosting: Presidio's +0.35 within five words, whole
      word rather than substring, no NLP backend required (lemmas still to
      come, once the NLP seam carries tokens)
- [ ] Zero-shot PII models (GLiNER-class): user-defined entity types at
      runtime, no retraining
- [ ] Optional LLM-backed detection/validation: separate module, explicit
      opt-in
- [ ] Precision/recall benchmark suite against a labeled corpus

See [TODO.md](https://github.com/hoophq/alcatraz/blob/main/TODO.md) for the
detailed plan.
