package recognizers

import (
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/hoophq/alcatraz/analyzer"
	"github.com/hoophq/alcatraz/entities"
)

// CNIDCard detects the 18-character Chinese citizen identification number
// defined by GB 11643-1999 (six address digits, YYYYMMDD, three sequence digits,
// and a MOD 11-2 check character). Lowercase x is accepted as transcribed input.
// It checks the calendar date and checksum, not issuance, identity, or the
// historical validity of the address code. Legacy 15-digit IDs are not covered.
// Standard: https://openstd.samr.gov.cn/bzgk/std/newGbInfo?hcno=080D6FBF2BB468F9007657F26D60013E
func CNIDCard() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"CnIdCardRecognizer", entities.CNIDCard, "zh",
		[]*analyzer.Pattern{
			analyzer.MustPattern("CN ID (18 characters)", `\b[1-9][0-9]{16}[0-9Xx]\b`, 0.5),
		},
	).WithContext("身份证", "身份證", "公民身份号码", "公民身份號碼", "identity", "citizen").
		WithValidator(validateCNIDCard).
		WithContextValidator(func(text string, start, end int) bool {
			// RE2's word boundary is ASCII-only. Han text next to a value is
			// legitimate, but a neighboring Unicode digit makes it a longer
			// numeric token, not a standalone identifier.
			if start > 0 {
				r, _ := utf8.DecodeLastRuneInString(text[:start])
				if unicode.IsDigit(r) {
					return false
				}
			}
			if end < len(text) {
				r, _ := utf8.DecodeRuneInString(text[end:])
				if unicode.IsDigit(r) {
					return false
				}
			}
			return true
		})
}

func validateCNIDCard(s string) bool {
	if len(s) != 18 || s[0] == '0' {
		return false
	}
	for i := 0; i < 17; i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	// Parse rejects impossible dates, including February 29 in non-leap years.
	// Do not impose a moving age cutoff or a current administrative-code list:
	// historical records remain PII even after their holder or district is gone.
	if s[6:10] == "0000" {
		return false
	}
	if _, err := time.Parse("20060102", s[6:14]); err != nil {
		return false
	}
	weights := [...]int{7, 9, 10, 5, 8, 4, 2, 1, 6, 3, 7, 9, 10, 5, 8, 4, 2}
	sum := 0
	for i, weight := range weights {
		sum += int(s[i]-'0') * weight
	}
	check := s[17]
	if check == 'x' {
		check = 'X'
	}
	return check == "10X98765432"[sum%11]
}
