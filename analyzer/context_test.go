package analyzer

import (
	"reflect"
	"strings"
	"testing"
)

// testMaxWord is the word-length bound the scanners get in tests that are not
// about the bound itself: comfortably past every word they scan, so it never
// changes what they collect. Enhance derives the real value from the context
// words in play (contextWords).
const testMaxWord = 64

// ctxRecognizer reports one fixed span, so a test can choose the exact score
// and context words the enhancer will see.
type ctxRecognizer struct {
	name    string
	entity  string
	span    [2]int
	score   float64
	context []string
}

func (r *ctxRecognizer) Name() string                { return r.name }
func (r *ctxRecognizer) SupportedEntities() []string { return []string{r.entity} }
func (r *ctxRecognizer) SupportedLanguage() string   { return "en" }
func (r *ctxRecognizer) Context() []string           { return r.context }

func (r *ctxRecognizer) Analyze(text string, entities []string) []Result {
	if entities != nil && !supportsAny(r.SupportedEntities(), entities) {
		return nil
	}
	return []Result{{
		EntityType:     r.entity,
		Start:          r.span[0],
		End:            r.span[1],
		Score:          r.score,
		RecognizerName: r.name,
	}}
}

// plainRecognizer is the same detector without Context, i.e. a recognizer that
// is not a ContextualRecognizer at all.
type plainRecognizer struct {
	name   string
	entity string
	span   [2]int
	score  float64
}

func (r *plainRecognizer) Name() string                { return r.name }
func (r *plainRecognizer) SupportedEntities() []string { return []string{r.entity} }
func (r *plainRecognizer) SupportedLanguage() string   { return "en" }

func (r *plainRecognizer) Analyze(text string, entities []string) []Result {
	if entities != nil && !supportsAny(r.SupportedEntities(), entities) {
		return nil
	}
	return []Result{{
		EntityType:     r.entity,
		Start:          r.span[0],
		End:            r.span[1],
		Score:          r.score,
		RecognizerName: r.name,
	}}
}

func TestSplitWords(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"single word", "email", []string{"email"}},
		{"lower-cased", "Codice Fiscale", []string{"codice", "fiscale"}},
		{"phrase", "social security", []string{"social", "security"}},
		{"accented phrase", "cadastro de pessoas físicas",
			[]string{"cadastro", "de", "pessoas", "físicas"}},
		{"hyphen splits", "e-mail", []string{"e", "mail"}},
		{"underscore splits", "user_name", []string{"user", "name"}},
		{"digits stay in the word", "ipv4", []string{"ipv4"}},
		{"punctuation dropped", "  ssn: ", []string{"ssn"}},
		{"empty", "", nil},
		{"separators only", " -_/ ", nil},
		{"non-latin is one word", "주민등록번호", []string{"주민등록번호"}},
		{"thai is one word", "บัตรประชาชน", []string{"บัตรประชาชน"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := splitWords(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("splitWords(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestWordsBefore(t *testing.T) {
	tests := []struct {
		name string
		text string
		end  int
		n    int
		want []string
	}{
		{"reading order preserved", "one two three four x", 19, 5,
			[]string{"one", "two", "three", "four"}},
		{"limited to n", "one two three four five six x", 28, 3,
			[]string{"four", "five", "six"}},
		{"stops at the start of the text", "one two x", 8, 5, []string{"one", "two"}},
		{"lower-cases", "Email: x", 7, 5, []string{"email"}},
		{"skips separators", "email:   \t x", 11, 5, []string{"email"}},
		{"word cut by the boundary is excluded", "email address", 9, 5, []string{"email"}},
		{"boundary inside the first word yields nothing", "email", 3, 5, nil},
		{"boundary at the start yields nothing", "email x", 0, 5, nil},
		{"n of zero yields nothing", "email x", 7, 0, nil},
		{"end of text is not a cut word", "my email", 8, 5, []string{"my", "email"}},
		{"offset past the end is clamped", "my email", 999, 5, []string{"my", "email"}},
		{"multi-byte words", "e-mail do José: x", 16, 5,
			[]string{"e", "mail", "do", "josé"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := appendWordsBefore(nil, tt.text, tt.end, tt.n, testMaxWord)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("appendWordsBefore(nil, %q, %d, %d) = %q, want %q",
					tt.text, tt.end, tt.n, got, tt.want)
			}
		})
	}
}

func TestWordsAfter(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		start int
		n     int
		want  []string
	}{
		{"reading order", "x is a phone number", 1, 5,
			[]string{"is", "a", "phone", "number"}},
		{"limited to n", "x one two three", 1, 2, []string{"one", "two"}},
		{"word cut by the boundary is excluded", "address line", 3, 5, []string{"line"}},
		{"start of text is not a cut word", "phone here", 0, 1, []string{"phone"}},
		{"start at the end yields nothing", "phone", 5, 5, nil},
		{"n of zero yields nothing", "x phone", 1, 0, nil},
		{"negative start is clamped", "phone here", -1, 1, []string{"phone"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := appendWordsAfter(nil, tt.text, tt.start, tt.n, testMaxWord)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("appendWordsAfter(nil, %q, %d, %d) = %q, want %q",
					tt.text, tt.start, tt.n, got, tt.want)
			}
		})
	}
}

