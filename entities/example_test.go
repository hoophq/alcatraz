package entities_test

import (
	"fmt"

	"github.com/hoophq/alcatraz"
	"github.com/hoophq/alcatraz/entities"
)

// The constants are plain strings, so they slot into Options.Entities to
// narrow a scan and compare directly against Result.EntityType when routing a
// hit to a severity or redaction policy.
func Example() {
	eng := alcatraz.NewEngine()
	text := "card 4111 1111 1111 1111, ssn 078-05-1120, email jane@example.com"

	results := eng.Analyze(text, alcatraz.Options{
		Entities: []string{entities.CreditCard, entities.USSSN},
	})

	for _, r := range results {
		switch r.EntityType {
		case entities.CreditCard:
			fmt.Printf("payment data: %q\n", r.Text)
		case entities.USSSN:
			fmt.Printf("national id: %q\n", r.Text)
		default:
			fmt.Printf("other (%s): %q\n", r.EntityType, r.Text)
		}
	}
	// Output:
	// payment data: "4111 1111 1111 1111"
	// national id: "078-05-1120"
}
