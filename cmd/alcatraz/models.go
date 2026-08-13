package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/hoophq/alcatraz/models"
)

// The model downloader for the optional NER backend, exposed so a model can
// be materialised as a build or deploy step rather than on first use:
//
//	alcatraz models download [-dest dir] [-model id] [-origin url]
//
// This is the one alcatraz command that touches the network, and it only
// fetches the pinned URLs. Scanning never does.
//
// The heavy lifting is [models.EnsureModelFrom], which lives in the root
// module precisely so this command costs the CLI no dependencies: the ONNX
// runtime stays behind the ner module.

// ensureModelFrom is the download entry point, indirected so tests can run
// the command without a network.
var ensureModelFrom = models.EnsureModelFrom

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
	origin := fs.String("origin", "", "base URL to fetch from, laid out like the hub (empty = the model's pinned origin)")
	if err := fs.Parse(rest); err != nil {
		return 0, err
	}
	if fs.NArg() > 0 {
		return 0, fmt.Errorf("models download: unexpected argument %q", fs.Arg(0))
	}
	// Cancel on a signal rather than dying where we stand. The download
	// streams to a temp file it removes on the way out, and that cleanup is a
	// defer — which a signalled process never reaches, stranding up to 250MB
	// under a ".partial-*" name that no later run reclaims. Being evicted or
	// rescheduled mid-fetch is routine for the init container this command is
	// documented to run as, and there the leak lands on a volume that outlives
	// the pod. SIGKILL is still unrecoverable, but in Kubernetes it only
	// follows SIGTERM and the grace period.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return download(ctx, os.Stdout, *model, *dest, *origin)
}

func download(ctx context.Context, out io.Writer, model, dest, origin string) (int, error) {
	files := models.PinnedFiles(model)
	if files == nil {
		return 0, fmt.Errorf("models download: %q has no pinned checksums, so it cannot be verified (pinned models: %s)",
			model, strings.Join(models.PinnedModels(), ", "))
	}
	// Resolved here rather than left to the models package, so the origin
	// printed below is the one actually fetched from. With two possible
	// origins, a receipt that only says "downloaded" answers the wrong half
	// of the question a mirrored build raises.
	if origin == "" {
		origin = models.Origin(model)
	}

	// The manifest goes out before the first byte is fetched: 250MB over a
	// slow link is a long silence to sit through without knowing what was
	// asked for, and a warm cache turns the same list into a receipt.
	var total int64
	for _, f := range files {
		total += f.Size
	}
	fmt.Fprintf(out, "%s @ %s\n", model, shortRev(models.Revision(model)))
	fmt.Fprintf(out, "origin: %s\n", origin)
	fmt.Fprintf(out, "fetching %d files, %s total (cached files are verified, not re-fetched):\n", len(files), humanSize(total))
	for _, f := range files {
		fmt.Fprintf(out, "  %-24s %10s\n", f.Name, humanSize(f.Size))
	}

	modelPath, err := ensureModelFrom(ctx, model, dest, origin)
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
//
// A failure that already explains itself gets nothing. The destination being
// unwritable, or the disk full, is the likeliest failure in the deployment
// this command is built for — a non-root container writing a mounted volume —
// and answering it with a note about proxies sends the operator off to debug
// egress that was never involved.
//
// For the same reason no hint names huggingface.co any more: the pin table can
// send a model somewhere else and -origin overrides both, so a hint that
// assumes the hub would send an operator to allow-list a host their build
// never contacted.
func hintFor(err error) string {
	var urlErr *url.Error
	switch msg := err.Error(); {
	case strings.Contains(msg, "sha256 mismatch"), strings.Contains(msg, "size mismatch"):
		return "\n  the bytes served do not match the pinned checksums — the transfer was corrupted," +
			"\n  intercepted, or the cached copy was modified. The mismatched file was not installed;" +
			"\n  re-run to fetch it again — files already verified are kept, not fetched twice."
	// Before the *url.Error arm: a cancelled request surfaces as a *url.Error
	// wrapping context.Canceled, and the interruption is the useful half.
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "\n  the download was interrupted; re-run to resume (verified files are not re-fetched)."
	case errors.As(err, &urlErr):
		return fmt.Sprintf("\n  only %s is contacted; behind a proxy, set HTTPS_PROXY.", hostOf(urlErr.URL))
	case strings.Contains(msg, "unexpected status"):
		return "\n  the origin answered but would not serve the file; the pinned revision may have been" +
			"\n  withdrawn, a mirror may not carry it, or the request was rate limited."
	default:
		return ""
	}
}

// hostOf names the host a request was aimed at, for a hint that has to tell an
// operator which host to reach. It reads the host off the failure rather than
// off the flag because the two can differ: a redirect is followed, and what
// could not be reached is where the request ended up.
func hostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return "the model origin"
	}
	return u.Host
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
	fmt.Fprintln(w, "usage: alcatraz models download [-dest dir] [-model id] [-origin url]")
	fmt.Fprintln(w, "\nDownloads a NER model and verifies every file against its pinned sha256.")
	fmt.Fprintln(w, "Without -dest it warms the cache ner.New reads from; with -dest it writes a")
	fmt.Fprintln(w, "self-contained directory to copy into an image or a shared volume.")
	fmt.Fprintln(w, "\n-origin points the fetch at a mirror laid out like the hub, at")
	fmt.Fprintln(w, "{origin}/{model}/resolve/{revision}/{file}. The pinned digests are unchanged,")
	fmt.Fprintln(w, "so a mirror serving anything else fails the same way a corrupt transfer does.")
	fmt.Fprintln(w, "\nverifiable models:")
	for _, id := range models.PinnedModels() {
		fmt.Fprintf(w, "  %-40s %s\n", id, models.Origin(id))
	}
}
