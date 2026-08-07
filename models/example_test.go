package models_test

import (
	"context"
	"fmt"
	"log"
	"path/filepath"

	"github.com/hoophq/alcatraz/models"
)

// The pin table is the package's contract: which model ids can be verified,
// which immutable revision each is frozen at, and exactly which files land on
// disk. Reading it costs nothing and touches no network.
func ExamplePinnedModels() {
	for _, id := range models.PinnedModels() {
		fmt.Printf("%s @ %s\n", id, models.Revision(id))
		var total int64
		for _, f := range models.PinnedFiles(id) {
			total += f.Size
			fmt.Printf("  %-24s %10d  %s\n", f.Name, f.Size, f.SHA256[:12])
		}
		fmt.Printf("  %-24s %10d bytes total\n", "", total)
	}
	fmt.Println("default:", models.DefaultModel, "pinned:", models.IsPinned(models.DefaultModel))
	// Output:
	// KnightsAnalytics/distilbert-NER @ 13a742d5ea02349d17e18f3755301282c9ee33f7
	//   config.json                     925  8f9f01d47f61
	//   model.onnx                260926482  4440f9fc64cd
	//   special_tokens_map.json         695  5d5b662e421e
	//   tokenizer.json               669021  cb26b43c98e8
	//   tokenizer_config.json          1305  4391b0abb71c
	//   vocab.txt                    213450  eeaa9875b23b
	//                             261811878 bytes total
	// default: KnightsAnalytics/distilbert-NER pinned: true
}

// Two paths, and mixing them up is the usual mistake. Dir shows the relation:
// the models directory is the parent (ner.Config.ModelsDir), the model
// directory is one model's subdirectory inside it (ner.Config.ModelPath).
func ExampleDir() {
	modelsDir := "/opt/alcatraz/models"
	fmt.Println(filepath.ToSlash(models.Dir(modelsDir, models.DefaultModel)))
	// Output:
	// /opt/alcatraz/models/KnightsAnalytics_distilbert-NER
}

// EnsureModelIn materialises a model into an explicit models directory, such
// as a Docker image layer or a shared volume, instead of the user cache. The
// download then happens at build time and the runtime never opens a socket.
// VerifyModelIn is the same check without the fetching, for the process that
// later reads that directory.
//
// This example downloads roughly 260MB, so it has no output block and is only
// compiled.
func ExampleEnsureModelIn() {
	ctx := context.Background()
	modelsDir := "/opt/alcatraz/models"

	// Build step: fetch and verify. Idempotent, and safe to run concurrently.
	modelPath, err := models.EnsureModelIn(ctx, models.DefaultModel, modelsDir)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("model ready at", modelPath)

	// Run step: prove the bytes are still the pinned ones, without network
	// access. A failure names the file and the kind of bad: absent,
	// mismatched or unreadable.
	verified, err := models.VerifyModelIn(models.DefaultModel, modelsDir)
	if err != nil {
		log.Fatalf("models directory is not usable: %v", err)
	}
	fmt.Println("verified", verified)
}
