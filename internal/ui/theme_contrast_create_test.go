package ui

import (
	"image"
	"image/color"
	"testing"
	"time"

	"gioui.org/io/pointer"
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// openOverlay stands for the Create menu while it shows.
type openOverlay struct{}

func (openOverlay) Layout(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }
func (openOverlay) Dismissed() bool                             { return false }

// The control strip's + while its Create menu is open is
// .p-control_strip__circle_button--active: theme-base-pry with
// theme-content-ter (its plus turned 135deg into an x) in the rail's
// scope, the default theme's inverted aubergine-0 with aubergine-100,
// Karim's light #FED4BE scope's custom shade 70 with its light ink, dark
// mode's aubergine-90; hovered, :hover's blended surf-inv-pry-hover layer
// over it. Drawn and sampled: the disc beside the icon, the x's ink.
func TestAccountThemeCreateButtonOpen(t *testing.T) {
	for _, c := range []struct {
		name, ia        string
		dark            bool
		fill, ink, hovd color.NRGBA
	}{
		{"default light", "", false, tcRGB(0xf9edff), tcRGB(0x39063a), tcRGB(0xefdef6)},
		{"karim picker light", tcPickerIA, false, tcRGB(0x573c2c), tcRGB(0xfff4f0), tcRGB(0x462f22)},
		{"default dark", "", true, tcRGB(0xe3ceeb), tcRGB(0x200221), tcRGB(0xe4d0ec)},
	} {
		a, ca, _ := fakeApp(t, map[string]any{"ia_theme": c.ia})
		ia, _ := theme.ParseIATheme(c.ia)
		theme.CachedFields(ia, c.dark, true)
		a.th.Dark = c.dark
		a.applyAccountPrefs(ca.Prefs())
		a.th.Refresh()
		a.createStrip().menu = openOverlay{}
		if !a.createMenuOpen() {
			t.Fatal("the Create menu doesn't count as open")
		}
		w := newTCWin(t, a, func(gtx layout.Context) { a.createButton(gtx, image.Pt(8, 8), 36) })
		w.later(400 * time.Millisecond)
		img := w.shot()
		if got := img.RGBAAt(tcDisc.X, tcDisc.Y); !tcNear(got, c.fill, 2) {
			t.Errorf("%s: + with its menu open is %s; Slack theme-base-pry %s in the rail's scope", c.name, tcHex(tcNRGBA(got)), tcHex(c.fill))
		}
		if k, off, peak := tcAlong(img, tcIcon, c.fill, c.ink); k < .8 || k > 1.1 || off > 16 {
			t.Errorf("%s: the x's ink peaks at %s (%.2f of the way to Slack's theme-content-ter %s)", c.name, tcHex(peak), k, tcHex(c.ink))
		}
		w.ptr(pointer.Move, tcMid)
		w.later(300 * time.Millisecond)
		if got := w.shot().RGBAAt(tcDisc.X, tcDisc.Y); !tcNear(got, c.hovd, 2) {
			t.Errorf("%s: + hovered with its menu open is %s; Slack's :hover layer over theme-base-pry %s", c.name, tcHex(tcNRGBA(got)), tcHex(c.hovd))
		}
	}
}
