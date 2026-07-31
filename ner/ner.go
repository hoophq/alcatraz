// Package ner provides a statistical NER backend for alcatraz: it detects
// free-text entities — PERSON, LOCATION, NRP, DATE_TIME — that pattern
// recognizers cannot express, using an ONNX token-classification model run
// in-process via hugot.
//
// Texts of any length are supported: input beyond the model's token limit is
// split into overlapping windows that are batched through the model and
// merged back (see windows.go), so entities deep inside large texts are
// detected instead of being truncated away. Inference shapes are padded to a
// small set of buckets (Config.BatchBuckets/SequenceBuckets), bounding JIT
// compilation to a few dozen programs regardless of corpus variety.
//
// It lives in a separate module on purpose (mirroring alcatraz/lookaround):
// importing it is the only way to pull in the model runtime, so the alcatraz
// core stays dependency-free. The default hugot backend is pure Go — no cgo,
// no shared libraries.
//
// # Faster inference
//
// The pure-Go backend is the portability floor, not the speed ceiling. For
// large corpora, Config.Backend selects a faster hugot backend and
// Config.Accelerator adds a GPU execution provider on top:
//
//	Backend      build tags   runtime dependency          speed (indicative)
//	"go"         none         none                        1x (baseline)
//	"ort"        -tags ORT    libonnxruntime.{so,dylib}   ~5-10x on CPU
//	"ort"+accel  -tags ORT    + CoreML / CUDA / DirectML  beyond that
//	"xla"        -tags XLA    PJRT plugin (CPU/CUDA/TPU)  similar to ORT
//
//	cfg := ner.DefaultConfig()
//	cfg.Backend = ner.BackendORT          // requires a -tags ORT build
//	cfg.Accelerator = ner.AcceleratorCoreML // Apple GPU/Neural Engine
//	nlp, err := ner.New(ctx, cfg)
//
// The ORT and XLA build tags imply cgo, so accelerated binaries cannot be
// cross-compiled the way pure-Go ones can, and they load a native shared
// library at runtime (on macOS, "brew install onnxruntime" is found
// automatically; elsewhere set Config.ORTLibraryPath). Selecting a backend
// that is not compiled in makes New fail with an error saying which build
// tag is missing, so a pure-Go binary degrades loudly, not silently.
//
// Everything this package does around the model — windowing, batching,
// fold-offset remapping, span merging and word snapping — is backend
// independent, and the spans it returns satisfy the same guarantees on every
// backend. The model's own tokenization does not: hugot's pure-Go tokenizer
// decodes each token id in isolation, which erases the WordPiece "##" marker
// and leaves its subword detection permanently false, so the upstream
// word-aggregating strategies are inert under BackendGo. That is why
// snapToWords (words.go) is applied unconditionally rather than delegated to
// the pipeline; see its doc comment.
//
// # Getting the model
//
// New downloads the model on first use, which is convenient on a laptop and
// unhelpful in a build pipeline or a network-restricted deployment.
// EnsureModel fetches it up front instead, into the same directory New reads
// from, so New finds a warm cache and touches the network not at all:
//
//	// In an image build or an init container.
//	dir, err := ner.EnsureModelIn(ctx, "KnightsAnalytics/distilbert-NER", "/opt/alcatraz/models")
//
//	// At runtime, pointed at what was built above.
//	cfg := ner.DefaultConfig()
//	cfg.ModelsDir = "/opt/alcatraz/models"
//
// Every file of a model listed by PinnedModels is fetched from a pinned
// commit and checked against a pinned sha256 — on download and again on
// every load, since a cached file that exists is not evidence that it is the
// file we pinned. Models outside that list still work, but nothing verifies
// what the hub serves for them.
//
// The same fetch is available without writing Go, for a Dockerfile or an
// init container:
//
//	alcatraz models download --dest /opt/alcatraz/models
//
// It prints both directories the config wants: ModelsDir, and the ModelPath
// beneath it. See the alcatraz/models package for the pin table itself.
//
// Config.Offline turns "should be cached by now" into a guarantee. New then
// opens no socket at all: a missing or mismatched model is an error naming
// the file and the fix, rather than a download that trips egress monitoring
// on its way to a DNS error that says nothing about the model.
//
//	cfg := ner.DefaultConfig()
//	cfg.ModelsDir = "/opt/alcatraz/models"
//	cfg.Offline = true
//
// Because an offline caller has no fallback, it also gets the directory
// checked before hugot sees it — pinned models against their sha256s, any
// model directory for config.json, tokenizer.json and an .onnx file. Nothing
// on that path writes, so a model mounted read-only loads unchanged.
//
// The Engine implements analyzer.NlpEngine, so an analyzer.Engine configured
// with SetNlpEngine runs the model once per Analyze call and shares the
// artifacts with every artifact-aware recognizer:
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
package ner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/gomlx/go-huggingface/tokenizers/api"
	"github.com/hoophq/alcatraz/analyzer"
	"github.com/hoophq/alcatraz/models"
	"github.com/knights-analytics/hugot"
	"github.com/knights-analytics/hugot/pipelines"
)

