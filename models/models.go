// Package models materialises the model files alcatraz's optional NER
// backends need, pinned to an immutable revision and verified against
// recorded sha256 digests and byte lengths.
//
// It lives in the root module rather than in ner on purpose. The alcatraz
// CLI ships "alcatraz models download" so a model can be fetched as a build
// or deploy step, and that command must not drag the ONNX model runtime —
// nor its newer Go requirement — into a module that is otherwise standard
// library only. Nothing here imports anything outside the standard library.
//
// The ner package re-exports EnsureModel, EnsureModelIn and PinnedModels, so
// NER users still have a single import.
package models

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// DefaultModel is the model ner uses unless told otherwise, and the default
// for "alcatraz models download". It is the single place the id is written
// down: ner.DefaultConfig and the pin table below both refer to this.
const DefaultModel = "KnightsAnalytics/distilbert-NER"

// modelFile is one file of a model repository, pinned by content hash.
type modelFile struct {
	// path is the file's path inside the repository. Downloads are
	// flattened to its base name in the model directory, matching how
	// hugot lays a downloaded model out.
	path   string
	sha256 string
	// size is the exact byte length. The digest is only conclusive once the
	// response ends, so on its own it bounds integrity but not consumption:
	// nothing stops an endpoint that streams until the disk is full. Capping
	// the read one byte past the pin does, and rejecting a short read too
	// turns a truncated response into an error that says so rather than an
	// opaque digest mismatch.
	size int64
}

// modelArtifact pins a model repository to an immutable revision and to the
// exact set of files alcatraz downloads from it.
type modelArtifact struct {
	// revision is a commit sha, never a branch name: a branch would let
	// the repository contents drift out from under the pinned checksums,
	// so the first thing a mismatch would tell us is nothing useful.
	revision string
	// files is the complete set alcatraz needs to load the model. It
	// mirrors what hugot's own downloader selects (the single .onnx file,
	// the tokenizer and the config files), so a directory produced here is
	// interchangeable with one produced by hugot.DownloadModel.
	files []modelFile
}

// modelArtifacts holds every model EnsureModel can verify. Adding a model
// means recording its commit sha, then the digest and byte length of each
// file at that revision:
//
//	rev=$(curl -sS https://huggingface.co/api/models/$REPO | jq -r .sha)
//	curl -sSLo /tmp/f https://huggingface.co/$REPO/resolve/$rev/$FILE
//	shasum -a 256 /tmp/f && wc -c < /tmp/f
var modelArtifacts = map[string]modelArtifact{
	DefaultModel: {
		revision: "13a742d5ea02349d17e18f3755301282c9ee33f7",
		files: []modelFile{
			{path: "config.json", sha256: "8f9f01d47f61087197f9fa85185d4a7a6248333c15af1b221aa5e8b9b76462b5", size: 925},
			{path: "model.onnx", sha256: "4440f9fc64cd28ac75d83a38d89716f25947799640cd0e5f1f9f6e57b9c14160", size: 260926482},
			{path: "special_tokens_map.json", sha256: "5d5b662e421ea9fac075174bb0688ee0d9431699900b90662acd44b2a350503a", size: 695},
			{path: "tokenizer.json", sha256: "cb26b43c98e8266ae3e99c2a583cf8315d73b33a17e6b20b4df7ff1f22392d34", size: 669021},
			{path: "tokenizer_config.json", sha256: "4391b0abb71cd639e50a333c5c642d3c8659ba34099cb12a83dba2efc26f5451", size: 1305},
			{path: "vocab.txt", sha256: "eeaa9875b23b04b4c54ef759d03db9d1ba1554838f8fb26c5d96fa551df93d02", size: 213450},
		},
	},
}

