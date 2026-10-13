package analyzer

import (
	"slices"
	"strings"
)

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

// RemoveDuplicates collapses overlapping detections of the same entity type:
// a span contained in another span of the same type is dropped (the
// enclosing one wins, whatever the scores), and of two identical spans the
// higher-scoring one is kept, the earlier on a tie. Zero-score results are
// discarded. Detections of different entity types never suppress each
// other. The result is sorted by score (descending), then start offset
// (ascending), then length (descending).
//
// The input slice is sorted and reused for the result; callers that need
// the original order must pass a copy. With fewer than two results no sort
// runs and the input is returned as is (or emptied when zero-scored).
func RemoveDuplicates(results []Result) []Result {
	if len(results) < 2 {
		if len(results) == 1 && results[0].Score == 0.0 {
			return results[:0]
		}
		return results
	}

	// Group by type, then order spans so that any span containing another
	// precedes it: start ascending, and for an equal start the longer first.
	// Stable, so identical spans keep input order and the tie rule below
	// favours the earlier one.
	slices.SortStableFunc(results, func(a, b Result) int {
		if a.EntityType != b.EntityType {
			return strings.Compare(a.EntityType, b.EntityType)
		}
		if a.Start != b.Start {
			return a.Start - b.Start
		}
		return b.End - a.End
	})

	// One sweep. Kept spans of a type have strictly increasing starts and
	// ends, so a span contained in any kept span of its type is contained
	// in the last one; a span not contained in the last one is kept.
	kept := results[:0]
	for _, r := range results {
		if r.Score == 0.0 {
			continue
		}
		if n := len(kept); n > 0 {
			last := &kept[n-1]
			if last.EntityType == r.EntityType && r.End <= last.End {
				if r.Start == last.Start && r.End == last.End && r.Score > last.Score {
					*last = r
				}
				continue
			}
		}
		kept = append(kept, r)
	}

	slices.SortStableFunc(kept, func(a, b Result) int {
		if a.Score != b.Score {
			if a.Score > b.Score {
				return -1
			}
			return 1
		}
		if a.Start != b.Start {
			return a.Start - b.Start
		}
		return b.Len() - a.Len()
	})
	return kept
}
