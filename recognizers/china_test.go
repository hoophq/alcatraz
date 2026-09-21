package recognizers

import (
	"slices"
	"strings"
	"testing"

	"github.com/hoophq/alcatraz/analyzer"
	"github.com/hoophq/alcatraz/entities"
)

// These are the published examples in GB 11643-1999 Appendix A, not customer
// records. All other complete identifiers below are generated test fixtures.
const cnExampleX = "11010519491231002X"
const cnExampleDigit = "440524188001010014"

// Independently derive a synthetic check character with the iterative MOD 11-2
// recurrence, rather than repeating the production weights/lookup table.
func cnTestID(body string) string {
	rem := 0
	for _, c := range body {
		rem = (rem + int(c-'0')) * 2 % 11
	}
	check := (12 - rem) % 11
	if check == 10 {
		return body + "X"
	}
	return body + string(rune('0'+check))
}

func TestCNIDCard(t *testing.T) {
	cases := []struct{ name, text, want string }{
		{"standard X", cnExampleX, cnExampleX},
		{"standard historical district", cnExampleDigit, cnExampleDigit},
		{"lowercase x", strings.ToLower(cnExampleX), strings.ToLower(cnExampleX)},
		{"Chinese adjacent", "身份证" + cnExampleX + "已核对", cnExampleX},
		{"traditional Chinese", "身份證：" + cnExampleX + "。", cnExampleX},
		{"English", "citizen identity: " + cnExampleX, cnExampleX},
		{"JSON", `{"identity":"` + cnExampleX + `"}`, cnExampleX},
		{"TSV", "name\tid\nexample\t" + cnExampleX + "\n", cnExampleX},
		{"leap year", cnTestID("11010520000229001"), cnTestID("11010520000229001")},
		{"bad check", "110105194912310020", ""},
		{"impossible date", cnTestID("11010520000230001"), ""},
		{"non leap century", cnTestID("11010519000229001"), ""},
		{"month zero", cnTestID("11010520000001001"), ""},
		{"month thirteen", cnTestID("11010520001301001"), ""},
		{"day zero", cnTestID("11010520000100001"), ""},
		{"year zero", cnTestID("11010500000101001"), ""},
		{"leading zero", cnTestID("01010520000101001"), ""},
		{"short", cnExampleX[:17], ""},
		{"legacy fifteen digits", "110105491231002", ""},
		{"leading digit", "1" + cnExampleX, ""},
		{"trailing digit", cnExampleDigit + "1", ""},
		{"ASCII token", "record_" + cnExampleX, ""},
		{"ASCII suffix", cnExampleX + "A", ""},
		{"Unicode leading digit", "１" + cnExampleX, ""},
		{"Unicode trailing digit", cnExampleX + "١", ""},
		{"split columns", "110105\t19491231\t002X", ""},
		{"split lines", "11010519491231\n002X", ""},
		{"ordinary code", `const identity: string = "";`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CNIDCard().Analyze(tc.text, nil)
			if tc.want == "" {
				if len(got) != 0 {
					t.Fatal("non-identifier produced a CN_ID_CARD finding")
				}
				return
			}
			if len(got) != 1 || got[0].EntityType != entities.CNIDCard ||
				tc.text[got[0].Start:got[0].End] != tc.want || got[0].Score != analyzer.MaxScore {
				t.Fatal("identifier was not detected at the exact byte span")
			}
		})
	}
}

func TestCNIDCardRegistrationAndFiltering(t *testing.T) {
	for _, lang := range []string{"en", "zh", "pt"} {
		t.Run(lang, func(t *testing.T) {
			reg := analyzer.NewRegistry(lang)
			LoadDefaults(reg, lang)
			eng := analyzer.NewEngine(reg, []string{lang})
			if !slices.Contains(eng.SupportedEntities(lang), entities.CNIDCard) {
				t.Fatal("CN_ID_CARD is missing from defaults")
			}
			text := "身份证" + cnExampleX + "，" + cnExampleDigit
			got := eng.Analyze(text, analyzer.Options{Language: lang, Entities: []string{entities.CNIDCard}})
			if len(got) != 2 || got[0].Text != cnExampleX || got[1].Text != cnExampleDigit {
				t.Fatal("engine did not preserve both exact identifier spans")
			}
			if got := eng.Analyze(cnExampleX, analyzer.Options{Language: lang, Entities: []string{entities.EmailAddress}}); len(got) != 0 {
				t.Fatal("entity filter was not respected")
			}
		})
	}
	if got := CNIDCard().Analyze(cnExampleX, []string{entities.EmailAddress}); len(got) != 0 {
		t.Fatal("recognizer ignored entity filter")
	}
}

func TestCNIDCardSingleCharacterCorruption(t *testing.T) {
	for i := range len(cnExampleDigit) {
		bad := []byte(cnExampleDigit)
		bad[i] = '0' + (bad[i]-'0'+1)%10
		if validateCNIDCard(string(bad)) {
			t.Fatalf("single-character corruption at position %d survived", i)
		}
	}
}

func FuzzCNIDCard(f *testing.F) {
	for _, text := range []string{"", cnExampleX, cnExampleDigit, "身份证" + cnExampleX + "结束", "\xff" + cnExampleX} {
		f.Add(text)
	}
	rec := CNIDCard()
	f.Fuzz(func(t *testing.T, text string) {
		if len(text) > 4096 {
			return
		}
		for _, r := range rec.Analyze(text, nil) {
			if r.Start < 0 || r.End > len(text) || r.End-r.Start != 18 ||
				!validateCNIDCard(text[r.Start:r.End]) {
				t.Fatal("invalid finding span or identifier")
			}
		}
	})
}
