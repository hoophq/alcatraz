# Custom recognizers

Add your own detector by implementing `analyzer.Recognizer` (or reuse
`analyzer.PatternRecognizer`) and registering it:

```go
reg := analyzer.NewRegistry("en")
recognizers.LoadDefaults(reg, "en")          // built-ins (optional)
reg.Add("en", analyzer.NewPatternRecognizer(
    "InternalIDRecognizer", "INTERNAL_ID", "en",
    []*analyzer.Pattern{analyzer.MustPattern("internal-id", `\bEMP-\d{6}\b`, 0.9)},
).WithValidator(myChecksum))

eng := analyzer.NewEngine(reg, []string{"en"})
```

`WithContext("employee", "emp id", "staff")` declares the words that support
the match; see [context-aware scoring](context-scoring.md).

The `Recognizer` interface is the seam for statistical backends too; nothing
in the framework assumes regex. The [`alcatraz/ner`](ner.md) module plugs in
through the same interface.

## Emulating lookaround without a backtracking engine

Go's RE2 `regexp` omits lookaround and backreferences by design, and that
omission is what guarantees linear-time matching. Two pure-Go tools cover most
lookaround needs; the third option is the
[`lookaround` module](lookaround.md).

**A. Context-aware validator.** For "match X only when surrounded by Y". The
validator sees the full text and the match's byte span:

```go
rec := analyzer.NewPatternRecognizer("PinRule", "PIN", "en",
    []*analyzer.Pattern{analyzer.MustPattern("pin", `\d{4}`, 0.5)},
).WithContextValidator(func(text string, start, end int) bool {
    return strings.HasSuffix(text[:start], "PIN ") // emulates (?<=PIN )
})
```

It is a *filter* (keep/drop) and never inflates the score the way a checksum
`WithValidator` does; the two compose if you need both.

**B. Capture-group span.** Match the surrounding context but report only the
captured entity. `WithGroup(n)` selects which group becomes the result span:

```go
// Emulates (?<=user=)\w+ : require the prefix, emit only the value.
p := analyzer.MustPattern("user", `user=(\w+)`, 0.9).WithGroup(1)
```

None of the 45 built-ins need more than A + B: they rely on `\b` anchors plus
validators and same-entity dedup.

## Custom matchers

`analyzer.NewPatternMatcher` backs a pattern with your own `analyzer.Matcher`
(the `lookaround` module is one). `FindAll` returns `analyzer.Match` values
built with `analyzer.NewMatch(loc []int)`, where `loc` holds start/end byte
offset pairs in `regexp.FindStringSubmatchIndex` layout — group 0 first,
`-1, -1` for a group that did not participate:

```go
func (m myMatcher) FindAll(text string) []analyzer.Match {
    var out []analyzer.Match
    for _, span := range m.scan(text) { // your engine, byte offsets
        out = append(out, analyzer.NewMatch([]int{span.start, span.end}))
    }
    return out
}
```

`Match` used to expose a `Groups [][2]int` field; `NewMatch` replaces it so
the default matcher can hand the standard library's offsets through without
copying.
