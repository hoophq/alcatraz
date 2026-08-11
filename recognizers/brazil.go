package recognizers

import (
	"strings"
	"unicode"

	"github.com/hoophq/alcatraz/analyzer"
	"github.com/hoophq/alcatraz/entities"
)

// BRCPF detects Brazilian CPF numbers (Cadastro de Pessoas Físicas) and
// validates the two mod-11 check digits.
func BRCPF() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"BrCpfRecognizer", entities.BRCPF, "pt",
		[]*analyzer.Pattern{
			analyzer.MustPattern("BR CPF (formatted)", `\b\d{3}\.\d{3}\.\d{3}-\d{2}\b`, 0.4),
			analyzer.MustPattern("BR CPF", `\b\d{11}\b`, 0.3),
		},
	).WithContext("cpf", "cadastro de pessoas físicas").WithValidator(validateBRCPF)
}

func validateBRCPF(s string) bool {
	ds, ok := digitsExactly(s, 11)
	if !ok || allEqual(ds) {
		return false
	}
	if mod11Weighted(ds[:9], []int{10, 9, 8, 7, 6, 5, 4, 3, 2}) != ds[9] {
		return false
	}
	return mod11Weighted(ds[:10], []int{11, 10, 9, 8, 7, 6, 5, 4, 3, 2}) == ds[10]
}

// BRCNPJ detects Brazilian CNPJ numbers (Cadastro Nacional da Pessoa Jurídica)
// and validates the two mod-11 check digits.
func BRCNPJ() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"BrCnpjRecognizer", entities.BRCNPJ, "pt",
		[]*analyzer.Pattern{
			analyzer.MustPattern("BR CNPJ (formatted)", `\b\d{2}\.\d{3}\.\d{3}/\d{4}-\d{2}\b`, 0.4),
			analyzer.MustPattern("BR CNPJ", `\b\d{14}\b`, 0.3),
		},
	).WithContext("cnpj", "cadastro nacional").WithValidator(validateBRCNPJ)
}

func validateBRCNPJ(s string) bool {
	ds, ok := digitsExactly(s, 14)
	if !ok || allEqual(ds) {
		return false
	}
	if mod11Weighted(ds[:12], []int{5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}) != ds[12] {
		return false
	}
	return mod11Weighted(ds[:13], []int{6, 5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}) == ds[13]
}

// BRRG detects Brazilian RG numbers (Registro Geral / identity card).
//
// RG check-digit rules are issued per-state and are not standardized
// nationally, so this is a shape-and-context recognizer with no validator: a
// single state's checksum would wrongly drop valid RGs from other states.
func BRRG() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"BrRgRecognizer", entities.BRRG, "pt",
		[]*analyzer.Pattern{
			analyzer.MustPattern("BR RG (formatted)", `\b\d{1,2}\.\d{3}\.\d{3}-[0-9Xx]\b`, 0.4),
		},
	).WithContext("rg", "registro geral", "identidade", "carteira de identidade")
}

// BRCNH detects Brazilian driver's license numbers (Carteira Nacional de
// Habilitação) and validates the two check digits.
func BRCNH() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"BrCnhRecognizer", entities.BRCNH, "pt",
		[]*analyzer.Pattern{analyzer.MustPattern("BR CNH", `\b\d{11}\b`, 0.3)},
	).WithContext("cnh", "habilitação", "carteira de motorista").WithValidator(validateBRCNH)
}

func validateBRCNH(s string) bool {
	ds, ok := digitsExactly(s, 11)
	if !ok || allEqual(ds) {
		return false
	}
	// First check digit: weights 9..1.
	sum, dsc := 0, 0
	for i := 0; i < 9; i++ {
		sum += ds[i] * (9 - i)
	}
	dv1 := sum % 11
	if dv1 >= 10 {
		dv1, dsc = 0, 2
	}
	if dv1 != ds[9] {
		return false
	}
	// Second check digit: weights 1..9, offset by dsc when the first rolled over.
	sum = 0
	for i := 0; i < 9; i++ {
		sum += ds[i] * (i + 1)
	}
	dv2 := sum % 11
	if dv2 >= 10 {
		dv2 = 0
	} else if dv2 -= dsc; dv2 < 0 {
		dv2 += 11
	}
	return dv2 == ds[10]
}

