package models

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
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
	// origin is the base URL the files hang off, empty for defaultOrigin. It
	// belongs to the model rather than to the downloader because the two move
	// separately: a model Hoop trains and mirrors itself is not on the hub at
	// all, while the ones that are should keep being fetched from it. Only the
	// base URL moves — the path under it stays hub-shaped — so a mirror is a
	// bucket laid out like the hub, not a second code path.
	//
	// Trusting an origin for availability is not trusting it for content. The
	// digests below are what a file is accepted against wherever it came from,
	// so the worst a wrong origin can do is fail the download.
	origin string
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

// Origin returns the base URL a model's files are fetched from — the one its
// table entry names, or Hugging Face — or "" if the model is not pinned. It is
// what a caller reports before a download; the origin argument of
// [EnsureModelFrom] is what overrides it.
func Origin(model string) string {
	art, ok := modelArtifacts[model]
	if !ok {
		return ""
	}
	if art.origin != "" {
		return art.origin
	}
	return defaultOrigin
}

// originFor applies the precedence — caller, then the pin table, then Hugging
// Face — and rejects an origin the downloader cannot use.
//
// The check is here rather than at the call site because the failure it
// prevents is unhelpful: an origin missing its scheme reaches net/http as a
// relative URL and comes back as "unsupported protocol scheme """, which names
// neither the origin nor the mistake.
func originFor(art modelArtifact, override string) (string, error) {
	origin := override
	if origin == "" {
		origin = art.origin
	}
	if origin == "" {
		origin = defaultOrigin
	}
	u, err := url.Parse(origin)
	if err != nil {
		return "", fmt.Errorf("models: origin %q is not a URL: %w", origin, err)
	}
	if u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("models: origin %q must be an http or https base URL, like %q", origin, defaultOrigin)
	}
	// A trailing slash would leave an empty path segment in the middle of the
	// URL. Most servers collapse it; S3 does not, because the key is literal,
	// and answers 404 for an object that is sitting right there. TrimRight,
	// not TrimSuffix: an origin pasted out of a config or joined by hand can
	// end in more than one, and two empty segments fail the same way as one.
	return strings.TrimRight(origin, "/"), nil
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
	return EnsureModelFrom(ctx, model, dir, "")
}

// EnsureModelFrom is EnsureModelIn fetching from an explicit origin: the base
// URL the pinned paths hang off, in place of the one the pin table names. An
// empty origin means the pinned one. It is for a build running against a
// mirror — an internal bucket, an air-gapped cache — without the model's table
// entry having to know about that build.
//
// Only the base URL moves. The layout under it is fixed at
// {origin}/{model}/resolve/{revision}/{file}, so a mirror is a bucket keyed
// like the hub rather than a second code path, and pointing at one is a flag
// rather than a format.
//
// Moving where the bytes come from does not move what is accepted: files are
// checked against the same pinned digests and byte lengths, so an origin
// serving anything else fails exactly as a corrupted transfer does and
// installs nothing. That is what makes a second origin safe to offer at all —
// an origin is trusted for availability, never for content.
func EnsureModelFrom(ctx context.Context, model, dir, origin string) (string, error) {
	art, ok := modelArtifacts[model]
	if !ok {
		return "", fmt.Errorf("models: model %q has no pinned checksums, so it cannot be verified (pinned models: %s)",
			model, strings.Join(PinnedModels(), ", "))
	}
	// Before ResolveDir, which creates directories: a rejected origin should
	// not leave a models directory behind for a download that never ran.
	origin, err := originFor(art, origin)
	if err != nil {
		return "", err
	}
	dir, err = ResolveDir(dir)
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
		src := fmt.Sprintf("%s/%s/resolve/%s/%s", origin, model, art.revision, f.path)
		if err := download(ctx, src, dest, f.sha256, f.size, 0o644); err != nil {
			return "", fmt.Errorf("models: downloading %s for model %s: %w", f.path, model, err)
		}
	}
	return modelPath, nil
}

// VerifyModelIn is EnsureModelIn's verification half without its downloading
// half: it reports that model is already present in dir and that every pinned
// file still matches, and returns the model directory. It opens no socket and
// writes nothing, so it is the resolution step for a caller that has promised
// not to touch the network (ner.Config.Offline) or that mounts its models
// read-only.
//
// Where EnsureModelIn answers a bad file by fetching it, this reports it, and
// says which file and which kind of bad. That distinction is the whole
// diagnostic: absent means the directory was never seeded, mismatched means it
// was seeded with the wrong bytes — a stale image layer, a truncated copy, a
// tampered volume — unreadable means the bytes may be fine and the process
// cannot see them, and the three have nothing to do with each other. All
// otherwise surface much later as an opaque failure inside the model runtime.
//
// dir is the models directory, not the model directory, exactly as in
// EnsureModelIn. An empty dir means the default, which unlike EnsureModelIn
// is not created: looking is not a reason to write.
func VerifyModelIn(model, dir string) (string, error) {
	art, ok := modelArtifacts[model]
	if !ok {
		return "", fmt.Errorf("models: model %q has no pinned checksums, so it cannot be verified (pinned models: %s)",
			model, strings.Join(PinnedModels(), ", "))
	}
	if dir == "" {
		var err error
		if dir, err = DefaultDir(); err != nil {
			return "", err
		}
	}
	modelPath := Dir(dir, model)
	fi, err := os.Stat(modelPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("models: no model directory at %s", modelPath)
		}
		return "", fmt.Errorf("models: cannot read %s: %w", modelPath, err)
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("models: %s is not a directory", modelPath)
	}
	for _, f := range art.files {
		// path.Base, not filepath.Base: repository paths are always
		// slash-separated, whatever the host OS uses.
		name := path.Base(f.path)
		dest := filepath.Join(modelPath, name)
		// Three outcomes, not two. A file the process cannot open is a
		// third one: reporting it as a digest mismatch would send an
		// operator to re-provision a model whose only problem is its mode,
		// and that is a likely mistake precisely here, where the model
		// arrives from a build stage or a volume that owns it.
		switch ok, err := fileMatches(dest, f.sha256); {
		case errors.Is(err, fs.ErrNotExist):
			return "", fmt.Errorf("models: %s is missing from %s", name, modelPath)
		case err != nil:
			return "", fmt.Errorf("models: cannot read %s in %s: %w", name, modelPath, err)
		case !ok:
			return "", fmt.Errorf("models: %s in %s does not match its pinned sha256", name, modelPath)
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
	dir, err := DefaultDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("models: creating the models directory: %w", err)
	}
	return dir, nil
}

// DefaultDir returns the default models directory — "alcatraz/models" under
// the user cache dir — without creating it. ResolveDir is the same lookup for
// a caller that is about to write there; this one is for a caller that only
// wants to look, or to name the directory in a message.
func DefaultDir() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("models: locating the user cache directory: %w", err)
	}
	return filepath.Join(cache, "alcatraz", "models"), nil
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
