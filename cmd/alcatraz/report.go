package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// mask redacts a detected value so output never re-publishes the PII it
// flagged. It keeps at most the first and last two characters.
func mask(s string) string {
	runes := []rune(s)
	n := len(runes)
	if n <= 4 {
		return strings.Repeat("*", n)
	}
	masked := n - 4
	if masked > 12 {
		masked = 12
	}
	return string(runes[:2]) + strings.Repeat("*", masked) + string(runes[n-2:])
}

// location renders where a finding was seen: "path:line", "line N" for
// file-less text input, or "-" when unknown.
func (f Finding) location() string {
	switch {
	case f.File != "":
		return fmt.Sprintf("%s:%d", f.File, f.Line)
	case f.Line > 0:
		return fmt.Sprintf("line %d", f.Line)
	default:
		return "-"
	}
}

// writeFindings prints one line per finding and a trailing count:
//
//	user.go:11  CREDIT_CARD  45************66  0.92
//	alcatraz: 1 finding(s)
//
// Write errors are returned so a failed stdout (e.g. broken pipe) surfaces
// as exit code 2, per the CLI contract.
func writeFindings(w io.Writer, findings []Finding) error {
	for _, f := range findings {
		if _, err := fmt.Fprintf(w, "%s  %s  %s  %.2f\n", f.location(), f.EntityType, mask(f.Value), f.Score); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "alcatraz: %d finding(s)\n", len(findings))
	return err
}

// jsonFinding is the machine-readable shape of one finding. Values are
// masked here too — JSON output is no less public than text output.
type jsonFinding struct {
	File        string  `json:"file,omitempty"`
	Line        int     `json:"line"`
	EntityType  string  `json:"entity_type"`
	ValueMasked string  `json:"value_masked"`
	Score       float64 `json:"score"`
}

func writeJSON(w io.Writer, findings []Finding) error {
	out := struct {
		Findings []jsonFinding `json:"findings"`
		Total    int           `json:"total"`
	}{Findings: make([]jsonFinding, 0, len(findings)), Total: len(findings)}
	for _, f := range findings {
		out.Findings = append(out.Findings, jsonFinding{
			File:        f.File,
			Line:        f.Line,
			EntityType:  f.EntityType,
			ValueMasked: mask(f.Value),
			Score:       f.Score,
		})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// parseAllowlist extracts allowed values from file content, one per line,
// skipping blanks and #-comments.
func parseAllowlist(content string) []string {
	var values []string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		values = append(values, line)
	}
	return values
}

func splitList(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}
