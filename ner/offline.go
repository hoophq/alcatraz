package ner

import (
	"errors"
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
			return "", fmt.Errorf("offline: %w; pre-download it with: %s", err, downloadCmd(cfg))
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
//
// The values are quoted because they are the caller's, not ours. A models
// directory under "Application Support" or "Program Files" would otherwise
// produce a command that still runs and writes somewhere else entirely, which
// is a worse failure than one that does not run at all.
func downloadCmd(cfg Config) string {
	cmd := "alcatraz models download"
	if cfg.ModelsDir != "" {
		cmd += fmt.Sprintf(" --dest %q", cfg.ModelsDir)
	}
	if cfg.Model != models.DefaultModel {
		cmd += fmt.Sprintf(" --model %q", cfg.Model)
	}
	return cmd
}

// checkModelDir reports the first reason hugot could not open modelPath as a
// token-classification model: the label map, the tokenizer and the weights
// are all required, and the runtime's own complaint about a missing one names
// neither the file nor the directory.
//
// It rejects only what hugot would also reject, with one deliberate
// exception: a Config.OnnxFilename naming a file that is not there. hugot
// ignores OnnxFilename entirely when the directory holds exactly one .onnx
// file, so a stale name there loads whatever is present — a full-precision
// export where a quantized one was asked for — and says nothing. Offline is
// the mode for being sure which model is loading, so that is an error here.
func checkModelDir(modelPath, onnxFilename string) error {
	fi, err := os.Stat(modelPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("no model directory at %s", modelPath)
		}
		return fmt.Errorf("cannot read %s: %w", modelPath, err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s is not a directory", modelPath)
	}
	for _, name := range []string{"config.json", "tokenizer.json"} {
		switch err := readable(filepath.Join(modelPath, name)); {
		case errors.Is(err, fs.ErrNotExist):
			return fmt.Errorf("%s is missing from %s", name, modelPath)
		case err != nil:
			return fmt.Errorf("cannot read %s in %s: %w", name, modelPath, err)
		}
	}
	onnx := findONNX(modelPath, onnxFilename)
	if onnx == "" {
		if onnxFilename != "" {
			return fmt.Errorf("%s is missing from %s", onnxFilename, modelPath)
		}
		return fmt.Errorf("no .onnx file in %s", modelPath)
	}
	if err := readable(onnx); err != nil {
		return fmt.Errorf("cannot read %s: %w", onnx, err)
	}
	return nil
}

// readable reports whether the process can actually open path. os.Stat cannot
// answer that — a file with mode 0 stats perfectly and opens never — and a
// model directory that arrives from a build stage or a mounted volume is
// exactly where the two come apart. hugot's complaint about an unreadable
// file is as opaque as its complaint about an absent one, so the pre-flight
// has to tell them apart itself.
func readable(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	return f.Close()
}

// findONNX returns the path of the .onnx file dir holds anywhere beneath it,
// named want when want is not empty, or "" if there is none.
//
// Both halves mirror hugot's own resolution (backends.getOnnxFiles): it walks
// the model directory recursively, tests the ".onnx" suffix case-sensitively,
// and compares Config.OnnxFilename against the base name wherever the file
// sits. So weights under onnx/ count, a named file under onnx/ counts, and
// MODEL.ONNX does not. Matching more loosely than hugot would wave through a
// directory that then fails inside the runtime with the opaque error this
// check exists to replace.
func findONNX(dir, want string) string {
	found := ""
	// The error from WalkDir itself is discarded: this answers one question,
	// and an unreadable subtree is not evidence either way. The caller above
	// has already established that dir exists and is a directory.
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".onnx") {
			return nil
		}
		if want != "" && d.Name() != want {
			return nil
		}
		found = p
		return fs.SkipAll
	})
	return found
}
