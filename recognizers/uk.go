package recognizers

import (
	"strings"

	"github.com/hoophq/alcatraz/analyzer"
	"github.com/hoophq/alcatraz/entities"
)

// UKNHS detects UK NHS numbers and validates the modulus-11 checksum.
func UKNHS() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"UkNhsRecognizer", entities.UKNHS, "en",
		[]*analyzer.Pattern{analyzer.MustPattern("NHS Number", `\b\d{3}\s?\d{3}\s?\d{4}\b`, 0.6)},
	).WithContext("nhs", "health").WithValidator(validateNHS)
}

func validateNHS(s string) bool {
	ds, ok := digitsExactly(s, 10)
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
			`\b[A-CEGHJ-PR-TW-Z][A-CEGHJ-NPR-TW-Z]\s?\d{2}\s?\d{2}\s?\d{2}\s?[A-D]\b`, 0.7)},
	).WithContext("nino", "national insurance").WithValidator(validateNINO)
}

func validateNINO(s string) bool {
	nino := strings.Join(strings.Fields(s), "")
	if len(nino) != 9 {
		return false
	}
	switch nino[0:2] {
	case "BG", "GB", "NK", "KN", "TN", "NT", "ZZ":
		return false
	}
	switch nino[8] {
	case 'A', 'B', 'C', 'D':
		return true
	}
	return false
}
