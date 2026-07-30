package ner

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/hoophq/alcatraz/models"
)

// The Config.Offline load path. Nothing in this file opens a socket, and
// nothing writes: not the directory lookup ([models.DefaultDir], unlike
// ResolveDir, does not create), not the verification, not the file checks.
// A model directory mounted read-only loads through here unchanged.

// offlineModel resolves cfg.Model against the local models directory, and
// reports what is wrong with it rather than fetching anything.
func offlineModel(cfg Config) (string, error) {
	dir := cfg.ModelsDir
	if dir == "" {
		var err error
		if dir, err = models.DefaultDir(); err != nil {
			return "", err
		}
	}

	// Pinned models get the checksums as well as the presence check. Offline
	// is the mode where that matters most: a mismatch here cannot be repaired
	// by re-fetching the file, so it has to be reported, and the operator who
	// seeded the volume is the only one who can act on it.
	if models.IsPinned(cfg.Model) {
		modelPath, err := models.VerifyModelIn(cfg.Model, dir)
		if err != nil {
			return "", fmt.Errorf("offline: %w; pre-download it with %q", err, downloadCmd(cfg))
		}
		return modelPath, nil
	}

	// Unpinned: nothing to verify against, and no alcatraz command that
	// fetches it, so the check is presence and the advice is to seed the
	// directory by hand.
	modelPath := models.Dir(dir, cfg.Model)
	if err := checkModelDir(modelPath, cfg.OnnxFilename); err != nil {
		return "", fmt.Errorf("offline: %w; %q is not a pinned model, so seed that directory yourself (pinned: %s)",
			err, cfg.Model, strings.Join(models.PinnedModels(), ", "))
	}
	return modelPath, nil
}

// downloadCmd is the CLI invocation that seeds the directory this Config
// reads from. An empty ModelsDir is the default cache, which is exactly what
// a bare invocation warms, so the advice is copy-pasteable either way.
func downloadCmd(cfg Config) string {
	cmd := "alcatraz models download"
	if cfg.ModelsDir != "" {
		cmd += " --dest " + cfg.ModelsDir
	}
	if cfg.Model != models.DefaultModel {
		cmd += " --model " + cfg.Model
	}
	return cmd
}

// checkModelDir reports the first reason hugot could not open modelPath as a
// token-classification model: the label map, the tokenizer and the weights
// are all required, and the runtime's own complaint about a missing one names
// neither the file nor the directory.
//
// It only rejects what certainly cannot load. A pre-flight check that guessed
// too much would refuse a directory that works, which is worse than the
// opaque error it set out to replace.
func checkModelDir(modelPath, onnxFilename string) error {
	fi, err := os.Stat(modelPath)
	if err != nil {
		return fmt.Errorf("no model directory at %s", modelPath)
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s is not a directory", modelPath)
	}
	for _, name := range []string{"config.json", "tokenizer.json"} {
		if _, err := os.Stat(filepath.Join(modelPath, name)); err != nil {
			return fmt.Errorf("%s is missing from %s", name, modelPath)
		}
	}
	if onnxFilename != "" {
		if _, err := os.Stat(filepath.Join(modelPath, onnxFilename)); err != nil {
			return fmt.Errorf("%s is missing from %s", onnxFilename, modelPath)
		}
		return nil
	}
	if !hasONNX(modelPath) {
		return fmt.Errorf("no .onnx file in %s", modelPath)
	}
	return nil
}

// hasONNX reports whether dir holds an .onnx file anywhere beneath it. It
// walks rather than testing for "model.onnx" because an empty
// Config.OnnxFilename means "the single .onnx file in the repository",
// whatever it is called and wherever the export put it.
func hasONNX(dir string) bool {
	found := false
	// The error from WalkDir itself is discarded: this answers one question,
	// and an unreadable subtree is not evidence either way. The callers above
	// have already established that dir exists and is a directory.
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && strings.EqualFold(filepath.Ext(d.Name()), ".onnx") {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	return found
}