// Engine runs an ONNX token-classification model and implements
// analyzer.NlpEngine. Create it with New and release it with Close. Per
// hugot's guidance a single pipeline may be called from multiple goroutines,
// so Engine is safe for concurrent use after construction.
type Engine struct {
	// runCtx is used for inference calls. It is derived from New's ctx
	// with context.WithoutCancel: the construction ctx bounds the model
	// download and session setup only, and cancelling it afterwards must
	// not poison every future ProcessText. The engine's lifetime is
	// governed by Close, not by a context.
	runCtx   context.Context
	session  *hugot.Session
	pipeline *pipelines.TokenClassificationPipeline
	cfg      Config

	// winTok is the tokenizer used to size inference windows (see
	// windows.go): the pipeline's own pure-Go tokenizer, or a standalone
	// one loaded from the model's tokenizer.json on rust-tokenizer builds.
	// Nil only if neither could be resolved (byte-window fallback).
	winTok api.Tokenizer

	// tokenBudget is the maximum tokens (special tokens included) a single
	// inference row may hold: the smaller of the largest sequence bucket
	// and the model's position limit. Longer texts are split into
	// overlapping windows of at most this many tokens (see windows.go).
	tokenBudget int
	// maxBatch is the inference sub-batch size: the largest batch bucket.
	// ProcessTexts never sends more rows than this in one model call.
	maxBatch int
}

