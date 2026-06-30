package analyzer

// Recognizer detects entities of one or more types in text. Pattern-based
// recognizers are provided by PatternRecognizer; the interface is exported so
// callers can plug in custom logic (including future ML/NER backends) and add
// it to a Registry or pass it to the engine.
type Recognizer interface {
	// Name returns a stable identifier for the recognizer.
	Name() string
	// SupportedEntities lists the entity types this recognizer can emit.
	SupportedEntities() []string
	// SupportedLanguage returns the ISO 639-1 language code the recognizer
	// is registered under.
	SupportedLanguage() string
	// Analyze scans text and returns detections. When entities is non-nil it
	// is a filter: the recognizer should only run if it supports at least one
	// of the requested types. Returned offsets are byte indices into text.
	Analyze(text string, entities []string) []Result
}

// supportsAny reports whether any of want is contained in have.
func supportsAny(have, want []string) bool {
	for _, w := range want {
		for _, h := range have {
			if h == w {
				return true
			}
		}
	}
	return false
}
