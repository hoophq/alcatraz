package ner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gomlx/go-huggingface/tokenizers/api"
	"github.com/hoophq/alcatraz/analyzer"
	"github.com/hoophq/alcatraz/entities"
)

// fakeTokenizer tokenizes on whitespace (one token per word) and adds two
// special tokens, mimicking a [CLS] ... [SEP] encoding. It implements just
// enough of api.Tokenizer for the windowing code.
type fakeTokenizer struct{}

func (fakeTokenizer) Encode(text string) []int {
	return make([]int, len(strings.Fields(text))+2)
}

func (fakeTokenizer) EncodeWithAnnotations(text string) api.AnnotatedEncoding {
	var spans []api.TokenSpan
	i := 0
	for i < len(text) {
		if text[i] == ' ' || text[i] == '\n' || text[i] == '\t' {
			i++
			continue
		}
		start := i
		for i < len(text) && text[i] != ' ' && text[i] != '\n' && text[i] != '\t' {
			i++
		}
		spans = append(spans, api.TokenSpan{Start: start, End: i})
	}
	enc := api.AnnotatedEncoding{
		IDs:               make([]int, len(spans)+2),
		Spans:             make([]api.TokenSpan, 0, len(spans)+2),
		SpecialTokensMask: make([]int, 0, len(spans)+2),
	}
	enc.Spans = append(enc.Spans, api.TokenSpan{})
	enc.SpecialTokensMask = append(enc.SpecialTokensMask, 1)
	for _, s := range spans {
		enc.Spans = append(enc.Spans, s)
		enc.SpecialTokensMask = append(enc.SpecialTokensMask, 0)
	}
	enc.Spans = append(enc.Spans, api.TokenSpan{})
	enc.SpecialTokensMask = append(enc.SpecialTokensMask, 1)
	return enc
}

func (fakeTokenizer) With(api.EncodeOptions) error                 { return nil }
func (fakeTokenizer) Decode([]int) string                          { return "" }
func (fakeTokenizer) SpecialTokenID(api.SpecialToken) (int, error) { return 0, api.ErrNotImplemented }
func (fakeTokenizer) Normalize(s string) string                    { return s }
func (fakeTokenizer) VocabSize() int                               { return 0 }
func (fakeTokenizer) Config() *api.Config                          { return nil }

func words(n int) string {
	w := make([]string, n)
	for i := range w {
		w[i] = "word"
	}
	return strings.Join(w, " ")
}

func TestTokenWindows(t *testing.T) {
	tk := fakeTokenizer{}

	t.Run("short text is a single full window", func(t *testing.T) {
		text := words(10)
		wins := tokenWindows(tk, text, 512)
		if len(wins) != 1 || wins[0].start != 0 || wins[0].end != len(text) {
			t.Fatalf("wins = %+v, want single window over full text", wins)
		}
	})

	t.Run("long text splits into overlapping windows covering everything", func(t *testing.T) {
		// 1000 words, budget 512 tokens (510 content after 2 specials).
		text := words(1000)
		wins := tokenWindows(tk, text, 512)
		if len(wins) < 2 {
			t.Fatalf("want multiple windows, got %+v", wins)
		}
		if wins[0].start != 0 {
			t.Errorf("first window starts at %d, want 0", wins[0].start)
		}
		if wins[len(wins)-1].end != len(text) {
			t.Errorf("last window ends at %d, want %d", wins[len(wins)-1].end, len(text))
		}
		for i, w := range wins {
			total := len(tk.Encode(text[w.start:w.end]))
			if total > 512 {
				t.Errorf("window %d re-encodes to %d tokens > 512", i, total)
			}
			if i > 0 && w.start >= wins[i-1].end {
				t.Errorf("gap between window %d (end %d) and %d (start %d)",
					i-1, wins[i-1].end, i, w.start)
			}
		}
	})

	t.Run("windows never cut words", func(t *testing.T) {
		text := words(600)
		for _, w := range tokenWindows(tk, text, 512) {
			body := text[w.start:w.end]
			if strings.HasPrefix(body, " ") || strings.HasSuffix(body, " ") ||
				len(body)%5 != 4 { // "word" repeated joins to length 5k+4
				t.Fatalf("window %+v does not align to word boundaries: %q…", w, body[:10])
			}
		}
	})
}

// countingTokenizer wraps a tokenizer and counts the encodings it is asked
// for, so tests can prove when the sizing pass was skipped.
type countingTokenizer struct {
	api.Tokenizer
	calls int
}

func (c *countingTokenizer) Encode(text string) []int {
	c.calls++
	return c.Tokenizer.Encode(text)
}

func (c *countingTokenizer) EncodeWithAnnotations(text string) api.AnnotatedEncoding {
	c.calls++
	return c.Tokenizer.EncodeWithAnnotations(text)
}

// testWindowTokenizer loads the WordPiece test tokenizer the way New does on
// rust-tokenizer builds.
func testWindowTokenizer(tb testing.TB) api.Tokenizer {
	tb.Helper()
	dir := tb.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tokenizer.json"), testTokenizerJSON, 0o644); err != nil {
		tb.Fatal(err)
	}
	tk := loadWindowTokenizer(dir)
	if tk == nil {
		tb.Fatal("loadWindowTokenizer returned nil for a valid tokenizer.json")
	}
	return tk
}