// New loads (downloading first if needed) the configured model and fails
// fast on any model problem, so a constructed Engine can always run. Zero
// fields of cfg fall back to DefaultConfig values.
//
// ctx bounds construction only (model download, session setup); cancelling
// it after New returns does not affect the engine.
func New(ctx context.Context, cfg Config) (*Engine, error) {
	def := DefaultConfig()
	if cfg.Model == "" && cfg.ModelPath == "" {
		cfg.Model = def.Model
	}
	if cfg.LabelMapping == nil {
		cfg.LabelMapping = def.LabelMapping
		if cfg.LabelsToIgnore == nil {
			cfg.LabelsToIgnore = def.LabelsToIgnore
		}
	}
	if cfg.BatchBuckets == nil {
		cfg.BatchBuckets = def.BatchBuckets
	}
	if cfg.SequenceBuckets == nil {
		cfg.SequenceBuckets = def.SequenceBuckets
	}
	sort.Ints(cfg.BatchBuckets)
	sort.Ints(cfg.SequenceBuckets)
	// The buckets feed padding shapes, the inference sub-batch size and the
	// window token budget, all of which must be positive — a non-positive
	// batch bucket would stall ProcessTexts' batching loop outright. The
	// slices are sorted, so checking the first entry covers them all.
	if len(cfg.BatchBuckets) > 0 && cfg.BatchBuckets[0] <= 0 {
		return nil, fmt.Errorf("ner: BatchBuckets entries must be positive, got %v", cfg.BatchBuckets)
	}
	if len(cfg.SequenceBuckets) > 0 && cfg.SequenceBuckets[0] <= 0 {
		return nil, fmt.Errorf("ner: SequenceBuckets entries must be positive, got %v", cfg.SequenceBuckets)
	}
	segmentation, err := normalizeSegmentation(cfg.Segmentation)
	if err != nil {
		return nil, fmt.Errorf("ner: %w", err)
	}
	cfg.Segmentation = segmentation

	modelPath := cfg.ModelPath
	if modelPath == "" {
		modelPath, err = ensureModel(ctx, cfg)
		if err != nil {
			return nil, fmt.Errorf("ner: obtaining model %s: %w", cfg.Model, err)
		}
	} else if cfg.Offline {
		// An explicit ModelPath already downloads nothing, so Offline adds
		// only the check — which is the half that matters here, because this
		// is the baked-image and mounted-volume case and a directory seeded
		// wrong should say so before hugot does.
		if err := checkModelDir(modelPath, cfg.OnnxFilename); err != nil {
			return nil, fmt.Errorf("ner: offline: %w", err)
		}
	}

	// hugot's session keeps the ctx it was created with and uses it for
	// inference, so it must not inherit the caller's cancellation either —
	// only the model download above is bound to the construction ctx.
	runCtx := context.WithoutCancel(ctx)
	session, err := newBackendSession(runCtx, cfg)
	if err != nil {
		return nil, fmt.Errorf("ner: creating hugot session: %w", err)
	}

	pipeline, err := hugot.NewPipeline(session, hugot.TokenClassificationConfig{
		ModelPath:    modelPath,
		Name:         "alcatraz-ner",
		OnnxFilename: cfg.OnnxFilename,
		Options: []hugot.TokenClassificationOption{
			pipelines.WithSimpleAggregation(),
		},
	})
	if err != nil {
		// The session owns no other pipelines; release it.
		_ = session.Destroy()
		return nil, fmt.Errorf("ner: creating token classification pipeline: %w", err)
	}

	tokenBudget := 0
	if len(cfg.SequenceBuckets) > 0 {
		tokenBudget = cfg.SequenceBuckets[len(cfg.SequenceBuckets)-1]
	}
	if m := pipeline.Model.MaxPositionEmbeddings; m > 0 && (tokenBudget == 0 || m < tokenBudget) {
		tokenBudget = m
	}
	if tokenBudget == 0 {
		// No bucket and no model limit declared: BERT-family default.
		tokenBudget = 512
	}
	maxBatch := 32
	if len(cfg.BatchBuckets) > 0 {
		maxBatch = cfg.BatchBuckets[len(cfg.BatchBuckets)-1]
	}

	return &Engine{
		runCtx:      runCtx,
		session:     session,
		pipeline:    pipeline,
		cfg:         cfg,
		winTok:      windowTokenizer(pipeline, modelPath),
		tokenBudget: tokenBudget,
		maxBatch:    maxBatch,
	}, nil
}

// ensureModel returns the local path of cfg.Model, downloading it from
// Hugging Face into the models directory on first use — unless cfg.Offline
// forbids that, in which case it only reports what is already there.
func ensureModel(ctx context.Context, cfg Config) (string, error) {
	if cfg.Offline {
		return offlineModel(cfg)
	}
	dir, err := models.ResolveDir(cfg.ModelsDir)
	if err != nil {
		return "", err
	}
	// Pinned models take the verified path: every file is checked against
	// its sha256, so a cache hit is a hit only if the bytes still match
	// and what New loads is what we pinned, not merely what the hub last
	// served (see the models package).
	if models.IsPinned(cfg.Model) {
		return models.EnsureModelIn(ctx, cfg.Model, dir)
	}
	// Any other model id keeps working, unverified: hugot's cache check is
	// presence-only, and so is ours.
	modelPath := models.Dir(dir, cfg.Model)
	if _, err := os.Stat(filepath.Join(modelPath, "tokenizer.json")); err == nil {
		return modelPath, nil
	}
	return hugot.DownloadModel(ctx, cfg.Model, dir, hugot.NewDownloadOptions())
}

// Close releases the model and its runtime resources.
func (e *Engine) Close() error {
	return e.session.Destroy()
}

// Config returns the engine's effective configuration.
func (e *Engine) Config() Config { return e.cfg }

// ProcessText implements analyzer.NlpEngine: it runs the model over text and
// returns the detected entity spans mapped to canonical entity names with
// byte offsets. The language parameter is currently unused (one model per
// engine); it is part of the interface for engines that route per language.
//
// The model sees an ASCII-folded rendering of the text (see foldASCII) and
// the reported spans are mapped back to byte offsets in the original text,
// so the alcatraz invariant text[Start:End] == matched span holds for any
// input.
func (e *Engine) ProcessText(text, language string) (*analyzer.NlpArtifacts, error) {
	all, err := e.ProcessTexts([]string{text}, language)
	if err != nil {
		return nil, err
	}
	return all[0], nil
}

