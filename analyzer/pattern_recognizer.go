package analyzer

// Validator inspects a matched substring and decides whether it is a true
// positive. It is used for entities with a verifiable structure (checksums,
// range rules). Returning true promotes the match to MaxScore; returning false
// drops it. A nil validator leaves the pattern's base score untouched.
type Validator func(match string) bool

// ContextValidator inspects a match together with its surroundings: the full
// text and the match's byte span [start, end). It is a filter — returning true
// keeps the match at its current score, returning false drops it — and is the
// pure-Go way to express lookahead/lookbehind ("keep this only if preceded or
// followed by X"). A nil context validator is a no-op.
type ContextValidator func(text string, start, end int) bool

// PatternRecognizer detects a single entity type using one or more regex
// patterns, with optional structural and context validators. It is the
// workhorse behind almost every built-in recognizer.
type PatternRecognizer struct {
	name     string
	entity   string
	language string
	// entities is the one-element SupportedEntities slice, built once so the
	// per-call entity filter does not allocate it.
	entities []string
	patterns []*Pattern
	context  []string
	// contextPhrases is context pre-split into lower-case words, and
	// contextLongest the longest such word in bytes, as contextWords wants
	// them; computed once in WithContext.
	contextPhrases  [][]string
	contextLongest  int
	validate        Validator
	contextValidate ContextValidator
}

// NewPatternRecognizer creates a recognizer for a single entity type.
func NewPatternRecognizer(name, entity, language string, patterns []*Pattern) *PatternRecognizer {
	return &PatternRecognizer{
		name:     name,
		entity:   entity,
		language: language,
		entities: []string{entity},
		patterns: patterns,
	}
}

// WithContext attaches words that hint at this entity when they appear near a
// match — the labels a value is usually written under ("ssn", "codice
// fiscale"). The engine's ContextEnhancer raises a result's score when one of
// them is in the surrounding words, so a pattern too weak to trust on its own
// reaches a usable score in text that agrees with it. Matching is
// case-insensitive and by whole word, so multi-word entries are matched as
// phrases. No NLP backend is required.
//
// Returns the recognizer for chaining.
func (pr *PatternRecognizer) WithContext(words ...string) *PatternRecognizer {
	pr.context = words
	// Split and lower-case once here rather than on every Enhance call;
	// see contextWords.
	pr.contextPhrases, pr.contextLongest = splitContext(words)
	return pr
}

// WithValidator attaches a structural validator (e.g. a checksum). Returns the
// recognizer for chaining.
func (pr *PatternRecognizer) WithValidator(v Validator) *PatternRecognizer {
	pr.validate = v
	return pr
}

// WithContextValidator attaches a context-aware filter that sees the full text
// and the match span. Use it for lookaround-style rules ("only if preceded by
// 'SSN:'"). Returns the recognizer for chaining.
func (pr *PatternRecognizer) WithContextValidator(v ContextValidator) *PatternRecognizer {
	pr.contextValidate = v
	return pr
}

// Context returns the recognizer's context words.
func (pr *PatternRecognizer) Context() []string { return pr.context }

// Name implements Recognizer.
func (pr *PatternRecognizer) Name() string { return pr.name }

// SupportedEntities implements Recognizer. The returned slice is shared
// across calls and must be treated as read-only.
func (pr *PatternRecognizer) SupportedEntities() []string { return pr.entities }

// SupportedLanguage implements Recognizer.
func (pr *PatternRecognizer) SupportedLanguage() string { return pr.language }

// Analyze implements Recognizer. Every pattern is run over the text; the
// configured capture group of each match is taken as the entity span, scored at
// the pattern's base score, then promoted to MaxScore or dropped by the
// structural validator, and finally filtered by the context validator. (A
// promotes/drops; the context validator only filters.) Overlapping same-entity
// matches are de-duplicated before returning.
//
// A pattern is only run when the text contains every byte class its matches
// require (see Pattern.requires), so prose without a digit skips the
// numeric-identifier patterns outright.
func (pr *PatternRecognizer) Analyze(text string, entities []string) []Result {
	if entities != nil && !supportsAny(pr.entities, entities) {
		return nil
	}
	var scans scanCache
	return RemoveDuplicates(pr.appendResults(nil, text, byteClasses(text), &scans))
}

// appendResults is Analyze after the entity filter and before
// de-duplication: it appends every validated match to dst and returns the
// extended slice. The text's byte-class summary and the per-call scan cache
// come from the caller, so the engine computes both once and shares them
// across every pattern recognizer it runs — and skips the per-recognizer
// de-duplication, since its own pass over the combined results subsumes it.
func (pr *PatternRecognizer) appendResults(dst []Result, text string, classes classMask, scans *scanCache) []Result {
	for _, p := range pr.patterns {
		if p.requires&classes != p.requires {
			continue
		}
		for _, m := range scans.findAll(p, text) {
			start, end := m.Span(p.Group)
			if start < 0 || end <= start {
				continue
			}
			match := text[start:end]
			score := p.Score
			if pr.validate != nil {
				if !pr.validate(match) {
					continue
				}
				score = MaxScore
			}
			if pr.contextValidate != nil && !pr.contextValidate(text, start, end) {
				continue
			}
			if score <= MinScore {
				continue
			}
			dst = append(dst, Result{
				EntityType:     pr.entity,
				Start:          start,
				End:            end,
				Score:          score,
				RecognizerName: pr.name,
			})
		}
	}
	return dst
}
