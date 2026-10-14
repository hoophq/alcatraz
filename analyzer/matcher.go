package analyzer

import (
	"regexp"
	"regexp/syntax"
	"sync"
	"sync/atomic"
	"unicode"
	"unicode/utf8"
)

// Match is a single regex match expressed in byte offsets: group 0 is the
// whole match, group i the i-th capture group. A group that did not
// participate in the match has both offsets set to -1.
type Match struct {
	// loc holds start/end pairs, group g at loc[2g], loc[2g+1] — the layout
	// regexp.FindAllStringSubmatchIndex produces, kept as is so the default
	// matcher hands the engine's slices through without copying.
	loc []int
}

// NewMatch builds a Match from start/end pairs laid out as
// regexp.FindStringSubmatchIndex returns them: loc[0], loc[1] is the whole
// match and loc[2g], loc[2g+1] is group g, with -1, -1 for a group that did
// not participate. The slice is retained, not copied.
func NewMatch(loc []int) Match { return Match{loc: loc} }

// NumGroups returns the number of groups the match reports, counting group 0.
func (m Match) NumGroups() int { return len(m.loc) / 2 }

// Span returns the byte offsets of group g, or (-1, -1) if g is out of range or
// did not participate.
func (m Match) Span(g int) (start, end int) {
	if g < 0 || 2*g+1 >= len(m.loc) {
		return -1, -1
	}
	return m.loc[2*g], m.loc[2*g+1]
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

// programCacheSize bounds programs. The built-in set is 74 sources; the
// bound exists so a process compiling patterns from variable input (per
// tenant lists, user recognizers) cannot grow the cache without limit.
// Past it, patterns still compile and work, they just do not share.
const programCacheSize = 1024

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
	if len(programs) < programCacheSize {
		programs[regex] = p
	}
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
	// FindAllStringSubmatchIndex already reports -1 for groups that did
	// not participate, which is the Match contract; its per-match slices
	// are adopted as they are.
	matches := make([]Match, len(raw))
	for i, loc := range raw {
		matches[i] = Match{loc: loc}
	}
	return matches
}

// digitRun describes a pattern of the exact form \b\d{min,max}\b (or
// \b[0-9]{n}\b): a maximal run of ASCII digits, min to max long, with no
// ASCII word byte on either side. Sixteen built-in recognizers use one, so
// the engine finds every such run once per Analyze and lets each pattern
// pick the lengths it wants, instead of running one regex per length.
type digitRun struct {
	min, max int // max < 0 means unbounded
}

// parseDigitRun recognizes the \b\d{min,max}\b (or \b\d+\b) shape in a
// parsed regex.
func parseDigitRun(re *syntax.Regexp) (digitRun, bool) {
	if re.Op != syntax.OpConcat || len(re.Sub) != 3 {
		return digitRun{}, false
	}
	if re.Sub[0].Op != syntax.OpWordBoundary || re.Sub[2].Op != syntax.OpWordBoundary {
		return digitRun{}, false
	}
	rep := re.Sub[1]
	var run digitRun
	switch {
	case rep.Op == syntax.OpRepeat && rep.Min >= 1:
		run = digitRun{min: rep.Min, max: rep.Max}
	case rep.Op == syntax.OpPlus:
		run = digitRun{min: 1, max: -1}
	default:
		return digitRun{}, false
	}
	if len(rep.Sub) != 1 {
		return digitRun{}, false
	}
	cc := rep.Sub[0]
	if cc.Op != syntax.OpCharClass || len(cc.Rune) != 2 || cc.Rune[0] != '0' || cc.Rune[1] != '9' {
		return digitRun{}, false
	}
	return run, true
}

