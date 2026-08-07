# pfilter: the privacy-filter.cpp backend

The **`alcatraz/pfilter`** module binds
[privacy-filter.cpp](https://github.com/localai-org/privacy-filter.cpp) (the
GGML runtime for the `openai-privacy-filter` PII model family) as a second
`analyzer.NlpEngine` implementation. Compared to the [`ner`](ner.md) module it
trades setup effort for:

- a **PII-specialized model** (8 categories in the base model, 54 across 16
  languages in the multilingual fine-tune, vs. generic person/location NER),
- **long-document support** (near-linear banded attention; 131k-token inputs
  with halo windowing), and
- **GPU inference** (CUDA/Vulkan).

The binding is FFI via [purego](https://github.com/ebitengine/purego), so the
module needs no cgo and cross-compiles like plain Go. At runtime it needs the
`libpf` shared library and a GGUF model file. Neither requires a manual
build: `EnsureLibrary` downloads a prebuilt, sha256-pinned `libpf` for your
platform, and `EnsureModel` downloads a GGUF (pre-converted:
[LocalAI-io/privacy-filter-GGUF](https://huggingface.co/LocalAI-io/privacy-filter-GGUF))
verified against its published checksum. Both cache under the user cache dir.

```go
import "github.com/hoophq/alcatraz/pfilter"

// One-time setup, no cmake, no clone: fetch libpf + a model (verified).
if _, err := pfilter.EnsureLibrary(ctx); err != nil { ... }
model, err := pfilter.EnsureModel(ctx, pfilter.ModelQ8) // ~1.6 GB, cached
if err != nil { ... }

// Library resolution: Config.Library, else $PF_LIBRARY, else the
// EnsureLibrary cache, else system paths.
nlp, err := pfilter.New(pfilter.DefaultConfig(model))
if err != nil { ... }
defer nlp.Close()

reg.Add("en", nlp.Recognizer("en"))
eng.SetNlpEngine(nlp) // same seam, same one-pass sharing as the ner module
```

## Building `libpf` from source

For CUDA/Vulkan, `pfilter/dist` has a CMake wrapper that produces one
self-contained shared library from a privacy-filter.cpp checkout:

```bash
git clone --recursive https://github.com/localai-org/privacy-filter.cpp
cmake -S pfilter/dist -B build -DPF_SOURCE_DIR=$PWD/privacy-filter.cpp \
      -DCMAKE_BUILD_TYPE=Release && cmake --build build -j
# -> build/libpf.dylib (macOS) / build/libpf.so (Linux); point $PF_LIBRARY at it
```

## Label mapping

Default mapping: `private_person`→`PERSON`, `private_address`→`LOCATION`,
`private_email`→`EMAIL_ADDRESS`, `private_phone`→`PHONE_NUMBER`,
`private_date`→`DATE_TIME`, `private_url`→`URL`, plus `ACCOUNT_NUMBER` and
`SECRET`. Because the model shares entity names with the pattern recognizers,
overlapping detections (e.g. an email found by both) collapse in the engine's
same-type dedup. Unmapped labels from the multilingual model surface as
SCREAMING_SNAKE_CASE of the model label; drop them via `Config.LabelsToIgnore`.
