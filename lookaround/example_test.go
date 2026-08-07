package lookaround_test

import (
	"fmt"
	"strings"
	"time"

	"github.com/hoophq/alcatraz/analyzer"
	"github.com/hoophq/alcatraz/lookaround"
)

// ExampleNewRecognizer turns one config-file regex rule into a recognizer the
// standard engine can run. The lookbehind `(?<=Bearer )` anchors on the
// prefix without consuming it, so the reported span is the token alone.
// RE2 cannot express that, which is the whole reason this module exists.
func ExampleNewRecognizer() {
	rec, err := lookaround.NewRecognizer("SecretRule", "API_SECRET", "en",
		lookaround.Spec{Name: "bearer", Regex: `(?<=Bearer )[A-Za-z0-9._-]{8,}`, Score: 0.95},
	)
	if err != nil {
		panic(err)
	}

	reg := analyzer.NewRegistry("en")
	reg.Add("en", rec)
	eng := analyzer.NewEngine(reg, []string{"en"})

	for _, hit := range eng.Analyze("Authorization: Bearer abc123.def456", analyzer.Options{}) {
		fmt.Printf("%s %q %.2f (bytes %d..%d)\n", hit.EntityType, hit.Text, hit.Score, hit.Start, hit.End)
	}
	// Output:
	// API_SECRET "abc123.def456" 0.95 (bytes 22..35)
}

// ExampleSpec_group reports a capture group instead of the whole match.
// Group 1 of `(?<=@)(\w+)\.com` is the domain label: the lookbehind drops the
// "@", the group drops the ".com", leaving just "acme".
func ExampleSpec_group() {
	rec, err := lookaround.NewRecognizer("DomainRule", "DOMAIN", "en",
		lookaround.Spec{Name: "domain", Regex: `(?<=@)(\w+)\.com`, Score: 0.6, Group: 1},
	)
	if err != nil {
		panic(err)
	}

	reg := analyzer.NewRegistry("en")
	reg.Add("en", rec)
	eng := analyzer.NewEngine(reg, []string{"en"})

	for _, hit := range eng.Analyze("mail bob@acme.com or sue@globex.com", analyzer.Options{}) {
		fmt.Printf("%s %q\n", hit.EntityType, hit.Text)
	}
	// Output:
	// DOMAIN "acme"
	// DOMAIN "globex"
}

// ExampleCompileWithTimeout bounds catastrophic backtracking. `(a+)+$` against
// a long run of "a" ending in a non-match is the classic ReDoS shape: without
// a cap it runs for exponential time. With one, the matcher abandons the
// attempt and FindAll returns whatever it had gathered, here nothing.
func ExampleCompileWithTimeout() {
	m, err := lookaround.CompileWithTimeout(`(a+)+$`, 50*time.Millisecond)
	if err != nil {
		panic(err)
	}

	start := time.Now()
	matches := m.FindAll(strings.Repeat("a", 40) + "!")
	fmt.Println(len(matches), time.Since(start) < 5*time.Second)
	// Output:
	// 0 true
}
