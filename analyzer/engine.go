package analyzer

import (
	"regexp"
	"strings"
)

// Options tunes a single Analyze call. The zero value is valid: it analyzes
// English text with every recognizer and no score threshold.
type Options struct {
	// Entities, when non-nil, restricts detection to these entity types.
	Entities []string
	// Language selects the recognizer set; defaults to "en" when empty.
	Language string
	// Threshold, when non-nil, drops results scoring below it.
	Threshold *float64
	// AllowList suppresses results whose matched text is allow-listed.
	AllowList []string
	// AllowListRegex treats AllowList entries as regular expressions (joined
	// with "|") instead of exact strings.
	AllowListRegex bool
}

// Engine runs a registry of recognizers over text and reconciles their
// results. It is safe for concurrent use: Analyze does not mutate engine state.
type Engine struct {
	registry  *Registry
	threshold float64
	languages []string
	nlp       NlpEngine
	context   ContextEnhancer
}

// NewEngine builds an engine over the given registry. languages records the
// engine's configured languages for reference; analysis language is chosen per
// call via Options.
//
// Context-aware scoring is on by default (see SetContextEnhancer), matching
// Presidio.
func NewEngine(registry *Registry, languages []string) *Engine {
	return &Engine{
		registry:  registry,
		languages: append([]string(nil), languages...),
		context:   NewWordContextEnhancer(),
	}
}

// SetThreshold sets the default score threshold applied when Options.Threshold
// is nil.
func (e *Engine) SetThreshold(t float64) { e.threshold = t }

// SetNlpEngine attaches an NLP backend. When set, Analyze runs it at most
// once per call and only when an applicable recognizer implements
// ArtifactRecognizer and shares the resulting NlpArtifacts with every such
// recognizer. Without it, ArtifactRecognizers fall back to their plain
// Analyze method. Call during setup, before the engine is used concurrently.
func (e *Engine) SetNlpEngine(n NlpEngine) { e.nlp = n }

// SetContextEnhancer replaces the context-aware score enhancer, which raises
// a result's score when a word the recognizer declared via WithContext appears
// near the match. NewEngine installs NewWordContextEnhancer; pass nil to score
// purely on the pattern. Call during setup, before the engine is used
// concurrently.
func (e *Engine) SetContextEnhancer(c ContextEnhancer) { e.context = c }

// Languages returns the engine's configured languages.
func (e *Engine) Languages() []string { return append([]string(nil), e.languages...) }

// SupportedEntities returns the entity types detectable for a language.
func (e *Engine) SupportedEntities(language string) []string {
	if language == "" {
		language = "en"
	}
	return e.registry.SupportedEntities(language)
}

// Analyze detects entities in text. The pipeline: run every applicable
// recognizer, de-duplicate overlapping same-type spans, apply the score
// threshold, then the allow list. Matched substrings are filled into each
// result's Text field.
func (e *Engine) Analyze(text string, o Options) []Result {
	lang := o.Language
	if lang == "" {
		lang = "en"
	}

	recs := e.registry.Recognizers(lang, o.Entities)

	// One shared NLP pass: run the backend only when a recognizer will
	// actually consume the artifacts, so the pattern-only path pays nothing.
	var artifacts *NlpArtifacts
	if e.nlp != nil && wantsArtifacts(recs) {
		// An inference failure degrades to pattern-only analysis rather
		// than failing the whole call.
		artifacts, _ = e.nlp.ProcessText(text, lang)
	}

	return e.analyzeWithArtifacts(text, o, recs, artifacts)
}

// AnalyzeBatch is Analyze over several texts at once, returning one result
// slice per text in input order. When the configured NLP backend implements
// BatchNlpEngine, the model runs a single inference call for the whole batch
// instead of once per text — the per-text results are identical to Analyze,
// only faster. Recognizers, threshold and allow list apply per text exactly
// as in Analyze.
func (e *Engine) AnalyzeBatch(texts []string, o Options) [][]Result {
	lang := o.Language
	if lang == "" {
		lang = "en"
	}

	recs := e.registry.Recognizers(lang, o.Entities)

	// One shared NLP pass for the whole batch when the backend supports it,
	// otherwise one per text; a failed pass degrades that text to the
	// pattern-only path, matching Analyze.
	artifacts := make([]*NlpArtifacts, len(texts))
	if e.nlp != nil && wantsArtifacts(recs) {
		if batch, ok := e.nlp.(BatchNlpEngine); ok {
			if got, err := batch.ProcessTexts(texts, lang); err == nil && len(got) == len(texts) {
				artifacts = got
			}
		} else {
			for i, text := range texts {
				artifacts[i], _ = e.nlp.ProcessText(text, lang)
			}
		}
	}

	results := make([][]Result, len(texts))
	for i, text := range texts {
		results[i] = e.analyzeWithArtifacts(text, o, recs, artifacts[i])
	}
	return results
}

// wantsArtifacts reports whether any recognizer consumes shared NlpArtifacts.
func wantsArtifacts(recs []Recognizer) bool {
	for _, rec := range recs {
		if _, ok := rec.(ArtifactRecognizer); ok {
			return true
		}
	}
	return false
}

// analyzeWithArtifacts runs the recognizer/dedup/threshold/allow-list
// pipeline for one text, feeding the shared artifacts (may be nil) to
// artifact-aware recognizers.
func (e *Engine) analyzeWithArtifacts(text string, o Options, recs []Recognizer, artifacts *NlpArtifacts) []Result {
	var all []Result
	for _, rec := range recs {
		if ar, ok := rec.(ArtifactRecognizer); ok && artifacts != nil {
			all = append(all, ar.AnalyzeWithArtifacts(text, o.Entities, artifacts)...)
			continue
		}
		all = append(all, rec.Analyze(text, o.Entities)...)
	}

	// Before de-duplication and the threshold, both of which decide what to
	// keep by score: a boost can settle which of two overlapping spans wins,
	// and a result boosted past the threshold has to survive to be seen.
	if e.context != nil {
		all = e.context.Enhance(text, all, recs, artifacts)
	}

	results := RemoveDuplicates(all)

	threshold := e.threshold
	if o.Threshold != nil {
		threshold = *o.Threshold
	}
	if threshold > MinScore {
		kept := results[:0]
		for _, r := range results {
			if r.Score >= threshold {
				kept = append(kept, r)
			}
		}
		results = kept
	}

	if len(o.AllowList) > 0 {
		results = applyAllowList(results, o.AllowList, text, o.AllowListRegex)
	}

	for i := range results {
		results[i].Text = text[results[i].Start:results[i].End]
	}
	return results
}

// applyAllowList drops results whose matched text is allow-listed. In regex
// mode the entries are joined with "|"; an invalid combined pattern falls back
// to exact matching.
func applyAllowList(results []Result, allowList []string, text string, regexMode bool) []Result {
	if regexMode {
		if re, err := regexp.Compile(strings.Join(allowList, "|")); err == nil {
			kept := results[:0]
			for _, r := range results {
				if !re.MatchString(text[r.Start:r.End]) {
					kept = append(kept, r)
				}
			}
			return kept
		}
		// invalid combined regex: fall through to exact matching
	}

	allowed := make(map[string]struct{}, len(allowList))
	for _, a := range allowList {
		allowed[a] = struct{}{}
	}
	kept := results[:0]
	for _, r := range results {
		if _, ok := allowed[text[r.Start:r.End]]; !ok {
			kept = append(kept, r)
		}
	}
	return kept
}
