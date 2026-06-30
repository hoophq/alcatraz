package recognizers

import (
	"github.com/hoophq/alcatraz/analyzer"
	"github.com/hoophq/alcatraz/entities"
)

// SGFIN detects Singapore Foreign Identification Numbers and validates the
// check letter.
func SGFIN() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"SgFinRecognizer", entities.SGFIN, "en",
		[]*analyzer.Pattern{analyzer.MustPattern("SG FIN", `\b[FGM]\d{7}[A-Z]\b`, 0.7)},
	).WithContext("fin", "singapore").WithValidator(validateSGFIN)
}

func validateSGFIN(s string) bool {
	if len(s) != 9 {
		return false
	}
	first := s[0]
	if first != 'F' && first != 'G' && first != 'M' {
		return false
	}
	ds := digitValues(s[1:8])
	if len(ds) != 7 {
		return false
	}
	weights := [7]int{2, 7, 6, 5, 4, 3, 2}
	sum := 0
	for i, d := range ds {
		sum += d * weights[i]
	}
	if first == 'M' {
		sum += 3
	}
	checkLetters := "XWUTRQPNMLK" // F or G
	if first == 'M' {
		checkLetters = "KLJNPQRTUWX"
	}
	return checkLetters[sum%11] == s[8]
}

// SGUEN detects Singapore Unique Entity Numbers (business and local company
// formats).
func SGUEN() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"SgUenRecognizer", entities.SGUEN, "en",
		[]*analyzer.Pattern{
			analyzer.MustPattern("SG UEN (Business)", `\b\d{8,9}[A-Z]\b`, 0.5),
			analyzer.MustPattern("SG UEN (Company)", `\b[A-Z]{1}\d{2}[A-Z]{2}\d{4}[A-Z]\b`, 0.7),
		},
	).WithContext("uen", "singapore", "company")
}
