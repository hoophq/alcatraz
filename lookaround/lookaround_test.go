package lookaround

import (
	"strings"
	"testing"
	"time"

	"github.com/hoophq/alcatraz/analyzer"
)

// spanText returns the byte substring of the first match's group 0.
func firstWhole(t *testing.T, m *Matcher, text string) (string, int, int) {
	t.Helper()
	all := m.FindAll(text)
	if len(all) == 0 {
		return "", -1, -1
	}
	s, e := all[0].Span(0)
	return text[s:e], s, e
}

func TestLookbehind(t *testing.T) {
	m := MustCompile(`(?<=token=)\w+`)

	got, _, _ := firstWhole(t, m, "auth token=ABC123XYZ end")
	if got != "ABC123XYZ" {
		t.Errorf("lookbehind: got %q, want ABC123XYZ", got)
	}

	// The same token text without the required prefix must not match.
	if all := m.FindAll("auth ABC123XYZ end"); len(all) != 0 {
		t.Errorf("lookbehind: expected no match without token= prefix, got %d", len(all))
	}
}

func TestNegativeLookahead(t *testing.T) {
	// Three digits NOT followed by another digit.
	m := MustCompile(`\d{3}(?!\d)`)
	var got []string
	for _, mt := range m.FindAll("1234 and 567") {
		s, e := mt.Span(0)
		got = append(got, "1234 and 567"[s:e])
	}
	want := []string{"234", "567"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("negative lookahead: got %v, want %v", got, want)
	}
}

func TestPositiveLookahead(t *testing.T) {
	// A number that is immediately followed by "USD".
	m := MustCompile(`\d+(?=USD)`)
	got, _, _ := firstWhole(t, m, "price 100USD and 50EUR")
	if got != "100" {
		t.Errorf("positive lookahead: got %q, want 100", got)
	}
}

// TestUnicodeByteOffsets is the critical correctness test: regexp2 reports rune
// offsets, alcatraz works in bytes. A multibyte prefix must not corrupt the
// reported span.
func TestUnicodeByteOffsets(t *testing.T) {
	// "café " is 6 bytes (é is 2 bytes) but 5 runes.
	text := "café token=SECRET42 ☺"
	m := MustCompile(`(?<=token=)\w+`)
	all := m.FindAll(text)
	if len(all) != 1 {
		t.Fatalf("want 1 match, got %d", len(all))
	}
	s, e := all[0].Span(0)
	if text[s:e] != "SECRET42" {
		t.Errorf("byte span wrong: got %q (bytes %d..%d), want SECRET42", text[s:e], s, e)
	}
	// Verify the offsets are byte offsets, not rune offsets. "SECRET42" begins
	// at byte 12 (café=5 bytes + space=1 + "token="=6) but at rune 11 — the é
	// contributes the extra byte. Getting 12 (not 11) proves the rune→byte
	// conversion ran.
	if s != 12 {
		t.Errorf("expected byte start 12, got %d (rune start would be 11)", s)
	}
}

func TestCaptureGroupSpan(t *testing.T) {
	// Report only the domain via group 2.
	m := MustCompile(`(?<=user:)(\w+)@(\w+)`)
	all := m.FindAll("contact user:alice@example then")
	if len(all) != 1 {
		t.Fatalf("want 1 match, got %d", len(all))
	}
	s, e := all[0].Span(2)
	if "contact user:alice@example then"[s:e] != "example" {
		t.Errorf("group 2: got %q, want example", "contact user:alice@example then"[s:e])
	}
	// A non-participating group reports (-1,-1).
	if s, e := all[0].Span(9); s != -1 || e != -1 {
		t.Errorf("out-of-range group should be (-1,-1), got (%d,%d)", s, e)
	}
}

// TestEndToEndThroughEngine wires a user-configured lookaround rule through the
// standard alcatraz engine — the actual "way to C" the user asked for.
func TestEndToEndThroughEngine(t *testing.T) {
	rec, err := NewRecognizer("SecretRule", "API_SECRET", "en",
		Spec{Name: "bearer", Regex: `(?<=Authorization: Bearer )[A-Za-z0-9._-]{8,}`, Score: 0.95},
		Spec{Name: "domain", Regex: `(?<=@)(\w+)\.com`, Score: 0.6, Group: 1},
	)
	if err != nil {
		t.Fatal(err)
	}

	reg := analyzer.NewRegistry("en")
	reg.Add("en", rec)
	eng := analyzer.NewEngine(reg, []string{"en"})

	got := eng.Analyze("Authorization: Bearer abc123.def456 to bob@acme.com", analyzer.Options{})
	if len(got) != 2 {
		t.Fatalf("want 2 results, got %d: %+v", len(got), got)
	}
	found := map[string]bool{}
	for _, r := range got {
		found[r.Text] = true
		if r.EntityType != "API_SECRET" {
			t.Errorf("entity type: got %q, want API_SECRET", r.EntityType)
		}
	}
	if !found["abc123.def456"] {
		t.Errorf("missing bearer token; got %+v", got)
	}
	if !found["acme"] { // group 1 of (?<=@)(\w+)\.com
		t.Errorf("missing domain capture group; got %+v", got)
	}
}

// TestTimeoutBoundsBacktracking proves the ReDoS guard: a catastrophic pattern
// against adversarial input returns promptly instead of hanging.
func TestTimeoutBoundsBacktracking(t *testing.T) {
	m, err := CompileWithTimeout(`(a+)+$`, 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	evil := strings.Repeat("a", 40) + "!" // never matches; exponential without a cap

	done := make(chan []analyzer.Match, 1)
	go func() { done <- m.FindAll(evil) }()

	select {
	case res := <-done:
		if len(res) != 0 {
			t.Errorf("expected no match for %q, got %d", evil, len(res))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("FindAll did not honor MatchTimeout — backtracking ran unbounded")
	}
}
