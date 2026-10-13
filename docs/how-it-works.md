# How it works

```
text  →  recognizers (regex)  →  validators (checksum)  →  context scoring  →  dedup  →  threshold + allow list  →  results
```

The pipeline:

1. Every applicable recognizer runs its regexes over the text. Three
   shortcuts keep this cheap: a pattern whose matches must contain a byte
   class the text lacks (a digit, an `@`, a `:`) is skipped without a scan;
   patterns that share a regex source share one scan per call; and a
   pattern of the exact form `\b\d{n}\b` (sixteen national identifiers)
   is served from a single pass that finds every bare digit run once. None
   of them changes what is detected.
2. The engine scores a matched span at the pattern's base confidence; a
   validator then either promotes it to `1.0` (verified) or drops it (failed
   checksum).
3. Words around a match that support it raise its score; see
   [context-aware scoring](context-scoring.md).
4. Overlapping spans **of the same entity type** are de-duplicated (the
   enclosing/higher-scoring span wins). Different entity types never suppress
   each other.
5. An optional score threshold and allow list filter what remains.
6. Each surviving result carries the matched substring (`Result.Text`).

## Reading a `Result`

```go
type Result struct {
    EntityType     string  // e.g. entities.CreditCard
    Start, End     int     // byte offsets: text[Start:End] == Text
    Score          float64
    Text           string
    RecognizerName string
}
```

Offsets are **byte** indices, not runes, and this holds for model-backed
results too, including multi-byte input. See [Design & limits](design.md) for
what the scores do and do not mean.
