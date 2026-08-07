# Running NER without internet access

By default `ner.New` downloads the model on first use: ~250 MiB from
huggingface.co, pulled at the worst possible moment, the first request that
needs a `PERSON`. In a restricted or air-gapped network that request fails
outright. Fetch the model as a build or deploy step instead:

```bash
alcatraz models download                      # warm the cache ner.New reads from
alcatraz models download --dest /opt/alcatraz/models   # or a self-contained directory
```

```
KnightsAnalytics/distilbert-NER @ 13a742d5
fetching 6 files, 249.7 MiB total (cached files are verified, not re-fetched):
  config.json                   925 B
  model.onnx                248.8 MiB
  ...
verified:
  config.json              8f9f01d47f61087197f9fa85185d4a7a6248333c15af1b221aa5e8b9b76462b5
  model.onnx               4440f9fc64cd28ac75d83a38d89716f25947799640cd0e5f1f9f6e57b9c14160
  ...

ModelsDir: /opt/alcatraz/models
ModelPath: /opt/alcatraz/models/KnightsAnalytics_distilbert-NER
```

Every file is pinned to a commit revision and checked against a known sha256
and byte size; a mismatch fails the command with a non-zero exit, and the
bytes that failed are never installed. The command keeps files it verified
earlier in the same run, re-verifying them instead of re-fetching, which keeps
a re-run cheap and offline. It lives in the **root** module, so the plain
`alcatraz` binary (Homebrew, `go install`, or a release download) can fetch
the model without the ONNX runtime being anywhere near it.

## Bake it into an image

No runtime network at all, at the cost of ~250 MiB of image:

```dockerfile
# Fetch and verify the model in a build stage.
FROM golang:1.24 AS models
RUN go install github.com/hoophq/alcatraz/cmd/alcatraz@latest
RUN alcatraz models download --dest /opt/alcatraz/models

FROM your-app-base
COPY --from=models /opt/alcatraz/models /opt/alcatraz/models
```

## Or prime a shared volume

An init container fetches once, the app container mounts the result:

```yaml
spec:
  volumes:
    - name: alcatraz-models
      # emptyDir re-fetches per pod; a ReadWriteMany PVC fetches once for the
      # cluster and every later start is a local re-verify.
      persistentVolumeClaim:
        claimName: alcatraz-models
  initContainers:
    - name: fetch-ner-model
      image: your-registry/alcatraz:latest # any image carrying the alcatraz binary
      # Use command:, not args:. args replaces the image's CMD but inherits
      # its ENTRYPOINT, so it works only when that entrypoint is alcatraz.
      # On an image that merely carries the binary, args makes Kubernetes try
      # to exec "models", and the init container crash-loops.
      command: ["alcatraz", "models", "download", "--dest", "/models"]
      volumeMounts:
        - { name: alcatraz-models, mountPath: /models }
  containers:
    - name: app
      volumeMounts:
        - { name: alcatraz-models, mountPath: /models }
```

## Wiring the result into the config

The command prints two paths one directory apart, and they are not
interchangeable. Picking the wrong one is the usual way this gets
misconfigured:

| Printed path | Config field | Downloads on load | Verifies on load |
|--------------|--------------|-------------------|------------------|
| `ModelsDir:`, the parent holding every model | `ner.Config.ModelsDir` | only files missing or failing verification | yes, every pinned file |
| `ModelPath:`, this model's own directory | `ner.Config.ModelPath` | never | no; the directory is trusted as-is |

`ModelsDir` is the better default for a mounted volume: it needs no network
when the files are intact, and it still catches a truncated or tampered-with
model instead of loading it. `ModelPath` is the escape hatch for a directory
alcatraz did not produce.

```go
cfg := ner.DefaultConfig()
cfg.ModelsDir = "/opt/alcatraz/models" // the ModelsDir: line, not ModelPath:
nlp, err := ner.New(ctx, cfg)
```

## Turn it into a guarantee

Everything above arranges for the cache to be warm; `ner.Config.Offline` makes
`ner.New` fail rather than fall back if it isn't:

```go
cfg := ner.DefaultConfig()
cfg.ModelsDir = "/opt/alcatraz/models"
cfg.Offline = true // New opens no socket; a bad model directory is an error
```

Falling back to a download is not a graceful degradation in a locked-down
deployment. The attempt trips egress monitoring even when it fails, and the
error that comes back is a DNS or TLS timeout that says nothing about the
model. With `Offline` set the failure names the model instead:

```
ner: obtaining model KnightsAnalytics/distilbert-NER: offline: models: model.onnx
in /opt/alcatraz/models/KnightsAnalytics_distilbert-NER does not match its pinned
sha256; pre-download it with: alcatraz models download --dest "/opt/alcatraz/models"
```

The module reports missing, mismatched and unreadable as three different
failures on purpose: absent means the volume was never seeded, mismatched
means it was seeded with the wrong bytes (a stale image layer, a truncated
copy), unreadable means the bytes may be fine and the container's user cannot
open them, and the fixes have nothing in common.

Because an offline caller has no fallback, `Offline` also tightens what counts
as loadable, and it is the one thing that makes `ModelPath` checked at all:

| | `Offline` unset | `Offline` set |
|---|---|---|
| `ModelsDir`, pinned model | downloads what is missing or fails verification | verifies every pinned sha256, never downloads |
| `ModelsDir`, unpinned model | downloads through hugot | requires `config.json`, `tokenizer.json` and an `.onnx` file |
| `ModelPath` | trusted as-is | same three-file check before hugot sees it |

Nothing on the offline path writes, including the directory lookup and the
verification, so a model mounted read-only (`readOnly: true` on the volume
mount above, or a `COPY`'d image layer) loads unchanged.

From Go, the same fetch is [`ner.EnsureModelIn`](https://pkg.go.dev/github.com/hoophq/alcatraz/ner#EnsureModelIn)
(or `ner.EnsureModel` for the default cache), which returns the `ModelPath`
form. Neither `ner` nor `models` reads environment variables; the path is
always a config field, so a host application decides how to surface it as a
deployment knob. Hoop's agent, for instance, maps `ALCATRAZ_NER_MODEL_PATH`
onto `Config.ModelPath`.
