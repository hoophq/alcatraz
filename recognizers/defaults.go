package recognizers

import "github.com/hoophq/alcatraz/analyzer"

// LoadDefaults registers the full built-in recognizer set under language.
//
// Every built-in detects a structured identifier (regex plus, where
// applicable, a checksum), and such identifiers are language-independent — a
// Thai national ID or an IBAN looks the same in any surrounding text. The
// complete set is therefore registered under whichever language is requested,
// so a default English engine detects all of them. (The language key still
// matters for future language-specific recognizers such as ML/NER models.)
func LoadDefaults(reg *analyzer.Registry, language string) {
	for _, r := range All() {
		reg.Add(language, r)
	}
}

// All returns a fresh instance of every built-in recognizer.
func All() []analyzer.Recognizer {
	groups := [][]analyzer.Recognizer{
		generic(), unitedStates(), unitedKingdom(), australia(),
		india(), italy(), spain(), singapore(), other(),
	}
	var recs []analyzer.Recognizer
	for _, g := range groups {
		recs = append(recs, g...)
	}
	return recs
}

func generic() []analyzer.Recognizer {
	return []analyzer.Recognizer{
		Email(), Phone(), CreditCard(), Crypto(), IP(), URL(), DateTime(), IBAN(),
	}
}

func unitedStates() []analyzer.Recognizer {
	return []analyzer.Recognizer{
		USSSN(), USITIN(), USPassport(), USDriverLicense(), USBank(), ABARouting(), MedicalLicense(),
	}
}

func unitedKingdom() []analyzer.Recognizer {
	return []analyzer.Recognizer{UKNHS(), UKNINO()}
}

func australia() []analyzer.Recognizer {
	return []analyzer.Recognizer{AUTFN(), AUABN(), AUACN(), AUMedicare()}
}

func india() []analyzer.Recognizer {
	return []analyzer.Recognizer{
		INAadhaar(), INPAN(), INPassport(), INVehicle(), INVoter(), INGSTIN(),
	}
}

func italy() []analyzer.Recognizer {
	return []analyzer.Recognizer{
		ITFiscalCode(), ITVAT(), ITIdentityCard(), ITDriverLicense(), ITPassport(),
	}
}

func spain() []analyzer.Recognizer {
	return []analyzer.Recognizer{ESNIF(), ESNIE()}
}

func singapore() []analyzer.Recognizer {
	return []analyzer.Recognizer{SGFIN(), SGUEN()}
}

func other() []analyzer.Recognizer {
	return []analyzer.Recognizer{PLPESEL(), KRRRN(), FIPersonalCode(), THTNIN()}
}
