package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/hoophq/alcatraz/models"
)

// The model downloader for the optional NER backend, exposed so a model can
// be materialised as a build or deploy step rather than on first use:
//
//	alcatraz models download [-dest dir] [-model id]
//
// This is the one alcatraz command that touches the network, and it only
// fetches the pinned URLs. Scanning never does.
//
// The heavy lifting is [models.EnsureModelIn], which lives in the root
// module precisely so this command costs the CLI no dependencies: the ONNX
// runtime stays behind the ner module.

// ensureModelIn is the download entry point, indirected so tests can run the
// command without a network. The models package hides its hub URL, so the
// seam has to be here.
var ensureModelIn = models.EnsureModelIn

func runModels(args []string) (int, error) {
	if len(args) == 0 {
		modelsUsage(os.Stderr)
		return 0, errors.New("models: expected a subcommand")
	}
	sub, rest := args[0], args[1:]
	if sub != "download" {
		modelsUsage(os.Stderr)
		return 0, fmt.Errorf("models: unknown subcommand %q (want download)", sub)
	}

	fs := flag.NewFlagSet("models download", flag.ContinueOnError)
	dest := fs.String("dest", "", "directory to write the model into (empty = the cache ner reads from)")
	model := fs.String("model", models.DefaultModel, "model id to download")
	if err := fs.Parse(rest); err != nil {
		return 0, err
	}
	if fs.NArg() > 0 {
		return 0, fmt.Errorf("models download: unexpected argument %q", fs.Arg(0))
	}
	return download(context.Background(), os.Stdout, *model, *dest)
}

func download(ctx context.Context, out io.Writer, model, dest string) (int, error) {
	files := models.PinnedFiles(model)
	if files == nil {
		return 0, fmt.Errorf("models download: %q has no pinned checksums, so it cannot be verified (pinned models: %s)",
			model, strings.Join(models.PinnedModels(), ", "))
	}

	// The manifest goes out before the first byte is fetched: 250MB over a
	// slow link is a long silence to sit through without knowing what was
	// asked for, and a warm cache turns the same list into a receipt.
	var total int64
	for _, f := range files {
		total += f.Size
	}
	fmt.Fprintf(out, "%s @ %s\n", model, shortRev(models.Revision(model)))
	fmt.Fprintf(out, "fetching %d files, %s total (cached files are verified, not re-fetched):\n", len(files), humanSize(total))
	for _, f := range files {
		fmt.Fprintf(out, "  %-24s %10s\n", f.Name, humanSize(f.Size))
	}

	modelPath, err := ensureModelIn(ctx, model, dest)
	if err != nil {
		return 0, fmt.Errorf("models download: %w%s", err, hintFor(err))
	}

	fmt.Fprintln(out, "verified:")
	for _, f := range files {
		fmt.Fprintf(out, "  %-24s %s\n", f.Name, f.SHA256)
	}
	// Both directories, because they are one level apart and picking the
	// wrong one is the likeliest way to misconfigure this: ModelsDir is the
	// parent that holds every model, ModelPath is this model's own directory.
	fmt.Fprintf(out, "\nModelsDir: %s\n", filepath.Dir(modelPath))
	fmt.Fprintf(out, "ModelPath: %s\n", modelPath)
	return 0, nil
}

// hintFor turns a download failure into something the operator can act on.
// The underlying error says what broke; this says what to do about it.
func hintFor(err error) string {
	switch msg := err.Error(); {
	case strings.Contains(msg, "sha256 mismatch"), strings.Contains(msg, "size mismatch"):
		return "\n  the bytes served do not match the pinned checksums — the transfer was corrupted," +
			"\n  intercepted, or the cached copy was modified. Nothing was installed; re-run to fetch again."
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "\n  the download was interrupted; re-run to resume (verified files are not re-fetched)."
	default:
		return "\n  only huggingface.co is contacted; behind a proxy, set HTTPS_PROXY."
	}
}

// shortRev abbreviates a commit sha for display, leaving anything that is not
// one alone.
func shortRev(rev string) string {
	if len(rev) > 8 {
		return rev[:8]
	}
	return rev
}

// humanSize renders a byte count in the units an operator sizing an image
// layer or a volume thinks in.
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for size := n / unit; size >= unit; size /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGT"[exp])
}

func modelsUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: alcatraz models download [-dest dir] [-model id]")
	fmt.Fprintln(w, "\nDownloads a NER model and verifies every file against its pinned sha256.")
	fmt.Fprintln(w, "Without -dest it warms the cache ner.New reads from; with -dest it writes a")
	fmt.Fprintln(w, "self-contained directory to copy into an image or a shared volume.")
	fmt.Fprintln(w, "\nverifiable models:")
	for _, id := range models.PinnedModels() {
		fmt.Fprintf(w, "  %s\n", id)
	}
}
