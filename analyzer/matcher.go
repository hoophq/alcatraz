package analyzer

import "regexp"

// Match is a single regex match expressed in byte offsets. Groups[0] is the
// whole match; Groups[i] is the i-th capture group. A group that did not
// participate in the match has both offsets set to -1.
type Match struct {
	Groups [][2]int
}

// Span returns the byte offsets of group g, or (-1, -1) if g is out of range or
// did not participate.
func (m Match) Span(g int) (start, end int) {
	if g < 0 || g >= len(m.Groups) {
		return -1, -1
	}
	return m.Groups[g][0], m.Groups[g][1]
}

// Matcher finds all non-overlapping matches of a compiled pattern in text,
// reporting byte-offset spans. The default implementation (stdMatcher) wraps
// the standard library's RE2 engine, which is linear-time and dependency-free
// but does not support lookaround or backreferences.
//
// Matcher is the extension point for alternative engines: supply one to
// NewPatternMatcher to back a Pattern with, for example, a backtracking engine
// that supports lookahead/lookbehind. Implementations MUST report byte offsets
// (not rune/code-point indices) so spans line up with the analyzed string.
type Matcher interface {
	// FindAll returns every non-overlapping match, left to right.
	FindAll(text string) []Match
	// String returns the source pattern, for diagnostics.
	String() string
}

// stdMatcher is the default Matcher, backed by the standard library RE2 engine.
type stdMatcher struct {
	re *regexp.Regexp
}

func (m stdMatcher) String() string { return m.re.String() }

func (m stdMatcher) FindAll(text string) []Match {
	raw := m.re.FindAllStringSubmatchIndex(text, -1)
	if raw == nil {
		return nil
	}
	matches := make([]Match, 0, len(raw))
	for _, loc := range raw {
		groups := make([][2]int, len(loc)/2)
		for i := 0; i < len(loc); i += 2 {
			// FindAllStringSubmatchIndex already reports -1 for groups that
			// did not participate, which matches the Match contract.
			groups[i/2] = [2]int{loc[i], loc[i+1]}
		}
		matches = append(matches, Match{Groups: groups})
	}
	return matches
}
