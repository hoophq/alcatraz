# Install & quickstart

```bash
go get github.com/hoophq/alcatraz
```

Requires Go 1.24+. The standard library is the only dependency.

Each optional module carries its own `go.mod` and version; your build pulls one
in only when you import it:

```bash
go get github.com/hoophq/alcatraz/ner         # statistical NER (Go 1.26.5+)
go get github.com/hoophq/alcatraz/pfilter     # privacy-filter.cpp backend
go get github.com/hoophq/alcatraz/lookaround  # regexp2 lookahead/lookbehind
```

## Quickstart

```go
// Build an engine with the full built-in recognizer set (English by default).
eng := alcatraz.NewEngine()

results := eng.Analyze(text, alcatraz.Options{
    Entities:       []string{entities.CreditCard}, // optional: restrict types
    Threshold:      ptr(0.4),                      // optional: drop low scores
    AllowList:      []string{"4111111111111111"},  // optional: ignore values
    AllowListRegex: false,                         // treat AllowList as regex
})

for _, r := range results {
    // r.EntityType, r.Start, r.End, r.Score, r.Text, r.RecognizerName
}
```

`Options{}` (the zero value) analyzes with every recognizer and no threshold.
`Result` offsets are byte indices, so `text[r.Start:r.End] == r.Text`.

An engine is safe to build once and reuse; construction compiles every regex,
so avoid calling `NewEngine` per request.

## Next steps

- [What it detects](entities.md): the 45 entity types.
- [Context-aware scoring](context-scoring.md): why a bare email scores 0.5.
- [Anonymize](anonymize.md): turn spans into sanitized text.
