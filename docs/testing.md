# Tests & CI

```bash
go test ./...                    # core (incl. the models pin table, no network)
ALCATRAZ_NER_LIVE=1 go test -run TestLiveEnsureModel ./models/   # + real download
cd lookaround && go test ./...   # lookaround module
cd ner && go test ./...          # ner module (unit tests, no model needed)
cd ner && ALCATRAZ_NER_LIVE=1 go test ./...   # + end-to-end (downloads model)
cd pfilter && go test ./...      # pfilter module (unit tests, no lib needed)
cd pfilter && ALCATRAZ_PF_LIVE=1 PF_LIBRARY=/path/libpf.dylib \
  PF_MODEL=/path/privacy-filter-q8.gguf go test ./...   # + end-to-end
```

CI runs the unit tests of all four modules on every push (`test.yml`). The
live end-to-end model tests run in `ml-live.yml` on PRs touching the ML
modules, weekly, and on demand, with the built `libpf` and the GGUF cached
between runs. The manual `libpf-release.yml` workflow produces the prebuilt
`libpf` binaries and publishes them as `libpf-vN` GitHub releases, which is
what `pfilter.EnsureLibrary` downloads.

## Documentation

Package-level API docs are Go doc comments;
[pkg.go.dev](https://pkg.go.dev/github.com/hoophq/alcatraz) renders them
straight from the source. Usage snippets are runnable `Example` functions in
`example_test.go`, so `go test` fails if an example stops compiling or its
output drifts.

This site is MkDocs Material over the `docs/` directory. Preview it locally:

```bash
pip install -r docs/requirements.txt
mkdocs serve            # http://127.0.0.1:8000
```

`docs.yml` builds with `--strict` on every push touching `docs/`, so a broken
internal link fails CI, and deploys `main` to GitHub Pages.
