package messages

import (
	"context"
	"image"
	"image/color"
	"strconv"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The message pane's floating banners and the day pills' menu, ported
// from Slack's MessagePane (gantry-v2-async-client-boot-render bU1n,
// build 4d0c78085232) under its IA4 styles (client-boot-styles):
//
//   - UnreadBanner (renderUnreadBanner → nV → UnreadBanner →
//     UnreadBannerButtons) sits in .p-message_pane__top_banners (14px
//     under the pane, 16px in from each side, centred) while the channel
//     has unreads and a last-read ts (isValid), not in archived channels.
//     The text is "{n} new message(s)", n the loaded messages after the
//     last read when the history reaches the newest (unreadCountKnown),
//     else the unread count with a "+" (floored to tens above 10); 0, or
//     1 when unknown, is "New messages". An arrow-up glyph when the "New"
//     line is above the view (getUnreadLinePlacement 1), else arrow-down;
//     the text acts on mouse-down (jumpOrMark): with the line in view it
//     marks the channel read, else it jumps to the line. Its tooltip is
//     "Jump" with ⌘J (the desktop app), the ✕'s "Mark as read" with Esc,
//     both below after 300 ms. When the unreads go the buttons fade out
//     in 80 ms (easeInQuad), after 1 s while the last read is a ghost;
//     marking read hides it at once. The unread_threads_in_channel_nav
//     variant (a dot and a caret menu of unread stops) isn't drawn: the
//     account isn't in it and Clack has no unread stops.
//   - ArchivesBanner's LatestMessagesJumpBanner (no AI summaries here):
//     "Latest messages" with arrow-down, centred 16px over the bottom,
//     only while the loaded history stops short of the newest message
//     (reachedEnd === false). Clack loads newest first, so it shows when
//     a window misses the newest page (model.newestMissing).
//   - DayDividerJumper: every day pill opens "Jump to…" (bottom-left, 5px
//     under the pill): dayDividerJumperMenuOptions, a separator and "Jump
//     to a specific date".

// unreadBannerState is UnreadBanner's state.
type unreadBannerState struct {
	msg, close       widget.Clickable
	msgTip, closeTip popups.TipState
	shown            bool      // state.hasUnreads: up, the fade included
	fadeAt           time.Time // when the fade starts (zero: not fading)
	marked           string    // the newest ts the banner marked read: hidden until the counts catch up
	ch               string

	// what the last frame drew (tests): the text, the arrow, the two
	// buttons' rects (list-local)
	text, icon   string
	msgR, closeR image.Rectangle
}

const (
	bannerFade      = 80 * time.Millisecond
	bannerGhostWait = time.Second
)

var (
	bannerBlue      = rgb(0x1264a3)
	bannerBlueHover = rgb(0x0b4c8c)
)

// bannerText is UnreadBannerButtons' text for n unread messages.
func bannerText(n int, known bool) string {
	certainty := ""
	if !known {
		certainty = "+"
		if n > 10 {
			n = 10 * (n / 10)
		}
	}
	switch {
	case n == 0, n == 1 && !known:
		return "New messages"
	case n == 1:
		return "1 new message"
	}
	return strconv.Itoa(n) + certainty + " new messages"
}

// unreadPlacement is the list's getUnreadLinePlacement: 1 above the view,
// 2 in it, 3 below; with no "New" line, 3 when the last read is past the
// newest message, else 1.
func (s *listState) unreadPlacement(since, newest string) int {
	ui := s.unreadIndex()
	switch {
	case ui < 0:
		if newest != "" && since != "" && tsLess(newest, since) {
			return 3
		}
		return 1
	case ui < s.visible[0]:
		return 1
	case ui < s.visible[1]:
		return 2
	}
	return 3
}

// newestTS is the newest loaded message's ts.
func (s *listState) newestTS() string {
	if n := len(s.snap.msgs); n > 0 {
		return s.snap.msgs[n-1].m.TS
	}
	return ""
}

// unreadBanner draws UnreadBanner for a channel list.
func (s *listState) unreadBanner(gtx layout.Context, h Host, rc *rowCtx, size image.Point) {
	ub := &s.ub
	app := h.Core()
	if ub.ch != s.ch {
		*ub = unreadBannerState{ch: s.ch}
	}
	if app == nil || s.thread || !s.snap.loaded || len(s.snap.msgs) == 0 {
		ub.shown, ub.fadeAt = false, time.Time{}
		return
	}
	if c, ok := app.S.Conv(s.ch); ok && c.IsArchived {
		return
	}
	cnt := app.Count(s.ch)
	since, ghost := s.snap.lastRead, false
	if s.openLastRead != "" && s.openLastRead != since { // the ghost last read the "New" line keeps
		since, ghost = s.openLastRead, true
	}
	newest := s.newestTS()
	has := cnt.Unread && tsPositive(since)
	if ub.marked != "" {
		if !cnt.Unread || tsLess(ub.marked, newest) {
			ub.marked = ""
		} else {
			has = false
			ub.shown, ub.fadeAt = false, time.Time{}
		}
	}
	now := gtx.Now
	opacity := float32(1)
	switch {
	case has:
		ub.shown, ub.fadeAt = true, time.Time{}
	case ub.shown && ub.fadeAt.IsZero():
		ub.fadeAt = now
		if ghost {
			ub.fadeAt = now.Add(bannerGhostWait)
		}
		fallthrough
	case ub.shown:
		t := float32(now.Sub(ub.fadeAt)) / float32(bannerFade)
		switch {
		case t >= 1:
			ub.shown, ub.fadeAt = false, time.Time{}
		case t > 0:
			opacity = 1 - t*t // easeInQuad from 1 to 0
		}
		if ub.shown {
			gtx.Execute(op.InvalidateCmd{})
		}
	}
	if !ub.shown {
		ub.text = ""
		return
	}
	n := 0 // the loaded messages after the last read (unreadCountKnown: Clack holds the newest)
	for i := range s.snap.msgs {
		if tsLess(since, s.snap.msgs[i].m.TS) {
			n++
		}
	}
	text := bannerText(n, true)
	place := s.unreadPlacement(since, newest)
	icon := "arrow-down"
	if place == 1 {
		icon = "arrow-up"
	}

	act := func(mark bool) {
		if mark {
			ub.marked, ub.shown, ub.fadeAt = newest, false, time.Time{}
			ch := s.ch
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				_ = markRead(ctx, app, ch, newest) // markMostRecentMsgRead
			}()
		} else if ui := s.unreadIndex(); ui >= 0 { // jumpToUnread
			s.list.Position = layout.Position{BeforeEnd: true, First: max(0, ui-1)}
		}
		h.Invalidate()
	}
	if pressedNow(&ub.msg, gtx) {
		act(place == 2)
	}
	if ub.close.Clicked(gtx) {
		act(true)
	}

	th := h.Theme()
	ts := textStyle{size: 10 * 4 / 3.0, weight: theme.Bold, col: rgb(0xffffff), lineH: 28} // 10pt
	tw := measure(gtx, th, ts, text).X
	bh := dp(gtx, 28)
	iconBox := dp(gtx, 20) // .c-icon--inline
	msgW := dp(gtx, 12) + iconBox + dp(gtx, 4) + tw + dp(gtx, 12)
	closeW := dp(gtx, 4) + iconBox + dp(gtx, 8)
	bw := msgW + closeW
	inner := size.X - 2*dp(gtx, 16)
	x := dp(gtx, 16) + (inner-bw)/2
	y := dp(gtx, 14)
	r := image.Rect(x, y, x+bw, y+bh)
	// box-shadow: 0 4px 12px #00000014
	softShadow(gtx.Ops, r.Add(image.Pt(0, dp(gtx, 4))), bh/2, dp(gtx, 12), color.NRGBA{A: 0x14})
	rrect(gtx.Ops, r, bh/2, bannerBlue)

	half := func(btn *widget.Clickable, br image.Rectangle, left bool, draw func(gtx layout.Context)) {
		at(gtx, br.Min.X, br.Min.Y, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(br.Size())
			return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				if btn.Hovered() {
					rad := bh / 2
					rr := clip.RRect{Rect: image.Rectangle{Max: br.Size()}}
					if left {
						rr.NW, rr.SW = rad, rad
					} else {
						rr.NE, rr.SE = rad, rad
					}
					paint.FillShape(gtx.Ops, bannerBlueHover, rr.Op(gtx.Ops))
				}
				if opacity < 1 {
					defer paint.PushOpacity(gtx.Ops, opacity).Pop()
				}
				draw(gtx)
				return layout.Dimensions{Size: br.Size()}
			})
		})
	}
	white := rgb(0xffffff)
	msgR := image.Rect(r.Min.X, r.Min.Y, r.Min.X+msgW, r.Max.Y)
	closeR := image.Rect(msgR.Max.X, r.Min.Y, r.Max.X, r.Max.Y)
	ub.text, ub.icon, ub.msgR, ub.closeR = text, icon, msgR, closeR
	gs := dp(gtx, 13.333) // the glyph is 1em
	half(&ub.msg, msgR, true, func(gtx layout.Context) {
		at(gtx, dp(gtx, 12)+(iconBox-gs)/2, (bh-gs)/2, func(gtx layout.Context) layout.Dimensions {
			return icons.Get(icon)(gtx, unit.Dp(13.333), white)
		})
		at(gtx, dp(gtx, 12)+iconBox+dp(gtx, 4), 0, func(gtx layout.Context) layout.Dimensions { return label(gtx, th, ts, text) })
	})
	cg := dp(gtx, 13) // .p-message_pane__unread_banner__close_icon{font-size:13px}
	half(&ub.close, closeR, false, func(gtx layout.Context) {
		at(gtx, dp(gtx, 4)+(iconBox-cg)/2, (bh-cg)/2, func(gtx layout.Context) layout.Dimensions {
			return icons.Get("close")(gtx, unit.Dp(13), white)
		})
	})
	ub.msgTip.Delay, ub.closeTip.Delay = 300*time.Millisecond, 300*time.Millisecond
	at(gtx, msgR.Min.X, msgR.Min.Y, func(gtx layout.Context) layout.Dimensions {
		popups.Tooltip(gtx, th, &ub.msgTip, ub.msg.Hovered(), msgR.Size(), popups.Below, "Jump", "Mod+J")
		return layout.Dimensions{}
	})
	at(gtx, closeR.Min.X, closeR.Min.Y, func(gtx layout.Context) layout.Dimensions {
		popups.Tooltip(gtx, th, &ub.closeTip, ub.close.Hovered(), closeR.Size(), popups.Below, "Mark as read", "Esc")
		return layout.Dimensions{}
	})
}

