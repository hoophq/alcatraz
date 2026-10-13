package recognizers

import (
	"github.com/hoophq/alcatraz/analyzer"
	"github.com/hoophq/alcatraz/entities"
)

// USSSN detects US Social Security Numbers and validates area/group/serial
// ranges plus trivially sequential or repeated numbers.
func USSSN() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"UsSsnRecognizer", entities.USSSN, "en",
		[]*analyzer.Pattern{
			analyzer.MustPattern("SSN (dashes)", `\b\d{3}-\d{2}-\d{4}\b`, 0.85),
			analyzer.MustPattern("SSN (no dashes)", `\b\d{9}\b`, 0.3),
		},
	).WithContext("ssn", "social", "security", "social security").
		WithValidator(validateSSN)
}

// ssnDigits collects the nine digits of a dashed or bare SSN-shaped value
// into ds. Only '-' may separate the digits; any other byte fails.
func ssnDigits(ds *[9]int, s string) bool {
	n := 0
	for i := range len(s) {
		c := s[i]
		switch {
		case c == '-':
			continue
		case c < '0' || c > '9' || n == len(ds):
			return false
		}
		ds[n] = int(c - '0')
		n++
	}
	return n == len(ds)
}

func validateSSN(s string) bool {
	var ds [9]int
	if !ssnDigits(&ds, s) {
		return false
	}
	area := ds[0]*100 + ds[1]*10 + ds[2]
	group := ds[3]*10 + ds[4]
	serial := ds[5]*1000 + ds[6]*100 + ds[7]*10 + ds[8]
	if area == 0 || area == 666 || area >= 900 {
		return false
	}
	if group == 0 || serial == 0 {
		return false
	}
	allSeq, allSame := true, true
	for i := 1; i < len(ds); i++ {
		if ds[i] != ds[i-1]+1 {
			allSeq = false
		}
		if ds[i] != ds[i-1] {
			allSame = false
		}
	}
	return !allSeq && !allSame
}

// USITIN detects US Individual Taxpayer Identification Numbers (9XX-XX-XXXX).
func USITIN() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"UsItinRecognizer", entities.USITIN, "en",
		[]*analyzer.Pattern{
			analyzer.MustPattern("ITIN (dashes)", `\b(?:9\d{2})-(?:\d{2})-(?:\d{4})\b`, 0.85),
			analyzer.MustPattern("ITIN (no dashes)", `\b9\d{8}\b`, 0.3),
		},
	).WithContext("itin", "taxpayer", "tax").WithValidator(validateITIN)
}

func validateITIN(s string) bool {
	var ds [9]int
	if !ssnDigits(&ds, s) || ds[0] != 9 {
		return false
	}
	ff := ds[1]*10 + ds[2]
	valid := (ff >= 50 && ff <= 65) || (ff >= 70 && ff <= 88) ||
		(ff >= 90 && ff <= 92) || (ff >= 94 && ff <= 99)
	if !valid {
		return false
	}
	group := ds[3]*10 + ds[4]
	serial := ds[5]*1000 + ds[6]*100 + ds[7]*10 + ds[8]
	return group != 0 && serial != 0
}

// USPassport detects US passport numbers (9 digits, or 1 letter + 8 digits).
func USPassport() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"UsPassportRecognizer", entities.USPassport, "en",
		[]*analyzer.Pattern{
			analyzer.MustPattern("US Passport (9 digits)", `\b[0-9]{9}\b`, 0.3),
			analyzer.MustPattern("US Passport (letter + 8 digits)", `\b[A-Z][0-9]{8}\b`, 0.4),
		},
	).WithContext("passport", "travel", "document")
}

// USDriverLicense detects US driver license numbers (generic + a couple of
// state formats).
func USDriverLicense() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"UsDriverLicenseRecognizer", entities.USDriverLicense, "en",
		[]*analyzer.Pattern{
			analyzer.MustPattern("Driver License (general)", `\b[A-Z]{1,2}[0-9]{5,9}\b`, 0.3),
			analyzer.MustPattern("Driver License (CA)", `\b[A-Z][0-9]{7}\b`, 0.5),
			analyzer.MustPattern("Driver License (FL)", `\b[A-Z][0-9]{12}\b`, 0.5),
		},
	).WithContext("driver", "license", "dl", "drivers")
}

// USBank detects US bank account numbers. Very low confidence: needs context.
func USBank() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"UsBankRecognizer", entities.USBankNumber, "en",
		[]*analyzer.Pattern{analyzer.MustPattern("US Bank Account", `\b\d{8,17}\b`, 0.05)},
	).WithContext("account", "bank", "routing")
}

// ABARouting detects ABA routing transit numbers and validates the checksum.
func ABARouting() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"AbaRoutingRecognizer", entities.ABARouting, "en",
		[]*analyzer.Pattern{analyzer.MustPattern("ABA Routing", `\b\d{9}\b`, 0.5)},
	).WithContext("routing", "aba", "transit").WithValidator(validateABA)
}

func validateABA(s string) bool {
	var buf [9]int
	ds, ok := digitsExactly(buf[:0], s, 9)
	if !ok {
		return false
	}
	checksum := (3*(ds[0]+ds[3]+ds[6]) + 7*(ds[1]+ds[4]+ds[7]) + (ds[2] + ds[5] + ds[8])) % 10
	return checksum == 0
}

// MedicalLicense detects US medical license numbers.
func MedicalLicense() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"MedicalLicenseRecognizer", entities.MedicalLicense, "en",
		[]*analyzer.Pattern{analyzer.MustPattern("Medical License", `\b[A-Z]{1,2}\d{4,8}\b`, 0.4)},
	).WithContext("medical", "license", "physician", "doctor")
}