// isWordByte is the ASCII word class RE2's \b is defined on.
func isWordByte(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// digitRuns writes the start/end pairs of every maximal run of ASCII digits
// in text that has no ASCII word byte before or after it — exactly the
// spans \b\d+\b matches — into buf, and returns how many ints it wrote, or
// -1 when buf is too small. One pass over the text.
func digitRuns(buf []int, text string) int {
	n := 0
	for i := 0; i < len(text); {
		if text[i] < '0' || text[i] > '9' {
			i++
			continue
		}
		start := i
		for i < len(text) && text[i] >= '0' && text[i] <= '9' {
			i++
		}
		// The run is maximal, so its neighbours are non-digits; \b holds
		// unless one of them is a letter or underscore.
		if start > 0 && isWordByte(text[start-1]) || i < len(text) && isWordByte(text[i]) {
			continue
		}
		if n+2 > len(buf) {
			return -1
		}
		buf[n], buf[n+1] = start, i
		n += 2
	}
	return n
}

// countDigitRuns is digitRuns without a buffer: the number of ints a full
// scan would write.
func countDigitRuns(text string) int {
	n := 0
	for i := 0; i < len(text); {
		if text[i] < '0' || text[i] > '9' {
			i++
			continue
		}
		start := i
		for i < len(text) && text[i] >= '0' && text[i] <= '9' {
			i++
		}
		if start > 0 && isWordByte(text[start-1]) || i < len(text) && isWordByte(text[i]) {
			continue
		}
		n += 2
	}
	return n
}

// matchDigitRuns selects the runs whose length r accepts, as Matches over
// one shared offset buffer: two allocations however many runs match.
func matchDigitRuns(runs []int, r digitRun) []Match {
	n := 0
	for i := 0; i < len(runs); i += 2 {
		if l := runs[i+1] - runs[i]; l >= r.min && (r.max < 0 || l <= r.max) {
			n++
		}
	}
	if n == 0 {
		return nil
	}
	loc := make([]int, 0, 2*n)
	matches := make([]Match, 0, n)
	for i := 0; i < len(runs); i += 2 {
		if l := runs[i+1] - runs[i]; l >= r.min && (r.max < 0 || l <= r.max) {
			loc = append(loc, runs[i], runs[i+1])
			matches = append(matches, Match{loc: loc[len(loc)-2:]})
		}
	}
	return matches
}

// window describes a pattern whose matches can be found by scanning only the
// neighbourhood of each digit run rather than the whole text. It holds for
// any regex that requires a digit: every match is then non-empty and made
// of bytes from a known class alphabet, so it sits inside one maximal
// alphabet run, and that run contains a digit. The engine runs the original
// regex over each digit-bearing alphabet run plus one byte of context and
// gets the matches the full text would give, because that byte is all the
// context RE2 reads past a match: \b and \B inspect one rune each side, and
// ^ $ \A \z (?m) can only disagree with the full text at the context bytes
// themselves — positions a match path reaches only by starting or ending
// on a byte outside the alphabet, which no match does. 56 of the built-in
// patterns are served this way (15 more take the digit-run shortcut), and
// on a prose line most of their runs are rejected by length or class
// before the regex is even called.
type window struct {
	ok bool
	// alphabet is the class set every match byte belongs to; a run extends
	// through bytes whose class intersects it. It always holds classDigit —
	// a required class is in every match — so a digit run is never split
	// by an alphabet run, which findAll relies on.
	alphabet classMask
	// minLen is a lower bound on a match's byte length: shorter runs cannot
	// hold one.
	minLen int
}

// parseWindow derives the window shape from a parsed regex, given the
// classes its matches require.
func parseWindow(re *syntax.Regexp, requires classMask) window {
	if requires&classDigit == 0 {
		return window{}
	}
	return window{ok: true, alphabet: matchAlphabet(re), minLen: minMatchLen(re)}
}

// minMatchLen is a lower bound, in bytes, on the length of any match of re.
func minMatchLen(re *syntax.Regexp) int {
	switch re.Op {
	case syntax.OpLiteral:
		n := 0
		fold := re.Flags&syntax.FoldCase != 0
		for _, r := range re.Rune {
			l := utf8.RuneLen(r)
			if fold {
				// The fold orbit can mix widths ('k' and the 3-byte
				// Kelvin sign); the shortest member bounds the match.
				for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
					l = min(l, utf8.RuneLen(f))
				}
			}
			n += max(l, 1)
		}
		return n
	case syntax.OpCharClass:
		// Ranges ascend and UTF-8 width is monotone in the code point, so
		// the lowest member is the shortest; a negative RuneLen (an invalid
		// code point) still costs at least one byte.
		if len(re.Rune) == 0 {
			return 0
		}
		return max(utf8.RuneLen(re.Rune[0]), 1)
	case syntax.OpAnyCharNotNL, syntax.OpAnyChar:
		return 1
	case syntax.OpCapture, syntax.OpPlus:
		return minMatchLen(re.Sub[0])
	case syntax.OpRepeat:
		return re.Min * minMatchLen(re.Sub[0])
	case syntax.OpConcat:
		n := 0
		for _, sub := range re.Sub {
			n += minMatchLen(sub)
		}
		return n
	case syntax.OpAlternate:
		n := minMatchLen(re.Sub[0])
		for _, sub := range re.Sub[1:] {
			n = min(n, minMatchLen(sub))
		}
		return n
	}
	// Star, quest, empty match and zero-width assertions can match nothing.
	return 0
}

