package ner

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/hoophq/alcatraz/analyzer"
	"github.com/hoophq/alcatraz/entities"
)

// segsOf renders the segments of folded under mode as strings, so tests can
// state the expected split literally.
func segsOf(t *testing.T, mode, folded string) []string {
	t.Helper()
	e := &Engine{cfg: Config{Segmentation: mode}}
	var out []string
	for _, s := range e.segments(folded) {
		out = append(out, folded[s.start:s.end])
	}
	return out
}

func TestSegments(t *testing.T) {
	const row = "id\tuser_name\temail\n1\tLuan\tluan@hoop.dev\n"

	tests := []struct {
		name string
		mode string
		text string
		want []string
	}{
		{"whole keeps one segment", SegmentWhole, row, []string{row}},
		{"empty mode means whole", "", row, []string{row}},
		{
			"lines cut after newline", SegmentLines, row,
			[]string{"id\tuser_name\temail\n", "1\tLuan\tluan@hoop.dev\n"},
		},
		{
			"fields cut after newline and tab", SegmentFields, row,
			[]string{"id\t", "user_name\t", "email\n", "1\t", "Luan\t", "luan@hoop.dev\n"},
		},
		{
			"unterminated last line is a segment", SegmentLines, "a\nb",
			[]string{"a\n", "b"},
		},
		{
			"blank lines survive as their own segments", SegmentLines, "a\n\nb\n",
			[]string{"a\n", "\n", "b\n"},
		},
		{
			"empty fields survive", SegmentFields, "a\t\tb\n",
			[]string{"a\t", "\t", "b\n"},
		},
		{
			"CRLF keeps the CR on the segment it ends", SegmentLines, "a\r\nb\r\n",
			[]string{"a\r\n", "b\r\n"},
		},
		{
			"prose is untouched by lines when it has no newline",
			SegmentLines, "Alice Johnson met Bob Miller in Paris.",
			[]string{"Alice Johnson met Bob Miller in Paris."},
		},
		{
			"fields ignores comma and pipe", SegmentFields, "Davis, Carol | Berlin\n",
			[]string{"Davis, Carol | Berlin\n"},
		},
		{"whole of empty text yields nothing", SegmentWhole, "", nil},
		{"lines of empty text yields nothing", SegmentLines, "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := segsOf(t, tt.mode, tt.text)
			if len(got) != len(tt.want) {
				t.Fatalf("segments = %q, want %q", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("segments = %q, want %q", got, tt.want)
				}
			}
		})
	}
}

// TestSegmentsPartition pins the invariant every offset rebase in
// ProcessTexts depends on: segments are ordered, disjoint, and cover the text
// exactly, so concatenating them reproduces the input under every mode.
func TestSegmentsPartition(t *testing.T) {
	texts := []string{
		"",
		"\n",
		"\t",
		"a",
		"a\n",
		"\na",
		"\n\n\n",
		"\t\t\t",
		"id\tuser_name\n1\tLuan\n(1 row)\n",
		"no separators at all",
		"trailing tab\t",
		"José\tSão Paulo\nNúñez\tBogotá\n",
		strings.Repeat("col\tvalue\n", 50),
	}
	for _, mode := range []string{SegmentWhole, SegmentLines, SegmentFields, ""} {
		for _, text := range texts {
			segs := (&Engine{cfg: Config{Segmentation: mode}}).segments(text)
			prev := 0
			var b strings.Builder
			for i, s := range segs {
				if s.start != prev {
					t.Fatalf("mode %q text %q: segment %d starts at %d, want %d (gap or overlap)",
						mode, text, i, s.start, prev)
				}
				if s.end <= s.start {
					t.Fatalf("mode %q text %q: segment %d is empty [%d,%d)",
						mode, text, i, s.start, s.end)
				}
				if s.end > len(text) {
					t.Fatalf("mode %q text %q: segment %d ends past the text at %d",
						mode, text, i, s.end)
				}
				b.WriteString(text[s.start:s.end])
				prev = s.end
			}
			if prev != len(text) {
				t.Fatalf("mode %q text %q: segments cover %d of %d bytes", mode, text, prev, len(text))
			}
			if b.String() != text {
				t.Fatalf("mode %q: segments concatenate to %q, want %q", mode, b.String(), text)
			}
		}
	}
}

