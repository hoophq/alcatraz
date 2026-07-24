package recognizers

import "testing"

// phoneMatches runs the phone recognizer over text and returns the matched
// substrings (Result.Text is only filled by the engine, so slice by span).
func phoneMatches(t *testing.T, text string) []string {
	t.Helper()
	var got []string
	for _, r := range Phone().Analyze(text, nil) {
		got = append(got, text[r.Start:r.End])
	}
	return got
}

func TestPhoneDetects(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{"us dashed", "call 415-555-2671 now", "415-555-2671"},
		{"us parenthesized", "call (415) 555-2671 now", "(415) 555-2671"},
		{"us with plus prefix", "call +1 (415) 555-2671 now", "+1 (415) 555-2671"},
		{"br mobile e164", "contato +55 11 91234-5678 obrigado", "+55 11 91234-5678"},
		{"br mobile e164 compact", "tel +5511912345678.", "+5511912345678"},
		{"br mobile bare with ddd", "celular 11912345678 cadastrado", "11912345678"},
		{"br mobile formatted", "ligue (11) 91234-5678 hoje", "(11) 91234-5678"},
		{"br formatted at text start", "(11) 91234-5678 hoje", "(11) 91234-5678"},
		{"uk e164 compact", "office +442071838750 line", "+442071838750"},
		{"e164 spaced", "reach me at +49 30 901820 today", "+49 30 901820"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := phoneMatches(t, tt.text)
			if len(got) != 1 || got[0] != tt.want {
				t.Fatalf("Phone().Analyze(%q) = %v, want [%q]", tt.text, got, tt.want)
			}
		})
	}
}

func TestPhoneRejects(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{"credit card length run", "card 4532015112830366 on file"},
		{"arithmetic plus", "total 2+34567890"},
		{"plus with too many digits", "id +123456789012345678"},
		{"plus with too few digits", "code +1234567"},
		{"short digit run", "order 12345678"},
		{"iso date", "created 2026-07-24"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := phoneMatches(t, tt.text); len(got) != 0 {
				t.Fatalf("Phone().Analyze(%q) = %v, want no matches", tt.text, got)
			}
		})
	}
}
