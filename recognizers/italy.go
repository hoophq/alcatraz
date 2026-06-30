package recognizers

import (
	"strings"

	"github.com/hoophq/alcatraz/analyzer"
	"github.com/hoophq/alcatraz/entities"
)

// ITFiscalCode detects Italian fiscal codes (Codice Fiscale) and validates the
// check character.
func ITFiscalCode() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"ItFiscalCodeRecognizer", entities.ITFiscalCode, "it",
		[]*analyzer.Pattern{analyzer.MustPattern("IT Fiscal Code",
			`\b[A-Z]{6}\d{2}[A-Z]\d{2}[A-Z]\d{3}[A-Z]\b`, 0.85)},
	).WithContext("codice fiscale", "fiscal").WithValidator(validateITFiscalCode)
}

func validateITFiscalCode(s string) bool {
	if len(s) != 16 {
		return false
	}
	const (
		evenChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
		oddChars  = "BAKPLCQDREVOSFTGUHMINJWZYX"
		oddDigits = "1021222423252627282924"
	)
	sum := 0
	for i := 0; i < 15; i++ {
		c := s[i]
		var value int
		if i%2 == 0 { // odd position (1-indexed): even 0-based index
			if c >= '0' && c <= '9' {
				d := int(c - '0')
				value = int(oddDigits[d*2]-'0') + int(oddDigits[d*2+1]-'0')
			} else if idx := strings.IndexByte(oddChars, c); idx >= 0 {
				value = idx
			}
		} else { // even position
			if c >= '0' && c <= '9' {
				value = int(c - '0')
			} else if idx := strings.IndexByte(evenChars, c); idx >= 0 {
				value = idx
			}
		}
		sum += value
	}
	return evenChars[sum%26] == s[15]
}

// ITVAT detects Italian VAT codes (Partita IVA) and validates the Luhn-style
// checksum.
func ITVAT() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"ItVatRecognizer", entities.ITVATCode, "it",
		[]*analyzer.Pattern{analyzer.MustPattern("IT VAT", `\b\d{11}\b`, 0.3)},
	).WithContext("vat", "iva", "partita iva").WithValidator(validateITVAT)
}

func validateITVAT(s string) bool {
	ds, ok := digitsExactly(s, 11)
	if !ok {
		return false
	}
	sum := 0
	for i := 0; i < 10; i++ {
		d := ds[i]
		if i%2 == 1 {
			if d *= 2; d > 9 {
				d = d/10 + d%10
			}
		}
		sum += d
	}
	check := (10 - (sum % 10)) % 10
	return check == ds[10]
}

// ITIdentityCard detects Italian identity card numbers (new and old formats).
func ITIdentityCard() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"ItIdentityCardRecognizer", entities.ITIdentityCard, "it",
		[]*analyzer.Pattern{
			analyzer.MustPattern("IT Identity Card (new)", `\b[A-Z]{2}\d{5}[A-Z]{2}\b`, 0.7),
			analyzer.MustPattern("IT Identity Card (old)", `\b[A-Z0-9]{7}\b`, 0.3),
		},
	).WithContext("identity", "carta identita")
}

// ITDriverLicense detects Italian driver license numbers.
func ITDriverLicense() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"ItDriverLicenseRecognizer", entities.ITDriverLicense, "it",
		[]*analyzer.Pattern{analyzer.MustPattern("IT Driver License", `\b[A-Z]{2}\d{7}[A-Z]\b`, 0.6)},
	).WithContext("patente", "driver")
}

// ITPassport detects Italian passport numbers.
func ITPassport() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"ItPassportRecognizer", entities.ITPassport, "it",
		[]*analyzer.Pattern{analyzer.MustPattern("IT Passport", `\b[A-Z]{2}\d{7}\b`, 0.6)},
	).WithContext("passport", "passaporto")
}
