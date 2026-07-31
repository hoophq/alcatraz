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
// Letters and digits only. '_' and '-' are deliberately excluded: BERT's
// pre-tokenizer splits on punctuation, so `user_name` and `Jean-Pierre` are
// several words to the model, and treating them as one would let a span over
// `name` swallow `user_`, or a span over one half of a hyphenated name swallow
// the other half the model deliberately left untagged.
func wordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// wordStart returns the first byte of the maximal run of word runes ending at
// byte offset i, which is i itself when the preceding rune is not a word rune.
func wordStart(s string, i int) int {
	for i > 0 {
		r, n := utf8.DecodeLastRuneInString(s[:i])
		if !wordRune(r) {
			break
		}
		i -= n
	}
	return i
}

// wordEnd returns the byte just past the maximal run of word runes starting at
// byte offset i, which is i itself when the rune at i is not a word rune.
func wordEnd(s string, i int) int {
	for i < len(s) {
		r, n := utf8.DecodeRuneInString(s[i:])
		if !wordRune(r) {
			break
		}
		i += n
	}
	return i
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
// The word-aggregating strategies (FIRST/MAX/AVERAGE) exist upstream to solve
// exactly this, but they are inert on the pure-Go backend: see the note on
// Config.Backend. Snapping here is not a substitute for that. It is a property
// of what this package promises its callers — byte spans over their text, on
// value boundaries — and it holds on every backend, including the ones where
// the upstream aggregation works.
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
		// span and the rune outside it must both be word runes.
		if first, _ := utf8.DecodeRuneInString(text[s.Start:]); wordRune(first) {
			if start := wordStart(text, s.Start); start != s.Start {
				if run := text[start:wordEnd(text, s.Start)]; utf8.RuneCountInString(run) <= maxWordChars {
					s.Start = start
				}
			}
		}
		if last, _ := utf8.DecodeLastRuneInString(text[:s.End]); wordRune(last) {
			if end := wordEnd(text, s.End); end != s.End {
				if run := text[wordStart(text, s.End):end]; utf8.RuneCountInString(run) <= maxWordChars {
					s.End = end
				}
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
