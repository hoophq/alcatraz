package recognizers

import (
	"strconv"
	"strings"

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

func validateSSN(s string) bool {
	san := strings.ReplaceAll(s, "-", "")
	if len(san) != 9 {
		return false
	}
	area, err1 := strconv.Atoi(san[0:3])
	group, err2 := strconv.Atoi(san[3:5])
	serial, err3 := strconv.Atoi(san[5:9])
	if err1 != nil || err2 != nil || err3 != nil {
		return false
	}
	if area == 0 || area == 666 || area >= 900 {
		return false
	}
	if group == 0 || serial == 0 {
		return false
	}
	ds := digitValues(san)
	if len(ds) == 9 {
		allSeq, allSame := true, true
		for i := 1; i < 9; i++ {
			if ds[i] != ds[i-1]+1 {
				allSeq = false
			}
			if ds[i] != ds[i-1] {
				allSame = false
			}
		}
		if allSeq || allSame {
			return false
		}
	}
	return true
}

// USITIN detects US Individual Taxpayer Identification Numbers (9XX-XX-XXXX).
func USITIN() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"UsItinRecognizer", entities.USITIN, "en",
		[]*analyzer.Pattern{
			analyzer.MustPattern("ITIN (dashes)", `\b(9\d{2})-(\d{2})-(\d{4})\b`, 0.85),
			analyzer.MustPattern("ITIN (no dashes)", `\b9\d{8}\b`, 0.3),
		},
	).WithContext("itin", "taxpayer", "tax").WithValidator(validateITIN)
}

func validateITIN(s string) bool {
	san := strings.ReplaceAll(s, "-", "")
	if len(san) != 9 || san[0] != '9' {
		return false
	}
	area, err1 := strconv.Atoi(san[0:3])
	group, err2 := strconv.Atoi(san[3:5])
	serial, err3 := strconv.Atoi(san[5:9])
	if err1 != nil || err2 != nil || err3 != nil {
		return false
	}
	ff := area % 100
	valid := (ff >= 50 && ff <= 65) || (ff >= 70 && ff <= 88) ||
		(ff >= 90 && ff <= 92) || (ff >= 94 && ff <= 99)
	if !valid {
		return false
	}
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
	ds := digitValues(s)
	if len(ds) != 9 {
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
