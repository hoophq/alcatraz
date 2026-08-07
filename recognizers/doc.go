// Package recognizers provides the built-in alcatraz entity recognizers and a
// loader that registers them by language. Each recognizer is a
// [analyzer.PatternRecognizer]: one or more regexes plus, for structured
// entities, a checksum/format validator.
//
// # Loading
//
// [LoadDefaults] registers the full built-in set (45 recognizers, each
// emitting one entity type from [entities]) under a language:
//
//	reg := analyzer.NewRegistry("en")
//	recognizers.LoadDefaults(reg, "en")
//	eng := analyzer.NewEngine(reg, []string{"en"})
//	results := eng.Analyze(text, analyzer.Options{})
//
// [All] returns the same set as a slice, for a caller that wants to register
// only part of it or to wrap each recognizer before adding it. Every call
// builds fresh recognizers; nothing is shared between registries.
//
// Structured identifiers are language-independent: a Thai national ID or
// an IBAN looks the same in any surrounding text. [LoadDefaults] therefore
// registers the whole set under whichever language you request, and a
// default English engine detects all of them; its doc comment explains the
// reasoning.
//
// # Layout
//
// Recognizers are grouped by country, one file each: generic.go for the
// language-independent ones (email, phone, credit card, crypto wallet, IP,
// URL, date/time, IBAN), then us.go, uk.go, au.go, india.go, italy.go,
// spain.go, singapore.go, brazil.go, and others.go for the remaining
// single-country identifiers (Polish PESEL, Korean RRN, Finnish personal
// code, Thai TNIN). Each constructor is exported and usable on its own:
//
//	reg.Add("en", recognizers.CreditCard())
//
// # Checksums
//
// checksum.go holds the shared digit helpers and the checksum algorithms the
// validators are built from: Luhn (mod-10, credit cards) and Verhoeff (Indian
// Aadhaar). ISO 7064 mod-97 lives with the IBAN recognizer in generic.go, and
// the Brazilian weighted mod-11 with CPF, CNPJ and PIS in brazil.go.
//
// A validator is a filter with teeth. [analyzer.PatternRecognizer] scores a
// raw regex match at the pattern's base score, then hands the matched text
// to the validator: a passing checksum promotes the result to
// [analyzer.MaxScore], a failing one drops the result. [CreditCard]'s weak
// 0.3 pattern therefore reports a Luhn-valid number at 1.0 and does not
// report a Luhn-invalid one at all: the regex proposes, the checksum
// decides. Recognizers for identifiers with no verifiable structure keep
// their pattern score and lean on the engine's context enhancer instead
// (see [analyzer.PatternRecognizer.WithContext]).
package recognizers
