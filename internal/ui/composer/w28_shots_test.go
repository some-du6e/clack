package composer

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/shortcut"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// TestW28Shots draws gap-w28's ports light and dark at 2x into
// COMPOSER_AUDIT_SHOTS_DIR (the composer-audit-shots workflow; skipped
// without it): the Mod+K coachmark over a composer with text selected
// (Windows' Ctrl chip), a code block inside a quote between a quote line
// and a plain code block, and a 520px composer whose footer folded with
// the overflow row open. Synthetic data and internal/fakeslack only.
func TestW28Shots(t *testing.T) {
	dir := os.Getenv("COMPOSER_AUDIT_SHOTS_DIR")
	if dir == "" {
		t.Skip("COMPOSER_AUDIT_SHOTS_DIR not set")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := shortcut.GOOS
	shortcut.GOOS = "windows" // Karim's platform: Ctrl + V
	t.Cleanup(func() { shortcut.GOOS = old })
	const scale = 2
	save := func(t *testing.T, name string, w, h int, ops *op.Ops) {
		img := pixeltest.Render(t, w, h, ops)
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s.png", name)
	}
	frame := func(th *theme.Theme, ops *op.Ops, r *input.Router, w, h int, draw func(gtx layout.Context)) {
		ops.Reset()
		paint.FillShape(ops, th.C.Bg, clip.Rect{Max: image.Pt(w, h)}.Op())
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: scale, PxPerSp: scale},
			Constraints: layout.Exact(image.Pt(w, h)), Now: time.Now(), Source: r.Source()}
		slackc.Frame(gtx)
		draw(gtx)
		r.Frame(ops)
	}
	app, _ := fakeslackApp(t)
	ch := "C0W28SHOT"
	t.Cleanup(func() { forgetDraft(ch) })
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		th := theme.New(dark)

		t.Run("cmdk-coachmark-"+mode, func(t *testing.T) {
			forgetDraft(ch)
			h := &cmdKHost{ovHost: &ovHost{slickHost: &slickHost{app: app, th: th}}}
			c := New(h, ch, "")
			t.Cleanup(c.Close)
			d := NewDoc()
			d.Insert(0, "The release notes are on the wiki", Attr{})
			c.ed.SetDoc(d)
			c.ed.SetSelection(26, 34) // "the wiki"
			c.cmdKSelected()
			w, hh := 760, 360
			var ops op.Ops
			var r input.Router
			for i := 0; i < 5; i++ {
				time.Sleep(40 * time.Millisecond)
				frame(th, &ops, &r, w*scale, hh*scale, func(gtx layout.Context) {
					top := (hh - 130) * scale
					c.Origin = image.Pt(0, top)
					off := op.Offset(image.Pt(0, top)).Push(gtx.Ops)
					g := gtx
					g.Constraints = layout.Constraints{Max: image.Pt(w*scale, 130*scale)}
					c.Layout(g)
					off.Pop()
					for _, o := range h.overlays {
						if !o.Dismissed() {
							o.Layout(gtx)
						}
					}
				})
			}
			save(t, "w28-cmdk-coachmark-"+mode, w*scale, hh*scale, &ops)
		})

		t.Run("quoted-code-"+mode, func(t *testing.T) {
			forgetDraft(ch)
			h := &ovHost{slickHost: &slickHost{app: app, th: th}}
			c := New(h, ch, "")
			t.Cleanup(c.Close)
			e := NewEditor()
			edType(e, ">")
			edType(e, "Run it like this:")
			edPress(e, key.NameReturn, key.ModShift)
			edType(e, "```")
			edType(e, "go test ./internal/ui/composer")
			edPress(e, key.NameReturn, key.ModShift)
			edType(e, "go vet ./...")
			edPress(e, key.NameReturn, key.ModShift)
			edPress(e, key.NameReturn, key.ModShift) // code exit
			edType(e, "```")
			edType(e, "plain code")
			c.ed.SetDoc(e.Doc())
			w, hh := 760, 380
			var ops op.Ops
			var r input.Router
			for i := 0; i < 5; i++ {
				time.Sleep(40 * time.Millisecond)
				frame(th, &ops, &r, w*scale, hh*scale, func(gtx layout.Context) {
					gtx.Constraints = layout.Constraints{Max: image.Pt(w*scale, hh*scale)}
					off := op.Offset(image.Pt(0, 20*scale)).Push(gtx.Ops)
					c.Layout(gtx)
					off.Pop()
				})
			}
			save(t, "w28-quoted-code-"+mode, w*scale, hh*scale, &ops)
		})

		t.Run("narrow-footer-"+mode, func(t *testing.T) {
			forgetDraft(ch)
			h := &ovHost{slickHost: &slickHost{app: app, th: th}}
			c := New(h, ch, "")
			t.Cleanup(c.Close)
			d := NewDoc()
			d.Insert(0, "A narrow composer", Attr{})
			c.ed.SetDoc(d)
			w, hh := 560, 220 // a 520px composer box: the toolbar is 414px, under 580
			var ops op.Ops
			var r input.Router
			for i := 0; i < 6; i++ {
				if i == 2 {
					c.ui.more.open = true
				}
				time.Sleep(40 * time.Millisecond)
				frame(th, &ops, &r, w*scale, hh*scale, func(gtx layout.Context) {
					top := 80 * scale
					c.Origin = image.Pt(0, top)
					off := op.Offset(image.Pt(0, top)).Push(gtx.Ops)
					g := gtx
					g.Constraints = layout.Constraints{Max: image.Pt(w*scale, (hh-80)*scale)}
					c.Layout(g)
					off.Pop()
				})
			}
			if !c.ui.f.fold {
				t.Errorf("not folded (toolbar %d)", c.ui.f.eq)
			}
			save(t, "w28-narrow-footer-"+mode, w*scale, hh*scale, &ops)
		})
	}
}
