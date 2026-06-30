package recognizers

import (
	"github.com/hoophq/alcatraz/analyzer"
	"github.com/hoophq/alcatraz/entities"
)

// AUTFN detects Australian Tax File Numbers and validates the weighted
// modulus-11 checksum.
func AUTFN() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"AuTfnRecognizer", entities.AUTFN, "en",
		[]*analyzer.Pattern{analyzer.MustPattern("AU TFN", `\b\d{3}\s?\d{3}\s?\d{3}\b`, 0.3)},
	).WithContext("tfn", "tax file").WithValidator(validateTFN)
}

func validateTFN(s string) bool {
	ds, ok := digitsExactly(s, 9)
	if !ok {
		return false
	}
	weights := [9]int{1, 4, 3, 7, 5, 8, 6, 9, 10}
	sum := 0
	for i, d := range ds {
		sum += d * weights[i]
	}
	return sum%11 == 0
}

// AUABN detects Australian Business Numbers and validates the modulus-89
// checksum.
func AUABN() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"AuAbnRecognizer", entities.AUABN, "en",
		[]*analyzer.Pattern{analyzer.MustPattern("AU ABN", `\b\d{2}\s?\d{3}\s?\d{3}\s?\d{3}\b`, 0.3)},
	).WithContext("abn", "business number").WithValidator(validateABN)
}

func validateABN(s string) bool {
	ds, ok := digitsExactly(s, 11)
	if !ok {
		return false
	}
	if ds[0] > 0 {
		ds[0]-- // subtract 1 from the leading digit
	}
	weights := [11]int{10, 1, 3, 5, 7, 9, 11, 13, 15, 17, 19}
	sum := 0
	for i, d := range ds {
		sum += d * weights[i]
	}
	return sum%89 == 0
}

// AUACN detects Australian Company Numbers and validates the checksum.
func AUACN() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"AuAcnRecognizer", entities.AUACN, "en",
		[]*analyzer.Pattern{analyzer.MustPattern("AU ACN", `\b\d{3}\s?\d{3}\s?\d{3}\b`, 0.3)},
	).WithContext("acn", "company number").WithValidator(validateACN)
}

func validateACN(s string) bool {
	ds, ok := digitsExactly(s, 9)
	if !ok {
		return false
	}
	weights := [8]int{8, 7, 6, 5, 4, 3, 2, 1}
	sum := 0
	for i := 0; i < 8; i++ {
		sum += ds[i] * weights[i]
	}
	complement := (10 - (sum % 10)) % 10
	return complement == ds[8]
}

// AUMedicare detects Australian Medicare numbers and validates the checksum.
func AUMedicare() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"AuMedicareRecognizer", entities.AUMedicare, "en",
		[]*analyzer.Pattern{analyzer.MustPattern("AU Medicare", `\b\d{4}\s?\d{5}\s?\d{1}\b`, 0.6)},
	).WithContext("medicare", "health").WithValidator(validateMedicare)
}

func validateMedicare(s string) bool {
	ds, ok := digitsExactly(s, 10)
	if !ok {
		return false
	}
	if ds[0] < 2 || ds[0] > 6 {
		return false
	}
	weights := [8]int{1, 3, 7, 9, 1, 3, 7, 9}
	sum := 0
	for i := 0; i < 8; i++ {
		sum += ds[i] * weights[i]
	}
	return sum%10 == ds[8]
}
