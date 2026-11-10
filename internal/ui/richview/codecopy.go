package richview

import (
	"image"
	"io"
	"strings"
	"time"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/clipboard"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/icons"
)

// Copying code, ported from Slack's web client (build a84662d2d135,
// gantry-v2-async-client-boot-shared):
//
//   - CopyCodeBlockButton (6xeA): every code block (rich_text_preformatted
//     and mrkdwn ```, both <pre class="c-mrkdwn__pre" data-copy-code-block>)
//     ends with a small SlackKit IconButton (28px, 8px radius, the "copy"
//     icon, tooltip "Copy") in span.container__nrkPh: position absolute,
//     top 4px, right 4px, opacity 0 until the pre is hovered. The pre gets
//     padding-right 32px for it. A click copies getCodeBlockText (jR3m: the
//     pre's text without its data-stringify-ignore children, trailing
//     newlines dropped), and useCopyCodeToClipboard (or0I) shows the
//     "check" icon and "Copied!" for 2 seconds.
//   - BaseCopyCodeBlockContextMenuTrigger (StVK i): a right-click on inline
//     code or a code block hands the desktop shell one item, "Copy this
//     code" or "Copy this code block", which copies getCodeRun(el) (/r2K:
//     the code element and its continuations, code split by formatting)
//     through getCodeBlockText. Hit tests for it: CodeAt.
//   - selectContentsOnTripleMouseDown (StVK m): a triple-click inside code
//     selects the whole code run or block (selectLine).

const (
	copyPadR    = 32 // .c-mrkdwn__pre[data-copy-code-block] padding-right
	copyBtnSize = 28 // .c-icon_button--size_small
	copyInset   = 4  // .container__nrkPh top/right
	copyIconPx  = 16 // calc(.5 * 28px + 2px)
	copyRadius  = 8  // --sk_button-styles-radius
	copiedFor   = 2 * time.Second
)

// codeRun is a run of inline code or a code block: its plain-text range
// and, for a block, the pre's box.
type codeRun struct {
	p0, p1 int32
	block  bool
	r      image.Rectangle
}

// addCodeRun records inline code [p0, p1); code continuing the previous
// run (a code element split by bold or a link) joins it, as getCodeRun.
func (l *lay) addCodeRun(p0, p1 int32) {
	if p1 <= p0 {
		return
	}
	if n := len(l.d.codes); n > 0 {
		if c := &l.d.codes[n-1]; !c.block && c.p1 == p0 {
			c.p1 = p1
			return
		}
	}
	l.d.codes = append(l.d.codes, codeRun{p0: p0, p1: p1})
}

// codeText is getCodeBlockText for run c: its text, trailing newlines
// dropped.
func (v *View) codeText(c *codeRun) string {
	n := int32(len(v.d.plain))
	a, b := min(c.p0, n), min(c.p1, n)
	if a >= b {
		return ""
	}
	return strings.TrimRight(string(v.d.plain[a:b]), "\n")
}

// codeAt returns the code run under p, or -1.
func (v *View) codeAt(p f32.Point) int {
	pt := image.Pt(int(p.X), int(p.Y))
	for i := range v.d.codes {
		c := &v.d.codes[i]
		if c.block {
			if pt.In(c.r) {
				return i
			}
			continue
		}
		// the <code> element: its frags' union on each line, spaces
		// between words included
		var r image.Rectangle
		line := int32(-1)
		for fi := range v.d.frags {
			f := &v.d.frags[fi]
			if f.p0 < c.p0 || f.p1 > c.p1 || f.p0 >= f.p1 {
				continue
			}
			if f.line != line {
				if pt.In(r) {
					return i
				}
				r, line = image.Rectangle{}, f.line
			}
			r = r.Union(fragRect(f))
		}
		if pt.In(r) {
			return i
		}
	}
	return -1
}

// CodeAt reports the code under p (View coordinates): its copy text and
// whether it is a code block rather than inline code.
func (v *View) CodeAt(p f32.Point) (text string, block, ok bool) {
	i := v.codeAt(p)
	if i < 0 {
		return "", false, false
	}
	c := &v.d.codes[i]
	return v.codeText(c), c.block, true
}

// codeRange is the code run around plain offset off (triple-click), if
// any.
func (v *View) codeRange(off int32) (int32, int32, bool) {
	for i := range v.d.codes {
		c := &v.d.codes[i]
		if off >= c.p0 && off < c.p1 {
			t := v.codeText(c)
			return c.p0, c.p0 + int32(len(t)), true
		}
	}
	return 0, 0, false
}

func (v *View) px(dp int) int {
	s := v.laidPx
	if s <= 0 {
		s = 1
	}
	return int(float32(dp)*s + 0.5)
}

