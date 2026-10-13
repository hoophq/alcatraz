package analyzer_test

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"

	"github.com/hoophq/alcatraz/analyzer"
	"github.com/hoophq/alcatraz/recognizers"
)

// The derivation must only claim a class when no match can lack it: every
// row names the construct under test and the classes it provably requires.
func TestPatternRequires(t *testing.T) {
	at, dot, hyphen, colon, plus := analyzer.ClassOf('@'), analyzer.ClassOf('.'), analyzer.ClassOf('-'), analyzer.ClassOf(':'), analyzer.ClassOf('+')
	tests := []struct {
		regex string
		want  uint64
	}{
		{`\b\d{9}\b`, analyzer.ClassDigit},
		{`\b[0-9]{9}\b`, analyzer.ClassDigit},
		{`[A-Z]{2}[0-9]{2}`, analyzer.ClassUpper | analyzer.ClassLetter | analyzer.ClassDigit},
		{`[A-Z0-9]{7}`, 0}, // mixed class shares nothing
		{`\w+`, 0},         // digits, letters and '_' share nothing
		{`a|1`, 0},         // alternation intersects
		{`(?:\(\d{3}\)|\d{3})`, analyzer.ClassDigit},
		{`x*`, 0}, // may be empty
		{`x?`, 0}, // may be empty
		{`x+`, analyzer.ClassLower | analyzer.ClassLetter},
		{`x{0,3}`, 0}, // zero minimum
		{`x{2,3}`, analyzer.ClassLower | analyzer.ClassLetter},
		{`(?i)a`, analyzer.ClassLetter}, // either case, still a letter
		{`(?i)k`, 0},                    // folds to U+212A KELVIN SIGN
		{`(?i)s`, 0},                    // folds to U+017F LONG S
		{`(?i)[a-z]`, 0},                // same orbit through a class
		{`[@]\w+\.\w+`, at | dot},
		{`^abc$`, analyzer.ClassLower | analyzer.ClassLetter},
		{`\p{Zs}+`, 0}, // includes non-ASCII spaces
		{`\d \d`, analyzer.ClassDigit | analyzer.ClassSpace},
		{`\B\+(?:[ .()-]{0,2}\d){8,15}\b`, plus | analyzer.ClassDigit},
		{`(?:https?://|ftp://|www\.)`, analyzer.ClassLower | analyzer.ClassLetter},
		{`\b(?:[0-9a-fA-F]{1,4}:){7}[0-9a-fA-F]{1,4}\b`, colon},
		{`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-4[0-9a-fA-F]{3}`, hyphen | analyzer.ClassDigit},
		{`.`, 0},
		{`é`, 0}, // non-ASCII literal
	}
	for _, tt := range tests {
		if got := analyzer.PatternRequires(tt.regex); got != tt.want {
			t.Errorf("PatternRequires(%q) = %#x, want %#x", tt.regex, got, tt.want)
		}
	}
	if got := analyzer.PatternRequires(`(?<=x)y`); got != 0 {
		t.Errorf("unparseable regex requires %#x, want 0", got)
	}
}

// A pattern built on a custom Matcher has no parseable RE2 source, so it
// must never be prefiltered.
func TestCustomMatcherRequiresNothing(t *testing.T) {
	p := analyzer.NewPatternMatcher("custom", fixedMatcher{}, 0.5)
	if got := p.Requires(); got != 0 {
		t.Errorf("custom matcher pattern requires %#x, want 0", got)
	}
	rec := analyzer.NewPatternRecognizer("R", "X", "en", []*analyzer.Pattern{p})
	if got := rec.Analyze("zzzz", nil); len(got) != 1 {
		t.Errorf("custom matcher skipped: got %+v", got)
	}
}

type fixedMatcher struct{}

func (fixedMatcher) String() string { return "(?<=custom)" }
func (fixedMatcher) FindAll(text string) []analyzer.Match {
	return []analyzer.Match{analyzer.NewMatch([]int{0, len(text)})}
}

// The prefilter must never hide a match: for every built-in pattern and
// every corpus document, whenever FindAll returns a match the document — and
// the matched span itself, which is the tightest possible text — must carry
// every class the pattern claims to require.
func TestPrefilterNeverSkipsCorpusMatch(t *testing.T) {
	docs := loadCorpusTexts(t, 16<<10)
	checked, matched := 0, 0
	for _, rec := range recognizers.All() {
		pr, ok := rec.(*analyzer.PatternRecognizer)
		if !ok {
			continue
		}
		for _, p := range pr.Patterns() {
			req := p.Requires()
			for _, text := range docs {
				ms := p.FindAll(text)
				checked++
				if len(ms) == 0 {
					continue
				}
				matched++
				if got := analyzer.ByteClasses(text); got&req != req {
					t.Errorf("%s/%s: text has classes %#x, pattern requires %#x, yet it matches", rec.Name(), p.Name, got, req)
				}
				for _, m := range ms {
					start, end := m.Span(0)
					if got := analyzer.ByteClasses(text[start:end]); got&req != req {
						t.Errorf("%s/%s: match %q has classes %#x, pattern requires %#x", rec.Name(), p.Name, text[start:end], got, req)
					}
				}
			}
		}
	}
	if matched == 0 {
		t.Fatalf("no pattern matched any of %d checks; corpus unusable", checked)
	}
	t.Logf("%d pattern/doc checks, %d with matches", checked, matched)
}

// The prefilter's whole point: a prose line without a digit must skip every
// digit-requiring pattern, and the recognizer must still find what is there.
func TestPrefilterSkipsAndKeeps(t *testing.T) {
	rec := analyzer.NewPatternRecognizer("R", "NUM", "en", []*analyzer.Pattern{
		analyzer.MustPattern("nine", `\b\d{9}\b`, 0.5),
	})
	if got := rec.Analyze("no digits here at all", nil); len(got) != 0 {
		t.Errorf("prose produced %+v", got)
	}
	if got := rec.Analyze("id 123456789 ok", nil); len(got) != 1 || got[0].Start != 3 || got[0].End != 12 {
		t.Errorf("digit line produced %+v, want one span [3,12)", got)
	}
}

func loadCorpusTexts(t *testing.T, maxBytes int) []string {
	t.Helper()
	f, err := os.Open("../bench/corpus.jsonl")
	if err != nil {
		t.Skipf("parity corpus unavailable: %v", err)
	}
	defer f.Close()
	var texts []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 8<<20)
	for sc.Scan() {
		var d struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(sc.Bytes(), &d); err != nil {
			t.Fatal(err)
		}
		if len(d.Text) <= maxBytes {
			texts = append(texts, d.Text)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return texts
}