// PinnedModels returns, sorted, the model ids EnsureModel can fetch and
// verify. Other model ids still work through ner.Config.Model, but nothing
// checks what the hub serves for them.
func PinnedModels() []string {
	ids := make([]string, 0, len(modelArtifacts))
	for id := range modelArtifacts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// IsPinned reports whether EnsureModel can verify model.
func IsPinned(model string) bool {
	_, ok := modelArtifacts[model]
	return ok
}

// PinnedFile describes one verified file of a pinned model, as it lands on
// disk.
type PinnedFile struct {
	// Name is the file's name inside the model directory. Repository paths
	// are flattened to their base name on download, so this is what a
	// caller finds on disk, not the path within the repository.
	Name string
	// SHA256 is the hex digest EnsureModel verifies the file against.
	SHA256 string
	// Size is the exact byte length.
	Size int64
}

// PinnedFiles returns the files EnsureModel verifies for model, in the order
// it fetches them, or nil if the model is not pinned. It lets a caller show
// what a download will cost, and what it verified, without EnsureModel
// having to report progress itself.
func PinnedFiles(model string) []PinnedFile {
	art, ok := modelArtifacts[model]
	if !ok {
		return nil
	}
	out := make([]PinnedFile, 0, len(art.files))
	for _, f := range art.files {
		// path.Base, not filepath.Base: repository paths are always
		// slash-separated, whatever the host OS uses.
		out = append(out, PinnedFile{Name: path.Base(f.path), SHA256: f.sha256, Size: f.size})
	}
	return out
}

// Revision returns the commit sha model is pinned to, or "" if it is not
// pinned.
func Revision(model string) string {
	return modelArtifacts[model].revision
}

// EnsureModel downloads model into the models directory ner.New reads from
// and returns the local directory holding it, so a warm cache makes ner.New
// network-free and the returned path can be handed straight to
// ner.Config.ModelPath.
//
// Every file is pinned to a sha256 and a byte length, re-verified on each
// call, cache hits included: a file that exists is not evidence that it is
// the file we asked for. A mismatched file is fetched again. Re-hashing the
// whole model costs roughly half a second for the 260MB default against a
// warm page cache — small enough beside session setup that ner.New verifies
// too, rather than trusting the cache once it is populated.
//
// It is idempotent, and safe to call concurrently and from several
// processes: each file is written to a temp file and renamed into place, so
// a reader never observes a partial one.
func EnsureModel(ctx context.Context, model string) (string, error) {
	return EnsureModelIn(ctx, model, "")
}

// EnsureModelIn is EnsureModel writing into an explicit models directory,
// for callers materialising a model outside the user cache — a Docker image
// layer or a shared volume. An empty dir means the default.
//
// dir is the models directory, not the model directory: the model lands in a
// subdirectory named after it, exactly as under the cache, so the layout is
// the same wherever it is built.
func EnsureModelIn(ctx context.Context, model, dir string) (string, error) {
	art, ok := modelArtifacts[model]
	if !ok {
		return "", fmt.Errorf("models: model %q has no pinned checksums, so it cannot be verified (pinned models: %s)",
			model, strings.Join(PinnedModels(), ", "))
	}
	dir, err := ResolveDir(dir)
	if err != nil {
		return "", err
	}
	modelPath := Dir(dir, model)
	if err := os.MkdirAll(modelPath, 0o755); err != nil {
		return "", fmt.Errorf("models: creating model directory: %w", err)
	}
	for _, f := range art.files {
		// path.Base, not filepath.Base: repository paths are always
		// slash-separated, whatever the host OS uses.
		dest := filepath.Join(modelPath, path.Base(f.path))
		if cachedFileValid(dest, f.sha256) {
			continue
		}
		url := fmt.Sprintf("%s/%s/resolve/%s/%s", hubBaseURL, model, art.revision, f.path)
		if err := download(ctx, url, dest, f.sha256, f.size, 0o644); err != nil {
			return "", fmt.Errorf("models: downloading %s for model %s: %w", f.path, model, err)
		}
	}
	return modelPath, nil
}

// ResolveDir returns dir, or (creating it) the default models directory when
// dir is empty: "alcatraz/models" under the user cache dir. This is the
// directory ner.Config.ModelsDir names — the parent of what EnsureModelIn
// returns, not the model directory itself.
func ResolveDir(dir string) (string, error) {
	if dir != "" {
		return dir, nil
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("models: locating the user cache directory: %w", err)
	}
	dir = filepath.Join(cache, "alcatraz", "models")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("models: creating the models directory: %w", err)
	}
	return dir, nil
}

// Dir returns the directory a model occupies inside a models directory. It
// reproduces hugot's naming so both downloaders agree on where a model
// lives: the id with "/" replaced by "_", minus any ":suffix".
func Dir(modelsDir, model string) string {
	if i := strings.Index(model, ":"); i >= 0 {
		model = model[:i]
	}
	return filepath.Join(modelsDir, strings.ReplaceAll(model, "/", "_"))
}
