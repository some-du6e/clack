package richview

// An event block: Slack's Event (boot-shared 0lYF; CDN build
// 9f450b8ad3db), from client-boot-styles:
//
//   - the card (Card elevation 2, size medium; .p-event-container: 440px
//     at most, margin 8px 0: blocks.go's margins);
//   - EventHeader (.p-event_event-header, padding 12px): the calendar's
//     36px icon (radius 4; Google's for appType gcal, Outlook's for any
//     other), 10px before the summary (margin-top -4px): the title (15px
//     bold, "(No title)" without one, struck through when the event is
//     cancelled) over the date line (13px, content .7): "Event cancelled",
//     or a NOW pill while it's on (#007a5a on #e6f2ee) or an ENDED one
//     after it (#868686 on #f8f8f8), then "Mon Nov 23 at 3:00 – 4:00 PM"
//     (ddd, toCalendarDate with shortenMonth and shortenDay, the year
//     only when it isn't this one, the time24 pref's clock);
//   - BaseEventRSVPStatus (.p-event_event-join-call: padding 0 12px 12px):
//     user-groups, then the accepted guests' 20px pictures (the viewer's
//     first, eight at most, the eighth with how many more) with their
//     green check, over "{n} guests: 2 yes, 1 maybe, 1 no, 1 hasn't
//     responded" (13px, the counts content .7, a count of 0 left out);
//   - BaseEventJoinCall for a guest or the organizer: video, then the
//     meeting link (13px bold, the link colour; content .7 once it's past
//     or cancelled): "Join Zoom Meeting" while it's on, "Zoom Meeting"
//     otherwise, "No meeting link available" without one;
//   - BaseEventRSVP for a guest before it ends (.p-event_event-rsvp:
//     border-top, padding 0 16px, bold): "Going?" and Yes, No and Maybe
//     (13px, 5px 8px, radius 4, at right 100, 61 and 0 of the 126px
//     options; the guest's answer white on #1264a3);
//   - BaseEventCopyToCalendar for anyone else before it ends
//     (.p-event_copy-to-calendar: border-top, padding 16px): Copy to Your
//     Calendar (an outline button), or "Copied to your primary calendar!";
//   - BaseEventActions on hover (inset): Share event (share-message) and
//     More actions, whose menu opens the event on its calendar.
//
// Left out: the past event's grayscale icon (no image filter), the video
// icon's pulse while the event is on, and fetching the message again for the next set of
// attendees (needsRefetch).

