package pfilter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// useTempCache redirects the user cache dir into a per-test temp dir so the
// download helpers never touch the real ~/Library/Caches or ~/.cache.
func useTempCache(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	return home
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestDownloadVerifiesChecksum(t *testing.T) {
	payload := []byte("gguf bytes")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(payload)
	}))
	defer srv.Close()
	dir := t.TempDir()

	t.Run("good checksum lands atomically", func(t *testing.T) {
		dest := filepath.Join(dir, "ok.bin")
		if err := download(context.Background(), srv.URL, dest, sha256Hex(payload), 0o644); err != nil {
			t.Fatalf("download: %v", err)
		}
		got, err := os.ReadFile(dest)
		if err != nil || string(got) != string(payload) {
			t.Fatalf("dest content = %q, %v", got, err)
		}
	})

	t.Run("bad checksum leaves no file", func(t *testing.T) {
		dest := filepath.Join(dir, "bad.bin")
		err := download(context.Background(), srv.URL, dest, strings.Repeat("0", 64), 0o644)
		if err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
			t.Fatalf("want sha256 mismatch error, got %v", err)
		}
		if _, err := os.Stat(dest); !os.IsNotExist(err) {
			t.Fatalf("corrupt download must not leave a file at dest (stat err: %v)", err)
		}
		leftovers, _ := filepath.Glob(filepath.Join(dir, "bad.bin.partial-*"))
		if len(leftovers) != 0 {
			t.Fatalf("temp files left behind: %v", leftovers)
		}
	})

	t.Run("http error is reported", func(t *testing.T) {
		srv404 := httptest.NewServer(http.NotFoundHandler())
		defer srv404.Close()
		err := download(context.Background(), srv404.URL, filepath.Join(dir, "x"), strings.Repeat("0", 64), 0o644)
		if err == nil || !strings.Contains(err.Error(), "404") {
			t.Fatalf("want 404 error, got %v", err)
		}
	})
}

func TestEnsureModelUnknownVariant(t *testing.T) {
	if _, err := EnsureModel(context.Background(), "nope"); err == nil ||
		!strings.Contains(err.Error(), "unknown model variant") {
		t.Fatalf("want unknown-variant error, got %v", err)
	}
}

func TestModelArtifactsPinned(t *testing.T) {
	for variant, art := range modelArtifacts {
		if len(art.sha256) != 64 {
			t.Errorf("%s: sha256 %q is not 64 hex chars", variant, art.sha256)
		}
		if !strings.HasPrefix(art.url, "https://huggingface.co/LocalAI-io/") {
			t.Errorf("%s: unexpected url %q", variant, art.url)
		}
		if !strings.HasSuffix(art.url, ".gguf") {
			t.Errorf("%s: url %q is not a gguf", variant, art.url)
		}
	}
}

func TestLibraryArtifactName(t *testing.T) {
	cases := map[[2]string]string{
		{"darwin", "arm64"}: "libpf-darwin-arm64.dylib",
		{"darwin", "amd64"}: "libpf-darwin-amd64.dylib",
		{"linux", "amd64"}:  "libpf-linux-amd64.so",
		{"linux", "arm64"}:  "libpf-linux-arm64.so",
	}
	for in, want := range cases {
		if got := libraryArtifactName(in[0], in[1]); got != want {
			t.Errorf("libraryArtifactName(%s, %s) = %q, want %q", in[0], in[1], got, want)
		}
	}
}

func TestEnsureLibrary(t *testing.T) {
	useTempCache(t)
	key := runtime.GOOS + "-" + runtime.GOARCH
	asset := libraryArtifactName(runtime.GOOS, runtime.GOARCH)
	payload := []byte("fake shared library")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "/" + libraryVersion + "/" + asset
		if r.URL.Path != want {
			t.Errorf("request path = %q, want %q", r.URL.Path, want)
			http.NotFound(w, r)
			return
		}
		w.Write(payload)
	}))
	defer srv.Close()

	oldBase, oldSum, hadSum := libraryBaseURL, libraryChecksums[key], false
	_, hadSum = libraryChecksums[key]
	libraryBaseURL = srv.URL
	libraryChecksums[key] = sha256Hex(payload)
	t.Cleanup(func() {
		libraryBaseURL = oldBase
		if hadSum {
			libraryChecksums[key] = oldSum
		} else {
			delete(libraryChecksums, key)
		}
	})

	path, err := EnsureLibrary(context.Background())
	if err != nil {
		t.Fatalf("EnsureLibrary: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(payload) {
		t.Fatalf("downloaded library = %q, %v", got, err)
	}
	info, _ := os.Stat(path)
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o100 == 0 {
		t.Errorf("library mode = %v, want executable bit", info.Mode())
	}

	// Second call must hit the cache, not the server.
	srv.Close()
	again, err := EnsureLibrary(context.Background())
	if err != nil || again != path {
		t.Fatalf("cached EnsureLibrary = %q, %v (want %q)", again, err, path)
	}
}

func TestCachedFileValid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "artifact")
	payload := []byte("payload")
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	if !cachedFileValid(path, sha256Hex(payload)) {
		t.Error("intact file must validate")
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("intact file must survive validation: %v", err)
	}

	if cachedFileValid(filepath.Join(dir, "missing"), sha256Hex(payload)) {
		t.Error("missing file must not validate")
	}

	if err := os.WriteFile(path, []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	if cachedFileValid(path, sha256Hex(payload)) {
		t.Error("tampered file must not validate")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("tampered file must be deleted, stat err: %v", err)
	}
}

func TestEnsureLibraryRedownloadsCorruptedCache(t *testing.T) {
	useTempCache(t)
	key := runtime.GOOS + "-" + runtime.GOARCH
	payload := []byte("real shared library bytes")

	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Write(payload)
	}))
	defer srv.Close()

	oldBase := libraryBaseURL
	oldSum, hadSum := libraryChecksums[key]
	libraryBaseURL = srv.URL
	libraryChecksums[key] = sha256Hex(payload)
	t.Cleanup(func() {
		libraryBaseURL = oldBase
		if hadSum {
			libraryChecksums[key] = oldSum
		} else {
			delete(libraryChecksums, key)
		}
	})

	path, err := EnsureLibrary(context.Background())
	if err != nil {
		t.Fatalf("EnsureLibrary: %v", err)
	}
	if requests != 1 {
		t.Fatalf("initial download made %d requests, want 1", requests)
	}

	// Corrupt the cached file; the next call must detect it, delete it and
	// download a fresh verified copy rather than returning the bad bytes.
	if err := os.WriteFile(path, []byte("bitrot"), 0o755); err != nil {
		t.Fatal(err)
	}
	again, err := EnsureLibrary(context.Background())
	if err != nil {
		t.Fatalf("EnsureLibrary after corruption: %v", err)
	}
	if requests != 2 {
		t.Errorf("corrupted cache made %d total requests, want 2 (re-download)", requests)
	}
	got, err := os.ReadFile(again)
	if err != nil || string(got) != string(payload) {
		t.Fatalf("re-downloaded library = %q, %v", got, err)
	}
}

func TestEnsureLibraryUnsupportedPlatform(t *testing.T) {
	// Remove the current platform from the support matrix so the
	// unsupported branch is exercised regardless of the host.
	key := runtime.GOOS + "-" + runtime.GOARCH
	orig, had := libraryChecksums[key]
	delete(libraryChecksums, key)
	t.Cleanup(func() {
		if had {
			libraryChecksums[key] = orig
		}
	})

	_, err := EnsureLibrary(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no prebuilt libpf") {
		t.Fatalf("want no-prebuilt error, got %v", err)
	}
}
