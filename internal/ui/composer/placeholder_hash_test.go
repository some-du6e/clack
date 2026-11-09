package composer

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme/pixeltest"
)

// The # in "Message #clack" is Slack's icon-font hash-small (U+E125, one
// unit strokes), drawn with the text's weight: it is no taller than the
// text's capitals and inks about what Lato's own strokes do. alpha.24 drew
// a stroked 16px hash instead, visibly bolder and bigger (Karim's
// screenshot).
func TestPlaceholderHashMatchesText(t *testing.T) {
	for _, dark := range []bool{false, true} {
		th := theme.Bare(dark)
		th.Shaper = theme.NewShaper()
		var c chrome
		p := c.palette(th)
		w, h := 220, 22
		draw := func(f func(gtx layout.Context)) *image.RGBA {
			var ops op.Ops
			paint.FillShape(&ops, p.bg, clip.Rect{Max: image.Pt(w, h)}.Op())
			gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(w, h))}
			f(gtx)
			return pixeltest.Render(t, w, h, &ops)
		}
		st := &chromeState{Mode: modeChannel, ChannelName: "clack", Placeholder: "Message clack", ShowPlaceholder: true}
		img := draw(func(gtx layout.Context) { c.placeholder(gtx, th, p, st, image.Point{}, w) })
		if dir := os.Getenv("ICON_SHOTS_DIR"); dir != "" { // the icon-shots workflow's artifact
			name := "placeholder-hash-light.png"
			if dark {
				name = "placeholder-hash-dark.png"
			}
			if f, err := os.Create(filepath.Join(dir, name)); err == nil {
				png.Encode(f, img)
				f.Close()
			}
		}

		gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
		mW := crMeasure(gtx, th.Shaper, textStyle{Size: 15, LineHeight: 22}, "M")
		msgW := crMeasure(gtx, th.Shaper, textStyle{Size: 15, LineHeight: 22}, "Message")
		hashX := msgW + c.space(gtx, th.Shaper, 0) - 4
		capH, capInk := inkBox(img, image.Rect(0, 0, mW, h), p.ph, p.bg)
		hashH, hashInk := inkBox(img, image.Rect(hashX, 0, hashX+16, h), p.ph, p.bg)
		if hashInk == 0 {
			t.Fatalf("dark=%v: no # drawn", dark)
		}
		if hashH.Dy() > capH.Dy()+1 || hashH.Min.Y < capH.Min.Y-1 || hashH.Max.Y > capH.Max.Y+1 {
			t.Errorf("dark=%v: # ink %v vs the M's %v (taller than the text)", dark, hashH, capH)
		}
		// the stroked hash alpha.24 drew, for scale
		old := draw(func(gtx layout.Context) {
			defer op.Offset(image.Pt(2, 3)).Push(gtx.Ops).Pop()
			drawHashIcon(gtx, 16, p.ph)
		})
		_, oldInk := inkBox(old, image.Rect(0, 0, w, h), p.ph, p.bg)
		if hashInk > oldInk*4/5 {
			t.Errorf("dark=%v: # inks %d (alpha.24's stroked hash %d): still bold", dark, hashInk, oldInk)
		}
		if hashInk > capInk*3/2 {
			t.Errorf("dark=%v: # inks %d, the M %d", dark, hashInk, capInk)
		}
	}
}

func crMeasure(gtx layout.Context, sh *text.Shaper, s textStyle, txt string) int {
	m := op.Record(gtx.Ops)
	d := crText(gtx, sh, 0, 0, 1<<12, s, txt)
	m.Stop()
	return d.Size.X
}

// inkBox is the bounding box of the pixels in r that moved from bg towards
// fg, and their summed coverage (0-255 per pixel, over the channel where
// fg and bg differ most).
func inkBox(img *image.RGBA, r image.Rectangle, fg, bg color.NRGBA) (image.Rectangle, int) {
	ch := func(c color.NRGBA) [3]int { return [3]int{int(c.R), int(c.G), int(c.B)} }
	f, b := ch(fg), ch(bg)
	k, best := 0, 0
	for i := 0; i < 3; i++ {
		if d := abs(f[i] - b[i]); d > best {
			k, best = i, d
		}
	}
	var box image.Rectangle
	sum := 0
	r = r.Intersect(img.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			v := int(img.Pix[img.PixOffset(x, y)+k])
			cov := (v - b[k]) * 255 / (f[k] - b[k])
			if cov < 24 {
				continue
			}
			sum += min(cov, 255)
			box = box.Union(image.Rect(x, y, x+1, y+1))
		}
	}
	return box, sum
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
