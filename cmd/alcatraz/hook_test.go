package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func hookScanner() *scanner {
	return newScanner(0.5, nil, []string{"DATE_TIME", "URL", "IP_ADDRESS"}, nil)
}

func TestMaskStringReplacesAndCounts(t *testing.T) {
	counts := map[string]int{}
	got := maskString(hookScanner(), "card 4532015112830366 for jane@example.com", counts)
	if strings.Contains(got, "4532015112830366") || strings.Contains(got, "jane@example.com") {
		t.Errorf("raw values survived masking: %q", got)
	}
	if !strings.Contains(got, "45************66") {
		t.Errorf("masked card missing: %q", got)
	}
	if counts["CREDIT_CARD"] != 1 || counts["EMAIL_ADDRESS"] != 1 {
		t.Errorf("counts = %v, want CREDIT_CARD:1 EMAIL_ADDRESS:1", counts)
	}
}

func TestMaskStringOverlappingDetections(t *testing.T) {
	// The engine keeps overlaps across entity types. With URL detection
	// enabled (not ignored), a URL containing an email produces overlapping
	// spans — the union must be masked, with no raw fragment left exposed.
	s := newScanner(0.4, nil, []string{"DATE_TIME"}, nil)
	counts := map[string]int{}
	got := maskString(s, "see https://example.com/jane@example.com for details", counts)
	for _, raw := range []string{"jane@example.com", "example.com/jane", "https://example.com"} {
		if strings.Contains(got, raw) {
			t.Errorf("overlap left raw fragment %q exposed: %q", raw, got)
		}
	}
}

func TestRunHookNeverErrors(t *testing.T) {
	// Setup mistakes must fail open (exit 0), never break the session.
	for _, args := range [][]string{
		{},
		{"claude-nope"},
		{"claude-post", "-definitely-not-a-flag"},
	} {
		code, err := runHook(args)
		if code != 0 || err != nil {
			t.Errorf("runHook(%v) = (%d, %v), want (0, nil)", args, code, err)
		}
	}
}

func TestMaskAnySkipsPathKeys(t *testing.T) {
	counts := map[string]int{}
	v := map[string]any{
		"filenames": []any{"/exports/jane@example.com.csv"},
		"content":   "row: jane@example.com",
	}
	got := maskAny(hookScanner(), v, "", counts).(map[string]any)
	if got["filenames"].([]any)[0] != "/exports/jane@example.com.csv" {
		t.Errorf("path value was masked: %v", got["filenames"])
	}
	if strings.Contains(got["content"].(string), "jane@example.com") {
		t.Errorf("content value not masked: %v", got["content"])
	}
}

// runPost runs runClaudePost and captures stdout.
func runPost(t *testing.T, input string, skipTools []string, chain string) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	runClaudePost(hookScanner(), []byte(input), skipTools, chain)
	w.Close()
	os.Stdout = orig
	var b strings.Builder
	buf := make([]byte, 64*1024)
	for {
		n, rerr := r.Read(buf)
		b.Write(buf[:n])
		if rerr != nil {
			break
		}
	}
	return b.String()
}

