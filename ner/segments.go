package ner

// This file implements input segmentation: the caller-selected rule that
// decides what the model sees as one sequence (Config.Segmentation).
//
// Windowing (windows.go) and segmentation answer different questions.
// Windowing is a capacity constraint — a text longer than the token budget
// physically cannot be one sequence, so it is cut into overlapping windows
// and the spans are merged back. Segmentation is a quality choice — the text
// would fit, but the model reads it better in pieces.
//
// The reason it is a choice at all is that a transformer's prediction for a
// token depends on every other token in the sequence. That is what makes the
// model good at prose and what makes it erratic on machine output: a name in
// a 28-column psql row sits among UUIDs, timestamps and JSON, which no
// news-trained model has seen, and the whole row can come back with nothing.
// The same name on its own line, or on its own in a cell, is found reliably.
// Cutting on the delimiters the output already has removes that noise without
// inventing any structure the caller has to describe.
//
// Ablating the reproduction fixture (three psql rows, one name each) shows
// how much of that noise is which, under SegmentWhole:
//
//	header + 3 rows   0/3
//	3 rows, no header 2/3
//	1 row alone       1/1
//
// A tab-separated header is the single worst thing in the input — 28 column
// names in a row is a sequence the model has no reading of, and it drags the
// rows after it down with it. Cross-row context costs the rest. Segmentation
// removes both, because both are context that was never really context.
//
// Segments partition the folded text: disjoint, in order, covering every
// byte. That is what lets a window offset inside a segment be rebased onto
// the text by adding the segment start, and it means concatenating the
// segments reproduces the input exactly — separators stay attached to the
// segment they terminate rather than being dropped.

import (
	"fmt"
	"strings"
	"unicode"
)

// segment is one inference unit: a byte range [start, end) into the folded
// text.
type segment struct {
	start, end int
}

// normalizeSegmentation lower-cases Config.Segmentation and rejects anything
// that is not a known rule.
//
// A misspelling has to be an error rather than a fallback: the quiet result
// would be analyzing tabular output as one blob, which is the exact failure
// this option exists to avoid, and it would look like the model simply not
// finding the names.
func normalizeSegmentation(mode string) (string, error) {
	mode = strings.ToLower(mode)
	switch mode {
	case "", SegmentWhole, SegmentLines, SegmentFields:
		return mode, nil
	default:
		return "", fmt.Errorf("unknown segmentation %q (want %q, %q or %q)",
			mode, SegmentWhole, SegmentLines, SegmentFields)
	}
}

// segments splits a folded text according to the configured rule. The result
// partitions the text; an empty text yields no segments.
func (e *Engine) segments(folded string) []segment {
	if folded == "" {
		return nil
	}
	switch e.cfg.Segmentation {
	case SegmentLines:
		return cutAfter(folded, isLineSep)
	case SegmentFields:
		return cutAfter(folded, isFieldSep)
	default:
		return []segment{{0, len(folded)}}
	}
}

// cutAfter partitions folded, ending a segment after every separator byte.
// The separator belongs to the segment it terminates, so the segments
// concatenate back to the input.
func cutAfter(folded string, sep func(byte) bool) []segment {
	segs := make([]segment, 0, 8)
	start := 0
	for i := 0; i < len(folded); i++ {
		if sep(folded[i]) {
			segs = append(segs, segment{start, i + 1})
			start = i + 1
		}
	}
	if start < len(folded) {
		segs = append(segs, segment{start, len(folded)})
	}
	return segs
}

// isLineSep cuts records apart. Only "\n" is a cut point: a bare "\r" as a
// line ending is extinct, and in CRLF text the "\r" simply trails the segment
// like any other separator byte.
func isLineSep(b byte) bool { return b == '\n' }

// isFieldSep cuts values apart, in addition to records.
//
// Tab is the only field delimiter recognized, because it is the only one that
// is unambiguously layout rather than punctuation: it is what psql's
// unaligned mode, TSV exports, cut and awk emit, and it does not occur inside
// prose or inside a name. Comma and pipe do occur in both ("Davis, Carol",
// "a || b"), so cutting on them would fragment the free text this engine is
// mainly used on. Aligned output padded with spaces is therefore not split
// into fields either.
func isFieldSep(b byte) bool { return b == '\n' || b == '\t' }

// hasWordRune reports whether s holds a letter or a digit.
//
// Segments without one are pure layout — a blank line, "|", "+----+----+", a
// run of dashes — and cannot carry an entity, so they are dropped before
// inference. That is a saving, not a filter: a sweep of 56 separator-only
// strings through the default model returned zero entities, and the corpus
// recall figures in Config.Segmentation are identical with and without the
// skip. Under SegmentFields it removes a large fraction of the segments a
// wide table produces.
func hasWordRune(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}
