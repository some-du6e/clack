package huddleui

import (
	"image"
	"image/color"
	"time"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/huddle"
	"github.com/some-du6e/yoloslackgoclient/internal/imgcache"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

type (
	C = layout.Context
	D = layout.Dimensions
)

// Slack's huddle colours (measured on the 2026 web client).
var (
	hudFrame    = color.NRGBA{R: 0x3b, G: 0x0f, B: 0x3d, A: 0xff} // window frame
	hudBtn      = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x26} // translucent control
	hudBtnHover = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x3a}
	hudBtnOn    = color.NRGBA{R: 0xf8, G: 0xea, B: 0xf8, A: 0xff} // the lit mic button
	hudInk      = color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xff}
	hudWhite    = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	hudLeave    = color.NRGBA{R: 0xe0, G: 0x1e, B: 0x5a, A: 0xff}
	hudLeaveHov = color.NRGBA{R: 0xc4, G: 0x11, B: 0x4b, A: 0xff}
	hudGreen    = color.NRGBA{R: 0x2b, G: 0xac, B: 0x76, A: 0xff}
	hudMint     = color.NRGBA{R: 0xc6, G: 0xf1, B: 0xdd, A: 0xff} // header "huddle live" pill
	hudMintDark = color.NRGBA{R: 0x1e, G: 0x51, B: 0x3d, A: 0xff}
	hudMintInk  = color.NRGBA{R: 0x00, G: 0x5e, B: 0x3d, A: 0xff}
	hudTileBg   = color.NRGBA{R: 0xf4, G: 0xf3, B: 0xf5, A: 0xff}
	hudShade    = color.NRGBA{A: 0x99}
	hudStageA   = color.NRGBA{R: 0xf9, G: 0xd9, B: 0xe9, A: 0xff} // stage gradient (Slack draws a theme picture)
	hudStageB   = color.NRGBA{R: 0xd5, G: 0xe6, B: 0xf7, A: 0xff}
)

func at(gtx C, p, sz image.Point, w layout.Widget) D {
	defer op.Offset(p).Push(gtx.Ops).Pop()
	gtx.Constraints = layout.Constraints{Max: sz}
	return w(gtx)
}

// button is a click area; bg paints the background given hover.
func button(gtx C, c *widget.Clickable, sz image.Point, bg func(gtx C, hov bool), w layout.Widget) D {
	return c.Layout(gtx, func(gtx C) D {
		gtx.Constraints = layout.Exact(sz)
		if bg != nil {
			bg(gtx, c.Hovered())
		}
		if w != nil {
			w(gtx)
		}
		pointer.CursorPointer.Add(gtx.Ops)
		return D{Size: sz}
	})
}

// iconAt draws a centered icon in a box.
func iconAt(gtx C, box image.Point, name string, size unit.Dp, col color.NRGBA) {
	s := gtx.Dp(size)
	at(gtx, image.Pt((box.X-s)/2, (box.Y-s)/2), image.Pt(s, s), func(gtx C) D {
		return icons.Get(name)(gtx, size, col)
	})
}

// centerLabel draws a label centered in box.
func centerLabel(gtx C, th *theme.Theme, box image.Point, size unit.Sp, w font.Weight, col color.NRGBA, txt string) {
	m := op.Record(gtx.Ops)
	gtx.Constraints = layout.Constraints{Max: box}
	d := th.Label(gtx, size, w, col, txt)
	call := m.Stop()
	at(gtx, image.Pt((box.X-d.Size.X)/2, (box.Y-d.Size.Y)/2), box, func(gtx C) D { call.Add(gtx.Ops); return d })
}

func labelSize(gtx C, th *theme.Theme, size unit.Sp, w font.Weight, txt string) image.Point {
	m := op.Record(gtx.Ops)
	d := th.Label(gtx, size, w, hudWhite, txt)
	m.Stop()
	return d.Size
}

