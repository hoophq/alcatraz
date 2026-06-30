// Package analyzer is the detection framework for alcatraz: the recognizer
// contract, regex pattern recognizers, the registry and the engine that runs
// them. It is a pure-Go, dependency-free port of the pattern-based core of
// Microsoft Presidio's analyzer.
//
// The framework is deliberately separate from the concrete recognizers (which
// live in the recognizers package) so callers can build a custom engine with
// only the recognizers they want, or add their own via the Recognizer
// interface. Most callers should use the top-level alcatraz package, which
// wires the framework together with the full default recognizer set.
package analyzer

import "sort"

// Score bounds for a detection. A score is a confidence in [MinScore, MaxScore].
const (
	MinScore = 0.0
	MaxScore = 1.0
)

// Result is a single detected entity: its type, byte-offset span in the
// analyzed text and a confidence score. Offsets are byte indices (Go's regexp
// engine reports bytes), so text[Start:End] yields the matched substring.
type Result struct {
	// EntityType is the canonical entity name, e.g. "EMAIL_ADDRESS".
	EntityType string
	// Start and End are byte offsets into the analyzed text: [Start, End).
	Start int
	End   int
	// Score is the detection confidence in [MinScore, MaxScore].
	Score float64
	// Text is the matched substring. It is populated by the engine after
	// filtering; recognizers leave it empty.
	Text string
	// RecognizerName identifies which recognizer produced the result.
	RecognizerName string
}

// Len returns the byte length of the detected span.
func (r Result) Len() int { return r.End - r.Start }

// Intersects returns the number of overlapping bytes between r and other, or 0
// when they do not overlap.
func (r Result) Intersects(other Result) int {
	if r.End < other.Start || other.End < r.Start {
		return 0
	}
	return min(r.End, other.End) - max(r.Start, other.Start)
}

// ContainedIn reports whether r's span is fully inside other's span.
func (r Result) ContainedIn(other Result) bool {
	return r.Start >= other.Start && r.End <= other.End
}

// Contains reports whether r's span fully encloses other's span.
func (r Result) Contains(other Result) bool {
	return r.Start <= other.Start && r.End >= other.End
}

// EqualIndices reports whether r and other cover exactly the same span.
func (r Result) EqualIndices(other Result) bool {
	return r.Start == other.Start && r.End == other.End
}

// RemoveDuplicates collapses overlapping detections of the same entity type,
// keeping the highest-scoring span and dropping any span contained within a
// kept one. Zero-score results are discarded. Detections of different entity
// types never suppress each other. The result is sorted by score (descending),
// then start offset (ascending), then length (descending).
func RemoveDuplicates(results []Result) []Result {
	filtered := make([]Result, 0, len(results))
	for _, r := range results {
		if r.Score == 0.0 {
			continue
		}
		add := true
		for _, ex := range filtered {
			// an equal span of the same type with a lower-or-equal score loses
			if r.EqualIndices(ex) && r.EntityType == ex.EntityType && r.Score <= ex.Score {
				add = false
				break
			}
			// a span contained in an existing one of the same type is redundant
			if r.ContainedIn(ex) && r.EntityType == ex.EntityType {
				add = false
				break
			}
		}
		if !add {
			continue
		}
		// drop any existing span that this one now subsumes (same type)
		kept := filtered[:0]
		for _, ex := range filtered {
			if ex.ContainedIn(r) && ex.EntityType == r.EntityType {
				continue
			}
			kept = append(kept, ex)
		}
		filtered = append(kept, r)
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].Score != filtered[j].Score {
			return filtered[i].Score > filtered[j].Score
		}
		if filtered[i].Start != filtered[j].Start {
			return filtered[i].Start < filtered[j].Start
		}
		return filtered[i].Len() > filtered[j].Len()
	})
	return filtered
}
