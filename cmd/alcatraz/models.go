package main

import (
	"context"
	"encoding/json"
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

// The model commands for the optional NER backend, exposed so a model can be
// materialised and checked as a build or deploy step rather than on first use:
//
//	alcatraz models download [-dest dir] [-model id] [-origin url]
//	alcatraz models verify   [-dir dir] [-model id]
//	alcatraz models pins     [-model id]
//
// download is the one alcatraz command that touches the network, and it only
// fetches the pinned URLs. Scanning never does, and neither do verify or pins.
//
// The heavy lifting is [models.EnsureModelFrom] and [models.VerifyModelIn],
// which live in the root module precisely so these commands cost the CLI no
// dependencies: the ONNX runtime stays behind the ner module.

// ensureModelFrom and verifyModelIn are the entry points, indirected so tests
// can run the commands without a network and without a seeded directory.
var (
	ensureModelFrom = models.EnsureModelFrom
	verifyModelIn   = models.VerifyModelIn
)

func runModels(args []string) (int, error) {
	if len(args) == 0 {
		modelsUsage(os.Stderr)
		return 0, errors.New("models: expected a subcommand")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "download":
		return runModelsDownload(rest)
	case "verify":
		return runModelsVerify(rest)
	case "pins":
		return runModelsPins(rest)
	default:
		modelsUsage(os.Stderr)
		return 0, fmt.Errorf("models: unknown subcommand %q (want download, verify or pins)", sub)
	}
}

func runModelsDownload(rest []string) (int, error) {
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

func runModelsVerify(rest []string) (int, error) {
	fs := flag.NewFlagSet("models verify", flag.ContinueOnError)
	// -dir, not -dest: this command writes nothing, and the naming split
	// between the models directory and a model's own directory is the one
	// thing worth being pedantic about here. It is the same directory
	// "download -dest" fills.
	dir := fs.String("dir", "", "models directory to check (empty = the cache ner reads from)")
	// -dest is accepted as an alias all the same, so a line lifted out of a
	// download runbook runs unedited. The two commands sit next to each other
	// in every runbook that has either; failing one of them over the name of
	// the same path is friction with nothing on the other side of it.
	dest := fs.String("dest", "", "alias for -dir, for symmetry with download")
	model := fs.String("model", models.DefaultModel, "model id to check")
	if err := fs.Parse(rest); err != nil {
		return 0, err
	}
	if fs.NArg() > 0 {
		return 0, fmt.Errorf("models verify: unexpected argument %q", fs.Arg(0))
	}
	switch {
	case *dir != "" && *dest != "" && *dir != *dest:
		return 0, fmt.Errorf("models verify: -dir %q and -dest %q name different directories, and they are the same flag", *dir, *dest)
	case *dir == "":
		*dir = *dest
	}
	// No signal handling, unlike download: this opens no socket and creates no
	// file, so an interrupted run leaves nothing behind to clean up.
	return verify(os.Stdout, *model, *dir)
}

func runModelsPins(rest []string) (int, error) {
	fs := flag.NewFlagSet("models pins", flag.ContinueOnError)
	model := fs.String("model", models.DefaultModel, "model id to describe")
	list := fs.Bool("list", false, "print every pinned model id, one per line")
	if err := fs.Parse(rest); err != nil {
		return 0, err
	}
	if fs.NArg() > 0 {
		return 0, fmt.Errorf("models pins: unexpected argument %q", fs.Arg(0))
	}
	if *list {
		return pinsList(os.Stdout)
	}
	return pins(os.Stdout, *model)
}

// pinsList prints the pinned model ids, so a publisher can loop over the table
// without carrying its own list of what is in it.
func pinsList(out io.Writer) (int, error) {
	w := &errWriter{w: out}
	for _, id := range models.PinnedModels() {
		w.printf("%s\n", id)
	}
	if w.err != nil {
		return 0, fmt.Errorf("models pins: writing output: %w", w.err)
	}
	return 0, nil
}

// pinManifest is the wire format of "models pins", declared here rather than
// reusing models.PinnedFile so an internal rename cannot break scripts.
type pinManifest struct {
	Model    string       `json:"model"`
	Revision string       `json:"revision"`
	Origin   string       `json:"origin"`
	License  pinLicense   `json:"license"`
	Files    []pinFileOut `json:"files"`
}

type pinLicense struct {
	ID     string `json:"id"`
	Source string `json:"source"`
}

type pinFileOut struct {
	// Key is the path under an origin the downloader asks for, precomputed so
	// a mirror does not keep its own copy of the layout.
	Key    string `json:"key"`
	Path   string `json:"path"`
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// pins writes the pin table's entry for model as JSON, so a publisher mirrors
// from the same source of truth the downloader verifies against.
func pins(out io.Writer, model string) (int, error) {
	files := models.PinnedFiles(model)
	if files == nil {
		return 0, fmt.Errorf("models pins: %q has no pinned checksums (pinned models: %s)",
			model, strings.Join(models.PinnedModels(), ", "))
	}
	rev := models.Revision(model)
	id, source := models.License(model)
	m := pinManifest{
		Model:    model,
		Revision: rev,
		Origin:   models.Origin(model),
		License:  pinLicense{ID: id, Source: source},
		Files:    make([]pinFileOut, 0, len(files)),
	}
	for _, f := range files {
		m.Files = append(m.Files, pinFileOut{
			Key:    fmt.Sprintf("%s/resolve/%s/%s", model, rev, f.Path),
			Path:   f.Path,
			Name:   f.Name,
			SHA256: f.SHA256,
			Size:   f.Size,
		})
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		return 0, fmt.Errorf("models pins: writing output: %w", err)
	}
	return 0, nil
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
	w := &errWriter{w: out}
	w.printf("%s @ %s\n", model, shortRev(models.Revision(model)))
	w.printf("origin: %s\n", origin)
	w.printf("license: %s\n", licenseOf(model))
	w.printf("fetching %d files, %s total (cached files are verified, not re-fetched):\n", len(files), humanSize(total))
	for _, f := range files {
		w.printf("  %-24s %10s\n", f.Name, humanSize(f.Size))
	}
	// Checked before the fetch, not only at the end: there is no point pulling
	// 250MB to report it into a writer that is already failing.
	if w.err != nil {
		return 0, fmt.Errorf("models download: writing output: %w", w.err)
	}

	modelPath, err := ensureModelFrom(ctx, model, dest, origin)
	if err != nil {
		return 0, fmt.Errorf("models download: %w%s", err, hintFor(err))
	}

	w.printf("verified:\n")
	for _, f := range files {
		w.printf("  %-24s %s\n", f.Name, f.SHA256)
	}
	// Both directories, because they are one level apart and picking the
	// wrong one is the likeliest way to misconfigure this: ModelsDir is the
	// parent that holds every model, ModelPath is this model's own directory.
	w.printf("\nModelsDir: %s\n", filepath.Dir(modelPath))
	w.printf("ModelPath: %s\n", modelPath)
	if w.err != nil {
		return 0, fmt.Errorf("models download: writing output: %w", w.err)
	}
	return 0, nil
}

// verify re-checks an already-seeded models directory against the pin table
// and reports what it found. It is the assertion half of download, for the two
// places that cannot just re-run download: a CI job proving an image really
// carries the model it claims to — from outside the image, since a distroless
// one has no shell — and an operator staring at a volume the model runtime
// rejected.
//
// It opens no socket and writes nothing, so it is safe against a read-only
// mount and safe to run in a network-sealed build.
func verify(out io.Writer, model, dir string) (int, error) {
	files := models.PinnedFiles(model)
	if files == nil {
		return 0, fmt.Errorf("models verify: %q has no pinned checksums, so it cannot be verified (pinned models: %s)",
			model, strings.Join(models.PinnedModels(), ", "))
	}
	// DefaultDir names the cache without creating it, which is exactly the
	// property this command needs: looking is not a reason to write. Resolved
	// here only so the line below names a real path rather than "the default".
	shown := dir
	if shown == "" {
		d, err := models.DefaultDir()
		if err != nil {
			return 0, fmt.Errorf("models verify: %w", err)
		}
		shown = d
	}

	w := &errWriter{w: out}
	w.printf("%s @ %s\n", model, shortRev(models.Revision(model)))
	w.printf("license: %s\n", licenseOf(model))
	w.printf("checking %d files in %s (no network, no writes):\n", len(files), shown)
	for _, f := range files {
		w.printf("  %-24s %10s\n", f.Name, humanSize(f.Size))
	}
	// Checked before the hashing, not only at the end: re-reading 250MB to
	// report the result into a writer that is already failing helps nobody.
	if w.err != nil {
		return 0, fmt.Errorf("models verify: writing output: %w", w.err)
	}

	modelPath, err := verifyModelIn(model, dir)
	if err != nil {
		return 0, fmt.Errorf("models verify: %w%s", err, verifyHintFor(err, model, dir))
	}

	w.printf("verified:\n")
	for _, f := range files {
		w.printf("  %-24s %s\n", f.Name, f.SHA256)
	}
	w.printf("\nModelsDir: %s\n", filepath.Dir(modelPath))
	w.printf("ModelPath: %s\n", modelPath)
	// A verify that could not report is not a verify that passed: the whole
	// output is the result, and a caller reading exit 0 with nothing on stdout
	// would conclude the model is good on no evidence.
	if w.err != nil {
		return 0, fmt.Errorf("models verify: writing output: %w", w.err)
	}
	return 0, nil
}

// errWriter latches the first write error so a run of report lines can be
// written straight through and answered for once, instead of nesting eight
// error checks around what is conceptually one paragraph.
//
// Write errors are returned rather than dropped, per the contract writeFindings
// already keeps: a stdout that failed — a full disk, a descriptor closed under
// the process — has to surface as exit code 2. Reporting nothing and exiting 0
// is the one outcome these commands must not produce, since both are read by
// something deciding whether a model is usable.
type errWriter struct {
	w   io.Writer
	err error
}

func (e *errWriter) printf(format string, a ...any) {
	if e.err != nil {
		return
	}
	_, e.err = fmt.Fprintf(e.w, format, a...)
}

// verifyHintFor turns a verification failure into the next thing to do. The
// three kinds VerifyModelIn keeps apart lead to three different places, and
// collapsing them is how an operator ends up re-provisioning a model whose
// only problem was its mode.
func verifyHintFor(err error, model, dir string) string {
	// A models directory that is really a model directory fails as "nothing
	// here", which reads as a missing download rather than as an argument one
	// level too deep. Checked first, because the advice it replaces — download
	// it again — would write a second copy nested inside the first.
	if wrongLevel := wrongLevelHint(err, model, dir); wrongLevel != "" {
		return wrongLevel
	}
	switch msg := err.Error(); {
	case strings.Contains(msg, "does not match its pinned sha256"):
		return "\n  the file is there and its bytes are not the pinned ones — a stale image layer, a" +
			"\n  truncated copy, or a modified volume. Re-fetch it with: alcatraz models download" + destHint(dir)
	case strings.Contains(msg, "cannot read"):
		return "\n  the bytes may be fine and this process cannot see them; check ownership and mode" +
			"\n  before re-provisioning the model."
	case strings.Contains(msg, "is missing from"), strings.Contains(msg, "no model directory at"),
		strings.Contains(msg, "is not a directory"):
		return "\n  seed the directory with: alcatraz models download" + destHint(dir)
	default:
		return ""
	}
}

// wrongLevelHint catches the mistake the ModelsDir/ModelPath split exists to
// prevent: -dir given this model's own directory instead of the parent that
// holds every model. Detected by name, because that is all there is to go on —
// the directory the command then looks in does not exist.
func wrongLevelHint(err error, model, dir string) string {
	if dir == "" || !strings.Contains(err.Error(), "no model directory at") {
		return ""
	}
	if filepath.Base(dir) != models.Dir("", model) {
		return ""
	}
	return "\n  -dir wants the models directory, the parent that holds every model, not this model's" +
		"\n  own directory. Try: alcatraz models verify -dir " + filepath.Dir(dir)
}

// destHint repeats the directory back in the form the download command takes
// it, so the suggestion can be pasted rather than reassembled.
func destHint(dir string) string {
	if dir == "" {
		return ""
	}
	return " --dest " + dir
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

// licenseOf renders a pinned model's license for the report. The URL is on the
// line because for the default model it is not the repository the files come
// from, and "Apache-2.0" alone would send an auditor to one that never
// claimed so.
func licenseOf(model string) string {
	id, source := models.License(model)
	switch {
	case id == "":
		return "unrecorded"
	case source == "":
		return id
	}
	return fmt.Sprintf("%s (declared at %s)", id, source)
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
	fmt.Fprintln(w, "       alcatraz models verify   [-dir dir] [-model id]")
	fmt.Fprintln(w, "       alcatraz models pins     [-model id] [-list]")
	fmt.Fprintln(w, "\nDownloads a NER model and verifies every file against its pinned sha256.")
	fmt.Fprintln(w, "Without -dest it warms the cache ner.New reads from; with -dest it writes a")
	fmt.Fprintln(w, "self-contained directory to copy into an image or a shared volume.")
	fmt.Fprintln(w, "\n-origin points the fetch at a mirror laid out like the hub, at")
	fmt.Fprintln(w, "{origin}/{model}/resolve/{revision}/{file}. The pinned digests are unchanged,")
	fmt.Fprintln(w, "so a mirror serving anything else fails the same way a corrupt transfer does.")
	fmt.Fprintln(w, "\nverify re-checks a directory download already filled, without fetching")
	fmt.Fprintln(w, "anything: no network, no writes, safe against a read-only mount. It exits")
	fmt.Fprintln(w, "non-zero naming the first file that is absent, mismatched or unreadable —")
	fmt.Fprintln(w, "three different problems that are worth telling apart. -dir is the models")
	fmt.Fprintln(w, "directory, the same one download fills with -dest, and -dest is accepted")
	fmt.Fprintln(w, "as an alias for it.")
	fmt.Fprintln(w, "\npins writes the pin table's entry for a model as JSON — revision, license,")
	fmt.Fprintln(w, "and every file with its digest, size and the key an origin must serve it")
	fmt.Fprintln(w, "under. It exists so a publisher mirroring the model reads the table instead")
	fmt.Fprintln(w, "of keeping a second copy of it that drifts.")
	// Listed here, not only in a run's report: whether a model may be baked
	// into an image we publish is a question that comes up before any fetch.
	fmt.Fprintln(w, "\nverifiable models:")
	for _, id := range models.PinnedModels() {
		license, _ := models.License(id)
		fmt.Fprintf(w, "  %-40s %-24s %s\n", id, models.Origin(id), license)
	}
}