// avatar draws a user's picture (cover) or a placeholder.
func avatar(gtx C, th *theme.Theme, imgs *imgcache.Cache, url string, sz image.Point, radius int) {
	if url != "" && imgs != nil {
		if img, ok := imgs.Get(url, max(sz.X, sz.Y)); ok {
			theme.Image(gtx, img, sz, radius)
			return
		}
	}
	theme.RRect(gtx, image.Rectangle{Max: sz}, radius, color.NRGBA{R: 0x00, G: 0x7a, B: 0x5a, A: 0xff})
}

// videoOps caches one paint.ImageOp per remote video frame.
type videoOps struct {
	seq map[string]uint64
	op  map[string]paint.ImageOp
}

func (v *videoOps) get(s *huddle.Session, key string) (paint.ImageOp, bool) {
	if v.seq == nil {
		v.seq, v.op = map[string]uint64{}, map[string]paint.ImageOp{}
	}
	f := s.Frame(key)
	if f.Img == nil {
		return paint.ImageOp{}, false
	}
	if v.seq[key] != f.Seq {
		v.seq[key] = f.Seq
		v.op[key] = paint.NewImageOp(f.Img)
	}
	return v.op[key], true
}

// fit draws img letterboxed (contain) inside sz.
func fit(gtx C, img paint.ImageOp, sz image.Point, radius int) {
	defer clip.UniformRRect(image.Rectangle{Max: sz}, radius).Push(gtx.Ops).Pop()
	is := img.Size()
	if is.X <= 0 || is.Y <= 0 {
		return
	}
	s := min(float32(sz.X)/float32(is.X), float32(sz.Y)/float32(is.Y))
	off := layout.FPt(sz).Sub(layout.FPt(is).Mul(s)).Div(2)
	defer op.Affine(f32Affine(s, off.X, off.Y)).Push(gtx.Ops).Pop()
	img.Filter = paint.FilterLinear
	img.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

func f32Affine(s, dx, dy float32) f32.Affine2D {
	return f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(s, s)).Offset(f32.Pt(dx, dy))
}

// gradient fills r with a vertical two-colour gradient.
func gradient(gtx C, r image.Rectangle, radius int, a, b color.NRGBA) {
	defer clip.UniformRRect(r, radius).Push(gtx.Ops).Pop()
	paint.LinearGradientOp{Stop1: layout.FPt(r.Min), Color1: a, Stop2: layout.FPt(image.Pt(r.Max.X, r.Max.Y)), Color2: b}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

// speakingBars is Slack's green "talking" chip (three animated bars).
func speakingBars(gtx C, now time.Time) D {
	w, h := gtx.Dp(26), gtx.Dp(20)
	theme.RRect(gtx, image.Rect(0, 0, w, h), gtx.Dp(6), color.NRGBA{R: 0x1a, G: 0x3d, B: 0x2e, A: 0xe6})
	ph := now.UnixMilli() / 120
	for i := 0; i < 4; i++ {
		bh := gtx.Dp(unit.Dp(3 + float32((int(ph)+i*3)%5)*1.6))
		x := gtx.Dp(unit.Dp(5 + 4.5*float32(i)))
		theme.RRect(gtx, image.Rect(x, (h-bh)/2, x+gtx.Dp(2.5), (h+bh)/2), gtx.Dp(1), color.NRGBA{R: 0x5b, G: 0xe0, B: 0x9b, A: 0xff})
	}
	return D{Size: image.Pt(w, h)}
}

// elapsed is "m:ss" or "h:mm:ss".
func elapsed(d time.Duration) string {
	s := int(d.Seconds())
	if s < 0 {
		s = 0
	}
	h, m := s/3600, s/60%60
	pad := func(n int) string { return string([]byte{byte('0' + n/10), byte('0' + n%10)}) }
	if h > 0 {
		return itoa(h) + ":" + pad(m) + ":" + pad(s%60)
	}
	return itoa(m) + ":" + pad(s%60)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
