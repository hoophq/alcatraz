// Package analyzer is the detection framework for alcatraz: the recognizer
// contract, regex pattern recognizers, the registry and the engine that runs
// them. It is pure Go and dependency-free.
//
// The framework stays separate from the concrete recognizers (which live in
// the recognizers package) so callers can build a custom engine with only
// the recognizers they want, or add their own via the [Recognizer]
// interface. Most callers should use the top-level alcatraz package, which
// wires the framework together with the full default recognizer set.
//
// # The extension seam
//
// [Recognizer] is the whole contract a detector has to satisfy: a name, the
// entity types it emits, the language it is registered under, and an Analyze
// method returning byte-offset [Result] spans. Anything implementing it (a
// regex, a checksum walker, a model) is a first-class citizen of the engine.
//
// # The batteries-included path
//
// Most detectors never need a custom [Recognizer]. [NewPatternRecognizer]
// builds one for a single entity type out of [Pattern] values, each a named
// regex with a base confidence score; [MustPattern] compiles one at package
// scope and panics on an invalid regex, [NewPattern] returns the error
// instead. Three chainable options refine a pattern recognizer:
//
//   - [PatternRecognizer.WithValidator] attaches a structural check
//     (a checksum, a range rule). Returning true promotes the match to
//     [MaxScore]; returning false drops it.
//   - [PatternRecognizer.WithContextValidator] attaches a filter that sees
//     the full text and the match span, the pure-Go way to express
//     lookaround ("keep this only if preceded by 'PIN '"). It only filters;
//     it never changes the score.
//   - [PatternRecognizer.WithContext] declares the words a value is usually
//     labelled with, which the engine turns into score (see below).
//
// The standard library's RE2 engine has no lookaround and no backreferences.
// Two ways around it: [Pattern.WithGroup] reports a capture group instead of
// the whole match, so a pattern can match surrounding context and emit only
// the entity; and [NewPatternMatcher] backs a pattern with any [Matcher]
// implementation, which is how the alcatraz/lookaround module supplies a
// backtracking engine.
//
// # Wiring
//
// [Registry] holds recognizers keyed by language. You pass that key yourself
// at [Registry.Add] time, because structured-identifier recognizers are
// language-independent and are usually registered under every analyzed
// language. [NewEngine] wraps a registry; [Engine.Analyze] runs
// every applicable recognizer, applies the [ContextEnhancer], de-duplicates,
// applies the score threshold, then the allow list, and fills in each
// result's Text. [Options] tunes one call (entity filter, language,
// threshold, allow list); its zero value analyzes English with every
// recognizer and no threshold. [Engine.AnalyzeBatch] does the same over
// several texts, collapsing the NLP pass into one inference call when the
// backend implements [BatchNlpEngine]. An [Engine] is safe for concurrent
// use once configured.
//
// # Results
//
// A [Result] carries an entity type, a byte-offset span, a confidence score
// in [MinScore, MaxScore] and the name of the recognizer that produced it.
// Offsets are byte indices, so text[Start:End] is the matched substring.
// [RemoveDuplicates] collapses overlapping detections of the same entity
// type, keeping the highest-scoring span and dropping spans contained within
// a kept one; detections of different entity types never suppress each other,
// so an overlapping PERSON and ORGANIZATION both survive.
//
// # Context scoring
//
// A regex alone cannot tell a card number in a payment form from the same
// digits in a log line, so most patterns carry a base score well below
// certainty. The [ContextEnhancer] hook reads that missing evidence out of
// the surrounding words: [NewWordContextEnhancer], which [NewEngine]
// installs by default, adds [DefaultContextBoost] to a result whose
// recognizer declared a matching word via [PatternRecognizer.WithContext],
// raising it to at least [DefaultContextMinScore] and capping at [MaxScore].
// The window is [DefaultContextWordsBefore] words before the match and
// [DefaultContextWordsAfter] after; both are fields on
// [WordContextEnhancer]. Pass nil to [Engine.SetContextEnhancer] to score on
// the pattern alone. Words come from a built-in scanner over the raw text,
// so this needs no NLP backend. Custom recognizers opt in by implementing
// [ContextualRecognizer].
//
// # The NLP seam
//
// [NlpEngine] is where a statistical backend plugs in. [Engine.SetNlpEngine]
// attaches one; the engine then runs it at most once per Analyze call, and
// only when an applicable recognizer implements [ArtifactRecognizer], sharing
// the resulting [NlpArtifacts] ([Token] and [NerSpan] values) with every such
// recognizer so several artifact-aware detectors never trigger duplicate
// model runs. Implementations live in separate modules (the ner module for
// transformer NER, and the pfilter module), so importing this package never
// pulls in a model runtime.
package analyzer
