package recognizers

import (
	"unicode"
	"unicode/utf8"

	"github.com/hoophq/alcatraz/analyzer"
	"github.com/hoophq/alcatraz/entities"
)

// UKNHS detects UK NHS numbers and validates the modulus-11 checksum.
func UKNHS() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"UkNhsRecognizer", entities.UKNHS, "en",
		[]*analyzer.Pattern{analyzer.MustPattern("NHS Number", `\b\d{3}`+hsp+`?\d{3}`+hsp+`?\d{4}\b`, 0.6)},
	).WithContext("nhs", "health").WithValidator(validateNHS)
}

func validateNHS(s string) bool {
	var buf [10]int
	ds, ok := digitsExactly(buf[:0], s, 10)
	if !ok {
		return false
	}
	sum := 0
	for i := 0; i < 9; i++ {
		sum += ds[i] * (10 - i)
	}
	check := 11 - (sum % 11)
	if check == 11 {
		check = 0
	}
	// a check digit of 10 marks an invalid number; it can never equal ds[9].
	return check == ds[9]
}

// UKNINO detects UK National Insurance Numbers.
func UKNINO() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"UkNinoRecognizer", entities.UKNINO, "en",
		[]*analyzer.Pattern{analyzer.MustPattern("NINO",
			`\b[A-CEGHJ-PR-TW-Z][A-CEGHJ-NPR-TW-Z]`+hsp+`?\d{2}`+hsp+`?\d{2}`+hsp+`?\d{2}`+hsp+`?[A-D]\b`, 0.7)},
	).WithContext("nino", "national insurance").WithValidator(validateNINO)
}

func validateNINO(s string) bool {
	// Compact the match into nino, dropping every Unicode space (the pattern
	// allows \p{Zs} between groups); more than nine other bytes is a miss.
	var nino [9]byte
	n := 0
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			i++
			if unicode.IsSpace(rune(c)) {
				continue
			}
			if n == len(nino) {
				return false
			}
			nino[n] = c
			n++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if unicode.IsSpace(r) {
			i += size
			continue
		}
		if n+size > len(nino) {
			return false
		}
		n += copy(nino[n:], s[i:i+size])
		i += size
	}
	if n != len(nino) {
		return false
	}
	switch string(nino[:2]) {
	case "BG", "GB", "NK", "KN", "TN", "NT", "ZZ":
		return false
	}
	switch nino[8] {
	case 'A', 'B', 'C', 'D':
		return true
	}
	return false
}
