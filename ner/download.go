package ner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// hubBaseURL is the Hugging Face origin the pinned artifacts are fetched
// from. It is a var so tests can point it at a local server.
var hubBaseURL = "https://huggingface.co"

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
	"KnightsAnalytics/distilbert-NER": {
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
// verify. Other model ids still work through Config.Model, but nothing
// checks what the hub serves for them.
func PinnedModels() []string {
	ids := make([]string, 0, len(modelArtifacts))
	for id := range modelArtifacts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// EnsureModel downloads model into the models directory New reads from and
// returns the local directory holding it, so a warm cache makes New
// network-free and the returned path can be handed straight to
// Config.ModelPath.
//
// Every file is pinned to a sha256 and a byte length, re-verified on each
// call, cache hits included: a file that exists is not evidence that it is
// the file we asked for. A mismatched file is fetched again. Re-hashing the
// whole model costs roughly half a second for the 260MB default against a
// warm page cache — small enough beside session setup that New verifies too,
// rather than trusting the cache once it is populated.
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
		return "", fmt.Errorf("ner: model %q has no pinned checksums, so it cannot be verified (pinned models: %s)",
			model, strings.Join(PinnedModels(), ", "))
	}
	dir, err := resolveModelsDir(dir)
	if err != nil {
		return "", err
	}
	modelPath := modelDir(dir, model)
	if err := os.MkdirAll(modelPath, 0o755); err != nil {
		return "", fmt.Errorf("ner: creating model directory: %w", err)
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
			return "", fmt.Errorf("ner: downloading %s for model %s: %w", f.path, model, err)
		}
	}
	return modelPath, nil
}

// resolveModelsDir returns dir, or (creating it) the default models
// directory when dir is empty: "alcatraz/models" under the user cache dir.
func resolveModelsDir(dir string) (string, error) {
	if dir != "" {
		return dir, nil
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("ner: locating the user cache directory: %w", err)
	}
	dir = filepath.Join(cache, "alcatraz", "models")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("ner: creating the models directory: %w", err)
	}
	return dir, nil
}

// modelDir returns the directory a model occupies inside a models
// directory. It reproduces hugot's naming so both downloaders agree on
// where a model lives: the id with "/" replaced by "_", minus any ":suffix".
func modelDir(modelsDir, model string) string {
	if i := strings.Index(model, ":"); i >= 0 {
		model = model[:i]
	}
	return filepath.Join(modelsDir, strings.ReplaceAll(model, "/", "_"))
}

// cachedFileValid reports whether path exists and still matches the pinned
// sha256.
//
// A mismatched file is left where it is rather than unlinked: download
// replaces it by renaming over it, which is atomic, and unlinking here is
// not. Two callers that find the same corrupt file both hold handles to it,
// and the slower one would unlink the pathname after the faster one had
// already installed a verified replacement under it — deleting a good file
// that its caller had been told was ready.
func cachedFileValid(path, wantSHA256 string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return false
	}
	return hex.EncodeToString(hasher.Sum(nil)) == wantSHA256
}

// download fetches url into dest atomically: it streams to a temp file in
// the destination directory, verifies the byte length and the sha256, sets
// mode, and renames. A failed, corrupt, truncated or oversized download never
// leaves a file at dest.
func download(ctx context.Context, url, dest, wantSHA256 string, wantSize int64, mode os.FileMode) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: unexpected status %s", url, resp.Status)
	}

	tmp, err := os.CreateTemp(filepath.Dir(dest), filepath.Base(dest)+".partial-*")
	if err != nil {
		return err
	}
	defer func() {
		tmp.Close()
		os.Remove(tmp.Name())
	}()

	hasher := sha256.New()
	// Bounded one byte past the pin, so an endpoint that never stops
	// sending is cut off rather than filling the disk before the digest
	// gets a chance to reject anything. The bound is on bytes actually
	// read, not on Content-Length, which the endpoint also controls.
	n, err := io.Copy(io.MultiWriter(tmp, hasher), io.LimitReader(resp.Body, wantSize+1))
	if err != nil {
		return err
	}
	if n != wantSize {
		return fmt.Errorf("size mismatch for %s: got %d bytes, want %d", url, n, wantSize)
	}
	if got := hex.EncodeToString(hasher.Sum(nil)); got != wantSHA256 {
		return fmt.Errorf("sha256 mismatch for %s: got %s, want %s", url, got, wantSHA256)
	}
	if err := tmp.Chmod(mode); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dest)
}
