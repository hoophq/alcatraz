package analyzer_test

import (
	"testing"

	"github.com/hoophq/alcatraz/analyzer"
	"github.com/hoophq/alcatraz/recognizers"
)

// The windowed scan must be invisible: for every built-in pattern it
// serves and every corpus document, the matches it reports — count, order
// and every group span — are the ones the regex finds over the whole text.
// Documents up to 16 KB: the 10 KB ones already push the full-text side
// onto the stdlib NFA while the windows stay on its backtracker, and the
// 1 MB ones only add two minutes under -race; the parity run in bench/
// covers those end to end.
func TestWindowedScanMatchesFullScanOnCorpus(t *testing.T) {
	docs := loadCorpusTexts(t, 16<<10)
	windowed, checked, matched := 0, 0, 0
	for _, rec := range recognizers.All() {
		pr, ok := rec.(*analyzer.PatternRecognizer)
		if !ok {
			continue
		}
		for _, p := range pr.Patterns() {
			if _, ok := p.FindAllWindowed(""); !ok {
				continue
			}
			windowed++
			for _, text := range docs {
				want := p.FindAll(text)
				got, _ := p.FindAllWindowed(text)
				checked++
				if len(want) > 0 {
					matched++
				}
				if len(got) != len(want) {
					t.Errorf("%s/%s: windowed scan found %d matches, full scan %d", rec.Name(), p.Name, len(got), len(want))
					continue
				}
				for i := range want {
					if got[i].NumGroups() != want[i].NumGroups() {
						t.Errorf("%s/%s: match %d has %d groups windowed, %d full", rec.Name(), p.Name, i, got[i].NumGroups(), want[i].NumGroups())
						break
					}
					for g := range want[i].NumGroups() {
						gs, ge := got[i].Span(g)
						ws, we := want[i].Span(g)
						if gs != ws || ge != we {
							t.Errorf("%s/%s: match %d group %d = [%d,%d) windowed, [%d,%d) full", rec.Name(), p.Name, i, g, gs, ge, ws, we)
						}
					}
				}
			}
		}
	}
	if windowed == 0 || matched == 0 {
		t.Fatalf("%d windowed patterns, %d pattern/doc pairs with matches; corpus unusable", windowed, matched)
	}
	t.Logf("%d windowed patterns, %d pattern/doc checks, %d with matches", windowed, checked, matched)
}
