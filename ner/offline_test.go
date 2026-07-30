package ner

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hoophq/alcatraz/entities"
	"github.com/hoophq/alcatraz/models"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// noNetwork fails the test if anything reaches for the network while it runs.
// Offline is a promise about what does not happen, and the only way to test a
// negative is to make the forbidden thing loud: asserting on an error message
// would pass just as well for code that tried the hub first and gave up.
//
// It replaces the process-wide http.DefaultTransport, which is what
// http.DefaultClient resolves lazily at call time, so it covers models.download
// and anything else on the load path that uses the default client. A test
// calling this cannot be parallel.
func noNetwork(t *testing.T) {
	t.Helper()
	prev := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Errorf("offline mode attempted a request to %s", r.URL)
		return nil, http.ErrUseLastResponse
	})
	t.Cleanup(func() { http.DefaultTransport = prev })
}

// seedFiles writes name -> content into dir, creating it.
func seedFiles(t *testing.T, dir string, files map[string]string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// unpinnedModel is a model id nothing has checksums for, which is the other
// half of the offline path: presence checks only, and different advice.
const unpinnedModel = "alcatraz-test/unpinned-NER"

func TestOfflineColdCacheDoesNotDownload(t *testing.T) {
	noNetwork(t)
	dir := t.TempDir()

	_, err := New(context.Background(), Config{Model: models.DefaultModel, ModelsDir: dir, Offline: true})
	if err == nil {
		t.Fatal("New succeeded with Offline set and an empty models directory")
	}
	msg := err.Error()
	for _, want := range []string{
		"offline:",
		"no model directory at",
		models.Dir(dir, models.DefaultModel),
		"alcatraz models download --dest " + dir,
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("err = %v\nwant it to contain %q", err, want)
		}
	}
}

// The pinned check reports the first file the pin table lists as missing, so
// an operator fixes one named file rather than guessing at "the model".
func TestOfflinePinnedIncompleteDirNamesTheFile(t *testing.T) {
	noNetwork(t)
	dir := t.TempDir()
	pinned := models.PinnedFiles(models.DefaultModel)
	if len(pinned) < 2 {
		t.Fatalf("expected the default model to pin several files, got %d", len(pinned))
	}
	// Everything but the first file, so the report cannot be a lucky guess
	// at an empty directory.
	files := map[string]string{}
	for _, f := range pinned[1:] {
		files[f.Name] = "placeholder"
	}
	seedFiles(t, models.Dir(dir, models.DefaultModel), files)

	_, err := ensureModel(context.Background(), Config{Model: models.DefaultModel, ModelsDir: dir, Offline: true})
	if err == nil {
		t.Fatal("ensureModel succeeded on an incomplete pinned model directory")
	}
	if !strings.Contains(err.Error(), pinned[0].Name) || !strings.Contains(err.Error(), "missing") {
		t.Errorf("err = %v, want it to name %s as missing", err, pinned[0].Name)
	}
}

// Present but wrong is the case Offline exists for: a stale image layer or a
// truncated copy cannot be repaired by a re-fetch here, so it has to be said
// out loud instead of surfacing as an opaque runtime failure much later.
func TestOfflinePinnedTamperedDirReportsMismatch(t *testing.T) {
	noNetwork(t)
	dir := t.TempDir()
	files := map[string]string{}
	for _, f := range models.PinnedFiles(models.DefaultModel) {
		files[f.Name] = "not the pinned bytes"
	}
	seedFiles(t, models.Dir(dir, models.DefaultModel), files)

	_, err := ensureModel(context.Background(), Config{Model: models.DefaultModel, ModelsDir: dir, Offline: true})
	if err == nil {
		t.Fatal("ensureModel succeeded on a tampered pinned model directory")
	}
	if !strings.Contains(err.Error(), "does not match its pinned sha256") {
		t.Errorf("err = %v, want a sha256 mismatch", err)
	}
	if strings.Contains(err.Error(), "missing") {
		t.Errorf("err = %v, reports a present-but-wrong file as missing", err)
	}
}

