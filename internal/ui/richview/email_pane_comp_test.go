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

// The email pane (EmailFileDetailsContent: richview's EmailDetails) draws
// its header's email svgicon on the blue square and the From pill's
// 16px email icon before the address, on Karim's path too: a Slick theme
// on and MessageFile, MessageFiles and Blocks patched, where the file is a
// MessageFile component whose box paints after the static pass. α.165
// painted the icons in the static pass, under it: a plain blue square and
// an empty gap in the pill (Karim, 2026-11-24, "i told u 3 ages ago").
func TestEmailPaneIconsInComponentMode(t *testing.T) {
	f := &rt.File{ID: "F0MAILPANE1", Name: "A synthetic newsletter", Title: "A synthetic newsletter", Type: "email", Mode: "email",
		Email: &rt.Email{Subject: "A synthetic newsletter", Timestamp: 1795302540,
			From: []rt.EmailAddress{{Name: "Synthetic Sender", Address: "sender@example.test"}},
			To:   []rt.EmailAddress{{Name: "Reader", Address: "reader@example.test"}}}}
	paths := []struct {
		name string
		on   func(t *testing.T)
	}{
		{"native", func(t *testing.T) {}},
		{"Slick theme, MessageFile MessageFiles Blocks patched", func(t *testing.T) {
			slackc.Styles.Set("test-email-pane", ".zz-test-email-pane{color:red}") // a theme sheet
			slackc.SetPatched([]string{"MessageFile", "MessageFiles", "Blocks"})
			t.Cleanup(func() {
				slackc.Styles.Remove("test-email-pane")
				slackc.SetPatched(nil)
			})
		}},
	}
	for _, p := range paths {
		t.Run(p.name, func(t *testing.T) {
			p.on(t)
			for _, dark := range []bool{false, true} {
				st := newTestStyle()
				bg := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
				if dark {
					st.SetPalette(DarkPalette())
					bg = color.NRGBA{R: 0x1a, G: 0x1d, B: 0x21, A: 0xff}
				}
				const w, h = 520, 560
				ops := new(op.Ops)
				paint.FillShape(ops, bg, clip.Rect{Max: image.Pt(w, h)}.Op())
				v := New(doc(fileBlock(f)), Options{EmailDetails: true, CompactFiles: true, Channel: "C0MAILPANE", TS: "1795302540.000100"})
				gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1.1, PxPerSp: 1.1}, Constraints: layout.Exact(image.Pt(w, h))}
				v.Layout(gtx, st, nil)
				var tile, pill image.Rectangle
				for _, o := range v.d.overlayIcons {
					switch o.name {
					case "file-email":
						tile = o.r
					case "email":
						pill = o.r
					}
				}
				if tile.Empty() || pill.Empty() {
					t.Fatalf("dark %v: the header icon %v or the pill's %v wasn't laid out", dark, tile, pill)
				}
				img := pixeltest.Render(t, w, h, ops)
				// the header: white strokes on the blue square
				white := 0
				for y := tile.Min.Y; y < tile.Max.Y; y++ {
					for x := tile.Min.X; x < tile.Max.X; x++ {
						if c := img.RGBAAt(x, y); c.R > 200 && c.G > 200 && c.B > 200 {
							white++
						}
					}
				}
				if white < tile.Dx()*tile.Dy()/20 {
					t.Errorf("dark %v: the header's square has %d white pixels of %d: no email icon on it", dark, white, tile.Dx()*tile.Dy())
				}
				// the pill: the icon's ink against the pill's fill (read at
				// the pill's corner, inside its radius)
				fill := img.RGBAAt(pill.Min.X-2, pill.Min.Y-2+pill.Dy()/2)
				ink := 0
				for y := pill.Min.Y; y < pill.Max.Y; y++ {
					for x := pill.Min.X; x < pill.Max.X; x++ {
						if emailPaneDist(img.RGBAAt(x, y), fill) > 60 {
							ink++
						}
					}
				}
				if ink < pill.Dx()*pill.Dy()/20 {
					t.Errorf("dark %v: the From pill's icon has %d inked pixels of %d: an empty gap before the address", dark, ink, pill.Dx()*pill.Dy())
				}
			}
		})
	}
}

func emailPaneDist(a, b color.RGBA) int {
	d := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	return d(a.R, b.R) + d(a.G, b.G) + d(a.B, b.B)
}
