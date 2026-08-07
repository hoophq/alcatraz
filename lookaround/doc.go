// Package lookaround provides an alcatraz Matcher backed by a backtracking
// regex engine (github.com/dlclark/regexp2). It enables patterns that use
// lookahead and lookbehind, (?=…), (?!…), (?<=…), (?<!…), plus
// backreferences, which the standard-library RE2 engine that powers
// alcatraz's core does not support.
//
// It lives in a separate module on purpose: importing it is the only way to
// pull in regexp2, so alcatraz's core stays dependency-free and linear-time.
// Use it for user-configured rules that require lookaround; prefer the
// core (anchors + validators, or a capture group via Pattern.WithGroup) when
// you can, because backtracking does not have RE2's linear-time guarantee.
//
// # Turning config rules into a recognizer
//
// [NewRecognizer] plus [Spec] is the one-call path from config-file regex
// rules to an [analyzer.PatternRecognizer] that plugs into the standard
// engine, with no matcher plumbing in between:
//
//	rec, err := lookaround.NewRecognizer("SecretRule", "API_SECRET", "en",
//		lookaround.Spec{Name: "bearer", Regex: `(?<=Bearer )[A-Za-z0-9._-]{8,}`, Score: 0.95},
//		lookaround.Spec{Name: "domain", Regex: `(?<=@)(\w+)\.com`, Score: 0.6, Group: 1},
//	)
//	// handle err
//	reg := analyzer.NewRegistry("en")
//	reg.Add("en", rec)
//	results := analyzer.NewEngine(reg, []string{"en"}).Analyze(text, analyzer.Options{})
//
// The [Spec] Group field selects which capture group becomes the reported
// span; 0 (the whole match) is the default. In the "domain" rule above,
// group 1 reports only "acme" out of "@acme.com".
//
// Reported offsets are byte offsets into the analyzed string, matching the
// rest of alcatraz, even though regexp2 works in rune space internally;
// [Matcher.FindAll] converts. A group that did not participate in a match
// reports the span (-1, -1).
//
// # Bounding backtracking
//
// To bound catastrophic backtracking (ReDoS), every compiled matcher carries a
// MatchTimeout ([DefaultTimeout] unless overridden via [CompileWithTimeout]).
// On timeout the affected match is abandoned rather than allowed to run
// unbounded: [Matcher.FindAll] returns the matches gathered so far.
//
// See also alcatraz/pfilter and alcatraz/ner, the other optional modules that
// keep a heavyweight dependency out of the core.
package lookaround