// TestWindowsShortCircuit pins the byte-count short-circuit in windows: a
// text whose bytes plus special tokens fit the budget is one whole window
// without any tokenizer call, and for every input — short or long, ASCII or
// not, on either side of the boundary — the windows are exactly what the
// full sizing pass (tokenWindows) produces.
func TestWindowsShortCircuit(t *testing.T) {
	const budget = 64
	for _, tk := range []api.Tokenizer{fakeTokenizer{}, testWindowTokenizer(t)} {
		specials := windowSpecials(tk)
		if specials != 2 { // [CLS] ... [SEP]
			t.Fatalf("windowSpecials = %d, want 2", specials)
		}
		counting := &countingTokenizer{Tokenizer: tk}
		e := &Engine{winTok: counting, tokenBudget: budget, winSpecials: specials}

		atBoundary := strings.Repeat("hello ", 10) + "wo" // 62 bytes: 62 + 2 == budget
		if len(atBoundary)+specials != budget {
			t.Fatalf("boundary fixture is %d bytes, want %d", len(atBoundary), budget-specials)
		}
		for _, tc := range []struct {
			name    string
			text    string
			skipped bool
		}{
			{"empty", "", true},
			{"one word", "hello", true},
			{"short line", "hello world hello world", true},
			{"non-ASCII short", "héllo wörld — 日本 hello", true},
			{"exactly at budget", atBoundary, true},
			{"one byte over", atBoundary + "r", false},
			{"over in bytes, within tokens", strings.Repeat("hello ", 20), false},
			{"non-ASCII over in bytes", strings.Repeat("héllo ", 12), false},
			{"long multi-window", strings.TrimSpace(strings.Repeat("hello world ", 200)), false},
			{"long non-ASCII multi-window", strings.TrimSpace(strings.Repeat("héllo wörld ", 200)), false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				want := tokenWindows(tk, tc.text, budget)
				counting.calls = 0
				got := e.windows(tc.text)
				if len(got) != len(want) {
					t.Fatalf("windows = %+v, want %+v", got, want)
				}
				for i := range want {
					if got[i] != want[i] {
						t.Fatalf("windows = %+v, want %+v", got, want)
					}
				}
				if tc.skipped && counting.calls != 0 {
					t.Errorf("tokenizer called %d times, want the sizing pass skipped", counting.calls)
				}
				if !tc.skipped && counting.calls == 0 {
					t.Errorf("tokenizer never called, want the sizing pass")
				}
			})
		}
	}
}

// testTokenizerJSON is a minimal BERT-style WordPiece tokenizer: one token
// per known word, [CLS]/[SEP] added by the post-processor.
var testTokenizerJSON = []byte(`{
  "version": "1.0",
  "added_tokens": [
    {"id": 0, "content": "[PAD]", "special": true},
    {"id": 1, "content": "[UNK]", "special": true},
    {"id": 2, "content": "[CLS]", "special": true},
    {"id": 3, "content": "[SEP]", "special": true}
  ],
  "normalizer": {"type": "BertNormalizer", "lowercase": true},
  "pre_tokenizer": {"type": "BertPreTokenizer"},
  "post_processor": {"type": "BertProcessing", "sep": ["[SEP]", 3], "cls": ["[CLS]", 2]},
  "decoder": {"type": "WordPiece", "prefix": "##"},
  "model": {
    "type": "WordPiece",
    "unk_token": "[UNK]",
    "continuing_subword_prefix": "##",
    "max_input_chars_per_word": 100,
    "vocab": {"[PAD]": 0, "[UNK]": 1, "[CLS]": 2, "[SEP]": 3, "hello": 4, "world": 5}
  }
}`)

// TestLoadWindowTokenizer verifies the standalone windowing tokenizer used
// on rust-tokenizer builds (ORT/XLA): loaded from tokenizer.json with
// specials, spans and the special-tokens mask enabled, it must drive
// tokenWindows to budget-respecting windows exactly like the pipeline's own
// Go tokenizer does.
func TestLoadWindowTokenizer(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tokenizer.json"), testTokenizerJSON, 0o644); err != nil {
		t.Fatal(err)
	}
	tk := loadWindowTokenizer(dir)
	if tk == nil {
		t.Fatal("loadWindowTokenizer returned nil for a valid tokenizer.json")
	}

	// The encode options must match what tokenWindows relies on: specials
	// included in the count, spans and mask exposed.
	enc := tk.EncodeWithAnnotations("hello world")
	if len(enc.IDs) != 4 { // [CLS] hello world [SEP]
		t.Fatalf("IDs = %v, want 4 tokens including specials", enc.IDs)
	}
	if len(enc.Spans) != 4 || len(enc.SpecialTokensMask) != 4 {
		t.Fatalf("spans/mask missing: %d spans, %d mask", len(enc.Spans), len(enc.SpecialTokensMask))
	}

	// End to end through the real windowing code: every window re-encodes
	// within budget and the windows cover the whole text without gaps.
	text := strings.TrimSpace(strings.Repeat("hello world ", 200))
	wins := tokenWindows(tk, text, 64)
	if len(wins) < 2 {
		t.Fatalf("want multiple windows, got %+v", wins)
	}
	if wins[0].start != 0 || wins[len(wins)-1].end != len(text) {
		t.Errorf("coverage broken: %+v", wins)
	}
	for i, w := range wins {
		if n := len(tk.Encode(text[w.start:w.end])); n > 64 {
			t.Errorf("window %d encodes to %d tokens > budget 64", i, n)
		}
		if i > 0 && w.start >= wins[i-1].end {
			t.Errorf("gap between windows %d and %d", i-1, i)
		}
	}
}

