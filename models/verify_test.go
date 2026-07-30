package models

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// seedModel lays a pinned model out in dir exactly as EnsureModelIn would,
// without going near the hub. Every test here asserts the hub was never
// touched, so seeding by download would defeat the point.
func seedModel(t *testing.T, dir, id string, files map[string][]byte) string {
	t.Helper()
	modelPath := Dir(dir, id)
	if err := os.MkdirAll(modelPath, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(modelPath, name), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return modelPath
}

func TestVerifyModelInAcceptsSeededDir(t *testing.T) {
	id, files, art := testModel(t)
	hub := newFakeHub(t, files)
	pinTestModel(t, hub, id, art)

	dir := t.TempDir()
	want := seedModel(t, dir, id, files)

	got, err := VerifyModelIn(id, dir)
	if err != nil {
		t.Fatalf("VerifyModelIn: %v", err)
	}
	if got != want {
		t.Errorf("model path = %q, want %q", got, want)
	}
	if n := hub.totalHits(); n != 0 {
		t.Errorf("hub hits = %d, want 0: VerifyModelIn must not download", n)
	}
}

func TestVerifyModelInReportsMissingFile(t *testing.T) {
	id, files, art := testModel(t)
	hub := newFakeHub(t, files)
	pinTestModel(t, hub, id, art)

	dir := t.TempDir()
	modelPath := seedModel(t, dir, id, files)
	if err := os.Remove(filepath.Join(modelPath, "tokenizer.json")); err != nil {
		t.Fatal(err)
	}

	_, err := VerifyModelIn(id, dir)
	if err == nil {
		t.Fatal("VerifyModelIn succeeded on a model directory missing a file")
	}
	if !strings.Contains(err.Error(), "tokenizer.json") || !strings.Contains(err.Error(), "missing") {
		t.Errorf("err = %v, want it to name tokenizer.json as missing", err)
	}
	if !strings.Contains(err.Error(), modelPath) {
		t.Errorf("err = %v, want it to name the directory %s", err, modelPath)
	}
	if n := hub.totalHits(); n != 0 {
		t.Errorf("hub hits = %d, want 0", n)
	}
}

// A file that is present but wrong is a different failure from an absent one
// — seeded with the wrong bytes, not never seeded — and the message has to
// say which, because the two have different fixes.
func TestVerifyModelInReportsMismatchNotMissing(t *testing.T) {
	id, files, art := testModel(t)
	hub := newFakeHub(t, files)
	pinTestModel(t, hub, id, art)

	dir := t.TempDir()
	modelPath := seedModel(t, dir, id, files)
	tampered := filepath.Join(modelPath, "model.onnx")
	if err := os.WriteFile(tampered, []byte("wrong bytes entirely"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := VerifyModelIn(id, dir)
	if err == nil {
		t.Fatal("VerifyModelIn succeeded on a tampered model directory")
	}
	if !strings.Contains(err.Error(), "model.onnx") || !strings.Contains(err.Error(), "sha256") {
		t.Errorf("err = %v, want it to name model.onnx as a sha256 mismatch", err)
	}
	if strings.Contains(err.Error(), "missing") {
		t.Errorf("err = %v, reports a present-but-wrong file as missing", err)
	}

	// And the tampered file stays put: this is a read-only check, and
	// deleting the evidence would strand a concurrent reader mid-load.
	if _, statErr := os.Stat(tampered); statErr != nil {
		t.Errorf("tampered file was removed: %v", statErr)
	}
	if n := hub.totalHits(); n != 0 {
		t.Errorf("hub hits = %d, want 0: a mismatch must not trigger a re-fetch", n)
	}
}

func TestVerifyModelInColdDir(t *testing.T) {
	id, files, art := testModel(t)
	hub := newFakeHub(t, files)
	pinTestModel(t, hub, id, art)

	dir := t.TempDir()
	_, err := VerifyModelIn(id, dir)
	if err == nil {
		t.Fatal("VerifyModelIn succeeded against an empty models directory")
	}
	if !strings.Contains(err.Error(), "no model directory at") {
		t.Errorf("err = %v, want it to report the absent model directory", err)
	}

	// Looking is not a reason to write: a failed verification leaves the
	// models directory exactly as it found it.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("VerifyModelIn created %d entries in the models directory", len(entries))
	}
	if n := hub.totalHits(); n != 0 {
		t.Errorf("hub hits = %d, want 0", n)
	}
}

func TestVerifyModelInModelPathIsAFile(t *testing.T) {
	id, files, art := testModel(t)
	hub := newFakeHub(t, files)
	pinTestModel(t, hub, id, art)

	dir := t.TempDir()
	if err := os.WriteFile(Dir(dir, id), []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := VerifyModelIn(id, dir)
	if err == nil || !strings.Contains(err.Error(), "is not a directory") {
		t.Errorf("err = %v, want a not-a-directory error", err)
	}
	if n := hub.totalHits(); n != 0 {
		t.Errorf("hub hits = %d, want 0", n)
	}
}

func TestVerifyModelInUnpinnedModel(t *testing.T) {
	_, err := VerifyModelIn("nobody/not-pinned", t.TempDir())
	if err == nil {
		t.Fatal("VerifyModelIn succeeded on an unpinned model")
	}
	if !strings.Contains(err.Error(), "nobody/not-pinned") {
		t.Errorf("err = %v, want it to name the rejected model", err)
	}
	for _, id := range PinnedModels() {
		if !strings.Contains(err.Error(), id) {
			t.Errorf("err = %v, want it to list the pinned model %q", err, id)
		}
	}
}

// A baked image or a shared volume mounts the model read-only, so any
// incidental write on the verification path is a real bug — and one that
// only shows up here, since a writable temp dir hides it completely.
func TestVerifyModelInReadOnlyDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root, which ignores the permission bits under test")
	}
	id, files, art := testModel(t)
	hub := newFakeHub(t, files)
	pinTestModel(t, hub, id, art)

	dir := t.TempDir()
	modelPath := seedModel(t, dir, id, files)
	// Both levels: the model directory and its parent. Restore before the
	// test ends so t.TempDir's own cleanup can remove the tree.
	for _, p := range []string{modelPath, dir} {
		if err := os.Chmod(p, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(p, 0o755) })
	}

	got, err := VerifyModelIn(id, dir)
	if err != nil {
		t.Fatalf("VerifyModelIn on a read-only models directory: %v", err)
	}
	if got != modelPath {
		t.Errorf("model path = %q, want %q", got, modelPath)
	}
	if n := hub.totalHits(); n != 0 {
		t.Errorf("hub hits = %d, want 0", n)
	}
}

// DefaultDir names the directory; ResolveDir is the one that creates it. The
// distinction is what lets an offline caller report a missing model instead
// of a permission error from a mkdir it never needed.
func TestDefaultDirDoesNotCreate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.UserCacheDir reads %LocalAppData% here, which these env vars do not set")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)           // darwin: $HOME/Library/Caches
	t.Setenv("XDG_CACHE_HOME", home) // linux: $XDG_CACHE_HOME

	dir, err := DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	if !strings.HasPrefix(dir, home) {
		t.Fatalf("DefaultDir = %q, want it under the fake cache root %q", dir, home)
	}
	if !strings.HasSuffix(dir, filepath.Join("alcatraz", "models")) {
		t.Errorf("DefaultDir = %q, want it to end in alcatraz/models", dir)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("DefaultDir created %s (stat err = %v); only ResolveDir may create", dir, err)
	}
}
