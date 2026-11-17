package ui

import (
	"image"
	"image/color"
	"testing"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// openOverlay stands for the Create menu while it shows.
type openOverlay struct{}

func (openOverlay) Layout(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }
func (openOverlay) Dismissed() bool                             { return false }

// The control strip's + while its Create menu is open is
// .p-control_strip__circle_button--active: theme-base-pry (its plus turned
// 135deg into an x) in the rail's scope, the default theme's inverted
// aubergine-0, Karim's light #FED4BE scope's custom shade 70, dark mode's
// aubergine-90. Drawn and sampled beside the icon (theme_contrast_test.go's
// tcDraw).
func TestAccountThemeCreateButtonOpen(t *testing.T) {
	for _, c := range []struct {
		name, ia string
		dark     bool
		want     color.NRGBA
	}{
		{"default light", "", false, color.NRGBA{R: 0xf9, G: 0xed, B: 0xff, A: 0xff}},
		{"karim picker light", tcPickerIA, false, color.NRGBA{R: 0x57, G: 0x3c, B: 0x2c, A: 0xff}},
		{"default dark", "", true, color.NRGBA{R: 0xe3, G: 0xce, B: 0xeb, A: 0xff}},
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
		cs := 36
		win := image.Pt(52, 52)
		a.winSize = win
		img := tcDraw(t, a, win, nil, func(gtx layout.Context) { a.createButton(gtx, image.Pt(8, 8), cs) })
		if got := img.RGBAAt(8+6, 8+cs/2); !tcNear(got, c.want, 2) {
			t.Errorf("%s: + with its menu open is %s; Slack theme-base-pry %s in the rail's scope", c.name, tcHex(tcNRGBA(got)), tcHex(c.want))
		}
	}
}