// tsPositive reports a last-read ts after 0 (Slack's since > 0).
func tsPositive(ts string) bool {
	f, err := strconv.ParseFloat(ts, 64)
	return err == nil && f > 0
}

// pressedNow reports a pointer press on btn this frame (Slack acts on
// mouse-down there): its newest press started now and hasn't ended.
func pressedNow(btn *widget.Clickable, gtx layout.Context) bool {
	btn.Clicked(gtx) // run its events: History records the press
	hs := btn.History()
	if len(hs) == 0 {
		return false
	}
	p := hs[len(hs)-1]
	return p.Start.Equal(gtx.Now) && p.End.IsZero()
}

// softShadow is a blurred rounded shadow under r (blur px): a few
// widening translucent layers.
func softShadow(ops *op.Ops, r image.Rectangle, radius, blur int, col color.NRGBA) {
	const steps = 4
	for i := steps; i >= 1; i-- {
		g := blur * i / (2 * steps)
		c := col
		c.A = uint8(int(col.A) * 2 / (steps + 1))
		rrect(ops, r.Inset(-g), radius+g, c)
	}
}

// archivesBanner is LatestMessagesJumpBanner (.p-archives_banner under
// IA4): a #1264a3 pill, 28px, radius 14, padding 8 left and 16 right,
// 13px bold white, arrow-down (margin_right_25) and "Latest messages",
// centred 16px over the bottom, the same blue under the pointer and in
// dark mode (no theme rule overrides it).
func (s *listState) archivesBanner(gtx layout.Context, h Host, size image.Point) {
	s.latestR = image.Rectangle{}
	if s.thread || !s.snap.loaded || len(s.snap.msgs) == 0 || !s.snap.newestMissing {
		return
	}
	th := h.Theme()
	white := rgb(0xffffff)
	ts := textStyle{size: 13, weight: theme.Bold, col: white, lineH: 28}
	txt := "Latest messages"
	sz := measure(gtx, th, ts, txt)
	bh := dp(gtx, 28)
	iconBox := dp(gtx, 20)
	bw := dp(gtx, 8) + iconBox + dp(gtx, 4) + sz.X + dp(gtx, 16)
	r := image.Rect((size.X-bw)/2, size.Y-bh-dp(gtx, 16), (size.X-bw)/2+bw, size.Y-dp(gtx, 16))
	s.latestR = r
	if s.jump.Clicked(gtx) { // jumpToMostRecent
		s.list.Position = layout.Position{}
		h.Invalidate()
	}
	at(gtx, r.Min.X, r.Min.Y, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(r.Size())
		return s.jump.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			rrect(gtx.Ops, image.Rectangle{Max: r.Size()}, bh/2, bannerBlue)
			gs := dp(gtx, 13)
			at(gtx, dp(gtx, 8)+(iconBox-gs)/2, (bh-gs)/2, func(gtx layout.Context) layout.Dimensions {
				return icons.Get("arrow-down")(gtx, unit.Dp(13), white)
			})
			at(gtx, dp(gtx, 8)+iconBox+dp(gtx, 4), 0, func(gtx layout.Context) layout.Dimensions { return label(gtx, th, ts, txt) })
			return layout.Dimensions{Size: r.Size()}
		})
	})
}