// TestAppendWordsIntoExistingSlice covers how Enhance actually calls the two
// scanners: one buffer, reused across results and filled twice per result. The
// reversal in appendWordsBefore must touch only what that call appended, and
// neither may drop what is already there.
func TestAppendWordsIntoExistingSlice(t *testing.T) {
	dst := []string{"keep"}

	dst = appendWordsBefore(dst, "one two 1234", 8, 5, testMaxWord)
	if want := []string{"keep", "one", "two"}; !reflect.DeepEqual(dst, want) {
		t.Fatalf("after appendWordsBefore: %q, want %q", dst, want)
	}

	dst = appendWordsAfter(dst, "one two 1234", 12, 2, testMaxWord)
	if want := []string{"keep", "one", "two"}; !reflect.DeepEqual(dst, want) {
		t.Fatalf("after appendWordsAfter past the end: %q, want %q", dst, want)
	}

	dst = appendWordsAfter(dst, "1234 three four", 4, 2, testMaxWord)
	if want := []string{"keep", "one", "two", "three", "four"}; !reflect.DeepEqual(dst, want) {
		t.Fatalf("after appendWordsAfter: %q, want %q", dst, want)
	}

	// Truncating to zero and refilling is what the enhancer does per result:
	// the buffer's contents must not leak from one result into the next.
	dst = appendWordsBefore(dst[:0], "my email 1234", 9, 5, testMaxWord)
	if want := []string{"my", "email"}; !reflect.DeepEqual(dst, want) {
		t.Fatalf("after reuse: %q, want %q", dst, want)
	}
}

// TestFoldWord covers the bound that keeps a long opaque run — a base64 blob,
// a hash, a JWT — from being copied by strings.ToLower only to be thrown away
// on length by wordMatches.
func TestFoldWord(t *testing.T) {
	tests := []struct {
		name    string
		word    string
		maxWord int
		want    string
	}{
		{"short word is lower-cased", "Email", 7, "email"},
		{"word exactly at the bound is lower-cased", "Address", 7, "address"},
		{"word past the bound is left alone", "AddressX", 7, "AddressX"},
		{"multi-byte under the bound", "JOSÉ", 7, "josé"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := foldWord(tt.word, tt.maxWord); got != tt.want {
				t.Errorf("foldWord(%q, %d) = %q, want %q",
					tt.word, tt.maxWord, got, tt.want)
			}
		})
	}
}

// TestAppendWordsSkipsCasingLongRuns is the scanner half of TestFoldWord: an
// over-long run still takes its slot in the window — dropping it would shift
// the words behind it — but arrives uncased, i.e. uncopied.
func TestAppendWordsSkipsCasingLongRuns(t *testing.T) {
	const run = "AbCdEfGhIjKlMnOp"
	text := "Label " + run + " 1234 " + run + " Tail"

	before := appendWordsBefore(nil, text, len("Label "+run+" "), 5, 8)
	if want := []string{"label", run}; !reflect.DeepEqual(before, want) {
		t.Fatalf("appendWordsBefore = %q, want %q", before, want)
	}
	after := appendWordsAfter(nil, text, len("Label "+run+" 1234"), 5, 8)
	if want := []string{run, "tail"}; !reflect.DeepEqual(after, want) {
		t.Fatalf("appendWordsAfter = %q, want %q", after, want)
	}
	// Uncased is safe only because nothing that long can match anyway.
	if wordMatches(run, strings.ToLower(run)) {
		t.Error("a word past the bound reached wordMatches and matched")
	}
}

