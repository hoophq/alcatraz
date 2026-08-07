package anonymizer_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/hoophq/alcatraz"
	"github.com/hoophq/alcatraz/anonymizer"
	"github.com/hoophq/alcatraz/entities"
)

// Example detects PII with the standard engine and masks it: one operator as
// the default and a per-entity override that keeps the last card digits.
func Example() {
	eng := alcatraz.NewEngine()
	text := "Email jane@example.com, card 4532015112830366, ssn 536-90-4399."

	results := eng.Analyze(text, alcatraz.Options{
		Entities: []string{entities.EmailAddress, entities.CreditCard, entities.USSSN},
	})

	fmt.Println(anonymizer.Anonymize(text, results, anonymizer.Mask('*')))
	fmt.Println(anonymizer.AnonymizeWith(text, results, anonymizer.Config{
		Default: anonymizer.Replace(),
		PerEntity: map[string]anonymizer.Operator{
			entities.CreditCard: anonymizer.MaskKeepLast('#', 4),
		},
	}))
	// Output:
	// Email ****************, card ****************, ssn ***********.
	// Email <EMAIL_ADDRESS>, card ############0366, ssn <US_SSN>.
}

// ExampleAnonymizeWith mixes operators: every entity becomes an
// "<ENTITY_TYPE>" placeholder except credit cards, which keep their last four
// digits so support staff can still match a card to a statement.
func ExampleAnonymizeWith() {
	eng := alcatraz.NewEngine()
	text := "Email jane@example.com, card 4532015112830366, ssn 536-90-4399."

	results := eng.Analyze(text, alcatraz.Options{
		Entities: []string{entities.EmailAddress, entities.CreditCard, entities.USSSN},
	})

	fmt.Println(anonymizer.AnonymizeWith(text, results, anonymizer.Config{
		Default: anonymizer.Replace(),
		PerEntity: map[string]anonymizer.Operator{
			entities.CreditCard: anonymizer.MaskKeepLast('#', 4),
		},
	}))
	// Output:
	// Email <EMAIL_ADDRESS>, card ############0366, ssn <US_SSN>.
}

// ExampleMask replaces every rune of the match, preserving its length.
func ExampleMask() {
	op := anonymizer.Mask('#')
	fmt.Println(op(entities.PhoneNumber, "555-1234"))
	// Output: ########
}

// ExampleMaskKeepLast keeps a recognizable tail. Matches no longer than keep
// are returned unchanged, so short values never become the whole secret.
func ExampleMaskKeepLast() {
	op := anonymizer.MaskKeepLast('*', 4)
	fmt.Println(op(entities.CreditCard, "4532015112830366"))
	fmt.Println(op(entities.CreditCard, "0366"))
	// Output:
	// ************0366
	// 0366
}

// ExampleReplace emits the entity type in angle brackets. It is the operator
// used when Config.Default is nil.
func ExampleReplace() {
	op := anonymizer.Replace()
	fmt.Println(op(entities.EmailAddress, "jane@example.com"))
	// Output: <EMAIL_ADDRESS>
}

// ExampleRedact drops the match entirely, shortening the text.
func ExampleRedact() {
	op := anonymizer.Redact()
	fmt.Printf("%q\n", op(entities.USSSN, "536-90-4399"))
	// Output: ""
}

// ExampleOperator shows that an Operator is just a function: this one
// tokenizes each match into a stable, entity-scoped sha256 prefix, so the same
// value always yields the same token and equality joins survive anonymization.
func ExampleOperator() {
	token := func(entityType, match string) string {
		sum := sha256.Sum256([]byte(entityType + "|" + match))
		return entityType + "_" + hex.EncodeToString(sum[:4])
	}

	var op anonymizer.Operator = token
	text := "from jane@example.com to jane@example.com"
	results := alcatraz.NewEngine().Analyze(text, alcatraz.Options{
		Entities: []string{entities.EmailAddress},
	})

	fmt.Println(anonymizer.Anonymize(text, results, op))
	// Output:
	// from EMAIL_ADDRESS_272f9d53 to EMAIL_ADDRESS_272f9d53
}
