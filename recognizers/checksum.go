// Package recognizers provides the built-in alcatraz entity recognizers and a
// loader that registers them by language. Each recognizer is a
// analyzer.PatternRecognizer: one or more regexes plus, for structured
// entities, a checksum/format validator.
package recognizers

import "strings"

// stripSeparators removes the dashes and spaces commonly used to group an
// identifier before checksum validation.
func stripSeparators(s string) string {
	return strings.NewReplacer("-", "", " ", "").Replace(s)
}

// digitValues returns the decimal digits of s as integers, ignoring any other
// characters.
func digitValues(s string) []int {
	out := make([]int, 0, len(s))
	for i := 0; i < len(s); i++ {
		if c := s[i]; c >= '0' && c <= '9' {
			out = append(out, int(c-'0'))
		}
	}
	return out
}

// digitsExactly returns the decimal digits of s and reports whether there are
// exactly n of them. It is the common guard for fixed-length checksums.
func digitsExactly(s string, n int) ([]int, bool) {
	ds := digitValues(s)
	if len(ds) != n {
		return nil, false
	}
	return ds, true
}

// Verhoeff multiplication (d) and permutation (p) tables.
var verhoeffD = [10][10]int{
	{0, 1, 2, 3, 4, 5, 6, 7, 8, 9},
	{1, 2, 3, 4, 0, 6, 7, 8, 9, 5},
	{2, 3, 4, 0, 1, 7, 8, 9, 5, 6},
	{3, 4, 0, 1, 2, 8, 9, 5, 6, 7},
	{4, 0, 1, 2, 3, 9, 5, 6, 7, 8},
	{5, 9, 8, 7, 6, 0, 4, 3, 2, 1},
	{6, 5, 9, 8, 7, 1, 0, 4, 3, 2},
	{7, 6, 5, 9, 8, 2, 1, 0, 4, 3},
	{8, 7, 6, 5, 9, 3, 2, 1, 0, 4},
	{9, 8, 7, 6, 5, 4, 3, 2, 1, 0},
}

var verhoeffP = [8][10]int{
	{0, 1, 2, 3, 4, 5, 6, 7, 8, 9},
	{1, 5, 7, 6, 2, 8, 3, 0, 9, 4},
	{5, 8, 0, 3, 7, 9, 6, 1, 4, 2},
	{8, 9, 1, 6, 0, 4, 3, 5, 2, 7},
	{9, 4, 5, 3, 1, 2, 6, 8, 7, 0},
	{4, 2, 8, 6, 5, 7, 3, 9, 0, 1},
	{2, 7, 9, 3, 8, 0, 6, 4, 1, 5},
	{7, 0, 4, 6, 9, 1, 3, 2, 5, 8},
}

// verhoeffValid runs the Verhoeff checksum over the decimal digits of s
// (the check digit must be included). It returns true when the checksum is 0.
func verhoeffValid(s string) bool {
	ds := digitValues(s)
	c := 0
	for i, idx := 0, len(ds)-1; idx >= 0; i, idx = i+1, idx-1 {
		c = verhoeffD[c][verhoeffP[i%8][ds[idx]]]
	}
	return c == 0
}

// luhnValid runs the Luhn (mod-10) checksum over the decimal digits of s.
func luhnValid(s string) bool {
	ds := digitValues(s)
	if len(ds) == 0 {
		return false
	}
	sum := 0
	alt := false
	for i := len(ds) - 1; i >= 0; i-- {
		n := ds[i]
		if alt {
			if n *= 2; n > 9 {
				n -= 9
			}
		}
		sum += n
		alt = !alt
	}
	return sum%10 == 0
}
