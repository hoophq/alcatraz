# Inference backends: ORT, XLA and GPU

The pure-Go backend is the zero-friction default. For more speed,
`ner.Config.Backend` selects one of hugot's faster backends, and
`ner.Config.Accelerator` adds a GPU execution provider on top:

| `Config.Backend` | Build tags | Runtime dependency | Accelerators |
|------------------|-----------|--------------------|--------------|
| `"go"` (default) | none; pure Go, cross-compiles | none | none |
| `"ort"` | `-tags ORT` (cgo + [libtokenizers.a](https://github.com/daulet/tokenizers/releases) at link time) | `libonnxruntime.{so,dylib}` ([releases](https://github.com/microsoft/onnxruntime/releases)) | `coreml` (Apple GPU/ANE), `cuda`, `directml` |
| `"xla"` | `-tags XLA` (cgo) | PJRT plugin | `cuda` |

```go
cfg := ner.DefaultConfig()
cfg.Backend = ner.BackendORT            // needs a -tags ORT build
cfg.Accelerator = ner.AcceleratorCoreML // optional: Apple GPU/Neural Engine
nlp, err := ner.New(ctx, cfg)
```

```bash
# macOS: brew install onnxruntime, and the loader finds it with no config.
# Otherwise point Config.ORTLibraryPath at the library (file or directory).
CGO_LDFLAGS="-L/path/to/libtokenizers" go build -tags ORT .
```

The whole pipeline (windowing, batching, span merging) behaves the same on
every backend, and a backend that is not compiled in fails `ner.New` with an
error naming the missing build tag rather than degrading without an error.
As a rough figure, ORT on CPU is ~5–10x faster than the pure-Go backend on
batch workloads; CoreML/CUDA go beyond that.

To compare on your hardware:

```bash
cd ner && ALCATRAZ_NER_LIVE=1 go test -bench LiveProcessTexts -benchtime 1x -run xxx .
# and the same with ALCATRAZ_NER_BACKEND=ort under a -tags ORT build
```
