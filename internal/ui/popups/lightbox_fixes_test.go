package popups

import (
	"testing"

	"gioui.org/io/event"
	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/richtext"
)

// ← and → on a slider inside the viewer (the Details tab's inline video:
// its scrubber or volume) are the slider's: use_key_commands skips a
// role="slider" target. The viewer moved to another file instead.
func TestMediaViewerSliderKeepsArrows(t *testing.T) {
	e := newMVDEnv(t, "treatment", mvdGallery)
	e.openDetails()
	p := e.h.panes[0]
	old := isSliderTag
	isSliderTag = func(tag event.Tag) bool { return tag == event.Tag(&p.btn) } // the pane's control stands in for a slider
	t.Cleanup(func() { isSliderTag = old })
	e.focus(&p.btn)
	if !e.r.Source().Focused(&p.btn) {
		t.Fatal("the pane's control lost the focus: the test proves nothing")
	}
	e.press(key.NameRightArrow)
	e.press(key.NameLeftArrow)
	if idx, _ := e.lb.Position(); idx != 0 {
		t.Errorf("← → on a focused slider moved the viewer to item %d", idx)
	}
}

// A BMP file opened outside a message (search, the Files browser, a file
// card) shows its original, as before the viewer carried the file:
// imgcache decodes BMP (decmem.go).
func TestViewerShowsBMPOriginal(t *testing.T) {
	for _, mime := range []string{"image/bmp", "image/x-ms-bmp"} {
		f := &richtext.File{ID: "F0BMP00001", Name: "scan.bmp", Mime: mime, IsImage: true, URL: "https://files.example.com/scan.bmp",
			Thumb: &richtext.Image{URL: "https://files.example.com/scan_360.png", W: 360, H: 240}}
		it := lbItem{img: *f.Thumb, file: f}
		if got := it.displayURL(); got != f.URL {
			t.Errorf("%s: the viewer shows %q, want the original %q", mime, got, f.URL)
		}
	}
}
