package ner

import (
	"sort"

	"github.com/hoophq/alcatraz/entities"
	"github.com/hoophq/alcatraz/models"
	"github.com/knights-analytics/hugot/options"
)

// Inference backends selectable via Config.Backend. The zero value means
// BackendGo.
const (
	BackendGo  = "go"
	BackendORT = "ort"
	BackendXLA = "xla"
)

// Hardware execution providers selectable via Config.Accelerator. The zero
// value means CPU.
const (
	AcceleratorCoreML   = "coreml"
	AcceleratorCUDA     = "cuda"
	AcceleratorDirectML = "directml"
)

// Input segmentation rules selectable via Config.Segmentation. The zero value
// means SegmentWhole. See Config.Segmentation for the recall/cost tradeoff.
const (
	SegmentWhole  = "whole"
	SegmentLines  = "lines"
	SegmentFields = "fields"
)

// Config configures the NER engine: which model to run and how its labels
// map onto alcatraz entity types.
//
// The label mapping plays the same role as Presidio's NerModelConfiguration:
// it is the single place where model-specific label names (PER, LOC, GPE, …)
// become canonical entities.* names, and where noisy labels are dropped or
// down-weighted.
type Config struct {
	// Model is a Hugging Face model id of an ONNX token-classification
	// export, e.g. "KnightsAnalytics/distilbert-NER". It is downloaded into
	// ModelsDir on first use. Ignored when ModelPath is set.
	//
	// Models listed by PinnedModels are fetched from a pinned commit and
	// verified against pinned sha256s on every load; any other id is
	// downloaded through hugot with no integrity check at all. Use
	// EnsureModel to populate ModelsDir ahead of time.
	Model string
	// ModelPath is a local directory holding the model files (model.onnx,
	// tokenizer.json, config.json). When set, no download happens.
	ModelPath string
	// ModelsDir is where downloaded models are stored. Defaults to
	// "alcatraz/models" under the user cache directory.
	ModelsDir string
	// OnnxFilename selects the .onnx file when the model repository holds
	// more than one. Empty means the single .onnx file in the repository.
	OnnxFilename string
	// Offline forbids New from performing any network I/O. The model must
	// already be on disk; a missing or mismatched one is an error naming the
	// file and the fix, not a download.
	//
	// Falling back to a download is not a graceful degradation in a
	// locked-down deployment. The attempt trips egress monitoring even when
	// it fails, and what the caller gets back is a DNS or TLS error that says
	// nothing about the model. This turns "should be cached by now" into a
	// guarantee that holds whatever the cache turns out to contain.
	//
	// It also tightens what counts as loadable, since a caller that cannot
	// fall back deserves to hear about a bad directory before the runtime
	// does: pinned models are re-checked against their sha256s, and any model
	// directory — including one named by ModelPath, which is otherwise passed
	// through untouched — must hold config.json, tokenizer.json and an .onnx
	// file. Seed one with EnsureModelIn or "alcatraz models download".
	Offline bool

	// Backend selects the hugot inference backend (see the package
	// documentation for the build matrix):
	//
	//   - BackendGo (default): pure Go, no cgo, no shared libraries. Works
	//     in every build; the slowest option.
	//   - BackendORT: ONNX Runtime via cgo. Typically 5-10x faster than the
	//     Go backend on CPU, and the gateway to GPU acceleration (see
	//     Accelerator). The binary must be built with "-tags ORT" and an
	//     ONNX Runtime shared library must be present at runtime; selecting
	//     it in a build without the tag makes New fail with a clear error.
	//   - BackendXLA: gomlx over XLA/PJRT plugins. Requires "-tags XLA" and
	//     a PJRT plugin at runtime.
	Backend string
	// ORTLibraryPath locates the ONNX Runtime shared library for
	// BackendORT: either the library file itself (libonnxruntime.dylib,
	// libonnxruntime.so, onnxruntime.dll) or the directory containing it.
	// Empty falls back to hugot's platform default (/usr/local/lib on
	// macOS, /usr/lib on Linux). Ignored by other backends.
	ORTLibraryPath string
	// Accelerator requests a hardware execution provider on top of the
	// selected backend. Empty means CPU. Supported values:
	//
	//   - AcceleratorCoreML: Apple GPU / Neural Engine (BackendORT, macOS).
	//   - AcceleratorCUDA: NVIDIA GPU (BackendORT or BackendXLA).
	//   - AcceleratorDirectML: DirectX 12 GPU (BackendORT, Windows).
	//
	// Selecting an accelerator the backend does not support makes New fail
	// fast rather than silently running on CPU.
	Accelerator string
	// AcceleratorOptions passes provider-specific flags through to the
	// execution provider, e.g. CoreML's "ModelFormat"/"MLComputeUnits" or
	// CUDA's "device_id". For AcceleratorDirectML the "device_id" entry
	// selects the GPU (default 0). Nil means provider defaults.
	AcceleratorOptions map[string]string
	// SessionOptions appends raw hugot session options after the ones
	// derived from this Config, as an escape hatch for tuning knobs not
	// modeled here (thread counts, graph optimization level, ...). Most
	// callers leave it nil.
	SessionOptions []options.WithOption

	// LabelMapping maps model labels (after BIO-prefix stripping, so "PER"
	// not "B-PER") to canonical entity names. Unmapped labels are kept
	// as-is, mirroring Presidio's keep-but-warn behavior; add them to
	// LabelsToIgnore to drop them.
	LabelMapping map[string]string
	// LabelsToIgnore drops spans whose model label or mapped entity name is
	// in the list.
	LabelsToIgnore []string
	// LowScoreEntities lists mapped entity names whose confidence is
	// multiplied by LowScoreMultiplier, for labels the model is known to
	// over-predict.
	LowScoreEntities []string
	// LowScoreMultiplier is the factor applied to LowScoreEntities scores.
	// Zero disables the adjustment.
	LowScoreMultiplier float64

	// BatchBuckets and SequenceBuckets bound the set of input shapes the
	// runtime JIT-compiles: inputs are padded up to the nearest bucket, so
	// at most len(BatchBuckets) × len(SequenceBuckets) programs are ever
	// compiled instead of one per distinct input shape — on varied corpora
	// that turns hundreds of expensive graph compilations into a handful.
	// Nil means DefaultConfig's buckets; an explicit empty slice disables
	// bucketing (exact shapes, unbounded compilation cache). Entries must
	// be positive; New rejects zero or negative values.
	//
	// The largest BatchBuckets entry is also the engine's inference
	// sub-batch size, and the largest SequenceBuckets entry caps the
	// windowed-inference window size (clamped to the model's own token
	// limit), so texts of any length are analyzed in full. Those two roles
	// apply on every backend; the shape-padding role is specific to
	// BackendGo and BackendXLA (BackendORT handles dynamic shapes natively
	// and ignores bucketing).
	BatchBuckets []int
	// SequenceBuckets is documented with BatchBuckets.
	SequenceBuckets []int

	// Segmentation selects what the model sees as one sequence. It exists
	// because transformer NER is context-sensitive by construction: the same
	// name scores differently depending on what surrounds it, and machine
	// output (query results, logs) surrounds it with text no news-trained
	// model has seen. Feeding a wide table in as one blob can drive recall to
	// zero on names the model finds easily in isolation.
	//
	//   - SegmentWhole (default): one sequence per text, split only when it
	//     exceeds the token budget (see BatchBuckets). Correct for prose.
	//   - SegmentLines: cut after every newline, so each record — table row,
	//     log line — is its own sequence.
	//   - SegmentFields: cut after every newline and tab, so each cell of
	//     tab-delimited output is its own sequence.
	//
	// Measured on a corpus of generated psql/log output plus a real 28-column
	// psql capture (286 planted names), against the default model:
	//
	//   whole    82% recall   1.0x cost
	//   lines    88% recall   1.8x cost
	//   fields   94% recall   4.3x cost
	//
	// The cost is inference calls: finer segmentation means more, shorter
	// sequences. The default stays SegmentWhole because prose is the common
	// case and finer segmentation cannot help it — a segment boundary is a
	// hard context boundary, so an entity is never read across one. Callers
	// streaming tabular or log output should set SegmentLines, and
	// SegmentFields when the output is tab-delimited and recall matters more
	// than throughput.
	//
	// Segmentation composes with windowing: a segment longer than the token
	// budget is still split into overlapping windows, so no input is
	// truncated under any setting.
	Segmentation string
}

