package models

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
)

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// fakeHub serves a pinned test model and counts the requests it gets, so a
// test can assert that a warm cache costs no network at all.
type fakeHub struct {
	*httptest.Server
	mu    sync.Mutex
	hits  map[string]int
	files map[string][]byte
}

func newFakeHub(t *testing.T, files map[string][]byte) *fakeHub {
	t.Helper()
	h := &fakeHub{hits: map[string]int{}, files: files}
	h.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		h.hits[r.URL.Path]++
		h.mu.Unlock()

		body, ok := h.files[fileOfURL(r.URL.Path)]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(body)
	}))
	t.Cleanup(h.Close)
	return h
}

func (h *fakeHub) totalHits() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, c := range h.hits {
		n += c
	}
	return n
}

// paths returns the request paths the hub saw, sorted.
func (h *fakeHub) paths() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, 0, len(h.hits))
	for p := range h.hits {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// fileOfURL pulls the file path out of /{org}/{repo}/resolve/{rev}/{file}.
func fileOfURL(p string) string {
	parts := strings.SplitN(strings.TrimPrefix(p, "/"), "/", 5)
	if len(parts) < 5 {
		return ""
	}
	return parts[4]
}

// pinTestModel registers a model served by hub for the duration of the test.
// The origin travels in the artifact, so a test model is pointed at a local
// server the same way a real one would be pointed at a mirror.
func pinTestModel(t *testing.T, hub *fakeHub, id string, art modelArtifact) {
	t.Helper()
	if _, exists := modelArtifacts[id]; exists {
		t.Fatalf("test model id %q collides with a real pinned model", id)
	}
	if art.origin == "" {
		art.origin = hub.URL
	}
	modelArtifacts[id] = art
	t.Cleanup(func() { delete(modelArtifacts, id) })
}

// testModel is a two-file stand-in for a real model directory.
func testModel(t *testing.T) (id string, files map[string][]byte, art modelArtifact) {
	t.Helper()
	files = map[string][]byte{
		"tokenizer.json": []byte(`{"tokenizer":"test"}`),
		"model.onnx":     []byte("not really onnx, but hashed the same way"),
	}
	return "alcatraz-test/fixture-NER", files, modelArtifact{
		revision: "0000000000000000000000000000000000000000",
		files: []modelFile{
			{path: "tokenizer.json", sha256: sum(files["tokenizer.json"]), size: int64(len(files["tokenizer.json"]))},
			{path: "model.onnx", sha256: sum(files["model.onnx"]), size: int64(len(files["model.onnx"]))},
		},
	}
}

// assertNoLeftovers checks that a rejected download for name left nothing
// usable in dir: neither the file itself nor a .partial-* temp file.
func assertNoLeftovers(t *testing.T, dir, name string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() == name || strings.Contains(e.Name(), ".partial-") {
			t.Errorf("leftover file %q after a rejected download", e.Name())
		}
	}
}

func TestEnsureModelInDownloadsAndVerifies(t *testing.T) {
	id, files, art := testModel(t)
	hub := newFakeHub(t, files)
	pinTestModel(t, hub, id, art)

	dir := t.TempDir()
	got, err := EnsureModelIn(context.Background(), id, dir)
	if err != nil {
		t.Fatalf("EnsureModelIn: %v", err)
	}

	want := filepath.Join(dir, "alcatraz-test_fixture-NER")
	if got != want {
		t.Fatalf("model dir = %q, want %q", got, want)
	}
	for name, body := range files {
		p := filepath.Join(got, name)
		onDisk, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		if string(onDisk) != string(body) {
			t.Errorf("%s = %q, want %q", name, onDisk, body)
		}
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0o644 {
			t.Errorf("%s mode = %v, want 0644", name, info.Mode().Perm())
		}
	}
	// Nothing but the pinned files, so no .partial-* survives a success.
	entries, err := os.ReadDir(got)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(files) {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("model dir holds %v, want exactly the %d pinned files", names, len(files))
	}
}

func TestEnsureModelInWarmCacheSkipsNetwork(t *testing.T) {
	id, files, art := testModel(t)
	hub := newFakeHub(t, files)
	pinTestModel(t, hub, id, art)

	dir := t.TempDir()
	if _, err := EnsureModelIn(context.Background(), id, dir); err != nil {
		t.Fatalf("first EnsureModelIn: %v", err)
	}
	after := hub.totalHits()
	if after != len(files) {
		t.Fatalf("cold cache made %d requests, want %d", after, len(files))
	}

	if _, err := EnsureModelIn(context.Background(), id, dir); err != nil {
		t.Fatalf("second EnsureModelIn: %v", err)
	}
	if hub.totalHits() != after {
		t.Errorf("warm cache made %d extra requests, want 0", hub.totalHits()-after)
	}
}

