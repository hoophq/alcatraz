# The CLI

The library also ships as a small command-line scanner with the same engine
and the same zero-network scan. Scan files, stdin, or a unified diff; detected
values are always masked in the output.

## Install

```bash
# Homebrew (macOS & Linux)
brew install hoophq/tap/alcatraz

# or grab a checksum-verified binary from GitHub releases, or build from source
go install github.com/hoophq/alcatraz/cmd/alcatraz@latest
```

## Commands

```
alcatraz scan [flags] [file...]      scan files, or stdin when no file is given
alcatraz diff [flags]                scan only the lines a unified diff adds
alcatraz hook claude-post [flags]    Claude Code PostToolUse output rewriter
alcatraz hook claude-prompt [flags]  Claude Code UserPromptSubmit guard
alcatraz models download [flags]     fetch and verify the optional NER model
alcatraz version                     print the version (also -version/--version)
```

```bash
alcatraz scan secrets.log app.log      # scan files line by line
git diff | alcatraz diff               # scan only the lines a diff adds
pbpaste | alcatraz scan                # scan pasted text from stdin
alcatraz scan -json report.log         # machine-readable output (masked too)
```

## Exit codes

Grep-style: `0` clean, `1` findings, `2` error.

**Hook mode always exits `0`**: findings travel in the hook's output, never in
the exit code, so a detection never fails the surrounding tool. That holds even
for bad flags, an oversized payload or a failed `-chain`.

## Flags: `scan` and `diff`

| Flag | Default | Meaning |
|---|---|---|
| `-threshold` | `0.4` | drop results scoring below this |
| `-entities` | all | comma-separated entity types to restrict to |
| `-ignore` | `DATE_TIME,URL` | entity types to suppress |
| `-allowlist-file` | none | one allowed value per line, `#` comments |
| `-json` | `false` | machine-readable output (values still masked) |
| `-exclude` | none | glob patterns of paths to skip; consulted by `diff` only |
| `-context` | `true` | context-aware scoring; `-context=false` scores each match on its pattern alone |

See [Context-aware scoring](context-scoring.md) for what `-context` changes.

## Flags: `hook claude-post`

Masks PII in tool outputs before they enter model context.

| Flag | Default | Meaning |
|---|---|---|
| `-threshold` | `0.5` | drop results scoring below this |
| `-entities` | all | comma-separated entity types to restrict to |
| `-ignore` | `DATE_TIME,URL,IP_ADDRESS` | entity types to suppress |
| `-context` | `true` | context-aware scoring |
| `-skip-tools` | `Read` | comma-separated tool names whose output is left alone |
| `-chain` | none | upstream rewriter to compose with, so two output rewriters never race |

## Flags: `hook claude-prompt`

Warns or blocks when the user's own prompt carries PII.

| Flag | Default | Meaning |
|---|---|---|
| `-threshold` | `0.5` | drop results scoring below this |
| `-entities` | all | comma-separated entity types to restrict to |
| `-ignore` | `DATE_TIME,URL,IP_ADDRESS` | entity types to suppress |
| `-context` | `true` | context-aware scoring |
| `-mode` | `warn` | `warn` annotates the prompt; `block` rejects it |

> [!NOTE]
> The hook defaults are stricter than `scan`: threshold `0.5` instead of
> `0.4`, and `IP_ADDRESS` added to `-ignore` because tool output is full of
> addresses that are not PII.

## Flags: `models download`

| Flag | Default | Meaning |
|---|---|---|
| `--dest` | the cache `ner.New` reads from | install into a self-contained directory instead |
| `--model` | `KnightsAnalytics/distilbert-NER` | which pinned model to fetch |
| `--origin` | the model's pinned origin | fetch from a mirror laid out like the hub |

All accept a single dash too; the double-dash form is what `ner`'s own error
messages tell you to run, so it is the spelling used throughout these docs.

`--origin` points the fetch at an internal bucket or an air-gapped cache. Only
the base URL moves — the layout under it stays
`{origin}/{model}/resolve/{revision}/{file}` — so a mirror is a bucket keyed
like the hub, not a different command. The pinned digests are unchanged, so a
mirror serving anything else fails exactly as a corrupt transfer does and
installs nothing. Run without the flag to see each pinned model's origin in the
usage text; the origin actually used is printed before the fetch starts.

See [Running NER without internet access](ner-offline.md) for the deployment
patterns this command exists for.

## Network

Scanning never touches the network: no telemetry, no lookups, nothing leaves
the process. The single exception is `alcatraz models download`, which fetches
the optional NER model from pinned, checksum-verified URLs when you ask it to.

## Integrations

The CLI powers the [Hoop plugin for Claude Code](https://github.com/hoophq/claude-marketplace)'s
`/hoop:pii-scan` command and pairs with
[alcatraz-action](https://github.com/hoophq/alcatraz-action) for CI. The Hoop
plugin wires both hook processors up without extra configuration.
