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
}

// NewEngine builds an engine over the given registry. languages records the
// engine's configured languages for reference; analysis language is chosen per
// call via Options.
func NewEngine(registry *Registry, languages []string) *Engine {
	return &Engine{
		registry:  registry,
		languages: append([]string(nil), languages...),
	}
}

// SetThreshold sets the default score threshold applied when Options.Threshold
// is nil.
func (e *Engine) SetThreshold(t float64) { e.threshold = t }

// Languages returns the engine's configured languages.
func (e *Engine) Languages() []string { return append([]string(nil), e.languages...) }

// SupportedEntities returns the entity types detectable for a language.
func (e *Engine) SupportedEntities(language string) []string {
	if language == "" {
		language = "en"
	}
	return e.registry.SupportedEntities(language)
}

// Analyze detects entities in text. The pipeline mirrors Presidio: run every
// applicable recognizer, de-duplicate overlapping same-type spans, apply the
// score threshold, then the allow list. Matched substrings are filled into
// each result's Text field.
func (e *Engine) Analyze(text string, o Options) []Result {
	lang := o.Language
	if lang == "" {
		lang = "en"
	}

	var all []Result
	for _, rec := range e.registry.Recognizers(lang, o.Entities) {
		all = append(all, rec.Analyze(text, o.Entities)...)
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
