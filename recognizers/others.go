package recognizers

import (
	"strconv"

	"github.com/hoophq/alcatraz/analyzer"
	"github.com/hoophq/alcatraz/entities"
)

// PLPESEL detects Polish PESEL numbers and validates the checksum.
func PLPESEL() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"PlPeselRecognizer", entities.PLPESEL, "pl",
		[]*analyzer.Pattern{analyzer.MustPattern("PL PESEL", `\b\d{11}\b`, 0.3)},
	).WithContext("pesel").WithValidator(validatePESEL)
}

func validatePESEL(s string) bool {
	ds, ok := digitsExactly(s, 11)
	if !ok {
		return false
	}
	weights := [10]int{1, 3, 7, 9, 1, 3, 7, 9, 1, 3}
	sum := 0
	for i := 0; i < 10; i++ {
		sum += ds[i] * weights[i]
	}
	check := (10 - (sum % 10)) % 10
	return check == ds[10]
}

// KRRRN detects Korean Resident Registration Numbers and validates the
// checksum.
func KRRRN() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"KrRrnRecognizer", entities.KRRRN, "ko",
		[]*analyzer.Pattern{analyzer.MustPattern("KR RRN", `\b\d{6}-\d{7}\b`, 0.7)},
	).WithContext("주민등록번호", "rrn").WithValidator(validateKRRRN)
}

func validateKRRRN(s string) bool {
	ds, ok := digitsExactly(s, 13)
	if !ok {
		return false
	}
	weights := [12]int{2, 3, 4, 5, 6, 7, 8, 9, 2, 3, 4, 5}
	sum := 0
	for i := 0; i < 12; i++ {
		sum += ds[i] * weights[i]
	}
	check := (11 - (sum % 11)) % 10
	return check == ds[12]
}

// FIPersonalCode detects Finnish personal identity codes (henkilötunnus) and
// validates the check character.
func FIPersonalCode() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"FiPersonalCodeRecognizer", entities.FIPersonalIdentityCode, "fi",
		[]*analyzer.Pattern{analyzer.MustPattern("FI Personal Code",
			`\b\d{6}[-+A]\d{3}[0-9A-FHJ-NPR-Y]\b`, 0.85)},
	).WithContext("henkilötunnus", "hetu").WithValidator(validateFIPersonalCode)
}

func validateFIPersonalCode(s string) bool {
	if len(s) != 11 {
		return false
	}
	switch s[6] {
	case '-', '+', 'A':
	default:
		return false
	}
	number, err := strconv.Atoi(s[0:6] + s[7:10])
	if err != nil {
		return false
	}
	const checkChars = "0123456789ABCDEFHJKLMNPRSTUVWXY"
	return checkChars[number%31] == s[10]
}

// THTNIN detects Thai national ID numbers and validates the checksum.
func THTNIN() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"ThTninRecognizer", entities.THTNIN, "th",
		[]*analyzer.Pattern{
			analyzer.MustPattern("TH TNIN", `\b\d{13}\b`, 0.3),
			analyzer.MustPattern("TH TNIN (formatted)", `\b\d-\d{4}-\d{5}-\d{2}-\d\b`, 0.85),
		},
	).WithContext("บัตรประชาชน", "tnin").WithValidator(validateTHTNIN)
}

func validateTHTNIN(s string) bool {
	ds, ok := digitsExactly(s, 13)
	if !ok {
		return false
	}
	sum := 0
	for i := 0; i < 12; i++ {
		sum += ds[i] * (13 - i)
	}
	check := (11 - (sum % 11)) % 10
	return check == ds[12]
}