func TestLoadWindowTokenizerFailure(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		if tk := loadWindowTokenizer(t.TempDir()); tk != nil {
			t.Fatal("want nil for a directory without tokenizer.json")
		}
	})

	t.Run("corrupt file", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "tokenizer.json"), []byte("{"), 0o644); err != nil {
			t.Fatal(err)
		}
		if tk := loadWindowTokenizer(dir); tk != nil {
			t.Fatal("want nil for corrupt tokenizer.json")
		}
	})
}

// TestWindowsByteFallback pins the guarantee of the last-resort path (no
// windowing tokenizer at all): windows are capped at tokenBudget minus the
// specials slack in *bytes*, and since every non-special token covers at
// least one byte, no window can encode past the token budget.
func TestWindowsByteFallback(t *testing.T) {
	e := &Engine{tokenBudget: 256} // winTok nil
	folded := strings.Repeat("a", 2000)
	wins := e.windows(folded)
	if len(wins) < 2 {
		t.Fatalf("want multiple windows, got %+v", wins)
	}
	if wins[0].start != 0 || wins[len(wins)-1].end != len(folded) {
		t.Errorf("coverage broken: %+v", wins)
	}
	for i, w := range wins {
		if w.end-w.start > 256-fallbackSpecialsSlack {
			t.Errorf("window %d is %d bytes, above the %d-byte cap", i, w.end-w.start, 256-fallbackSpecialsSlack)
		}
		if i > 0 && w.start >= wins[i-1].end {
			t.Errorf("gap between windows %d and %d", i-1, i)
		}
	}
}

func TestByteWindows(t *testing.T) {
	t.Run("fits in one window", func(t *testing.T) {
		wins := byteWindows(100, 200, 20)
		if len(wins) != 1 || wins[0] != (textWindow{0, 100}) {
			t.Fatalf("wins = %+v", wins)
		}
	})

	t.Run("splits with overlap and full coverage", func(t *testing.T) {
		wins := byteWindows(1000, 300, 50)
		if wins[0].start != 0 || wins[len(wins)-1].end != 1000 {
			t.Fatalf("coverage broken: %+v", wins)
		}
		for i := 1; i < len(wins); i++ {
			if wins[i].start != wins[i-1].start+250 {
				t.Fatalf("step wrong at %d: %+v", i, wins)
			}
			if wins[i].start >= wins[i-1].end {
				t.Fatalf("gap at %d: %+v", i, wins)
			}
		}
	})

	t.Run("degenerate sizes never loop forever", func(t *testing.T) {
		wins := byteWindows(10, 1, 5)
		if wins[len(wins)-1].end != 10 {
			t.Fatalf("coverage broken: %+v", wins)
		}
	})
}

func TestMergeSpans(t *testing.T) {
	t.Run("exact duplicate keeps max score", func(t *testing.T) {
		got := mergeSpans([]analyzer.NerSpan{
			{EntityType: entities.Person, Start: 10, End: 20, Score: 0.7},
			{EntityType: entities.Person, Start: 10, End: 20, Score: 0.9},
		})
		if len(got) != 1 || got[0].Score != 0.9 {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("fragment contained in wider same-type span is dropped", func(t *testing.T) {
		got := mergeSpans([]analyzer.NerSpan{
			{EntityType: entities.Person, Start: 15, End: 20, Score: 0.9},
			{EntityType: entities.Person, Start: 10, End: 20, Score: 0.8},
		})
		if len(got) != 1 || got[0].Start != 10 || got[0].End != 20 {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("contained span of a different type is kept", func(t *testing.T) {
		got := mergeSpans([]analyzer.NerSpan{
			{EntityType: entities.Person, Start: 10, End: 30, Score: 0.9},
			{EntityType: entities.Location, Start: 15, End: 20, Score: 0.8},
		})
		if len(got) != 2 {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("disjoint spans pass through sorted", func(t *testing.T) {
		got := mergeSpans([]analyzer.NerSpan{
			{EntityType: entities.Person, Start: 50, End: 60, Score: 0.9},
			{EntityType: entities.Person, Start: 10, End: 20, Score: 0.8},
		})
		if len(got) != 2 || got[0].Start != 10 || got[1].Start != 50 {
			t.Fatalf("got %+v", got)
		}
	})
}