func TestOfflineUnpinnedModel(t *testing.T) {
	noNetwork(t)

	complete := map[string]string{
		"config.json":    `{"id2label":{}}`,
		"tokenizer.json": `{}`,
		"model.onnx":     "weights",
	}

	t.Run("complete directory resolves", func(t *testing.T) {
		dir := t.TempDir()
		want := seedFiles(t, models.Dir(dir, unpinnedModel), complete)

		got, err := ensureModel(context.Background(), Config{Model: unpinnedModel, ModelsDir: dir, Offline: true})
		if err != nil {
			t.Fatalf("ensureModel: %v", err)
		}
		if got != want {
			t.Errorf("model path = %q, want %q", got, want)
		}
	})

	t.Run("missing file is named, with advice that fits an unpinned model", func(t *testing.T) {
		dir := t.TempDir()
		seedFiles(t, models.Dir(dir, unpinnedModel), map[string]string{"config.json": "{}"})

		_, err := ensureModel(context.Background(), Config{Model: unpinnedModel, ModelsDir: dir, Offline: true})
		if err == nil {
			t.Fatal("ensureModel succeeded without a tokenizer")
		}
		if !strings.Contains(err.Error(), "tokenizer.json") {
			t.Errorf("err = %v, want it to name tokenizer.json", err)
		}
		// "alcatraz models download" refuses unpinned ids, so suggesting it
		// here would send the operator down a dead end.
		if strings.Contains(err.Error(), "alcatraz models download") {
			t.Errorf("err = %v, must not suggest a command that rejects unpinned ids", err)
		}
		if !strings.Contains(err.Error(), "not a pinned model") {
			t.Errorf("err = %v, want it to explain why there is nothing to verify against", err)
		}
	})

	t.Run("no onnx file at all", func(t *testing.T) {
		dir := t.TempDir()
		seedFiles(t, models.Dir(dir, unpinnedModel), map[string]string{
			"config.json":    "{}",
			"tokenizer.json": "{}",
		})

		_, err := ensureModel(context.Background(), Config{Model: unpinnedModel, ModelsDir: dir, Offline: true})
		if err == nil || !strings.Contains(err.Error(), "no .onnx file in") {
			t.Errorf("err = %v, want it to report the absent weights", err)
		}
	})

	// An empty OnnxFilename means "the single .onnx file in the repository",
	// whatever the export called it, so the check must not assume model.onnx.
	t.Run("differently named onnx is accepted", func(t *testing.T) {
		dir := t.TempDir()
		want := seedFiles(t, models.Dir(dir, unpinnedModel), map[string]string{
			"config.json":          "{}",
			"tokenizer.json":       "{}",
			"model_quantized.onnx": "weights",
		})

		got, err := ensureModel(context.Background(), Config{Model: unpinnedModel, ModelsDir: dir, Offline: true})
		if err != nil {
			t.Fatalf("ensureModel: %v", err)
		}
		if got != want {
			t.Errorf("model path = %q, want %q", got, want)
		}
	})

	t.Run("OnnxFilename must exist when set", func(t *testing.T) {
		dir := t.TempDir()
		seedFiles(t, models.Dir(dir, unpinnedModel), complete)

		_, err := ensureModel(context.Background(), Config{
			Model:        unpinnedModel,
			ModelsDir:    dir,
			OnnxFilename: "model_quantized.onnx",
			Offline:      true,
		})
		if err == nil || !strings.Contains(err.Error(), "model_quantized.onnx") {
			t.Errorf("err = %v, want it to name the requested onnx file", err)
		}
	})
}

// ModelPath already downloads nothing, so Offline adds only the pre-flight
// check — and adds it strictly opt-in, since existing callers pass directories
// this package has never inspected.
func TestOfflineModelPathIsChecked(t *testing.T) {
	noNetwork(t)
	empty := t.TempDir()

	_, err := New(context.Background(), Config{ModelPath: empty, Offline: true})
	if err == nil {
		t.Fatal("New succeeded with Offline set and an empty ModelPath")
	}
	if !strings.Contains(err.Error(), "ner: offline:") {
		t.Errorf("err = %v, want the offline pre-flight error", err)
	}
	if !strings.Contains(err.Error(), "config.json is missing from "+empty) {
		t.Errorf("err = %v, want it to name the missing file and the directory", err)
	}
}

func TestCheckModelDir(t *testing.T) {
	t.Run("path is a file", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "model")
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := checkModelDir(f, ""); err == nil || !strings.Contains(err.Error(), "is not a directory") {
			t.Errorf("err = %v, want a not-a-directory error", err)
		}
	})

	t.Run("path does not exist", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "nope")
		if err := checkModelDir(missing, ""); err == nil || !strings.Contains(err.Error(), "no model directory at") {
			t.Errorf("err = %v, want an absent-directory error", err)
		}
	})

	// hugot's own layouts put the weights in an onnx/ subdirectory often
	// enough that a top-level-only check would reject a directory that loads.
	t.Run("onnx in a subdirectory counts", func(t *testing.T) {
		dir := seedFiles(t, t.TempDir(), map[string]string{
			"config.json":    "{}",
			"tokenizer.json": "{}",
		})
		seedFiles(t, filepath.Join(dir, "onnx"), map[string]string{"model.onnx": "weights"})

		if err := checkModelDir(dir, ""); err != nil {
			t.Errorf("checkModelDir: %v", err)
		}
	})

	t.Run("extension match is case-insensitive", func(t *testing.T) {
		dir := seedFiles(t, t.TempDir(), map[string]string{
			"config.json":    "{}",
			"tokenizer.json": "{}",
			"MODEL.ONNX":     "weights",
		})
		if err := checkModelDir(dir, ""); err != nil {
			t.Errorf("checkModelDir: %v", err)
		}
	})
}