func TestHasWordRune(t *testing.T) {
	with := []string{"a", "Z", "0", "Luan", "id\t", "x\n", "José", "日本", "_a_", "v1.2"}
	without := []string{"", " ", "\t", "\n", "\t\t", " | ", "+----+----+", "---",
		"!@#$%^&*()", "  \t \n", "::", "->", "«»", "—"}
	for _, s := range with {
		if !hasWordRune(s) {
			t.Errorf("hasWordRune(%q) = false, want true", s)
		}
	}
	for _, s := range without {
		if hasWordRune(s) {
			t.Errorf("hasWordRune(%q) = true, want false", s)
		}
	}
}

func TestNormalizeSegmentation(t *testing.T) {
	for in, want := range map[string]string{
		"":       "",
		"whole":  SegmentWhole,
		"lines":  SegmentLines,
		"fields": SegmentFields,
		"Lines":  SegmentLines,
		"FIELDS": SegmentFields,
	} {
		got, err := normalizeSegmentation(in)
		if err != nil {
			t.Errorf("normalizeSegmentation(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("normalizeSegmentation(%q) = %q, want %q", in, got, want)
		}
	}
	for _, in := range []string{"per-line", "line", "field", "cells", "none", " lines"} {
		if _, err := normalizeSegmentation(in); err == nil {
			t.Errorf("normalizeSegmentation(%q) accepted an unknown rule", in)
		}
	}
}

func TestNewRejectsUnknownSegmentation(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Segmentation = "per-line"
	// Validation runs before any model resolution, so this needs no model on
	// disk and no network.
	_, err := New(context.Background(), cfg)
	if err == nil {
		t.Fatal("New accepted an unknown segmentation")
	}
	for _, want := range []string{"segmentation", `"per-line"`, SegmentLines} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// psqlColumns and psqlRow mirror, column for column, the `SELECT * FROM
// sessions` capture from hoop that motivated Config.Segmentation: 28 columns
// of tab-separated psql output in unaligned mode. The value shapes matter as
// much as the width — the sparse empty cells, the JSON blob, and the name
// sandwiched between two lowercase addresses derived from it are what the
// model actually chokes on — so they are reproduced rather than filled with
// generic placeholder text. The identifiers are synthesized.
var psqlColumns = []string{
	"id", "org_id", "connection", "connection_type", "verb", "labels",
	"user_id", "user_name", "user_email", "status", "blob_input_id",
	"blob_stream_id", "metadata", "created_at", "ended_at", "metrics",
	"jira_issue", "integrations_metadata", "exit_code", "connection_subtype",
	"connection_tags", "session_batch_id", "ai_analysis", "guardrails_info",
	"correlation_id", "machine_identity_id", "identity_type", "origin",
}

// psqlRow returns the 28 cell values for row r, with "" marking the cell the
// caller fills with the planted name.
func psqlRow(r int, handle string) []string {
	uuid := func(seed int) string {
		return fmt.Sprintf("%08x-%04x-4%03x-8%03x-%012x",
			0x8aa7007e+seed*7919, (0x18e9+seed*31)&0xffff, (0x4f9+seed*13)&0xfff,
			(0xa84+seed*17)&0xfff, 0x8c237351fd68+seed*104729)
	}
	mail := handle + "@example.com"
	return []string{
		uuid(r*10 + 1), uuid(r*10 + 2), "postgres-demo", "database", "exec", "",
		mail, "", mail, "done", uuid(r*10 + 3), uuid(r*10 + 4), "{}",
		fmt.Sprintf("2026-07-29 15:%02d:09.93009", 19+r),
		fmt.Sprintf("2026-07-29 15:%02d:12.072462", 19+r),
		`{"truncated": false, "event_size": 342871, "data_masking": {"err_count": 0, ` +
			`"info_types": {}, "transformed_bytes": 0, "total_redact_count": 0}}`,
		"", "", "0", "postgres", "{}", "", "", "", "", "", "user", "webapp",
	}
}

// psqlFixture renders nCols columns of that capture with one name per row in
// the user_name cell. handles supplies the local part of the surrounding email
// addresses, so the name keeps its real neighbours.
//
// It returns the text, the byte range of each planted name, and the byte
// ranges of the email cells. Those hold the name's own handle, so the model
// tags parts of them too — "ose" inside "jose.nunez@example.com" — and an
// offset assertion has to allow for that without allowing a span that landed
// on a UUID or a timestamp.
func psqlFixture(nCols int, names, handles []string) (text string, planted, mails [][2]int) {
	head := append([]string(nil), psqlColumns[:min(nCols, len(psqlColumns))]...)
	nameCol := 7 // user_name
	if nameCol >= len(head) {
		nameCol = len(head) / 2
		head[nameCol] = "user_name"
	}

	var b strings.Builder
	b.WriteString(strings.Join(head, "\t") + "\n")
	for r, name := range names {
		mail := handles[r%len(handles)] + "@example.com"
		cells := psqlRow(r, handles[r%len(handles)])
		for c := range head {
			if c > 0 {
				b.WriteString("\t")
			}
			if c == nameCol {
				planted = append(planted, [2]int{b.Len(), b.Len() + len(name)})
				b.WriteString(name)
				continue
			}
			if cells[c] == mail {
				mails = append(mails, [2]int{b.Len(), b.Len() + len(mail)})
			}
			b.WriteString(cells[c])
		}
		b.WriteString("\n")
	}
	b.WriteString(fmt.Sprintf("(%d rows)\n", len(names)))
	return b.String(), planted, mails
}

// analyzeWith runs one text through an engine configured with the given
// segmentation and returns its spans.
func analyzeWith(t *testing.T, mode, text string) []analyzer.NerSpan {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Backend = os.Getenv("ALCATRAZ_NER_BACKEND")
	cfg.ORTLibraryPath = os.Getenv("ALCATRAZ_NER_ORT_LIB")
	cfg.Accelerator = os.Getenv("ALCATRAZ_NER_ACCELERATOR")
	cfg.Segmentation = mode

	nlp, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New(%q): %v", mode, err)
	}
	defer nlp.Close()

	arts, err := nlp.ProcessTexts([]string{text}, "en")
	if err != nil {
		t.Fatalf("ProcessTexts(%q): %v", mode, err)
	}
	return arts[0].Ents
}

// found counts how many planted ranges a PERSON span overlaps.
func found(spans []analyzer.NerSpan, planted [][2]int) int {
	n := 0
	for _, p := range planted {
		for _, s := range spans {
			if s.EntityType == entities.Person && s.Start < p[1] && p[0] < s.End {
				n++
				break
			}
		}
	}
	return n
}

// TestLiveSegmentationTabular is the ATR-205 regression: names that the model
// finds easily on their own disappear when a wide tab-separated row is fed in
// as one sequence, and segmentation recovers them.
func TestLiveSegmentationTabular(t *testing.T) {
	if os.Getenv("ALCATRAZ_NER_LIVE") != "1" {
		t.Skip("set ALCATRAZ_NER_LIVE=1 to run the live model test")
	}
	names := []string{"Luan", "Luan", "Luan"}
	text, planted, _ := psqlFixture(28, names, []string{"luan"})

	whole := found(analyzeWith(t, SegmentWhole, text), planted)
	lines := found(analyzeWith(t, SegmentLines, text), planted)
	fields := found(analyzeWith(t, SegmentFields, text), planted)
	t.Logf("%d bytes, %d planted names: whole=%d lines=%d fields=%d",
		len(text), len(planted), whole, lines, fields)

	if lines != len(planted) {
		t.Errorf("SegmentLines found %d/%d planted names", lines, len(planted))
	}
	if fields != len(planted) {
		t.Errorf("SegmentFields found %d/%d planted names", fields, len(planted))
	}
	// The defect itself. If the model ever stops losing these names as one
	// blob this fails, which is the right prompt to re-measure the tradeoff
	// documented on Config.Segmentation rather than a silent pass.
	if whole >= lines {
		t.Errorf("SegmentWhole found %d/%d, no worse than SegmentLines' %d — "+
			"the recall gap this option exists to close is gone; re-measure",
			whole, len(planted), lines)
	}
}

// TestLiveSegmentationOffsets pins the byte-offset guarantee under every
// segmentation: spans are rebased window → segment → folded → original, and
// multi-byte input exercises the fold remap on top of the segment offset.
func TestLiveSegmentationOffsets(t *testing.T) {
	if os.Getenv("ALCATRAZ_NER_LIVE") != "1" {
		t.Skip("set ALCATRAZ_NER_LIVE=1 to run the live model test")
	}
	text, planted, mails := psqlFixture(28,
		[]string{"José Núñez", "Wojciech Nowak", "Hiroshi Tanaka"},
		[]string{"jose.nunez", "wojciech.nowak", "hiroshi.tanaka"})
	// The only cells holding anything person-shaped. Everything else in the
	// row is a UUID, a timestamp, a JSON blob or a keyword.
	personCells := append(append([][2]int(nil), planted...), mails...)

	for _, mode := range []string{SegmentWhole, SegmentLines, SegmentFields} {
		t.Run(mode, func(t *testing.T) {
			spans := analyzeWith(t, mode, text)
			for _, s := range spans {
				if s.Start < 0 || s.End > len(text) || s.Start >= s.End {
					t.Fatalf("span [%d:%d) out of range for %d bytes", s.Start, s.End, len(text))
				}
				if !utf8.ValidString(text[s.Start:s.End]) {
					t.Errorf("span [%d:%d) = %q cuts a rune", s.Start, s.End, text[s.Start:s.End])
				}
			}
			// Every PERSON span must land inside a name cell or one of the
			// email cells built from that name. A rebase off by a segment
			// start would still produce in-range, rune-aligned offsets while
			// pointing at a UUID, so this — not the range check — is what
			// catches a bad segment offset.
			for _, s := range spans {
				if s.EntityType != entities.Person {
					continue
				}
				hit := false
				for _, p := range personCells {
					if s.Start >= p[0] && s.End <= p[1] {
						hit = true
						break
					}
				}
				if !hit {
					t.Errorf("PERSON %q at [%d:%d) is outside every name and email cell",
						text[s.Start:s.End], s.Start, s.End)
				}
			}
			if n := found(spans, planted); n == 0 {
				t.Errorf("no planted name found at all under %q", mode)
			}
			t.Logf("%d spans, %d/%d planted names, offsets hold",
				len(spans), found(spans, planted), len(planted))
		})
	}
}

// TestLiveSegmentationProse guards the reason SegmentWhole stays the default:
// segmentation must not be a free win that everyone should take. On prose,
// cutting at newlines changes nothing, and cutting further can only remove
// context the model needs.
func TestLiveSegmentationProse(t *testing.T) {
	if os.Getenv("ALCATRAZ_NER_LIVE") != "1" {
		t.Skip("set ALCATRAZ_NER_LIVE=1 to run the live model test")
	}
	const prose = "Alice Johnson met Bob Miller in Paris last spring.\n" +
		"They were joined by Carol Davis, who had flown in from Berlin.\n"

	render := func(spans []analyzer.NerSpan) []string {
		var out []string
		for _, s := range spans {
			out = append(out, fmt.Sprintf("%s:%q", s.EntityType, prose[s.Start:s.End]))
		}
		return out
	}
	whole := render(analyzeWith(t, SegmentWhole, prose))
	lines := render(analyzeWith(t, SegmentLines, prose))
	t.Logf("whole=%v", whole)
	t.Logf("lines=%v", lines)

	if strings.Join(whole, " ") != strings.Join(lines, " ") {
		t.Errorf("prose spans differ between whole and lines:\n whole = %v\n lines = %v", whole, lines)
	}
	for _, want := range []string{`PERSON:"Alice Johnson"`, `PERSON:"Bob Miller"`, `LOCATION:"Paris"`} {
		if !strings.Contains(strings.Join(whole, " "), want) {
			t.Errorf("prose is missing %s (got %v)", want, whole)
		}
	}
}
