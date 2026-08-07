// Package anonymizer replaces detected PII spans in text, turning the
// analyzer's []analyzer.Result into a sanitized string:
//
//	results := eng.Analyze(text, alcatraz.Options{})
//	safe := anonymizer.Anonymize(text, results, anonymizer.Mask('*'))
//
// [Anonymize] applies a single [Operator] to every span; [AnonymizeWith] takes
// a [Config] that picks an operator per entity type, falling back to
// Config.Default (or [Replace] when that is nil) for the rest:
//
//	safe := anonymizer.AnonymizeWith(text, results, anonymizer.Config{
//		Default: anonymizer.Replace(),
//		PerEntity: map[string]anonymizer.Operator{
//			entities.CreditCard: anonymizer.MaskKeepLast('#', 4),
//		},
//	})
//
// # Operators
//
// Operators decide what each span becomes:
//
//   - [Mask] keeps the span's length using a chosen character ('#', '*', …),
//     one mask rune per text rune.
//   - [MaskKeepLast] leaves a recognizable tail visible (last 4 card digits).
//   - [Replace] emits "<ENTITY_TYPE>" placeholders.
//   - [ReplaceWith] emits a fixed string.
//   - [Redact] removes the span.
//
// An [Operator] is a func(entityType, match string) string, so custom
// transforms (hashing, encryption, tokenization) drop in the same way.
//
// # Overlap resolution
//
// The anonymizer resolves overlapping spans without leaking text. A
// higher-scoring span keeps its full extent; a lower-scoring one shrinks to
// the uncovered remainder. Every detected byte is anonymized once, no byte
// is anonymized twice, and a partial overlap never leaks the uncovered part
// of a detection. The operator for a trimmed span receives only the trimmed
// portion as its match argument.
//
// Detections come from the analyzer package, usually through the top-level
// alcatraz engine; entity type names are the constants in the entities
// package.
//
// Like the rest of the core, this package is pure Go and dependency-free.
package anonymizer