// TestContextWordsBound pins the bound Enhance hands the scanners: the longest
// context word in play plus the two bytes wordMatches allows for the plural
// fold. Deriving it from the words themselves is what keeps a caller's long
// custom context word working instead of silently never matching.
func TestContextWordsBound(t *testing.T) {
	rec := &ctxRecognizer{
		name: "R", entity: "TEST", span: [2]int{0, 4}, score: 0.3,
		context: []string{"iban", "henkilötunnus"}, // 4 and 14 bytes
	}
	results := []Result{{RecognizerName: "R", Score: 0.3}}

	_, maxWord := contextWords([]Recognizer{rec}, results)
	if want := len("henkilötunnus") + 2; maxWord != want {
		t.Errorf("maxWord = %d, want %d", maxWord, want)
	}
}

// TestLongRunDoesNotHideContext is why the scanners bound copying rather than
// traversal: abandoning a scan on a long run would lose the label sitting
// behind it, which is exactly the shape of a logged blob with a field name in
// front of it.
func TestLongRunDoesNotHideContext(t *testing.T) {
	blob := strings.Repeat("aBcD1234", 512) // 4 KiB, no separators
	text := "card " + blob + " 1234"
	rec := &ctxRecognizer{
		name: "R", entity: "TEST",
		span:  [2]int{len(text) - 4, len(text)},
		score: 0.3, context: []string{"card"},
	}

	got := NewWordContextEnhancer().Enhance(text, rec.Analyze(text, nil), []Recognizer{rec}, nil)
	if len(got) != 1 || !nearly(got[0].Score, 0.65) {
		t.Fatalf("got %+v, want a single 0.65 result", got)
	}
}

// TestWordContextEnhancerNegativeWindow: the window fields are exported, and a
// negative one used to reach make as a negative capacity and panic Analyze.
func TestWordContextEnhancerNegativeWindow(t *testing.T) {
	const text = "my card 1234"
	rec := &ctxRecognizer{
		name: "R", entity: "TEST", span: [2]int{8, 12}, score: 0.3,
		context: []string{"card"},
	}

	tests := []struct {
		name          string
		before, after int
		want          float64
	}{
		{"negative before", -5, 0, 0.3},
		{"negative after", 5, -5, 0.65},
		{"both negative", -5, -5, 0.3},
		{"negative sum", -10, 1, 0.3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := NewWordContextEnhancer()
			w.WordsBefore, w.WordsAfter = tt.before, tt.after
			got := w.Enhance(text, rec.Analyze(text, nil), []Recognizer{rec}, nil)
			if len(got) != 1 || !nearly(got[0].Score, tt.want) {
				t.Fatalf("got %+v, want a single %v result", got, tt.want)
			}
		})
	}
}

// TestWordContextEnhancerClampsToScoreRange: Boost and MinScore are exported
// too, and Result.Score is documented to stay within [MinScore, MaxScore]
// whatever they are set to.
func TestWordContextEnhancerClampsToScoreRange(t *testing.T) {
	const text = "my card 1234"
	rec := &ctxRecognizer{
		name: "R", entity: "TEST", span: [2]int{8, 12}, score: 0.3,
		context: []string{"card"},
	}

	tests := []struct {
		name           string
		boost, minimum float64
		want           float64
	}{
		{"floor below MinScore cannot take a score negative", -1, -5, MinScore},
		{"negative boost cannot go below the floor", -5, 0.4, 0.4},
		{"negative boost with a zero floor stops at MinScore", -5, 0, MinScore},
		{"floor above MaxScore is pulled down", 0, 5, MaxScore},
		{"huge boost is capped", 99, 0.4, MaxScore},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := NewWordContextEnhancer()
			w.Boost, w.MinScore = tt.boost, tt.minimum
			got := w.Enhance(text, rec.Analyze(text, nil), []Recognizer{rec}, nil)
			if len(got) != 1 {
				t.Fatalf("got %d results, want 1", len(got))
			}
			if !nearly(got[0].Score, tt.want) {
				t.Errorf("score = %v, want %v", got[0].Score, tt.want)
			}
			if got[0].Score < MinScore || got[0].Score > MaxScore {
				t.Errorf("score %v is outside [%v, %v]", got[0].Score, MinScore, MaxScore)
			}
		})
	}
}

