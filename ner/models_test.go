package ner

import (
	"testing"

	"github.com/hoophq/alcatraz/models"
)

// TestDefaultModelIsPinned guards the ner default specifically: the pin table
// lives in another package now, so nothing but this stops DefaultConfig from
// drifting onto a model New would download unverified.
func TestDefaultModelIsPinned(t *testing.T) {
	if model := DefaultConfig().Model; !models.IsPinned(model) {
		t.Errorf("default model %q is not pinned, so New downloads it unverified", model)
	}
}
