package analyzer

// This file implements context-aware scoring: raising a detection's score when
// a word that hints at its entity type appears just before it.
//
// A regex alone cannot tell "1234-5678-9012-3456" in a payment form from the
// same digits in a log line, so most pattern recognizers are scored well below
// certainty. The words around a match carry the missing evidence, and
// recognizers already declare which ones matter via
// PatternRecognizer.WithContext. This turns those words into score.
//
// The rule follows Presidio's LemmaContextAwareEnhancer: +0.35 when a
// recognizer's own context word appears within the five words before the
// match, raised to at least 0.4, capped at MaxScore. Those numbers are the
// reason a pattern-based EMAIL_ADDRESS can reach 0.85 there and could only
// ever reach 0.5 here.
//
// Two deliberate differences from Presidio, both because this package has no
// NLP dependency to lean on:
//
//   - Words come from a built-in scanner over the raw text, not from spaCy
//     tokens, so context scoring works in the pattern-only engine — which is
//     where it matters, since that is the path with no model to fall back on.
//   - Matching is by whole word, not substring. Presidio asks whether the
//     context word occurs anywhere inside a token, which fires on unrelated
//     words: "ip" is inside "recipient", "zip" and "script", so any address
//     near them would be boosted. Whole-word matching with a plural fold
//     ("card" matches "cards", "address" matches "addresses") covers the
//     inflection that Presidio's substring rule was there to absorb, without
//     the false positives. Real lemmas are a follow-up, once the NLP seam
//     carries tokens (see TODO.md).

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Presidio's LemmaContextAwareEnhancer defaults, reproduced so a caller can
// see what the default enhancer does without reading its constructor.
const (
	// DefaultContextBoost is added to a result supported by a context word.
	DefaultContextBoost = 0.35
	// DefaultContextMinScore is the floor a supported result is raised to,
	// so a very weak pattern still lands somewhere usable when the
	// surrounding text agrees with it.
	DefaultContextMinScore = 0.4
	// DefaultContextWordsBefore is how many words before a match are read.
	DefaultContextWordsBefore = 5
	// DefaultContextWordsAfter is how many words after a match are read.
	// Presidio reads none: a label almost always precedes its value.
	DefaultContextWordsAfter = 0
)

// ContextualRecognizer is an optional extension of Recognizer for detectors
// that publish words hinting at their entity type nearby. PatternRecognizer
// implements it through WithContext. The engine's ContextEnhancer detects it
// by type assertion; a recognizer without context words is left alone.
type ContextualRecognizer interface {
	Recognizer
	// Context returns the words that support this recognizer's results when
	// they appear near a match. Matching is case-insensitive.
	Context() []string
}

// ContextEnhancer adjusts result scores using the text around each match. The
// engine runs it once per Analyze call, after every recognizer and before
// de-duplication and the score threshold — so a boost can decide which of two
// overlapping spans wins, and a result boosted over the threshold is kept.
//
// Implementations may modify and return results in place.
type ContextEnhancer interface {
	// Enhance returns results with scores adjusted for context. recs are the
	// recognizers that ran, so an implementation can look up the context
	// words behind a result's RecognizerName. artifacts holds the shared NLP
	// pass when one ran, and is nil otherwise.
	Enhance(text string, results []Result, recs []Recognizer, artifacts *NlpArtifacts) []Result
}

// WordContextEnhancer is the default ContextEnhancer: it scans the words
// immediately around each match and boosts the result when one of them is a
// context word of the recognizer that produced it.
//
// The zero value does nothing useful; use NewWordContextEnhancer and adjust
// the fields from there.
type WordContextEnhancer struct {
	// Boost is added to the score of a supported result.
	Boost float64
	// MinScore is the floor a supported result is raised to. A result already
	// scoring above it keeps its boosted score.
	MinScore float64
	// WordsBefore and WordsAfter size the window read around a match, in
	// words. The word a match starts inside is never part of its own context.
	WordsBefore int
	WordsAfter  int
}

// NewWordContextEnhancer returns the default enhancer, configured with
// Presidio's numbers.
func NewWordContextEnhancer() *WordContextEnhancer {
	return &WordContextEnhancer{
		Boost:       DefaultContextBoost,
		MinScore:    DefaultContextMinScore,
		WordsBefore: DefaultContextWordsBefore,
		WordsAfter:  DefaultContextWordsAfter,
	}
}

