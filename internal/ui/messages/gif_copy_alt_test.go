package messages

import (
	"testing"

	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// Copy Image on the GIF's still is for its <img>: the proxy's still for
// src, the image block's alt_text for alt (the copy's markup and
// bookmark: clipimg), drawn from the GIF itself (gif_copy_test.go has the
// menus).
func TestGIFPickerCopyImageIsTheImg(t *testing.T) {
	uipref.Set(store.Prefs{NoAnimations: true})
	t.Cleanup(uipref.Reset)
	e := gifPickEnv(t)
	m := e.gifPickStillMenu()
	im, ok := m.Items()[0].CopiesImage()
	if !ok || im.Src != gifPickStill || im.Alt != gifPickAlt || im.Shown != gifPickURL {
		t.Errorf("Copy Image is for %+v (%v), want src %q, alt %q, drawn from %q", im, ok, gifPickStill, gifPickAlt, gifPickURL)
	}
}
