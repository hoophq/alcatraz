package bench_test

// Standard testing.B benchmarks for use with `go test -bench` + benchstat.
// The cross-engine comparison uses cmd/benchgo instead; this file is for
// tracking Alcatraz-only regressions over time.

import (
	"testing"

	"github.com/hoophq/alcatraz"
	"github.com/hoophq/alcatraz/bench/internal/corpus"
	"github.com/hoophq/alcatraz/entities"
)

func loadCorpus(b *testing.B) []corpus.Doc {
	b.Helper()
	docs, err := corpus.Load("corpus.jsonl")
	if err != nil {
		b.Skipf("corpus.jsonl not found — run `go run ./cmd/gencorpus` first: %v", err)
	}
	return docs
}

func BenchmarkAnalyze(b *testing.B) {
	docs := loadCorpus(b)
	eng := alcatraz.NewEngine()

	for _, sc := range corpus.SizeClasses {
		for _, den := range corpus.Densities {
			var group []corpus.Doc
			for _, d := range docs {
				if d.SizeClass == sc && d.Density == den {
					group = append(group, d)
				}
			}
			if len(group) == 0 {
				continue
			}
			b.Run(sc+"/"+den, func(b *testing.B) {
				bytes := 0
				for _, d := range group {
					bytes += len(d.Text)
				}
				b.SetBytes(int64(bytes / len(group)))
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					_ = eng.Analyze(group[i%len(group)].Text, alcatraz.Options{})
				}
			})
		}
	}
}

// ocrLineEntities is the ~50-entry structured-entity list an OCR consumer
// (libhoop-ica) passes as Options.Entities on every per-line Analyze call.
var ocrLineEntities = []string{
	entities.EmailAddress, entities.PhoneNumber, entities.CreditCard,
	entities.Crypto, entities.DateTime, entities.IBANCode, entities.IPAddress, entities.URL,
	entities.USSSN, entities.USITIN, entities.USPassport, entities.USDriverLicense,
	entities.USBankNumber, entities.ABARouting, entities.MedicalLicense,
	entities.UKNHS, entities.UKNINO, entities.AUTFN, entities.AUABN, entities.AUACN, entities.AUMedicare,
	entities.INAadhaar, entities.INPAN, entities.INPassport, entities.INVehicleRegistration, entities.INVoter, entities.INGSTIN,
	entities.ITFiscalCode, entities.ITVATCode, entities.ITIdentityCard, entities.ITDriverLicense, entities.ITPassport,
	entities.ESNIF, entities.ESNIE, entities.SGFIN, entities.SGUEN,
	entities.BRCPF, entities.BRCNPJ, entities.BRRG, entities.BRCNH, entities.BRPIS, entities.BRCNS,
	entities.BRTitulo, entities.BRRenavam, entities.BRCEP, entities.BRPlaca, entities.BRPixKey,
	entities.PLPESEL, entities.KRRRN, entities.FIPersonalIdentityCode, entities.THTNIN,
}

// BenchmarkAnalyzeOCRLine is the short-text regime: one 60-byte line per
// call with a 50-entry Options.Entities filter, as an OCR pipeline issues
// 50-150 times per frame.
func BenchmarkAnalyzeOCRLine(b *testing.B) {
	eng := alcatraz.NewEngine()
	opts := alcatraz.Options{Entities: ocrLineEntities}
	lines := []struct{ name, text string }{
		{"prose", "Patient reported mild discomfort after the morning visit now"},
		{"digits", "Member ID 48213 visited on 03/14/2024 and paid 1,250.00 USD."},
	}
	for _, l := range lines {
		if len(l.text) != 60 {
			b.Fatalf("%s line is %d bytes, want 60", l.name, len(l.text))
		}
		b.Run(l.name, func(b *testing.B) {
			b.SetBytes(int64(len(l.text)))
			b.ReportAllocs()
			for range b.N {
				_ = eng.Analyze(l.text, opts)
			}
		})
	}
}

func BenchmarkNewEngine(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = alcatraz.NewEngine()
	}
}
