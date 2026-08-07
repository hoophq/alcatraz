package pfilter

import (
	"errors"

	"github.com/hoophq/alcatraz/analyzer"
)

// Engine runs a privacy-filter model and implements analyzer.NlpEngine.
// Create it with New and release it with Close. It is safe for concurrent
// use: the engine keeps a pool of Config.PoolSize model contexts (default
// 1) and each classify call runs on an idle one, waiting when all are busy
// (pf.h does not document a single pf_ctx as thread-safe). Raise PoolSize
// for parallel inference — each context loads the model separately, so
// memory scales with it.
type Engine struct {
	classifier classifier
	cfg        Config
}

// New loads the shared library and the GGUF model, failing fast on either,
// so a constructed Engine can always run.
func New(cfg Config) (*Engine, error) {
	if cfg.ModelPath == "" {
		return nil, errors.New("pfilter: Config.ModelPath is required (path to a privacy-filter GGUF)")
	}
	if cfg.LabelMapping == nil {
		cfg.LabelMapping = DefaultConfig(cfg.ModelPath).LabelMapping
	}
	c, err := newFFIClassifier(cfg)
	if err != nil {
		return nil, err
	}
	return &Engine{classifier: c, cfg: cfg}, nil
}

// Close releases the model context. The Engine must not be used afterwards.
func (e *Engine) Close() error {
	return e.classifier.close()
}

// Config returns the engine's effective configuration.
func (e *Engine) Config() Config { return e.cfg }

// ProcessText implements analyzer.NlpEngine: it runs the model over text and
// returns the detected spans mapped to canonical entity names. Offsets come
// back from privacy-filter.cpp as byte offsets into the original UTF-8 text
// and are bounds-checked before use. The language parameter is unused (one
// model per engine); it is part of the interface for engines that route per
// language.
func (e *Engine) ProcessText(text, language string) (*analyzer.NlpArtifacts, error) {
	raw, err := e.classifier.classify(text, float32(e.cfg.Threshold))
	if err != nil {
		return nil, err
	}
	artifacts := &analyzer.NlpArtifacts{}
	for _, ent := range raw {
		span, ok := e.toNerSpan(text, ent)
		if !ok {
			continue
		}
		artifacts.Ents = append(artifacts.Ents, span)
	}
	return artifacts, nil
}

// toNerSpan converts one raw model span into a NerSpan: label mapping,
// ignore list, score clamping and byte-span validation. ok is false when
// the span is dropped.
func (e *Engine) toNerSpan(text string, ent rawEntity) (analyzer.NerSpan, bool) {
	if e.ignored(ent.label) {
		return analyzer.NerSpan{}, false
	}
	entity, ok := e.cfg.LabelMapping[ent.label]
	if !ok {
		// Keep unmapped labels (the multilingual model has 54 categories),
		// normalized to the entities.* naming convention; users silence
		// them via LabelsToIgnore.
		entity = normalizeLabel(ent.label)
	}
	if e.ignored(entity) {
		return analyzer.NerSpan{}, false
	}

	score := ent.score
	if score > analyzer.MaxScore {
		score = analyzer.MaxScore
	}
	if score <= analyzer.MinScore {
		return analyzer.NerSpan{}, false
	}

	start, end, ok := byteSpan(text, ent.start, ent.end)
	if !ok {
		return analyzer.NerSpan{}, false
	}
	return analyzer.NerSpan{EntityType: entity, Start: start, End: end, Score: score}, true
}

func (e *Engine) ignored(label string) bool {
	for _, l := range e.cfg.LabelsToIgnore {
		if l == label {
			return true
		}
	}
	return false
}

// Recognizer returns an analyzer.Recognizer emitting this engine's entities
// for the given language. Register it in a Registry alongside the pattern
// recognizers; pair it with Engine.SetNlpEngine so the model runs once per
// Analyze call.
func (e *Engine) Recognizer(language string) *Recognizer {
	return &Recognizer{engine: e, language: language}
}

// Recognizer adapts an Engine to the analyzer.Recognizer contract. When the
// analyzer.Engine provides shared artifacts the model has already run and
// AnalyzeWithArtifacts just converts spans; the plain Analyze path runs the
// model itself so the recognizer also works in an engine without
// SetNlpEngine.
type Recognizer struct {
	engine   *Engine
	language string
}

// Name implements analyzer.Recognizer.
func (r *Recognizer) Name() string { return "PrivacyFilterRecognizer" }

// SupportedEntities implements analyzer.Recognizer.
func (r *Recognizer) SupportedEntities() []string { return r.engine.cfg.SupportedEntities() }

// SupportedLanguage implements analyzer.Recognizer.
func (r *Recognizer) SupportedLanguage() string { return r.language }

// Analyze implements analyzer.Recognizer by running the model directly. An
// inference error yields no results, matching the engine's degrade-to-
// patterns behavior.
func (r *Recognizer) Analyze(text string, entities []string) []analyzer.Result {
	if entities != nil && !supportsAny(r.SupportedEntities(), entities) {
		return nil
	}
	artifacts, err := r.engine.ProcessText(text, r.language)
	if err != nil {
		return nil
	}
	return r.AnalyzeWithArtifacts(text, entities, artifacts)
}

// AnalyzeWithArtifacts implements analyzer.ArtifactRecognizer.
func (r *Recognizer) AnalyzeWithArtifacts(text string, entities []string, artifacts *analyzer.NlpArtifacts) []analyzer.Result {
	var results []analyzer.Result
	for _, ent := range artifacts.Ents {
		if entities != nil && !contains(entities, ent.EntityType) {
			continue
		}
		results = append(results, analyzer.Result{
			EntityType:     ent.EntityType,
			Start:          ent.Start,
			End:            ent.End,
			Score:          ent.Score,
			RecognizerName: r.Name(),
		})
	}
	return results
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func supportsAny(have, want []string) bool {
	for _, w := range want {
		if contains(have, w) {
			return true
		}
	}
	return false
}
