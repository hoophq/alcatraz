package recognizers_test

import (
	"fmt"
	"sort"
	"strings"

	"github.com/hoophq/alcatraz/analyzer"
	"github.com/hoophq/alcatraz/entities"
	"github.com/hoophq/alcatraz/recognizers"
)

// byPosition orders results by their offset in the analyzed text. The engine
// returns them ranked by score, which is what a reviewer wants; reading order
// is what an example wants.
func byPosition(results []analyzer.Result) []analyzer.Result {
	sort.Slice(results, func(i, j int) bool { return results[i].Start < results[j].Start })
	return results
}

// LoadDefaults registers every built-in recognizer under a language, so one
// engine detects identifiers from every country alcatraz knows.
func ExampleLoadDefaults() {
	reg := analyzer.NewRegistry("en")
	recognizers.LoadDefaults(reg, "en")
	eng := analyzer.NewEngine(reg, []string{"en"})

	text := "Email bob@example.com or call (11) 91234-5678; card 4111 1111 1111 1111; CPF 529.982.247-25."
	for _, r := range byPosition(eng.Analyze(text, analyzer.Options{})) {
		fmt.Printf("%-14s %-21q %.2f\n", r.EntityType, r.Text, r.Score)
	}
	// Output:
	// EMAIL_ADDRESS  "bob@example.com"     0.85
	// PHONE_NUMBER   "(11) 91234-5678"     0.50
	// CREDIT_CARD    "4111 1111 1111 1111" 1.00
	// BR_CPF         "529.982.247-25"      1.00
}

// A score threshold is the dial between recall and precision. Checksum-backed
// entities always score 1.00, so raising the threshold above a pattern-only
// recognizer's score keeps the verified detections and drops the guesses.
func ExampleLoadDefaults_threshold() {
	reg := analyzer.NewRegistry("en")
	recognizers.LoadDefaults(reg, "en")
	eng := analyzer.NewEngine(reg, []string{"en"})

	text := "Email bob@example.com or call (11) 91234-5678; card 4111 1111 1111 1111; CPF 529.982.247-25."
	threshold := 0.6
	for _, r := range byPosition(eng.Analyze(text, analyzer.Options{Threshold: &threshold})) {
		fmt.Printf("%-14s %q\n", r.EntityType, r.Text)
	}
	// Output:
	// EMAIL_ADDRESS  "bob@example.com"
	// CREDIT_CARD    "4111 1111 1111 1111"
	// BR_CPF         "529.982.247-25"
}

// All returns the whole built-in set, which is the hook for registering only
// part of it: filter on the entity types each recognizer supports instead of
// naming constructors by hand. A Brazilian-only engine skips the work of the
// other 40 recognizers on every call.
func ExampleAll() {
	reg := analyzer.NewRegistry("pt")
	for _, rec := range recognizers.All() {
		for _, e := range rec.SupportedEntities() {
			if strings.HasPrefix(e, "BR_") || e == entities.EmailAddress {
				reg.Add("pt", rec)
			}
		}
	}

	fmt.Println(len(recognizers.All()), "built-ins,", len(reg.Recognizers("pt", nil)), "registered")
	fmt.Println(reg.SupportedEntities("pt"))
	// Output:
	// 52 built-ins, 13 registered
	// [BR_CEP BR_CNH BR_CNPJ BR_CNS BR_CPF BR_PIS BR_PIX_KEY BR_PLACA BR_RENAVAM BR_RG BR_TITULO_ELEITORAL EMAIL_ADDRESS]
}

// CreditCard pairs a deliberately weak 0.3 regex with the Luhn checksum. A
// number that passes is promoted to 1.00; a number that fails is not reported
// at all, even though the regex matched it.
func ExampleCreditCard() {
	reg := analyzer.NewRegistry("en")
	reg.Add("en", recognizers.CreditCard())
	eng := analyzer.NewEngine(reg, []string{"en"})

	for _, text := range []string{"4111 1111 1111 1111", "4111 1111 1111 1112"} {
		results := eng.Analyze(text, analyzer.Options{Entities: []string{entities.CreditCard}})
		if len(results) == 0 {
			fmt.Printf("%s -> no match (Luhn failed)\n", text)
			continue
		}
		fmt.Printf("%s -> %s %.2f\n", text, results[0].EntityType, results[0].Score)
	}
	// Output:
	// 4111 1111 1111 1111 -> CREDIT_CARD 1.00
	// 4111 1111 1111 1112 -> no match (Luhn failed)
}

// The same promote-or-drop rule runs on three other checksum families: ISO
// 7064 mod-97 for IBAN, Verhoeff for Indian Aadhaar, and the Brazilian
// weighted mod-11 for CPF. Flipping one digit of each removes the detection.
func ExampleIBAN() {
	reg := analyzer.NewRegistry("en")
	reg.Add("en", recognizers.IBAN())      // ISO 7064 mod-97
	reg.Add("en", recognizers.INAadhaar()) // Verhoeff
	reg.Add("en", recognizers.BRCPF())     // Brazilian mod-11
	eng := analyzer.NewEngine(reg, []string{"en"})

	for _, text := range []string{
		"DE89 3704 0044 0532 0130 00", "DE89 3704 0044 0532 0130 01",
		"2341 2341 2346", "2341 2341 2347",
		"529.982.247-25", "529.982.247-26",
	} {
		results := eng.Analyze(text, analyzer.Options{})
		if len(results) == 0 {
			fmt.Printf("%-27s rejected\n", text)
			continue
		}
		fmt.Printf("%-27s %s %.2f\n", text, results[0].EntityType, results[0].Score)
	}
	// Output:
	// DE89 3704 0044 0532 0130 00 IBAN_CODE 1.00
	// DE89 3704 0044 0532 0130 01 rejected
	// 2341 2341 2346              IN_AADHAAR 1.00
	// 2341 2341 2347              rejected
	// 529.982.247-25              BR_CPF 1.00
	// 529.982.247-26              rejected
}

// A built-in set is a starting point, not a fence: your own
// [analyzer.PatternRecognizer] goes into the same registry, with the same
// validator contract, and the engine reconciles it against the built-ins.
func ExampleLoadDefaults_customRecognizer() {
	reg := analyzer.NewRegistry("en")
	recognizers.LoadDefaults(reg, "en")

	// Internal ticket ids: ACME-<7 digits>, valid only when the digits sum
	// to a multiple of 9.
	ticket := analyzer.NewPatternRecognizer(
		"AcmeTicketRecognizer", "ACME_TICKET", "en",
		[]*analyzer.Pattern{analyzer.MustPattern("acme ticket", `\bACME-\d{7}\b`, 0.4)},
	).WithValidator(func(m string) bool {
		sum := 0
		for _, c := range m[len("ACME-"):] {
			sum += int(c - '0')
		}
		return sum%9 == 0
	})
	reg.Add("en", ticket)

	eng := analyzer.NewEngine(reg, []string{"en"})
	text := "ping bob@example.com about ACME-1234503, not ACME-1234504"
	opts := analyzer.Options{Entities: []string{entities.EmailAddress, "ACME_TICKET"}}
	for _, r := range byPosition(eng.Analyze(text, opts)) {
		fmt.Printf("%-13s %q %.2f\n", r.EntityType, r.Text, r.Score)
	}
	// Output:
	// EMAIL_ADDRESS "bob@example.com" 0.50
	// ACME_TICKET   "ACME-1234503" 1.00
}