func TestEnsureModelInRepairsTamperedCache(t *testing.T) {
	id, files, art := testModel(t)
	hub := newFakeHub(t, files)
	pinTestModel(t, hub, id, art)

	dir := t.TempDir()
	modelPath, err := EnsureModelIn(context.Background(), id, dir)
	if err != nil {
		t.Fatalf("EnsureModelIn: %v", err)
	}

	// A file that exists is not evidence it is the file we pinned.
	tampered := filepath.Join(modelPath, "model.onnx")
	if err := os.WriteFile(tampered, []byte("malicious"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := hub.totalHits()

	if _, err := EnsureModelIn(context.Background(), id, dir); err != nil {
		t.Fatalf("EnsureModelIn after tampering: %v", err)
	}
	if hub.totalHits() != before+1 {
		t.Errorf("made %d requests to repair, want 1", hub.totalHits()-before)
	}
	repaired, err := os.ReadFile(tampered)
	if err != nil {
		t.Fatal(err)
	}
	if string(repaired) != string(files["model.onnx"]) {
		t.Errorf("model.onnx = %q, want the pinned content restored", repaired)
	}
}

func TestEnsureModelInRejectsChecksumMismatch(t *testing.T) {
	id, files, art := testModel(t)
	// The hub serves something other than what we pinned, at exactly the
	// pinned length: the digest has to be what rejects it, not the size.
	files["model.onnx"] = []byte(strings.Repeat("z", len(files["model.onnx"])))
	hub := newFakeHub(t, files)
	pinTestModel(t, hub, id, art)

	dir := t.TempDir()
	_, err := EnsureModelIn(context.Background(), id, dir)
	if err == nil {
		t.Fatal("EnsureModelIn succeeded, want a sha256 mismatch error")
	}
	if !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Errorf("error = %v, want it to name the sha256 mismatch", err)
	}
	assertNoLeftovers(t, Dir(dir, id), "model.onnx")
}

// TestEnsureModelInRejectsOversizedResponse covers the case the digest alone
// cannot: a response that never ends. The read is bounded one byte past the
// pin, so an endpoint streaming far more than the pinned length is cut off
// and rejected rather than filling the disk while the hash waits for EOF.
func TestEnsureModelInRejectsOversizedResponse(t *testing.T) {
	id, files, art := testModel(t)
	pinned := len(files["model.onnx"])
	files["model.onnx"] = []byte(strings.Repeat("x", 100*pinned))
	hub := newFakeHub(t, files)
	pinTestModel(t, hub, id, art)

	dir := t.TempDir()
	_, err := EnsureModelIn(context.Background(), id, dir)
	if err == nil {
		t.Fatal("EnsureModelIn succeeded, want a size mismatch error")
	}
	if !strings.Contains(err.Error(), "size mismatch") {
		t.Errorf("error = %v, want it to name the size mismatch", err)
	}
	assertNoLeftovers(t, Dir(dir, id), "model.onnx")
}

// TestEnsureModelInRejectsTruncatedResponse checks that a short read fails as
// a truncation rather than as an opaque digest mismatch, so the error says
// what actually went wrong.
func TestEnsureModelInRejectsTruncatedResponse(t *testing.T) {
	id, files, art := testModel(t)
	files["model.onnx"] = files["model.onnx"][:5]
	hub := newFakeHub(t, files)
	pinTestModel(t, hub, id, art)

	dir := t.TempDir()
	_, err := EnsureModelIn(context.Background(), id, dir)
	if err == nil {
		t.Fatal("EnsureModelIn succeeded, want a size mismatch error")
	}
	if !strings.Contains(err.Error(), "size mismatch") {
		t.Errorf("error = %v, want it to name the size mismatch", err)
	}
	assertNoLeftovers(t, Dir(dir, id), "model.onnx")
}

func TestEnsureModelInMissingFileLeavesNoPartial(t *testing.T) {
	id, files, art := testModel(t)
	delete(files, "model.onnx") // hub 404s for it
	hub := newFakeHub(t, files)
	pinTestModel(t, hub, id, art)

	dir := t.TempDir()
	_, err := EnsureModelIn(context.Background(), id, dir)
	if err == nil {
		t.Fatal("EnsureModelIn succeeded, want an error for the missing file")
	}
	if !strings.Contains(err.Error(), "model.onnx") {
		t.Errorf("error = %v, want it to name the file that failed", err)
	}
	assertNoLeftovers(t, Dir(dir, id), "model.onnx")
}

func TestOrigin(t *testing.T) {
	if got := Origin(DefaultModel); got != defaultOrigin {
		t.Errorf("Origin(default) = %q, want the hub %q", got, defaultOrigin)
	}
	// alcatraz is a public repository. A model that names no origin has to
	// keep coming from the hub, or every outside user starts fetching from
	// infrastructure that exists for Hoop's own deployments.
	if art := modelArtifacts[DefaultModel]; art.origin != "" {
		t.Errorf("the default model pins origin %q; it should fall back to the hub", art.origin)
	}
	if got := Origin("some-org/unpinned-model"); got != "" {
		t.Errorf("Origin(unpinned) = %q, want empty", got)
	}

	id, files, art := testModel(t)
	art.origin = "https://mirror.internal/models"
	pinTestModel(t, newFakeHub(t, files), id, art)
	if got := Origin(id); got != art.origin {
		t.Errorf("Origin(%s) = %q, want the pinned origin %q", id, got, art.origin)
	}
}

// TestEnsureModelFromOverridesThePinnedOrigin is the case a build against an
// internal mirror needs: the caller redirects the fetch without the model's
// table entry knowing anything about that build.
func TestEnsureModelFromOverridesThePinnedOrigin(t *testing.T) {
	id, files, art := testModel(t)
	pinned, mirror := newFakeHub(t, files), newFakeHub(t, files)
	art.origin = pinned.URL
	pinTestModel(t, pinned, id, art)

	if _, err := EnsureModelFrom(context.Background(), id, t.TempDir(), mirror.URL); err != nil {
		t.Fatalf("EnsureModelFrom: %v", err)
	}
	if got := mirror.totalHits(); got != len(files) {
		t.Errorf("the mirror served %d files, want %d", got, len(files))
	}
	if got := pinned.totalHits(); got != 0 {
		t.Errorf("the pinned origin was contacted %d times, want 0", got)
	}
}

// TestEnsureModelFromKeepsTheHubLayout pins the shape of the URL under the
// origin. A mirror is a bucket keyed like the hub, so the only difference
// between origins must be the base URL — and trailing slashes on it must not
// leak empty path segments into the key, which S3 would answer 404 for. One
// is the copy-paste; more than one is the config value joined to a path that
// already had it.
func TestEnsureModelFromKeepsTheHubLayout(t *testing.T) {
	for _, suffix := range []string{"", "/", "///"} {
		t.Run("origin"+suffix, func(t *testing.T) {
			id, files, art := testModel(t)
			hub := newFakeHub(t, files)
			pinTestModel(t, hub, id, art)

			if _, err := EnsureModelFrom(context.Background(), id, t.TempDir(), hub.URL+suffix); err != nil {
				t.Fatalf("EnsureModelFrom: %v", err)
			}
			want := []string{
				"/" + id + "/resolve/" + art.revision + "/model.onnx",
				"/" + id + "/resolve/" + art.revision + "/tokenizer.json",
			}
			sort.Strings(want)
			if got := hub.paths(); !slices.Equal(got, want) {
				t.Errorf("requested %v, want %v", got, want)
			}
		})
	}
}

// TestEnsureModelFromRejectsUnusableOrigin keeps the failure at the origin the
// operator typed. Left to net/http, a missing scheme surfaces as
// `unsupported protocol scheme ""`, which names neither.
func TestEnsureModelFromRejectsUnusableOrigin(t *testing.T) {
	id, files, art := testModel(t)
	pinTestModel(t, newFakeHub(t, files), id, art)

	for _, origin := range []string{
		"huggingface.co",        // no scheme
		"ftp://mirror.internal", // not a scheme we can fetch over
		"file:///mnt/models",    // a mirror is fetched, not mounted
		"https://",              // no host
		"://mirror.internal",    // not a URL at all
	} {
		t.Run(origin, func(t *testing.T) {
			// A models directory that does not exist yet: a rejected origin
			// must not create one for a download that never ran.
			dir := filepath.Join(t.TempDir(), "models")
			_, err := EnsureModelFrom(context.Background(), id, dir, origin)
			if err == nil {
				t.Fatalf("EnsureModelFrom accepted origin %q", origin)
			}
			if !strings.Contains(err.Error(), origin) {
				t.Errorf("error = %v, want it to name the origin %q", err, origin)
			}
			if _, err := os.Stat(dir); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("a rejected origin created %s", dir)
			}
		})
	}
}

