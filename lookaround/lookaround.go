// Package lookaround provides an alcatraz Matcher backed by a backtracking
// regex engine (github.com/dlclark/regexp2). It enables patterns that use
// lookahead and lookbehind — (?=…), (?!…), (?<=…), (?<!…) — plus backreferences,
// which the standard-library RE2 engine that powers alcatraz's core does not
// support.
//
// It lives in a separate module on purpose: importing it is the only way to
// pull in regexp2, so alcatraz's core stays dependency-free and linear-time.
// Use it for user-configured rules that genuinely need lookaround; prefer the
// core (anchors + validators, or a capture group via Pattern.WithGroup) when
// you can, because backtracking does not have RE2's linear-time guarantee.
//
// To bound catastrophic backtracking (ReDoS), every compiled matcher carries a
// MatchTimeout (DefaultTimeout unless overridden). On timeout the affected
// match is abandoned rather than allowed to run unbounded.
package lookaround

import (
	"fmt"
	"time"

	"github.com/dlclark/regexp2"
	"github.com/hoophq/alcatraz/analyzer"
)

// DefaultTimeout bounds a single match attempt. Backtracking engines can
// degrade to exponential time on adversarial input, so a finite cap is the
// safe default.
const DefaultTimeout = time.Second

// Matcher is an analyzer.Matcher backed by regexp2. It reports byte offsets, so
// its results compose with the rest of alcatraz even though regexp2 works in
// rune space internally.
type Matcher struct {
	re *regexp2.Regexp
}

// Compile builds a lookaround-capable matcher with DefaultTimeout. The pattern
// uses regexp2/.NET syntax, which is a superset of RE2 for the common cases and
// additionally supports lookaround and backreferences.
func Compile(pattern string) (*Matcher, error) {
	return CompileWithTimeout(pattern, DefaultTimeout)
}

// CompileWithTimeout is Compile with an explicit per-match timeout. A timeout
// <= 0 disables the cap (not recommended for untrusted patterns or input).
func CompileWithTimeout(pattern string, timeout time.Duration) (*Matcher, error) {
	// None = full .NET semantics (lookaround enabled). Note: regexp2.RE2 would
	// instead restrict syntax to RE2 compatibility and is deliberately not used
	// here — the whole point of this package is the non-RE2 features.
	re, err := regexp2.Compile(pattern, regexp2.None)
	if err != nil {
		return nil, err
	}
	if timeout > 0 {
		re.MatchTimeout = timeout
	}
	return &Matcher{re: re}, nil
}

// MustCompile is Compile but panics on an invalid pattern, for package-level
// initialization of patterns known to compile.
func MustCompile(pattern string) *Matcher {
	m, err := Compile(pattern)
	if err != nil {
		panic("alcatraz/lookaround: invalid pattern: " + err.Error())
	}
	return m
}

// String returns the source pattern.
func (m *Matcher) String() string { return m.re.String() }

// FindAll implements analyzer.Matcher. It walks every non-overlapping match and
// converts regexp2's rune offsets into byte offsets so spans line up with the
// analyzed string. On a match-time error (e.g. timeout) it stops and returns
// the matches gathered so far.
func (m *Matcher) FindAll(text string) []analyzer.Match {
	toByte := runeToByteMapper(text)

	var out []analyzer.Match
	match, err := m.re.FindStringMatch(text)
	if err != nil {
		return out
	}
	for match != nil {
		groups := match.Groups()
		spans := make([][2]int, len(groups))
		for i, g := range groups {
			// A group with no captures did not participate in this match.
			if len(g.Captures) == 0 {
				spans[i] = [2]int{-1, -1}
				continue
			}
			spans[i] = [2]int{toByte(g.Index), toByte(g.Index + g.Length)}
		}
		out = append(out, analyzer.Match{Groups: spans})

		match, err = m.re.FindNextMatch(match)
		if err != nil {
			return out
		}
	}
	return out
}

// runeToByteMapper returns a function mapping a rune index (as reported by
// regexp2) to a byte offset into text. Indices in [0, runeCount] are valid;
// runeCount maps to len(text) (one-past-the-end), and out-of-range inputs clamp.
func runeToByteMapper(text string) func(runeIdx int) int {
	table := make([]int, 0, len(text)+1)
	for byteIdx := range text { // range yields the byte index of each rune start
		table = append(table, byteIdx)
	}
	table = append(table, len(text)) // sentinel for one-past-the-last rune
	return func(runeIdx int) int {
		switch {
		case runeIdx < 0:
			return -1
		case runeIdx >= len(table):
			return len(text)
		default:
			return table[runeIdx]
		}
	}
}

// Pattern compiles a lookaround-capable pattern for use with alcatraz's
// PatternRecognizer. Chain (*analyzer.Pattern).WithGroup to report a capture
// group instead of the whole match.
func Pattern(name, regex string, score float64) (*analyzer.Pattern, error) {
	m, err := Compile(regex)
	if err != nil {
		return nil, err
	}
	return analyzer.NewPatternMatcher(name, m, score), nil
}

// Spec describes one user-configured pattern that may use lookaround.
type Spec struct {
	Name  string
	Regex string
	Score float64
	// Group optionally selects the capture group to report as the entity span.
	// 0 (the whole match) is the default.
	Group int
}

// NewRecognizer builds a recognizer from user-configured patterns that may use
// lookahead/lookbehind. It is the one-call path for turning config-file regex
// rules into an alcatraz recognizer that plugs into the standard engine.
func NewRecognizer(name, entity, language string, specs ...Spec) (*analyzer.PatternRecognizer, error) {
	pats := make([]*analyzer.Pattern, 0, len(specs))
	for _, s := range specs {
		p, err := Pattern(s.Name, s.Regex, s.Score)
		if err != nil {
			return nil, fmt.Errorf("lookaround: pattern %q: %w", s.Name, err)
		}
		if s.Group != 0 {
			p.WithGroup(s.Group)
		}
		pats = append(pats, p)
	}
	return analyzer.NewPatternRecognizer(name, entity, language, pats), nil
}