// Enhance implements ContextEnhancer. It ignores artifacts: words are read
// from the text directly, so context scoring does not require an NLP backend.
func (w *WordContextEnhancer) Enhance(text string, results []Result, recs []Recognizer, _ *NlpArtifacts) []Result {
	if len(results) == 0 {
		return results
	}
	byRecognizer, maxWord := contextWords(recs, results)
	if len(byRecognizer) == 0 {
		return results
	}

	// The fields are exported for tuning, so read them defensively rather than
	// trusting them: a negative window size is a caller's slip, not a request
	// to read backwards, and it would otherwise reach make as a negative
	// capacity and panic the whole Analyze call.
	before, after := max(w.WordsBefore, 0), max(w.WordsAfter, 0)

	// Result.Score is documented to stay within [MinScore, MaxScore]. Pulling
	// the floor into range here keeps every score written below in range too,
	// whatever the caller set, and without a second clamp per result.
	floor := min(max(w.MinScore, MinScore), MaxScore)

	// One window buffer for the whole call: on a chunk of log or query output
	// this runs once per detection, and a fresh slice each time would be the
	// bulk of what the enhancer allocates.
	window := make([]string, 0, before+after)

	for i := range results {
		want := byRecognizer[results[i].RecognizerName]
		if len(want) == 0 || results[i].Score >= MaxScore {
			continue
		}
		window = appendWordsBefore(window[:0], text, results[i].Start, before, maxWord)
		window = appendWordsAfter(window, text, results[i].End, after, maxWord)
		if !supported(window, want) {
			continue
		}
		score := results[i].Score + w.Boost
		if score < floor {
			score = floor
		}
		if score > MaxScore {
			score = MaxScore
		}
		results[i].Score = score
	}
	return results
}

// contextWords maps recognizer name to its context words, each pre-split into
// lower-case words so a multi-word entry ("social security", "codice
// fiscale") is matched as a phrase rather than as a string that no single
// word can equal.
//
// Only recognizers that actually produced a boostable result are split. A
// call typically has dozens of recognizers registered and results from one or
// two of them, so doing it the other way round would spend the bulk of the
// work on entries nothing will ever look up.
//
// The second return is the longest window word that could still match one of
// those phrases, in bytes; see foldWord for what the scanners do with it.
func contextWords(recs []Recognizer, results []Result) (map[string][][]string, int) {
	byRecognizer := make(map[string][][]string, 4)
	for _, r := range results {
		if r.Score < MaxScore {
			byRecognizer[r.RecognizerName] = nil
		}
	}
	if len(byRecognizer) == 0 {
		return nil, 0
	}

	longest := 0
	for _, rec := range recs {
		cr, ok := rec.(ContextualRecognizer)
		if !ok {
			continue
		}
		if _, want := byRecognizer[rec.Name()]; !want {
			continue
		}
		var phrases [][]string
		for _, entry := range cr.Context() {
			words := splitWords(entry)
			if len(words) == 0 {
				continue
			}
			for _, word := range words {
				longest = max(longest, len(word))
			}
			phrases = append(phrases, words)
		}
		if len(phrases) > 0 {
			byRecognizer[rec.Name()] = phrases
		}
	}
	if longest == 0 {
		return nil, 0
	}
	// wordMatches accepts at most two trailing bytes past the context word,
	// for the plural fold.
	return byRecognizer, longest + 2
}

// supported reports whether any of the recognizer's context phrases occurs in
// the window as a run of consecutive words.
func supported(window []string, phrases [][]string) bool {
	for _, phrase := range phrases {
		if containsPhrase(window, phrase) {
			return true
		}
	}
	return false
}

