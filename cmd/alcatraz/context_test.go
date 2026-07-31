package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestScanContextFlag covers the -context flag end to end, through run.
//
// The threshold is 0.8, the value downstream CI wrappers default to. A
// labelled email scores 0.5 on its pattern alone and 0.85 once the words in
// front of it are read, so the flag is the difference between a clean scan and
// a failing one — which is the whole reason non-Go callers need a way to say
// "pattern strength only".
func TestScanContextFlag(t *testing.T) {
	file := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(file, []byte("email: jane@example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		flag string
		want int // exit code: 1 = findings, 0 = none
	}{
		{"context on by default", "-context=true", 1},
		{"context off keeps pattern-only scores", "-context=false", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, err := runQuiet(t, []string{"scan", "-threshold", "0.8", tt.flag, file})
			if err != nil {
				t.Fatal(err)
			}
			if code != tt.want {
				t.Errorf("exit code = %d, want %d", code, tt.want)
			}
		})
	}
}

// TestHookContextFlag pins that the hook flag set accepts -context too: the
// hook is how Claude Code calls the scanner, and it parses its own flags.
func TestHookContextFlag(t *testing.T) {
	for _, event := range []string{"claude-post", "claude-prompt"} {
		t.Run(event, func(t *testing.T) {
			stdin := os.Stdin
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			w.Close()
			os.Stdin = r
			defer func() { os.Stdin = stdin; r.Close() }()

			if _, err := runQuiet(t, []string{"hook", event, "-context=false"}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// runQuiet calls run with stdout swallowed: these tests assert on exit codes,
// and the findings report would otherwise land in the test log.
func runQuiet(t *testing.T, args []string) (int, error) {
	t.Helper()
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer devnull.Close()

	saved := os.Stdout
	os.Stdout = devnull
	defer func() { os.Stdout = saved }()

	return run(args)
}
