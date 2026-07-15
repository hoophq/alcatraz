package main

import (
	"strings"
	"testing"
)

func TestMask(t *testing.T) {
	tests := []struct{ in, want string }{
		{"abcd", "****"},
		{"jane@example.com", "ja************om"},
		{"4532015112830366", "45************66"},
		{"a-very-long-secret-value-that-keeps-going", "a-************ng"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := mask(tt.in); got != tt.want {
			t.Errorf("mask(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestWriteFindingsMasksValues(t *testing.T) {
	var b strings.Builder
	err := writeFindings(&b, []Finding{
		{File: "user.go", Line: 11, EntityType: "CREDIT_CARD", Value: "4532015112830366", Score: 0.92},
	})
	if err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if strings.Contains(out, "4532015112830366") {
		t.Errorf("raw value leaked into output:\n%s", out)
	}
	for _, want := range []string{"user.go:11", "CREDIT_CARD", "45************66", "1 finding(s)"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestWriteJSONMasksValues(t *testing.T) {
	var b strings.Builder
	err := writeJSON(&b, []Finding{
		{Line: 2, EntityType: "EMAIL_ADDRESS", Value: "jane@example.com", Score: 0.85},
	})
	if err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if strings.Contains(out, "jane@example.com") {
		t.Errorf("raw value leaked into JSON:\n%s", out)
	}
	for _, want := range []string{`"value_masked": "ja************om"`, `"total": 1`, `"entity_type": "EMAIL_ADDRESS"`} {
		if !strings.Contains(out, want) {
			t.Errorf("JSON missing %q:\n%s", want, out)
		}
	}
}

func TestWriteJSONEmpty(t *testing.T) {
	var b strings.Builder
	if err := writeJSON(&b, nil); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	// An empty scan must render findings as [], not null — consumers parse it.
	if !strings.Contains(out, `"findings": []`) {
		t.Errorf("empty findings not rendered as []:\n%s", out)
	}
}

func TestParseAllowlist(t *testing.T) {
	got := parseAllowlist("# comment\njane@example.com\n\n  spaced@x.io  \n")
	want := []string{"jane@example.com", "spaced@x.io"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %q, want %q", i, got[i], want[i])
		}
	}
}
