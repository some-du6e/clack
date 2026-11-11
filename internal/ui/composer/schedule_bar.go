package composer

import (
	"image"
	"math"
	"time"

	"gioui.org/font"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// The scheduled-send context bar (boot-shared 07q3
// ContextBarScheduledSendContent, which useContextBarOverride picks): once
// a message here is scheduled, i.e. there are more scheduled messages for
// the conversation (or thread) than when it was opened or the soonest one
// was created in the last 5 seconds, and until the conversation is left,
// the bar above the composer says "Your message will be sent today at
// 9:00 AM." ("… tomorrow at …", "… on {date} at {time}." with formatDate's
// {date_pretty}), or "{N} messages scheduled to be sent." for several,
// then the bold link "See all scheduled messages"
// (navigateToScheduledMessages: Drafts & sent's Scheduled tab). The bar
// is .p-context_bar_v2: the soft foreground fill, top corners 8px,
// padding 12px, the schedule-send icon (16px, 12px before the text,
// 1px down) and .p-context_bar_v2__content (13/18); its 6px filler runs
// under the composer's top edge. An agent channel shows its own bar
// outside threads instead.

// scheduledBar is useContextBarOverride's scheduledSend state.
type scheduledBar struct {
	seen    int       // the count when the conversation was opened (p)
	init    bool      // seen holds a count from loaded drafts
	lastLay time.Time // the last layout: a gap means the conversation was left
	see     widget.Clickable
}

// scheduledBarText is ContextBarScheduledSendContent's sentence for msgs
// (soonest first), "" for none.
func scheduledBarText(msgs []core.ScheduledMessage, now time.Time, h24 bool) string {
	switch {
	case len(msgs) == 0:
		return ""
	case len(msgs) > 1:
		return i18n.T("messages", "{draft_count} messages scheduled to be sent.", "draft_count", len(msgs))
	}
	at := time.Unix(msgs[0].PostAt, 0).In(now.Location())
	tm := i18n.Time(i18n.Locale(), at, h24, false)
	switch {
	case sameDate(at, now):
		return i18n.T("messages", "Your message will be sent today at {time}.", "time", tm)
	case sameDate(at, now.AddDate(0, 0, 1)):
		return i18n.T("messages", "Your message will be sent tomorrow at {time}.", "time", tm)
	}
	return i18n.T("messages", "Your message will be sent on {date} at {time}.", "date", datePretty(at, now), "time", tm)
}

// datePretty is formatDate's {date_pretty} without capitals: "today",
// "tomorrow", else toCalendarDate ("November 18th", the year only beyond
// about six months).
func datePretty(t, now time.Time) string {
	loc := i18n.Locale()
	if r := i18n.RelativeDay(loc, t, now, true); r != "" {
		return r
	}
	excl := math.Abs(now.Sub(t).Minutes()) < 262080
	return i18n.CalendarDate(loc, t, i18n.CalendarOpts{ExcludeYear: excl})
}

// scheduledBarShown is the scheduledSend override's rule.
func (b *scheduledBar) shown(msgs []core.ScheduledMessage, loaded bool, now time.Time) bool {
	if !loaded {
		return false
	}
	if !b.init || now.Sub(b.lastLay) > time.Second {
		// opened (or came back to it): the count to compare with
		b.seen, b.init = len(msgs), true
	}
	b.lastLay = now
	if len(msgs) == 0 {
		return false
	}
	recent := time.Unix(msgs[0].DateCreated, 0).After(now.Add(-5 * time.Second))
	return len(msgs) > b.seen || recent
}

// layoutScheduledBar draws the bar when it shows (zero dimensions
// otherwise).
func (c *Composer) layoutScheduledBar(gtx layout.Context) layout.Dimensions {
	a := c.api.a
	if a == nil || c.edit != nil {
		return layout.Dimensions{}
	}
	if c.ts == "" {
		if cv, ok := c.api.conv(c.ch); ok && cv.Code != nil {
			return layout.Dimensions{} // an agent channel's own bar
		}
	}
	msgs, loaded := a.ScheduledFor(c.ch, c.ts)
	now := time.Now()
	if !c.sbar.shown(msgs, loaded, now) {
		return layout.Dimensions{}
	}
	if len(msgs) > 0 {
		if recent := time.Unix(msgs[0].DateCreated, 0).Add(5 * time.Second); recent.After(now) {
			gtx.Execute(op.InvalidateCmd{At: recent}) // the 5-second window closes
		}
	}
	if c.sbar.see.Clicked(gtx) {
		c.h.Navigate("drafts?tab=scheduled") // navigateToScheduledMessages
	}
	th := c.h.Theme()
	w := gtx.Constraints.Max.X
	pad := gtx.Dp(12)
	text := scheduledBarText(msgs, now, uipref.Get().Time24)
	st := textStyle{Size: 13, LineHeight: 18, Color: th.C.Fg}
	link := st
	link.Weight, link.Color = font.Bold, th.C.Link

	// the content, recorded to size the bar
	tx := pad + gtx.Dp(16) + gtx.Dp(12)
	tw := max(w-tx-pad, 1)
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(tw, 1<<20)}
	dd, lineW := richFlow(g, th.Shaper, []richSpan{{text + " ", st}})
	call := m.Stop()
	// the link after the sentence (its own run so it is the clickable)
	lm := op.Record(gtx.Ops)
	ld := c.sbar.see.Layout(g, func(gtx layout.Context) layout.Dimensions {
		pointer.CursorPointer.Add(gtx.Ops)
		return label(gtx, th.Shaper, link, i18n.T("messages", "See all scheduled messages"))
	})
	lcall := lm.Stop()
	lh := gtx.Sp(18)
	lx, ly := lineW, dd.Size.Y-lh // the link follows the sentence's last line
	if lx+ld.Size.X > tw {
		lx, ly = 0, dd.Size.Y
	}
	contentH := max(dd.Size.Y, ly+lh)
	H := contentH + 2*pad

	bg := withAlpha(th.C.Fg, 0x0f) // --sk_foreground_soft_solid over the base
	rr := clip.RRect{Rect: image.Rect(0, 0, w, H+gtx.Dp(6)), NW: gtx.Dp(8), NE: gtx.Dp(8)}
	paint.FillShape(gtx.Ops, bg, rr.Op(gtx.Ops))
	off := op.Offset(image.Pt(pad, pad+gtx.Dp(1))).Push(gtx.Ops)
	icons.Get("schedule-send")(gtx, unit.Dp(16), th.C.FgMuted)
	off.Pop()
	off = op.Offset(image.Pt(tx, pad)).Push(gtx.Ops)
	call.Add(gtx.Ops)
	lo := op.Offset(image.Pt(lx, ly)).Push(gtx.Ops)
	lcall.Add(gtx.Ops)
	lo.Pop()
	off.Pop()
	return layout.Dimensions{Size: image.Pt(w, H)}
}
