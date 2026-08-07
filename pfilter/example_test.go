package pfilter_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

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

// ExampleEnsureLibrary fetches the prebuilt privacy-filter.cpp shared
// library for this OS/arch into the user cache dir, verifying the pinned
// sha256. Once it is cached, New finds it with no configuration. New resolves
// the library in order: Config.Library, $PF_LIBRARY, the EnsureLibrary cache,
// then the system loader paths. Set Config.Library only to override that.
//
// Not executed by go test: it downloads a native library.
func ExampleEnsureLibrary() {
	// Bound the download; the artifact is a few tens of MB.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	lib, err := pfilter.EnsureLibrary(ctx)
	if err != nil {
		// Platforms with no published artifact land here: build from
		// source (see pfilter/dist) and set $PF_LIBRARY.
		log.Fatal(err)
	}

	cfg := pfilter.DefaultConfig("privacy-filter-q8.gguf")
	cfg.Library = lib // explicit; the cache would be found anyway
	nlp, err := pfilter.New(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer nlp.Close()
}

// ExampleEnsureModel picks a model variant. The base model (ModelQ8,
// ModelF16) covers 8 English PII categories; the multilingual fine-tune
// covers 54 across 16 languages, at the cost of many more labels to map or
// silence. Anything the LabelMapping does not name is still reported,
// normalized to SCREAMING_SNAKE_CASE. LabelsToIgnore is how you drop the
// ones you do not want.
//
// Not executed by go test: the GGUF is 1.6-2.8 GB.
func ExampleEnsureModel() {
	ctx := context.Background()

	model, err := pfilter.EnsureModel(ctx, pfilter.ModelMultilingualQ8)
	if err != nil {
		log.Fatal(err)
	}

	cfg := pfilter.DefaultConfig(model)
	// Keep the extra multilingual categories out of the results, except
	// the ones DefaultConfig already maps.
	cfg.LabelsToIgnore = []string{"CRYPTO_WALLET", "ORGANIZATION"}
	// One model context per concurrent Analyze; each loads the model
	// again, so memory scales with it.
	cfg.PoolSize = 2

	nlp, err := pfilter.New(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer nlp.Close()

	fmt.Println(nlp.Config().SupportedEntities())
}

// ExampleNew shows the two halves of wiring the engine in: the recognizer
// goes into the registry so its entities are declared and scored like any
// other, and SetNlpEngine makes the analyzer run the model once per Analyze
// and share the resulting artifacts. Register the recognizer without
// SetNlpEngine and it still works, running the model itself once per
// recognizer pass.
//
// Not executed by go test: it needs libpf and a GGUF on disk.
func ExampleNew() {
	cfg := pfilter.DefaultConfig(os.Getenv("PF_MODEL"))
	// Shed low-confidence spans inside the model instead of in the
	// analyzer. Leave it at 0 and use analyzer.Options.Threshold unless
	// you have a reason.
	cfg.Threshold = 0.5

	nlp, err := pfilter.New(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer nlp.Close()

	reg := analyzer.NewRegistry("en")
	recognizers.LoadDefaults(reg, "en")
	reg.Add("en", nlp.Recognizer("en"))

	eng := analyzer.NewEngine(reg, []string{"en"})
	eng.SetNlpEngine(nlp)

	for _, hit := range eng.Analyze("Call Maria Silva on +55 11 91234-5678", analyzer.Options{}) {
		fmt.Printf("%s %q\n", hit.EntityType, hit.Text)
	}
}
