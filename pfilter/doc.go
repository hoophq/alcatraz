// Package pfilter provides a statistical PII backend for alcatraz on top of
// privacy-filter.cpp (https://github.com/localai-org/privacy-filter.cpp),
// the GGML runtime for the openai-privacy-filter NER model family. The model
// labels PII spans (person, address, email, phone, date, url, account
// number, secret; 54 categories with the multilingual fine-tune) with exact
// UTF-8 byte offsets, in-process, on CPU or GPU.
//
// [Engine] implements [analyzer.NlpEngine], so it is the second NLP backend
// alcatraz ships (alongside alcatraz/ner) and drops into the same
// SetNlpEngine slot.
//
// It lives in a separate module on purpose (mirroring alcatraz/lookaround
// and alcatraz/ner): the core stays dependency-free. The binding is FFI via
// purego with no cgo, so this module cross-compiles like plain Go; at
// runtime it needs the privacy-filter.cpp shared library (libpf) and a GGUF
// model file.
//
// # Getting the artifacts
//
// Both can be fetched (and sha256-verified, on download and on every cache
// hit) into the user cache dir (~/.cache/alcatraz on Linux, the platform
// equivalent elsewhere):
//
//	lib, err := pfilter.EnsureLibrary(ctx)   // prebuilt libpf for this OS/arch
//	model, err := pfilter.EnsureModel(ctx, pfilter.ModelQ8)
//	nlp, err := pfilter.New(pfilter.DefaultConfig(model))
//
// [EnsureLibrary] pulls the prebuilt libpf published as an alcatraz GitHub
// release asset; platforms without one get an error pointing at the
// build-from-source path. [EnsureModel] pulls one of the Model* GGUF
// variants (1.6–2.8 GB) from Hugging Face; pass a cancellable ctx.
//
// Or build the library from source (pfilter/dist has a CMake wrapper
// producing one self-contained shared library):
//
//	git clone --recursive https://github.com/localai-org/privacy-filter.cpp
//	cmake -S pfilter/dist -B build -DPF_SOURCE_DIR=$PWD/privacy-filter.cpp \
//	      -DCMAKE_BUILD_TYPE=Release && cmake --build build -j
//	# then point Config.Library (or $PF_LIBRARY) at build/libpf.*, and
//	# Config.ModelPath at a GGUF from LocalAI-io/privacy-filter-GGUF.
//
// [New] resolves the library in this order: [Config].Library if set, then
// $PF_LIBRARY, then the [EnsureLibrary] cache path, then the system loader's
// default search paths for "libpf". A cached library therefore needs no
// configuration at all.
//
// # Labels
//
// [DefaultConfig] maps the base model's eight labels onto the canonical
// entity names the pattern recognizers use ("private_person" →
// [entities.Person], "private_address" → [entities.Location], and so on), so
// model and pattern results de-duplicate against each other. Labels outside
// the mapping are kept, normalized to SCREAMING_SNAKE_CASE ("crypto_wallet" →
// "CRYPTO_WALLET"), which is how the multilingual model's 54 categories come
// through. Config.LabelsToIgnore drops spans by model label or by mapped
// entity name.
//
// # Wiring it in
//
// An [analyzer.Engine] configured with SetNlpEngine runs the model once per
// Analyze call and shares the artifacts with every artifact-aware
// recognizer:
//
//	nlp, err := pfilter.New(pfilter.DefaultConfig("privacy-filter-f16.gguf"))
//	// handle err
//	defer nlp.Close()
//
//	reg := analyzer.NewRegistry("en")
//	recognizers.LoadDefaults(reg, "en")
//	reg.Add("en", nlp.Recognizer("en"))
//
//	eng := analyzer.NewEngine(reg, []string{"en"})
//	eng.SetNlpEngine(nlp)
//
// # Platform support
//
// The purego binding is built only for 64-bit darwin and linux, because the
// pf_entity struct mirror hardcodes the 64-bit C layout. Everything else
// compiles against a stub, so importers can gate on the runtime error from
// [New] rather than on build tags.
package pfilter
