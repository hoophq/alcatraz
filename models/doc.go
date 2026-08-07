// Package models materialises the model files alcatraz's optional NER
// backends need, pinned to an immutable revision and verified against
// recorded sha256 digests and byte lengths.
//
// # The pin
//
// A model here is not "whatever the hub serves under that name today". Each
// entry in the pin table records a repository, the commit sha of one
// revision, and for every file alcatraz downloads from it the exact sha256
// and byte length. [PinnedModels] lists the ids covered, [IsPinned] answers
// for one, [Revision] gives the commit, and [PinnedFiles] enumerates the
// files with their digests and sizes, enough to show what a download will
// cost before starting it. Unpinned ids still work through ner.Config.Model;
// nothing checks what arrives for them.
//
// Verification runs on every call, cache hits included, because only the
// digest proves the bytes on disk are the file alcatraz asked for.
// [EnsureModel] and [EnsureModelIn] re-fetch a file whose bytes do not
// match; [VerifyModelIn] only reports, distinguishing absent from
// mismatched from unreadable. Downloads are atomic (temp file in the
// destination directory, then rename), so a concurrent reader never
// observes a partial file, and the functions are safe to call from several
// goroutines and several processes at once.
//
// # Directories
//
// Two different paths, consistently named. The models directory is the parent
// holding every model, one subdirectory per model id ("/" replaced by "_", as
// hugot names them); it is what [ResolveDir], [DefaultDir] and the dir
// argument of [EnsureModelIn] and [VerifyModelIn] mean, and what
// ner.Config.ModelsDir wants. The model directory is one model's own
// subdirectory inside it, holding model.onnx and the tokenizer files; it is
// what [Dir] computes, what [EnsureModel] and friends return, and what
// ner.Config.ModelPath wants. Passing one where the other belongs is the
// mistake the split naming exists to prevent.
//
// The default models directory is "alcatraz/models" under the user cache dir.
// [ResolveDir] creates it, [DefaultDir] only names it: looking is not a
// reason to write.
//
// # Why here
//
// It lives in the root module rather than in ner on purpose. The alcatraz
// CLI ships "alcatraz models download" so a model can be fetched as a build
// or deploy step, and that command must not drag the ONNX model runtime,
// nor its newer Go requirement, into a module that is otherwise standard
// library only. Nothing here imports anything outside the standard library:
// net/http and crypto/sha256 are the whole machinery, so the plain CLI, a
// Dockerfile build stage or a provisioning job can populate a shared volume
// with no ONNX runtime linked in and no cgo.
//
// The ner package re-exports EnsureModel, EnsureModelIn, VerifyModelIn,
// DefaultDir and PinnedModels, so NER users still have a single import.
package models
