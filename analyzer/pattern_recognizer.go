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
	name            string
	entity          string
	language        string
	patterns        []*Pattern
	context         []string
	validate        Validator
	contextValidate ContextValidator
}

// NewPatternRecognizer creates a recognizer for a single entity type.
func NewPatternRecognizer(name, entity, language string, patterns []*Pattern) *PatternRecognizer {
	return &PatternRecognizer{
		name:     name,
		entity:   entity,
		language: language,
		patterns: patterns,
	}
}

// WithContext attaches context words that hint at the entity nearby. They are
// retained for future context-aware scoring (which requires an NLP backend)
// and are inert in the pattern-only engine. Returns the recognizer for
// chaining.
func (pr *PatternRecognizer) WithContext(words ...string) *PatternRecognizer {
	pr.context = words
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

// SupportedEntities implements Recognizer.
func (pr *PatternRecognizer) SupportedEntities() []string { return []string{pr.entity} }

// SupportedLanguage implements Recognizer.
func (pr *PatternRecognizer) SupportedLanguage() string { return pr.language }

// Analyze implements Recognizer. Every pattern is run over the text; the
// configured capture group of each match is taken as the entity span, scored at
// the pattern's base score, then promoted to MaxScore or dropped by the
// structural validator, and finally filtered by the context validator. (A
// promotes/drops; the context validator only filters.) Overlapping same-entity
// matches are de-duplicated before returning.
func (pr *PatternRecognizer) Analyze(text string, entities []string) []Result {
	if entities != nil && !supportsAny(pr.SupportedEntities(), entities) {
		return nil
	}

	var results []Result
	for _, p := range pr.patterns {
		for _, m := range p.matcher.FindAll(text) {
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
			results = append(results, Result{
				EntityType:     pr.entity,
				Start:          start,
				End:            end,
				Score:          score,
				RecognizerName: pr.name,
			})
		}
	}
	return RemoveDuplicates(results)
}