// dayJumpOption is one of dayDividerJumperMenuOptions' rows: when is the
// time to jump to (zero: the most recent messages).
type dayJumpOption struct {
	label string
	when  time.Time
}

// dayJumpOptions is dayDividerJumperMenuOptions: the first row is "Most
// recent", or "Today" / "Yesterday" (that day's start) when the latest
// message is from then; "Yesterday" unless the divider is yesterday's,
// the first row says it already, or the history has no yesterday; "Last
// week" (the start of last week) and "Last month" (the start of last
// month) when the latest message is after them; "The very beginning"
// (the channel's creation) unless the divider is that day.
func dayJumpOptions(divider, latest, created, now time.Time, hasYesterday bool) []dayJumpOption {
	day := func(t time.Time) time.Time { return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location()) }
	yesterday := day(now).AddDate(0, 0, -1)
	dividerYesterday := sameDay(divider, yesterday)
	dividerCreated := sameDay(divider, created)
	latestToday := sameDay(now, latest)
	latestYesterday := sameDay(latest, yesterday)
	// moment's startOf("week") (en-US: Sunday) a week ago, startOf("month") a month ago
	wk := day(now.AddDate(0, 0, -7))
	wk = wk.AddDate(0, 0, -int(wk.Weekday()))
	m := now.AddDate(0, -1, 0)
	mo := time.Date(m.Year(), m.Month(), 1, 0, 0, 0, 0, now.Location())
	first := dayJumpOption{label: "Most recent"}
	if latestToday {
		first = dayJumpOption{label: "Today", when: day(now)}
	}
	if latestYesterday {
		first = dayJumpOption{label: "Yesterday", when: yesterday}
	}
	out := []dayJumpOption{first}
	if !dividerYesterday && !latestYesterday && hasYesterday {
		out = append(out, dayJumpOption{label: "Yesterday", when: yesterday})
	}
	if latest.After(wk) {
		out = append(out, dayJumpOption{label: "Last week", when: wk})
	}
	if latest.After(mo) {
		out = append(out, dayJumpOption{label: "Last month", when: mo})
	}
	if !dividerCreated {
		out = append(out, dayJumpOption{label: "The very beginning", when: created})
	}
	return out
}

