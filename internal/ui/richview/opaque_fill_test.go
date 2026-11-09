package richview

import (
	"image"
	"image/color"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme/pixeltest"
)

// A message list whose last message is pending (Options.Dim: drawn in an
// opacity layer), then in the same frame a full-window opaque fill (a
// modal backdrop a theme makes opaque, the lightbox panel in a small
// window) and a dialog card on it. Gio v0.10.3 kept the dimmed body's
// opacity layer after the opaque fill discarded its ops, and its renderer
// panicked indexing past them ("index out of range [609] with length 135",
// alpha.18 on macOS). third_party/gioui.org/CLACK_PATCHES.md has the fix.
func TestDimmedViewUnderOpaqueFill(t *testing.T) {
	st := NewStyle(themeShaper(), noImages{}, LightPalette())
	const w, h = 420, 160
	var ops op.Ops
	paint.FillShape(&ops, st.Palette.Panel, clip.Rect{Max: image.Pt(w, h)}.Op())
	sent := layoutAt(st, codeDoc(t), Options{}, w)
	sent.Layout(testGtx(&ops, w), st, &recHandler{})
	pending := layoutAt(st, codeDoc(t), Options{Dim: true}, w)
	off := op.Offset(image.Pt(0, 40)).Push(&ops)
	pending.Layout(testGtx(&ops, w), st, &recHandler{})
	off.Pop()

	gtx := layout.Context{Ops: &ops, Constraints: layout.Exact(image.Pt(w, h))}
	backdrop := color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xff}
	theme.Fill(gtx, image.Rect(0, 0, w, h), backdrop)
	card := image.Rect(100, 100, 320, 150)
	theme.Fill(gtx, card, st.Palette.Panel)

	img := pixeltest.Render(t, w, h, &ops) // unpatched Gio panics here
	if n := pixeltest.Count(img, image.Rect(0, 0, w, 90), backdrop, 2); n != w*90 {
		t.Errorf("%d of %d px above the card are the backdrop: the covered messages show through", n, w*90)
	}
	if n := pixeltest.Count(img, card, st.Palette.Panel, 2); n != card.Dx()*card.Dy() {
		t.Errorf("dialog card: %d of %d px", n, card.Dx()*card.Dy())
	}
}
