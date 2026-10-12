package analyzer

import (
	"sort"
	"sync"
)

// filteredCacheSize bounds Registry.filtered. Callers typically use a handful
// of distinct entity filters; once the bound is reached the cache is dropped
// rather than evicted, which keeps the hot path a single map lookup.
const filteredCacheSize = 64

// Registry holds recognizers keyed by language.
type Registry struct {
	byLanguage map[string][]Recognizer
	languages  []string

	// filtered memoizes Recognizers for a non-nil entity filter, keyed by
	// language and the entity list in the order given. Add clears it.
	mu       sync.RWMutex
	filtered map[string][]Recognizer
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
	r.mu.Lock()
	r.filtered = nil
	r.mu.Unlock()
}

// Recognizers returns the recognizers for a language. When entities is non-nil
// only recognizers that support at least one requested type are returned. The
// returned slice is shared across calls and must be treated as read-only.
func (r *Registry) Recognizers(language string, entities []string) []Recognizer {
	recs := r.byLanguage[language]
	if entities == nil {
		return recs
	}
	// The key is built in a stack buffer so a cache hit does not allocate;
	// the map lookup on a []byte-to-string conversion is copy-free.
	var buf [1024]byte
	key := append(buf[:0], language...)
	for _, e := range entities {
		key = append(key, 0)
		key = append(key, e...)
	}
	r.mu.RLock()
	out, ok := r.filtered[string(key)]
	r.mu.RUnlock()
	if ok {
		return out
	}
	for _, rec := range recs {
		if supportsAny(rec.SupportedEntities(), entities) {
			out = append(out, rec)
		}
	}
	r.mu.Lock()
	if len(r.filtered) >= filteredCacheSize {
		r.filtered = nil
	}
	if r.filtered == nil {
		r.filtered = make(map[string][]Recognizer)
	}
	r.filtered[string(key)] = out
	r.mu.Unlock()
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
