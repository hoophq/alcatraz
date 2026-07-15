package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// Claude Code hook processors. Both read one hook-event JSON document on
// stdin and write a hook-output JSON document on stdout (or nothing, meaning
// "no opinion"). They always exit 0: a masking hook must never break the
// session it protects, so internal errors go to stderr and the tool result
// passes through untouched.
//
//	alcatraz hook claude-post    PostToolUse — masks PII in tool outputs via
//	                             updatedToolOutput before they enter model
//	                             context; -chain composes an upstream
//	                             rewriter (e.g. julius) so two rewriters
//	                             never race on the same event
//	alcatraz hook claude-prompt  UserPromptSubmit — warns (or blocks) when
//	                             the user's prompt itself carries PII

// chainTimeout bounds the upstream rewriter: a hung chain command must not
// stall the session (Claude Code enforces its own hook timeout well above
// this).
const chainTimeout = 10 * time.Second

// maxHookBytes caps the hook payload read from stdin: a pathological
// tool_response must degrade to "no opinion", not to memory pressure in a
// session-critical process.
const maxHookBytes = 10 << 20

// pathKeys are JSON fields whose string values are filesystem paths the
// agent navigates by (Grep filenames, Read file paths). Masking a path that
// happens to contain PII would break every follow-up tool call on it, so
// path fields pass through unmasked.
var pathKeys = map[string]bool{
	"filenames": true,
	"filePath":  true,
	"file_path": true,
	"path":      true,
}

type postInput struct {
	ToolName     string          `json:"tool_name"`
	ToolInput    json.RawMessage `json:"tool_input"`
	ToolResponse any             `json:"tool_response"`
}

type hookSpecificOutput struct {
	HookEventName     string `json:"hookEventName"`
	UpdatedToolOutput any    `json:"updatedToolOutput,omitempty"`
	AdditionalContext string `json:"additionalContext,omitempty"`
}

type hookOutput struct {
	SystemMessage      string              `json:"systemMessage,omitempty"`
	Decision           string              `json:"decision,omitempty"`
	Reason             string              `json:"reason,omitempty"`
	HookSpecificOutput *hookSpecificOutput `json:"hookSpecificOutput,omitempty"`
}

// runHook never returns a non-zero code or an error: a hook process exits 0
// no matter what, because Claude Code treats non-zero hook exits as session
// errors — a misconfigured masking hook must degrade to "no opinion", never
// to a broken session. Setup mistakes are reported on stderr only.
func runHook(args []string) (int, error) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "alcatraz hook: usage: alcatraz hook <claude-post|claude-prompt> [flags]")
		return 0, nil
	}
	event, rest := args[0], args[1:]

	fs := flag.NewFlagSet("hook "+event, flag.ContinueOnError)
	threshold := fs.Float64("threshold", 0.5, "minimum confidence score in [0,1]")
	entities := fs.String("entities", "", "comma-separated entity types to restrict to (empty = all)")
	ignore := fs.String("ignore", "DATE_TIME,URL,IP_ADDRESS", "comma-separated entity types to drop as noise")
	var skipTools, chain, mode *string
	switch event {
	case "claude-post":
		skipTools = fs.String("skip-tools", "Read", "comma-separated tool names whose outputs are never masked (fresh file content feeds exact-match edits)")
		chain = fs.String("chain", "", "upstream rewriter to run first (whitespace-split command, e.g. 'julius hook claude-post'); its updatedToolOutput is masked instead of the raw result")
	case "claude-prompt":
		mode = fs.String("mode", "warn", "what to do on findings: warn or block")
	default:
		fmt.Fprintf(os.Stderr, "alcatraz hook: unknown event %q (want claude-post or claude-prompt)\n", event)
		return 0, nil
	}
	if err := fs.Parse(rest); err != nil {
		// flag already printed the problem to stderr.
		return 0, nil
	}

	s := newScanner(*threshold, splitList(*entities), splitList(*ignore), nil)

	input, err := io.ReadAll(io.LimitReader(os.Stdin, maxHookBytes+1))
	if err != nil {
		fmt.Fprintln(os.Stderr, "alcatraz hook: reading stdin:", err)
		return 0, nil // fail open
	}
	if len(input) > maxHookBytes {
		fmt.Fprintf(os.Stderr, "alcatraz hook: payload exceeds %d bytes; passing through unmasked\n", maxHookBytes)
		return 0, nil
	}

	switch event {
	case "claude-post":
		runClaudePost(s, input, splitList(*skipTools), *chain)
	case "claude-prompt":
		runClaudePrompt(s, input, *mode)
	}
	return 0, nil
}