// copyButton is block run i's button rect.
func (v *View) copyButton(c *codeRun) image.Rectangle {
	in, sz := v.px(copyInset), v.px(copyBtnSize)
	return image.Rect(c.r.Max.X-in-sz, c.r.Min.Y+in, c.r.Max.X-in, c.r.Min.Y+in+sz)
}

// copyButtonAt is the code block whose copy button is under p, or -1.
// The button only takes input while it shows (its pre is hovered).
func (v *View) copyButtonAt(p f32.Point) int {
	pt := image.Pt(int(p.X), int(p.Y))
	for i := range v.d.codes {
		if c := &v.d.codes[i]; c.block && pt.In(v.copyButton(c)) {
			return i
		}
	}
	return -1
}

// OnCopyButton reports whether p is on a code block's copy button (the
// owner of an External selection leaves such presses to the View).
func (v *View) OnCopyButton(p f32.Point) bool { return v.copyButtonAt(p) >= 0 }

// copyBlock puts block i's text on the clipboard and shows "Copied!".
func (v *View) copyBlock(gtx layout.Context, i int) {
	t := v.codeText(&v.d.codes[i])
	gtx.Execute(clipboard.WriteCmd{Type: "application/text", Data: io.NopCloser(strings.NewReader(t))})
	v.copied, v.copiedAt = i+1, gtx.Now
	v.copyTipAt = gtx.Now // Slack's announcer and the tooltip switch at once
	gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(copiedFor)})
}

func (v *View) copiedNow(i int, now time.Time) bool {
	return v.copied == i+1 && now.Before(v.copiedAt.Add(copiedFor))
}

// drawCopyButtons draws the hovered code block's copy button (and its
// tooltip while the pointer rests on it).
func (v *View) drawCopyButtons(gtx layout.Context, st *Style) {
	if !v.hovering {
		return
	}
	pt := image.Pt(int(v.ptr.X), int(v.ptr.Y))
	p := &st.Palette
	for i := range v.d.codes {
		c := &v.d.codes[i]
		if !c.block || !pt.In(c.r) {
			continue
		}
		br := v.copyButton(c)
		hot := pt.In(br)
		col := p.Text3 // .c-icon_button--light: content-sec
		if hot {
			col = p.Text // :hover: content-pry on base-ter
			paint.FillShape(gtx.Ops, p.Placeholder, clip.UniformRRect(br, v.px(copyRadius)).Op(gtx.Ops))
		}
		name, tip := "copy", "Copy"
		if v.copiedNow(i, gtx.Now) {
			name, tip = "check", "Copied!"
		}
		ip := v.px(copyIconPx)
		o := br.Min.Add(image.Pt((br.Dx()-ip)/2, (br.Dy()-ip)/2))
		t := op.Offset(o).Push(gtx.Ops)
		g := gtx
		g.Constraints = layout.Exact(image.Pt(ip, ip))
		g.Metric = unit.Metric{PxPerDp: 1, PxPerSp: 1}
		icons.GetSlack(name)(g, unit.Dp(ip), col)
		t.Pop()
		if hot {
			v.copyTip(gtx, st, br, tip)
		}
	}
}

// copyTip is the button's tooltip, centred above it after the pointer
// rests EmojiTipDelay (deferred, above everything).
func (v *View) copyTip(gtx layout.Context, st *Style, br image.Rectangle, tip string) {
	if wait := v.copyTipAt.Add(EmojiTipDelay); !gtx.Now.IsZero() && gtx.Now.Before(wait) {
		gtx.Execute(op.InvalidateCmd{At: wait})
		return
	}
	l := lay{st: st, d: &v.d, scale: v.laidPx, spx: v.laidSp}
	if l.scale <= 0 {
		l.scale, l.spx = 1, 1
	}
	k := l.faceFor(font.Font{Typeface: st.Face, Weight: font.Bold}, l.sp(13), 0)
	sh := st.word(k, tip)
	padX, padY, lh := l.dp(12), l.dp(8), l.dp(18)
	bw, bh := int(sh.adv+0.999)+2*padX, lh+2*padY
	cx := (br.Min.X + br.Max.X) / 2
	gap, ar := l.dp(8), l.dp(6)
	r := image.Rect(cx-bw/2, br.Min.Y-gap-bh, cx-bw/2+bw, br.Min.Y-gap)
	bg, fg, o := tooltipStyle(&st.Palette)
	m := op.Record(gtx.Ops)
	var opac paint.OpacityStack
	if o < 1 {
		opac = paint.PushOpacity(gtx.Ops, max(o, 0))
	}
	paint.FillShape(gtx.Ops, bg, clip.UniformRRect(r, l.dp(8)).Op(gtx.Ops))
	var path clip.Path
	path.Begin(gtx.Ops)
	ax, ay := float32(cx), float32(br.Min.Y-gap)
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
