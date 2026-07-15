package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/hoophq/alcatraz"
)

// Finding is one PII detection mapped back to its location in the scanned
// input. File is empty in text mode.
type Finding struct {
	File       string
	Line       int
	EntityType string
	Value      string
	Score      float64
}

// scanner wraps an alcatraz engine with the scan-time options shared by all
// modes.
type scanner struct {
	engine  *alcatraz.Engine
	opts    alcatraz.Options
	ignored map[string]bool
	// exclude holds glob patterns for diff paths to skip (e.g. "go.sum",
	// "*.lock", "vendor/**").
	exclude []string
}

func newScanner(threshold float64, entities, ignore, allowList []string) *scanner {
	ignored := make(map[string]bool, len(ignore))
	for _, e := range ignore {
		ignored[e] = true
	}
	return &scanner{
		engine: alcatraz.NewEngine(),
		opts: alcatraz.Options{
			Entities:  entities,
			Threshold: &threshold,
			AllowList: allowList,
		},
		ignored: ignored,
	}
}

// excluded reports whether a diff path matches any exclude pattern. Patterns
// match the full path or its basename; "dir/**" matches everything under dir.
func (s *scanner) excluded(file string) bool {
	for _, p := range s.exclude {
		if dir, ok := strings.CutSuffix(p, "/**"); ok && strings.HasPrefix(file, dir+"/") {
			return true
		}
		if ok, _ := path.Match(p, file); ok {
			return true
		}
		if ok, _ := path.Match(p, path.Base(file)); ok {
			return true
		}
	}
	return false
}

// analyzeLine runs the engine over one line of input and appends a Finding
// per surviving detection.
func (s *scanner) analyzeLine(file string, line int, content string, out []Finding) []Finding {
	for _, r := range s.engine.Analyze(content, s.opts) {
		if s.ignored[r.EntityType] {
			continue
		}
		out = append(out, Finding{
			File:       file,
			Line:       line,
			EntityType: r.EntityType,
			Value:      r.Text,
			Score:      r.Score,
		})
	}
	return out
}

// hunkRe extracts the new-file start line from a unified diff hunk header.
var hunkRe = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)`)

// scanDiff parses a unified diff and analyzes only added lines, tracking the
// file and line number each added line lands on in the new version.
func (s *scanner) scanDiff(r io.Reader) ([]Finding, error) {
	var findings []Finding
	var file string
	line := 0
	inHunk := false

	sc := newLineScanner(r)
	for sc.Scan() {
		l := sc.Text()
		switch {
		case strings.HasPrefix(l, "+++ "):
			file = strings.TrimPrefix(strings.TrimPrefix(l, "+++ "), "b/")
			if file == "/dev/null" || s.excluded(file) {
				file = ""
			}
			inHunk = false
		case file == "":
			// Deleted or excluded file: skip until the next file header.
		case hunkRe.MatchString(l):
			m := hunkRe.FindStringSubmatch(l)
			line, _ = strconv.Atoi(m[1])
			inHunk = true
		case !inHunk:
			// Skip diff headers (diff --git, index, ---, mode lines).
		case strings.HasPrefix(l, "+"):
			findings = s.analyzeLine(file, line, l[1:], findings)
			line++
		case strings.HasPrefix(l, " "):
			line++
		}
	}
	return findings, sc.Err()
}

// scanFile analyzes every line of the file at path.
func (s *scanner) scanFile(path string) ([]Finding, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var findings []Finding
	sc := newLineScanner(f)
	for line := 1; sc.Scan(); line++ {
		findings = s.analyzeLine(path, line, sc.Text(), findings)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return findings, nil
}

// scanText analyzes free text (e.g. a PR comment or issue body) line by line.
// Findings carry line numbers but no file.
func (s *scanner) scanText(r io.Reader) ([]Finding, error) {
	var findings []Finding
	sc := newLineScanner(r)
	for line := 1; sc.Scan(); line++ {
		findings = s.analyzeLine("", line, sc.Text(), findings)
	}
	return findings, sc.Err()
}

// newLineScanner returns a bufio.Scanner sized for long log lines.
func newLineScanner(r io.Reader) *bufio.Scanner {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	return sc
}