// BRPIS detects Brazilian PIS/PASEP/NIS numbers and validates the mod-11 check
// digit.
func BRPIS() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"BrPisRecognizer", entities.BRPIS, "pt",
		[]*analyzer.Pattern{
			analyzer.MustPattern("BR PIS (formatted)", `\b\d{3}\.\d{5}\.\d{2}-\d\b`, 0.4),
			analyzer.MustPattern("BR PIS", `\b\d{11}\b`, 0.3),
		},
	).WithContext("pis", "pasep", "nis", "nit").WithValidator(validateBRPIS)
}

func validateBRPIS(s string) bool {
	ds, ok := digitsExactly(s, 11)
	if !ok || allEqual(ds) {
		return false
	}
	return mod11Weighted(ds[:10], []int{3, 2, 9, 8, 7, 6, 5, 4, 3, 2}) == ds[10]
}

// mod11Weighted computes a Brazilian mod-11 check digit: the dot product of ds
// with weights taken mod 11, where a remainder below 2 yields 0.
func mod11Weighted(ds, weights []int) int {
	sum := 0
	for i, d := range ds {
		sum += d * weights[i]
	}
	if r := sum % 11; r >= 2 {
		return 11 - r
	}
	return 0
}

// allEqual reports whether every digit in ds is identical (e.g. 111.111.111-11)
// — a sequence that satisfies the mod-11 math but is never a real document.
func allEqual(ds []int) bool {
	for i := 1; i < len(ds); i++ {
		if ds[i] != ds[0] {
			return false
		}
	}
	return len(ds) > 0
}

// BRCNS detects the Cartão Nacional de Saúde, Brazil's health-card number: 15
// digits validated by a mod-11 rule that differs by range.
//
// A definitive card (leading 1 or 2) is a PIS/PASEP number in the first eleven
// digits followed by a three-digit control block and a check digit: the block
// is "000", or "001" where the first computed digit came out as 10 and the sum
// is adjusted by 2. Checking only that the whole 15-digit weighted sum is a
// multiple of 11 is not enough — the control block can be corrupted while
// preserving that sum, which would mask an arbitrary 15-digit number.
//
// A provisional card (leading 7, 8 or 9) carries no PIS base and its rule is
// exactly the weighted-sum test.
func BRCNS() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"BrCnsRecognizer", entities.BRCNS, "pt",
		[]*analyzer.Pattern{
			analyzer.MustPattern("BR CNS (formatted)", `\b\d{3} \d{4} \d{4} \d{4}\b`, 0.4),
			analyzer.MustPattern("BR CNS", `\b\d{15}\b`, 0.3),
		},
	).WithContext("cns", "cartão nacional de saúde", "cartao nacional de saude", "sus").
		WithValidator(validateBRCNS)
}

func validateBRCNS(s string) bool {
	ds, ok := digitsExactly(s, 15)
	if !ok {
		return false
	}
	switch ds[0] {
	case 1, 2:
		return validDefinitiveBRCNS(ds)
	case 7, 8, 9:
		return weightedSum15(ds)%11 == 0
	default:
		return false
	}
}

// validDefinitiveBRCNS recomputes the control block and check digit from the
// PIS/PASEP base and requires an exact match.
func validDefinitiveBRCNS(ds []int) bool {
	sum := 0
	for i := 0; i < 11; i++ {
		sum += ds[i] * (15 - i)
	}
	dv := 11 - sum%11
	if dv == 11 {
		dv = 0
	}
	block := 0
	if dv == 10 {
		// The +2 lands the eleventh digit's weight at 6 instead of 5, which is
		// what the "001" block records.
		dv = 11 - (sum+2)%11
		block = 1
	}
	return ds[11] == 0 && ds[12] == 0 && ds[13] == block && ds[14] == dv
}

