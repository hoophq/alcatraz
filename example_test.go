package alcatraz_test

import (
	"fmt"
	"testing"
	"unicode/utf8"

	"github.com/hoophq/alcatraz"
	"github.com/hoophq/alcatraz/entities"
)

func hasEntityType(results []alcatraz.Result, entityType string) bool {
	for _, r := range results {
		if r.EntityType == entityType {
			return true
		}
	}
	return false
}

func TestDetectsAcrossRegions(t *testing.T) {
	eng := alcatraz.NewEngine()
	cases := []struct {
		entity string
		text   string
	}{
		{entities.IBANCode, "IBAN DE89370400440532013000"},
		{entities.UKNHS, "nhs 943 476 5919"},
		{entities.PLPESEL, "pesel 44051401359"},
		{entities.INAadhaar, "aadhaar 2341 2341 2346"},
		{entities.ESNIF, "nif 12345678Z"},
		{entities.KRRRN, "rrn 900101-1234568"},
		{entities.THTNIN, "tnin 1-1017-00230-25-2"},
	}
	for _, c := range cases {
		got := eng.Analyze(c.text, alcatraz.Options{})
		if !hasEntityType(got, c.entity) {
			t.Errorf("%s: expected detection in %q, got %+v", c.entity, c.text, got)
		}
	}
}

func Example() {
	eng := alcatraz.NewEngine()
	text := "Contact jane@example.com or pay to IBAN DE89370400440532013000"
	for _, hit := range eng.Analyze(text, alcatraz.Options{}) {
		fmt.Printf("%s %q %.2f\n", hit.EntityType, hit.Text, hit.Score)
	}
	// Output:
	// IBAN_CODE "DE89370400440532013000" 1.00
	// EMAIL_ADDRESS "jane@example.com" 0.50
}

// Options.Threshold trades recall for precision: results scoring below it are
// dropped. Pattern-only matches with no corroborating context score low, so a
// threshold is the main knob for suppressing them.
func ExampleOptions_threshold() {
	eng := alcatraz.NewEngine()
	text := "Contact jane@example.com or pay to IBAN DE89370400440532013000"

	fmt.Println("no threshold:")
	for _, hit := range eng.Analyze(text, alcatraz.Options{}) {
		fmt.Printf("  %s %.2f\n", hit.EntityType, hit.Score)
	}

	// Threshold is a *float64 so that "unset" stays distinct from 0.
	threshold := 0.6
	fmt.Println("threshold 0.6:")
	for _, hit := range eng.Analyze(text, alcatraz.Options{Threshold: &threshold}) {
		fmt.Printf("  %s %.2f\n", hit.EntityType, hit.Score)
	}
	// Output:
	// no threshold:
	//   IBAN_CODE 1.00
	//   EMAIL_ADDRESS 0.50
	// threshold 0.6:
	//   IBAN_CODE 1.00
}

// Options.Entities restricts a scan to the entity types you care about.
// Recognizers for every other type are skipped entirely, so a narrow scan is
// also a cheaper one.
func ExampleOptions_entities() {
	eng := alcatraz.NewEngine()
	text := "card 4111 1111 1111 1111, email jane@example.com, ip 10.0.0.1"

	fmt.Println("everything:")
	for _, hit := range eng.Analyze(text, alcatraz.Options{}) {
		fmt.Printf("  %s %q\n", hit.EntityType, hit.Text)
	}

	fmt.Println("credit cards only:")
	for _, hit := range eng.Analyze(text, alcatraz.Options{
		Entities: []string{entities.CreditCard},
	}) {
		fmt.Printf("  %s %q\n", hit.EntityType, hit.Text)
	}
	// Output:
	// everything:
	//   CREDIT_CARD "4111 1111 1111 1111"
	//   IP_ADDRESS "10.0.0.1"
	//   EMAIL_ADDRESS "jane@example.com"
	// credit cards only:
	//   CREDIT_CARD "4111 1111 1111 1111"
}

// Options.AllowList suppresses matches whose text is known to be safe:
// fixtures, documentation samples, a vendor's sandbox account. Entries are
// compared as exact strings unless Options.AllowListRegex is set, in which
// case they are joined with "|" and compiled as one regular expression.
func ExampleOptions_allowList() {
	eng := alcatraz.NewEngine()
	text := "test card 4111 1111 1111 1111 billed to jane@example.com"

	fmt.Println("as detected:")
	for _, hit := range eng.Analyze(text, alcatraz.Options{}) {
		fmt.Printf("  %s %q\n", hit.EntityType, hit.Text)
	}

	fmt.Println("with the test card allow-listed:")
	for _, hit := range eng.Analyze(text, alcatraz.Options{
		AllowList: []string{"4111 1111 1111 1111"},
	}) {
		fmt.Printf("  %s %q\n", hit.EntityType, hit.Text)
	}

	// Matching is against the detected span, so a regex anchored at the end
	// of the match clears every address on a throwaway domain at once.
	fmt.Println("with example.com addresses allow-listed:")
	for _, hit := range eng.Analyze(text, alcatraz.Options{
		AllowList:      []string{`@example\.com$`},
		AllowListRegex: true,
	}) {
		fmt.Printf("  %s %q\n", hit.EntityType, hit.Text)
	}
	// Output:
	// as detected:
	//   CREDIT_CARD "4111 1111 1111 1111"
	//   EMAIL_ADDRESS "jane@example.com"
	// with the test card allow-listed:
	//   EMAIL_ADDRESS "jane@example.com"
	// with example.com addresses allow-listed:
	//   CREDIT_CARD "4111 1111 1111 1111"
}

// Result.Start and Result.End are byte offsets into the text you passed to
// Analyze, not rune indices, because Go's regexp engine reports bytes. Slice
// the original string with them directly and you get Result.Text back, even
// when the text contains multi-byte characters. Counting runes to locate a
// span gives different numbers, so do not mix the two.
func ExampleResult() {
	eng := alcatraz.NewEngine()
	// "Reservación" is 11 runes but 12 bytes: the á costs two.
	text := "Reservación: jane@example.com"
	fmt.Printf("%d bytes, %d runes\n", len(text), utf8.RuneCountInString(text))

	for _, r := range eng.Analyze(text, alcatraz.Options{}) {
		fmt.Printf("%s [%d:%d] %q\n", r.EntityType, r.Start, r.End, r.Text)
		fmt.Println("text[r.Start:r.End] == r.Text:", text[r.Start:r.End] == r.Text)
		// The same span expressed in runes starts one earlier, which is why
		// []rune(text)[r.Start:r.End] would cut the wrong bytes.
		fmt.Println("rune offset of the match:", utf8.RuneCountInString(text[:r.Start]))
	}
	// Output:
	// 30 bytes, 29 runes
	// EMAIL_ADDRESS [14:30] "jane@example.com"
	// text[r.Start:r.End] == r.Text: true
	// rune offset of the match: 13
}
