package analyzer

import "testing"

// TestPatternGroup verifies the capture-group span selector (B): a pattern can
// match surrounding context but report only the captured entity — the RE2
// idiom for emulating lookbehind/lookahead without a backtracking engine.
func TestPatternGroup(t *testing.T) {
	// Emulates (?<=user=)\w+ : require the "user=" prefix, report only the name.
	p := MustPattern("user", `user=(\w+)`, 0.9).WithGroup(1)
	rec := NewPatternRecognizer("UserRecognizer", "USER", "en", []*Pattern{p})
	reg := NewRegistry("en")
	reg.Add("en", rec)
	eng := NewEngine(reg, []string{"en"})

	got := eng.Analyze("login user=alice then logout", Options{})
	if len(got) != 1 {
		t.Fatalf("want 1 result, got %+v", got)
	}
	if got[0].Text != "alice" {
		t.Errorf("want reported span %q, got %q", "alice", got[0].Text)
	}
	// The reported offsets must be the group span, not the whole match
	// ("alice" begins at byte 11 in "login user=alice then logout").
	if got[0].Start != 11 || got[0].End != 16 {
		t.Errorf("want span [11,16) for alice, got [%d,%d)", got[0].Start, got[0].End)
	}

	// Without the required prefix there is no match.
	if got := eng.Analyze("login alice then logout", Options{}); len(got) != 0 {
		t.Errorf("want no match without user= prefix, got %+v", got)
	}
}

// TestContextValidator verifies the context-aware filter (A): a recognizer can
// keep a match only when its surroundings satisfy a predicate.
func TestContextValidator(t *testing.T) {
	// Match any 4-digit run, but keep it only when immediately preceded by
	// "PIN " — a negative/positive lookbehind expressed in plain Go.
	p := MustPattern("four-digits", `\d{4}`, 0.5)
	rec := NewPatternRecognizer("PinRecognizer", "PIN", "en", []*Pattern{p}).
		WithContextValidator(func(text string, start, end int) bool {
			const prefix = "PIN "
			return start >= len(prefix) && text[start-len(prefix):start] == prefix
		})
	reg := NewRegistry("en")
	reg.Add("en", rec)
	eng := NewEngine(reg, []string{"en"})

	got := eng.Analyze("PIN 1234 but year 5678", Options{})
	if len(got) != 1 {
		t.Fatalf("want 1 result, got %+v", got)
	}
	if got[0].Text != "1234" {
		t.Errorf("want 1234 (preceded by PIN ), got %q", got[0].Text)
	}
	// A context filter must NOT inflate the score the way a checksum validator
	// does: it only filters.
	if got[0].Score != 0.5 {
		t.Errorf("context filter should preserve base score 0.5, got %v", got[0].Score)
	}
}

// TestContextValidatorWithStructuralValidator verifies the two hooks compose:
// the structural validator promotes to MaxScore, the context validator filters.
func TestContextValidatorWithStructuralValidator(t *testing.T) {
	p := MustPattern("digits", `\d{4}`, 0.2)
	rec := NewPatternRecognizer("R", "X", "en", []*Pattern{p}).
		WithValidator(func(match string) bool { return match != "0000" }).
		WithContextValidator(func(text string, start, end int) bool {
			return start == 0 // only at the very start of the text
		})
	reg := NewRegistry("en")
	reg.Add("en", rec)
	eng := NewEngine(reg, []string{"en"})

	// "1234" at offset 0: passes validator (promote to 1.0) and context (start==0).
	got := eng.Analyze("1234 0000 5678", Options{})
	if len(got) != 1 || got[0].Text != "1234" {
		t.Fatalf("want only 1234, got %+v", got)
	}
	if got[0].Score != MaxScore {
		t.Errorf("structural validator should promote to %v, got %v", MaxScore, got[0].Score)
	}
}