func TestClaudePostMasksBashOutput(t *testing.T) {
	in := `{"tool_name":"Bash","tool_input":{"command":"psql -c 'select email from users'"},"tool_response":{"stdout":"jane@example.com\nssn 536-90-4399","stderr":"","interrupted":false}}`
	out := runPost(t, in, []string{"Read"}, "")
	if out == "" {
		t.Fatal("expected hook output, got none")
	}
	var parsed struct {
		HookSpecificOutput struct {
			HookEventName     string         `json:"hookEventName"`
			UpdatedToolOutput map[string]any `json:"updatedToolOutput"`
			AdditionalContext string         `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("output not valid JSON: %v\n%s", err, out)
	}
	h := parsed.HookSpecificOutput
	if h.HookEventName != "PostToolUse" {
		t.Errorf("hookEventName = %q", h.HookEventName)
	}
	stdout, _ := h.UpdatedToolOutput["stdout"].(string)
	if strings.Contains(stdout, "jane@example.com") || strings.Contains(stdout, "536-90-4399") {
		t.Errorf("raw PII survived: %q", stdout)
	}
	// Untouched fields are echoed back so the response shape survives.
	if _, ok := h.UpdatedToolOutput["interrupted"]; !ok {
		t.Error("non-string field dropped from updatedToolOutput")
	}
	if !strings.Contains(h.AdditionalContext, "EMAIL_ADDRESS") || !strings.Contains(h.AdditionalContext, "masked") {
		t.Errorf("additionalContext missing summary: %q", h.AdditionalContext)
	}
	if strings.Contains(out, "jane@example.com") {
		t.Error("raw value present somewhere in hook output")
	}
}

func TestClaudePostSkipsReadByDefault(t *testing.T) {
	in := `{"tool_name":"Read","tool_input":{"file_path":"/tmp/u.csv"},"tool_response":{"file":{"filePath":"/tmp/u.csv","content":"jane@example.com"}}}`
	if out := runPost(t, in, []string{"Read"}, ""); out != "" {
		t.Errorf("Read output was rewritten: %s", out)
	}
}

func TestClaudePostNoFindingsNoOutput(t *testing.T) {
	in := `{"tool_name":"Bash","tool_input":{"command":"true"},"tool_response":{"stdout":"all clean here","stderr":""}}`
	if out := runPost(t, in, []string{"Read"}, ""); out != "" {
		t.Errorf("expected no opinion, got: %s", out)
	}
}

// chainScript writes an executable that emits a fixed hook output, standing
// in for julius.
func chainScript(t *testing.T, output string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "chain.sh")
	script := "#!/bin/sh\ncat >/dev/null\nprintf '%s' " + shellQuote(output) + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func TestClaudePostChainComposition(t *testing.T) {
	// The chain compresses the output and leaves PII in the compressed text;
	// masking must apply to the chain's version, not the original.
	chainOut := `{"hookSpecificOutput":{"hookEventName":"PostToolUse","updatedToolOutput":{"stdout":"[compressed] jane@example.com","stderr":""}}}`
	in := `{"tool_name":"Bash","tool_input":{"command":"git log"},"tool_response":{"stdout":"original long output jane@example.com","stderr":""}}`
	out := runPost(t, in, []string{"Read"}, chainScript(t, chainOut))
	if !strings.Contains(out, "[compressed]") {
		t.Fatalf("chain's rewrite lost: %s", out)
	}
	if strings.Contains(out, "jane@example.com") {
		t.Errorf("raw PII survived chain+mask: %s", out)
	}
}

func TestClaudePostChainPassThroughWhenClean(t *testing.T) {
	// Chain rewrites, alcatraz finds nothing: the chain's output must pass
	// through verbatim, not be swallowed.
	chainOut := `{"hookSpecificOutput":{"hookEventName":"PostToolUse","updatedToolOutput":{"stdout":"[compressed] nothing sensitive","stderr":""}}}`
	in := `{"tool_name":"Bash","tool_input":{"command":"git status"},"tool_response":{"stdout":"long clean output","stderr":""}}`
	out := runPost(t, in, []string{"Read"}, chainScript(t, chainOut))
	if !strings.Contains(out, "[compressed] nothing sensitive") {
		t.Errorf("chain output swallowed: %q", out)
	}
}

func TestClaudePostChainFailureFailsOpen(t *testing.T) {
	in := `{"tool_name":"Bash","tool_input":{"command":"x"},"tool_response":{"stdout":"mail jane@example.com","stderr":""}}`
	out := runPost(t, in, []string{"Read"}, "/nonexistent/rewriter")
	// Chain is gone: masking still applies to the original response.
	if out == "" || strings.Contains(out, "jane@example.com") {
		t.Errorf("fail-open masking broken: %q", out)
	}
}

// runPrompt runs runClaudePrompt and captures stdout.
func runPrompt(t *testing.T, input, mode string) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	runClaudePrompt(hookScanner(), []byte(input), mode)
	w.Close()
	os.Stdout = orig
	var b strings.Builder
	buf := make([]byte, 64*1024)
	for {
		n, rerr := r.Read(buf)
		b.Write(buf[:n])
		if rerr != nil {
			break
		}
	}
	return b.String()
}

func TestClaudePromptWarn(t *testing.T) {
	out := runPrompt(t, `{"prompt":"here is the customer: jane@example.com"}`, "warn")
	var parsed hookOutput
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("bad JSON: %v\n%s", err, out)
	}
	if parsed.Decision != "" {
		t.Errorf("warn mode must not block, got decision %q", parsed.Decision)
	}
	if !strings.Contains(parsed.SystemMessage, "EMAIL_ADDRESS") {
		t.Errorf("systemMessage missing summary: %q", parsed.SystemMessage)
	}
	if parsed.HookSpecificOutput == nil || !strings.Contains(parsed.HookSpecificOutput.AdditionalContext, "Do not repeat") {
		t.Errorf("additionalContext missing guidance: %+v", parsed.HookSpecificOutput)
	}
}

func TestClaudePromptBlockMasksReason(t *testing.T) {
	out := runPrompt(t, `{"prompt":"ssn is 536-90-4399"}`, "block")
	var parsed hookOutput
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("bad JSON: %v\n%s", err, out)
	}
	if parsed.Decision != "block" {
		t.Errorf("decision = %q, want block", parsed.Decision)
	}
	if strings.Contains(parsed.Reason, "536-90-4399") {
		t.Errorf("block reason re-leaks the raw value: %q", parsed.Reason)
	}
	if !strings.Contains(parsed.Reason, "US_SSN") {
		t.Errorf("reason missing entity summary: %q", parsed.Reason)
	}
}

func TestClaudePromptCleanNoOutput(t *testing.T) {
	if out := runPrompt(t, `{"prompt":"please refactor the parser"}`, "warn"); out != "" {
		t.Errorf("clean prompt produced output: %q", out)
	}
}
