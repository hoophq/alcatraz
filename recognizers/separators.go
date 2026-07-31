package recognizers

// hsp matches a single horizontal space separator: the Unicode Zs category
// (U+0020 SPACE, U+00A0 NO-BREAK SPACE, U+202F NARROW NO-BREAK SPACE, the
// U+2000-U+200A range, U+3000 and friends).
//
// Use it — never `\s` — for the optional separators inside a grouped
// identifier, a phone number or a date.
//
// Go's RE2 desugars `\s` to `[\t\n\f\r ]`, so a separator class built on `\s`
// happily bridges a tab or a newline. In the tabular output alcatraz is
// routinely pointed at (psql results, TSV exports, log tables) a tab is a
// column boundary and a newline is a row boundary, and no identifier ever
// spans one. A `\s`-based class stitches two unrelated cells into a single
// match: `100\t000\t0001` reads as a UK NHS number, `09.93009\t2026` as a
// phone number.
//
// That is worse than an ordinary false positive for checksum-backed entities.
// digitValues ignores every non-digit byte, so a bridged run still passes its
// checksum, and analyzer.PatternRecognizer.Analyze then promotes any
// validator-passing match to analyzer.MaxScore — a 1.000 that clears every
// caller threshold and masks the wrong bytes unconditionally.
//
// Filtering boundary-crossing spans after the fact is not an option: on
// "count\t1\t415-555-2671" the greedy match is "1\t415-555-2671", so dropping
// the span would discard a real phone number. The separator class is the only
// place where the distinction can be drawn without losing the true positive.
//
// Zs is preferred over a bare literal space because it keeps the real values
// that carry a non-breaking or narrow no-break space — common when a number is
// pasted out of a web page or formatted for a European locale — while still
// excluding everything vertical.
const hsp = `\p{Zs}`
