package ner

// This file implements windowed inference: texts whose token count exceeds
// the model's sequence limit are split into overlapping model-sized windows,
// each window is analyzed independently (batched with everything else), and
// the per-window spans are merged back. Without it, texts beyond the limit
// either fail inference outright (the pure-Go tokenizer applies no
// truncation for token-classification pipelines) or are silently truncated
// (the rust tokenizer), losing every entity past ~2KB of text.
//
// Window boundaries always come from a real tokenizer so every window
// provably fits the token budget: the pipeline's own tokenizer on the
// pure-Go backend, or a standalone pure-Go tokenizer loaded from the same
// tokenizer.json on the rust-tokenizer backends (ORT/XLA), where the
// pipeline's tokenizer cannot be introspected. Only if that load fails does
// windowing fall back to byte-sized windows — sized so they cannot exceed
// the budget either (see windows).

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/gomlx/go-huggingface/tokenizers/api"
	"github.com/gomlx/go-huggingface/tokenizers/hftokenizer"
	"github.com/hoophq/alcatraz/analyzer"
	"github.com/knights-analytics/hugot/pipelines"
)

const (
	// windowOverlapTokens is how many content tokens consecutive windows
	// share. An entity that straddles a window boundary is cut in one
	// window but appears whole in the next as long as it is shorter than
	// the overlap; mergeSpans then drops the cut fragment.
	windowOverlapTokens = 32

	// fallbackSpecialsSlack is how many tokens of the budget byte-fallback
	// windows reserve for special tokens ([CLS]/[SEP] and friends).
	fallbackSpecialsSlack = 8

	// fallbackOverlapBytes is the overlap of byte-fallback windows.
	fallbackOverlapBytes = 96
)

// textWindow is one model-sized slice of a folded text: byte offsets
// [start, end) into the folded string.
type textWindow struct {
	start, end int
}

// windows splits a folded text into inference-ready windows. Texts within
// the model's token budget yield a single window covering the whole text.
func (e *Engine) windows(folded string) []textWindow {
	if e.winTok != nil {
		// Every content token covers at least one byte (token spans are
		// non-empty and non-overlapping), so a text whose byte count plus
		// the fixed special tokens fits the budget provably encodes within
		// it: one window, no tokenizer pass. This is the common case —
		// short lines and cells — and the sizing pass is the only
		// tokenization ProcessTexts does outside the model call.
		if len(folded)+e.winSpecials <= e.tokenBudget {
			return []textWindow{{0, len(folded)}}
		}
		return tokenWindows(e.winTok, folded, e.tokenBudget)
	}
	// Last resort, when no windowing tokenizer could be loaded. Every
	// non-special token covers at least one byte of text (token spans are
	// non-empty and non-overlapping), so a window of tokenBudget-minus-
	// slack *bytes* can never encode past the token budget. That hard
	// guarantee costs efficiency — prose runs ~4.5 bytes per token, so
	// these windows are ~4x smaller than necessary — which is why the
	// tokenizer paths above are preferred.
	size := e.tokenBudget - fallbackSpecialsSlack
	return byteWindows(len(folded), size, fallbackOverlapBytes)
}

// windowTokenizer resolves the tokenizer used to size windows: the
// pipeline's tokenizer when it is the pure-Go implementation (the default
// backend), otherwise — rust-tokenizer builds (ORT/XLA) — a standalone
// pure-Go tokenizer loaded from the model's own tokenizer.json, which yields
// the same token counts as the rust one (same tokenizer spec). Nil means no
// tokenizer could be resolved; callers must fall back to byte windows.
func windowTokenizer(pipeline *pipelines.TokenClassificationPipeline, modelPath string) api.Tokenizer {
	if tk := pipeline.Model.Tokenizer; tk != nil && tk.GoTokenizer != nil {
		return tk.GoTokenizer.Tokenizer
	}
	return loadWindowTokenizer(modelPath)
}

// windowSpecials counts the special tokens tk adds around a text: the
// post-processor template is fixed, so one probe encoding tells how many
// tokens of every window's budget are not content. Tokens the mask does not
// flag but that cover no bytes count as specials too, as in contentSpans.
func windowSpecials(tk api.Tokenizer) int {
	enc := tk.EncodeWithAnnotations("a")
	return max(0, len(enc.IDs)-len(contentSpans(enc)))
}