// TestWordMatches pins the whole-word rule that replaces Presidio's substring
// test. The "recipient"/"zip"/"script" cases are the reason for the divergence.
func TestWordMatches(t *testing.T) {
	tests := []struct {
		word, context string
		want          bool
	}{
		{"email", "email", true},
		{"emails", "email", true},
		{"card", "card", true},
		{"cards", "card", true},
		{"address", "address", true},
		{"addresses", "address", true},
		{"ips", "ip", true},
		{"boxes", "box", true},

		{"email", "emails", false},
		{"recipient", "ip", false},
		{"zip", "ip", false},
		{"script", "ip", false},
		{"mailbox", "mail", false},
		{"e", "email", false},
		{"", "email", false},
		{"email", "", false},

		// +1/+2 in length is necessary but not sufficient: the suffix has to
		// be the plural and the prefix has to be the context word.
		{"emailx", "email", false},
		{"emailxy", "email", false},
		{"xmails", "email", false},
	}
	for _, tt := range tests {
		if got := wordMatches(tt.word, tt.context); got != tt.want {
			t.Errorf("wordMatches(%q, %q) = %v, want %v", tt.word, tt.context, got, tt.want)
		}
	}
}

func TestContainsPhrase(t *testing.T) {
	window := []string{"his", "social", "security", "number", "is"}
	tests := []struct {
		name   string
		phrase []string
		want   bool
	}{
		{"single word present", []string{"number"}, true},
		{"single word absent", []string{"ssn"}, false},
		{"contiguous phrase", []string{"social", "security"}, true},
		{"phrase at the start", []string{"his", "social"}, true},
		{"phrase at the end", []string{"number", "is"}, true},
		{"non-contiguous words are not a phrase", []string{"social", "number"}, false},
		{"reversed phrase", []string{"security", "social"}, false},
		{"phrase longer than the window", []string{"a", "b", "c", "d", "e", "f"}, false},
		{"empty phrase never matches", nil, false},
		{"whole window", window, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containsPhrase(window, tt.phrase); got != tt.want {
				t.Errorf("containsPhrase(%q, %q) = %v, want %v",
					window, tt.phrase, got, tt.want)
			}
		})
	}
}

func TestWordContextEnhancerScores(t *testing.T) {
	// The span is the digits of "my card 1234", so "my" and "card" are the
	// context and the enhancer must not read the match itself.
	const text = "my card 1234"
	span := [2]int{8, 12}

	tests := []struct {
		name    string
		score   float64
		context []string
		want    float64
	}{
		{"boost applied", 0.3, []string{"card"}, 0.65},
		{"floor lifts a very weak match", 0.01, []string{"card"}, DefaultContextMinScore},
		{"capped at MaxScore", 0.8, []string{"card"}, MaxScore},
		{"already at MaxScore is untouched", MaxScore, []string{"card"}, MaxScore},
		{"one of several context words is enough", 0.3,
			[]string{"iban", "card", "bank"}, 0.65},
		{"case-insensitive", 0.3, []string{"CARD"}, 0.65},
		{"context word is itself plural", 0.3, []string{"cards"}, 0.3},
		{"no matching context word", 0.3, []string{"iban", "bank"}, 0.3},
		{"empty context list", 0.3, nil, 0.3},
		{"context word is the match itself", 0.3, []string{"1234"}, 0.3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &ctxRecognizer{
				name: "R", entity: "TEST", span: span, score: tt.score, context: tt.context,
			}
			got := NewWordContextEnhancer().Enhance(
				text, rec.Analyze(text, nil), []Recognizer{rec}, nil)
			if len(got) != 1 {
				t.Fatalf("got %d results, want 1", len(got))
			}
			if !nearly(got[0].Score, tt.want) {
				t.Errorf("score = %v, want %v", got[0].Score, tt.want)
			}
		})
	}
}

