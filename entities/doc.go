// Package entities defines the canonical entity-type identifiers alcatraz can
// detect. The names follow the widely used SCREAMING_SNAKE_CASE convention for
// PII entity types so reports and downstream severity maps stay compatible
// across implementations.
//
// Use these constants instead of literal strings when narrowing a scan with
// Options.Entities or when switching on a Result.EntityType:
//
//	results := eng.Analyze(text, alcatraz.Options{
//		Entities: []string{entities.CreditCard, entities.USSSN},
//	})
//
// This package groups the constants by jurisdiction: generic,
// language-independent entities first, then per-country identifiers. Every
// value is an untyped string constant, so it is usable anywhere a string is.
//
// The entity types an engine can emit depend on the recognizers loaded for
// the analyzed language; ask [github.com/hoophq/alcatraz.Engine] via its
// SupportedEntities method. PERSON, LOCATION and NRP appear only when you
// wire in the optional github.com/hoophq/alcatraz/ner module.
package entities