// DefaultConfig returns a configuration matching Presidio's default NER
// surface: it emits PERSON, LOCATION, NRP and DATE_TIME, and drops
// ORGANIZATION (many false positives) plus CoNLL's MISC (too broad to emit
// under a canonical name).
func DefaultConfig() Config {
	return Config{
		Model: models.DefaultModel,
		LabelMapping: map[string]string{
			// CoNLL-style labels.
			"PER": entities.Person,
			"LOC": entities.Location,
			"ORG": "ORGANIZATION",
			// OntoNotes-style labels, for models trained on that scheme.
			"PERSON":   entities.Person,
			"GPE":      entities.Location,
			"FAC":      entities.Location,
			"LOCATION": entities.Location,
			"NORP":     entities.NRP,
			"DATE":     entities.DateTime,
			"TIME":     entities.DateTime,
		},
		LabelsToIgnore:     []string{"ORGANIZATION", "MISC"},
		LowScoreMultiplier: 0.4,
		// Shapes are padded to (batch bucket, sequence bucket) pairs: fine
		// enough that short texts don't pay for long ones, coarse enough
		// that at most 6 × 8 programs are compiled. The 256 cap is well
		// under the model's 512-token position limit on purpose: BERT-family
		// NER recall degrades sharply as sequences approach the limit
		// (entities late in a near-limit window are missed outright), and
		// shorter windows are also cheaper to run.
		BatchBuckets:    []int{1, 2, 4, 8, 16, 32},
		SequenceBuckets: []int{16, 32, 48, 64, 96, 128, 192, 256},
	}
}

// SupportedEntities returns the sorted, de-duplicated entity names this
// configuration can emit: the mapping's values minus the ignore list.
func (c Config) SupportedEntities() []string {
	ignored := make(map[string]bool, len(c.LabelsToIgnore))
	for _, l := range c.LabelsToIgnore {
		ignored[l] = true
	}
	seen := map[string]bool{}
	var out []string
	for _, entity := range c.LabelMapping {
		if ignored[entity] || seen[entity] {
			continue
		}
		seen[entity] = true
		out = append(out, entity)
	}
	sort.Strings(out)
	return out
}
