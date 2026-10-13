package analyzer

import "regexp/syntax"

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
	// prog is the shared RE2 program behind matcher, nil for a custom
	// Matcher; see scanCache.
	prog *program
	// requires is the byte classes every match must contain, derived from
	// the regex at construction; a text lacking any of them is not scanned.
	// Zero (nothing required) for patterns backed by a custom Matcher.
	requires classMask
	// run is set (min > 0) when the regex is exactly \b\d{min,max}\b, which
	// the engine serves from one shared digit-run scan instead of the regex.
	run digitRun
}

// NewPattern compiles a pattern with the standard library RE2 engine,
// returning an error if the regex is invalid.
func NewPattern(name, regex string, score float64) (*Pattern, error) {
	prog, err := compileProgram(regex)
	if err != nil {
		return nil, err
	}
	p := &Pattern{Name: name, Regex: regex, Score: score, matcher: stdMatcher{prog: prog}, prog: prog}
	// regexp.Compile succeeded, so this parse cannot fail.
	if re, err := syntax.Parse(regex, syntax.Perl); err == nil {
		p.requires = requiredClasses(re)
		p.run, _ = parseDigitRun(re)
	}
	return p, nil
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
