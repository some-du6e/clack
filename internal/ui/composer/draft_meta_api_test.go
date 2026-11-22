package composer

import (
	"image"
	"image/color"
	"time"

	"github.com/some-du6e/clack/internal/ui/icons"
)

// The draft file tests' reads of what the change adds (a red run replaces
// this file with stubs).

// attChipState is what an attachment card draws.
type attChipState struct {
	name, title, label string
	loading, gone      bool
}

func chipState(c *attChip) attChipState {
	return attChipState{name: c.name, title: c.title, label: c.kind.Label, loading: c.loading, gone: c.gone}
}

// hashedFileID is the id the log names a file by.
func hashedFileID(id string) string { return attLogID(id) }

// shortMetaTimeout makes each files.info attempt time out after d.
func shortMetaTimeout(d time.Duration) func() {
	old, oldB := attMetaTimeout, attMetaBackoff
	attMetaTimeout, attMetaBackoff = d, 10*time.Millisecond
	return func() { attMetaTimeout, attMetaBackoff = old, oldB }
}

// recordDraws sends what the composer draws to box (each rounded box it
// fills, and its colour) and icon (each icon's name) until stop.
func recordDraws(box func(r image.Rectangle, col color.NRGBA), icon func(name string)) (stop func()) {
	oldF, oldI := fillHook, icons.DrawHook
	fillHook = func(r image.Rectangle, _ int, col color.NRGBA) { box(r, col) }
	icons.DrawHook = icon
	return func() { fillHook, icons.DrawHook = oldF, oldI }
}
