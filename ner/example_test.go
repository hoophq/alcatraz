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
