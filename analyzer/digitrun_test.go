package analyzer

import (
	"fmt"
	"math/rand/v2"
	"regexp"
	"regexp/syntax"
	"testing"
)

// The digit-run scan replaces the regex for every \b\d{min,max}\b pattern,
// so it must report exactly the spans the regex would: same runs, same
// boundaries, for every mix of digits, word bytes, separators and
// non-ASCII the text can throw at \b.
func TestDigitRunsMatchRegex(t *testing.T) {
	alphabet := []string{"0", "1", "9", "a", "Z", "_", " ", "-", ".", "\n", "é", "٣", "@"}
	rng := rand.New(rand.NewPCG(1, 2))
	texts := []string{
		"", "1", "12345678901", "a12345678901", "12345678901b", "_123", "123_",
		"cpf 12345678901 e cnpj 12345678000195.", "é123é", "٣123", "x1 22 333 4444",
	}
	for range 2000 {
		n := rng.IntN(40)
		s := ""
		for range n {
			s += alphabet[rng.IntN(len(alphabet))]
		}
		texts = append(texts, s)
	}

	shapes := []struct {
		regex    string
		min, max int
	}{
		{`\b\d{11}\b`, 11, 11},
		{`\b[0-9]{9}\b`, 9, 9},
		{`\b\d{8,17}\b`, 8, 17},
		{`\b\d{1,2}\b`, 1, 2},
		{`\b\d+\b`, 1, -1},
	}
	for _, sh := range shapes {
		re := regexp.MustCompile(sh.regex)
		parsed, err := syntax.Parse(sh.regex, syntax.Perl)
		if err != nil {
			t.Fatal(err)
		}
		run, ok := parseDigitRun(parsed)
		if !ok || run.min != sh.min || run.max != sh.max {
			t.Fatalf("%s: parsed as %+v ok=%v, want min=%d max=%d", sh.regex, run, ok, sh.min, sh.max)
		}
		for _, text := range texts {
			want := re.FindAllStringSubmatchIndex(text, -1)
			buf := make([]int, countDigitRuns(text))
			digitRuns(buf, text)
			got := matchDigitRuns(buf, run)
			if len(got) != len(want) {
				t.Fatalf("%s on %q: got %d matches, regex %d", sh.regex, text, len(got), len(want))
			}
			for i := range want {
				s, e := got[i].Span(0)
				if s != want[i][0] || e != want[i][1] {
					t.Fatalf("%s on %q: match %d = [%d,%d), regex [%d,%d)", sh.regex, text, i, s, e, want[i][0], want[i][1])
				}
			}
		}
	}
}

// Only the exact \b\d{min,max}\b shape qualifies; anything with another
// element must keep its regex.
func TestParseDigitRunRejectsOtherShapes(t *testing.T) {
	for _, regex := range []string{
		`\d{11}`, `\b\d{3}-\d{2}\b`, `\b\d{11}`, `\b[0-8]{9}\b`, `\b\d*\b`,
		`\b\d{11}\b|x`, `(\b\d{11}\b)`, `\b[0-9a-f]{9}\b`,
	} {
		parsed, err := syntax.Parse(regex, syntax.Perl)
		if err != nil {
			t.Fatal(err)
		}
		if run, ok := parseDigitRun(parsed); ok {
			t.Errorf("%s: parsed as digit run %+v", regex, run)
		}
	}
}

// A text with more runs than the stack buffer holds is served from the
// heap with the same matches.
func TestDigitRunsOverflowBuffer(t *testing.T) {
	text := ""
	for i := range 100 {
		text += fmt.Sprintf("%011d ", i)
	}
	p := MustPattern("cpf", `\b\d{11}\b`, 0.3)
	var scans scanCache
	got := scans.findAll(p, text)
	if len(got) != 100 {
		t.Fatalf("got %d matches, want 100", len(got))
	}
	if scans.runsHeap == nil {
		t.Fatal("expected the heap path for 100 runs")
	}
	if s, e := got[99].Span(0); text[s:e] != "00000000099" {
		t.Fatalf("last match %q", text[s:e])
	}
}
