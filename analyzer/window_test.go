package analyzer

import (
	"fmt"
	"math/rand/v2"
	"regexp"
	"regexp/syntax"
	"testing"
)

// windowShapes are the regex shapes the built-in set relies on the
// windowed scan for: fixed and ranged digit groups, separators, \p{Zs}
// (which admits non-ASCII), letters either side of the digits, case folding,
// \B, alternation, optional parts, capture groups, and the text and line
// anchors — including the (?:^|\W) prefix the phone patterns open with —
// whose truth at a window edge must never leak into a match.
var windowShapes = []string{
	`\b9\d{8}\b`,
	`\b\d{3}-\d{2}-\d{4}\b`,
	`\b\d{3}\p{Zs}?\d{3}\p{Zs}?\d{3}\b`,
	`\b\d{1,2}\.\d{3}\.\d{3}-[0-9Xx]\b`,
	`\b[A-Z]{2}\d{7}[A-Z]\b`,
	`\b[A-Z]{1,2}[0-9]{5,9}\b`,
	`\b\d{8,9}[A-Z]\b`,
	`(?i)\b[a-z]{3}\d[a-z]\d{2}\b`,
	`\B\+(?:[\p{Zs}.()-]{0,2}\d){8,15}\b`,
	`\b(?:0?[1-9]|1[0-2])[/-](?:0?[1-9]|[12]\d|3[01])[/-](?:\d{4})\b`,
	`\b(?:(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\b`,
	`(\d{2})-(\d{2})(?:-(\d{2}))?`,
	`\b[13][a-km-zA-HJ-NP-Z1-9]{25,34}\b`,
	`\d{6}[-+A]\d{3}[0-9A-FHJ-NPR-Y]`,
	`\b(?:Jan|Feb)\.?\p{Zs}+(?:\d{1,2}),?\p{Zs}+(?:\d{4})\b`,
	`(?:^|\W)((?:\+?1[-.\p{Zs}]?)?(?:\(\d{3}\)|\d{3})[-.\p{Zs}]?\d{3}[-.\p{Zs}]?\d{4})\b`,
	`(?:^|\W)(\d{2})`,
	`(\d{2})(?:$|\W)`,
	`^\d+`,
	`\d+$`,
	`\A\d{3}`,
	`\d{3}\z`,
	`(?m)^\d+`,
	`(?m)\d+$`,
	`(?m)^\s*\d+\s*$`,
	`(?s)\d.\d`,
}

// The windowed scan must report exactly what the regex reports on the whole
// text — same matches, same order, same group spans — whatever the text
// puts around the digits: word bytes, separators, non-ASCII spaces, runs
// that touch, and more digit groups than the stack buffer holds.
func TestWindowedScanMatchesRegex(t *testing.T) {
	alphabet := []string{
		"0", "1", "9", "a", "Z", "X", "_", " ", "-", ".", "/", "+", "(", ")", ",", "\n", "\t",
		"é", "٣", "\u00a0", "@", "AB", "123", "4567", "Jan ", "25.", "K", "ſ",
	}
	rng := rand.New(rand.NewPCG(3, 4))
	texts := []string{
		"", "1", "123-45-6789", "a123-45-6789", "123-45-6789b", "123-45-6789 987-65-4321",
		"AB1234567C", "ab1234567c", "+55 (11) 91234-5678", "1.2.3.4", "01/02/2024",
		"é123 456 789é", "123\u00a0456\u00a0789", "123 456 789\u00a0", "K12 34 56", "x1 22 333 4444",
		"Jan 5, 2024", "12-34-56-78", "٣123", "0x1234abcd", "9" + "123456789", "12345678A",
	}
	for range 3000 {
		n := rng.IntN(16)
		s := ""
		for range n {
			s += alphabet[rng.IntN(len(alphabet))]
		}
		texts = append(texts, s)
	}
	many := ""
	for i := range 100 {
		many += fmt.Sprintf("%03d-%02d-%04d ", i, i%100, i*7)
	}
	texts = append(texts, many)

	for _, regex := range windowShapes {
		re := regexp.MustCompile(regex)
		p := MustPattern("w", regex, 0.5)
		if !p.win.ok {
			t.Fatalf("%s: not windowed", regex)
		}
		for _, text := range texts {
			want := re.FindAllStringSubmatchIndex(text, -1)
			var scans scanCache
			got := scans.scan(p, text)
			if len(got) != len(want) {
				t.Fatalf("%s on %q: got %d matches %v, regex %d %v", regex, text, len(got), spans(got), len(want), want)
			}
			for i := range want {
				if got[i].NumGroups() != len(want[i])/2 {
					t.Fatalf("%s on %q: match %d has %d groups, regex %d", regex, text, i, got[i].NumGroups(), len(want[i])/2)
				}
				for g := range got[i].NumGroups() {
					s, e := got[i].Span(g)
					if s != want[i][2*g] || e != want[i][2*g+1] {
						t.Fatalf("%s on %q: match %d group %d = [%d,%d), regex [%d,%d)", regex, text, i, g, s, e, want[i][2*g], want[i][2*g+1])
					}
				}
			}
			if text == many && scans.groupsHeap == nil {
				t.Fatalf("%s: expected the heap path for 300 digit groups", regex)
			}
		}
	}
}

func spans(ms []Match) [][]int {
	out := make([][]int, len(ms))
	for i, m := range ms {
		out[i] = m.loc
	}
	return out
}

// Shapes the window cannot serve keep the full scan: without a required
// digit a match may sit anywhere.
func TestParseWindowRejectsOtherShapes(t *testing.T) {
	for _, regex := range []string{
		`\b[A-Z]{5}\b`, `[a-z]+@[a-z]+`, `x\d?`, `\b[A-Z0-9]{7}\b`, `\d*`, `(?:\d|x)`,
	} {
		re, err := syntax.Parse(regex, syntax.Perl)
		if err != nil {
			t.Fatal(err)
		}
		if w := parseWindow(re, requiredClasses(re)); w.ok {
			t.Errorf("%s: windowed as %+v, want full scan", regex, w)
		}
	}
}

// The run filters are exact lower bounds: minLen never exceeds a real
// match, and the alphabet always covers one.
func TestWindowShape(t *testing.T) {
	for _, tc := range []struct {
		regex    string
		minLen   int
		alphabet classMask
	}{
		{`\b\d{3}-\d{2}-\d{4}\b`, 11, classDigit | byteClass['-']},
		{`\b\d{3}\p{Zs}?\d{3}\b`, 6, classDigit | classSpace | classOther},
		{`(?i)\b[a-z]{3}\d\b`, 4, classDigit | classLetter | classLower | classUpper | classOther},
		{`\b(?:0?[1-9]|1[0-2])/(?:\d{4})\b`, 6, classDigit | byteClass['/']},
		{`(?i)k\d`, 2, classDigit | classLetter | classLower | classUpper | classOther},
		{`\b\d+\b|\d{3}x`, 1, classDigit | classLetter | classLower},
	} {
		re, err := syntax.Parse(tc.regex, syntax.Perl)
		if err != nil {
			t.Fatal(err)
		}
		w := parseWindow(re, requiredClasses(re))
		if !w.ok || w.minLen != tc.minLen || w.alphabet != tc.alphabet {
			t.Errorf("%s: window %+v, want ok minLen=%d alphabet=%#x", tc.regex, w, tc.minLen, tc.alphabet)
		}
	}
}
