package recognizers

import (
	"github.com/hoophq/alcatraz/analyzer"
	"github.com/hoophq/alcatraz/entities"
)

// emailPattern is written as an interpreted string literal because the
// character class contains a literal backtick, which a raw string literal
// cannot hold.
const emailPattern = "\\b(?:(?:(?:[!#$%&'*+\\-/=?^_`{|}~\\w])|(?:[!#$%&'*+\\-/=?^_`{|}~\\w][!#$%&'*+\\-/=?^_`{|}~\\.\\w]{0,}[!#$%&'*+\\-/=?^_`{|}~\\w]))[@]\\w+(?:[-.]\\w+)*\\.\\w+(?:[-.]\\w+)*)\\b"

// Email detects email addresses.
func Email() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"EmailRecognizer", entities.EmailAddress, "en",
		[]*analyzer.Pattern{analyzer.MustPattern("Email (Medium)", emailPattern, 0.5)},
	).WithContext("email", "mail")
}

// Phone detects US, Brazilian and international (E.164-style) phone numbers.
//
// The US and BR patterns match one leading context character but report only
// the captured group, so a parenthesized area code keeps balanced parens in
// the reported span — downstream anonymization and allow-listing operate on
// exact spans, and `11) 91234-5678` cut out of `(11) 91234-5678` would leave
// a stray `(` behind.
func Phone() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"PhoneRecognizer", entities.PhoneNumber, "en",
		[]*analyzer.Pattern{
			analyzer.MustPattern("Phone (US)",
				`(?:^|\W)((?:\+?1[-.`+hsp+`]?)?(?:\(\d{3}\)|\d{3})[-.`+hsp+`]?\d{3}[-.`+hsp+`]?\d{4})\b`, 0.5).WithGroup(1),
			// A '+' country prefix followed by 8-15 digits (the E.164 range)
			// with at most two separator characters between consecutive
			// digits. The digit count is enforced by the repetition bounds,
			// so longer numeric runs (IDs, card numbers) never match a
			// prefix. \B rejects a '+' preceded by a word character, keeping
			// arithmetic like 2+34567890 out.
			analyzer.MustPattern("Phone (intl)",
				`\B\+(?:[`+hsp+`.()-]{0,2}\d){8,15}\b`, 0.5),
			// Brazilian domestic format: optional 55 country code, 2-digit
			// area code (DDD), optional mobile '9', then 4+4 digits. Scored
			// like the US pattern: with balanced parens its shape is no
			// weaker, and 0.5 clears the `alcatraz hook` default threshold.
			analyzer.MustPattern("Phone (BR)",
				`(?:^|\W)((?:\+?55[`+hsp+`.-]?)?(?:\(\d{2}\)|\d{2})[`+hsp+`.-]?9?\d{4}[`+hsp+`.-]?\d{4})\b`, 0.5).WithGroup(1),
		},
	).WithContext("phone", "number", "telephone", "cell", "mobile")
}

// CreditCard detects credit card numbers and validates them with the Luhn
// checksum.
func CreditCard() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"CreditCardRecognizer", entities.CreditCard, "en",
		[]*analyzer.Pattern{analyzer.MustPattern("All Credit Cards (weak)",
			`\b(?:(?:4\d{3})|(?:5[0-5]\d{2})|(?:6\d{3})|(?:3\d{3}))[- ]?(?:\d{3,4})[- ]?(?:\d{3,4})[- ]?(?:\d{3,5})\b`, 0.3)},
	).WithContext("credit", "card", "visa", "mastercard", "cc", "amex", "discover").
		WithValidator(luhnValid)
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
		// The character after the host's first byte is `[^\s]`, not `.`: an
		// unqualified dot also matches a tab, which lets a URL in one column
		// swallow the start of the next one.
		[]*analyzer.Pattern{analyzer.MustPattern("URL",
			`\b(?:https?://|ftp://|www\.)[^\s/$.?#][^\s][^\s]*\b`, 0.5)},
	).WithContext("url", "link", "website")
}

// DateTime detects common date formats.
func DateTime() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"DateTimeRecognizer", entities.DateTime, "en",
		[]*analyzer.Pattern{
			analyzer.MustPattern("Date (MM/DD/YYYY)",
				`\b(?:0?[1-9]|1[0-2])[/-](?:0?[1-9]|[12]\d|3[01])[/-](?:\d{4})\b`, 0.6),
			analyzer.MustPattern("Date (ISO)",
				`\b(?:\d{4})-(?:0[1-9]|1[0-2])-(?:0[1-9]|[12]\d|3[01])\b`, 0.7),
			// hsp rather than \s: a date wrapped across a line is rare, while
			// three adjacent table cells that happen to read as one is not,
			// and DATE_TIME scores high enough (0.7-0.8) to clear most
			// caller thresholds and mask them.
			analyzer.MustPattern("Date (Month DD, YYYY)",
				`\b(?:January|February|March|April|May|June|July|August|September|October|November|December)`+hsp+`+(?:\d{1,2}),?`+hsp+`+(?:\d{4})\b`, 0.8),
			analyzer.MustPattern("Date (Mon DD, YYYY)",
				`\b(?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\.?`+hsp+`+(?:\d{1,2}),?`+hsp+`+(?:\d{4})\b`, 0.7),
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
			analyzer.MustPattern("IBAN (spaces)", `\b(?:[A-Z]{2}[0-9]{2}`+hsp+`?(?:[A-Z0-9]{4}`+hsp+`?){1,7}[A-Z0-9]{1,4})\b`, 0.4),
		},
	).WithContext("iban", "account", "bank").WithValidator(validateIBAN)
}

// validateIBAN implements the ISO 7064 mod-97 checksum, streaming the
// rearranged number (body first, then country code and check digits) straight
// off the match bytes: ASCII spaces are skipped, letters expand to two digits
// (A=10 … Z=35), anything else fails.
func validateIBAN(s string) bool {
	n := 0
	var head [4]byte
	for i := range len(s) {
		c := s[i]
		if c == ' ' {
			continue
		}
		if n < 4 {
			head[n] = c
		}
		n++
	}
	if n < 15 || n > 34 {
		return false
	}
	for _, c := range head[:2] {
		if c < 'A' || c > 'Z' {
			return false
		}
	}
	for _, c := range head[2:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	var rem uint64
	seen := 0
	for i := range len(s) {
		c := s[i]
		if c == ' ' {
			continue
		}
		if seen++; seen <= 4 {
			continue
		}
		if !mod97Push(&rem, c) {
			return false
		}
	}
	for _, c := range head {
		mod97Push(&rem, c)
	}
	return rem == 1
}

// mod97Push folds one IBAN character into the running mod-97 remainder and
// reports whether c was a valid digit or upper-case letter.
func mod97Push(rem *uint64, c byte) bool {
	switch {
	case c >= '0' && c <= '9':
		*rem = (*rem*10 + uint64(c-'0')) % 97
	case c >= 'A' && c <= 'Z':
		v := uint64(c-'A') + 10
		*rem = (*rem*10 + v/10) % 97
		*rem = (*rem*10 + v%10) % 97
	default:
		return false
	}
	return true
}
