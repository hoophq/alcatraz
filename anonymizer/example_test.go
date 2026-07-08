package anonymizer_test

import (
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
