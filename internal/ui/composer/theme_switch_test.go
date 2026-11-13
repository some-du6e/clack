package composer

import (
	"image"
	"image/color"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// slackInk is the composer text's colour in Slack: .ql-editor's
// rgb(var(--sk_primary_foreground)), 29,28,29 light and 209,210,211 dark.
func slackInk(t *testing.T, dark bool) color.NRGBA {
	t.Helper()
	c, ok := css.ParseColor("rgb(" + css.SlackVars(dark)["--sk_primary_foreground"] + ")")
	if !ok {
		t.Fatal("no --sk_primary_foreground")
	}
	return c
}

// TestComposerFollowsLiveThemeSwitch: typed text follows a live Light/Dark
// switch on the next frame (Karim, 2026-11-12: "white text in the
// composer on white bg" after switching to light on Windows). The two
// modes share one shaper here, as they can on Windows, so only the
// theme's Gen says the colours changed. The drawn text colour (the
// editor's recorded macro, and the pixels under a GPU) is checked against
// Slack's tokens: dark → light → dark, in a channel and an edit composer.
func TestComposerFollowsLiveThemeSwitch(t *testing.T) {
	app, _ := fakeslackApp(t)
	th := theme.New(true)
	sh := theme.NewShaper()
	th.SetShapers(sh, sh)
	h := &ovHost{slickHost: &slickHost{app: app, th: th}}
	ch := "C0THEMESW"
	forgetDraft(ch)
	t.Cleanup(func() { forgetDraft(ch) })
	c := New(h, ch, "")
	t.Cleanup(c.Close)
	c.InsertText("typed after the switch")

	var r input.Router
	ops := new(op.Ops)
	now := time.Now()
	const w, hh = 700, 160
	frame := func() {
		ops.Reset()
		paint.FillShape(ops, th.C.Bg, clip.Rect{Max: image.Pt(w, hh)}.Op())
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Source: r.Source(), Now: now,
			Constraints: layout.Constraints{Max: image.Pt(w, hh)}}
		slackc.Frame(gtx)
		c.Layout(gtx)
		r.Frame(ops)
		now = now.Add(16 * time.Millisecond)
	}
	for i, dark := range []bool{true, false, true} {
		if th.Dark != dark {
			th.SetDark(dark)
		}
		frame() // the next frame after the switch
		want := slackInk(t, dark)
		if got := c.st.Color; got != want {
			t.Errorf("step %d (dark %v): the editor style's text %v, want Slack's %v", i, dark, got, want)
		}
		if got := c.ed.lay.paintCol.text; got != want {
			t.Errorf("step %d (dark %v): the text was drawn in %v, want %v", i, dark, got, want)
		}
	}

	// an edit composer opened in dark, then switched
	ec := NewEdit(h, core.Message{Channel: ch, TS: "1700000000.000200", Text: "edit me"}, nil)
	t.Cleanup(ec.Close)
	gtxFor := func() layout.Context {
		return layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Now: time.Now(),
			Constraints: layout.Constraints{Max: image.Pt(w, hh)}}
	}
	ec.Layout(gtxFor())
	th.SetDark(false)
	ec.Layout(gtxFor())
	if got, want := ec.st.Color, slackInk(t, false); got != want {
		t.Errorf("edit composer after the switch: %v, want %v", got, want)
	}

	t.Run("pixels", func(t *testing.T) {
		for _, dark := range []bool{false, true} {
			th.SetDark(dark)
			frame()
			frame()
			img := pixeltest.Render(t, w, hh, ops)
			want := slackInk(t, dark)
			// the most inked pixel inside the editor: the darkest in
			// light, the lightest in dark
			box := image.Rectangle{Min: c.ui.InputOffset(), Max: c.ui.InputOffset().Add(image.Pt(300, 22))}
			best, bestL := color.RGBA{}, -1
			for y := box.Min.Y; y < box.Max.Y; y++ {
				for x := box.Min.X; x < box.Max.X; x++ {
					p := img.RGBAAt(x, y)
					l := int(p.R) + int(p.G) + int(p.B)
					if !dark {
						l = 765 - l
					}
					if l > bestL {
						best, bestL = p, l
					}
				}
			}
			d := func(a, b uint8) int {
				if a > b {
					return int(a - b)
				}
				return int(b - a)
			}
			if d(best.R, want.R) > 24 || d(best.G, want.G) > 24 || d(best.B, want.B) > 24 {
				t.Errorf("dark %v: the text's ink is %v, want about Slack's %v", dark, best, want)
			}
		}
	})
}
