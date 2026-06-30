package alcatraz_test

import (
	"fmt"
	"testing"

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
