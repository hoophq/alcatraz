# Lookahead & lookbehind

The optional **`alcatraz/lookaround`** module handles rules that need
`(?<=…)`, `(?=…)`, `(?!…)` or backreferences, such as regexes supplied in a
config file. It is a *separate module*, so importing it is the only way to
pull in the backtracking engine
([`dlclark/regexp2`](https://github.com/dlclark/regexp2)); the Alcatraz core
stays dependency-free and linear-time.

```bash
go get github.com/hoophq/alcatraz/lookaround   # regexp2 only for importers of this package
```

```go
import "github.com/hoophq/alcatraz/lookaround"

// One call turns user-configured regex rules into a recognizer.
rec, err := lookaround.NewRecognizer("Secret", "API_SECRET", "en",
    lookaround.Spec{Name: "bearer", Regex: `(?<=Bearer )[A-Za-z0-9._-]{8,}`, Score: 0.95},
    lookaround.Spec{Name: "domain", Regex: `(?<=@)(\w+)\.com`, Score: 0.6, Group: 1},
)
reg.Add("en", rec)
```

## ReDoS bound

Backtracking has no linear-time guarantee, so every compiled matcher carries a
`MatchTimeout` (default 1s) to bound catastrophic backtracking (ReDoS); set
your own with `CompileWithTimeout`. Matches report byte offsets in the same
form as the core, so results feed the same `Engine` with no conversion.

## Before you reach for this

Two pure-Go alternatives cover most cases without a backtracking engine: a
context validator and a capture-group span. See
[Custom recognizers](custom-recognizers.md#emulating-lookaround-without-a-backtracking-engine).
