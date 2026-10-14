package analyzer

// Test-only accessors for the byte-class prefilter, so the safety test in
// package analyzer_test can check every built-in recognizer's patterns.

// Patterns returns the recognizer's patterns.
func (pr *PatternRecognizer) Patterns() []*Pattern { return pr.patterns }

// Requires returns the pattern's required byte classes.
func (p *Pattern) Requires() uint64 { return uint64(p.requires) }

// FindAll runs the pattern's matcher over text.
func (p *Pattern) FindAll(text string) []Match { return p.matcher.FindAll(text) }

// FindAllWindowed runs the pattern through the engine's windowed scan, or
// reports false when the pattern's shape keeps the full scan.
func (p *Pattern) FindAllWindowed(text string) ([]Match, bool) {
	if !p.win.ok {
		return nil, false
	}
	var scans scanCache
	return scans.scan(p, text), true
}

// ByteClasses is byteClasses.
func ByteClasses(text string) uint64 { return uint64(byteClasses(text)) }

// PatternRequires is patternRequires.
func PatternRequires(regex string) uint64 { return uint64(patternRequires(regex)) }

// Class constants, for assertions on derived masks.
const (
	ClassDigit  = uint64(classDigit)
	ClassUpper  = uint64(classUpper)
	ClassLower  = uint64(classLower)
	ClassLetter = uint64(classLetter)
	ClassSpace  = uint64(classSpace)
)

// ClassOf returns the classes of one ASCII byte.
func ClassOf(c byte) uint64 { return uint64(byteClass[c]) }
