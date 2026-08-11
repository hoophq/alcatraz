package recognizers_test

import (
	"testing"

	"github.com/hoophq/alcatraz/analyzer"
	"github.com/hoophq/alcatraz/entities"
	"github.com/hoophq/alcatraz/recognizers"
)

// detectBR runs the default recognizer set over text and returns the first
// value found for entity. It exercises registration, pattern, validator and
// context together, the way a caller experiences them.
func detectBR(t *testing.T, text, entity string) string {
	t.Helper()
	reg := analyzer.NewRegistry("en")
	recognizers.LoadDefaults(reg, "en")
	eng := analyzer.NewEngine(reg, []string{"en"})

	for _, r := range eng.Analyze(text, analyzer.Options{Language: "en", Entities: []string{entity}}) {
		return text[r.Start:r.End]
	}
	return ""
}

// TestBrazilianIdentifiers pins that each recognizer fires on a checksum-valid
// value in the context it appears in production.
func TestBrazilianIdentifiers(t *testing.T) {
	cases := []struct{ name, text, entity, want string }{
		{"cns", "cns do paciente: 297220185600003", entities.BRCNS, "297220185600003"},
		{"cns provisional", "cns 823111431391203 sus", entities.BRCNS, "823111431391203"},
		{"titulo", "titulo de eleitor 483917830248 emitido", entities.BRTitulo, "483917830248"},
		{"renavam", "renavam 20193580815 do veiculo", entities.BRRenavam, "20193580815"},
		{"cep", "endereco cep 86759-690 zona sul", entities.BRCEP, "86759-690"},
		{"cep unformatted", "cep 04571010 zona sul", entities.BRCEP, "04571010"},
		{"placa mercosul", "placa IXR5D51 apreendida", entities.BRPlaca, "IXR5D51"},
		{"placa legacy", "placa ABC-1234 apreendida", entities.BRPlaca, "ABC-1234"},
		// A dump or a note stores the plate as typed, not as painted.
		{"placa lowercase", "placa ixr5d51 apreendida", entities.BRPlaca, "ixr5d51"},
		{"pix", "chave pix 0eac66dd-182d-434f-8ab8-297738b2d66d", entities.BRPixKey, "0eac66dd-182d-434f-8ab8-297738b2d66d"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectBR(t, tc.text, tc.entity); got != tc.want {
				t.Errorf("%s = %q, want %q", tc.entity, got, tc.want)
			}
		})
	}
}

// TestBrazilianRejectsNonIdentifiers is the point of the validators and the
// context gates: an eleven-digit number is a phone, a fifteen-digit one is an
// order id, and neither should be masked for being long.
func TestBrazilianRejectsNonIdentifiers(t *testing.T) {
	const uuid = "0eac66dd-182d-434f-8ab8-297738b2d66d"
	cases := []struct{ name, text, entity string }{
		{"cns bad check", "cns do paciente: 297220185600004", entities.BRCNS},
		// Control block corrupted while the weighted sum stays a multiple of 11.
		{"cns bad control block", "cns do paciente: 297220185600100", entities.BRCNS},
		{"titulo bad check", "titulo de eleitor 483917830249", entities.BRTitulo},
		{"renavam bad check", "renavam 20193580816 do veiculo", entities.BRRenavam},
		{"renavam repdigit", "renavam 11111111111 do veiculo", entities.BRRenavam},

		// Ordinary values that must pass through: the label gates match whole
		// words, so a token merely containing "cep" or "pix" is not a label.
		{"plain order id", "order 45012876 shipped", entities.BRCEP},
		{"cep substring", "recepcao 12345678 registrada", entities.BRCEP},
		{"pix substring", "pixel " + uuid + " id", entities.BRPixKey},
		{"bare uuid", "row id " + uuid + " updated", entities.BRPixKey},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectBR(t, tc.text, tc.entity); got != "" {
				t.Errorf("%s matched %q, want no detection", tc.entity, got)
			}
		})
	}
}

// TestRenavamAndPisShareTheirChecksum documents a collision callers have to
// live with: PIS and RENAVAM are both eleven digits with the same mod-11
// weights, so every valid RENAVAM is also a structurally valid PIS. Only the
// surrounding word separates them, and in a context-free dump neither
// recognizer can claim to be the right one.
func TestRenavamAndPisShareTheirChecksum(t *testing.T) {
	const v = "20193580815"
	if got := detectBR(t, "renavam "+v+" do veiculo", entities.BRRenavam); got != v {
		t.Errorf("RENAVAM in vehicle context = %q, want %q", got, v)
	}
	if got := detectBR(t, "pis "+v+" do trabalhador", entities.BRPIS); got != v {
		t.Errorf("same digits in PIS context = %q, want %q — the checksums are identical", got, v)
	}
}
