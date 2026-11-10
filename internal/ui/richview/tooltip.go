package richview

import (
	"image"
	"image/color"
	"time"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/ui/slackc"
)

// tooltipClass is Slack's tooltip box (a portal under <body>).
var tooltipClass = slackc.Class("c-tooltip__tip c-tooltip__tip--top", "role", "tooltip")

// EmojiTipDelay is how long the pointer rests on an emoji before Slack
// shows its :name: (the emoji's delay="300" tooltip).
const EmojiTipDelay = 300 * time.Millisecond

// emojiAt returns the emoji frag under p (view coordinates), or -1.
func (v *View) emojiAt(p f32.Point) int {
	for i := range v.d.frags {
		f := &v.d.frags[i]
		if f.kind != fImage || f.tip == 0 {
			continue
		}
		if p.X >= f.x && p.X < f.x+float32(f.size) && p.Y >= float32(f.y) && p.Y < float32(f.y+f.size) {
			return i
		}
	}
	return -1
}

// HoveredEmoji is the :name: of the emoji under the pointer, if any.
func (v *View) HoveredEmoji() (string, bool) {
	if v.tipFrag < 0 || v.tipFrag >= len(v.d.frags) {
		return "", false
	}
	f := &v.d.frags[v.tipFrag]
	if f.tip == 0 || int(f.tip) > len(v.d.tips) {
		return "", false
	}
	return v.d.tips[f.tip-1], true
}

// emojiTip draws the hovered emoji's :name: in Slack's dark tooltip,
// centred above it, once the pointer has rested EmojiTipDelay. Deferred,
// so it sits above the rest of the window and outside every clip.
func (v *View) emojiTip(gtx layout.Context, st *Style) {
	name, ok := v.HoveredEmoji()
	if !ok || !v.hovering {
		return
	}
	if wait := v.tipAt.Add(EmojiTipDelay); !gtx.Now.IsZero() && gtx.Now.Before(wait) {
		gtx.Execute(op.InvalidateCmd{At: wait})
		return
	}
	f := &v.d.frags[v.tipFrag]
	l := lay{st: st, d: &v.d, scale: v.laidPx, spx: v.laidSp}
	if l.scale <= 0 {
		l.scale, l.spx = 1, 1
	}
	k := l.faceFor(font.Font{Typeface: st.Face, Weight: font.Bold}, l.sp(13), 0)
	sh := st.word(k, name)
	padX, padY, lh := l.dp(12), l.dp(8), l.dp(18)
	bw, bh := int(sh.adv+0.999)+2*padX, lh+2*padY
	cx := int(f.x + float32(f.size)/2)
	gap, ar := l.dp(8), l.dp(6)
	r := image.Rect(cx-bw/2, f.y-gap-bh, cx-bw/2+bw, f.y-gap)
	if r.Min.X < 0 {
		r = r.Add(image.Pt(-r.Min.X, 0))
	}
	p := &st.Palette
	// Slack's tooltip is a portal under <body> (.c-tooltip__tip), so a
	// row's rules don't reach it but a plugin's tooltip rule does.
	bg, fg, o := tooltipStyle(p)
	m := op.Record(gtx.Ops)
	var opac paint.OpacityStack
	if o < 1 {
		opac = paint.PushOpacity(gtx.Ops, max(o, 0))
	}
	paint.FillShape(gtx.Ops, bg, clip.UniformRRect(r, l.dp(8)).Op(gtx.Ops))
	var path clip.Path
	path.Begin(gtx.Ops)
	ax, ay := float32(cx), float32(f.y-gap)
	path.MoveTo(f32.Pt(ax-float32(ar), ay-float32(ar)))
	path.LineTo(f32.Pt(ax+float32(ar), ay-float32(ar)))
	path.LineTo(f32.Pt(ax, ay))
	path.Close()
	paint.FillShape(gtx.Ops, bg, clip.Outline{Path: path.End()}.Op())
	bm := st.spaceOf(k)
	base := r.Min.Y + padY + int((float32(lh)-(bm.asc+bm.desc))/2+bm.asc+0.5)
	t := op.Offset(image.Pt(r.Min.X+padX, base)).Push(gtx.Ops)
	glyphs, _ := st.pathOf(sh, true)
	cs := clip.Outline{Path: glyphs}.Op().Push(gtx.Ops)
	paint.ColorOp{Color: fg}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	cs.Pop()
	t.Pop()
	if o < 1 {
		opac.Pop()
	}
	op.Defer(gtx.Ops, m.Stop())
}

// tooltipStyle is the emoji tooltip's colors and opacity: Slack's, or a
// plugin's .c-tooltip__tip rule.
func tooltipStyle(p *Palette) (bg, fg color.NRGBA, opacity float32) {
	ts := slackc.BodyScope().Child(tooltipClass, 0)
	return ts.Background(p.TooltipBg), ts.Color(p.TooltipText), ts.Opacity()
}

// animator is implemented by imgcache.Cache: images that change over time.
type animator interface{ Animated(url string, px int) bool }

func animated(imgs Images, url string, px int) bool {
	a, ok := imgs.(animator)
	return ok && a.Animated(url, px)
}