func runClaudePost(s *scanner, input []byte, skipTools []string, chain string) {
	var in postInput
	if err := json.Unmarshal(input, &in); err != nil || in.ToolResponse == nil {
		return
	}

	// Compose the upstream rewriter first: masking applies to what the model
	// would actually see. Its raw output is kept so a no-findings run can
	// pass it through unchanged.
	working := in.ToolResponse
	var chainRaw []byte
	chainCtx := ""
	if chain != "" {
		if updated, ctx, raw, ok := runChainCmd(chain, input); ok {
			working = updated
			chainRaw = raw
			chainCtx = ctx
		}
	}

	skip := false
	for _, t := range skipTools {
		if t == in.ToolName {
			skip = true
			break
		}
	}

	counts := map[string]int{}
	if !skip {
		working = maskAny(s, working, "", counts)
	}

	if len(counts) == 0 {
		// Nothing masked: defer entirely to the chain's opinion, if any.
		if chainRaw != nil {
			os.Stdout.Write(chainRaw)
		}
		return
	}

	ctx := fmt.Sprintf(
		"Hoop masked %s in this tool output before it entered context (values render like ja************om). Do not attempt to reconstruct or guess the raw values. The data is intact in the underlying system; the user can view it outside this session, or disable masking with HOOP_PII_MASK_DISABLE=1.",
		summarizeCounts(counts))
	if chainCtx != "" {
		ctx = chainCtx + " " + ctx
	}
	emit(hookOutput{HookSpecificOutput: &hookSpecificOutput{
		HookEventName:     "PostToolUse",
		UpdatedToolOutput: working,
		AdditionalContext: ctx,
	}})
}

func runClaudePrompt(s *scanner, input []byte, mode string) {
	var in struct {
		Prompt string `json:"prompt"`
	}
	if err := json.Unmarshal(input, &in); err != nil || in.Prompt == "" {
		return
	}

	counts := map[string]int{}
	masked := maskString(s, in.Prompt, counts)
	if len(counts) == 0 {
		return
	}
	summary := summarizeCounts(counts)

	if mode == "block" {
		emit(hookOutput{
			Decision: "block",
			Reason: fmt.Sprintf(
				"Hoop blocked this prompt: it appears to contain %s. Remove or mask the values and resend — or set HOOP_PROMPT_GUARD=warn (or off) to change this behavior. Masked view:\n%s",
				summary, masked),
		})
		return
	}
	emit(hookOutput{
		SystemMessage: fmt.Sprintf("⚠️ Hoop: your prompt looks like it contains %s — it has entered the model context.", summary),
		HookSpecificOutput: &hookSpecificOutput{
			HookEventName: "UserPromptSubmit",
			AdditionalContext: fmt.Sprintf(
				"The user's prompt appears to contain PII (%s). Do not repeat these raw values back in your replies or write them to files/logs; refer to them indirectly.",
				summary),
		},
	})
}

// runChainCmd executes the upstream rewriter with the original hook input on
// stdin and interprets its output. Any failure — non-zero exit, timeout,
// unparsable output — is treated as "chain had no opinion" so the pipeline
// fails open.
func runChainCmd(chain string, input []byte) (updated any, addCtx string, raw []byte, ok bool) {
	parts := strings.Fields(chain)
	if len(parts) == 0 {
		return nil, "", nil, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), chainTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil || len(bytes.TrimSpace(out)) == 0 {
		return nil, "", nil, false
	}
	var parsed struct {
		HookSpecificOutput struct {
			UpdatedToolOutput any    `json:"updatedToolOutput"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if json.Unmarshal(out, &parsed) != nil || parsed.HookSpecificOutput.UpdatedToolOutput == nil {
		return nil, "", nil, false
	}
	return parsed.HookSpecificOutput.UpdatedToolOutput, parsed.HookSpecificOutput.AdditionalContext, out, true
}

// maskAny walks a decoded JSON value and masks PII in every string leaf,
// except values under path-carrying keys (see pathKeys). counts accumulates
// findings per entity type.
func maskAny(s *scanner, v any, key string, counts map[string]int) any {
	switch t := v.(type) {
	case string:
		if pathKeys[key] {
			return t
		}
		return maskString(s, t, counts)
	case map[string]any:
		for k, val := range t {
			t[k] = maskAny(s, val, k, counts)
		}
		return t
	case []any:
		for i, val := range t {
			// Elements inherit the parent key: entries of a "filenames"
			// array are paths too.
			t[i] = maskAny(s, val, key, counts)
		}
		return t
	default:
		return v
	}
}

// maskString replaces every surviving detection in text with its masked
// form, working back-to-front so byte offsets stay valid. The engine keeps
// overlapping detections across entity types, so overlapping spans are
// clamped to the still-unmasked region — the union of all enabled detections
// gets covered, never partially exposed.
func maskString(s *scanner, text string, counts map[string]int) string {
	results := s.engine.Analyze(text, s.opts)
	if len(results) == 0 {
		return text
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Start > results[j].Start })
	limit := len(text)
	for _, r := range results {
		if s.ignored[r.EntityType] || r.Start < 0 {
			continue
		}
		end := r.End
		if end > limit {
			end = limit
		}
		if r.Start >= end {
			continue
		}
		text = text[:r.Start] + mask(text[r.Start:end]) + text[end:]
		limit = r.Start
		counts[r.EntityType]++
	}
	return text
}

// summarizeCounts renders "EMAIL_ADDRESS ×2, CREDIT_CARD ×1" in a stable
// order (highest count first, then name).
func summarizeCounts(counts map[string]int) string {
	types := make([]string, 0, len(counts))
	for t := range counts {
		types = append(types, t)
	}
	sort.Slice(types, func(i, j int) bool {
		if counts[types[i]] != counts[types[j]] {
			return counts[types[i]] > counts[types[j]]
		}
		return types[i] < types[j]
	})
	var b strings.Builder
	for i, t := range types {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s ×%d", t, counts[t])
	}
	return b.String()
}

func emit(out hookOutput) {
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		fmt.Fprintln(os.Stderr, "alcatraz hook: writing output:", err)
	}
}
