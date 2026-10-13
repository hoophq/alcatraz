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

	t.Run("single zero score dropped", func(t *testing.T) {
		got := RemoveDuplicates([]Result{{EntityType: E, Start: 0, End: 5, Score: 0.0}})
		if len(got) != 0 {
			t.Fatalf("want nothing, got %+v", got)
		}
	})

	t.Run("single result kept", func(t *testing.T) {
		got := RemoveDuplicates([]Result{{EntityType: E, Start: 0, End: 5, Score: 0.5}})
		if len(got) != 1 || got[0].Start != 0 || got[0].Score != 0.5 {
			t.Fatalf("want the one result back, got %+v", got)
		}
	})

	t.Run("sorted by score, start, length", func(t *testing.T) {
		got := RemoveDuplicates([]Result{
			{EntityType: E, Start: 20, End: 25, Score: 0.5},
			{EntityType: P, Start: 0, End: 8, Score: 0.9},
			{EntityType: E, Start: 0, End: 5, Score: 0.9},
			{EntityType: P, Start: 10, End: 15, Score: 0.5},
		})
		want := []Result{
			{EntityType: P, Start: 0, End: 8, Score: 0.9},
			{EntityType: E, Start: 0, End: 5, Score: 0.9},
			{EntityType: P, Start: 10, End: 15, Score: 0.5},
			{EntityType: E, Start: 20, End: 25, Score: 0.5},
		}
		if len(got) != len(want) {
			t.Fatalf("got %+v, want %+v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("position %d: got %+v, want %+v", i, got[i], want[i])
			}
		}
	})

	t.Run("identical span keeps the higher score, earlier on a tie", func(t *testing.T) {
		got := RemoveDuplicates([]Result{
			{EntityType: E, Start: 0, End: 5, Score: 0.5, RecognizerName: "a"},
			{EntityType: E, Start: 0, End: 5, Score: 0.9, RecognizerName: "b"},
			{EntityType: E, Start: 0, End: 5, Score: 0.9, RecognizerName: "c"},
		})
		if len(got) != 1 || got[0].Score != 0.9 || got[0].RecognizerName != "b" {
			t.Fatalf("want the first 0.9 span, got %+v", got)
		}
	})

	t.Run("span contained in an earlier kept span, not the last", func(t *testing.T) {
		// A=[0,10) and B=[3,12) partially overlap and both survive; C=[4,6)
		// sits inside both and must go, whichever is compared last.
		got := RemoveDuplicates([]Result{
			{EntityType: E, Start: 4, End: 6, Score: 0.9},
			{EntityType: E, Start: 3, End: 12, Score: 0.5},
			{EntityType: E, Start: 0, End: 10, Score: 0.5},
		})
		if len(got) != 2 || got[0].Start != 0 || got[1].Start != 3 {
			t.Fatalf("want [0,10) and [3,12), got %+v", got)
		}
	})

	t.Run("zero-score container does not suppress", func(t *testing.T) {
		got := RemoveDuplicates([]Result{
			{EntityType: E, Start: 0, End: 10, Score: 0.0},
			{EntityType: E, Start: 2, End: 6, Score: 0.9},
		})
		if len(got) != 1 || got[0].Start != 2 {
			t.Fatalf("want the inner span, got %+v", got)
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

// A cached recognizer set must track the registry: the same filter is served
// from the cache, a recognizer added afterwards shows up, and recognizer
// order is the registration order whatever the filter's order.
func TestRegistryRecognizersCache(t *testing.T) {
	reg := NewRegistry("en")
	digits := NewPatternRecognizer("digits", "DIGITS", "en", []*Pattern{MustPattern("d", `\d+`, 0.4)})
	words := NewPatternRecognizer("words", "WORD", "en", []*Pattern{MustPattern("w", `[a-z]+`, 0.8)})
	reg.Add("en", digits)
	reg.Add("en", words)

	filter := []string{"WORD", "DIGITS"}
	first := reg.Recognizers("en", filter)
	if len(first) != 2 || first[0] != Recognizer(digits) || first[1] != Recognizer(words) {
		t.Fatalf("filtered set = %v, want [digits words]", first)
	}
	if again := reg.Recognizers("en", filter); len(again) != 2 || &again[0] != &first[0] {
		t.Errorf("second call did not hit the cache")
	}
	if got := reg.Recognizers("en", []string{"WORD"}); len(got) != 1 || got[0] != Recognizer(words) {
		t.Errorf("WORD filter = %v, want [words]", got)
	}
	if got := reg.Recognizers("en", []string{}); len(got) != 0 {
		t.Errorf("empty filter = %v, want none", got)
	}
	if got := reg.Recognizers("pt", filter); len(got) != 0 {
		t.Errorf("unknown language = %v, want none", got)
	}

	more := NewPatternRecognizer("more", "WORD", "en", []*Pattern{MustPattern("m", `[A-Z]+`, 0.8)})
	reg.Add("en", more)
	if got := reg.Recognizers("en", filter); len(got) != 3 || got[2] != Recognizer(more) {
		t.Errorf("after Add: %v, want [digits words more]", got)
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
