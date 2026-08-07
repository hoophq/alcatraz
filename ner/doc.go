// Package ner provides a statistical NER backend for alcatraz: it detects
// the free-text entities PERSON, LOCATION, NRP and DATE_TIME that pattern
// recognizers cannot express, using an ONNX token-classification model run
// in-process via hugot. The canonical names it emits are the
// [entities.Person], [entities.Location], [entities.NRP] and
// [entities.DateTime] constants of the root module; [Config.SupportedEntities]
// reports the set a given configuration can produce.
//
// It lives in a separate module on purpose (mirroring alcatraz/lookaround):
// importing it is the only way to pull in the model runtime, so the alcatraz
// core stays dependency-free. The default hugot backend is pure Go: no cgo,
// no shared libraries.
//
// Texts of any length work. The package splits input beyond the model's
// token limit into overlapping windows, batches them through the model and
// merges the results back (see windows.go), so it detects entities deep
// inside large texts instead of truncating them away. It pads inference
// shapes to a small set of buckets ([Config.BatchBuckets] and
// [Config.SequenceBuckets]), bounding JIT compilation to a few dozen
// programs regardless of corpus variety.
//
// # Wiring it into an analyzer
//
// [Engine] implements [analyzer.NlpEngine], so an [analyzer.Engine] configured
// with [analyzer.Engine.SetNlpEngine] runs the model at most once per Analyze
// call and shares the artifacts with every artifact-aware recognizer.
// [Engine.Recognizer] produces the [analyzer.Recognizer] that turns those
// artifacts into results, and it goes into the registry beside the pattern
// recognizers:
//
//	nlp, err := ner.New(ctx, ner.DefaultConfig())
//	// handle err; model is downloaded on first use
//	defer nlp.Close()
//
//	reg := analyzer.NewRegistry("en")
//	recognizers.LoadDefaults(reg, "en")
//	reg.Add("en", nlp.Recognizer("en"))
//
//	eng := analyzer.NewEngine(reg, []string{"en"})
//	eng.SetNlpEngine(nlp)
//
// Without SetNlpEngine the recognizer still works ([Recognizer.Analyze] runs
// the model itself), but then every artifact-aware recognizer that wants the
// same spans pays for its own inference pass.
//
// [Engine] also implements [analyzer.BatchNlpEngine]: [Engine.ProcessTexts]
// runs many texts through batched inference calls, which amortizes the
// per-call tokenization and graph overhead and is substantially faster than
// calling [Engine.ProcessText] in a loop.
//
// # Machine output: segmentation
//
// Transformer NER is context-sensitive by construction, and machine output
// (query results, logs) surrounds a name with text no news-trained model has
// seen. Feeding a wide table in as one blob can drive recall to zero on names
// the model catches in isolation. [Config.Segmentation] chooses what the
// model sees as one sequence:
//
//	SegmentWhole (default)  one sequence per text     82% recall  1.0x cost
//	SegmentLines            cut after every newline   88% recall  1.8x cost
//	SegmentFields           also cut after every tab  94% recall  4.3x cost
//
// Those figures are corpus averages measured on generated psql/log output plus
// a real 28-column psql capture (286 planted names) against the default model;
// cost is inference calls, and it scales with line and cell count rather than
// byte count. The default stays SegmentWhole because prose is the common case
// and finer segmentation cannot help it: a segment boundary is a hard context
// boundary, so an entity is never read across one. Callers streaming tabular
// or log output should set SegmentLines, and SegmentFields when the output is
// tab-delimited and recall matters more than throughput.
//
// Segmentation composes with windowing: a segment longer than the token budget
// is still split into overlapping windows, so no input is truncated under any
// setting.
//
// # Getting the model
//
// [New] downloads the model on first use, which is convenient on a laptop and
// unhelpful in a build pipeline or a network-restricted deployment.
// [EnsureModel] fetches it up front instead, into the same directory [New]
// reads from, so [New] finds a warm cache and touches the network not at all:
//
//	// In an image build or an init container.
//	dir, err := ner.EnsureModelIn(ctx, "KnightsAnalytics/distilbert-NER", "/opt/alcatraz/models")
//
//	// At runtime, pointed at what was built above.
//	cfg := ner.DefaultConfig()
//	cfg.ModelsDir = "/opt/alcatraz/models"
//
// [Config.ModelsDir] is the directory models are stored under, one
// subdirectory per model id; [Config.ModelPath] names a single model
// directory and suppresses the download. [EnsureModelIn] returns the latter
// given the former, so either field can be filled from the same call.
//
// Every file of a model listed by [PinnedModels] is fetched from a pinned
// commit and checked against a pinned sha256, on download and again on every
// load, since a cached file may differ from the one we pinned. Models
// outside that list still work, but nothing verifies what the hub serves for
// them.
//
// The same fetch is available without writing Go, for a Dockerfile or an
// init container:
//
//	alcatraz models download --dest /opt/alcatraz/models
//
// It prints both directories the config wants: ModelsDir, and the ModelPath
// beneath it. See the [models] package for the pin table itself.
//
// [Config.Offline] turns "should be cached by now" into a guarantee. [New]
// then opens no socket at all: a missing or mismatched model is an error
// naming the file and the fix, rather than a download that trips egress
// monitoring on its way to a DNS error that says nothing about the model.
//
//	cfg := ner.DefaultConfig()
//	cfg.ModelsDir = "/opt/alcatraz/models"
//	cfg.Offline = true
//
// Because an offline caller has no fallback, it also gets the directory
// checked before hugot sees it: pinned models against their sha256s, any
// model directory for config.json, tokenizer.json and an .onnx file. Nothing
// on that path writes, so a model mounted read-only loads unchanged.
// [VerifyModelIn] runs the same digest check on demand, for a startup probe or
// a container health check.
//
// # Faster inference
//
// The pure-Go backend is the portability floor, not the speed ceiling. For
// large corpora, [Config.Backend] selects a faster hugot backend and
// [Config.Accelerator] adds a GPU execution provider on top:
//
//	Backend      build tags   runtime dependency          speed (indicative)
//	"go"         none         none                        1x (baseline)
//	"ort"        -tags ORT    libonnxruntime.{so,dylib}   ~5-10x on CPU
//	"ort"+accel  -tags ORT    + CoreML / CUDA / DirectML  beyond that
//	"xla"        -tags XLA    PJRT plugin (CPU/CUDA/TPU)  similar to ORT
//
//	cfg := ner.DefaultConfig()
//	cfg.Backend = ner.BackendORT            // requires a -tags ORT build
//	cfg.Accelerator = ner.AcceleratorCoreML // Apple GPU/Neural Engine
//	nlp, err := ner.New(ctx, cfg)
//
// The ORT and XLA build tags imply cgo, so accelerated binaries cannot be
// cross-compiled the way pure-Go ones can, and they load a native shared
// library at runtime (on macOS, "brew install onnxruntime" is found without
// configuration; elsewhere set [Config.ORTLibraryPath]). Selecting a backend
// that is not compiled in makes [New] fail with an error naming the missing
// build tag, so a pure-Go binary reports the mismatch instead of degrading
// with no error. Asking for an accelerator the backend does not support
// fails the same way, rather than dropping to CPU with no diagnostic.
//
// # What the spans guarantee
//
// Everything this package does around the model (windowing, batching,
// fold-offset remapping, span merging and word snapping) is backend
// independent, and the spans it returns satisfy the same guarantees on every
// backend:
//
//   - Byte offsets into the caller's text. The model sees an ASCII-folded
//     rendering (see foldASCII in offsets.go, which works around a
//     span-tracking bug in the pure-Go tokenizer) and every reported span is
//     mapped back and validated, so the alcatraz invariant
//     text[Start:End] == matched span holds for any input, including
//     non-ASCII.
//   - Whole words. The model tags WordPiece subwords and the SIMPLE
//     aggregation configured here opens a new group at every B- tag, so on
//     any backend it hands back fragments like "andar Seri Begawa". The
//     package grows every span to the word it sits inside and unions
//     same-type spans that then touch. See snapToWords (words.go) for why
//     half a name is not an acceptable answer at the API boundary.
package ner
