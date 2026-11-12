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

	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// TestW10Shots draws gap-w10's ports light and dark at 2x into
// COMPOSER_AUDIT_SHOTS_DIR (the composer-audit-shots workflow; skipped
// without it): a list typed inside a quote with the full-width markers
// and the code fence that ends a code line, the link dialog's rich text
// for a selection holding an emoji, and the @ list's offline help.
// Synthetic data and internal/fakeslack only. The native spellcheck menu
// is the OS's own and has no headless shot.
func TestW10Shots(t *testing.T) {
	dir := os.Getenv("COMPOSER_AUDIT_SHOTS_DIR")
	if dir == "" {
		t.Skip("COMPOSER_AUDIT_SHOTS_DIR not set")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
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
	ch := "C0W10SHOT"
	t.Cleanup(func() { forgetDraft(ch) })
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		th := theme.New(dark)
		h := &ovHost{slickHost: &slickHost{app: app, th: th}}

		t.Run("quoted-list-"+mode, func(t *testing.T) {
			forgetDraft(ch)
			c := New(h, ch, "")
			t.Cleanup(c.Close)
			e := NewEditor()
			edType(e, ">Quoted lead-in")
			edPress(e, key.NameReturn, key.ModShift)
			edType(e, "- first point")
			edPress(e, key.NameReturn, key.ModShift)
			edType(e, "second point")
			edPress(e, key.NameReturn, key.ModShift)
			edPress(e, key.NameReturn, 0) // leaves the list
			edType(e, "これは＊太字＊と｀コード｀、＿斜体＿")
			edPress(e, key.NameReturn, key.ModShift)
			edType(e, "```code line```")
			edType(e, "after the block")
			c.ed.SetDoc(e.Doc())
			w, hh := 760, 360
			var ops op.Ops
			var r input.Router
			for i := 0; i < 5; i++ {
				time.Sleep(40 * time.Millisecond)
				frame(th, &ops, &r, w*scale, hh*scale, func(gtx layout.Context) {
					gtx.Constraints = layout.Constraints{Max: image.Pt(w*scale, hh*scale)}
					off := op.Offset(image.Pt(0, 40*scale)).Push(gtx.Ops)
					c.Layout(gtx)
					off.Pop()
				})
			}
			save(t, "w10-quoted-list-"+mode, w*scale, hh*scale, &ops)
		})

		t.Run("link-rich-"+mode, func(t *testing.T) {
			forgetDraft(ch)
			c := New(h, ch, "")
			t.Cleanup(c.Close)
			d := NewDoc()
			end := d.Insert(0, "see ", Attr{})
			end = d.InsertAtom(end, &Atom{Kind: AtomEmoji, ID: "tada", Text: ":tada:", Uni: "🎉"})
			end = d.Insert(end, " the ", Attr{})
			d.Insert(end, "docs", Attr{Style: Bold})
			c.ed.SetDoc(d)
			c.ed.SetSelection(0, c.ed.Doc().Len())
			c.openLink()
			o := h.last()
			w, hh := 1100*scale, 760*scale
			var ops op.Ops
			var r input.Router
			for i := 0; i < 6; i++ {
				frame(th, &ops, &r, w, hh, func(gtx layout.Context) { o.Layout(gtx) })
				time.Sleep(60 * time.Millisecond)
			}
			save(t, "w10-link-rich-"+mode, w, hh, &ops)
		})

		t.Run("autocomplete-offline-"+mode, func(t *testing.T) {
			prev := acClientOnline
			acClientOnline = func(Host) bool { return false }
			t.Cleanup(func() { acClientOnline = prev })
			acApp, _ := acSlackApp(t)
			ac := newAutocomplete(&slickHost{app: acApp, th: th}, tChGen)
			t.Cleanup(ac.Close)
			acSettle(t, ac, "@al", func(n []string) bool { return hasName(n, "alina") })
			w, hh := 600*scale, 320*scale
			var ops op.Ops
			paint.FillShape(&ops, th.C.Bg, clip.Rect{Max: image.Pt(w, hh)}.Op())
			gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: scale, PxPerSp: scale},
				Constraints: layout.Constraints{Max: image.Pt(w-32*scale, hh-32*scale)}}
			st := op.Offset(image.Pt(16*scale, 16*scale)).Push(gtx.Ops)
			ac.Layout(gtx, 0)
			st.Pop()
			save(t, "w10-autocomplete-offline-"+mode, w, hh, &ops)
		})
	}
}
