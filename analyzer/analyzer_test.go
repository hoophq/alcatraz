package analyzer

import (
	"testing"
)

func TestResultGeometry(t *testing.T) {
	a := Result{Start: 0, End: 10}
	b := Result{Start: 2, End: 6}
	c := Result{Start: 5, End: 15}

	if !b.ContainedIn(a) {
		t.Error("b should be contained in a")
	}
	if !a.Contains(b) {
		t.Error("a should contain b")
	}
	if a.Intersects(c) != 5 {
		t.Errorf("a∩c = %d, want 5", a.Intersects(c))
	}
	if b.Intersects(c) != 1 {
		t.Errorf("b∩c = %d, want 1", b.Intersects(c))
	}
	d := Result{Start: 20, End: 25}
	if a.Intersects(d) != 0 {
		t.Errorf("a∩d = %d, want 0", a.Intersects(d))
	}
}

func TestRemoveDuplicates(t *testing.T) {
	const E, P = "EMAIL", "PERSON"

	t.Run("contained span dropped, container kept", func(t *testing.T) {
		got := RemoveDuplicates([]Result{
			{EntityType: E, Start: 0, End: 10, Score: 0.5},
			{EntityType: E, Start: 2, End: 6, Score: 0.9},
		})
		if len(got) != 1 || got[0].Start != 0 || got[0].End != 10 {
			t.Fatalf("want single [0,10), got %+v", got)
		}
	})

	t.Run("later span subsumes earlier", func(t *testing.T) {
		got := RemoveDuplicates([]Result{
			{EntityType: E, Start: 2, End: 6, Score: 0.9},
			{EntityType: E, Start: 0, End: 10, Score: 0.5},
		})
		if len(got) != 1 || got[0].Start != 0 || got[0].End != 10 {
			t.Fatalf("want single [0,10), got %+v", got)
		}
	})

	t.Run("partial overlap keeps both", func(t *testing.T) {
		got := RemoveDuplicates([]Result{
			{EntityType: E, Start: 0, End: 5, Score: 0.5},
			{EntityType: E, Start: 3, End: 8, Score: 0.9},
		})
		if len(got) != 2 {
			t.Fatalf("want 2, got %+v", got)
		}
		// sorted by score desc
		if got[0].Score != 0.9 || got[1].Score != 0.5 {
			t.Errorf("want score order 0.9,0.5, got %v,%v", got[0].Score, got[1].Score)
		}
	})

	t.Run("different entity types coexist", func(t *testing.T) {
		got := RemoveDuplicates([]Result{
			{EntityType: E, Start: 0, End: 5, Score: 0.5},
			{EntityType: P, Start: 0, End: 5, Score: 0.9},
		})
		if len(got) != 2 {
			t.Fatalf("want 2 (different types), got %+v", got)
		}
	})

	t.Run("zero score dropped", func(t *testing.T) {
		got := RemoveDuplicates([]Result{
			{EntityType: E, Start: 0, End: 5, Score: 0.0},
			{EntityType: E, Start: 10, End: 15, Score: 0.5},
		})
		if len(got) != 1 || got[0].Start != 10 {
			t.Fatalf("want single [10,15), got %+v", got)
		}
	})
}

// testEngine builds an engine with two simple recognizers: digit runs (DIGITS,
// 0.4) and lowercase words (WORD, 0.8).
func testEngine() *Engine {
	reg := NewRegistry("en")
	reg.Add("en", NewPatternRecognizer("digits", "DIGITS", "en",
		[]*Pattern{MustPattern("digits", `\d+`, 0.4)}))
	reg.Add("en", NewPatternRecognizer("words", "WORD", "en",
		[]*Pattern{MustPattern("words", `[a-z]+`, 0.8)}))
	return NewEngine(reg, []string{"en"})
}

func found(results []Result, entity, text string) bool {
	for _, r := range results {
		if r.EntityType == entity && r.Text == text {
			return true
		}
	}
	return false
}

func TestEngineTextFilled(t *testing.T) {
	got := testEngine().Analyze("abc 123", Options{})
	if !found(got, "WORD", "abc") {
		t.Errorf("want WORD abc, got %+v", got)
	}
	if !found(got, "DIGITS", "123") {
		t.Errorf("want DIGITS 123, got %+v", got)
	}
}

func TestEngineThreshold(t *testing.T) {
	th := 0.5
	got := testEngine().Analyze("abc 123", Options{Threshold: &th})
	if found(got, "DIGITS", "123") {
		t.Errorf("DIGITS (0.4) should be below threshold 0.5: %+v", got)
	}
	if !found(got, "WORD", "abc") {
		t.Errorf("WORD (0.8) should survive threshold: %+v", got)
	}
}

func TestEngineEntitiesFilter(t *testing.T) {
	got := testEngine().Analyze("abc 123", Options{Entities: []string{"WORD"}})
	if found(got, "DIGITS", "123") {
		t.Errorf("DIGITS recognizer should not run when filtering to WORD: %+v", got)
	}
	if !found(got, "WORD", "abc") {
		t.Errorf("want WORD abc, got %+v", got)
	}
}

func TestEngineAllowListExact(t *testing.T) {
	got := testEngine().Analyze("abc 123", Options{AllowList: []string{"abc"}})
	if found(got, "WORD", "abc") {
		t.Errorf("allow-listed abc should be dropped: %+v", got)
	}
	if !found(got, "DIGITS", "123") {
		t.Errorf("want DIGITS 123, got %+v", got)
	}
}

func TestEngineAllowListRegex(t *testing.T) {
	got := testEngine().Analyze("abc 123", Options{
		AllowList:      []string{`\d+`},
		AllowListRegex: true,
	})
	if found(got, "DIGITS", "123") {
		t.Errorf("regex-allow-listed digits should be dropped: %+v", got)
	}
	if !found(got, "WORD", "abc") {
		t.Errorf("want WORD abc, got %+v", got)
	}
}