// hasYesterday is hasYesterdayInTimestamps over the loaded messages: the
// newest is yesterday's, or it is today's and the oldest isn't.
func (s *listState) hasYesterday(now time.Time) bool {
	n := len(s.snap.msgs)
	if n == 0 {
		return false
	}
	last := tsTime(s.snap.msgs[n-1].m.TS)
	if sameDay(last, now.AddDate(0, 0, -1)) {
		return true
	}
	if !sameDay(last, now) {
		return false
	}
	return !sameDay(tsTime(s.snap.msgs[0].m.TS), now)
}

// dayMenuReq is a day pill clicked this frame: its day, the item it sits
// in (-1: the sticky pill) and its rect (item-local; list-local for the
// sticky one).
type dayMenuReq struct {
	day  time.Time
	item int
	r    image.Rectangle
}

// openDayMenu shows the pill's "Jump to…" menu (bottom-left, offsetY 5).
func (s *listState) openDayMenu(h Host, req dayMenuReq, now time.Time, gtx layout.Context) {
	r := req.r
	if req.item >= 0 {
		if req.item >= len(s.pos) {
			return
		}
		r = r.Add(image.Pt(0, s.pos[req.item]))
	}
	r = r.Add(s.origin)
	created := time.Unix(0, 0) // E.Ay.unix(channel.created || 0)
	if app := h.Core(); app != nil {
		if c, ok := app.S.Conv(s.ch); ok && c.Created > 0 {
			created = time.Unix(c.Created, 0)
		}
	}
	latest := tsTime(s.newestTS())
	opts := dayJumpOptions(req.day, latest, created, now, s.hasYesterday(now))
	items := []popups.MenuItem{{Header: "Jump to…"}}
	for _, o := range opts {
		when := o.when
		items = append(items, popups.MenuItem{Label: o.label, Do: func() { s.jumpToTime(h, when) }})
	}
	items = append(items, popups.Sep(), popups.MenuItem{Label: "Jump to a specific date", Do: func() {
		// JumpToDateModal's calendar: dates after today and before the
		// channel's creation are disabled; picking one jumps there
		h.ShowOverlay(popups.NewDatePicker(h, r, time.Now(), created, func(d time.Time) { s.jumpToTime(h, d) }))
	}})
	anchor := r.Add(image.Pt(0, dp(gtx, 5)))
	h.ShowOverlay(popups.NewMenuOpts(h, items, popups.MenuOpts{Anchor: anchor, Side: popups.Below}))
}

