# Context-aware scoring

A regex cannot tell a card number in a payment form from the same digits in a
log line, so most patterns carry a base score well below certainty. The words
around a match supply that missing evidence, and each recognizer declares the
ones that matter:

```go
recognizers.Email() // base score 0.5, context words "email", "mail"
```

A context word in the five words before a match raises the score by `0.35`
(floor `0.4`, capped at `1.0`), the same numbers as Presidio's
`LemmaContextAwareEnhancer`:

```go
eng.Analyze("jane@example.com", alcatraz.Options{})        // EMAIL_ADDRESS 0.50
eng.Analyze("email: jane@example.com", alcatraz.Options{}) // EMAIL_ADDRESS 0.85
```

This is why a threshold above `0.5` is worth setting: without context scoring
a pattern-only `EMAIL_ADDRESS` could never exceed its base score, so such a
threshold would match nothing and report no error.

## Whole-word matching

Matching is case-insensitive and by **whole word**, with English plurals folded
in (`cards` supports a `card` context, `addresses` supports `address`).
Presidio instead asks whether the context word occurs anywhere inside a token,
which fires on unrelated words: `ip` is inside `recipient`, `zip` and
`script`. Multi-word entries (`"social security"`, `"codice fiscale"`) match as
consecutive words. The enhancer needs no NLP backend: it reads the words
straight from the text, so this works in the pattern-only engine.

## Tuning it

Scores already at `1.0` (anything a checksum validator verified) stay
untouched. Tune or switch off the behaviour per engine:

```go
eng.SetContextEnhancer(nil) // score purely on the pattern

enh := analyzer.NewWordContextEnhancer()
enh.WordsAfter = 3          // Presidio reads none; labels usually precede values
eng.SetContextEnhancer(enh)
```

On the CLI, `-context=false` does the same thing. Reach for it if you read a
threshold as a statement about pattern strength alone ("0.8 means a checksum
validated it"), since a labelled email or phone now clears that line on the
words around it.

Your own recognizers get the same treatment; see
[Custom recognizers](custom-recognizers.md).
