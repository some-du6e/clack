package composer

import (
	"image"
	"reflect"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme/pixeltest"
)

// TestInlineCodeDraws: a draft with `sho` in inline code (the reported
// blank chip) shapes real outlines in the code font, and (with a GPU) the
// chip shows CodeColor glyphs over CodeBg. Light and dark, with the
// colours Composer.editorStyle takes from the theme.
func TestInlineCodeDraws(t *testing.T) {
	sh := theme.NewShaper()
	for _, dark := range []bool{false, true} {
		name := "light"
		if dark {
			name = "dark"
		}
		t.Run(name, func(t *testing.T) {
			th := theme.Bare(dark)
			st := DefaultEditorStyle(sh)
			p := &th.C
			st.Color, st.CodeColor, st.CodeBg, st.CodeBorder = p.Fg, p.CodeFg, p.CodeBg, p.CodeBorder

			e := NewEditor()
			d := NewDoc()
			d.Insert(0, "a sho b", Attr{})
			d.SetStyle(2, 5, Code, true)
			e.SetDoc(d)

			const w, h = 300, 40
			var ops op.Ops
			paint.FillShape(&ops, p.Bg, clip.Rect{Max: image.Pt(w, h)}.Op())
			gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(w, h))}
			e.Layout(gtx, st)

			var code *tlItem
			for pi := range e.lay.paras {
				for ii := range e.lay.paras[pi].items {
					if it := &e.lay.paras[pi].items[ii]; it.code {
						code = it
					}
				}
			}
			if code == nil || code.shape == nil || len(code.shape.glyphs) == 0 {
				t.Fatalf("no shaped inline-code item: %+v", e.lay.paras)
			}
			path := sh.Shape(code.shape.glyphs)
			if !reflect.ValueOf(path).FieldByName("hasSegments").Bool() {
				t.Fatal("inline code glyphs have no outline (drawn as nothing)")
			}

			img := pixeltest.Render(t, w, h, &ops)
			all := image.Rect(0, 0, w, h)
			if n := pixeltest.Ink(img, all, st.CodeColor, st.CodeBg); n < 8 {
				t.Errorf("%d CodeColor pixels, want ≥8 (blank code chip)", n)
			}
			if n := pixeltest.Count(img, all, st.CodeBg, 3); n < 40 {
				t.Errorf("%d CodeBg pixels: no code chip", n)
			}
		})
	}
}