// weightedSum15 is the CNS weighting: digit i counts (15-i) times.
func weightedSum15(ds []int) int {
	sum := 0
	for i, d := range ds {
		sum += d * (15 - i)
	}
	return sum
}

// BRTitulo detects a voter registration (título eleitoral): 12 digits whose
// last two are check digits over the sequence and the issuing electoral region.
func BRTitulo() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"BrTituloEleitoralRecognizer", entities.BRTitulo, "pt",
		[]*analyzer.Pattern{
			analyzer.MustPattern("BR título eleitoral (formatted)", `\b\d{4} \d{4} \d{4}\b`, 0.4),
			analyzer.MustPattern("BR título eleitoral", `\b\d{12}\b`, 0.3),
		},
	).WithContext("título", "titulo", "eleitor", "eleitoral", "tse").
		WithValidator(validateBRTitulo)
}

func validateBRTitulo(s string) bool {
	ds, ok := digitsExactly(s, 12)
	if !ok {
		return false
	}
	if uf := ds[8]*10 + ds[9]; uf < 1 || uf > 28 { // 01-28 are the electoral regions
		return false
	}
	sum := 0
	for i := 0; i < 8; i++ {
		sum += ds[i] * (i + 2)
	}
	dv1 := sum % 11
	if dv1 == 10 {
		dv1 = 0
	}
	if dv1 != ds[10] {
		return false
	}
	dv2 := (ds[8]*7 + ds[9]*8 + dv1*9) % 11
	if dv2 == 10 {
		dv2 = 0
	}
	return dv2 == ds[11]
}

// BRRenavam detects a vehicle registration: 11 digits with a mod-11 check
// digit.
//
// PIS shares the length and the weights, so every valid RENAVAM is also a
// structurally valid PIS and vice versa. No validator can separate them; only
// the surrounding word can. Both recognizers therefore fire on a bare number
// and whichever scores first wins, which is correct for masking (the value is
// blanked either way) and honest for detection (in a context-free dump the
// engine genuinely cannot know which one it found).
func BRRenavam() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"BrRenavamRecognizer", entities.BRRenavam, "pt",
		[]*analyzer.Pattern{
			analyzer.MustPattern("BR RENAVAM", `\b\d{11}\b`, 0.3),
		},
	).WithContext("renavam", "veículo", "veiculo", "detran", "crlv").
		WithValidator(validateBRRenavam)
}

func validateBRRenavam(s string) bool {
	ds, ok := digitsExactly(s, 11)
	if !ok || allEqual(ds) {
		return false
	}
	weights := []int{3, 2, 9, 8, 7, 6, 5, 4, 3, 2}
	sum := 0
	for i := 0; i < 10; i++ {
		sum += ds[i] * weights[i]
	}
	dv := 11 - sum%11
	if dv >= 10 {
		dv = 0
	}
	return dv == ds[10]
}

// BRCEP detects a postal code in its hyphenated form.
//
// Only XXXXX-XXX is matched, and there is no validator: a passing validator
// promotes the match to MaxScore, and a CEP has no check digit, so any
// structural test would be promoting a guess. The hyphen is the evidence —
// five digits, a hyphen, three digits is not a shape ordinary numbers take.
// The unformatted form is BRCEPUnformatted.
func BRCEP() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"BrCepRecognizer", entities.BRCEP, "pt",
		[]*analyzer.Pattern{
			analyzer.MustPattern("BR CEP", `\b\d{5}-\d{3}\b`, 0.5),
		},
	).WithContext("cep", "código postal", "codigo postal", "endereço", "endereco")
}

