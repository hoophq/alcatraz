# Anonymize: mask, replace, redact

Detection gives you spans; the `anonymizer` package turns them into sanitized
text. Pick an operator: mask with the character of your choice (`#`, `*`, …),
keep a recognizable tail, replace with a placeholder, or redact. Apply it to
the results of an `Analyze` call:

```go
import "github.com/hoophq/alcatraz/anonymizer"

text := "Email jane@example.com, card 4532015112830366, ssn 536-90-4399."
results := eng.Analyze(text, alcatraz.Options{})

anonymizer.Anonymize(text, results, anonymizer.Mask('*'))
// Email ****************, card ****************, ssn ***********.

anonymizer.AnonymizeWith(text, results, anonymizer.Config{
    Default: anonymizer.Replace(), // <ENTITY_TYPE> placeholders
    PerEntity: map[string]anonymizer.Operator{
        entities.CreditCard: anonymizer.MaskKeepLast('#', 4),
    },
})
// Email <EMAIL_ADDRESS>, card ############0366, ssn <US_SSN>.
```

## Operators

| Operator | Result |
|---|---|
| `Mask(char)` | length-preserving, one mask rune per text rune |
| `MaskKeepLast(char, n)` | masks all but the trailing `n` runes |
| `Replace()` | `<ENTITY_TYPE>` placeholder |
| `ReplaceWith(s)` | a fixed string |
| `Redact()` | removes the span entirely |

An `Operator` is a `func(entityType, match string) string`, so hashing,
tokenization or encryption plug in the same way.

## Overlaps

The anonymizer resolves overlapping spans of different entity types before
replacement: the higher-scoring span wins and the rest is trimmed, never
leaked. The package is pure Go, dependency-free, and part of the core module.
