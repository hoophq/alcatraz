// Command alcatraz scans files, stdin text, or a unified diff for PII and
// secrets using the alcatraz library — entirely in-process, no service, no
// network calls.
//
// Usage:
//
//	alcatraz scan [flags] [path ...]   scan files line by line (no paths: read stdin)
//	alcatraz diff [flags]              read a unified diff on stdin, scan added lines
//	alcatraz version                   print the version
//
// Exit codes: 0 = no findings, 1 = findings detected, 2 = error. Detected
// values are always masked in the output — the raw values never leave the
// scan.
package main

import (
	"flag"
	"fmt"
	"os"
)

// version is stamped by release builds via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	code, err := run(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "alcatraz:", err)
		os.Exit(2)
	}
	os.Exit(code)
}

func run(args []string) (int, error) {
	if len(args) == 0 {
		return 0, fmt.Errorf("usage: alcatraz <scan|diff|version> [flags] [path ...]")
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "version", "-version", "--version":
		fmt.Println(version)
		return 0, nil
	case "scan", "diff":
	default:
		return 0, fmt.Errorf("unknown command %q (want scan, diff, or version)", cmd)
	}

	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	threshold := fs.Float64("threshold", 0.4, "minimum confidence score in [0,1]")
	entities := fs.String("entities", "", "comma-separated entity types to restrict to (empty = all)")
	ignore := fs.String("ignore", "DATE_TIME,URL", "comma-separated entity types to drop as noise")
	allowlistFile := fs.String("allowlist-file", "", "file with allowed values, one per line (# comments ok)")
	jsonOut := fs.Bool("json", false, "emit the findings as JSON instead of text")
	exclude := fs.String("exclude", "", "comma-separated glob patterns of diff paths to skip (diff only)")
	if err := fs.Parse(rest); err != nil {
		return 0, err
	}

	allowList, err := readAllowlist(*allowlistFile)
	if err != nil {
		return 0, err
	}
	s := newScanner(*threshold, splitList(*entities), splitList(*ignore), allowList)
	s.exclude = splitList(*exclude)

	var findings []Finding
	switch cmd {
	case "diff":
		findings, err = s.scanDiff(os.Stdin)
	case "scan":
		if fs.NArg() == 0 {
			findings, err = s.scanText(os.Stdin)
		} else {
			for _, path := range fs.Args() {
				fileFindings, ferr := s.scanFile(path)
				if ferr != nil {
					return 0, ferr
				}
				findings = append(findings, fileFindings...)
			}
		}
	}
	if err != nil {
		return 0, err
	}

	if *jsonOut {
		err = writeJSON(os.Stdout, findings)
	} else {
		err = writeFindings(os.Stdout, findings)
	}
	if err != nil {
		return 0, err
	}
	if len(findings) > 0 {
		return 1, nil
	}
	return 0, nil
}

// readAllowlist loads one allowed value per line, skipping blanks and
// #-comments.
func readAllowlist(path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("allowlist: %w", err)
	}
	return parseAllowlist(string(data)), nil
}