// jumpToTime is jumpToTimestamp: the zero time is the most recent
// messages, else the first message at or after t, paging back until it
// is loaded (layoutList's pendingDate).
func (s *listState) jumpToTime(h Host, t time.Time) {
	if t.IsZero() {
		s.list.Position = layout.Position{}
		s.pendingDate = time.Time{}
	} else {
		s.pendingDate, s.jumpPages = t, 0
	}
	h.Invalidate()
}

// pendingDateJump pages back until a message at or before pendingDate is
// loaded (or history ends), then shows the first message at or after it.
func (s *listState) pendingDateJump(now time.Time) {
	if s.pendingDate.IsZero() || s.thread || !s.snap.loaded || s.snap.older {
		return
	}
	msgs := s.snap.msgs
	if len(msgs) > 0 && s.snap.hasMore && tsTime(msgs[0].m.TS).After(s.pendingDate) && s.jumpPages < 20 {
		if now.Sub(s.lastOlder) > 300*time.Millisecond {
			s.jumpPages++
			s.lastOlder = now
			s.hb.loadOlder(s.m)
		}
		return
	}
	for i := range msgs {
		if !tsTime(msgs[i].m.TS).Before(s.pendingDate) {
			s.scrollTo(msgs[i].m.TS)
			break
		}
	}
	s.pendingDate = time.Time{}
}
