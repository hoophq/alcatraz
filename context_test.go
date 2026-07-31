package alcatraz_test

import (
	"testing"

	"github.com/hoophq/alcatraz"
	"github.com/hoophq/alcatraz/entities"
)

// scoreOf returns the score of the first result of the given type, or -1.
func scoreOf(results []alcatraz.Result, entityType string) float64 {
	for _, r := range results {
		if r.EntityType == entityType {
			return r.Score
		}
	}
	return -1
}

// TestContextBoostWithBuiltinRecognizers is the end-to-end shape of ATR-208:
// with the real recognizer set, a label next to a value lifts a pattern-only
// detection from its base score to Presidio's 0.85.
func TestContextBoostWithBuiltinRecognizers(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		entity string
		want   float64
	}{
		{"bare email scores on the pattern alone",
			"jane@example.com", entities.EmailAddress, 0.5},
		{"labelled email is boosted",
			"email: jane@example.com", entities.EmailAddress, 0.85},
		{"a plural label still counts",
			"Emails: jane@example.com", entities.EmailAddress, 0.85},
		{"a word merely containing the label does not",
			"the recipient is jane@example.com", entities.EmailAddress, 0.5},
		{"bare phone number",
			"call me on 555-123-4567", entities.PhoneNumber, 0.5},
		{"labelled phone number",
			"my phone is 555-123-4567", entities.PhoneNumber, 0.85},
		// A checksum-validated entity is already at MaxScore, so context
		// cannot and need not raise it.
		{"validated SSN is unaffected",
			"536-90-4399", entities.USSSN, alcatraz.MaxScore},
		{"labelled SSN is unaffected",
			"ssn: 536-90-4399", entities.USSSN, alcatraz.MaxScore},
	}

	eng := alcatraz.NewEngine()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scoreOf(eng.Analyze(tt.text, alcatraz.Options{}), tt.entity)
			if diff := got - tt.want; diff > 1e-9 || diff < -1e-9 {
				t.Errorf("%s score = %v, want %v (text %q)",
					tt.entity, got, tt.want, tt.text)
			}
		})
	}
}

// TestContextBoostClearsThreshold is the reason ATR-208 was filed: a caller
// running a threshold above the pattern ceiling used to match nothing at all,
// however clearly the text labelled the value.
func TestContextBoostClearsThreshold(t *testing.T) {
	threshold := 0.6
	eng := alcatraz.NewEngine()

	got := eng.Analyze("email: jane@example.com", alcatraz.Options{Threshold: &threshold})
	if !hasEntity(got, entities.EmailAddress, "jane@example.com") {
		t.Errorf("labelled email should clear a %.2f threshold, got %+v", threshold, got)
	}

	// The threshold still does its job on text with nothing to support it.
	got = eng.Analyze("jane@example.com", alcatraz.Options{Threshold: &threshold})
	if hasEntity(got, entities.EmailAddress, "jane@example.com") {
		t.Errorf("bare email should not clear a %.2f threshold, got %+v", threshold, got)
	}
}

// TestContextEnhancerCanBeDisabled keeps the pattern-only scores reachable for
// a caller that has tuned its thresholds against them.
func TestContextEnhancerCanBeDisabled(t *testing.T) {
	eng := alcatraz.NewEngine()
	eng.SetContextEnhancer(nil)

	got := scoreOf(eng.Analyze("email: jane@example.com", alcatraz.Options{}),
		entities.EmailAddress)
	if diff := got - 0.5; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("score = %v, want the unboosted 0.5", got)
	}
}