// TestEnsureModelFromMirrorCannotSubstituteBytes is why a second origin is
// safe to offer. An origin is trusted to have the file, never to decide what
// is in it: the digests are the same wherever the bytes came from, so a
// mirror serving something else fails exactly as a corrupt transfer does.
func TestEnsureModelFromMirrorCannotSubstituteBytes(t *testing.T) {
	id, files, art := testModel(t)
	pinTestModel(t, newFakeHub(t, files), id, art)

	// Same length as the pin, so the digest is what has to reject it.
	swapped := map[string][]byte{}
	for name, body := range files {
		swapped[name] = []byte(strings.Repeat("z", len(body)))
	}
	mirror := newFakeHub(t, swapped)

	dir := t.TempDir()
	_, err := EnsureModelFrom(context.Background(), id, dir, mirror.URL)
	if err == nil {
		t.Fatal("EnsureModelFrom accepted a mirror serving other bytes")
	}
	if !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Errorf("error = %v, want it to name the sha256 mismatch", err)
	}
	for name := range files {
		assertNoLeftovers(t, Dir(dir, id), name)
	}
}

func TestEnsureModelUnpinnedModel(t *testing.T) {
	_, err := EnsureModel(context.Background(), "some-org/unpinned-model")
	if err == nil {
		t.Fatal("EnsureModel succeeded for an unpinned model, want an error")
	}
	// The message has to be actionable: which ids can be verified.
	for _, want := range []string{"some-org/unpinned-model", "KnightsAnalytics/distilbert-NER"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to mention %q", err, want)
		}
	}
}

