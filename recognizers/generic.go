package recognizers

import (
	"strconv"
	"strings"

	"github.com/hoophq/alcatraz/analyzer"
	"github.com/hoophq/alcatraz/entities"
)

// emailPattern is written as an interpreted string literal because the
// character class contains a literal backtick, which a raw string literal
// cannot hold.
const emailPattern = "\\b((([!#$%&'*+\\-/=?^_`{|}~\\w])|([!#$%&'*+\\-/=?^_`{|}~\\w][!#$%&'*+\\-/=?^_`{|}~\\.\\w]{0,}[!#$%&'*+\\-/=?^_`{|}~\\w]))[@]\\w+([-.]\\w+)*\\.\\w+([-.]\\w+)*)\\b"

// Email detects email addresses.
func Email() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"EmailRecognizer", entities.EmailAddress, "en",
		[]*analyzer.Pattern{analyzer.MustPattern("Email (Medium)", emailPattern, 0.5)},
	).WithContext("email", "mail")
}

// Phone detects US, Brazilian and international (E.164-style) phone numbers.
func Phone() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"PhoneRecognizer", entities.PhoneNumber, "en",
		[]*analyzer.Pattern{
			analyzer.MustPattern("Phone (US)",
				`\b(\+?1[-.\s]?)?\(?\d{3}\)?[-.\s]?\d{3}[-.\s]?\d{4}\b`, 0.5),
			// A '+' country prefix followed by 8-15 digits (the E.164 range)
			// with at most two separator characters between consecutive
			// digits. The digit count is enforced by the repetition bounds,
			// so longer numeric runs (IDs, card numbers) never match a
			// prefix. \B rejects a '+' preceded by a word character, keeping
			// arithmetic like 2+34567890 out.
			analyzer.MustPattern("Phone (intl)",
				`\B\+(?:[\s.()-]{0,2}\d){8,15}\b`, 0.5),
			// Brazilian domestic format: optional 55 country code, 2-digit
			// area code (DDD), optional mobile '9', then 4+4 digits.
			analyzer.MustPattern("Phone (BR)",
				`\b(?:\+?55[\s.-]?)?\(?\d{2}\)?[\s.-]?9?\d{4}[\s.-]?\d{4}\b`, 0.4),
		},
	).WithContext("phone", "number", "telephone", "cell", "mobile")
}

// CreditCard detects credit card numbers and validates them with the Luhn
// checksum.
func CreditCard() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"CreditCardRecognizer", entities.CreditCard, "en",
		[]*analyzer.Pattern{analyzer.MustPattern("All Credit Cards (weak)",
			`\b((4\d{3})|(5[0-5]\d{2})|(6\d{3})|(3\d{3}))[- ]?(\d{3,4})[- ]?(\d{3,4})[- ]?(\d{3,5})\b`, 0.3)},
	).WithContext("credit", "card", "visa", "mastercard", "cc", "amex", "discover").
		WithValidator(func(m string) bool { return luhnValid(stripSeparators(m)) })
}

// Crypto detects Bitcoin and Ethereum wallet addresses.
func Crypto() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"CryptoRecognizer", entities.Crypto, "en",
		[]*analyzer.Pattern{
			analyzer.MustPattern("Bitcoin", `\b[13][a-km-zA-HJ-NP-Z1-9]{25,34}\b`, 0.5),
			analyzer.MustPattern("Ethereum", `\b0x[a-fA-F0-9]{40}\b`, 0.5),
		},
	).WithContext("wallet", "bitcoin", "btc", "ethereum", "eth")
}

// IP detects IPv4 and (simplified) IPv6 addresses.
func IP() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"IpRecognizer", entities.IPAddress, "en",
		[]*analyzer.Pattern{
			analyzer.MustPattern("IPv4",
				`\b(?:(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\b`, 0.6),
			analyzer.MustPattern("IPv6", `\b(?:[0-9a-fA-F]{1,4}:){7}[0-9a-fA-F]{1,4}\b`, 0.6),
		},
	).WithContext("ip", "address", "ipv4", "ipv6")
}

// URL detects HTTP(S)/FTP/www URLs.
func URL() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"UrlRecognizer", entities.URL, "en",
		[]*analyzer.Pattern{analyzer.MustPattern("URL",
			`\b(?:https?://|ftp://|www\.)[^\s/$.?#].[^\s]*\b`, 0.5)},
	).WithContext("url", "link", "website")
}

// DateTime detects common date formats.
func DateTime() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"DateTimeRecognizer", entities.DateTime, "en",
		[]*analyzer.Pattern{
			analyzer.MustPattern("Date (MM/DD/YYYY)",
				`\b(0?[1-9]|1[0-2])[/-](0?[1-9]|[12]\d|3[01])[/-](\d{4})\b`, 0.6),
			analyzer.MustPattern("Date (ISO)",
				`\b(\d{4})-(0[1-9]|1[0-2])-(0[1-9]|[12]\d|3[01])\b`, 0.7),
			analyzer.MustPattern("Date (Month DD, YYYY)",
				`\b(January|February|March|April|May|June|July|August|September|October|November|December)\s+(\d{1,2}),?\s+(\d{4})\b`, 0.8),
			analyzer.MustPattern("Date (Mon DD, YYYY)",
				`\b(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\.?\s+(\d{1,2}),?\s+(\d{4})\b`, 0.7),
		},
	).WithContext("date", "born", "birthday", "dob")
}

// IBAN detects International Bank Account Numbers and validates the ISO 7064
// mod-97 checksum.
func IBAN() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"IbanRecognizer", entities.IBANCode, "en",
		[]*analyzer.Pattern{
			analyzer.MustPattern("IBAN", `\b[A-Z]{2}[0-9]{2}[A-Z0-9]{1,30}\b`, 0.3),
			analyzer.MustPattern("IBAN (spaces)", `\b([A-Z]{2}[0-9]{2}\s?([A-Z0-9]{4}\s?){1,7}[A-Z0-9]{1,4})\b`, 0.4),
		},
	).WithContext("iban", "account", "bank").WithValidator(validateIBAN)
}

// validateIBAN implements the ISO 7064 mod-97 checksum.
func validateIBAN(s string) bool {
	iban := strings.ReplaceAll(s, " ", "")
	if len(iban) < 15 || len(iban) > 34 {
		return false
	}
	for i := 0; i < 2; i++ {
		if c := iban[i]; c < 'A' || c > 'Z' {
			return false
		}
	}
	for i := 2; i < 4; i++ {
		if c := iban[i]; c < '0' || c > '9' {
			return false
		}
	}
	rearranged := iban[4:] + iban[0:4]
	var numeric strings.Builder
	for i := 0; i < len(rearranged); i++ {
		switch c := rearranged[i]; {
		case c >= '0' && c <= '9':
			numeric.WriteByte(c)
		case c >= 'A' && c <= 'Z':
			numeric.WriteString(strconv.Itoa(int(c-'A') + 10))
		default:
			return false
		}
	}
	num := numeric.String()
	var rem uint64
	for i := 0; i < len(num); i++ {
		rem = (rem*10 + uint64(num[i]-'0')) % 97
	}
	return rem == 1
}
