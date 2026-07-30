package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hoophq/alcatraz/models"
)

// ensureCall records what the swapped-in downloader was handed.
type ensureCall struct {
	ctx         context.Context
	model, dest string
}

// stubEnsure swaps the downloader for the duration of a test, so the command
// is exercised without touching the network. It records the arguments it was
// called with, since flag handling is most of what these tests check.
func stubEnsure(t *testing.T, ret string, err error) *ensureCall {
	t.Helper()
	got := &ensureCall{}
	prev := ensureModelIn
	ensureModelIn = func(ctx context.Context, model, dest string) (string, error) {
		got.ctx, got.model, got.dest = ctx, model, dest
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
		// want is a substring the hint must add. Empty means the failure
		// speaks for itself and must not be dressed up as a network problem.
		want string
	}{
		{"checksum", errors.New("models: downloading model.onnx: sha256 mismatch for x: got a, want b"), "re-run"},
		{"size", errors.New("models: downloading model.onnx: size mismatch for x: got 1, want 2"), "re-run"},
		{
			"transport",
			fmt.Errorf("models: downloading model.onnx: %w", &url.Error{
				Op: "Get", URL: "https://huggingface.co/x", Err: errors.New("dial tcp: i/o timeout"),
			}),
			"HTTPS_PROXY",
		},
		{"status", errors.New("models: downloading model.onnx: GET https://x: unexpected status 404 Not Found"), "withdrawn"},
		{"interrupted", fmt.Errorf("models: downloading model.onnx: %w", context.Canceled), "interrupted"},
		// The deployment this command targets is a non-root container writing
		// a mounted volume, which makes this the likeliest failure of all.
		// Answering it with HTTPS_PROXY sends the operator to debug egress
		// that was never involved.
		{"local", errors.New("models: creating model directory: mkdir /x: permission denied"), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stubEnsure(t, "", c.err)
			_, err := runModels([]string{"download"})
			if err == nil {
				t.Fatal("runModels succeeded, want the download error")
			}
			if c.want == "" {
				if strings.Contains(err.Error(), "HTTPS_PROXY") {
					t.Errorf("error = %v, want no proxy hint for a local failure", err)
				}
				return
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %v, want a hint mentioning %q", err, c.want)
			}
		})
	}
}

func TestModelsDownloadContextIsCancellable(t *testing.T) {
	got := stubEnsure(t, t.TempDir(), nil)
	if _, err := runModels([]string{"download"}); err != nil {
		t.Fatalf("runModels: %v", err)
	}
	// context.Background().Done() is nil. A non-nil channel is what proves a
	// signal can unwind the fetch, which is what lets download remove its
	// temp file instead of stranding up to 250MB of it on a shared volume.
	if got.ctx == nil || got.ctx.Done() == nil {
		t.Error("runModels passed a context that cannot be cancelled; a signal would strand the partial download")
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