// TestWordContextEnhancerIgnoresPlainRecognizer covers a recognizer that never
// declared context words: it is not a ContextualRecognizer, so it is skipped.
func TestWordContextEnhancerIgnoresPlainRecognizer(t *testing.T) {
	const text = "my card 1234"
	rec := &plainRecognizer{name: "R", entity: "TEST", span: [2]int{8, 12}, score: 0.3}

	got := NewWordContextEnhancer().Enhance(text, rec.Analyze(text, nil), []Recognizer{rec}, nil)
	if len(got) != 1 || !nearly(got[0].Score, 0.3) {
		t.Fatalf("got %+v, want a single untouched 0.3 result", got)
	}
}

// TestWordContextEnhancerScopesContextToItsRecognizer pins that a result is
// only boosted by the words its own recognizer declared, never by another's.
func TestWordContextEnhancerScopesContextToItsRecognizer(t *testing.T) {
	const text = "my card 1234"
	mine := &ctxRecognizer{
		name: "mine", entity: "MINE", span: [2]int{8, 12}, score: 0.3,
		context: []string{"iban"},
	}
	theirs := &ctxRecognizer{
		name: "theirs", entity: "THEIRS", span: [2]int{8, 12}, score: 0.3,
		context: []string{"card"},
	}

	results := append(mine.Analyze(text, nil), theirs.Analyze(text, nil)...)
	got := NewWordContextEnhancer().Enhance(
		text, results, []Recognizer{mine, theirs}, nil)
	if len(got) != 2 {
		t.Fatalf("got %d results, want 2", len(got))
	}
	if !nearly(got[0].Score, 0.3) {
		t.Errorf("mine: score = %v, want 0.3 (its own context word is absent)", got[0].Score)
	}
	if !nearly(got[1].Score, 0.65) {
		t.Errorf("theirs: score = %v, want 0.65", got[1].Score)
	}
}

func TestWordContextEnhancerWindowSize(t *testing.T) {
	// "card" sits progressively further from the match; the default window is
	// five words, so the six-words-away case must not boost.
	tests := []struct {
		name string
		text string
		want float64
	}{
		{"adjacent", "card 1234", 0.65},
		{"five words away", "card a b c d 1234", 0.65},
		{"six words away", "card a b c d e 1234", 0.3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start := len(tt.text) - 4 // the trailing "1234"
			rec := &ctxRecognizer{
				name: "R", entity: "TEST",
				span: [2]int{start, start + 4}, score: 0.3, context: []string{"card"},
			}
			got := NewWordContextEnhancer().Enhance(
				tt.text, rec.Analyze(tt.text, nil), []Recognizer{rec}, nil)
			if !nearly(got[0].Score, tt.want) {
				t.Errorf("score = %v, want %v (text %q)", got[0].Score, tt.want, tt.text)
			}
		})
	}
}

func TestWordContextEnhancerWordsAfter(t *testing.T) {
	// Presidio reads nothing after the match — a label almost always precedes
	// its value — but the window is configurable.
	const text = "1234 card"
	rec := &ctxRecognizer{
		name: "R", entity: "TEST", span: [2]int{0, 4}, score: 0.3, context: []string{"card"},
	}

	got := NewWordContextEnhancer().Enhance(text, rec.Analyze(text, nil), []Recognizer{rec}, nil)
	if !nearly(got[0].Score, 0.3) {
		t.Errorf("score = %v, want 0.3: nothing after the match is read by default",
			got[0].Score)
	}

	enh := NewWordContextEnhancer()
	enh.WordsAfter = 2
	got = enh.Enhance(text, rec.Analyze(text, nil), []Recognizer{rec}, nil)
	if !nearly(got[0].Score, 0.65) {
		t.Errorf("score = %v, want 0.65 with WordsAfter=2", got[0].Score)
	}
}

