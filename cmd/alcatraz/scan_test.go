package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sampleDiff adds one file with a real (Luhn-valid) card number and an email,
// and touches a second file where the only PII-looking line is a removal.
const sampleDiff = `diff --git a/user.go b/user.go
index 111..222 100644
--- a/user.go
+++ b/user.go
@@ -10,2 +10,4 @@ func setup() {
 	ctx := context.Background()
+	log.Printf("card=4532015112830366")
+	admin := "jane@example.com"
 	return ctx
diff --git a/old.go b/old.go
index 333..444 100644
--- a/old.go
+++ b/old.go
@@ -5 +5 @@ func old() {
-	ssn := "536-90-4399"
+	ssn := os.Getenv("SSN")
`

func defaultTestScanner(allow []string) *scanner {
	return newScanner(0.4, nil, []string{"DATE_TIME", "URL"}, allow)
}

func TestScanDiff(t *testing.T) {
	findings, err := defaultTestScanner(nil).scanDiff(strings.NewReader(sampleDiff))
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 2 {
		t.Fatalf("got %d findings, want 2: %+v", len(findings), findings)
	}

	card := findings[0]
	if card.EntityType != "CREDIT_CARD" || card.File != "user.go" || card.Line != 11 {
		t.Errorf("card finding = %+v, want CREDIT_CARD at user.go:11", card)
	}
	email := findings[1]
	if email.EntityType != "EMAIL_ADDRESS" || email.File != "user.go" || email.Line != 12 {
		t.Errorf("email finding = %+v, want EMAIL_ADDRESS at user.go:12", email)
	}
}

func TestScanDiffIgnoresRemovedLines(t *testing.T) {
	findings, err := defaultTestScanner(nil).scanDiff(strings.NewReader(sampleDiff))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range findings {
		if f.File == "old.go" {
			t.Errorf("removed-line PII was flagged: %+v", f)
		}
	}
}

func TestScanDiffAllowlist(t *testing.T) {
	findings, err := defaultTestScanner([]string{"jane@example.com"}).
		scanDiff(strings.NewReader(sampleDiff))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range findings {
		if f.EntityType == "EMAIL_ADDRESS" {
			t.Errorf("allow-listed email was flagged: %+v", f)
		}
	}
}

func TestScanDiffExclude(t *testing.T) {
	tests := []struct {
		pattern string
		want    int
	}{
		{"user.go", 0},   // exact path
		{"*.go", 0},      // basename glob
		{"user.*", 0},    // basename glob with wildcard ext
		{"vendor/**", 2}, // unrelated directory: nothing excluded
	}
	for _, tt := range tests {
		s := defaultTestScanner(nil)
		s.exclude = []string{tt.pattern}
		findings, err := s.scanDiff(strings.NewReader(sampleDiff))
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != tt.want {
			t.Errorf("exclude %q: got %d findings, want %d: %+v",
				tt.pattern, len(findings), tt.want, findings)
		}
	}
}

func TestScanText(t *testing.T) {
	body := "here are my logs:\nuser=jane@example.com logged in\nall good"
	findings, err := defaultTestScanner(nil).scanText(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("got %d findings, want 1: %+v", len(findings), findings)
	}
	if f := findings[0]; f.EntityType != "EMAIL_ADDRESS" || f.Line != 2 || f.File != "" {
		t.Errorf("finding = %+v, want EMAIL_ADDRESS at line 2 with no file", f)
	}
}

func TestScanFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.log")
	content := "starting\ncard 4532015112830366 charged\ndone\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	findings, err := defaultTestScanner(nil).scanFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("got %d findings, want 1: %+v", len(findings), findings)
	}
	if f := findings[0]; f.EntityType != "CREDIT_CARD" || f.File != path || f.Line != 2 {
		t.Errorf("finding = %+v, want CREDIT_CARD at %s:2", f, path)
	}
}

func TestThresholdDropsLowConfidence(t *testing.T) {
	// A bare 8-digit run only triggers low-confidence recognizers
	// (e.g. US_BANK_NUMBER at 0.05); threshold 0.4 must drop it.
	findings, err := defaultTestScanner(nil).scanText(strings.NewReader("id 12345678 ok"))
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Errorf("low-confidence match survived threshold: %+v", findings)
	}
}
