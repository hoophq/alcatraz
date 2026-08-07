// Package alcatraz is a pure-Go, dependency-free, pattern-based PII
// detection library.
//
// You import it and invoke it in-process: no service, no network.
// Build an engine once with [NewEngine], then call its Analyze method
// ([github.com/hoophq/alcatraz/analyzer.Engine.Analyze]) on every
// text you want scanned:
//
//	eng := alcatraz.NewEngine()
//	for _, hit := range eng.Analyze("email me at jane@example.com", alcatraz.Options{}) {
//		fmt.Println(hit.EntityType, hit.Text, hit.Score)
//	}
//
// [NewEngine] loads the full built-in recognizer set for the languages you
// pass, defaulting to English. You configure an engine once and reuse it;
// the per-call knobs live in [Options]:
//
//   - Options.Entities restricts detection to the listed entity types, so
//     recognizers for everything else never run.
//   - Options.Threshold drops results scoring below it, trading recall for
//     precision.
//   - Options.AllowList suppresses matches whose text is known-safe, as exact
//     strings or, with Options.AllowListRegex, as regular expressions.
//   - Options.Language selects which language's recognizer set to use.
//
// Analyze returns []Result sorted by score (descending), then start offset
// (ascending), then span length (descending). Result offsets are byte indices
// into the text you passed, not rune indices, so text[r.Start:r.End] == r.Text
// holds even when the text contains multi-byte characters. Slice the original
// text with them; do not convert to []rune first.
//
// Detection in this package is pattern-based (regular expressions plus
// checksum/format validators), which is what keeps the core stdlib-only.
//
// # Related packages
//
// Within this module:
//
//   - [github.com/hoophq/alcatraz/entities] holds the canonical entity-type
//     name constants, e.g. entities.CreditCard, to use in Options.Entities
//     and when switching on Result.EntityType.
//   - [github.com/hoophq/alcatraz/anonymizer] turns the []Result from
//     Analyze into a sanitized string (mask, redact, hash, replace).
//   - [github.com/hoophq/alcatraz/analyzer] is the detection framework
//     itself: the Recognizer contract, pattern recognizers, the registry and
//     the engine. Use it to build an engine from a hand-picked recognizer
//     set or to add your own recognizers. The types this package re-exports
//     ([Engine], [Result], [Options], [Recognizer], [Registry]) are aliases
//     for the analyzer ones.
//
// Optional, in separate modules so the core stays dependency-free. Go builds
// each one into your binary only if you import it:
//
//   - github.com/hoophq/alcatraz/ner adds free-text entities that require a
//     statistical model (PERSON, LOCATION, NRP), plugged in through the
//     Recognizer and NlpEngine interfaces in the analyzer package.
//   - github.com/hoophq/alcatraz/pfilter is an FFI layer over the
//     privacy-filter.cpp shared library.
//   - github.com/hoophq/alcatraz/lookaround supplies recognizers needing the
//     backtracking regexp2 engine.
package alcatraz