func TestEnsureModelInCancelledContext(t *testing.T) {
	id, files, art := testModel(t)
	hub := newFakeHub(t, files)
	pinTestModel(t, hub, id, art)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := EnsureModelIn(ctx, id, t.TempDir())
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want it to wrap context.Canceled", err)
	}
}

func TestEnsureModelInConcurrent(t *testing.T) {
	id, files, art := testModel(t)
	hub := newFakeHub(t, files)
	pinTestModel(t, hub, id, art)

	dir := t.TempDir()
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = EnsureModelIn(context.Background(), id, dir)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: %v", i, err)
		}
	}
	// Whoever won each rename, the result must be the pinned bytes and
	// nothing else — no torn file, no surviving temp file.
	entries, err := os.ReadDir(Dir(dir, id))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(files) {
		t.Errorf("model dir holds %d entries, want %d", len(entries), len(files))
	}
	for name, body := range files {
		got, err := os.ReadFile(filepath.Join(Dir(dir, id), name))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(body) {
			t.Errorf("%s = %q, want %q", name, got, body)
		}
	}
}

// TestCachedFileValidLeavesMismatchInPlace pins the decision that makes
// repair safe under concurrency: validation reports, it does not clean up.
// Unlinking here is not atomic against the rename that replaces the file, so
// a slow validator could remove a pathname after a faster caller had already
// installed a verified copy under it. The mismatched file stays until
// download renames over it.
func TestCachedFileValidLeavesMismatchInPlace(t *testing.T) {
	p := filepath.Join(t.TempDir(), "model.onnx")
	if err := os.WriteFile(p, []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	if cachedFileValid(p, sum([]byte("pinned"))) {
		t.Fatal("cachedFileValid accepted a file that does not match the pin")
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("mismatched file was removed: %v", err)
	}
}

// TestEnsureModelInConcurrentRepair points several callers at the same
// corrupt cache file at once. Each one has to end up with the pinned bytes
// readable at the pathname it was handed: repair replaces the file by
// renaming over it, so a caller that finished later must never remove what an
// earlier one already installed and reported as ready.
//
// Interleavings are not deterministic, so this exercises the path rather than
// proving it; -race and -count make it worth its runtime.
func TestEnsureModelInConcurrentRepair(t *testing.T) {
	id, files, art := testModel(t)
	hub := newFakeHub(t, files)
	pinTestModel(t, hub, id, art)

	dir := t.TempDir()
	if _, err := EnsureModelIn(context.Background(), id, dir); err != nil {
		t.Fatalf("seeding the cache: %v", err)
	}
	tampered := filepath.Join(Dir(dir, id), "model.onnx")
	if err := os.WriteFile(tampered, []byte("malicious"), 0o644); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make([]error, 8)
	got := make([][]byte, len(errs))
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := EnsureModelIn(context.Background(), id, dir); err != nil {
				errs[i] = err
				return
			}
			// Read through the pathname the caller was told is ready.
			got[i], errs[i] = os.ReadFile(tampered)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: %v", i, err)
			continue
		}
		if string(got[i]) != string(files["model.onnx"]) {
			t.Errorf("goroutine %d read %q, want the pinned content", i, got[i])
		}
	}
}

