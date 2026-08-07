package ner_test

import (
	"context"
	"fmt"
	"log"

	"github.com/hoophq/alcatraz/analyzer"
	"github.com/hoophq/alcatraz/ner"
	"github.com/hoophq/alcatraz/recognizers"
)

// Example wires the statistical NER backend into an analyzer engine so
// free-text entities (PERSON, LOCATION, NRP, DATE_TIME) are detected
// alongside the pattern recognizers. The ONNX model is downloaded from
// Hugging Face on first use and cached under the user cache directory.
//
// There is no // Output: comment on purpose: the example is compiled but
// not executed by go test, because it downloads a model.
func Example() {
	nlp, err := ner.New(context.Background(), ner.DefaultConfig())
	if err != nil {
		log.Fatal(err)
	}
	defer nlp.Close()

	// Pattern recognizers plus the NER recognizer in one registry.
	reg := analyzer.NewRegistry("en")
	recognizers.LoadDefaults(reg, "en")
	reg.Add("en", nlp.Recognizer("en"))

	eng := analyzer.NewEngine(reg, []string{"en"})
	// With SetNlpEngine the model runs once per Analyze call and its
	// artifacts are shared with every artifact-aware recognizer.
	eng.SetNlpEngine(nlp)

	text := "John Smith moved to Berlin; reach him at john@example.com"
	for _, hit := range eng.Analyze(text, analyzer.Options{}) {
		fmt.Printf("%s %q %.2f\n", hit.EntityType, hit.Text, hit.Score)
	}
	// Prints PERSON "John Smith" and LOCATION "Berlin" (from the model)
	// plus EMAIL_ADDRESS "john@example.com" (from the pattern recognizer).
}

// ExampleNew_offline pins the engine to a models directory seeded ahead of
// time and forbids any network I/O. New then fails with an error naming the
// missing file and the command that fixes it, instead of falling back to a
// download that trips egress monitoring on its way to an opaque DNS error.
//
// Compiled, not executed: there is no // Output: comment because the example
// needs a model on disk.
func ExampleNew_offline() {
	cfg := ner.DefaultConfig()
	cfg.ModelsDir = "/opt/alcatraz/models"
	cfg.Offline = true

	nlp, err := ner.New(context.Background(), cfg)
	if err != nil {
		// Offline means "already cached" is a guarantee, so this error
		// names the file and the fix rather than a network failure.
		log.Fatal(err)
	}
	defer nlp.Close()

	fmt.Println(nlp.Config().SupportedEntities())
}

// ExampleConfig_segmentation analyzes tab-delimited query output. The default
// SegmentWhole feeds a whole row to the model as one sequence, and a name
// surrounded by 28 columns of UUIDs and timestamps is a sequence no
// news-trained model has a reading of, so recall drops to zero on names it
// finds in isolation. SegmentFields cuts after every newline and tab, so each
// cell is its own sequence: 94% recall against 82%, for 4.3x the inference
// calls on the measured corpus.
//
// Compiled, not executed: there is no // Output: comment because the example
// downloads a model.
func ExampleConfig_segmentation() {
	cfg := ner.DefaultConfig()
	cfg.Segmentation = ner.SegmentFields

	nlp, err := ner.New(context.Background(), cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer nlp.Close()

	// One psql --no-align row: header line, then values.
	row := "id\tname\tcity\n41\tJohn Smith\tBerlin\n"
	arts, err := nlp.ProcessText(row, "en")
	if err != nil {
		log.Fatal(err)
	}
	for _, ent := range arts.Ents {
		// Byte offsets into row, always on whole-word boundaries.
		fmt.Printf("%s %q\n", ent.EntityType, row[ent.Start:ent.End])
	}
}

// ExampleConfig_backend swaps the pure-Go backend for ONNX Runtime with
// Apple's CoreML execution provider, worth roughly 5-10x on CPU before the
// accelerator and beyond that with it. The binary must be built with
// "-tags ORT" and find an ONNX Runtime shared library at runtime; without the
// tag New fails and names the missing tag rather than running slower with no
// warning.
//
// Compiled, not executed: there is no // Output: comment because the example
// needs a model, a cgo build tag and a native library.
func ExampleConfig_backend() {
	cfg := ner.DefaultConfig()
	cfg.Backend = ner.BackendORT
	cfg.Accelerator = ner.AcceleratorCoreML
	// Empty uses hugot's platform default (/usr/local/lib on macOS).
	cfg.ORTLibraryPath = "/opt/homebrew/lib/libonnxruntime.dylib"

	nlp, err := ner.New(context.Background(), cfg)
	if err != nil {
		// Wrong build tag, missing library or an accelerator the backend
		// does not support all land here.
		log.Fatal(err)
	}
	defer nlp.Close()

	fmt.Println(nlp.Config().Backend)
}

// ExampleEnsureModelIn materialises the model during an image build, so the
// runtime process never downloads anything. Files of a pinned model are
// fetched from a pinned commit and checked against a pinned sha256.
//
// The returned path is the model directory (Config.ModelPath); the directory
// passed in is the models directory (Config.ModelsDir), which holds one
// subdirectory per model id.
//
// Compiled, not executed: there is no // Output: comment because the example
// downloads a model.
func ExampleEnsureModelIn() {
	ctx := context.Background()

	modelPath, err := ner.EnsureModelIn(ctx, "KnightsAnalytics/distilbert-NER", "/opt/alcatraz/models")
	if err != nil {
		log.Fatal(err)
	}

	// Either field works from here: ModelPath names the directory above,
	// ModelsDir names its parent and resolves Config.Model beneath it.
	cfg := ner.DefaultConfig()
	cfg.ModelPath = modelPath
	cfg.Offline = true

	nlp, err := ner.New(ctx, cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer nlp.Close()

	fmt.Println(ner.PinnedModels())
}