// digitGroups is digitRuns without the \b condition: the start/end pairs of
// every maximal run of ASCII digits in text, into buf, returning how many
// ints it wrote or -1 when buf is too small. The windowed scan starts from
// these, so it needs the runs letters touch as well.
func digitGroups(buf []int, text string) int {
	n := 0
	for i := 0; i < len(text); {
		if text[i] < '0' || text[i] > '9' {
			i++
			continue
		}
		start := i
		for i < len(text) && text[i] >= '0' && text[i] <= '9' {
			i++
		}
		if n+2 > len(buf) {
			return -1
		}
		buf[n], buf[n+1] = start, i
		n += 2
	}
	return n
}

// countDigitGroups is digitGroups without a buffer.
func countDigitGroups(text string) int {
	n := 0
	for i := 0; i < len(text); {
		if text[i] < '0' || text[i] > '9' {
			i++
			continue
		}
		for i < len(text) && text[i] >= '0' && text[i] <= '9' {
			i++
		}
		n += 2
	}
	return n
}

// windowClass is byteClass with classOther for the classless bytes, so
// that a run can be extended through them when the alphabet admits them.
func windowClass(c byte) classMask {
	if m := byteClass[c]; m != 0 {
		return m
	}
	return classOther
}

// findAll returns the matches of re in text, in order, by running it over
// each maximal alphabet run that holds a digit run — the only places a
// match can be — widened by one byte each side for \b. groups are the
// digit runs of text as digitGroups reports them. A run is skipped without
// a regex call when it is shorter than any match or lacks a class every
// match requires; both tests are exact for the run, and a match is a
// substring of its run, so neither can hide one. Offsets come back in text
// coordinates: the stdlib's per-match slices are shifted in place.
func (w window) findAll(re *regexp.Regexp, requires classMask, groups []int, text string) []Match {
	var matches []Match
	covered := 0
	for i := 0; i < len(groups); i += 2 {
		start, end := groups[i], groups[i+1]
		if start < covered {
			// Inside the run grown from an earlier digit group: digits
			// are in the alphabet, so that run swallowed this one whole.
			continue
		}
		mask := classDigit
		for start > 0 {
			cls := windowClass(text[start-1])
			if cls&w.alphabet == 0 {
				break
			}
			mask |= cls
			start--
		}
		for end < len(text) {
			cls := windowClass(text[end])
			if cls&w.alphabet == 0 {
				break
			}
			mask |= cls
			end++
		}
		covered = end
		if end-start < w.minLen || mask&requires != requires {
			continue
		}
		lo, hi := start, end
		if lo > 0 {
			lo--
		}
		if hi < len(text) {
			hi++
		}
		// Most runs hold one match at most, so take the first alone — the
		// same leftmost match FindAll would list first — and only when the
		// rest of the run could still fit a whole match ask for the full
		// list, which costs the stdlib's [][]int wrapper on top.
		loc := re.FindStringSubmatchIndex(text[lo:hi])
		if loc == nil {
			continue
		}
		if end-(lo+loc[1]) < w.minLen {
			matches = append(matches, shiftMatch(loc, lo))
			continue
		}
		for _, loc := range re.FindAllStringSubmatchIndex(text[lo:hi], -1) {
			matches = append(matches, shiftMatch(loc, lo))
		}
	}
	return matches
}