// BRCEPUnformatted detects a CEP written as eight bare digits.
//
// Separate from BRCEP because eight digits is also an order id, a reference
// number and a yyyymmdd date, and nothing about the digits says which. It is
// gated on an explicit CEP word before the number rather than on a structural
// test: a passing validator is promoted to MaxScore, so a validator here would
// mask every eight-digit number in a response at full confidence.
//
// The score is high because a context validator only filters, it does not
// raise the score the way WithContext does. A low score would leave the
// recognizer unreachable to any caller whose threshold sits above it, even
// though the label gate had already passed — the gate, not the score, is what
// establishes confidence here.
func BRCEPUnformatted() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"BrCepUnformattedRecognizer", entities.BRCEP, "pt",
		[]*analyzer.Pattern{
			analyzer.MustPattern("BR CEP (unformatted)", `\b\d{8}\b`, 0.8),
		},
	).WithContextValidator(func(text string, start, _ int) bool {
		return labelBefore(text, start, "cep", "codigo postal", "código postal", "postal code", "zip")
	})
}

// BRPlaca detects a vehicle plate in both the Mercosur layout (ABC1D23) and the
// legacy one (ABC-1234). Neither shape occurs by accident in prose, so both
// score on the pattern.
//
// Case-insensitive: a plate is uppercase on the car, but a database dump or a
// free-text note routinely stores it lowercase, and a missed detection there is
// a leak.
func BRPlaca() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"BrPlacaRecognizer", entities.BRPlaca, "pt",
		[]*analyzer.Pattern{
			analyzer.MustPattern("BR placa Mercosul", `(?i)\b[a-z]{3}\d[a-z]\d{2}\b`, 0.6),
			analyzer.MustPattern("BR placa (legacy)", `(?i)\b[a-z]{3}-\d{4}\b`, 0.6),
		},
	).WithContext("placa", "veículo", "veiculo", "carro")
}

// BRPixKey detects a PIX random key (EVP): a UUID the Central Bank issues as a
// payment address. The other PIX key kinds are a CPF, an email or a phone,
// which alcatraz already detects under their own entities.
//
// A bare UUID is not a payment key — most UUIDs in a database are row ids — so
// this is gated on a PIX word just before the value. The gate is a context
// validator rather than a low score plus WithContext because a pattern scored
// under the caller's threshold is dropped before the context bonus can lift it.
//
// The bare word "chave" is not accepted: it is Portuguese for "key", so
// "chave primária" ahead of a row id would mask an ordinary UUID. Only "pix",
// "evp" and the phrase "chave pix" count.
func BRPixKey() analyzer.Recognizer {
	return analyzer.NewPatternRecognizer(
		"BrPixKeyRecognizer", entities.BRPixKey, "pt",
		[]*analyzer.Pattern{
			analyzer.MustPattern("BR PIX EVP key",
				`\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-4[0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}\b`, 0.8),
		},
	).WithContextValidator(func(text string, start, _ int) bool {
		return labelBefore(text, start, "pix", "evp", "chave pix")
	})
}

// labelBefore reports whether one of labels appears as a whole word in the few
// characters before start.
//
// Whole word, not substring: "recepcao 12345678" contains "cep" and would
// otherwise satisfy a CEP gate, masking an ordinary eight-digit number — the
// exact false positive these gates exist to prevent. Multi-word labels are
// matched against the joined tokens so "codigo postal" still works.
func labelBefore(text string, start int, labels ...string) bool {
	const window = 24
	from := start - window
	if from < 0 {
		from = 0
	}
	prefix := strings.ToLower(text[from:start])
	fields := strings.FieldsFunc(prefix, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	if len(fields) == 0 {
		return false
	}
	joined := strings.Join(fields, " ")
	for _, l := range labels {
		if !strings.Contains(l, " ") {
			for _, f := range fields {
				if f == l {
					return true
				}
			}
			continue
		}
		if joined == l || strings.HasPrefix(joined, l+" ") ||
			strings.HasSuffix(joined, " "+l) || strings.Contains(joined, " "+l+" ") {
			return true
		}
	}
	return false
}
