package richview

import (
	"image"
	"image/color"
	"strconv"
	"strings"
	"time"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// tooltipClass is Slack's tooltip box (a portal under <body>).
var tooltipClass = slackc.Class("c-tooltip__tip c-tooltip__tip--top", "role", "tooltip")

// EmojiTipDelay is how long the pointer rests on an emoji before Slack
// shows its :name: (the emoji's delay="300" tooltip).
const EmojiTipDelay = 300 * time.Millisecond

// editedTipKey marks the "(edited)" word's entry in display.tips: its
// text is the edit's time, written when the tooltip shows (editedTip).
const editedTipKey = "\x00edited"

// emojiAt returns the frag with a tooltip under p (view coordinates): an
// emoji image, or the "(edited)" label; -1 for none.
func (v *View) emojiAt(p f32.Point) int {
	for i := range v.d.frags {
		f := &v.d.frags[i]
		if f.tip == 0 {
			continue
		}
		switch f.kind {
		case fImage:
			if p.X >= f.x && p.X < f.x+float32(f.size) && p.Y >= float32(f.y) && p.Y < float32(f.y+f.size) {
				return i
			}
		case fText:
			if p.X >= f.x && p.X < f.x+f.w && p.Y >= float32(f.top) && p.Y < float32(f.bot) {
				return i
			}
		}
	}
	return -1
}

// HoveredEmoji is the :name: of the emoji under the pointer, if any (not
// the "(edited)" label's or a message card time's tooltip).
func (v *View) HoveredEmoji() (string, bool) {
	name, ok := v.hoveredTip()
	if !ok || name == editedTipKey || !strings.HasPrefix(name, ":") {
		return "", false
	}
	return name, true
}

// hoveredTip is the display.tips entry of the frag under the pointer.
func (v *View) hoveredTip() (string, bool) {
	if v.tipFrag < 0 || v.tipFrag >= len(v.d.frags) {
		return "", false
	}
	f := &v.d.frags[v.tipFrag]
	if f.tip == 0 || int(f.tip) > len(v.d.tips) {
		return "", false
	}
	return v.d.tips[f.tip-1], true
}

// editedTip is EditedLabel's tooltip (boot-shared Y5SQ; MessageText's
// renderEditedLabel is the same): "{date} at {time}", the date
// toCalendarDateOrNameDay(edited.ts, {shortenMonth}): "Today",
// "Yesterday" or "Tomorrow" (maybeGetRelativeDay), else "Nov 7th", with
// ", 2025" unless shouldExcludeYear (this year, or last year within 31
// days); the time in the time24 pref's clock.
func editedTip(ts string, now time.Time) string {
	t, date, ok := calendarDay(ts, now)
	if !ok {
		return ""
	}
	return date + " at " + uipref.Clock(t)
}

// fullTimestamp is getFullTimestamp (boot-shared n8t9), the tooltip of a
// message card's time (AttachmentFooterTimestamp with compactTime: delay
// 300): editedTip's "{date} at {time}" with the seconds.
func fullTimestamp(ts string, now time.Time) string {
	t, date, ok := calendarDay(ts, now)
	if !ok {
		return ""
	}
	if l := i18n.Locale(); l != i18n.EnUS {
		return date + " at " + i18n.Time(l, t, uipref.Get().Time24, true)
	}
	return date + " at " + t.Format(uipref.Layout("3:04:05 PM"))
}

// calendarDay is ts as a time and toCalendarDateOrNameDay's date for it
// (shortenMonth): "Today", "Yesterday", "Tomorrow", else "Nov 7th" with
// ", 2025" unless shouldExcludeYear.
func calendarDay(ts string, now time.Time) (time.Time, string, bool) {
	sec, _, _ := strings.Cut(ts, ".")
	n, err := strconv.ParseInt(sec, 10, 64)
	if err != nil || n <= 0 {
		return time.Time{}, "", false
	}
	t := time.Unix(n, 0).In(now.Location())
	day := func(x time.Time) time.Time { y, m, d := x.Date(); return time.Date(y, m, d, 0, 0, 0, 0, x.Location()) }
	var date string
	switch day(t).Sub(day(now)).Round(time.Hour) {
	case 0:
		date = "Today"
	case -24 * time.Hour:
		date = "Yesterday"
	case 24 * time.Hour:
		date = "Tomorrow"
	default:
		date = t.Format("Jan 2") + ordinal(t.Day())
		exclude := t.Year() == now.Year() || (t.Year() < now.Year() && now.Sub(t) <= 31*24*time.Hour)
		if !exclude {
			date += t.Format(", 2006")
		}
	}
	return t, date, true
}

// emojiTip draws the hovered emoji's :name: (or the "(edited)" label's
// time) in Slack's dark tooltip, centred above it, once the pointer has
// rested EmojiTipDelay. Deferred, so it sits above the rest of the window
// and outside every clip.
func (v *View) emojiTip(gtx layout.Context, st *Style) {
	name, ok := v.hoveredTip()
	if !ok || !v.hovering {
		return
	}
	if wait := v.tipAt.Add(EmojiTipDelay); !gtx.Now.IsZero() && gtx.Now.Before(wait) {
		gtx.Execute(op.InvalidateCmd{At: wait})
		return
	}
	f := &v.d.frags[v.tipFrag]
	// the trigger's box: an emoji image, or the label's word (its line box)
	cxf, top := f.x+float32(f.size)/2, f.y
	if f.kind == fText {
		cxf, top = f.x+f.w/2, f.top
	}
	if name == editedTipKey {
		now := gtx.Now
		if now.IsZero() {
			now = time.Now()
		}
		if name = editedTip(v.opts.EditedTS, now); name == "" {
			return
		}
	}
	l := lay{st: st, d: &v.d, scale: v.laidPx, spx: v.laidSp}
	if l.scale <= 0 {
		l.scale, l.spx = 1, 1
	}
	k := l.faceFor(font.Font{Typeface: st.Face, Weight: font.Bold}, l.sp(13), 0)
	sh := st.word(k, name)
	padX, padY, lh := l.dp(12), l.dp(8), l.dp(18)
	bw, bh := int(sh.adv+0.999)+2*padX, lh+2*padY
	cx := int(cxf)
	gap, ar := l.dp(8), l.dp(6)
	r := image.Rect(cx-bw/2, top-gap-bh, cx-bw/2+bw, top-gap)
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
	ax, ay := float32(cx), float32(top-gap)
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
