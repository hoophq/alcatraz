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

	t.Run("ties between types keep input order", func(t *testing.T) {
		// Same span, same score, two types, in an order the alphabet
		// would reverse: the anonymizer resolves such overlaps by order,
		// so de-duplication must not reorder them.
		in := []Result{
			{EntityType: P, Start: 0, End: 5, Score: 0.5},
			{EntityType: E, Start: 0, End: 5, Score: 0.5},
		}
		got := RemoveDuplicates(in)
		if len(got) != 2 || got[0].EntityType != P || got[1].EntityType != E {
			t.Fatalf("want P then E, got %+v", got)
		}
	})

	t.Run("input slice is not modified", func(t *testing.T) {
		in := []Result{
			{EntityType: E, Start: 5, End: 9, Score: 0.5},
			{EntityType: E, Start: 0, End: 3, Score: 0.9},
		}
		orig := append([]Result(nil), in...)
		got := RemoveDuplicates(in)
		if len(got) != 2 || got[0].Start != 0 {
			t.Fatalf("got %+v", got)
		}
		for i := range in {
			if in[i] != orig[i] {
				t.Fatalf("input modified at %d: %+v, was %+v", i, in[i], orig[i])
			}
		}
	})

	t.Run("many types each keep their own containment", func(t *testing.T) {
		// More types than the stack table holds, interleaved, each with a
		// container and an inner span.
		var in []Result
		for i := range 20 {
			typ := string(rune('A' + i))
			in = append(in,
				Result{EntityType: typ, Start: 0, End: 10, Score: 0.5},
				Result{EntityType: typ, Start: 2, End: 4, Score: 0.9},
			)
		}
		got := RemoveDuplicates(in)
		if len(got) != 20 {
			t.Fatalf("want 20 containers, got %d: %+v", len(got), got)
		}
		for _, r := range got {
			if r.Start != 0 || r.End != 10 {
				t.Fatalf("inner span survived: %+v", r)
			}
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

// Entity names are not restricted, so the cache key must not be fooled by
// a name that contains whatever separates the key's elements.
func TestRegistryRecognizersCacheKeyIsUnambiguous(t *testing.T) {
	reg := NewRegistry("en")
	ab := NewPatternRecognizer("ab", "a\x00b", "en", []*Pattern{MustPattern("x", `x`, 0.4)})
	a := NewPatternRecognizer("a", "a", "en", []*Pattern{MustPattern("x", `x`, 0.4)})
	b := NewPatternRecognizer("b", "b", "en", []*Pattern{MustPattern("x", `x`, 0.4)})
	reg.Add("en", ab)
	reg.Add("en", a)
	reg.Add("en", b)

	if got := reg.Recognizers("en", []string{"a\x00b"}); len(got) != 1 || got[0] != Recognizer(ab) {
		t.Fatalf("[a\\x00b]: got %v", got)
	}
	if got := reg.Recognizers("en", []string{"a", "b"}); len(got) != 2 || got[0] != Recognizer(a) || got[1] != Recognizer(b) {
		t.Fatalf("[a b] after [a\\x00b] was cached: got %v", got)
	}
}

// The context words are a snapshot: neither the caller's slice nor the one
// Context returns can change what the enhancer sees.
func TestWithContextSnapshots(t *testing.T) {
	words := []string{"card"}
	rec := NewPatternRecognizer("r", "DIGITS", "en", []*Pattern{MustPattern("d", `\d+`, 0.3)}).
		WithContext(words...)
	words[0] = "other"
	rec.Context()[0] = "another"

	reg := NewRegistry("en")
	reg.Add("en", rec)
	got := NewEngine(reg, []string{"en"}).Analyze("card 1234", Options{})
	if len(got) != 1 || got[0].Score < 0.6 {
		t.Fatalf("want the card context boost, got %+v", got)
	}
	if ctx := rec.Context(); len(ctx) != 1 || ctx[0] != "card" {
		t.Fatalf("Context() = %v, want [card]", ctx)
	}
}

// A slice handed out by SupportedEntities is the caller's to change.
func TestSupportedEntitiesIsACopy(t *testing.T) {
	rec := NewPatternRecognizer("r", "DIGITS", "en", []*Pattern{MustPattern("d", `\d+`, 0.3)})
	rec.SupportedEntities()[0] = "OTHER"
	if got := rec.SupportedEntities(); got[0] != "DIGITS" {
		t.Fatalf("SupportedEntities = %v, want [DIGITS]", got)
	}
	if got := rec.Analyze("1234", []string{"DIGITS"}); len(got) != 1 {
		t.Fatalf("filtered Analyze after mutation: %+v", got)
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
