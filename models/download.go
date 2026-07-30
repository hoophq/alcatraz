package models

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// hubBaseURL is the Hugging Face origin the pinned artifacts are fetched
// from. It is a var so tests can point it at a local server.
var hubBaseURL = "https://huggingface.co"

// fileMatches reports whether path's contents hash to wantSHA256, keeping an
// I/O failure separate from a false result. A file that cannot be read is not
// a file with the wrong bytes, and in the deployments VerifyModelIn serves —
// non-root containers, volumes mounted from elsewhere — the difference
// between the two is the difference between fixing a mode and re-provisioning
// the model.
func fileMatches(path, wantSHA256 string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return false, err
	}
	return hex.EncodeToString(hasher.Sum(nil)) == wantSHA256, nil
}

// cachedFileValid reports whether path exists and still matches the pinned
// sha256. It folds every failure into false because the download path answers
// all of them the same way: fetch the file again.
//
// A mismatched file is left where it is rather than unlinked: download
// replaces it by renaming over it, which is atomic, and unlinking here is
// not. Two callers that find the same corrupt file both hold handles to it,
// and the slower one would unlink the pathname after the faster one had
// already installed a verified replacement under it — deleting a good file
// that its caller had been told was ready.
func cachedFileValid(path, wantSHA256 string) bool {
	ok, err := fileMatches(path, wantSHA256)
	return err == nil && ok
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