func TestWordContextEnhancerPhrase(t *testing.T) {
	tests := []struct {
		name string
		text string
		ctx  []string
		want float64
	}{
		{"phrase present", "his social security is 078-05-1120",
			[]string{"social security"}, 0.65},
		{"phrase split by another word", "social number security 078-05-1120",
			[]string{"social security"}, 0.3},
		{"phrase falls outside the window", "social security a b c d 078-05-1120",
			[]string{"social security"}, 0.3},
		{"plural inside a phrase is not folded", "his social securities 078-05-1120",
			[]string{"social security"}, 0.3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start := len(tt.text) - len("078-05-1120")
			rec := &ctxRecognizer{
				name: "R", entity: "TEST",
				span: [2]int{start, len(tt.text)}, score: 0.3, context: tt.ctx,
			}
			got := NewWordContextEnhancer().Enhance(
				tt.text, rec.Analyze(tt.text, nil), []Recognizer{rec}, nil)
			if !nearly(got[0].Score, tt.want) {
				t.Errorf("score = %v, want %v (text %q)", got[0].Score, tt.want, tt.text)
			}
		})
	}
}

func TestEngineAppliesContextEnhancer(t *testing.T) {
	const text = "my card 1234"
	newEngine := func() *Engine {
		reg := NewRegistry("en")
		reg.Add("en", &ctxRecognizer{
			name: "R", entity: "TEST", span: [2]int{8, 12}, score: 0.3,
			context: []string{"card"},
		})
		return NewEngine(reg, []string{"en"})
	}

	t.Run("on by default", func(t *testing.T) {
		got := newEngine().Analyze(text, Options{})
		if len(got) != 1 || !nearly(got[0].Score, 0.65) {
			t.Fatalf("got %+v, want a single 0.65 result", got)
		}
	})

	t.Run("nil enhancer disables boosting", func(t *testing.T) {
		eng := newEngine()
		eng.SetContextEnhancer(nil)
		got := eng.Analyze(text, Options{})
		if len(got) != 1 || !nearly(got[0].Score, 0.3) {
			t.Fatalf("got %+v, want a single 0.3 result", got)
		}
	})

	// The reason the enhancer runs before the threshold: a result boosted over
	// it has to survive to be returned.
	t.Run("boost is applied before the threshold", func(t *testing.T) {
		threshold := 0.6
		if got := newEngine().Analyze(text, Options{Threshold: &threshold}); len(got) != 1 {
			t.Fatalf("got %d results, want the boosted result kept", len(got))
		}

		eng := newEngine()
		eng.SetContextEnhancer(nil)
		if got := eng.Analyze(text, Options{Threshold: &threshold}); len(got) != 0 {
			t.Fatalf("got %+v, want the unboosted result dropped", got)
		}
	})

	t.Run("batch analysis boosts per text", func(t *testing.T) {
		got := newEngine().AnalyzeBatch([]string{text, "no hint 1234"}, Options{})
		if len(got) != 2 {
			t.Fatalf("got %d result slices, want 2", len(got))
		}
		if len(got[0]) != 1 || !nearly(got[0][0].Score, 0.65) {
			t.Errorf("text with context: got %+v, want 0.65", got[0])
		}
		if len(got[1]) != 1 || !nearly(got[1][0].Score, 0.3) {
			t.Errorf("text without context: got %+v, want 0.3", got[1])
		}
	})
}

// TestEngineContextBoostDecidesOverlap is why the enhancer runs before
// RemoveDuplicates: of two overlapping same-entity spans, the one the
// surrounding text supports wins, even though it scores lower on the pattern
// alone.
func TestEngineContextBoostDecidesOverlap(t *testing.T) {
	const text = "card 1234"
	reg := NewRegistry("en")
	reg.Add("en", &ctxRecognizer{
		name: "weak-but-supported", entity: "TEST",
		span: [2]int{5, 9}, score: 0.4, context: []string{"card"},
	})
	reg.Add("en", &ctxRecognizer{
		name: "strong-but-unsupported", entity: "TEST",
		span: [2]int{5, 9}, score: 0.6, context: []string{"iban"},
	})

	got := NewEngine(reg, []string{"en"}).Analyze(text, Options{})
	if len(got) != 1 {
		t.Fatalf("got %d results, want 1 after de-duplication", len(got))
	}
	if got[0].RecognizerName != "weak-but-supported" || !nearly(got[0].Score, 0.75) {
		t.Errorf("got %s at %v, want weak-but-supported at 0.75",
			got[0].RecognizerName, got[0].Score)
	}
}

// nearly compares scores without depending on exact float equality.
func nearly(a, b float64) bool {
	const eps = 1e-9
	d := a - b
	return d < eps && d > -eps
}
