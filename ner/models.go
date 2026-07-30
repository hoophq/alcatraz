package ner

import (
	"context"

	"github.com/hoophq/alcatraz/models"
)

// The pin table and the verified downloader live in the root module's
// [models] package, not here, so the alcatraz CLI can ship
// "alcatraz models download" without pulling in the model runtime or this
// module's newer Go requirement. These forwarders keep ner the single import
// for NER users.

// EnsureModel downloads model into the models directory New reads from and
// returns the local directory holding it, so a warm cache makes New
// network-free and the returned path can be handed straight to
// Config.ModelPath. It is [models.EnsureModel].
func EnsureModel(ctx context.Context, model string) (string, error) {
	return models.EnsureModel(ctx, model)
}

// EnsureModelIn is EnsureModel writing into an explicit models directory,
// for callers materialising a model outside the user cache — a Docker image
// layer or a shared volume. An empty dir means the default. It is
// [models.EnsureModelIn].
//
// dir is the models directory, not the model directory: the model lands in a
// subdirectory named after it, so dir is what Config.ModelsDir wants and the
// returned path is what Config.ModelPath wants.
func EnsureModelIn(ctx context.Context, model, dir string) (string, error) {
	return models.EnsureModelIn(ctx, model, dir)
}

// VerifyModelIn checks an already-seeded model directory against the pinned
// digests without fetching anything, and returns it. It is what Config.Offline
// runs, exported here for a caller that wants to fail at startup — or in a
// container health check — rather than at first load. It is
// [models.VerifyModelIn].
//
// dir is the models directory, as in EnsureModelIn. An empty dir means the
// default, which unlike EnsureModelIn is not created.
func VerifyModelIn(model, dir string) (string, error) {
	return models.VerifyModelIn(model, dir)
}

// DefaultDir returns the models directory New reads from when Config.ModelsDir
// is empty, without creating it. It is [models.DefaultDir].
func DefaultDir() (string, error) {
	return models.DefaultDir()
}

// PinnedModels returns, sorted, the model ids EnsureModel can fetch and
// verify. Other model ids still work through Config.Model, but nothing
// checks what the hub serves for them. It is [models.PinnedModels].
func PinnedModels() []string {
	return models.PinnedModels()
}