func TestDownloadCmd(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{"defaults", Config{Model: models.DefaultModel}, "alcatraz models download"},
		{"explicit dir", Config{Model: models.DefaultModel, ModelsDir: "/opt/m"}, "alcatraz models download --dest /opt/m"},
		{"other model", Config{Model: "org/other"}, "alcatraz models download --model org/other"},
		{"both", Config{Model: "org/other", ModelsDir: "/opt/m"}, "alcatraz models download --dest /opt/m --model org/other"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := downloadCmd(tt.cfg); got != tt.want {
				t.Errorf("downloadCmd = %q, want %q", got, tt.want)
			}
		})
	}
}

// The offline path never writes, so a model directory mounted read-only —
// the baked-image and shared-volume case — resolves unchanged.
func TestOfflineReadOnlyModelsDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root, which ignores the permission bits under test")
	}
	noNetwork(t)

	dir := t.TempDir()
	want := seedFiles(t, models.Dir(dir, unpinnedModel), map[string]string{
		"config.json":    "{}",
		"tokenizer.json": "{}",
		"model.onnx":     "weights",
	})
	for _, p := range []string{want, dir} {
		if err := os.Chmod(p, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(p, 0o755) })
	}

	got, err := ensureModel(context.Background(), Config{Model: unpinnedModel, ModelsDir: dir, Offline: true})
	if err != nil {
		t.Fatalf("ensureModel on a read-only models directory: %v", err)
	}
	if got != want {
		t.Errorf("model path = %q, want %q", got, want)
	}
}

// TestLiveOfflineNER is the whole feature against a real model: warm the
// cache, mount a copy of it read-only, forbid the network, and load. Gated
// behind ALCATRAZ_NER_LIVE=1.
//
// The unit tests above stop at path resolution, because the fixtures they use
// are not loadable models. This is the only place that proves hugot itself
// gets through a read-only directory without writing — no lockfile, no temp
// extraction, no metadata rewrite — which is exactly how a baked image or a
// shared volume presents the model in the deployments Offline exists for.
func TestLiveOfflineNER(t *testing.T) {
	if os.Getenv("ALCATRAZ_NER_LIVE") != "1" {
		t.Skip("set ALCATRAZ_NER_LIVE=1 to run the live model test")
	}

	// Warm the shared cache first — the one step here allowed to use the
	// network, and on CI a no-op against the restored cache.
	src, err := models.EnsureModelIn(context.Background(), models.DefaultModel, os.Getenv("ALCATRAZ_NER_MODELS_DIR"))
	if err != nil {
		t.Fatalf("EnsureModelIn: %v", err)
	}

	// Copy rather than chmod the cache in place: a test has no business
	// leaving the user's model cache read-only if it dies partway through.
	dir := t.TempDir()
	dest := models.Dir(dir, models.DefaultModel)
	copyModelDir(t, src, dest)
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		for _, p := range []string{dest, dir} {
			if err := os.Chmod(p, 0o555); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.Chmod(p, 0o755) })
		}
	}

	noNetwork(t)

	ctx, cancel := context.WithCancel(context.Background())
	nlp, err := New(ctx, Config{Model: models.DefaultModel, ModelsDir: dir, Offline: true})
	cancel()
	if err != nil {
		t.Fatalf("New with Offline against a read-only model directory: %v", err)
	}
	defer nlp.Close()

	arts, err := nlp.ProcessText("My name is John Smith and I live in Berlin", "en")
	if err != nil {
		t.Fatalf("ProcessText: %v", err)
	}
	found := map[string]bool{}
	for _, span := range arts.Ents {
		found[span.EntityType] = true
	}
	if !found[entities.Person] {
		t.Errorf("no PERSON in %v", arts.Ents)
	}
}

// copyModelDir copies the flat model directory src to dest, read-only.
func copyModelDir(t *testing.T, src, dest string) {
	t.Helper()
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			copyModelDir(t, filepath.Join(src, e.Name()), filepath.Join(dest, e.Name()))
			continue
		}
		in, err := os.Open(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out, err := os.OpenFile(filepath.Join(dest, e.Name()), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o444)
		if err != nil {
			in.Close()
			t.Fatal(err)
		}
		_, err = io.Copy(out, in)
		in.Close()
		if closeErr := out.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			t.Fatal(err)
		}
	}
}