// loadWindowTokenizer loads a pure-Go tokenizer from the model directory's
// tokenizer.json, configured like hugot configures its own Go tokenizer:
// specials count against the budget, and tokenWindows needs spans plus the
// special-tokens mask to separate content tokens from specials. Nil on any
// failure.
func loadWindowTokenizer(modelPath string) api.Tokenizer {
	content, err := os.ReadFile(filepath.Join(modelPath, "tokenizer.json"))
	if err != nil {
		return nil
	}
	tk, err := hftokenizer.NewFromContent(nil, content)
	if err != nil {
		return nil
	}
	if err := tk.With(api.EncodeOptions{
		AddSpecialTokens:         true,
		IncludeSpans:             true,
		IncludeSpecialTokensMask: true,
	}); err != nil {
		return nil
	}
	return tk
}

// tokenWindows builds windows by tokenizing the text once and slicing the
// content-token stream into runs that re-encode within totalBudget tokens
// (special tokens included). Each candidate window is verified by re-encoding
// its substring — cutting mid-word can inflate the token count, so the run is
// shrunk until it provably fits. Windows are built sequentially and each next
// window starts windowOverlapTokens before the previous one's actual end, so
// shrinking never opens a gap.
func tokenWindows(tk api.Tokenizer, folded string, totalBudget int) []textWindow {
	enc := tk.EncodeWithAnnotations(folded)
	if len(enc.IDs) <= totalBudget {
		return []textWindow{{0, len(folded)}}
	}
	spans := contentSpans(enc)
	if len(spans) == 0 {
		return []textWindow{{0, len(folded)}}
	}
	// Special tokens ([CLS]/[SEP], model-dependent) occupy part of the
	// budget in every window.
	specials := len(enc.IDs) - len(spans)
	contentBudget := totalBudget - specials
	if contentBudget < 1 {
		contentBudget = 1
	}

	var wins []textWindow
	start := 0
	for start < len(spans) {
		take := min(contentBudget, len(spans)-start)
		for take > 1 {
			w := textWindow{spans[start].Start, spans[start+take-1].End}
			excess := len(tk.Encode(folded[w.start:w.end])) - totalBudget
			if excess <= 0 {
				break
			}
			take -= max(1, excess)
		}
		if take < 1 {
			take = 1
		}
		wins = append(wins, textWindow{spans[start].Start, spans[start+take-1].End})
		if start+take >= len(spans) {
			break
		}
		start += max(1, take-windowOverlapTokens)
	}
	return wins
}

// contentSpans returns the byte spans of non-special tokens, in text order.
// Special tokens are identified by the tokenizer's mask when present, and by
// their empty span otherwise.
func contentSpans(enc api.AnnotatedEncoding) []api.TokenSpan {
	spans := make([]api.TokenSpan, 0, len(enc.Spans))
	for i, s := range enc.Spans {
		if len(enc.SpecialTokensMask) == len(enc.Spans) && enc.SpecialTokensMask[i] == 1 {
			continue
		}
		if s.End <= s.Start {
			continue
		}
		spans = append(spans, s)
	}
	return spans
}

// byteWindows splits [0, n) into fixed-size windows with the given overlap.
// The folded text is always single-byte-per-rune ASCII, so any byte offset
// is a valid cut point.
func byteWindows(n, size, overlap int) []textWindow {
	if size < 1 {
		size = 1
	}
	if n <= size {
		return []textWindow{{0, n}}
	}
	step := size - overlap
	if step < 1 {
		step = 1
	}
	var wins []textWindow
	for s := 0; ; s += step {
		e := min(s+size, n)
		wins = append(wins, textWindow{s, e})
		if e == n {
			return wins
		}
	}
}

// mergeSpans resolves the duplicates that overlapping windows produce: exact
// duplicates collapse keeping the highest score, and a span contained within
// a wider span of the same entity type (a fragment cut at a window boundary)
// is dropped. The result is sorted by Start, then End.
func mergeSpans(spans []analyzer.NerSpan) []analyzer.NerSpan {
	if len(spans) < 2 {
		return spans
	}
	sort.Slice(spans, func(i, j int) bool {
		if spans[i].Start != spans[j].Start {
			return spans[i].Start < spans[j].Start
		}
		return spans[i].End > spans[j].End
	})
	// widest[type] is the largest End among kept spans of that type; since
	// spans are visited in ascending Start order, a span with End within it
	// is fully contained in an earlier, wider span.
	widest := map[string]int{}
	out := spans[:0]
	for _, s := range spans {
		if len(out) > 0 {
			last := &out[len(out)-1]
			if last.EntityType == s.EntityType && last.Start == s.Start && last.End == s.End {
				if s.Score > last.Score {
					last.Score = s.Score
				}
				continue
			}
		}
		if end, ok := widest[s.EntityType]; ok && s.End <= end {
			continue
		}
		out = append(out, s)
		if s.End > widest[s.EntityType] {
			widest[s.EntityType] = s.End
		}
	}
	return out
}
