package analyzer

import "regexp"

// Pattern is a named, compiled regular expression with a base confidence score.
//
// By default a match's whole span (group 0) is reported as the entity. Set
// Group (via WithGroup) to report a capture group instead — the idiomatic RE2
// way to emulate lookbehind/lookahead: match the surrounding context but emit
// only the captured entity.
type Pattern struct {
	Name  string
	Regex string
	Score float64
	// Group selects which capture group is reported as the entity span.
	// 0 (the whole match) is the default.
	Group int

	matcher Matcher
}

// NewPattern compiles a pattern with the standard library RE2 engine,
// returning an error if the regex is invalid.
func NewPattern(name, regex string, score float64) (*Pattern, error) {
	re, err := regexp.Compile(regex)
	if err != nil {
		return nil, err
	}
	return &Pattern{Name: name, Regex: regex, Score: score, matcher: stdMatcher{re: re}}, nil
}

// MustPattern is like NewPattern but panics on an invalid regex. It is meant
// for package-level recognizer definitions where the patterns are constants
// known to compile.
func MustPattern(name, regex string, score float64) *Pattern {
	p, err := NewPattern(name, regex, score)
	if err != nil {
		panic("alcatraz: invalid pattern " + name + ": " + err.Error())
	}
	return p
}

// NewPatternMatcher builds a pattern from an arbitrary Matcher. This is the
// extension point for engines that support features the RE2 default lacks —
// most notably lookahead/lookbehind (see the alcatraz/lookaround module). The
// Regex field is populated from m.String() for diagnostics.
func NewPatternMatcher(name string, m Matcher, score float64) *Pattern {
	return &Pattern{Name: name, Regex: m.String(), Score: score, matcher: m}
}

// WithGroup selects which capture group is reported as the entity span and
// returns the pattern for chaining. Group 0 (the whole match) is the default.
func (p *Pattern) WithGroup(group int) *Pattern {
	p.Group = group
	return p
}
