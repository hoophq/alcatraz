package recognizers

import (
	"github.com/hoophq/alcatraz/analyzer"
	"github.com/hoophq/alcatraz/entities"
)

// INAadhaar detects Indian Aadhaar numbers and validates the Verhoeff checksum.
func INAadhaar() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"InAadhaarRecognizer", entities.INAadhaar, "en",
		[]*analyzer.Pattern{analyzer.MustPattern("Aadhaar", `\b\d{4}\s?\d{4}\s?\d{4}\b`, 0.6)},
	).WithContext("aadhaar", "aadhar").WithValidator(func(m string) bool {
		ds := digitValues(m)
		if len(ds) != 12 {
			return false
		}
		return verhoeffValid(m)
	})
}

// INPAN detects Indian Permanent Account Numbers.
func INPAN() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"InPanRecognizer", entities.INPAN, "en",
		[]*analyzer.Pattern{analyzer.MustPattern("IN PAN", `\b[A-Z]{5}\d{4}[A-Z]\b`, 0.85)},
	).WithContext("pan", "permanent account")
}

// INPassport detects Indian passport numbers.
func INPassport() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"InPassportRecognizer", entities.INPassport, "en",
		[]*analyzer.Pattern{analyzer.MustPattern("IN Passport", `\b[A-Z]\d{7}\b`, 0.6)},
	).WithContext("passport", "travel")
}

// INVehicle detects Indian vehicle registration numbers.
func INVehicle() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"InVehicleRecognizer", entities.INVehicleRegistration, "en",
		[]*analyzer.Pattern{analyzer.MustPattern("IN Vehicle",
			`\b[A-Z]{2}\s?\d{1,2}\s?[A-Z]{1,2}\s?\d{4}\b`, 0.6)},
	).WithContext("vehicle", "registration", "car")
}

// INVoter detects Indian voter IDs (EPIC numbers).
func INVoter() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"InVoterRecognizer", entities.INVoter, "en",
		[]*analyzer.Pattern{analyzer.MustPattern("IN Voter ID", `\b[A-Z]{3}\d{7}\b`, 0.6)},
	).WithContext("voter", "epic")
}

// INGSTIN detects Indian GST Identification Numbers.
func INGSTIN() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"InGstinRecognizer", entities.INGSTIN, "en",
		[]*analyzer.Pattern{analyzer.MustPattern("IN GSTIN",
			`\b\d{2}[A-Z]{5}\d{4}[A-Z][A-Z\d]Z[A-Z\d]\b`, 0.85)},
	).WithContext("gstin", "gst")
}
