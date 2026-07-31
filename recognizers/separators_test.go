package recognizers

import (
	"regexp"
	"strings"
	"testing"

	"github.com/hoophq/alcatraz/analyzer"
)

// psqlOutput is a tab-separated database result of the shape alcatraz sees
// when a caller streams query output through it. Every value in it is
// synthetic, and none of them is a phone number — but before separator classes
// were narrowed to hsp, four spans were reported anyway, each stitched out of
// two adjacent cells (`417768` + `2026`, `09.93009` + `2026`, ...).
const psqlOutput = "session_id\tyear\tuser_name\tuser_email\tstarted_at\tduration_ms\n" +
	"417768\t2026\tLuan\tluan@example.com\t13:45:09.93009\t2026\n" +
	"417769\t2026\tMaria\tmaria@example.com\t09:12:31.44021\t1204\n"

// boundary reports the spans of results that contain a tab, a newline or a
// carriage return — that is, spans stitched across a column or row boundary.
func boundary(text string, results []analyzer.Result) []string {
	var out []string
	for _, r := range results {
		if s := text[r.Start:r.End]; strings.ContainsAny(s, "\t\n\r") {
			out = append(out, s)
		}
	}
	return out
}

// TestPsqlFixtureExercisesTheBug keeps the fixture honest. If a future edit
// makes psqlOutput stop reproducing the original defect, the invariant tests
// below would still pass while testing nothing, so assert here that the
// pre-fix pattern really does stitch cells together on it.
func TestPsqlFixtureExercisesTheBug(t *testing.T) {
	// The Phone (BR) pattern exactly as it read before the fix: `\s` where
	// hsp now is.
	before := regexp.MustCompile(
		`(?:^|\W)((?:\+?55[\s.-]?)?(?:\(\d{2}\)|\d{2})[\s.-]?9?\d{4}[\s.-]?\d{4})\b`)

	var crossed []string
	for _, m := range before.FindAllStringSubmatch(psqlOutput, -1) {
		if strings.ContainsAny(m[1], "\t\n\r") {
			crossed = append(crossed, m[1])
		}
	}
	if len(crossed) == 0 {
		t.Fatal("psqlOutput no longer reproduces the cross-column defect; " +
			"the invariant tests below would pass vacuously")
	}
	t.Logf("pre-fix pattern stitched %d cross-cell matches: %q", len(crossed), crossed)
}

// TestNoDefaultRecognizerCrossesABoundary is the general invariant: no
// built-in recognizer may ever report a span that contains a tab or a newline.
// A tab is a column boundary and a newline is a row boundary; a single entity
// value never spans one.
func TestNoDefaultRecognizerCrossesABoundary(t *testing.T) {
	texts := map[string]string{
		"psql output": psqlOutput,
		"tsv ids":     "a\tb\tc\td\n100\t000\t0001\t2026\n123\t456\t782\t99\n",
		"row wrap":    "header\n100000\n0001\nfooter\n",
		"letters":     "AB\t12\t34\t56\tC\tnext\n",
		"aadhaar":     "w\tx\ty\tz\n2341\t2341\t2346\tq\n",
		"iban":        "cc\tacct\nDE89\t3704 0044 0532 0130 00\n",
		"dates":       "month\tday\tyear\nJanuary\t15,\t2024\nJan\t3\t2025\n",
		"url":         "site\tnote\nhttps://a\tredacted\nwww.b\tredacted\n",
		"medicare":    "a\tb\tc\n2123\t45670\t1\n",
	}
	for _, r := range All() {
		for name, text := range texts {
			if got := boundary(text, r.Analyze(text, nil)); len(got) != 0 {
				t.Errorf("%s on %s reported boundary-crossing spans %q",
					r.Name(), name, got)
			}
		}
	}
}

// TestBridgedIdentifiersAreNotDetected covers each recognizer whose separator
// class used to be built on `\s`. Every one of these used to be reported, and
// the checksum-backed ones at score 1.000: digitValues ignores the tab, so the
// checksum passed, and a passing validator is promoted to analyzer.MaxScore.
func TestBridgedIdentifiersAreNotDetected(t *testing.T) {
	tests := []struct {
		name string
		rec  analyzer.Recognizer
		text string
	}{
		{"nhs across columns", UKNHS(), "a\tb\tc\n943\t476\t5919\tz"},
		{"nhs across rows", UKNHS(), "header\n943476\n5919\n"},
		{"nino across columns", UKNINO(), "AB\t12\t34\t56\tC\tnext"},
		{"tfn across columns", AUTFN(), "a\tb\tc\n123\t456\t782\tq"},
		{"abn across columns", AUABN(), "a\tb\tc\td\n51\t824\t753\t556\tq"},
		{"acn across columns", AUACN(), "a\tb\tc\n004\t085\t616\tq"},
		{"medicare across columns", AUMedicare(), "a\tb\tc\n2123\t45670\t1\tz"},
		{"aadhaar across columns", INAadhaar(), "x\ty\tz\n2341\t2341\t2346\tw"},
		{"vehicle across columns", INVehicle(), "st\tno\n KA\t01\tAB\t1234\t"},
		{"iban across columns", IBAN(), "cc\tv\nDE89\t3704\t0044\t0532\t0130\t00\t"},
		{"us phone across columns", Phone(), "a\tb\tc\n415\t555\t2671\tz"},
		{"br phone across columns", Phone(), "id\tyear\n417768\t2026\tz"},
		{"intl phone across columns", Phone(), "cc\tnum\n+55\t11912345678\t"},
		{"month date across columns", DateTime(), "m\td\ty\nJanuary\t15,\t2024\t"},
		{"abbrev date across columns", DateTime(), "m\td\ty\nJan\t15,\t2024\t"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := boundary(tt.text, tt.rec.Analyze(tt.text, nil)); len(got) != 0 {
				t.Fatalf("%s.Analyze(%q) reported %q, want no boundary-crossing span",
					tt.rec.Name(), tt.text, got)
			}
		})
	}
}

