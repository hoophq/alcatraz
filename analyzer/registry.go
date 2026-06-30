package analyzer

import "sort"

// Registry holds recognizers keyed by language.
type Registry struct {
	byLanguage map[string][]Recognizer
	languages  []string
}

// NewRegistry creates a registry for the given languages.
func NewRegistry(languages ...string) *Registry {
	r := &Registry{
		byLanguage: make(map[string][]Recognizer),
		languages:  append([]string(nil), languages...),
	}
	for _, lang := range languages {
		if _, ok := r.byLanguage[lang]; !ok {
			r.byLanguage[lang] = nil
		}
	}
	return r
}

// Add registers a recognizer under the given language. The language is the
// key analysis is performed against; it is supplied explicitly (rather than
// taken from the recognizer) because structured-identifier recognizers are
// language-independent and are typically registered under every analyzed
// language.
func (r *Registry) Add(language string, rec Recognizer) {
	if _, ok := r.byLanguage[language]; !ok {
		r.languages = append(r.languages, language)
	}
	r.byLanguage[language] = append(r.byLanguage[language], rec)
}

// Recognizers returns the recognizers for a language. When entities is non-nil
// only recognizers that support at least one requested type are returned.
func (r *Registry) Recognizers(language string, entities []string) []Recognizer {
	recs := r.byLanguage[language]
	if entities == nil {
		return recs
	}
	var out []Recognizer
	for _, rec := range recs {
		if supportsAny(rec.SupportedEntities(), entities) {
			out = append(out, rec)
		}
	}
	return out
}

// SupportedEntities returns the sorted, de-duplicated set of entity types a
// language's recognizers can emit.
func (r *Registry) SupportedEntities(language string) []string {
	seen := map[string]struct{}{}
	for _, rec := range r.byLanguage[language] {
		for _, e := range rec.SupportedEntities() {
			seen[e] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for e := range seen {
		out = append(out, e)
	}
	sort.Strings(out)
	return out
}

// Languages returns the languages registered.
func (r *Registry) Languages() []string {
	return append([]string(nil), r.languages...)
}
