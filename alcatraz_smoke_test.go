package alcatraz_test

import (
	"testing"

	"github.com/hoophq/alcatraz"
	"github.com/hoophq/alcatraz/entities"
)

func hasEntity(results []alcatraz.Result, entityType, text string) bool {
	for _, r := range results {
		if r.EntityType == entityType && r.Text == text {
			return true
		}
	}
	return false
}

func TestSmoke(t *testing.T) {
	eng := alcatraz.NewEngine()
	// 536-90-4399 is a structurally valid SSN (123-45-6789 is rejected as
	// sequential by the SSN validator).
	text := "Email jane.doe@example.com, card 4532015112830366, ssn 536-90-4399."
	got := eng.Analyze(text, alcatraz.Options{})

	if !hasEntity(got, entities.EmailAddress, "jane.doe@example.com") {
		t.Errorf("expected email detection, got %+v", got)
	}
	if !hasEntity(got, entities.CreditCard, "4532015112830366") {
		t.Errorf("expected valid credit card detection, got %+v", got)
	}
	if !hasEntity(got, entities.USSSN, "536-90-4399") {
		t.Errorf("expected SSN detection, got %+v", got)
	}
}

func TestInvalidCreditCardDropped(t *testing.T) {
	eng := alcatraz.NewEngine()
	// fails Luhn
	got := eng.Analyze("card 4532015112830367", alcatraz.Options{})
	if hasEntity(got, entities.CreditCard, "4532015112830367") {
		t.Errorf("expected invalid card to be dropped, got %+v", got)
	}
}