// TestSpaceSeparatedIdentifiersStillDetected is the other half: narrowing the
// class must not cost a single real value. These are the space-separated forms
// of the same identifiers, and every checksum-backed one must still reach
// MaxScore.
func TestSpaceSeparatedIdentifiersStillDetected(t *testing.T) {
	tests := []struct {
		name  string
		rec   analyzer.Recognizer
		text  string
		want  string
		score float64
	}{
		{"nhs", UKNHS(), "nhs 943 476 5919 on file", "943 476 5919", analyzer.MaxScore},
		{"nino", UKNINO(), "nino AB 12 34 56 C here", "AB 12 34 56 C", analyzer.MaxScore},
		{"tfn", AUTFN(), "tfn 123 456 782 ok", "123 456 782", analyzer.MaxScore},
		{"abn", AUABN(), "abn 51 824 753 556 ok", "51 824 753 556", analyzer.MaxScore},
		{"acn", AUACN(), "acn 004 085 616 ok", "004 085 616", analyzer.MaxScore},
		{"medicare", AUMedicare(), "medicare 2123 45670 1 ok", "2123 45670 1", analyzer.MaxScore},
		{"aadhaar", INAadhaar(), "aadhaar 2341 2341 2346 ok", "2341 2341 2346", analyzer.MaxScore},
		{"iban", IBAN(), "iban DE89 3704 0044 0532 0130 00 ok",
			"DE89 3704 0044 0532 0130 00", analyzer.MaxScore},
		{"month date", DateTime(), "born January 15, 2024 in Lisbon", "January 15, 2024", 0.8},
		{"abbrev date", DateTime(), "born Jan 15, 2024 in Lisbon", "Jan 15, 2024", 0.7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			var score float64
			for _, r := range tt.rec.Analyze(tt.text, nil) {
				if s := tt.text[r.Start:r.End]; s == tt.want {
					got, score = append(got, s), r.Score
				}
			}
			if len(got) == 0 {
				t.Fatalf("%s.Analyze(%q) did not report %q",
					tt.rec.Name(), tt.text, tt.want)
			}
			if score != tt.score {
				t.Fatalf("%s.Analyze(%q) scored %q at %.2f, want %.2f",
					tt.rec.Name(), tt.text, tt.want, score, tt.score)
			}
		})
	}
}

// TestNoBreakSpaceSeparatorsAreDetected is what hsp buys over a bare literal
// space: values pasted out of a web page or formatted for a European locale
// carry U+00A0 or U+202F between the groups, and they are still real values.
func TestNoBreakSpaceSeparatorsAreDetected(t *testing.T) {
	tests := []struct {
		name string
		rec  analyzer.Recognizer
		text string
		want string
	}{
		{"nhs nbsp", UKNHS(), "nhs 943\u00a0476\u00a05919 ok", "943\u00a0476\u00a05919"},
		{"tfn narrow nbsp", AUTFN(), "tfn 123\u202f456\u202f782 ok", "123\u202f456\u202f782"},
		{"us phone nbsp", Phone(), "call 415\u00a0555\u00a02671 now", "415\u00a0555\u00a02671"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, r := range tt.rec.Analyze(tt.text, nil) {
				got = append(got, tt.text[r.Start:r.End])
			}
			for _, g := range got {
				if g == tt.want {
					return
				}
			}
			t.Fatalf("%s.Analyze(%q) = %q, want it to contain %q",
				tt.rec.Name(), tt.text, got, tt.want)
		})
	}
}

// TestPhoneAfterAColumnBoundaryKeepsTheRealNumber is why the fix belongs in
// the separator class and not in a post-filter that drops boundary-crossing
// spans. The greedy pre-fix match here was "1\t415-555-2671": discarding it
// would have thrown away a real phone number along with the false positive.
func TestPhoneAfterAColumnBoundaryKeepsTheRealNumber(t *testing.T) {
	const text = "count\t1\t415-555-2671"
	var got []string
	for _, r := range Phone().Analyze(text, nil) {
		got = append(got, text[r.Start:r.End])
	}
	if len(got) != 1 || got[0] != "415-555-2671" {
		t.Fatalf("Phone().Analyze(%q) = %q, want [\"415-555-2671\"]", text, got)
	}
}

// TestPsqlOutputKeepsItsRealEntities guards the recall side of the fixture:
// narrowing the separator classes must not have cost the values that really
// are there.
func TestPsqlOutputKeepsItsRealEntities(t *testing.T) {
	reg := analyzer.NewRegistry()
	LoadDefaults(reg, "en")
	eng := analyzer.NewEngine(reg, []string{"en"})

	found := map[string]bool{}
	for _, r := range eng.Analyze(psqlOutput, analyzer.Options{Language: "en"}) {
		found[psqlOutput[r.Start:r.End]] = true
	}
	for _, want := range []string{"luan@example.com", "maria@example.com"} {
		if !found[want] {
			t.Errorf("engine no longer detects %q in the psql fixture", want)
		}
	}
}
