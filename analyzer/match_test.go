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

// Patterns with the same source share one compiled program and, within an
// Analyze call, one scan. Each pattern must still apply its own group, score
// and recognizer to the shared matches.
func TestSharedProgramKeepsPerPatternSettings(t *testing.T) {
	const src = `(\d{2})-(\d{2})`
	left := NewPatternRecognizer("Left", "LEFT", "en",
		[]*Pattern{MustPattern("left", src, 0.3).WithGroup(1)})
	right := NewPatternRecognizer("Right", "RIGHT", "en",
		[]*Pattern{MustPattern("right", src, 0.7).WithGroup(2)})
	if left.patterns[0].prog != right.patterns[0].prog {
		t.Fatal("identical sources did not share a program")
	}
	reg := NewRegistry("en")
	reg.Add("en", left)
	reg.Add("en", right)
	eng := NewEngine(reg, []string{"en"})
	eng.SetContextEnhancer(nil)

	got := eng.Analyze("id 12-34 and 56-78", Options{})
	want := []Result{
		{EntityType: "RIGHT", Start: 6, End: 8, Score: 0.7, Text: "34", RecognizerName: "Right"},
		{EntityType: "RIGHT", Start: 16, End: 18, Score: 0.7, Text: "78", RecognizerName: "Right"},
		{EntityType: "LEFT", Start: 3, End: 5, Score: 0.3, Text: "12", RecognizerName: "Left"},
		{EntityType: "LEFT", Start: 13, End: 15, Score: 0.3, Text: "56", RecognizerName: "Left"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("result %d: got %+v, want %+v", i, got[i], want[i])
		}
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