// inferenceRow is one call to the model: one window of one segment of one
// text, with the offset needed to rebase its spans onto the folded text.
type inferenceRow struct {
	textIdx int
	offset  int // window start, in folded-text bytes
	body    string
}

// inferenceRows expands texts into the rows the model will see: each folded
// text is cut into segments (the caller's Config.Segmentation rule), layout-
// only segments are dropped, and each surviving segment is windowed to the
// token budget. A short unsegmented text — the common case — yields exactly
// one row covering the whole text.
//
// The second result records, per text, that some segment needed more than one
// window. That is the only way one text can report the same entity twice, so
// it is the only case that needs mergeSpans: segments are disjoint, so their
// spans never overlap.
//
// folded and foldOffsets come from foldASCII over texts, positionally.
func (e *Engine) inferenceRows(texts, folded []string, foldOffsets [][]int) ([]inferenceRow, []bool) {
	var rows []inferenceRow
	windowed := make([]bool, len(texts))
	for i := range texts {
		for _, seg := range e.segments(folded[i]) {
			if !hasWordRune(segmentText(texts[i], foldOffsets[i], seg)) {
				continue
			}
			body := folded[i][seg.start:seg.end]
			wins := e.windows(body)
			if len(wins) > 1 {
				windowed[i] = true
			}
			for _, w := range wins {
				rows = append(rows, inferenceRow{i, seg.start + w.start, body[w.start:w.end]})
			}
		}
	}
	return rows, windowed
}

// ProcessTexts implements analyzer.BatchNlpEngine: it runs the model over all
// texts in batched inference calls and returns one NlpArtifacts per text, in
// input order. Batching amortizes the per-call tokenization and graph
// overhead, so it is substantially faster than calling ProcessText in a loop;
// the spans of each text carry the same byte-offset guarantee as ProcessText.
//
// Each text is first split into segments — one by default, per line or per
// field when Config.Segmentation asks for it — and each segment that exceeds
// the model's token limit is split further into overlapping windows (see
// windows.go), whose spans are merged. Entities anywhere in a text of any
// length are detected; nothing is truncated away under any setting.
//
// Spans are reported on word boundaries: the model tags subword tokens and
// can hand back half of one, so every span is grown to the word it sits
// inside and same-type spans that then touch are unioned (see words.go).
func (e *Engine) ProcessTexts(texts []string, language string) ([]*analyzer.NlpArtifacts, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	folded := make([]string, len(texts))
	foldOffsets := make([][]int, len(texts))
	for i, text := range texts {
		folded[i], foldOffsets[i] = foldASCII(text)
	}

	rows, windowed := e.inferenceRows(texts, folded, foldOffsets)

	artifacts := make([]*analyzer.NlpArtifacts, len(texts))
	for i := range texts {
		artifacts[i] = &analyzer.NlpArtifacts{}
	}

	for c := 0; c < len(rows); c += e.maxBatch {
		chunk := rows[c:min(c+e.maxBatch, len(rows))]
		bodies := make([]string, len(chunk))
		for j, r := range chunk {
			bodies[j] = r.body
		}
		out, err := e.pipeline.RunPipeline(e.runCtx, bodies)
		if err != nil {
			return nil, err
		}
		for j, ents := range out.Entities {
			if j >= len(chunk) {
				break
			}
			row := chunk[j]
			for _, ent := range ents {
				// Row-relative offsets → folded-text offsets; toNerSpan
				// then remaps folded → original text.
				ent.Start += uint(row.offset)
				ent.End += uint(row.offset)
				span, ok := e.toNerSpan(texts[row.textIdx], foldOffsets[row.textIdx], ent)
				if !ok {
					continue
				}
				artifacts[row.textIdx].Ents = append(artifacts[row.textIdx].Ents, span)
			}
		}
	}

	// Overlapping windows can report the same entity twice; texts whose
	// segments each fit one window need no merge. Word snapping applies to
	// every text: the model splits words into subword tokens and can tag only
	// part of one.
	for i, a := range artifacts {
		if windowed[i] {
			a.Ents = mergeSpans(a.Ents)
		}
		a.Ents = snapToWords(texts[i], a.Ents)
	}
	return artifacts, nil
}

