package pfilter_test

import (
	"context"
	"fmt"
	"log"

	"github.com/hoophq/alcatraz/analyzer"
	"github.com/hoophq/alcatraz/pfilter"
	"github.com/hoophq/alcatraz/recognizers"
)

// Example wires the privacy-filter GGML backend into an analyzer engine.
// The first run downloads (and sha256-verifies) the prebuilt libpf shared
// library for this OS/arch and the Q8 GGUF (~1.6 GB); later runs hit the
// cache. New finds the EnsureLibrary-cached libpf automatically when
// Config.Library is empty.
//
// There is no // Output: comment on purpose: the example is compiled but
// not executed by go test, because it downloads a model and a native
// library.
func Example() {
	ctx := context.Background()

	if _, err := pfilter.EnsureLibrary(ctx); err != nil {
		log.Fatal(err)
	}
	model, err := pfilter.EnsureModel(ctx, pfilter.ModelQ8)
	if err != nil {
		log.Fatal(err)
	}

	nlp, err := pfilter.New(pfilter.DefaultConfig(model))
	if err != nil {
		log.Fatal(err)
	}
	defer nlp.Close()

	// Pattern recognizers plus the privacy-filter recognizer in one
	// registry.
	reg := analyzer.NewRegistry("en")
	recognizers.LoadDefaults(reg, "en")
	reg.Add("en", nlp.Recognizer("en"))

	eng := analyzer.NewEngine(reg, []string{"en"})
	// With SetNlpEngine the model runs once per Analyze call and its
	// artifacts are shared with every artifact-aware recognizer.
	eng.SetNlpEngine(nlp)

	text := "Maria Silva lives at 12 Baker Street, card 4532015112830366"
	for _, hit := range eng.Analyze(text, analyzer.Options{}) {
		fmt.Printf("%s %q %.2f\n", hit.EntityType, hit.Text, hit.Score)
	}
	// Prints PERSON "Maria Silva" and LOCATION "12 Baker Street" (from
	// the model) plus CREDIT_CARD "4532015112830366" (Luhn-validated
	// pattern recognizer).
}