import (
	"image"
	"image/color"
	"strconv"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slackasset"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

var nsEvent = i18n.NS("event")

// eventNow is the clock the card reads its state from (tests pin it).
var eventNow = time.Now

// EventHandler is optional on a Handler: an event card's action was
// clicked. act is "share" or "more" (the hover bar), "rsvp:accepted",
// "rsvp:declined" or "rsvp:tentative", or "copy"; ch and ts are the
// message's; btn is the button, in window coordinates when window, else
// in the view's plus Origin (the owner maps it).
type EventHandler interface {
	EventAction(act string, ev *richtext.Event, ch, ts string, btn image.Rectangle, window bool)
}

// Event card geometry (Dp).
const (
	evMaxW  = 440
	evPad   = 12
	evIcon  = 36
	evGap   = 10 // .p-event_row_left_side's margin-right
	evGlyph = 20 // the rows' icons and the guests' pictures
)

var (
	evNowFg   = color.NRGBA{0x00, 0x7a, 0x5a, 0xff}
	evNowBg   = color.NRGBA{0xe6, 0xf2, 0xee, 0xff}
	evEndFg   = color.NRGBA{0x86, 0x86, 0x86, 0xff}
	evEndBg   = color.NRGBA{0xf8, 0xf8, 0xf8, 0xff}
	evPicked  = color.NRGBA{0x12, 0x64, 0xa3, 0xff}
	evHoverBg = color.NRGBA{0xf8, 0xf8, 0xf8, 0xff}
	evIconCol = color.NRGBA{0x61, 0x60, 0x61, 0xff} // sk_dark_gray
)

// eventState is the card's moment: on (start <= now <= end), past (now
// after end).
// An invalid end (moment's invalid date) compares false either way: never
// on, never past.
func eventState(ev *richtext.Event, now time.Time) (on, past bool) {
	if ev.NoEnd {
		return false, false
	}
	s, e := time.Unix(ev.Start, 0), time.Unix(ev.End, 0)
	return !now.Before(s) && !now.After(e), now.After(e)
}

// EventKey is an event card's write (RSVP or Copy to Your Calendar,
// writes.go): its message and event.
func EventKey(ch, ts, id string) string { return "event\x00" + ch + "\x00" + ts + "\x00" + id }

// shouldExcludeYear is Nmg4's: no year for a date this year, or last year
// within the 31 days before now.
func shouldExcludeYear(t, now time.Time) bool {
	switch {
	case t.Year() > now.Year():
		return false
	case t.Year() == now.Year():
		return true
	}
	return now.Sub(t) <= 31*24*time.Hour
}

// eventRefresh notes when the card's state next changes (it starts, or
// ends): Slack's card reads the clock as it renders, Clack's lays out
// again then (drawEventRefresh) instead of on a frame loop.
func (l *lay) eventRefresh(ev *richtext.Event, now time.Time) {
	if ev.Cancelled || ev.NoEnd {
		return
	}
	var at time.Time
	switch s, e := time.Unix(ev.Start, 0), time.Unix(ev.End, 0); {
	case now.Before(s):
		at = s
	case !now.After(e):
		at = e.Add(time.Second)
	default:
		return
	}
	if l.d.refreshAt.IsZero() || at.Before(l.d.refreshAt) {
		l.d.refreshAt = at
	}
}

// drawEventRefresh lays the view out again once its event cards' state
// changes, asking for that frame only.
func (v *View) drawEventRefresh(gtx layout.Context) {
	at := v.d.refreshAt
	if at.IsZero() {
		return
	}
	if now := eventNow(); !now.Before(at) {
		v.d.refreshAt = time.Time{}
		v.dirty = true
		gtx.Execute(op.InvalidateCmd{})
		return
	}
	gtx.Execute(op.InvalidateCmd{At: at})
}

// calendarIcon is getCalAppIcon's image.
func calendarIcon(app string) string {
	if app == "gcal" {
		return slackasset.CalendarGoogle
	}
	return slackasset.CalendarOutlook
}

// calendarName is sVpJ's name for an appType.
func calendarName(app string) string {
	switch app {
	case "gcal":
		return "Google"
	case "ocal":
		return "Outlook"
	case "ocal_gcc":
		return "Outlook Calendar GCC High"
	}
	return ""
}

// EventCalendarLabel is BaseEventActions' menu item: "View on {calApp}"
// for Outlook GCC High, "View on {calApp} calendar" otherwise.
func EventCalendarLabel(app string) string {
	if app == "ocal_gcc" {
		return nsEvent.T("View on {calApp}", "calApp", calendarName(app))
	}
	return nsEvent.T("View on {calApp} calendar", "calApp", calendarName(app))
}

// eventDate is EventHeader's date line after its pills.
func eventDate(ev *richtext.Event, now time.Time) string {
	loc := i18n.Locale()
	s, e := time.Unix(ev.Start, 0), time.Unix(ev.End, 0)
	date := i18n.CalendarDate(loc, s, i18n.CalendarOpts{ShortenMonth: true, ShortenDay: true, ExcludeYear: shouldExcludeYear(s, now)})
	end := func(f string) string {
		if ev.NoEnd {
			return "Invalid date" // moment's for an invalid date
		}
		return i18n.FormatDayjs(loc, e, f)
	}
	var tm string
	if uipref.Get().Time24 {
		tm = i18n.FormatDayjs(loc, s, "HH:mm") + " – " + end("HH:mm")
	} else {
		tm = i18n.FormatDayjs(loc, s, "h:mm") + " – " + end("h:mm A")
	}
	return i18n.FormatDayjs(loc, s, "ddd") + " " + date + " at " + tm
}

// eventGuests is BaseEventRSVPStatus' line: "{n} guests:" and the counts.
func eventGuests(ev *richtext.Event) (head, counts string) {
	head = nsEvent.T("{attendeeCount, plural, =1 {# guest:} other {# guests:}}", "attendeeCount", ev.AttendeeCount)
	var parts []string
	if n := len(ev.Accepted); n > 0 {
		parts = append(parts, nsEvent.T("{yesCount} yes", "yesCount", n))
	}
	if ev.Maybe > 0 {
		parts = append(parts, nsEvent.T("{maybeCount} maybe", "maybeCount", ev.Maybe))
	}
	if ev.No > 0 {
		parts = append(parts, nsEvent.T("{noCount} no", "noCount", ev.No))
	}
	if ev.NoReply > 0 {
		parts = append(parts, nsEvent.T("{notResponded, plural, =1 {# hasn’t responded } other {# haven’t responded}}", "notResponded", ev.NoReply))
	}
	return head, strings.Join(parts, ", ")
}

// eventMeeting is BaseEventJoinCall's link text: "Join … Meeting" while
// the event is on (join), "… Meeting" otherwise.
func eventMeeting(provider string, join bool) string {
	if join {
		switch provider {
		case "slack":
			return nsEvent.T("Join Slack Meeting")
		case "zoom":
			return nsEvent.T("Join Zoom Meeting")
		case "hangout":
			return nsEvent.T("Join Hangout Meeting")
		case "webex":
			return nsEvent.T("Join Webex Meeting")
		case "msft_teams":
			return nsEvent.T("Join Microsoft Teams Meeting")
		case "bluejeans":
			return nsEvent.T("Join BlueJeans Meeting")
		case "google_meet":
			return nsEvent.T("Join Google Meet Meeting")
		case "chime":
			return nsEvent.T("Join Chime Meeting")
		case "skype":
			return nsEvent.T("Join Skype Meeting")
		}
		return nsEvent.T("Join Meeting")
	}
	switch provider {
	case "slack":
		return nsEvent.T("Slack Meeting")
	case "zoom":
		return nsEvent.T("Zoom Meeting")
	case "hangout":
		return nsEvent.T("Hangout Meeting")
	case "webex":
		return nsEvent.T("Webex Meeting")
	case "msft_teams":
		return nsEvent.T("Microsoft Teams Meeting")
	case "bluejeans":
		return nsEvent.T("BlueJeans Meeting")
	case "google_meet":
		return nsEvent.T("Google Meet Meeting")
	case "chime":
		return nsEvent.T("Chime Meeting")
	case "skype":
		return nsEvent.T("Skype Meeting")
	}
	return nsEvent.T("Meeting")
}

// event lays out an event block.
func (l *lay) event(b *richtext.Block, x, y, w int) int {
	ev := b.Event
	if ev == nil {
		return 0
	}
	p := &l.st.Palette
	now := eventNow()
	on, past := eventState(ev, now)
	done := past || ev.Cancelled
	if !ev.Attendee && !ev.Organizer && !done && !ev.CopiesOK {
		// BaseEventCopyToCalendar's copies.includes throws: the error line
		eb := richtext.Block{Kind: richtext.Unsupported, Kit: true, Spans: []richtext.Span{{Kind: richtext.Text, Text: "This content could not be displayed."}}}
		return l.block1(&eb, x, y, w, false)
	}
	l.eventRefresh(ev, now)
	cw := min(w, l.dp(evMaxW))
	pad, ic := l.dp(evPad), l.dp(evIcon)
	bi := len(l.d.boxes)
	l.d.boxes = append(l.d.boxes, box{kind: bBorder, fill: p.Panel, stroke: p.Border, bw: l.dp(1), radius: l.dp(12), target: -1})

	// EventHeader
	r4 := l.dp(4)
	l.d.imgs = append(l.d.imgs, imgPrim{r: image.Rect(x+pad, y+pad, x+pad+ic, y+pad+ic), url: calendarIcon(ev.AppType), radii: [4]int{r4, r4, r4, r4}, target: -1, px: ic})
	tx := x + pad + ic + l.dp(evGap)
	tw := max(x+cw-pad-tx, 1)
	ty := y + pad - l.dp(4)
	title := ev.Title
	if title == "" {
		title = nsEvent.T("(No title)")
	}
	ts := l.bodyStyle()
	ts.face.Weight = font.Bold
	var st richtext.Style
	if ev.Cancelled {
		st = richtext.Strike
	}
	l.plainAdd("\n")
	l.addText(title, &ts, l.faceFor(ts.face, ts.size, 0), ts.col, st, -1, -1)
	th, _ := l.flow(tx, ty, tw, &ts)
	ds := l.small(p.Text2)
	ds.lineH = l.dp(19) // 13px at 1.46668
	dy := ty + th - l.dp(1)
	var dh int
	if ev.Cancelled {
		l.plainAdd("\n")
		dh, _ = l.plainFlow(nsEvent.T("Event cancelled"), tx, dy, tw, &ds, -1)
	} else {
		lead := 0
		if on || past {
			label, fg, bg := nsEvent.T("now"), evNowFg, evNowBg
			if past {
				label, fg, bg = nsEvent.T("ended"), evEndFg, evEndBg
			}
			lead = l.eventPill(strings.ToUpper(label), fg, bg, tx, dy, ds.lineH)
		}
		l.plainAdd("\n")
		l.lead = float32(lead)
		dh, _ = l.plainFlow(eventDate(ev, now), tx, dy, tw, &ds, -1)
	}
	yy := max(y+pad+ic, dy+dh) + pad

	// BaseEventRSVPStatus
	yy = l.eventRow(yy, x, tx, tw, "user-groups", func(ry int) int {
		h := 0
		if len(ev.Accepted) > 0 {
			h = l.eventFaces(ev, tx, ry)
		}
		head, counts := eventGuests(ev)
		gs := l.small(p.Text)
		gs.lineH = l.dp(19)
		k := l.faceFor(gs.face, gs.size, 0)
		l.plainAdd("\n")
		l.addText(head+" ", &gs, k, p.Text, 0, -1, -1)
		l.addText(counts, &gs, k, p.Text2, 0, -1, -1)
		gh, _ := l.flow(tx, ry+h-l.dp(2), tw, &gs)
		return h - l.dp(2) + gh
	})

	// BaseEventJoinCall
	if ev.Attendee || ev.Organizer {
		yy = l.eventRow(yy, x, tx, tw, "video", func(ry int) int {
			js := l.small(p.Link)
			js.lineH = l.dp(19)
			if ev.MeetingURL == "" {
				js.col = p.Text2
				l.plainAdd("\n")
				h, _ := l.plainFlow(nsEvent.T("No meeting link available"), tx, ry-l.dp(5), tw, &js, -1)
				return h - l.dp(5)
			}
			js.face.Weight = font.Bold
			if done {
				js.col = p.Text2
			}
			tg := l.d.addTarget(target{kind: tLink, url: ev.MeetingURL, underline: true})
			l.plainAdd("\n")
			h, sw := l.plainFlow(eventMeeting(ev.MeetingProvider, on && !done), tx, ry, tw, &js, tg)
			l.d.hits = append(l.d.hits, hit{r: image.Rect(tx, ry, tx+sw, ry+h), t: tg})
			return h + l.dp(2)
		})
	}

	switch {
	case ev.Attendee && !done:
		yy = l.eventRSVP(ev, x, yy, cw)
	case !ev.Attendee && !ev.Organizer && !done:
		yy = l.eventCopy(ev, x, yy, cw)
	}
	r := image.Rect(x, y, x+cw, yy)
	l.d.boxes[bi].r = r
	if !l.opts.NoHoverCards && !l.opts.Canvas {
		l.d.bars = append(l.d.bars, fileBar{card: r, event: ev, inset: 12, group: true, btns: []barBtn{
			{cact: "share", w: 32, h: 32, icon: "share-message", tip: nsEvent.T("Share event")},
			{cact: "more", w: 32, h: 32, icon: "ellipsis-vertical-filled", tip: nsBlockKit.T("More actions"), right: true},
		}})
	}
	return yy - y
}

// eventPill draws a NOW or ENDED pill at the start of the date line at
// (x, y) and returns the width the line's text starts after
// (.p-event_status: radius 4, padding .5px 3px 1.5px, 8px after it;
// its text 10px bold, upper case).
func (l *lay) eventPill(label string, fg, bg color.NRGBA, x, y, lineH int) int {
	ps := l.small(fg)
	ps.size, ps.lineH = l.sp(10), l.dp(15)
	ps.face.Weight = font.Bold
	ps.noSelect = true
	tw := int(l.measure(label, l.faceFor(ps.face, ps.size, 0)) + 0.999)
	bh := l.dp(17)
	by := y + (lineH-bh)/2
	l.d.boxes = append(l.d.boxes, box{kind: bFill, r: image.Rect(x, by, x+tw+l.dp(6), by+bh), radius: l.dp(4), fill: bg, target: -1})
	l.plainAdd("\n")
	l.plainFlow(label, x+l.dp(3), by+l.dp(1), tw+l.dp(2), &ps, -1)
	return tw + l.dp(6) + l.dp(8)
}

// eventRow is a .p-event_event-join-call row at y: the icon in the 36px
// column, the content (body, laid out at its top) beside it; it returns
// the row's bottom (12px under the taller side).
func (l *lay) eventRow(y, x, tx, tw int, icon string, body func(ry int) int) int {
	g := l.dp(evGlyph)
	ix := x + l.dp(evPad) + (l.dp(evIcon)-g)/2
	iy := y + l.dp(1)
	l.d.imgs = append(l.d.imgs, imgPrim{r: image.Rect(ix, iy, ix+g, iy+g), url: iconURL + icon, target: -1, px: g, tint: evIconCol})
	h := body(y)
	return y + max(h, l.dp(22)) + l.dp(evPad)
}

// eventFaces draws the accepted guests' pictures at (x, y), eight at most,
// each with its check, and returns the line's height. The check is
// .p-event-avatar_badge's check-circle-filled, an SvgIcon with no size:
// 1em of the badge's 12px, on its base-pry circle (border-radius 9999px),
// at bottom 1px right 1px of the avatar's p-event-avatar_wrapper, an
// inline span whose bottom is the 15px line's descent (3px) under the
// avatar.
func (l *lay) eventFaces(ev *richtext.Event, x, y int) int {
	p := &l.st.Palette
	g := l.dp(evGlyph)
	fy := y + (l.dp(22)-g)/2
	for i, gu := range ev.Accepted {
		if i == 8 {
			break
		}
		fx := x + i*(g+l.dp(4))
		r := image.Rect(fx, fy, fx+g, fy+g)
		rad := l.dp(4)
		tg := l.d.addTarget(target{kind: tUser, id: gu.UserID})
		if gu.Avatar != "" {
			l.d.imgs = append(l.d.imgs, imgPrim{r: r, url: gu.Avatar, radii: [4]int{rad, rad, rad, rad}, target: tg, px: g, onFail: DefaultAvatar})
		} else {
			l.d.boxes = append(l.d.boxes, box{kind: bFill, r: r, radius: rad, fill: p.Placeholder, target: tg})
		}
		l.d.hits = append(l.d.hits, hit{r: r, t: tg})
		if i == 7 && len(ev.Accepted) > 8 { // additionalOverlay: how many more
			// .c-avatar__additional_overlay: 15px bold on #00000080, "9+"
			// past nine (BaseAvatar's renderAdditionalOverlay)
			l.d.boxes = append(l.d.boxes, box{kind: bFill, r: r, radius: rad, fill: color.NRGBA{0, 0, 0, 0x80}, target: -1})
			os := l.small(color.NRGBA{0xff, 0xff, 0xff, 0xff})
			os.size, os.lineH, os.noSelect = l.sp(15), g, true
			os.face.Weight = font.Bold
			more := len(ev.Accepted) - 8
			n := "+" + strconv.Itoa(more)
			if more > 9 {
				n = "9+"
			}
			nw := int(l.measure(n, l.faceFor(os.face, os.size, 0)) + 0.999)
			l.plainAdd("\n")
			l.plainFlow(n, fx+(g-nw)/2, fy, nw+l.dp(2), &os, -1)
		}
		b := l.dp(10) // MUTANT
		bx, by := r.Max.X-l.dp(1)-b, r.Max.Y+l.dp(3)-l.dp(1)-b
		l.d.boxes = append(l.d.boxes, box{kind: bFill, r: image.Rect(bx, by, bx+b, by+b), radius: b / 2, fill: p.Panel, target: -1})
		l.d.imgs = append(l.d.imgs, imgPrim{r: image.Rect(bx, by, bx+b, by+b), url: iconURL + "check-circle-filled", target: -1, px: b, tint: evNowFg})
	}
	return l.dp(22)
}

// eventRSVP is BaseEventRSVP at y: the row and its border; it returns the
// row's bottom.
func (l *lay) eventRSVP(ev *richtext.Event, x, y, cw int) int {
	p := &l.st.Palette
	l.d.boxes = append(l.d.boxes, box{kind: bFill, r: image.Rect(x, y, x+cw, y+l.dp(1)), fill: p.Border, target: -1})
	rowH := l.dp(51) // 16px, a 13px line, 16px
	qs := l.small(p.Text)
	qs.lineH = l.dp(19)
	qs.face.Weight = font.Bold
	l.plainAdd("\n")
	l.plainFlow(nsEvent.T("Going?"), x+l.dp(16), y+l.dp(16), max(cw-l.dp(32+126+12), 1), &qs, -1)
	right := x + cw - l.dp(16)
	oy := y + (rowH-l.dp(32))/2
	for _, o := range []struct {
		rsvp, label string
		min, at     float32
	}{
		{"accepted", nsEvent.T("Yes"), 40, 100},
		{"declined", nsEvent.T("No"), 30, 61},
		{"tentative", nsEvent.T("Maybe"), 50, 0},
	} {
		bs := l.small(p.Link)
		bs.lineH = l.dp(19)
		bs.face.Weight = font.Bold
		bs.noSelect = true
		lw := int(l.measure(o.label, l.faceFor(bs.face, bs.size, 0)) + 0.999)
		bw := max(l.dp(o.min), lw+l.dp(16))
		r := image.Rect(right-l.dp(o.at)-bw, oy, right-l.dp(o.at), oy+l.dp(29))
		tg := l.d.addTarget(target{kind: tEvent, id: "rsvp:" + o.rsvp, event: ev})
		bg := p.Panel
		if ev.RSVP == o.rsvp { // .p-event_rsvp--selection
			l.d.boxes = append(l.d.boxes, box{kind: bFill, r: r, radius: l.dp(4), fill: evPicked, target: tg})
			bs.col = color.NRGBA{0xff, 0xff, 0xff, 0xff}
			bg = evPicked
		} else {
			l.d.boxes = append(l.d.boxes, box{kind: bFill, r: r, radius: l.dp(4), target: tg, hover: evHoverBg, hoverOver: true})
		}
		// BaseEventRSVP: all three disabled while a call is out, the
		// clicked one's spinner (writes.go)
		l.d.busyBtns = append(l.d.busyBtns, busyBtn{r: r, radius: l.dp(4), key: EventKey(l.opts.Channel, l.opts.TS, ev.ID), act: "rsvp:" + o.rsvp, bg: bg})
		l.plainAdd("\n")
		l.plainFlow(o.label, r.Min.X+(bw-lw)/2, r.Min.Y+l.dp(5), lw+l.dp(2), &bs, tg)
		l.d.hits = append(l.d.hits, hit{r: r, t: tg})
	}
	return y + rowH
}

// eventCopy is BaseEventCopyToCalendar at y: Copy to Your Calendar (an
// outline button) or "Copied to your primary calendar!"; it returns the
// row's bottom.
func (l *lay) eventCopy(ev *richtext.Event, x, y, cw int) int {
	p := &l.st.Palette
	l.d.boxes = append(l.d.boxes, box{kind: bFill, r: image.Rect(x, y, x+cw, y+l.dp(1)), fill: p.Border, target: -1})
	pad := l.dp(16)
	if ev.Copied {
		fs := l.bodyStyle()
		l.plainAdd("\n")
		h, _ := l.plainFlow(nsEvent.T("Copied to your primary calendar!"), x+pad, y+pad, max(cw-2*pad, 1), &fs, -1)
		return y + pad + h + pad
	}
	label := nsEvent.T("Copy to Your Calendar")
	bs := l.bodyStyle()
	bs.face.Weight = font.Bold
	bh := l.dp(36)
	bs.lineH, bs.maxLines, bs.ellipsis, bs.noSelect = bh, 1, true, true
	lw := int(l.measure(label, l.faceFor(bs.face, bs.size, 0)) + 0.999)
	bw := min(lw+l.dp(24), cw-2*pad)
	r := image.Rect(x+pad, y+pad, x+pad+bw, y+pad+bh)
	tg := l.d.addTarget(target{kind: tEvent, id: "copy", event: ev})
	l.d.boxes = append(l.d.boxes, box{kind: bBorder, r: r, radius: l.dp(8), fill: p.Panel, stroke: p.ButtonBorder, bw: l.dp(1), target: tg, hover: p.ButtonHoverBg})
	// Button's loading while copyToCalendar is out (writes.go)
	l.d.busyBtns = append(l.d.busyBtns, busyBtn{r: r.Inset(l.dp(1)), radius: l.dp(7), key: EventKey(l.opts.Channel, l.opts.TS, ev.ID), act: "copy", bg: p.Panel})
	l.plainAdd("\n")
	l.plainFlow(label, r.Min.X+l.dp(12), r.Min.Y-l.dp(1), max(bw-l.dp(24), 1)+l.dp(2), &bs, tg)
	l.d.hits = append(l.d.hits, hit{r: r, t: tg})
	return y + pad + bh + pad
}

// EventRect is a laid out event card (tests): the card, its in-card
// buttons by action ("rsvp:accepted", …, "copy") and its hover bar's
// ("share", "more"), view-local.
type EventRect struct {
	Event   *richtext.Event
	Card    image.Rectangle
	Buttons map[string]image.Rectangle
	Actions map[string]image.Rectangle
}

// EventRects lists the view's event cards (tests).
func (v *View) EventRects() []EventRect {
	var out []EventRect
	for i := range v.d.bars {
		b := &v.d.bars[i]
		if b.event == nil {
			continue
		}
		_, rs := v.barRects(i)
		e := EventRect{Event: b.event, Card: b.card, Buttons: map[string]image.Rectangle{}, Actions: map[string]image.Rectangle{}}
		for k, bt := range b.btns {
			e.Actions[bt.cact] = rs[k]
		}
		for j := range v.d.hits {
			hr := &v.d.hits[j]
			if hr.t >= 0 && int(hr.t) < len(v.d.targets) {
				if tg := &v.d.targets[hr.t]; tg.kind == tEvent && tg.event == b.event {
					e.Buttons[tg.id] = hr.r
				}
			}
		}
		out = append(out, e)
	}
	return out
}
