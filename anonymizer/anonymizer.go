// Package anonymizer replaces detected PII spans in text, turning the
// analyzer's []Result into a sanitized string:
//
//	results := eng.Analyze(text, alcatraz.Options{})
//	safe := anonymizer.Anonymize(text, results, anonymizer.Mask('*'))
//
// Operators decide what each span becomes: Mask keeps the span's length
// using a chosen character ('#', '*', …), MaskKeepLast leaves a recognizable
// tail (last 4 card digits), Replace emits "<ENTITY_TYPE>" placeholders,
// ReplaceWith a fixed string, and Redact removes the span. An Operator is
// just a func, so custom transforms (hashing, encryption, tokenization) drop
// in the same way. Per-entity operators are configured via Config.
//
// Overlapping spans are resolved safely: higher-scoring spans keep their
// full extent and lower-scoring ones are trimmed to the uncovered remainder,
// so every detected byte is anonymized exactly once — a partial overlap
// never leaks the uncovered part of a detection.
//
// Like the rest of the core, this package is pure Go and dependency-free.
package anonymizer

import (
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/hoophq/alcatraz/analyzer"
)

// Operator produces the replacement for one detected span. entityType is the
// canonical entity name (e.g. "EMAIL_ADDRESS") and match is the exact text
// being replaced. When overlap resolution trims a span, match is the trimmed
// portion.
type Operator func(entityType, match string) string

// Mask replaces every rune of the match with char, preserving the match's
// rune length: "555-1234" masked with '#' becomes "########".
func Mask(char rune) Operator {
	return func(_, match string) string {
		return strings.Repeat(string(char), utf8.RuneCountInString(match))
	}
}

// MaskKeepLast is Mask but leaves the trailing keep runes visible:
// MaskKeepLast('*', 4) turns "4532015112830366" into "************0366".
// When the match has keep runes or fewer, it is returned unchanged.
func MaskKeepLast(char rune, keep int) Operator {
	if keep < 0 {
		keep = 0
	}
	return func(_, match string) string {
		runes := []rune(match)
		if len(runes) <= keep {
			return match
		}
		cut := len(runes) - keep
		return strings.Repeat(string(char), cut) + string(runes[cut:])
	}
}

// Replace substitutes each span with its entity type in angle brackets:
// "jane@example.com" becomes "<EMAIL_ADDRESS>". This is the default operator
// when Config.Default is nil.
func Replace() Operator {
	return func(entityType, _ string) string { return "<" + entityType + ">" }
}

// ReplaceWith substitutes each span with a fixed placeholder.
func ReplaceWith(placeholder string) Operator {
	return func(_, _ string) string { return placeholder }
}

// Redact removes each span entirely.
func Redact() Operator {
	return func(_, _ string) string { return "" }
}

// Config selects operators per entity type, with a fallback for the rest.
type Config struct {
	// Default handles every entity type without a PerEntity entry.
	// Nil means Replace().
	Default Operator
	// PerEntity overrides the operator for specific entity types, keyed by
	// canonical name (see the entities package constants).
	PerEntity map[string]Operator
}

// Anonymize rewrites text by applying op to every detected span. results is
// the output of an Analyze call on the same text.
func Anonymize(text string, results []analyzer.Result, op Operator) string {
	return AnonymizeWith(text, results, Config{Default: op})
}

// AnonymizeWith is Anonymize with per-entity operator selection.
func AnonymizeWith(text string, results []analyzer.Result, cfg Config) string {
	def := cfg.Default
	if def == nil {
		def = Replace()
	}
	out := []byte(text)
	// Spans come back sorted by Start descending, so each replacement
	// leaves the offsets of the spans still to process untouched.
	for _, s := range resolve(results, len(text)) {
		op := def
		if o, ok := cfg.PerEntity[s.EntityType]; ok && o != nil {
			op = o
		}
		repl := op(s.EntityType, text[s.Start:s.End])
		out = append(out[:s.Start], append([]byte(repl), out[s.End:]...)...)
	}
	return string(out)
}

// resolve turns detections into non-overlapping spans sorted by Start
// descending. The engine only de-duplicates same-type overlaps, so spans of
// different entity types can still intersect here. Higher-scoring spans keep
// their full extent; lower-scoring ones are trimmed to whatever they cover
// that nothing above them does, guaranteeing every detected byte is
// anonymized under exactly one entity type.
func resolve(results []analyzer.Result, textLen int) []analyzer.Result {
	spans := make([]analyzer.Result, 0, len(results))
	for _, r := range results {
		// Results may come from outside the engine; clamp instead of
		// trusting the offsets.
		if r.Start < 0 {
			r.Start = 0
		}
		if r.End > textLen {
			r.End = textLen
		}
		if r.End > r.Start {
			spans = append(spans, r)
		}
	}
	sort.SliceStable(spans, func(i, j int) bool {
		if spans[i].Score != spans[j].Score {
			return spans[i].Score > spans[j].Score
		}
		if spans[i].Len() != spans[j].Len() {
			return spans[i].Len() > spans[j].Len()
		}
		return spans[i].Start < spans[j].Start
	})

	var kept []analyzer.Result
	for _, s := range spans {
		pieces := []analyzer.Result{s}
		for _, k := range kept {
			var next []analyzer.Result
			for _, p := range pieces {
				next = append(next, subtract(p, k)...)
			}
			pieces = next
			if len(pieces) == 0 {
				break
			}
		}
		kept = append(kept, pieces...)
	}

	sort.Slice(kept, func(i, j int) bool { return kept[i].Start > kept[j].Start })
	return kept
}

// subtract returns the parts of p not covered by k: zero pieces (p contained
// in k), one (no overlap, or overlap on one side) or two (k strictly inside p).
func subtract(p, k analyzer.Result) []analyzer.Result {
	if p.End <= k.Start || k.End <= p.Start {
		return []analyzer.Result{p}
	}
	var out []analyzer.Result
	if p.Start < k.Start {
		left := p
		left.End = k.Start
		out = append(out, left)
	}
	if p.End > k.End {
		right := p
		right.Start = k.End
		out = append(out, right)
	}
	return out
}
