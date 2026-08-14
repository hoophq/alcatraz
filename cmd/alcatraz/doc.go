// Command alcatraz scans files, stdin text, or a unified diff for PII and
// secrets using the alcatraz library: in-process, no service, no network
// calls.
//
// Usage:
//
//	alcatraz scan [flags] [path ...]   scan files line by line (no paths: read stdin)
//	alcatraz diff [flags]              read a unified diff on stdin, scan added lines
//	alcatraz hook claude-post [flags]  PostToolUse masking hook for Claude Code
//	alcatraz hook claude-prompt [flags]
//	                                   UserPromptSubmit guard for Claude Code
//	alcatraz models download [flags]   fetch a verified NER model
//	alcatraz models verify [flags]     re-check an already-seeded model
//	alcatraz version                   print the version (also -version, --version)
//
// Scanning is the network-free part, and it is the whole product: no
// telemetry, no lookups, nothing leaves the process. "models download" is the
// single exception, and it exists so a deployment can fetch the optional NER
// model as its own step, from pinned URLs, instead of on first use.
//
// Detected values are always masked in the output; the raw values never
// leave the scan. Masking keeps at most the first and last two characters
// ("45************66"), in text and JSON output alike.
//
// Exit codes are grep-style: 0 = no findings, 1 = findings detected, 2 =
// error. Hook mode always exits 0 and reports findings through hook output
// rather than exit codes, because Claude Code reads a non-zero hook exit as
// a session error.
//
// # Scan and diff
//
// Both modes share one flag set:
//
//	-threshold float
//		minimum confidence score in [0,1] (default 0.4)
//	-entities string
//		comma-separated entity types to restrict to (empty = all)
//	-ignore string
//		comma-separated entity types to drop as noise
//		(default "DATE_TIME,URL")
//	-allowlist-file string
//		file with allowed values, one per line (# comments ok)
//	-json
//		emit the findings as JSON instead of text
//	-exclude string
//		comma-separated glob patterns of diff paths to skip (diff only).
//		A pattern matches the full path or its basename, and "dir/**"
//		matches everything under dir.
//	-context
//		score a match higher when a word naming its entity type precedes
//		it (default true; -context=false for pattern-only scores)
//
// scan reads the named files, or stdin when given no paths; findings from
// stdin carry a line number but no file. diff reads a unified diff on stdin
// and analyzes only added lines, reporting each at the line it lands on in
// the new version.
//
// Text output is one line per finding plus a count:
//
//	user.go:11  CREDIT_CARD  45************66  0.92
//	alcatraz: 1 finding(s)
//
// With -json the same findings are emitted as {"findings": [...], "total": n},
// each entry carrying file, line, entity_type, value_masked and score.
//
// -context=false exists for callers who read a threshold as a statement about
// pattern strength alone ("0.8 means checksum-validated"), since context can
// lift a labelled email or phone over that line. The Go API says the same
// thing with Engine.SetContextEnhancer(nil).
//
// # Claude Code hooks
//
// Both hook processors read one hook-event JSON document on stdin and write a
// hook-output JSON document on stdout, or nothing at all, meaning "no
// opinion". A masking hook must never break the session it protects, so every
// internal error (unparsable input, a payload above 10MiB, a failed chain
// command) degrades to passing the tool result through untouched, with the
// complaint on stderr and an exit code of 0.
//
// Shared flags:
//
//	-threshold float
//		minimum confidence score in [0,1] (default 0.5)
//	-entities string
//		comma-separated entity types to restrict to (empty = all)
//	-ignore string
//		comma-separated entity types to drop as noise
//		(default "DATE_TIME,URL,IP_ADDRESS")
//	-context
//		score a match higher when a word naming its entity type precedes
//		it (default true; -context=false for pattern-only scores)
//
// alcatraz hook claude-post handles PostToolUse: it walks the decoded tool
// response and masks PII in every string leaf, returning it as
// updatedToolOutput before it enters model context. String values under
// path-carrying keys (filenames, filePath, file_path, path) pass through
// unmasked, since masking a path breaks every follow-up tool call on it. It
// adds:
//
//	-skip-tools string
//		comma-separated tool names whose outputs are never masked
//		(default "Read": fresh file content feeds exact-match edits)
//	-chain string
//		upstream rewriter to run first (whitespace-split command, e.g.
//		"julius hook claude-post"); its updatedToolOutput is masked
//		instead of the raw result, so two rewriters never race on the
//		same event
//
// alcatraz hook claude-prompt handles UserPromptSubmit, reacting when the
// user's own prompt carries PII. It adds:
//
//	-mode string
//		what to do on findings: warn or block (default "warn")
//
// In warn mode the prompt proceeds with a system message and a note telling
// the model not to repeat the values; in block mode the prompt is rejected
// with a masked view of what tripped it.
//
// # Model download and verify
//
// alcatraz models download fetches the optional NER backend's model and
// verifies every file against its pinned sha256. This is the one alcatraz
// command that touches the network, and it only fetches the pinned URLs.
//
//	-dest string
//		directory to write the model into (empty = the cache ner reads
//		from)
//	-model string
//		model id to download (default "KnightsAnalytics/distilbert-NER")
//	-origin string
//		base URL to fetch from, laid out like the hub (empty = the
//		model's pinned origin)
//
// Without -dest it warms the cache ner.New reads from; with -dest it writes
// a self-contained directory to copy into an image or a shared volume.
// Re-running it re-verifies the cached files in place, with no download. On
// success it prints the models directory and this model's own directory, the
// two paths a deployment wires into its config. SIGINT or SIGTERM cancels
// the fetch so an evicted init container does not strand a partial file.
//
// -origin points the fetch at a mirror — an internal bucket, an air-gapped
// cache — without the pin table having to know about that build. Only the
// base URL moves: the layout under it stays
// {origin}/{model}/resolve/{revision}/{file}, so a mirror is a bucket keyed
// like the hub rather than a second code path. The pinned digests are
// unchanged, so a mirror serving anything else fails exactly as a corrupted
// transfer does and installs nothing — an origin is trusted for
// availability, never for content. The origin actually used is printed
// before the fetch, and every pinned model is listed with its own in the
// usage text.
//
// alcatraz models verify re-checks a directory download already filled,
// without fetching anything:
//
//	-dir string
//		models directory to check (empty = the cache ner reads from)
//	-dest string
//		alias for -dir, so a line lifted out of a download runbook runs
//		unedited
//	-model string
//		model id to check (default "KnightsAnalytics/distilbert-NER")
//
// It opens no socket and writes nothing — not even the default cache
// directory, which it names but does not create — so it is safe against a
// read-only mount and inside a network-sealed build. It exits non-zero
// naming the first file that is absent, mismatched or unreadable, three
// problems worth telling apart: absent means the directory was never seeded,
// mismatched means it was seeded with the wrong bytes, and unreadable means
// the bytes may be fine and the process cannot see them. Two callers need
// this rather than a second download: a CI job proving an image really
// carries the model it claims to, asserted from outside because a distroless
// image has no shell, and an operator diagnosing a volume the model runtime
// rejected.
//
// -dir is the models directory, the same one download fills with -dest.
// Handing it a model's own directory is the mistake the ModelsDir/ModelPath
// split exists to prevent, so the failure says so instead of advising a
// download that would nest a second copy inside the first. It is spelled -dir
// rather than -dest because this command writes nothing and so has no
// destination, but -dest names the same directory and is accepted: the two
// commands sit next to each other in every runbook that has either.
//
// The heavy lifting is [models.EnsureModelFrom] and [models.VerifyModelIn],
// which live in the root module so these commands cost the CLI no
// dependencies: the ONNX runtime stays behind the ner module.
package main
