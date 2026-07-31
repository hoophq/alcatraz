package ner

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/hoophq/alcatraz/analyzer"
	"github.com/hoophq/alcatraz/entities"
)

// render formats spans as "TYPE:text" so a test case can state what it wants
// to see rather than a table of byte offsets.
func render(text string, spans []analyzer.NerSpan) []string {
	out := make([]string, len(spans))
	for i, s := range spans {
		out[i] = fmt.Sprintf("%s:%s", s.EntityType, text[s.Start:s.End])
	}
	return out
}

func person(start, end int, score float64) analyzer.NerSpan {
	return analyzer.NerSpan{EntityType: entities.Person, Start: start, End: end, Score: score}
}

func TestSnapToWords(t *testing.T) {
	for _, tc := range []struct {
		name  string
		text  string
		spans []analyzer.NerSpan
		want  []string
	}{{
		name:  "left edge inside a word",
		text:  "Bandar",
		spans: []analyzer.NerSpan{person(1, 6, 0.9)},
		want:  []string{"PERSON:Bandar"},
	}, {
		name:  "right edge inside a word",
		text:  "Begawan",
		spans: []analyzer.NerSpan{person(0, 6, 0.9)},
		want:  []string{"PERSON:Begawan"},
	}, {
		name:  "both edges inside a word",
		text:  "Bandar Seri Begawan",
		spans: []analyzer.NerSpan{person(1, 18, 0.9)},
		want:  []string{"PERSON:Bandar Seri Begawan"},
	}, {
		name:  "adjacent subwords are unioned",
		text:  "Luan",
		spans: []analyzer.NerSpan{person(0, 2, 0.81), person(2, 4, 0.58)},
		want:  []string{"PERSON:Luan"},
	}, {
		name:  "three subwords are unioned",
		text:  "Nguyen Thi Hoa",
		spans: []analyzer.NerSpan{person(0, 2, 0.9), person(2, 4, 0.9), person(4, 14, 0.9)},
		want:  []string{"PERSON:Nguyen Thi Hoa"},
	}, {
		name:  "overlapping spans are unioned",
		text:  "Ouagadougou",
		spans: []analyzer.NerSpan{person(0, 7, 0.9), person(5, 11, 0.9)},
		want:  []string{"PERSON:Ouagadougou"},
	}, {
		name: "different types are kept apart",
		text: "Luan",
		spans: []analyzer.NerSpan{
			person(0, 2, 0.9),
			{EntityType: entities.Location, Start: 2, End: 4, Score: 0.7},
		},
		want: []string{"PERSON:Luan", "LOCATION:Luan"},
	}, {
		name:  "separate words are not unioned",
		text:  "Alice Bob",
		spans: []analyzer.NerSpan{person(0, 5, 0.9), person(6, 9, 0.9)},
		want:  []string{"PERSON:Alice", "PERSON:Bob"},
	}, {
		// An underscore is a word boundary to BERT's pre-tokenizer, so it is
		// one here: a span over the value must not swallow the column name.
		name:  "underscore is not crossed",
		text:  "user_name",
		spans: []analyzer.NerSpan{person(5, 7, 0.9)},
		want:  []string{"PERSON:name"},
	}, {
		name:  "hyphen is not crossed",
		text:  "Jean-Pierre",
		spans: []analyzer.NerSpan{person(5, 8, 0.9)},
		want:  []string{"PERSON:Pierre"},
	}, {
		name:  "apostrophe is not crossed",
		text:  "O'Brien",
		spans: []analyzer.NerSpan{person(2, 5, 0.9)},
		want:  []string{"PERSON:Brien"},
	}, {
		name:  "email local part is not swallowed",
		text:  "luan@example.com",
		spans: []analyzer.NerSpan{person(5, 9, 0.9)},
		want:  []string{"PERSON:example"},
	}, {
		name:  "tab is not crossed",
		text:  "417768\tLuan",
		spans: []analyzer.NerSpan{person(7, 9, 0.9)},
		want:  []string{"PERSON:Luan"},
	}, {
		name:  "spans already on boundaries are untouched",
		text:  "John Smith lives here",
		spans: []analyzer.NerSpan{person(0, 10, 0.9)},
		want:  []string{"PERSON:John Smith"},
	}, {
		name:  "trailing punctuation is not absorbed",
		text:  "Hello Smith, goodbye",
		spans: []analyzer.NerSpan{person(6, 9, 0.9)},
		want:  []string{"PERSON:Smith"},
	}, {
		// "Zieliński": the n-acute is two bytes, so a naive byte walk would
		// land mid-rune. [0:5] is "Zieli", [5:10] is "ński".
		name:  "multi-byte rune before the edge",
		text:  "Zieliński",
		spans: []analyzer.NerSpan{person(0, 5, 0.9)},
		want:  []string{"PERSON:Zieliński"},
	}, {
		name:  "multi-byte rune after the edge",
		text:  "Zieliński",
		spans: []analyzer.NerSpan{person(5, 10, 0.9)},
		want:  []string{"PERSON:Zieliński"},
	}, {
		name:  "digits are word runes",
		text:  "417768",
		spans: []analyzer.NerSpan{person(0, 3, 0.9)},
		want:  []string{"PERSON:417768"},
	}, {
		// Decomposed "Zieliński": the "ń" is "n" + U+0301, so [0:6] ends
		// between the letter and its own accent. A combining mark is part of
		// its word, so the span grows past it — otherwise masking yields
		// "<PERSON>\u0301ski", the accent floating outside the mask and "ski"
		// left in the clear. This is not a hypothetical input: foldASCII
		// renders one byte per rune, so the model sees "Zielinxski" and can
		// tag "Zielin" on its own.
		name:  "combining mark is part of the word",
		text:  "Zielin\u0301ski",
		spans: []analyzer.NerSpan{person(0, 6, 0.9)},
		want:  []string{"PERSON:Zielin\u0301ski"},
	}, {
		// The same shape at the other edge: [8:11] is "ski", and growing it
		// leftwards must cross the mark rather than strand "Zielin".
		name:  "combining mark is crossed leftwards",
		text:  "Zielin\u0301ski",
		spans: []analyzer.NerSpan{person(8, 11, 0.9)},
		want:  []string{"PERSON:Zielin\u0301ski"},
	}, {
		name:  "no spans",
		text:  "nothing here",
		spans: nil,
		want:  nil,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			got := render(tc.text, snapToWords(tc.text, tc.spans))
			if !equalStrings(got, tc.want) {
				t.Errorf("snapToWords = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestSnapToWordsUsesTheHighestScore records that a union keeps the score of
// its most confident part. Taking the minimum would let one weak subword pull
// a whole confident name below a caller's threshold — the failure this fix
// exists to prevent, reintroduced from the other side.
func TestSnapToWordsUsesTheHighestScore(t *testing.T) {
	text := "Luan"
	got := snapToWords(text, []analyzer.NerSpan{person(0, 2, 0.8138), person(2, 4, 0.5778)})
	if len(got) != 1 {
		t.Fatalf("got %d spans, want 1: %q", len(got), render(text, got))
	}
	if got[0].Score != 0.8138 {
		t.Errorf("score = %v, want 0.8138", got[0].Score)
	}
}

// TestSnapToWordsBoundsGrowth pins the maxWordChars cap. WordPiece emits a
// word longer than max_input_chars_per_word as a single [UNK], so no subword
// split can exist inside one — extending across it would only ever turn a
// stray span inside an opaque blob into a mask over the whole blob.
func TestSnapToWordsBoundsGrowth(t *testing.T) {
	t.Run("a word at the cap is snapped", func(t *testing.T) {
		text := strings.Repeat("a", maxWordChars)
		got := snapToWords(text, []analyzer.NerSpan{person(10, 20, 0.9)})
		if len(got) != 1 || got[0].Start != 0 || got[0].End != len(text) {
			t.Errorf("got %v, want one span covering [0:%d]", got, len(text))
		}
	})

	t.Run("a longer word is left alone", func(t *testing.T) {
		text := strings.Repeat("a", maxWordChars+1)
		got := snapToWords(text, []analyzer.NerSpan{person(10, 20, 0.9)})
		if len(got) != 1 || got[0].Start != 10 || got[0].End != 20 {
			t.Errorf("got %v, want the span unchanged at [10:20]", got)
		}
	})

	t.Run("the cap counts runes, not bytes", func(t *testing.T) {
		// 60 two-byte runes: 120 bytes, 60 characters, so under the cap.
		text := strings.Repeat("é", 60)
		got := snapToWords(text, []analyzer.NerSpan{person(20, 40, 0.9)})
		if len(got) != 1 || got[0].Start != 0 || got[0].End != len(text) {
			t.Errorf("got %v, want one span covering [0:%d]", got, len(text))
		}
	})
}

// TestSnapToWordsLeavesMalformedSpansAlone: an out-of-range span means a bug
// upstream of here. Snapping would index out of bounds; silently dropping it
// would hide the bug and unmask whatever it covered.
func TestSnapToWordsLeavesMalformedSpansAlone(t *testing.T) {
	text := "Luan"
	for _, s := range []analyzer.NerSpan{
		person(-1, 2, 0.9),
		person(0, 99, 0.9),
		person(3, 3, 0.9),
		person(3, 1, 0.9),
	} {
		got := snapToWords(text, []analyzer.NerSpan{s})
		if len(got) != 1 || got[0] != s {
			t.Errorf("snapToWords(%v) = %v, want it unchanged", s, got)
		}
	}
}

func TestSnapToWordsIsIdempotent(t *testing.T) {
	text := "Bandar Seri Begawan lives at user_name\tLuan"
	once := snapToWords(text, []analyzer.NerSpan{
		person(1, 18, 0.9), person(33, 35, 0.7), person(39, 41, 0.6), person(41, 43, 0.5),
	})
	twice := snapToWords(text, append([]analyzer.NerSpan(nil), once...))
	if !equalStrings(render(text, once), render(text, twice)) {
		t.Errorf("second pass changed the result: %q then %q",
			render(text, once), render(text, twice))
	}
}

// TestObservedSubwordSplitsBecomeWholeWords replays the exact spans the live
// model returned during the ATR-206 investigation (distilbert-NER on the
// pure-Go backend, SIMPLE aggregation). It is the non-vacuous core of this
// file: the unit cases above prove snapToWords does what it says, and these
// prove that what it says is what the model's output actually needs.
//
// Measured with ALCATRAZ_NER_LIVE against KnightsAnalytics/distilbert-NER.
func TestObservedSubwordSplitsBecomeWholeWords(t *testing.T) {
	for _, tc := range []struct {
		text  string
		spans []analyzer.NerSpan // as reported by the model
		want  []string
	}{{
		// Two B-PER tags on one word: hugot's SIMPLE aggregation starts a new
		// group at every B-, so "Lu" + "##an" comes back as two entities.
		text:  "Luan",
		spans: []analyzer.NerSpan{person(0, 2, 0.9949), person(2, 4, 0.9912)},
		want:  []string{"PERSON:Luan"},
	}, {
		// The same name in tabular context, where the scores diverge enough
		// that a 0.6 threshold keeps "Lu" and drops "an" — half a name masked.
		text:  "user_name\nLuan\n",
		spans: []analyzer.NerSpan{person(10, 12, 0.8138), person(12, 14, 0.5778)},
		want:  []string{"PERSON:Luan"},
	}, {
		text: "Nguyen Thi Hoa lives in Hanoi.",
		spans: []analyzer.NerSpan{
			person(0, 2, 0.99), person(2, 4, 0.99), person(4, 14, 0.99),
			{EntityType: entities.Location, Start: 24, End: 27, Score: 0.99},
			{EntityType: entities.Location, Start: 27, End: 29, Score: 0.99},
		},
		want: []string{"PERSON:Nguyen Thi Hoa", "LOCATION:Hanoi"},
	}, {
		// The case merging alone cannot fix: the model tagged neither the
		// leading "B" nor the trailing "n", so masking the reported span
		// leaves "B<PERSON>n" and the reader recovers the name.
		text:  "Bandar Seri Begawan",
		spans: []analyzer.NerSpan{person(1, 18, 0.98)},
		want:  []string{"PERSON:Bandar Seri Begawan"},
	}} {
		t.Run(strings.TrimSpace(tc.text), func(t *testing.T) {
			// Anti-vacuity: the recorded spans must actually be broken, or
			// this case is asserting nothing.
			if equalStrings(render(tc.text, tc.spans), tc.want) {
				t.Fatalf("recorded spans %q already equal the want; the case tests nothing",
					render(tc.text, tc.spans))
			}
			got := render(tc.text, snapToWords(tc.text, tc.spans))
			if !equalStrings(got, tc.want) {
				t.Errorf("snapToWords = %q, want %q", got, tc.want)
			}
		})
	}
}

// midWordEdges returns a description of every span edge that falls inside a
// word — the property this fix exists to eliminate.
func midWordEdges(text string, spans []analyzer.NerSpan) []string {
	var bad []string
	for _, s := range spans {
		if s.Start > 0 {
			before, _ := utf8.DecodeLastRuneInString(text[:s.Start])
			first, _ := utf8.DecodeRuneInString(text[s.Start:])
			if wordRune(before) && wordRune(first) {
				bad = append(bad, fmt.Sprintf("%s start [%d] in %q", s.EntityType, s.Start, text[s.Start:s.End]))
			}
		}
		if s.End < len(text) {
			last, _ := utf8.DecodeLastRuneInString(text[:s.End])
			after, _ := utf8.DecodeRuneInString(text[s.End:])
			if wordRune(last) && wordRune(after) {
				bad = append(bad, fmt.Sprintf("%s end [%d] in %q", s.EntityType, s.End, text[s.Start:s.End]))
			}
		}
	}
	return bad
}

// liveCorpus is deliberately heavy on names the WordPiece vocabulary does not
// hold whole, plus the tabular shapes hoop streams through the analyzer.
func liveCorpus() []string {
	names := []string{
		"Luan Lorenzo", "Nguyen Thi Hoa", "Wojciech Szczęsny", "Oluwaseun Adebayo",
		"Xochitl Guadalupe", "Þórunn Sveinsdóttir", "Aoife Ó Súilleabháin",
		"Thaddeus Kosciuszko", "Anastasiya Dziamidava", "Chidinma Okonkwo",
		"Bandar Seri Begawan", "Ouagadougou", "Antananarivo", "Reykjavík",
		"Thiruvananthapuram", "Ekaterinburg",
	}
	var texts []string
	for _, n := range names {
		texts = append(texts,
			n,
			"My name is "+n+" and I live here.",
			"user_name\t"+n+"\n",
			"id\tname\temail\n417768\t"+n+"\ta@b.com\n",
		)
	}
	return texts
}

// TestLiveNoSpanEndsMidWord is the end-to-end half of ATR-206: over a corpus
// built to provoke subword splits, no span the engine returns may start or end
// inside a word. Gated behind ALCATRAZ_NER_LIVE=1 (downloads ~250MB on first
// run); see TestLiveNER for the backend environment variables.
func TestLiveNoSpanEndsMidWord(t *testing.T) {
	if os.Getenv("ALCATRAZ_NER_LIVE") != "1" {
		t.Skip("set ALCATRAZ_NER_LIVE=1 to run the live model test")
	}
	cfg := DefaultConfig()
	cfg.Backend = os.Getenv("ALCATRAZ_NER_BACKEND")
	cfg.ORTLibraryPath = os.Getenv("ALCATRAZ_NER_ORT_LIB")
	cfg.Accelerator = os.Getenv("ALCATRAZ_NER_ACCELERATOR")

	nlp, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer nlp.Close()

	texts := liveCorpus()
	arts, err := nlp.ProcessTexts(texts, "en")
	if err != nil {
		t.Fatalf("ProcessTexts: %v", err)
	}

	var total, rawSplit int
	for i, text := range texts {
		if bad := midWordEdges(text, arts[i].Ents); len(bad) > 0 {
			t.Errorf("%q: span edges inside a word: %v", text, bad)
		}
		total += len(arts[i].Ents)
		if raw := nlp.rawSpans(t, text); len(raw) > len(arts[i].Ents) {
			rawSplit++
		}
	}
	if total == 0 {
		t.Fatal("the model found nothing in the whole corpus; the test proves nothing")
	}
	if rawSplit == 0 {
		t.Log("the pipeline no longer splits words on this backend: snapToWords is " +
			"now a safety net rather than a repair. Revisit the backend note in ner.go " +
			"and whether the upstream aggregation strategy should be selected instead.")
	} else {
		t.Logf("%d/%d texts came back split before snapping", rawSplit, len(texts))
	}

	// The measured regression, asserted directly: "Luan" under a column
	// header arrived as PERSON 0.8138 "Lu" and PERSON 0.5778 "an", so a 0.6
	// threshold masked exactly half the name.
	const tabular = "user_name\nLuan\n"
	arts2, err := nlp.ProcessTexts([]string{tabular}, "en")
	if err != nil {
		t.Fatalf("ProcessTexts: %v", err)
	}
	var got []string
	for _, s := range arts2[0].Ents {
		if s.EntityType == entities.Person {
			got = append(got, tabular[s.Start:s.End])
		}
	}
	if !equalStrings(got, []string{"Luan"}) {
		t.Errorf("PERSON spans in %q = %q, want [\"Luan\"]", tabular, got)
	}
}

// rawSpans runs the pipeline the way ProcessTexts does but without merging or
// snapping, so a live test can see what the model actually handed back.
func (e *Engine) rawSpans(t *testing.T, text string) []analyzer.NerSpan {
	t.Helper()
	folded, foldOffsets := foldASCII(text)
	out, err := e.pipeline.RunPipeline(e.runCtx, []string{folded})
	if err != nil {
		t.Fatalf("RunPipeline: %v", err)
	}
	var spans []analyzer.NerSpan
	for _, ents := range out.Entities {
		for _, ent := range ents {
			if s, ok := e.toNerSpan(text, foldOffsets, ent); ok {
				spans = append(spans, s)
			}
		}
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].Start < spans[j].Start })
	return spans
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
