package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hoophq/alcatraz/models"
)

// ensureCall records what the swapped-in downloader was handed.
type ensureCall struct {
	ctx                 context.Context
	model, dest, origin string
}

// stubEnsure swaps the downloader for the duration of a test, so the command
// is exercised without touching the network. It records the arguments it was
// called with, since flag handling is most of what these tests check.
func stubEnsure(t *testing.T, ret string, err error) *ensureCall {
	t.Helper()
	got := &ensureCall{}
	prev := ensureModelFrom
	ensureModelFrom = func(ctx context.Context, model, dest, origin string) (string, error) {
		got.ctx, got.model, got.dest, got.origin = ctx, model, dest, origin
		return ret, err
	}
	t.Cleanup(func() { ensureModelFrom = prev })
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

func TestModelsDownloadOriginIsHonoured(t *testing.T) {
	got := stubEnsure(t, "/opt/alcatraz/models/KnightsAnalytics_distilbert-NER", nil)

	const mirror = "https://models.internal/alcatraz"
	if _, err := runModels([]string{"download", "-origin", mirror}); err != nil {
		t.Fatalf("runModels: %v", err)
	}
	if got.origin != mirror {
		t.Errorf("origin = %q, want %q", got.origin, mirror)
	}
}

func TestModelsDownloadResolvesTheOriginItReports(t *testing.T) {
	got := stubEnsure(t, "/opt/alcatraz/models/KnightsAnalytics_distilbert-NER", nil)

	var out bytes.Buffer
	if _, err := download(context.Background(), &out, models.DefaultModel, "", ""); err != nil {
		t.Fatalf("download: %v", err)
	}

	// An omitted -origin is resolved here, not left to the models package, so
	// that the origin on the receipt is the one the bytes came from. Printing
	// "origin: (default)" while the pin table quietly redirected the fetch is
	// the confusion this command exists to prevent.
	want := models.Origin(models.DefaultModel)
	if got.origin != want {
		t.Errorf("origin passed to the downloader = %q, want the resolved %q", got.origin, want)
	}
	if !strings.Contains(out.String(), "origin: "+want) {
		t.Errorf("output does not report the origin %q:\n%s", want, out.String())
	}
}

func TestModelsDownloadPrintsBothPaths(t *testing.T) {
	modelPath := filepath.Join("/opt/alcatraz/models", "KnightsAnalytics_distilbert-NER")
	stubEnsure(t, modelPath, nil)

	var out bytes.Buffer
	if _, err := download(context.Background(), &out, models.DefaultModel, "/opt/alcatraz/models", ""); err != nil {
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

func TestHintNamesTheHostThatFailed(t *testing.T) {
	// A hint that hardcodes the hub tells an operator running against a mirror
	// to allow-list a host their build never contacts. The host is read off the
	// failure, so it survives a redirect too.
	cases := []struct {
		name, url, want string
	}{
		{"hub", "https://huggingface.co/x/resolve/abc/model.onnx", "huggingface.co"},
		{"mirror", "https://models.internal/alcatraz/x/resolve/abc/model.onnx", "models.internal"},
		// Nothing to name: better a vague hint than a confident wrong host.
		{"unparseable", "://nonsense", "the model origin"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := fmt.Errorf("models: downloading model.onnx: %w", &url.Error{
				Op: "Get", URL: c.url, Err: errors.New("dial tcp: i/o timeout"),
			})
			got := hintFor(err)
			if !strings.Contains(got, c.want) {
				t.Errorf("hint = %q, want it to name %q", got, c.want)
			}
			if c.want != "huggingface.co" && strings.Contains(got, "huggingface.co") {
				t.Errorf("hint = %q, want no mention of the hub", got)
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

// TestModelsReportsCarryTheLicense pins the license into both commands'
// output, because the output is the artefact: a build log for download, and
// for verify the evidence a release keeps about what an image contains.
func TestModelsReportsCarryTheLicense(t *testing.T) {
	license, source := models.License(models.DefaultModel)

	for _, tc := range []struct {
		name string
		run  func(io.Writer) (int, error)
	}{
		{"download", func(w io.Writer) (int, error) {
			stubEnsure(t, "/models/KnightsAnalytics_distilbert-NER", nil)
			return download(context.Background(), w, models.DefaultModel, "/models", "")
		}},
		{"verify", func(w io.Writer) (int, error) {
			stubVerify(t, "/models/KnightsAnalytics_distilbert-NER", nil)
			return verify(w, models.DefaultModel, "/models")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if code, err := tc.run(&out); err != nil || code != 0 {
				t.Fatalf("%s = (%d, %v), want (0, nil)", tc.name, code, err)
			}
			got := out.String()
			if !strings.Contains(got, license) {
				t.Errorf("%s does not report the license %q:\n%s", tc.name, license, got)
			}
			// The repository the files come from declares no license, so the
			// id alone points an auditor at the wrong place.
			if !strings.Contains(got, source) {
				t.Errorf("%s does not report where %q is declared:\n%s", tc.name, license, got)
			}
		})
	}
}

// decodePins runs "models pins" and decodes it, failing on either.
func decodePins(t *testing.T, model string) pinManifest {
	t.Helper()
	var out bytes.Buffer
	if code, err := pins(&out, model); err != nil || code != 0 {
		t.Fatalf("pins(%q) = (%d, %v), want (0, nil)", model, code, err)
	}
	var got pinManifest
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("pins(%q) emitted invalid JSON: %v\n%s", model, err, out.String())
	}
	return got
}

func TestModelsPinsMatchesTheTable(t *testing.T) {
	got := decodePins(t, models.DefaultModel)

	id, source := models.License(models.DefaultModel)
	if got.Model != models.DefaultModel || got.Revision != models.Revision(models.DefaultModel) ||
		got.Origin != models.Origin(models.DefaultModel) || got.License.ID != id || got.License.Source != source {
		t.Errorf("pins header = %+v, does not match the table", got)
	}

	want := models.PinnedFiles(models.DefaultModel)
	if len(got.Files) != len(want) {
		t.Fatalf("pins listed %d files, want %d", len(got.Files), len(want))
	}
	for i, f := range want {
		// A publisher uploads by key and checks by digest, so both have to be
		// right for the mirror to serve anything the downloader accepts.
		g := got.Files[i]
		if g.Name != f.Name || g.Path != f.Path || g.SHA256 != f.SHA256 || g.Size != f.Size {
			t.Errorf("pins file %d = %+v, want %+v", i, g, f)
		}
		if wantKey := models.DefaultModel + "/resolve" + "/" + got.Revision + "/" + f.Path; g.Key != wantKey {
			t.Errorf("pins file %d key = %q, want %q", i, g.Key, wantKey)
		}
	}
}

func TestModelsPinsRejectsAnUnpinnedModel(t *testing.T) {
	var out bytes.Buffer
	_, err := pins(&out, "some-org/unpinned-model")
	if err == nil {
		t.Fatal("pins succeeded for an unpinned model, want an error naming the pinned ones")
	}
	if !strings.Contains(err.Error(), models.DefaultModel) {
		t.Errorf("error %q does not list the pinned models", err)
	}
	if out.Len() > 0 {
		t.Errorf("pins wrote output for an unpinned model: %s", out.String())
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
	for _, want := range []string{"alcatraz models download", "alcatraz models verify"} {
		if !strings.Contains(got, want) {
			t.Errorf("usage does not show %q:\n%s", want, got)
		}
	}
	// The list of verifiable ids is the point of the usage text, and each one
	// is listed with where it is fetched from: with two possible origins, an id
	// on its own no longer says what a bare download would contact.
	for _, id := range models.PinnedModels() {
		if !strings.Contains(got, id) {
			t.Errorf("usage does not list the pinned model %q:\n%s", id, got)
		}
		if !strings.Contains(got, models.Origin(id)) {
			t.Errorf("usage does not show the origin of %q:\n%s", id, got)
		}
		// Answered by the list rather than by a run: whether a model may go
		// into an image we publish is decided before anything is fetched.
		license, _ := models.License(id)
		if !strings.Contains(got, license) {
			t.Errorf("usage does not show the license of %q:\n%s", id, got)
		}
	}
	// The layout is the contract a mirror has to satisfy, so it belongs in the
	// usage rather than only in the package docs.
	for _, want := range []string{"-origin", "{origin}/{model}/resolve/{revision}/{file}"} {
		if !strings.Contains(got, want) {
			t.Errorf("usage does not mention %q:\n%s", want, got)
		}
	}
}

// verifyCall records what the swapped-in verifier was handed.
type verifyCall struct {
	model, dir string
}

// stubVerify swaps the verifier for the duration of a test, for the cases that
// are about flag handling and output rather than about verification itself.
func stubVerify(t *testing.T, ret string, err error) *verifyCall {
	t.Helper()
	got := &verifyCall{}
	prev := verifyModelIn
	verifyModelIn = func(model, dir string) (string, error) {
		got.model, got.dir = model, dir
		return ret, err
	}
	t.Cleanup(func() { verifyModelIn = prev })
	return got
}

func TestModelsVerifyDefaults(t *testing.T) {
	got := stubVerify(t, "/cache/alcatraz/models/KnightsAnalytics_distilbert-NER", nil)

	code, err := runModels([]string{"verify"})
	if err != nil || code != 0 {
		t.Fatalf("runModels = (%d, %v), want (0, nil)", code, err)
	}
	if got.model != models.DefaultModel {
		t.Errorf("model = %q, want the default %q", got.model, models.DefaultModel)
	}
	// Empty is passed through: that is what checks the same cache ner.New
	// reads from, and unlike download it must not create it.
	if got.dir != "" {
		t.Errorf("dir = %q, want empty so the default cache is checked", got.dir)
	}
}

func TestModelsVerifyDirIsHonoured(t *testing.T) {
	got := stubVerify(t, "/opt/alcatraz/models/KnightsAnalytics_distilbert-NER", nil)

	if _, err := runModels([]string{"verify", "--dir", "/opt/alcatraz/models"}); err != nil {
		t.Fatalf("runModels: %v", err)
	}
	if got.dir != "/opt/alcatraz/models" {
		t.Errorf("dir = %q, want /opt/alcatraz/models", got.dir)
	}
}

// TestModelsVerifyAcceptsDestAsAnAlias covers the runbook case the alias is
// for: the verify line sits under the download line, and copying the path
// across must not fail over the name of the flag holding it.
func TestModelsVerifyAcceptsDestAsAnAlias(t *testing.T) {
	for _, flag := range []string{"--dir", "--dest", "-dest"} {
		t.Run(flag, func(t *testing.T) {
			got := stubVerify(t, "/opt/alcatraz/models/KnightsAnalytics_distilbert-NER", nil)

			if _, err := runModels([]string{"verify", flag, "/opt/alcatraz/models"}); err != nil {
				t.Fatalf("runModels: %v", err)
			}
			if got.dir != "/opt/alcatraz/models" {
				t.Errorf("%s: dir = %q, want /opt/alcatraz/models", flag, got.dir)
			}
		})
	}
}

// TestModelsVerifyRejectsTwoDirectories keeps the alias from silently picking
// one: two spellings of the same flag disagreeing is a mistake in the caller's
// script, and verifying whichever won would answer a question nobody asked.
func TestModelsVerifyRejectsTwoDirectories(t *testing.T) {
	got := stubVerify(t, "/unused", nil)

	_, err := runModels([]string{"verify", "--dir", "/opt/a", "--dest", "/opt/b"})
	if err == nil {
		t.Fatal("runModels accepted -dir and -dest naming different directories")
	}
	for _, want := range []string{"/opt/a", "/opt/b"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to name %s", err, want)
		}
	}
	if got.dir != "" {
		t.Errorf("verified %q anyway", got.dir)
	}
}

// failAfter accepts n bytes and then refuses, standing in for a stdout that
// fails partway through: a full disk, a descriptor closed under the process.
// (A broken pipe on fd 1 is not this case — Go raises SIGPIPE and the process
// dies where it stands, which is already the right answer.)
type failAfter struct {
	left int
	err  error
}

func (f *failAfter) Write(p []byte) (int, error) {
	if f.left <= 0 {
		return 0, f.err
	}
	if len(p) > f.left {
		n := f.left
		f.left = 0
		return n, f.err
	}
	f.left -= len(p)
	return len(p), nil
}

// TestModelsCommandsReportAFailedStdout holds these two commands to the
// contract writeFindings states: a stdout that failed surfaces as an error,
// never as a silent exit 0. It matters more here than for a scan — both
// commands are read by something deciding whether a model is usable, and
// "verified" with no output would be taken as a pass.
func TestModelsCommandsReportAFailedStdout(t *testing.T) {
	errFull := errors.New("no space left on device")
	const dir = "/opt/alcatraz/models"
	modelPath := filepath.Join(dir, "KnightsAnalytics_distilbert-NER")

	// The length of a whole successful run, so a writer can be made to fail on
	// its very last write — past every early check, where only the final one
	// can catch it.
	full := func(run func(io.Writer)) int {
		var buf bytes.Buffer
		run(&buf)
		return buf.Len()
	}

	t.Run("verify before hashing", func(t *testing.T) {
		got := stubVerify(t, modelPath, nil)

		_, err := verify(&failAfter{err: errFull}, models.DefaultModel, dir)
		if !errors.Is(err, errFull) {
			t.Fatalf("verify: %v, want %v", err, errFull)
		}
		// The manifest is printed before the hashing starts, so a writer that
		// is already gone should stop the run there rather than re-read 250MB
		// to report into it.
		if got.dir != "" {
			t.Error("hashed the model into a writer that had already failed")
		}
	})

	t.Run("verify on the last line", func(t *testing.T) {
		stubVerify(t, modelPath, nil)
		n := full(func(w io.Writer) { verify(w, models.DefaultModel, dir) })

		if _, err := verify(&failAfter{left: n - 1, err: errFull}, models.DefaultModel, dir); !errors.Is(err, errFull) {
			t.Fatalf("verify: %v, want %v", err, errFull)
		}
	})

	t.Run("download before fetching", func(t *testing.T) {
		got := stubEnsure(t, modelPath, nil)

		_, err := download(context.Background(), &failAfter{err: errFull}, models.DefaultModel, dir, "")
		if !errors.Is(err, errFull) {
			t.Fatalf("download: %v, want %v", err, errFull)
		}
		if got.model != "" {
			t.Error("fetched 250MB into a writer that had already failed")
		}
	})

	t.Run("download on the last line", func(t *testing.T) {
		stubEnsure(t, modelPath, nil)
		n := full(func(w io.Writer) { download(context.Background(), w, models.DefaultModel, dir, "") })

		_, err := download(context.Background(), &failAfter{left: n - 1, err: errFull}, models.DefaultModel, dir, "")
		if !errors.Is(err, errFull) {
			t.Fatalf("download: %v, want %v", err, errFull)
		}
	})
}

func TestModelsVerifyPrintsWhatItChecked(t *testing.T) {
	modelPath := filepath.Join("/opt/alcatraz/models", "KnightsAnalytics_distilbert-NER")
	stubVerify(t, modelPath, nil)

	var out bytes.Buffer
	if _, err := verify(&out, models.DefaultModel, "/opt/alcatraz/models"); err != nil {
		t.Fatalf("verify: %v", err)
	}
	got := out.String()

	for _, want := range []string{
		// Same two paths download prints, for the same reason: one of them is
		// what the caller's config wants and they are one level apart.
		"ModelsDir: " + filepath.Dir(modelPath),
		"ModelPath: " + modelPath,
		// A CI job asserting an image's contents is reading this line to know
		// the assertion did not quietly reach for the network.
		"no network, no writes",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
	for _, f := range models.PinnedFiles(models.DefaultModel) {
		if !strings.Contains(got, f.Name) || !strings.Contains(got, f.SHA256) {
			t.Errorf("output does not report %s and its digest:\n%s", f.Name, got)
		}
	}
}

// seedState lays out a models directory in one of the states VerifyModelIn
// tells apart and returns the real failure it produces. Real, not hand-copied:
// the hints below are matched by substring, so the thing worth testing is that
// they still fire on the messages the models package actually emits.
//
// The pinned files are hundreds of megabytes; none of these states needs their
// contents, only their names.
func seedState(t *testing.T, state string) (dir string, err error) {
	t.Helper()
	dir = t.TempDir()
	modelPath := models.Dir(dir, models.DefaultModel)
	first := models.PinnedFiles(models.DefaultModel)[0].Name

	switch state {
	case "no-dir": // nothing was ever seeded here
	case "missing", "mismatch", "unreadable":
		if err := os.MkdirAll(modelPath, 0o755); err != nil {
			t.Fatal(err)
		}
		if state != "missing" {
			bad := filepath.Join(modelPath, first)
			if err := os.WriteFile(bad, []byte("not the pinned bytes"), 0o644); err != nil {
				t.Fatal(err)
			}
			if state == "unreadable" {
				if err := os.Chmod(bad, 0o000); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.Chmod(bad, 0o644) })
			}
		}
	default:
		t.Fatalf("unknown state %q", state)
	}

	_, err = models.VerifyModelIn(models.DefaultModel, dir)
	if err == nil {
		t.Fatalf("VerifyModelIn succeeded on state %q, want a failure to hint about", state)
	}
	return dir, err
}

func TestModelsVerifyFailureIsActionable(t *testing.T) {
	cases := []struct {
		state, want string
		// unwanted is advice that would send the operator to the wrong place.
		unwanted string
	}{
		{state: "no-dir", want: "alcatraz models download"},
		{state: "missing", want: "alcatraz models download"},
		{state: "mismatch", want: "stale image layer"},
		// The likeliest failure in the deployment this command is for — a
		// model owned by the build stage that put it there, read by a non-root
		// process — and the one where re-downloading fixes nothing.
		{state: "unreadable", want: "ownership and mode", unwanted: "alcatraz models download"},
	}
	for _, c := range cases {
		t.Run(c.state, func(t *testing.T) {
			if c.state == "unreadable" {
				if runtime.GOOS == "windows" {
					t.Skip("unix permission bits")
				}
				if os.Geteuid() == 0 {
					t.Skip("running as root, which ignores the permission bits under test")
				}
			}
			dir, err := seedState(t, c.state)

			hint := verifyHintFor(err, models.DefaultModel, dir)
			if !strings.Contains(hint, c.want) {
				t.Errorf("hint = %q, want it to mention %q (error: %v)", hint, c.want, err)
			}
			if c.unwanted != "" && strings.Contains(hint, c.unwanted) {
				t.Errorf("hint = %q, want no mention of %q", hint, c.unwanted)
			}
			// The suggestion has to be pastable: a checked directory that is
			// not the default has to come back with it.
			if strings.Contains(hint, "alcatraz models download") && !strings.Contains(hint, dir) {
				t.Errorf("hint = %q, want it to name the directory %q", hint, dir)
			}
		})
	}
}

// TestModelsVerifyHintsTheWrongLevel covers the mistake the ModelsDir/ModelPath
// split exists to prevent. Untreated it reads as a missing download, and acting
// on that advice nests a second copy of the model inside the first.
func TestModelsVerifyHintsTheWrongLevel(t *testing.T) {
	dir := t.TempDir()
	modelPath := models.Dir(dir, models.DefaultModel)

	_, err := models.VerifyModelIn(models.DefaultModel, modelPath)
	if err == nil {
		t.Fatal("VerifyModelIn succeeded one level too deep")
	}
	hint := verifyHintFor(err, models.DefaultModel, modelPath)
	if !strings.Contains(hint, "models directory") || !strings.Contains(hint, dir) {
		t.Errorf("hint = %q, want it to name the parent %q", hint, dir)
	}
	if strings.Contains(hint, "alcatraz models download") {
		t.Errorf("hint = %q, want it not to advise a download that would nest a second copy", hint)
	}
}

// TestModelsVerifyWritesNothing runs the real verifier, not the stub: the
// promise is that this command is safe against a read-only mount and against
// the cache directory not existing yet, and only the real one can keep it.
func TestModelsVerifyWritesNothing(t *testing.T) {
	dir := t.TempDir()
	if _, err := verify(io.Discard, models.DefaultModel, dir); err == nil {
		t.Fatal("verify succeeded against an empty models directory")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("verify created %d entries in the models directory", len(entries))
	}
}

func TestModelsVerifyUnpinnedModel(t *testing.T) {
	// No stub: an unpinned id has to be rejected before anything is read.
	_, err := runModels([]string{"verify", "-model", "some-org/unpinned-model"})
	if err == nil {
		t.Fatal("runModels succeeded for an unpinned model, want an error")
	}
	for _, want := range []string{"some-org/unpinned-model", models.DefaultModel} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to mention %q", err, want)
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
