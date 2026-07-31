package ner

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/hoophq/alcatraz/analyzer"
	"github.com/hoophq/alcatraz/entities"
)

// BenchmarkLiveProcessTexts measures end-to-end batched inference throughput
// (tokenization, windowing, model, span mapping) over a synthetic message
// corpus. Like TestLiveNER it needs the real model, so it is gated behind
// ALCATRAZ_NER_LIVE=1 and configured by the same environment variables,
// which makes backend comparisons one-liners:
//
//	ALCATRAZ_NER_LIVE=1 go test -bench LiveProcessTexts -benchtime 1x -run xxx .
//	ALCATRAZ_NER_LIVE=1 ALCATRAZ_NER_BACKEND=ort CGO_LDFLAGS=-L/path/to/libtokenizers \
//	  go test -tags ORT -bench LiveProcessTexts -benchtime 1x -run xxx .
//
// ALCATRAZ_NER_BENCH_BYTES sets the corpus size (default 300000). Reported
// MB/s is corpus bytes per wall-clock second, single inference stream.
func BenchmarkLiveProcessTexts(b *testing.B) {
	if os.Getenv("ALCATRAZ_NER_LIVE") != "1" {
		b.Skip("set ALCATRAZ_NER_LIVE=1 to run the live benchmark")
	}
	cfg := DefaultConfig()
	cfg.Backend = os.Getenv("ALCATRAZ_NER_BACKEND")
	cfg.ORTLibraryPath = os.Getenv("ALCATRAZ_NER_ORT_LIB")
	cfg.Accelerator = os.Getenv("ALCATRAZ_NER_ACCELERATOR")

	corpusBytes := 300_000
	if v := os.Getenv("ALCATRAZ_NER_BENCH_BYTES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			b.Fatalf("ALCATRAZ_NER_BENCH_BYTES: %v", err)
		}
		corpusBytes = n
	}

	// Message-like prose with entities, in varied lengths (1-7 paragraphs)
	// so batching sees a realistic mix of short rows and windowed rows.
	const para = "Yesterday John Smith from Berlin deployed the payment service " +
		"and emailed maria.silva@example.com about the incident in Sao Paulo. " +
		"The on-call engineer, Alice Johnson, reviewed the production logs and " +
		"confirmed the fix before the Tuesday retrospective. "
	var texts []string
	total := 0
	for i := 0; total < corpusBytes; i++ {
		msg := strings.Repeat(para, 1+i%7)
		texts = append(texts, msg)
		total += len(msg)
	}

	nlp, err := New(context.Background(), cfg)
	if err != nil {
		b.Fatalf("New: %v", err)
	}
	defer nlp.Close()

	b.SetBytes(int64(total))
	b.ResetTimer()
	for b.Loop() {
		if _, err := nlp.ProcessTexts(texts, "en"); err != nil {
			b.Fatalf("ProcessTexts: %v", err)
		}
	}
}

// BenchmarkSnapToWordsOpaqueBlob pins the cost of snapping a span stranded
// inside a long run of word runes — a base64 payload or an opaque identifier,
// where there is no word boundary to find. wordBounds gives up after
// maxWordChars runes, so the time is a function of the cap and not of the
// blob: walking to the ends of a 4 MiB run first cost 17ms per span, against
// under a microsecond now. Needs no model, so it runs unconditionally.
func BenchmarkSnapToWordsOpaqueBlob(b *testing.B) {
	text := strings.Repeat("a", 4<<20)
	span := analyzer.NerSpan{EntityType: entities.Person, Start: 1000, End: 1010, Score: 0.9}
	// snapToWords writes through its input slice, so each iteration needs a
	// fresh copy or it would measure the second pass over a snapped span.
	spans := make([]analyzer.NerSpan, 1)
	b.ReportAllocs()
	for b.Loop() {
		spans = spans[:1]
		spans[0] = span
		snapToWords(text, spans)
	}
}
