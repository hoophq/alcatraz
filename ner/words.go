package ner

import (
	"sort"
	"unicode"
	"unicode/utf8"

	"github.com/hoophq/alcatraz/analyzer"
)

// maxWordChars bounds how far snapToWords will grow a span at one edge.
//
// It is WordPiece's max_input_chars_per_word, 100 in the pinned model's
// tokenizer.json and the default in every BERT tokenizer config. A word longer
// than that is never split into subwords — the tokenizer emits a single [UNK]
// covering the whole word — so a span edge stranded inside a word can only
// ever be inside a run of at most 100 characters. Refusing to extend across a
// longer run therefore costs nothing real, and it stops a stray span inside a
// base64 blob or a long opaque identifier from growing into a mask over
// thousands of bytes.
const maxWordChars = 100

// wordRune reports whether r can appear inside a tokenizer word.
//
// Letters, digits and combining marks. A mark belongs to the letter it sits
// on: decomposed text reaches us from macOS exports and database dumps that
// never normalized, and foldASCII renders one byte per rune, so the model
// sees "n" and "x" where the text holds "n" + U+0301 and can tag the base
// letter alone. Counting the mark as a boundary would end the span between a
// letter and its own accent — masking NFD "Zieliński" then yields
// "<PERSON>́ski", the leak this file exists to close.
//
// '_' and '-' are deliberately excluded: BERT's pre-tokenizer splits on
// punctuation, so `user_name` and `Jean-Pierre` are several words to the
// model, and treating them as one would let a span over `name` swallow
// `user_`, or a span over one half of a hyphenated name swallow the other
// half the model deliberately left untagged.
func wordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.M, r)
}

// wordBounds returns the maximal run of word runes spanning byte offset i:
// the word runes immediately before i, and those from i onwards. Either half
// may be empty, so a run of one word rune, or none at all, is a normal answer.
//
// ok is false when the run holds more than maxWordChars runes. That verdict
// is reached after walking at most maxWordChars+1 of them, so the cost is
// bounded by the cap rather than by the length of the run — a stray span
// inside a megabyte-long base64 blob is refused without reading the blob.
func wordBounds(s string, i int) (start, end int, ok bool) {
	budget := maxWordChars
	start, end = i, i
	for start > 0 {
		r, n := utf8.DecodeLastRuneInString(s[:start])
		if !wordRune(r) {
			break
		}
		if budget == 0 {
			return i, i, false
		}
		budget--
		start -= n
	}
	for end < len(s) {
		r, n := utf8.DecodeRuneInString(s[end:])
		if !wordRune(r) {
			break
		}
		if budget == 0 {
			return i, i, false
		}
		budget--
		end += n
	}
	return start, end, true
}

// snapToWords grows every span out to the word boundaries it sits inside and
// unions same-type spans that then touch or overlap. Scores are combined by
// taking the maximum.
//
// A span that begins or ends in the middle of a word is never a usable answer
// from a redactor. The rest of the word stays in the clear — on
// "Bandar Seri Begawan" the model reports "andar Seri Begawa", so masking
// leaves "B<PERSON>n" and the reader recovers the name. The span cannot be
// allow-listed or pseudonymized either, because it is not the value anyone
// would write down. And each fragment carries its own score, so a caller
// threshold can accept one half of a name and reject the other: "Luan" in
// tabular context scores 0.81 on "Lu" and 0.58 on "an", and at a threshold of
// 0.6 exactly half the name is masked.
//
// Mid-word spans arise because the model tags WordPiece tokens, not words, and
// hugot's SIMPLE aggregation starts a new group at every B- tag — so a name
// split into "Lu" + "##an" and tagged B-PER twice comes back as two entities.
// The word-aggregating strategies (FIRST/MAX/AVERAGE) address that upstream.
// They were inert on the pure-Go backend until hugot v0.7.7, which fixed the
// subword detection they depend on (knights-analytics/hugot#136). Snapping is
// not a substitute for them: it is a property of what this package promises
// its callers — byte spans over their text, on value boundaries — and it holds
// under any strategy, including the SIMPLE one configured here, which never
// consults subword information at all.
//
// Growth is bounded at maxWordChars per edge and never crosses a non-word
// rune, so a span can only ever expand within the single word it was already
// inside. text must be the exact string the spans index into.
func snapToWords(text string, spans []analyzer.NerSpan) []analyzer.NerSpan {
	if len(spans) == 0 {
		return spans
	}

	for i := range spans {
		s := &spans[i]
		if s.Start < 0 || s.End > len(text) || s.Start >= s.End {
			continue // malformed; leave it exactly as it is
		}
		// Only grow an edge that is genuinely mid-word: the rune inside the
		// span and the rune outside it must both be word runes. The guards
		// establish the inside rune — without them a span ending just after a
		// space would grow forward into the next word.
		if first, _ := utf8.DecodeRuneInString(text[s.Start:]); wordRune(first) {
			if start, _, ok := wordBounds(text, s.Start); ok {
				s.Start = start
			}
		}
		if last, _ := utf8.DecodeLastRuneInString(text[:s.End]); wordRune(last) {
			if _, end, ok := wordBounds(text, s.End); ok {
				s.End = end
			}
		}
	}

	// Stable: two spans of different types can snap to the exact same word,
	// and callers should not see them swap order run to run.
	sort.SliceStable(spans, func(i, j int) bool {
		if spans[i].Start != spans[j].Start {
			return spans[i].Start < spans[j].Start
		}
		return spans[i].End > spans[j].End
	})

	// last[type] indexes the running union of that type in out. Spans arrive
	// in ascending Start order, so any span that touches an earlier one of its
	// type also touches that union.
	last := map[string]int{}
	out := spans[:0]
	for _, s := range spans {
		if i, ok := last[s.EntityType]; ok && s.Start <= out[i].End {
			if s.End > out[i].End {
				out[i].End = s.End
			}
			if s.Score > out[i].Score {
				out[i].Score = s.Score
			}
			continue
		}
		out = append(out, s)
		last[s.EntityType] = len(out) - 1
	}
	return out
}