func TestModelDir(t *testing.T) {
	cases := []struct{ model, want string }{
		{"KnightsAnalytics/distilbert-NER", "KnightsAnalytics_distilbert-NER"},
		{"bert-base-NER", "bert-base-NER"},
		// hugot drops a ":variant" suffix; we must land in the same place.
		{"org/model:quantized", "org_model"},
	}
	for _, c := range cases {
		if got := Dir("/models", c.model); got != filepath.Join("/models", c.want) {
			t.Errorf("Dir(%q) = %q, want .../%s", c.model, got, c.want)
		}
	}
}

// TestPinnedArtifactsWellFormed guards the pin table itself: a branch name
// where a commit sha belongs would let the upstream repository change
// underneath the checksums, which is the whole thing pinning prevents.
func TestPinnedArtifactsWellFormed(t *testing.T) {
	commit := regexp.MustCompile(`^[0-9a-f]{40}$`)
	digest := regexp.MustCompile(`^[0-9a-f]{64}$`)

	for id, art := range modelArtifacts {
		if !commit.MatchString(art.revision) {
			t.Errorf("%s: revision %q is not a 40-hex commit sha", id, art.revision)
		}
		// An origin in the table is never validated at call time — Origin
		// hands it straight back — so a malformed one has to fail here rather
		// than as a transport error on somebody's build.
		if art.origin != "" {
			if _, err := originFor(art, ""); err != nil {
				t.Errorf("%s: %v", id, err)
			}
			if strings.HasSuffix(art.origin, "/") {
				t.Errorf("%s: origin %q has a trailing slash", id, art.origin)
			}
		}
		seen := map[string]bool{}
		for _, f := range art.files {
			if !digest.MatchString(f.sha256) {
				t.Errorf("%s: %s has a malformed sha256 %q", id, f.path, f.sha256)
			}
			// A zero size would cap every read at one byte, so a missing
			// size has to fail here rather than at download time.
			if f.size <= 0 {
				t.Errorf("%s: %s has a non-positive size %d", id, f.path, f.size)
			}
			if seen[f.path] {
				t.Errorf("%s: %s is pinned twice", id, f.path)
			}
			seen[f.path] = true
		}
		// hugot's pipeline loader needs all three to build a session.
		for _, required := range []string{"model.onnx", "tokenizer.json", "config.json"} {
			if !seen[required] {
				t.Errorf("%s: %s is not pinned", id, required)
			}
		}
	}
}

func TestDefaultModelIsPinned(t *testing.T) {
	if !IsPinned(DefaultModel) {
		t.Errorf("default model %q is not pinned, so it would be fetched unverified", DefaultModel)
	}
}

// TestPinnedFiles checks the view the CLI builds its manifest from: the same
// files as the pin table, in the same order, named as they land on disk
// rather than as they sit in the repository.
func TestPinnedFiles(t *testing.T) {
	if got := PinnedFiles("some-org/unpinned-model"); got != nil {
		t.Errorf("PinnedFiles(unpinned) = %v, want nil", got)
	}

	id, files, art := testModel(t)
	art.files = append(art.files, modelFile{path: "nested/config.json", sha256: sum(files["tokenizer.json"]), size: 1})
	hub := newFakeHub(t, files)
	pinTestModel(t, hub, id, art)

	got := PinnedFiles(id)
	if len(got) != len(art.files) {
		t.Fatalf("PinnedFiles returned %d files, want %d", len(got), len(art.files))
	}
	for i, f := range art.files {
		want := PinnedFile{Name: path.Base(f.path), SHA256: f.sha256, Size: f.size}
		if got[i] != want {
			t.Errorf("file %d = %+v, want %+v", i, got[i], want)
		}
	}
}

// TestLiveEnsureModel downloads the real default model from Hugging Face and
// verifies it against the pinned checksums. It is gated like the other live
// tests because it pulls ~260MB:
//
//	ALCATRAZ_NER_LIVE=1 go test -run LiveEnsureModel .
func TestLiveEnsureModel(t *testing.T) {
	if os.Getenv("ALCATRAZ_NER_LIVE") != "1" {
		t.Skip("set ALCATRAZ_NER_LIVE=1 to run the live download test")
	}
	model := DefaultModel
	dir := os.Getenv("ALCATRAZ_NER_MODELS_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	path, err := EnsureModelIn(context.Background(), model, dir)
	if err != nil {
		t.Fatalf("EnsureModelIn(%s): %v", model, err)
	}
	for _, f := range modelArtifacts[model].files {
		if !cachedFileValid(filepath.Join(path, f.path), f.sha256) {
			t.Errorf("%s did not verify after download", f.path)
		}
	}
	fmt.Fprintf(os.Stderr, "verified %s at %s\n", model, path)
}