// toNerSpan converts one hugot entity into a NerSpan: label mapping, ignore
// list, score calibration, fold-offset remapping and byte-span validation.
// ok is false when the entity is dropped.
func (e *Engine) toNerSpan(text string, foldOffsets []int, ent pipelines.Entity) (analyzer.NerSpan, bool) {
	label := ent.Entity
	if e.ignored(label) {
		return analyzer.NerSpan{}, false
	}
	entity, ok := e.cfg.LabelMapping[label]
	if !ok {
		// Keep unmapped labels as-is (Presidio's behavior); users silence
		// them via LabelsToIgnore.
		entity = label
	}
	if e.ignored(entity) {
		return analyzer.NerSpan{}, false
	}

	score := float64(ent.Score)
	if e.cfg.LowScoreMultiplier > 0 {
		for _, low := range e.cfg.LowScoreEntities {
			if low == entity {
				score *= e.cfg.LowScoreMultiplier
				break
			}
		}
	}
	if score > analyzer.MaxScore {
		score = analyzer.MaxScore
	}
	if score <= analyzer.MinScore {
		return analyzer.NerSpan{}, false
	}

	start, end := remapSpan(foldOffsets, len(text), int(ent.Start), int(ent.End))
	start, end, ok = byteSpan(text, start, end)
	if !ok {
		return analyzer.NerSpan{}, false
	}
	return analyzer.NerSpan{EntityType: entity, Start: start, End: end, Score: score}, true
}

func (e *Engine) ignored(label string) bool {
	for _, l := range e.cfg.LabelsToIgnore {
		if l == label {
			return true
		}
	}
	return false
}

// Recognizer returns an analyzer.Recognizer emitting this engine's entities
// for the given language. Register it in a Registry alongside the pattern
// recognizers; pair it with Engine.SetNlpEngine so the model runs once per
// Analyze call.
func (e *Engine) Recognizer(language string) *Recognizer {
	return &Recognizer{
		engine:   e,
		language: language,
	}
}

// Recognizer adapts an Engine to the analyzer.Recognizer contract. It is the
// thin consumer of NlpArtifacts: when the analyzer.Engine provides shared
// artifacts the model has already run and AnalyzeWithArtifacts just converts
// spans; the plain Analyze path runs the model itself so the recognizer also
// works in an engine without SetNlpEngine.
type Recognizer struct {
	engine   *Engine
	language string
}

// Name implements analyzer.Recognizer.
func (r *Recognizer) Name() string { return "NERecognizer" }

// SupportedEntities implements analyzer.Recognizer.
func (r *Recognizer) SupportedEntities() []string { return r.engine.cfg.SupportedEntities() }

// SupportedLanguage implements analyzer.Recognizer.
func (r *Recognizer) SupportedLanguage() string { return r.language }

// Analyze implements analyzer.Recognizer by running the model directly. An
// inference error yields no results, matching the engine's degrade-to-
// patterns behavior.
func (r *Recognizer) Analyze(text string, entities []string) []analyzer.Result {
	if entities != nil && !supportsAny(r.SupportedEntities(), entities) {
		return nil
	}
	artifacts, err := r.engine.ProcessText(text, r.language)
	if err != nil {
		return nil
	}
	return r.AnalyzeWithArtifacts(text, entities, artifacts)
}

// AnalyzeWithArtifacts implements analyzer.ArtifactRecognizer.
func (r *Recognizer) AnalyzeWithArtifacts(text string, entities []string, artifacts *analyzer.NlpArtifacts) []analyzer.Result {
	var results []analyzer.Result
	for _, ent := range artifacts.Ents {
		if entities != nil && !contains(entities, ent.EntityType) {
			continue
		}
		results = append(results, analyzer.Result{
			EntityType:     ent.EntityType,
			Start:          ent.Start,
			End:            ent.End,
			Score:          ent.Score,
			RecognizerName: r.Name(),
		})
	}
	return results
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func supportsAny(have, want []string) bool {
	for _, w := range want {
		if contains(have, w) {
			return true
		}
	}
	return false
}
