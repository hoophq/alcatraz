package main

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hoophq/alcatraz/models"
)

// stubEnsure swaps the downloader for the duration of a test, so the command
// is exercised without touching the network. It records the arguments it was
// called with, since flag handling is most of what these tests check.
func stubEnsure(t *testing.T, ret string, err error) *struct{ model, dest string } {
	t.Helper()
	got := &struct{ model, dest string }{}
	prev := ensureModelIn
	ensureModelIn = func(_ context.Context, model, dest string) (string, error) {
		got.model, got.dest = model, dest
		return ret, err
	}
	t.Cleanup(func() { ensureModelIn = prev })
	return got
}

func TestModelsDownloadDefaults(t *testing.T) {
	// An empty -dest is passed through as empty: that is what makes a bare
	// invocation warm the same cache ner.New reads from.
	got := stubEnsure(t, "/cache/alcatraz/models/KnightsAnalytics_distilbert-NER", nil)

	code, err := runModels([]string{"download"})
	if err != nil || code != 0 {
		t.Fatalf("runModels = (%d, %v), want (0, nil)", code, err)
	}
	if got.model != models.DefaultModel {
		t.Errorf("model = %q, want the default %q", got.model, models.DefaultModel)
	}
	if got.dest != "" {
		t.Errorf("dest = %q, want empty so the default cache is used", got.dest)
	}
}

func TestModelsDownloadDestIsHonoured(t *testing.T) {
	got := stubEnsure(t, "/opt/alcatraz/models/KnightsAnalytics_distilbert-NER", nil)

	// Go's flag package accepts both spellings; the issue and the docs use
	// the double dash, so pin that it works.
	if _, err := runModels([]string{"download", "--dest", "/opt/alcatraz/models"}); err != nil {
		t.Fatalf("runModels: %v", err)
	}
	if got.dest != "/opt/alcatraz/models" {
		t.Errorf("dest = %q, want /opt/alcatraz/models", got.dest)
	}
}

func TestModelsDownloadPrintsBothPaths(t *testing.T) {
	modelPath := filepath.Join("/opt/alcatraz/models", "KnightsAnalytics_distilbert-NER")
	stubEnsure(t, modelPath, nil)

	var out bytes.Buffer
	if _, err := download(context.Background(), &out, models.DefaultModel, "/opt/alcatraz/models"); err != nil {
		t.Fatalf("download: %v", err)
	}
	got := out.String()

	// ModelsDir is the parent, ModelPath the model's own directory. Printing
	// only one of them is the misconfiguration this output exists to prevent.
	for _, want := range []string{
		"ModelsDir: " + filepath.Dir(modelPath),
		"ModelPath: " + modelPath,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
	// The manifest is printed before the fetch, the digests after it.
	for _, f := range models.PinnedFiles(models.DefaultModel) {
		if !strings.Contains(got, f.Name) {
			t.Errorf("output does not list %s:\n%s", f.Name, got)
		}
		if !strings.Contains(got, f.SHA256) {
			t.Errorf("output does not report the verified digest of %s", f.Name)
		}
	}
	if i, j := strings.Index(got, "fetching"), strings.Index(got, "verified:"); i < 0 || j < i {
		t.Errorf("manifest should precede the digests:\n%s", got)
	}
}

func TestModelsDownloadUnpinnedModel(t *testing.T) {
	// No stub: an unpinned id has to be rejected before anything is fetched.
	code, err := runModels([]string{"download", "-model", "some-org/unpinned-model"})
	if err == nil {
		t.Fatal("runModels succeeded for an unpinned model, want an error")
	}
	if code != 0 {
		t.Errorf("code = %d, want 0 so main exits 2 on the error", code)
	}
	// Actionable: which id was refused, and what can be used instead.
	for _, want := range []string{"some-org/unpinned-model", models.DefaultModel} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to mention %q", err, want)
		}
	}
}

func TestModelsDownloadFailureIsActionable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"checksum", errors.New("models: downloading model.onnx: sha256 mismatch for x: got a, want b"), "re-run"},
		{"size", errors.New("models: downloading model.onnx: size mismatch for x: got 1, want 2"), "re-run"},
		{"transport", errors.New("models: downloading model.onnx: dial tcp: i/o timeout"), "HTTPS_PROXY"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stubEnsure(t, "", c.err)
			_, err := runModels([]string{"download"})
			if err == nil {
				t.Fatal("runModels succeeded, want the download error")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %v, want a hint mentioning %q", err, c.want)
			}
		})
	}
}

func TestModelsUsage(t *testing.T) {
	for _, args := range [][]string{{}, {"downlaod"}} {
		_, err := runModels(args)
		if err == nil {
			t.Fatalf("runModels(%v) succeeded, want a usage error", args)
		}
	}

	var out bytes.Buffer
	modelsUsage(&out)
	got := out.String()
	if !strings.Contains(got, "alcatraz models download") {
		t.Errorf("usage does not show the command:\n%s", got)
	}
	// The list of verifiable ids is the point of the usage text.
	for _, id := range models.PinnedModels() {
		if !strings.Contains(got, id) {
			t.Errorf("usage does not list the pinned model %q:\n%s", id, got)
		}
	}
}

func TestHumanSize(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{925, "925 B"},
		{213450, "208.4 KiB"},
		{260926482, "248.8 MiB"},
	}
	for _, c := range cases {
		if got := humanSize(c.n); got != c.want {
			t.Errorf("humanSize(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestRunDispatchesModels(t *testing.T) {
	stubEnsure(t, t.TempDir(), nil)
	if _, err := run([]string{"models", "download"}); err != nil {
		t.Errorf("run: %v", err)
	}
}
