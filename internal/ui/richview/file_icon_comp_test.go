package richview

import (
	"image"
	"image/color"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// A single file card in component mode (any plugin or theme stylesheet,
// or a patched MessageFile) draws its svgicon over the card, as without
// one: the icon is the card's (its comp), painted with the card after the
// card's own fill. It was the static pass's, under the opaque Panel fill
// MessageFile's box paints later, so a Slick theme left every PDF, ZIP or
// EXE pillow with an empty 36px square; an email's attachment pillows
// too.
func TestFileIconInComponentMode(t *testing.T) {
	exe := &rt.File{ID: "F0COMP0001", Name: "game_scarlett_master.exe", Type: "exe", PrettyType: "Windows Application", Mode: "hosted"}
	mail := &rt.File{ID: "F0COMP0002", Name: "Quarterly numbers", Title: "Quarterly numbers", Type: "email", Mode: "email", Expanded: true,
		Email: &rt.Email{Subject: "Quarterly numbers", Attachments: []rt.EmailAttachment{{Name: "assets.zip", FileID: "F0COMP0003", Size: 2048}}}}
	cases := []struct {
		name string
		f    *rt.File
		art  string // the icon checked (the email's attachment pillow's)
	}{
		{"exe", exe, "fileicon-generic"},
		{"email attachment", mail, "fileicon-archive"},
	}
	modes := []struct {
		name string
		on   func(t *testing.T)
	}{
		{"stylesheet", func(t *testing.T) {
			slackc.Styles.Set("test-file-icon-comp", ".zz-test-file-icon-comp{color:red}") // a theme sheet: a rule that matches nothing here
			t.Cleanup(func() { slackc.Styles.Remove("test-file-icon-comp") })
		}},
		{"MessageFile patched", func(t *testing.T) {
			slackc.SetPatched([]string{"MessageFile"})
			t.Cleanup(func() { slackc.SetPatched(nil) })
		}},
	}
	for _, c := range cases {
		st := newTestStyle()
		want, wr := renderFileIcon(t, st, c.f, c.art)
		ink := 0 // the icon as drawn without components: not the white page under it
		for y := wr.Min.Y; y < wr.Max.Y; y++ {
			for x := wr.Min.X; x < wr.Max.X; x++ {
				if colDist(want.RGBAAt(x, y), color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}) > 24 {
					ink++
				}
			}
		}
		if ink*4 < wr.Dx()*wr.Dy() {
			t.Fatalf("%s: the %s icon at %v has %d inked pixels without components: nothing to compare", c.name, c.art, wr, ink)
		}
		for _, m := range modes {
			t.Run(c.name+"/"+m.name, func(t *testing.T) {
				m.on(t)
				if compMode()&modeAtts == 0 {
					t.Fatal("not in component mode")
				}
				got, r := renderFileIcon(t, st, c.f, c.art)
				if r != wr {
					t.Fatalf("the icon moved: %v, without components %v", r, wr)
				}
				diff, n := 0, 0
				for y := r.Min.Y; y < r.Max.Y; y++ {
					for x := r.Min.X; x < r.Max.X; x++ {
						n++
						if colDist(got.RGBAAt(x, y), want.RGBAAt(x, y)) > 24 {
							diff++
						}
					}
				}
				if diff*10 > n {
					t.Errorf("%d of the icon's %d pixels differ from the card without components: its svgicon is covered", diff, n)
				}
			})
		}
	}
}

// renderFileIcon paints f's message on white and returns it with the
// rect of its art icon name.
func renderFileIcon(t *testing.T, st *Style, f *rt.File, name string) (*image.RGBA, image.Rectangle) {
	t.Helper()
	const w, h = 480, 900
	ops := new(op.Ops)
	paint.FillShape(ops, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, clip.Rect{Max: image.Pt(w, h)}.Op())
	v := New(doc(fileBlock(f)), Options{TS: "1700000000.000100", Channel: "C0COMP"})
	gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(w, h))}
	v.Layout(gtx, st, nil)
	r, ok := artIcon(&v.d, name)
	if !ok {
		t.Fatalf("no %s icon laid out", name)
	}
	return pixeltest.Render(t, w, h, ops), r
}
