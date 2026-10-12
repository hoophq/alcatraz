package analyzer

import (
	"regexp"
	"sync"
	"sync/atomic"
)

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

// program is a compiled RE2 regex. Patterns with the same source share one
// (`\b\d{11}\b` alone backs six built-in recognizers); shared flips when a
// second Pattern adopts it, which tells scanCache the program's matches are
// worth memoizing within an Analyze call.
type program struct {
	re     *regexp.Regexp
	shared atomic.Bool
}

var (
	programsMu sync.Mutex
	programs   = map[string]*program{}
)

// compileProgram returns the program for regex, compiling it on first use.
func compileProgram(regex string) (*program, error) {
	programsMu.Lock()
	defer programsMu.Unlock()
	if p, ok := programs[regex]; ok {
		p.shared.Store(true)
		return p, nil
	}
	re, err := regexp.Compile(regex)
	if err != nil {
		return nil, err
	}
	p := &program{re: re}
	programs[regex] = p
	return p, nil
}

// stdMatcher is the default Matcher, backed by the standard library RE2 engine.
type stdMatcher struct {
	prog *program
}

func (m stdMatcher) String() string { return m.prog.re.String() }

func (m stdMatcher) FindAll(text string) []Match {
	raw := m.prog.re.FindAllStringSubmatchIndex(text, -1)
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

// scanCache memoizes FindAll per shared program for the duration of one
// Analyze call, so a text is scanned once for every pattern that uses the
// same regex. It lives on the caller's stack; programs beyond its capacity
// are simply scanned again.
type scanCache struct {
	n       int
	entries [8]scanEntry
}

type scanEntry struct {
	prog    *program
	matches []Match
}

// findAll runs p's matcher over text, serving shared programs from the cache.
func (s *scanCache) findAll(p *Pattern, text string) []Match {
	if p.prog == nil || !p.prog.shared.Load() {
		return p.matcher.FindAll(text)
	}
	for i := range s.n {
		if s.entries[i].prog == p.prog {
			return s.entries[i].matches
		}
	}
	matches := p.matcher.FindAll(text)
	if s.n < len(s.entries) {
		s.entries[s.n] = scanEntry{prog: p.prog, matches: matches}
		s.n++
	}
	return matches
}
