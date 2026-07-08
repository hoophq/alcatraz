package anonymizer_test

import (
	"testing"

	"github.com/hoophq/alcatraz"
	"github.com/hoophq/alcatraz/analyzer"
	"github.com/hoophq/alcatraz/anonymizer"
	"github.com/hoophq/alcatraz/entities"
)

func span(entity string, start, end int, score float64) analyzer.Result {
	return analyzer.Result{EntityType: entity, Start: start, End: end, Score: score}
}

func TestOperators(t *testing.T) {
	text := "ssn 536-90-4399 ok"
	ssn := []analyzer.Result{span(entities.USSSN, 4, 15, 1.0)}

	cases := []struct {
		name string
		op   anonymizer.Operator
		want string
	}{
		{"Mask hash", anonymizer.Mask('#'), "ssn ########### ok"},
		{"Mask star", anonymizer.Mask('*'), "ssn *********** ok"},
		{"MaskKeepLast", anonymizer.MaskKeepLast('*', 4), "ssn *******4399 ok"},
		{"Replace", anonymizer.Replace(), "ssn <US_SSN> ok"},
		{"ReplaceWith", anonymizer.ReplaceWith("[PII]"), "ssn [PII] ok"},
		{"Redact", anonymizer.Redact(), "ssn  ok"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := anonymizer.Anonymize(text, ssn, c.op); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestMaskKeepLastShortMatch(t *testing.T) {
	text := "pin 123"
	res := []analyzer.Result{span("PIN", 4, 7, 1.0)}
	if got := anonymizer.Anonymize(text, res, anonymizer.MaskKeepLast('*', 4)); got != "pin 123" {
		t.Errorf("match shorter than keep should be unchanged, got %q", got)
	}
}

func TestMaskMultiByte(t *testing.T) {
	text := "name José Núñez end"
	res := []analyzer.Result{span(entities.Person, 5, 5+len("José Núñez"), 0.9)}
	// 10 runes, so 10 mask characters regardless of byte length.
	want := "name ########## end"
	if got := anonymizer.Anonymize(text, res, anonymizer.Mask('#')); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMultipleSpansReplacedIndependently(t *testing.T) {
	text := "a@b.com and c@d.com"
	res := []analyzer.Result{
		span(entities.EmailAddress, 0, 7, 0.5),
		span(entities.EmailAddress, 12, 19, 0.5),
	}
	want := "<EMAIL_ADDRESS> and <EMAIL_ADDRESS>"
	if got := anonymizer.Anonymize(text, res, anonymizer.Replace()); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPerEntityConfig(t *testing.T) {
	text := "card 4532015112830366 mail a@b.com"
	res := []analyzer.Result{
		span(entities.CreditCard, 5, 21, 1.0),
		span(entities.EmailAddress, 27, 34, 0.5),
	}
	got := anonymizer.AnonymizeWith(text, res, anonymizer.Config{
		Default: anonymizer.Mask('*'),
		PerEntity: map[string]anonymizer.Operator{
			entities.CreditCard: anonymizer.MaskKeepLast('*', 4),
		},
	})
	want := "card ************0366 mail *******"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDefaultOperatorIsReplace(t *testing.T) {
	text := "mail a@b.com"
	res := []analyzer.Result{span(entities.EmailAddress, 5, 12, 0.5)}
	if got := anonymizer.AnonymizeWith(text, res, anonymizer.Config{}); got != "mail <EMAIL_ADDRESS>" {
		t.Errorf("got %q", got)
	}
}

func TestOverlapHigherScoreWinsLowerTrimmed(t *testing.T) {
	// PERSON (low score) overlaps EMAIL (high score): the email keeps its
	// full span, the person span is trimmed to its uncovered prefix, and
	// no detected byte survives unmasked.
	text := "by jane jane@x.com!"
	res := []analyzer.Result{
		span(entities.Person, 3, 12, 0.4),        // "jane jane"
		span(entities.EmailAddress, 8, 18, 0.95), // "jane@x.com"
	}
	got := anonymizer.Anonymize(text, res, anonymizer.Mask('#'))
	want := "by ###############!" // 5 (trimmed "jane ") + 10 (email), one '#' each
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestContainedSpanFullyCovered(t *testing.T) {
	// A span contained in a higher-scoring one disappears into it.
	text := "x 4532015112830366 y"
	res := []analyzer.Result{
		span(entities.CreditCard, 2, 18, 1.0),
		span(entities.USBankNumber, 2, 18, 0.05),
	}
	got := anonymizer.Anonymize(text, res, anonymizer.Replace())
	want := "x <CREDIT_CARD> y"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestOutOfRangeSpansClamped(t *testing.T) {
	text := "short"
	res := []analyzer.Result{
		span("X", -2, 3, 0.9),
		span("Y", 4, 99, 0.9),
		span("Z", 5, 5, 0.9), // empty after clamp: dropped
	}
	got := anonymizer.Anonymize(text, res, anonymizer.Mask('*'))
	want := "***r*" // X clamps to [0,3), Y to [4,5); byte 3 ('r') is untouched
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNoResultsReturnsTextUnchanged(t *testing.T) {
	if got := anonymizer.Anonymize("nothing here", nil, anonymizer.Mask('#')); got != "nothing here" {
		t.Errorf("got %q", got)
	}
}

func TestEndToEndWithEngine(t *testing.T) {
	eng := alcatraz.NewEngine()
	text := "Email jane.doe@example.com, card 4532015112830366."
	results := eng.Analyze(text, alcatraz.Options{
		Entities: []string{entities.EmailAddress, entities.CreditCard},
	})
	got := anonymizer.AnonymizeWith(text, results, anonymizer.Config{
		Default: anonymizer.Replace(),
		PerEntity: map[string]anonymizer.Operator{
			entities.CreditCard: anonymizer.MaskKeepLast('#', 4),
		},
	})
	want := "Email <EMAIL_ADDRESS>, card ############0366."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
