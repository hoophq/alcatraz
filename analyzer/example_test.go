package analyzer_test

import (
	"fmt"
	"strings"

	"github.com/hoophq/alcatraz/analyzer"
)

// A custom entity type only needs a regex, a score and a name: wrap them in a
// PatternRecognizer, register it under a language and hand the registry to an
// engine.
func ExampleNewPatternRecognizer() {
	rec := analyzer.NewPatternRecognizer(
		"InternalIDRecognizer", "INTERNAL_ID", "en",
		[]*analyzer.Pattern{analyzer.MustPattern("internal-id", `\bEMP-\d{6}\b`, 0.9)},
	)

	reg := analyzer.NewRegistry("en")
	reg.Add("en", rec)
	eng := analyzer.NewEngine(reg, []string{"en"})

	for _, r := range eng.Analyze("ticket filed by EMP-004217 last friday", analyzer.Options{}) {
		fmt.Printf("%s %q score=%.2f by=%s\n", r.EntityType, r.Text, r.Score, r.RecognizerName)
	}

	// Output:
	// INTERNAL_ID "EMP-004217" score=0.90 by=InternalIDRecognizer
}

// Nine digits are nine digits; the words around them carry the evidence a
// regex cannot. WithContext declares the labels a value is usually written
// under, and the engine's default enhancer turns them into score.
func ExamplePatternRecognizer_WithContext() {
	rec := analyzer.NewPatternRecognizer(
		"SsnRecognizer", "US_SSN", "en",
		[]*analyzer.Pattern{analyzer.MustPattern("ssn", `\b\d{3}-\d{2}-\d{4}\b`, 0.3)},
	).WithContext("ssn", "social security")

	reg := analyzer.NewRegistry("en")
	reg.Add("en", rec)
	eng := analyzer.NewEngine(reg, []string{"en"})

	for _, text := range []string{
		"the number is 078-05-1120",
		"social security number 078-05-1120",
	} {
		for _, r := range eng.Analyze(text, analyzer.Options{}) {
			fmt.Printf("%.2f  %s\n", r.Score, text)
		}
	}

	// The bare number keeps the pattern's weak base score; the labelled one is
	// boosted by DefaultContextBoost (0.35).

	// Output:
	// 0.30  the number is 078-05-1120
	// 0.65  social security number 078-05-1120
}

// RE2 has no lookbehind. WithGroup is the idiomatic replacement: match the
// surrounding context, then report only the captured span as the entity.
func ExamplePattern_WithGroup() {
	whole := analyzer.MustPattern("user", `user=(\w+)`, 0.6)
	captured := analyzer.MustPattern("user", `user=(\w+)`, 0.6).WithGroup(1)

	text := "login user=jdoe from 10.0.0.4"
	for _, p := range []*analyzer.Pattern{whole, captured} {
		rec := analyzer.NewPatternRecognizer(
			"UsernameRecognizer", "USERNAME", "en", []*analyzer.Pattern{p},
		)
		for _, r := range rec.Analyze(text, nil) {
			fmt.Printf("group=%d [%d:%d] %q\n", p.Group, r.Start, r.End, text[r.Start:r.End])
		}
	}

	// Output:
	// group=0 [6:15] "user=jdoe"
	// group=1 [11:15] "jdoe"
}

// A context validator sees the full text and the match span, which is enough
// to express the lookbehind RE2 lacks: here, four digits count as a PIN only
// when "PIN " sits immediately in front of them.
func ExamplePatternRecognizer_WithContextValidator() {
	rec := analyzer.NewPatternRecognizer(
		"PinRecognizer", "PIN", "en",
		[]*analyzer.Pattern{analyzer.MustPattern("pin", `\d{4}`, 0.5)},
	).WithContextValidator(func(text string, start, end int) bool {
		return strings.HasSuffix(text[:start], "PIN ") // emulates (?<=PIN )
	})

	for _, text := range []string{"PIN 4291", "order 4291"} {
		results := rec.Analyze(text, nil)
		fmt.Printf("%-12q %d result(s)\n", text, len(results))
	}

	// Output:
	// "PIN 4291"   1 result(s)
	// "order 4291" 0 result(s)
}

// The default enhancer reads five words before a match and none after,
// following Presidio: a label almost always precedes its value. Widen
// WordsAfter when yours trails instead, or pass nil to SetContextEnhancer to
// score on the pattern alone.
func ExampleNewWordContextEnhancer() {
	newEngine := func() *analyzer.Engine {
		rec := analyzer.NewPatternRecognizer(
			"SsnRecognizer", "US_SSN", "en",
			[]*analyzer.Pattern{analyzer.MustPattern("ssn", `\b\d{3}-\d{2}-\d{4}\b`, 0.3)},
		).WithContext("ssn")
		reg := analyzer.NewRegistry("en")
		reg.Add("en", rec)
		return analyzer.NewEngine(reg, []string{"en"})
	}

	// The label trails the value, so the default window misses it.
	text := "078-05-1120 is the ssn on file"

	report := func(label string, eng *analyzer.Engine) {
		for _, r := range eng.Analyze(text, analyzer.Options{}) {
			fmt.Printf("%-12s %.2f\n", label, r.Score)
		}
	}

	report("default", newEngine())

	widened := analyzer.NewWordContextEnhancer()
	widened.WordsAfter = 3
	eng := newEngine()
	eng.SetContextEnhancer(widened)
	report("WordsAfter=3", eng)

	off := newEngine()
	off.SetContextEnhancer(nil)
	report("disabled", off)

	// Output:
	// default      0.30
	// WordsAfter=3 0.65
	// disabled     0.30
}
