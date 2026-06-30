package recognizers

import (
	"strconv"

	"github.com/hoophq/alcatraz/analyzer"
	"github.com/hoophq/alcatraz/entities"
)

// nifLetters maps the modulus-23 result to the NIF/NIE check letter.
const nifLetters = "TRWAGMYFPDXBNJZSQVHLCKE"

// ESNIF detects Spanish NIF/DNI numbers and validates the check letter.
func ESNIF() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"EsNifRecognizer", entities.ESNIF, "es",
		[]*analyzer.Pattern{analyzer.MustPattern("ES NIF", `\b\d{8}[A-Z]\b`, 0.7)},
	).WithContext("nif", "dni").WithValidator(validateESNIF)
}

func validateESNIF(s string) bool {
	if len(s) != 9 {
		return false
	}
	number, err := strconv.Atoi(s[0:8])
	if err != nil {
		return false
	}
	return nifLetters[number%23] == s[8]
}

// ESNIE detects Spanish NIE (foreigner ID) numbers and validates the check
// letter.
func ESNIE() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"EsNieRecognizer", entities.ESNIE, "es",
		[]*analyzer.Pattern{analyzer.MustPattern("ES NIE", `\b[XYZ]\d{7}[A-Z]\b`, 0.7)},
	).WithContext("nie", "extranjero").WithValidator(validateESNIE)
}

func validateESNIE(s string) bool {
	if len(s) != 9 {
		return false
	}
	var lead byte
	switch s[0] {
	case 'X':
		lead = '0'
	case 'Y':
		lead = '1'
	case 'Z':
		lead = '2'
	default:
		return false
	}
	number, err := strconv.Atoi(string(lead) + s[1:8])
	if err != nil {
		return false
	}
	return nifLetters[number%23] == s[8]
}
