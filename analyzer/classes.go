package analyzer

import (
	"regexp/syntax"
	"unicode"
)

// classMask is a set of ASCII byte classes. A text's mask records which
// classes occur in it; a pattern's mask records which classes every one of
// its matches must contain. A pattern whose required classes are not all
// present in a text cannot match it, so its scan is skipped.
//
// Bits 0-4 are digit, upper, lower, letter (upper or lower) and space; the
// 32 printable ASCII punctuation bytes each get their own bit above those.
// Bytes outside printable ASCII belong to no class, which keeps the
// derivation conservative for any pattern that can match them.
type classMask uint64

const (
	classDigit classMask = 1 << iota
	classUpper
	classLower
	classLetter
	classSpace
	classPunctBase = iota
)

// byteClass maps every byte to its classes.
var byteClass = buildByteClass()

func buildByteClass() [256]classMask {
	var table [256]classMask
	bit := classPunctBase
	for c := range 256 {
		switch {
		case c >= '0' && c <= '9':
			table[c] = classDigit
		case c >= 'A' && c <= 'Z':
			table[c] = classUpper | classLetter
		case c >= 'a' && c <= 'z':
			table[c] = classLower | classLetter
		case c == ' ':
			table[c] = classSpace
		case c > ' ' && c < 0x7f:
			table[c] = 1 << bit
			bit++
		}
	}
	return table
}

// byteClasses summarizes which classes occur in text. One table lookup per
// byte; it is computed once per analyzed text.
func byteClasses(text string) classMask {
	var m classMask
	for i := range len(text) {
		m |= byteClass[text[i]]
	}
	return m
}

// runeClasses is byteClass for a rune: non-ASCII runes belong to no class.
func runeClasses(r rune) classMask {
	if r < 0 || r >= 0x80 {
		return 0
	}
	return byteClass[r]
}

// patternRequires derives the classes every match of regex must contain, or
// zero when the regex does not parse (a custom Matcher's syntax, for one).
func patternRequires(regex string) classMask {
	re, err := syntax.Parse(regex, syntax.Perl)
	if err != nil {
		return 0
	}
	return requiredClasses(re)
}

// requiredClasses walks a parsed regex and returns the classes that every
// string it matches contains. Only constructs that are provably present in
// every match contribute: a literal or a character class whose members all
// share a class, concatenation (union of parts), alternation (intersection
// of branches) and repetition with a non-zero minimum. Everything else —
// optional parts, anchors, boundaries, any-char — requires nothing.
func requiredClasses(re *syntax.Regexp) classMask {
	switch re.Op {
	case syntax.OpLiteral:
		var m classMask
		fold := re.Flags&syntax.FoldCase != 0
		for _, r := range re.Rune {
			m |= literalClasses(r, fold)
		}
		return m
	case syntax.OpCharClass:
		return charClassClasses(re.Rune)
	case syntax.OpConcat:
		var m classMask
		for _, sub := range re.Sub {
			m |= requiredClasses(sub)
		}
		return m
	case syntax.OpAlternate:
		if len(re.Sub) == 0 {
			return 0
		}
		m := ^classMask(0)
		for _, sub := range re.Sub {
			m &= requiredClasses(sub)
		}
		return m
	case syntax.OpCapture, syntax.OpPlus:
		return requiredClasses(re.Sub[0])
	case syntax.OpRepeat:
		if re.Min > 0 {
			return requiredClasses(re.Sub[0])
		}
	}
	return 0
}

// literalClasses is the class set shared by every rune a literal can match:
// the rune itself, or under case folding its whole fold orbit (which for
// 'k' and 's' includes non-ASCII runes, leaving nothing required).
func literalClasses(r rune, fold bool) classMask {
	m := runeClasses(r)
	if fold {
		for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
			m &= runeClasses(f)
		}
	}
	return m
}

// charClassClasses is the class set shared by every rune in a character
// class, given as inclusive [lo, hi] pairs. Any non-ASCII member means the
// class requires nothing.
func charClassClasses(ranges []rune) classMask {
	if len(ranges) == 0 {
		return 0
	}
	m := ^classMask(0)
	for i := 0; i < len(ranges); i += 2 {
		lo, hi := ranges[i], ranges[i+1]
		if hi >= 0x80 {
			return 0
		}
		for r := lo; r <= hi; r++ {
			m &= byteClass[r]
		}
	}
	return m
}