// containsPhrase reports whether phrase appears in window as consecutive
// words, comparing with wordMatches.
func containsPhrase(window, phrase []string) bool {
	if len(phrase) == 0 || len(phrase) > len(window) {
		return false
	}
	for i := 0; i+len(phrase) <= len(window); i++ {
		all := true
		for j, p := range phrase {
			if !wordMatches(window[i+j], p) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

// wordMatches reports whether a window word satisfies a context word.
//
// The plural fold is the whole of the stemming done here. It is what lets
// "cards" support a "card" context and "addresses" support "address" — the
// inflection that shows up in front of a real match — without accepting the
// unrelated words a substring test would. Anything beyond English plurals
// needs real lemmas, which is why the context lists spell their variants out.
//
// Every case here is bounded by len(context)+2, which is what lets foldWord
// leave a longer window word uncased: no amount of casing can bring it into
// range. Loosening the bound means loosening foldWord with it.
func wordMatches(word, context string) bool {
	switch {
	case word == context:
		return true
	case len(word) == len(context)+1:
		return strings.HasPrefix(word, context) && word[len(context)] == 's'
	case len(word) == len(context)+2:
		return strings.HasPrefix(word, context) && word[len(context):] == "es"
	default:
		return false
	}
}

// foldWord lower-cases a window word so it can be compared with the
// lower-cased context words, unless it is longer than maxWord — the longest
// word wordMatches could still accept — in which case the copy is dead weight
// and the word is passed through as a slice of the text.
//
// This is what keeps a base64 blob or a long hash sitting in front of a match
// from being copied in full just to be rejected on length. Measured on a 1 MiB
// opaque run between a label and an email, it is the difference between
// 1,057,166 and 2,323 bytes allocated per Analyze call.
//
// Note what is *not* bounded: the scanners still walk the run to find its far
// edge. Traversal is free next to the regex pass that found the match in the
// first place (596 ms/op either way in the same measurement), and giving up
// mid-run would silently drop a legitimate label sitting behind the blob.
func foldWord(s string, maxWord int) string {
	if len(s) > maxWord {
		return s
	}
	return strings.ToLower(s)
}

// appendWordsBefore appends up to n words ending before byte offset end to
// dst, in reading order, and returns the extended slice. maxWord is the
// longest word worth lower-casing; see foldWord.
func appendWordsBefore(dst []string, text string, end, n, maxWord int) []string {
	if n <= 0 || end <= 0 {
		return dst
	}
	if end > len(text) {
		end = len(text)
	}
	// A word cut in half by the boundary is part of the match, not of its
	// context: in "card1234" the match is the digits and "card" is only the
	// front of the word they sit in.
	if end < len(text) {
		if r, _ := utf8.DecodeRuneInString(text[end:]); isWordRune(r) {
			for end > 0 {
				r, size := utf8.DecodeLastRuneInString(text[:end])
				if !isWordRune(r) {
					break
				}
				end -= size
			}
		}
	}
	if end <= 0 {
		return dst
	}

	base := len(dst)
	i := end
	for i > 0 && len(dst)-base < n {
		// skip back over the separators before the next word
		for i > 0 {
			r, size := utf8.DecodeLastRuneInString(text[:i])
			if isWordRune(r) {
				break
			}
			i -= size
		}
		// consume the word
		wordEnd := i
		for i > 0 {
			r, size := utf8.DecodeLastRuneInString(text[:i])
			if !isWordRune(r) {
				break
			}
			i -= size
		}
		if i < wordEnd {
			dst = append(dst, foldWord(text[i:wordEnd], maxWord))
		}
	}

	// collected back to front; restore reading order
	for l, r := base, len(dst)-1; l < r; l, r = l+1, r-1 {
		dst[l], dst[r] = dst[r], dst[l]
	}
	return dst
}

// appendWordsAfter appends up to n words starting at or after byte offset
// start to dst and returns the extended slice. A word the boundary cuts in
// half is skipped, mirroring appendWordsBefore.
func appendWordsAfter(dst []string, text string, start, n, maxWord int) []string {
	if n <= 0 || start >= len(text) {
		return dst
	}
	if start < 0 {
		start = 0
	}

	base := len(dst)
	i := start
	if start > 0 {
		if r, _ := utf8.DecodeLastRuneInString(text[:start]); isWordRune(r) {
			for i < len(text) {
				r, size := utf8.DecodeRuneInString(text[i:])
				if !isWordRune(r) {
					break
				}
				i += size
			}
		}
	}
	for i < len(text) && len(dst)-base < n {
		for i < len(text) {
			r, size := utf8.DecodeRuneInString(text[i:])
			if isWordRune(r) {
				break
			}
			i += size
		}
		wordStart := i
		for i < len(text) {
			r, size := utf8.DecodeRuneInString(text[i:])
			if !isWordRune(r) {
				break
			}
			i += size
		}
		if wordStart < i {
			dst = append(dst, foldWord(text[wordStart:i], maxWord))
		}
	}
	return dst
}

// splitWords returns the lower-cased words of s, using the same word rule as
// the window scanners so a context entry and the text are cut alike.
func splitWords(s string) []string {
	var words []string
	start := -1
	for i, r := range s {
		if isWordRune(r) {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			words = append(words, strings.ToLower(s[start:i]))
			start = -1
		}
	}
	if start >= 0 {
		words = append(words, strings.ToLower(s[start:]))
	}
	return words
}

// isWordRune reports whether r can be part of a word.
//
// Letters and digits, which keeps "ipv4" and "2fa" whole while cutting
// "e-mail" into "e" and "mail" and "user_name" into "user" and "name" — both
// splits are wanted, since the halves are the words a context list names.
// Being rune-based rather than ASCII-based is what lets non-Latin context
// words ("주민등록번호", "บัตรประชาชน") work at all; combining marks count too,
// or a Thai vowel sign or a decomposed accent would split a word in half.
func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r)
}