// shiftMatch moves a window-relative match to text coordinates, in place.
func shiftMatch(loc []int, by int) Match {
	for j, v := range loc {
		if v >= 0 {
			loc[j] = v + by
		}
	}
	return Match{loc: loc}
}

// scanCache memoizes scans for the duration of one Analyze call: FindAll
// per shared program, so a text is scanned once for every pattern that uses
// the same regex; the digit runs that every \b\d{n}\b pattern selects from;
// and the digit groups the windowed patterns grow their runs from. It lives
// on the caller's stack; programs beyond its capacity are simply scanned
// again, and runs beyond the stack buffers go to the heap.
type scanCache struct {
	n       int
	entries [8]scanEntry

	// Digit runs are kept as a count into runsBuf, not a slice of it: a
	// slice would be a pointer into the cache stored in the cache, which
	// escape analysis takes as a reason to move the whole thing to the
	// heap. runsHeap takes over for a text with more runs than fit.
	ranRuns  bool
	runsN    int
	runsBuf  [64]int
	runsHeap []int

	// The same layout for digitGroups, which the windowed scan needs
	// unfiltered by \b.
	ranGroups  bool
	groupsN    int
	groupsBuf  [64]int
	groupsHeap []int
}

type scanEntry struct {
	prog    *program
	matches []Match
}

// findAll runs p's matcher over text, serving shared programs and digit-run
// patterns from the cache.
func (s *scanCache) findAll(p *Pattern, text string) []Match {
	if p.run.min > 0 {
		if !s.ranRuns {
			if n := digitRuns(s.runsBuf[:], text); n >= 0 {
				s.runsN = n
			} else {
				s.runsHeap = make([]int, countDigitRuns(text))
				digitRuns(s.runsHeap, text)
			}
			s.ranRuns = true
		}
		runs := s.runsHeap
		if runs == nil {
			runs = s.runsBuf[:s.runsN]
		}
		return matchDigitRuns(runs, p.run)
	}
	if p.prog == nil || !p.prog.shared.Load() {
		return s.scan(p, text)
	}
	for i := range s.n {
		if s.entries[i].prog == p.prog {
			return s.entries[i].matches
		}
	}
	matches := s.scan(p, text)
	if s.n < len(s.entries) {
		s.entries[s.n] = scanEntry{prog: p.prog, matches: matches}
		s.n++
	}
	return matches
}

// scan is one uncached pass of p over text: the windowed scan when the
// pattern's shape allows it, the matcher's full scan otherwise.
func (s *scanCache) scan(p *Pattern, text string) []Match {
	if !p.win.ok {
		return p.matcher.FindAll(text)
	}
	if !s.ranGroups {
		if n := digitGroups(s.groupsBuf[:], text); n >= 0 {
			s.groupsN = n
		} else {
			s.groupsHeap = make([]int, countDigitGroups(text))
			digitGroups(s.groupsHeap, text)
		}
		s.ranGroups = true
	}
	groups := s.groupsHeap
	if groups == nil {
		groups = s.groupsBuf[:s.groupsN]
	}
	return p.win.findAll(p.prog.re, p.requires, groups, text)
}
