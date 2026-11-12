package views

import (
	"image"
	"image/color"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// The live part of the page: HuddlesActiveSection (Igs2) with its
// HuddlesActiveTile cards (shared gF2E) and SuggestedHuddlesTile cards
// (Igs2), in .p-huddles_page_history_hhv__active_section (padding-top
// 20). The grid follows the page's width (the huddlePageRef
// ResizeObserver): up to 428 one card a row, up to 640 two
// (calc(50% - 8px)), else three (calc(33.333% - 8px)), spaced between;
// a card is 200 tall with 16 below. At most that many live huddles show
// (three above 640) until "Show more live huddles"; the huddle you're in
// comes first. Then, unless you're in a huddle or hide_suggested_huddles
// is on, suggested huddles fill the row from the last week's history.

// The section's breakpoints (ew) and the tiles' (ew too).
const (
	hdSmallBP  = 428
	hdMediumBP = 640
)

// liveLimits is useMemo's maxToRender and how many live huddles show.
func (v *Huddles) liveLimits(gtx C, n int) (maxCards, shown int) {
	x := v.pageW
	switch {
	case x <= gtx.Dp(hdSmallBP):
		maxCards = 1
	case x <= gtx.Dp(hdMediumBP):
		maxCards = 2
	}
	shown = n
	if !v.moreLive {
		lim := 3
		if maxCards > 0 {
			lim = maxCards
		}
		shown = min(n, lim)
	}
	return maxCards, shown
}

// hdCard is one card in the active section's grid.
type hdCard struct {
	live    *huddleRoom // a live huddle (its huddles.get info)
	room    huddleRoom  // the live room (participants now)
	suggest *core.Huddle
	count   int // a suggestion's huddles in the last week
}

func (v *Huddles) layoutActive(gtx C, st pageState) D {
	w := gtx.Constraints.Max.X
	x, cw := hdColumn(gtx, w)
	all := v.pageLive()
	maxCards, shown := v.liveLimits(gtx, len(all))
	var cards []hdCard
	liveChans := map[string]bool{}
	for _, r := range all[:shown] {
		liveChans[roomChannel(r)] = true
		info, ok := v.huddleInfo(r)
		if !ok {
			continue // HuddlesActiveTile renders nothing until huddles.get lands
		}
		if len(info.Channels) == 0 {
			info.Channels = r.Channels
		}
		if roomChannel(info) == "" {
			continue
		}
		in := info
		cards = append(cards, hdCard{live: &in, room: r})
	}
	if v.activeChannel() == "" && len(st.unfiltered) > 0 && shown < 3 && !v.exp("hide_suggested_huddles") {
		sug := v.suggestedHuddles(st.unfiltered, liveChans, gtx.Now)
		lim := 3 - shown
		if maxCards > 0 {
			lim = max(0, maxCards-shown)
		}
		for i := range sug {
			if i >= lim {
				break
			}
			cards = append(cards, hdCard{suggest: &sug[i].h, count: sug[i].n})
		}
	}
	cols, colW := 1, cw
	switch {
	case v.pageW > gtx.Dp(hdMediumBP):
		cols, colW = 3, cw*33333/100000-gtx.Dp(8)
	case v.pageW > gtx.Dp(hdSmallBP):
		cols, colW = 2, cw/2-gtx.Dp(8)
	}
	gapX := 0
	if cols > 1 {
		gapX = (cw - cols*colW) / (cols - 1)
	}
	cardH, below := gtx.Dp(200), gtx.Dp(16)
	top := gtx.Dp(20)
	y := top
	for i, c := range cards {
		cx := x + (i%cols)*(colW+gapX)
		cy := top + (i/cols)*(cardH+below)
		func() {
			defer opOffset(gtx, image.Pt(cx, cy)).Pop()
			g := gtx
			g.Constraints = layout.Exact(image.Pt(colW, cardH))
			if c.live != nil {
				v.liveTile(g, *c.live, c.room, maxCards)
			} else {
				v.suggestedTile(g, *c.suggest, c.count, maxCards)
			}
		}()
		y = cy + cardH + below
	}
	// the overflow toggle: when a live huddle shows (or you're in one)
	// and there are more than fit, or the list is expanded
	if (shown > 0 || v.activeChannel() != "") && (v.moreLive || len(all) > shown) {
		if v.clk("more-live").Clicked(gtx) {
			v.moreLive = !v.moreLive
		}
		d := v.overflowToggle(gtx, x, y, cw)
		y += d.Size.Y
	}
	return D{Size: image.Pt(w, y)}
}

// overflowToggle is the section's overflow (.overflow__WQ0Wb: centred,
// gap 12): a 40px otl-ter line, "Show more live huddles" / "Show fewer
// live huddles" (13px/18px bold) with the caret, another line.
func (v *Huddles) overflowToggle(gtx C, x, y, cw int) D {
	label := nsHuddles.T("Show more live huddles")
	caret := "caret-down"
	if v.moreLive {
		label, caret = nsHuddles.T("Show fewer live huddles"), "caret-up"
	}
	fg := v.tok("dt_color-content-pry")
	tw := v.th.Measure(gtx, 13, wBold, label)
	bw := tw + gtx.Dp(20)
	lw, gap := gtx.Dp(40), gtx.Dp(12)
	total := lw + gap + bw + gap + lw
	h := gtx.Dp(20)
	x0 := x + (cw-total)/2
	line := v.tokOver("dt_color-otl-ter", v.tok("dt_color-base-sec"))
	px1 := max(1, gtx.Dp(1))
	fill(gtx, image.Rect(x0, y+h/2, x0+lw, y+h/2+px1), line)
	fill(gtx, image.Rect(x0+total-lw, y+h/2, x0+total, y+h/2+px1), line)
	c := v.clk("more-live")
	func() {
		defer opOffset(gtx, image.Pt(x0+lw+gap, y)).Pop()
		c.Layout(gtx, func(gtx C) D {
			pointerCursor(gtx)
			g := gtx
			g.Constraints = layout.Exact(image.Pt(bw, h))
			row(g, layout.Middle,
				layout.Rigid(func(gtx C) D { return v.label(gtx, 13, wBold, fg, label) }),
				layout.Rigid(func(gtx C) D { return v.icon(gtx, caret, 20, fg) }),
			)
			return D{Size: image.Pt(bw, h)}
		})
	}()
	return D{Size: image.Pt(cw, h)}
}

// ---- the live huddle card ----

// liveTile is HuddlesActiveTile: a 200px card (1px otl-ter, radius 8,
// base-pry; sk_primary_background once you're in it), its 136px header
// the huddle's background picture (previewUrl, cover) with the timer pill
// and the participants (40px faces, 8 apart, centred 10px down, at most
// three, two up to 640), then (padding 8) the topic or the channel's name
// with its icon (bold, a link to the conversation), "N people" (or the
// channel under a topic) with "· N replies", and LiveHuddleButton. A click
// on the card opens JoinLiveHuddlesModal.
func (v *Huddles) liveTile(gtx C, h huddleRoom, room huddleRoom, maxCards int) D {
	sz := gtx.Constraints.Max
	ch := roomChannel(h)
	in := ch != "" && ch == v.activeChannel()
	key := "live:" + h.ID
	card := v.clk(key)
	name := v.clk(key + ":name")
	replies := v.clk(key + ":replies")
	btn := v.clk(key + ":join")
	inner := false
	if name.Clicked(gtx) {
		inner = true
		v.h.Navigate("c/" + ch)
	}
	if replies.Clicked(gtx) {
		inner = true
		if h.ThreadTS != "" {
			v.h.OpenThread(ch, h.ThreadTS)
		}
	}
	if btn.Clicked(gtx) {
		inner = true
		if in {
			if l, ok := v.h.(huddleLeaver); ok {
				l.LeaveHuddle()
			}
		} else {
			v.joinClicked(h.ID)
		}
	}
	if card.Clicked(gtx) && !inner && !in {
		v.openJoinLive(ch, h)
	}

	rad := gtx.Dp(8)
	r := image.Rectangle{Max: sz}
	bg := v.tok("dt_color-base-pry") // sk_primary_background once joined: the same in both modes
	people := room.Participants
	if len(people) == 0 {
		people = h.Participants
	}
	// the card is a 200px flex column: the 136px header gives way to the
	// content (padding 8) down to its facepile's 50px
	content := recordTo(gtx, func(gtx C) D {
		gtx.Constraints = layout.Constraints{Max: image.Pt(sz.X-gtx.Dp(16), sz.Y)}
		return v.liveTileContent(gtx, h, len(people), in, name, replies, btn)
	})
	hh := max(gtx.Dp(50), min(gtx.Dp(136), sz.Y-content.d.Size.Y-gtx.Dp(16)))
	card.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		func() {
			defer clipRR(gtx, r, rad, rad, rad, rad).Pop()
			fill(gtx, r, bg)
			if url := v.backgroundURL(h.BackgroundID, true); url != "" {
				v.cdnImage(gtx, url, image.Pt(sz.X, hh))
			}
			// the timer: 10px down, 9px in, 4px 8px, radius 12, 11px bold
			v.timerPill(gtx, h.Start, image.Pt(gtx.Dp(9), gtx.Dp(10)))
			faces := 3
			if maxCards > 1 && v.pageW > gtx.Dp(hdSmallBP) && v.pageW <= gtx.Dp(hdMediumBP) {
				faces = 2
			}
			func() {
				defer clip.Rect(image.Rect(0, 0, sz.X, hh)).Push(gtx.Ops).Pop()
				v.facepile(gtx, people, 40, faces, image.Rect(0, gtx.Dp(10), sz.X, hh+gtx.Dp(10)))
			}()
			func() {
				defer opOffset(gtx, image.Pt(gtx.Dp(8), hh+gtx.Dp(8))).Pop()
				content.call.Add(gtx.Ops)
			}()
		}()
		border(gtx, r, rad, max(1, gtx.Dp(1)), v.tokOver("dt_color-otl-ter", bg))
		return D{Size: sz}
	})
	return D{Size: sz}
}

// liveTileContent is the card's lower part (.p-huddles_active_tile__content_v3).
func (v *Huddles) liveTileContent(gtx C, h huddleRoom, n int, in bool, name, replies, btn *widget.Clickable) D {
	w := gtx.Constraints.Max.X
	fg := v.tok("dt_color-content-pry")
	ter := v.tok("dt_color-content-ter")
	ch := roomChannel(h)
	return col(gtx,
		layout.Rigid(func(gtx C) D {
			return name.Layout(gtx, func(gtx C) D {
				pointerCursor(gtx)
				if h.Name != "" {
					return v.label(gtx, 15, wBold, fg, h.Name)
				}
				return v.channelNameWithIcon(gtx, ch, h.History, 15, wBold, fg, true)
			})
		}),
		layout.Rigid(func(gtx C) D {
			// .p-huddles_active_tile__reply_count: content-ter, 13px, gap 4, 8px below
			d := row(gtx, layout.Middle,
				layout.Rigid(func(gtx C) D {
					if h.Name != "" {
						return v.channelNameWithIcon(gtx, ch, h.History, 13, wReg, fg, false)
					}
					return v.label(gtx, 13, wReg, fg, nsHuddles.T("{memberCount, plural, =1 {# person} other {# people}}", "memberCount", n))
				}),
				layout.Rigid(func(gtx C) D {
					c := v.replyCount(h)
					if c <= 0 {
						return D{}
					}
					return row(gtx, layout.Middle,
						hgap(4),
						layout.Rigid(func(gtx C) D { return v.dot(gtx, 2, ter) }),
						hgap(4),
						layout.Rigid(func(gtx C) D {
							return v.linkText(gtx, replies, 13, nsHuddles.T("{replyCount, plural, =1 {# reply} other {# replies}}", "replyCount", c))
						}),
					)
				}),
			)
			return D{Size: image.Pt(d.Size.X, d.Size.Y+gtx.Dp(8))}
		}),
		layout.Rigid(func(gtx C) D {
			if in {
				return v.liveButton(gtx, btn, w, in)
			}
			return v.joinPoint(gtx, h, func(gtx C) D { return v.liveButton(gtx, btn, w, in) })
		}),
	)
}

// liveButton is LiveHuddleButton: a full-width 30px button (radius 5,
// padding 6px 12px, 13px): "Join Huddle" (primary); in the huddle, the
// headphones and "Joined" on rgba(foreground_low, .13) at
// foreground_max .7, turning base-inv-imp "Leave" under the pointer.
func (v *Huddles) liveButton(gtx C, c *widget.Clickable, w int, in bool) D {
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		sz := image.Pt(w, gtx.Dp(30))
		r := image.Rectangle{Max: sz}
		rad := gtx.Dp(5)
		var bg, fg color.NRGBA
		label, icon := nsHuddles.T("Join Huddle"), ""
		switch {
		case in && c.Hovered():
			bg, fg, label = v.tok("dt_color-base-inv-imp"), v.tok("dt_color-constants-white"), "Leave"
		case in:
			bg = v.sk("sk_foreground_low", 0x21, v.tok("dt_color-base-pry"))
			fg = v.sk("sk_foreground_max", 0xb3, bg)
			label, icon = nsHuddles.T("Joined"), "headphones"
		default:
			bg, fg = v.tok("dt_color-base-inv-hgl-2"), v.tok("dt_color-content-inv-pry")
			if c.Hovered() {
				bg = v.p.GreenHover
			}
		}
		rrect(gtx, r, rad, bg)
		x := gtx.Dp(12)
		if icon != "" {
			func() {
				defer opOffset(gtx, image.Pt(x, (sz.Y-gtx.Dp(20))/2)).Pop()
				v.icon(gtx, icon, 20, fg)
			}()
			x += gtx.Dp(20)
		}
		x += gtx.Dp(4) // margin_left_25
		func() {
			defer opOffset(gtx, image.Pt(x, 0)).Pop()
			g := gtx
			g.Constraints = layout.Exact(image.Pt(max(0, w-x-gtx.Dp(12)), sz.Y))
			layout.W.Layout(g, func(gtx C) D { return v.label(gtx, 13, wBold, fg, label) })
		}()
		return D{Size: sz}
	})
}

// timerPill is HuddlesActiveTimer (.timer__VafhL): the huddle's age,
// refreshed every second.
func (v *Huddles) timerPill(gtx C, start int64, at image.Point) {
	if start <= 0 {
		return
	}
	s := hdDuration(gtx.Now.Unix() - start)
	gtx.Execute(op.InvalidateCmd{At: gtx.Now.Truncate(time.Second).Add(time.Second)})
	tw := v.th.Measure(gtx, 11, wBold, s)
	sz := image.Pt(tw+gtx.Dp(16), gtx.Dp(11*1.36)+gtx.Dp(8))
	defer opOffset(gtx, at).Pop()
	rrect(gtx, image.Rectangle{Max: sz}, gtx.Dp(12), v.tok("dt_color-base-inv-pry"))
	g := gtx
	g.Constraints = layout.Exact(sz)
	layout.Center.Layout(g, func(gtx C) D { return v.label(gtx, 11, wBold, v.tok("dt_color-content-inv-pry"), s) })
}

// hdDuration is ZvaB's duration: weeks, days, hours, minutes and (when
// nonzero, or alone) seconds, joined by spaces with dayjs's relativeTime
// units ("1 hour 4 min 2 sec").
func hdDuration(secs int64) string {
	if secs < 0 {
		secs = 0
	}
	wk := secs / (7 * 86400)
	d := (secs / 86400) % 7
	h := (secs / 3600) % 24
	m := (secs / 60) % 60
	s := secs % 60
	var parts []string
	unit := func(n int64, one, many string) {
		if n == 0 {
			return
		}
		f := many
		if n == 1 {
			f = one
		}
		parts = append(parts, strings.Replace(f, "%d", formatInt(n), 1))
	}
	unit(wk, "%d week", "%d weeks")
	unit(d, "%d day", "%d days")
	unit(h, "%d hour", "%d hours")
	unit(m, "%d min", "%d min")
	if s != 0 || len(parts) == 0 {
		parts = append(parts, strings.Replace("%d sec", "%d", formatInt(s), 1))
	}
	return strings.Join(parts, " ")
}

// replyCount is useHuddleThreadReplyCount (ekET): the huddle message's
// reply_count, 0 without a thread.
func (v *Huddles) replyCount(h huddleRoom) int {
	if h.ThreadTS == "" || h.ThreadTS == "0000000000.000000" || roomChannel(h) == "" {
		return 0
	}
	k := roomChannel(h) + "/" + h.ThreadTS
	now := time.Now()
	if r, ok := v.replies[k]; ok && now.Sub(r.at) < 5*time.Second {
		return r.n
	}
	n := 0
	if m, ok := v.h.Core().S.Find(roomChannel(h), h.ThreadTS); ok {
		n = m.ReplyCount
	}
	if v.replies == nil {
		v.replies = map[string]hdReplies{}
	}
	v.replies[k] = hdReplies{n: n, at: now}
	return n
}

// hdReplies is one replyCount answer and when it was read.
type hdReplies struct {
	n  int
	at time.Time
}

// backgroundURL is the huddle's background picture: previewUrl on a live
// card, url elsewhere.
func (v *Huddles) backgroundURL(id string, preview bool) string {
	v.mu.Lock()
	defer v.mu.Unlock()
	for _, b := range v.bgs {
		if b.ID == id {
			if preview {
				return b.PreviewURL
			}
			return b.URL
		}
	}
	return ""
}

// facepile is HuddlesParticipantsFacepile (LcjV) without overlap: s px
// faces 8px apart, at most max; the last of a longer list carries "+N"
// (9+ past nine) on #00000080 (white, 15px 900), centred in box.
func (v *Huddles) facepile(gtx C, ids []string, s int, maxFaces int, box image.Rectangle) {
	if len(ids) == 0 {
		return
	}
	n := min(len(ids), maxFaces)
	px := gtx.Dp(unit.Dp(s))
	gap := gtx.Dp(8)
	total := n*px + (n-1)*gap
	x0 := box.Min.X + (box.Dx()-total)/2
	y0 := box.Min.Y + (box.Dy()-px)/2
	for i := 0; i < n; i++ {
		func() {
			defer opOffset(gtx, image.Pt(x0+i*(px+gap), y0)).Pop()
			u, _ := v.user(ids[i])
			v.avatar(gtx, u.Avatar, unit.Dp(s))
			if extra := len(ids) - n; i == n-1 && extra > 0 {
				v.overlayCount(gtx, px, extra, argb(0x80000000), v.tok("dt_color-constants-white"), 15, wBlack)
			}
		}()
	}
}

// overlayCount is c-avatar__additional_overlay over an s px face: "+N",
// "9+" past nine.
func (v *Huddles) overlayCount(gtx C, s, n int, bg, fg color.NRGBA, size unit.Sp, wt font.Weight) {
	label := "+" + formatInt(int64(n))
	if n > 9 {
		label = "9+"
	}
	rrect(gtx, image.Rect(0, 0, s, s), avatarRadius(gtx, unit.Dp(float32(s)/gtx.Metric.PxPerDp)), bg)
	g := gtx
	g.Constraints = layout.Exact(image.Pt(s, s))
	layout.Center.Layout(g, func(gtx C) D { return v.label(gtx, size, wt, fg, label) })
}

// channelNameWithIcon is ChannelNameWithIcon over useChannelName's name:
// a public channel's filled hash or a private channel's lock (2px before
// the name), else (DMs, group DMs, a conversation you can't see) the
// name alone.
func (v *Huddles) channelNameWithIcon(gtx C, ch string, history []string, size unit.Sp, wt font.Weight, fg color.NRGBA, filled bool) D {
	icon, label := v.channelIconAndName(ch, history)
	if icon == "channel" && filled {
		icon = "channel-filled"
	}
	return row(gtx, layout.Middle,
		layout.Rigid(func(gtx C) D {
			if icon == "" {
				return D{}
			}
			is := unit.Dp(20)
			if size < 15 {
				is = 16
			}
			return layout.Inset{Right: 2}.Layout(gtx, func(gtx C) D { return v.icon(gtx, icon, is, fg) })
		}),
		layout.Flexed(1, func(gtx C) D { return v.label(gtx, size, wt, fg, label) }),
	)
}

// channelIconAndName is useChannelName (9F8a): channel / lock for public
// and private channels, and the conversation's name (no # or @); for a
// conversation you can't see, the participants (truncatedList).
func (v *Huddles) channelIconAndName(ch string, history []string) (icon, name string) {
	app := v.h.Core()
	c, ok := app.Conv(ch)
	if !ok {
		v.mu.Lock()
		gone := v.missing[ch]
		v.mu.Unlock()
		if gone || ch == "" {
			return "", v.truncatedNames(history)
		}
		ref := app.ConvRef(ch)
		c = core.Conv{ID: ch, Name: ref.Name, Kind: ref.Kind, User: ref.User}
	}
	switch c.Kind {
	case "channel":
		icon = "channel"
	case "private":
		icon = "lock"
	}
	if icon != "" { // getChannelDisplayName {publicChannelIncludeHashSign: false}: the icon stands for the #
		if n := v.viewChannelName(ch, c.Name); n != "" {
			return icon, n
		}
		return icon, ch
	}
	return icon, v.convTitle(ch, nil)
}

// truncatedNames is useTruncatedParticipantList (MnDU): the people other
// than you, five names then "and N others" (EwhZ).
func (v *Huddles) truncatedNames(ids []string) string {
	self := v.selfID()
	var names []string
	for _, id := range ids {
		if id == self {
			continue
		}
		names = append(names, v.userName(id))
	}
	vis := names
	if len(vis) > 5 {
		vis = vis[:5]
	}
	s := strings.Join(vis, ", ")
	if extra := len(names) - len(vis); extra > 0 {
		return nsHuddles.T("{ visibleNamesString } and{ truncatedCount, plural, =1 { # other } other { # others }}",
			"visibleNamesString", s, "truncatedCount", extra)
	}
	return s
}

// linkText is a 13px link button (c-link--button) that underlines on
// hover.
func (v *Huddles) linkText(gtx C, c *widget.Clickable, size unit.Sp, label string) D {
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		fg := v.tok("dt_color-content-hgl-1")
		d := v.label(gtx, size, wReg, fg, label)
		if c.Hovered() {
			y := d.Size.Y - gtx.Dp(2)
			fill(gtx, image.Rect(0, y, d.Size.X, y+max(1, gtx.Dp(1))), fg)
		}
		return d
	})
}

// ---- suggested huddles ----

type hdSuggestion struct {
	h core.Huddle
	n int
}

// suggestedHuddles is SuggestedHuddles' list: the unfiltered history's
// huddles from the last 7 days to the end of this week, grouped by
// conversation, most huddles first; not group DMs, records, KTE, file or
// other conversations (nor ones whose access_type_by_user is 0 for you),
// not one with a live huddle, and only ones you were in.
func (v *Huddles) suggestedHuddles(hs []core.Huddle, liveChans map[string]bool, now time.Time) []hdSuggestion {
	self := v.selfID()
	from := now.AddDate(0, 0, -7)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	endWeek := day.AddDate(0, 0, 7-int(day.Weekday()))
	type group struct {
		first core.Huddle
		n     int
	}
	var order []string
	groups := map[string]*group{}
	for _, h := range hs {
		t := unixTime(h.Start)
		if !t.After(from) || !t.Before(endWeek) {
			continue
		}
		k := strings.Join(h.Channels, ",")
		g := groups[k]
		if g == nil {
			g = &group{first: h}
			groups[k] = g
			order = append(order, k)
		}
		g.n++
	}
	// orderBy count desc (stable)
	out := make([]hdSuggestion, 0, len(order))
	for _, k := range order {
		out = append(out, hdSuggestion{h: groups[k].first, n: groups[k].n})
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].n > out[j-1].n; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	keep := out[:0]
	for _, s := range out {
		switch s.h.ChannelType {
		case "file", "other", "mpim", "record", "kte":
			continue
		}
		if a, ok := s.h.AccessByUser[self]; ok && a == 0 && s.h.AccessByUser != nil {
			continue
		}
		if liveChans[roomChannel(s.h)] || !contains(s.h.History, self) {
			continue
		}
		keep = append(keep, s)
	}
	return keep
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// suggestedTile is SuggestedHuddlesTile: a dashed 1px otl-ter card on
// surf-pry (200 tall, radius 8, content centred, gap 16): the
// participants (40px, at .5 until hovered), "Start a huddle in #channel?"
// / "Start a huddle with name?" (13px bold) and "You huddled here N
// times in the last week" (13px content-ter) — on hover the outline
// "Start Huddle" in its place. A click starts the huddle there through
// HuddleJoinSpeedbumpModal ("Starting a huddle?").
func (v *Huddles) suggestedTile(gtx C, h core.Huddle, count int, maxCards int) D {
	sz := gtx.Constraints.Max
	ch := roomChannel(h)
	icon, name := v.channelIconAndName(ch, h.History)
	v.mu.Lock()
	gone := v.missing[ch]
	v.mu.Unlock()
	if gone {
		return D{Size: sz} // doesNotExist: the tile renders nothing
	}
	c := v.clk("sug:" + h.ID)
	if c.Clicked(gtx) {
		v.joinOrStart(ch, &h, "huddles_page_suggested_huddle_card")
	}
	isDM := false
	if conv, ok := v.h.Core().Conv(ch); ok {
		isDM = conv.Kind == "im" || conv.Kind == "mpim"
	}
	hov := c.Hovered()
	c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		rad := gtx.Dp(8)
		r := image.Rectangle{Max: sz}
		pageBg := v.tok("dt_color-base-sec")
		bg := v.tokOver("dt_color-surf-pry", pageBg)
		rrect(gtx, r, rad, bg)
		v.dashedBorder(gtx, r, rad, v.tokOver("dt_color-otl-ter", bg))
		faces := 3
		if maxCards > 1 && v.pageW > gtx.Dp(hdSmallBP) && v.pageW <= gtx.Dp(hdMediumBP) {
			faces = 2
		}
		px := gtx.Dp(40)
		contentH := gtx.Dp(42)
		gap := gtx.Dp(16)
		blockH := px + gap + contentH
		y0 := (sz.Y - blockH) / 2
		// the facepile at .5 opacity until hovered
		func() {
			rec := op.Record(gtx.Ops)
			v.facepile(gtx, h.History, 40, faces, image.Rect(0, y0, sz.X, y0+px))
			call := rec.Stop()
			if hov {
				call.Add(gtx.Ops)
				return
			}
			defer paint.PushOpacity(gtx.Ops, 0.5).Pop()
			call.Add(gtx.Ops)
		}()
		cy := y0 + px + gap
		fg := v.tok("dt_color-content-pry")
		func() {
			defer opOffset(gtx, image.Pt(gtx.Dp(8), cy)).Pop()
			g := gtx
			g.Constraints = layout.Exact(image.Pt(sz.X-gtx.Dp(16), contentH))
			col(g,
				layout.Rigid(func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					return layout.N.Layout(gtx, func(gtx C) D { return v.startHuddleCopy(gtx, isDM, icon, name, fg) })
				}),
				vgap(2),
				layout.Rigid(func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					if hov {
						return layout.N.Layout(gtx, func(gtx C) D {
							return layout.Inset{Top: 8}.Layout(gtx, func(gtx C) D {
								return v.outlineFace(gtx, nsHuddles.T("Start Huddle"), false, 15)
							})
						})
					}
					var s string
					if isDM {
						peer := ""
						for _, id := range h.History {
							if id != v.selfID() {
								peer = v.userName(id)
								break
							}
						}
						if peer == "" {
							peer = "them"
						}
						s = nsHuddles.T("You huddled with { peerName } { count, plural, =1 { # time } other { # times }} in the last week",
							"peerName", peer, "count", count)
					} else {
						s = nsHuddles.T("You huddled here { count, plural, =1 { # time } other { # times }} in the last week", "count", count)
					}
					return v.centered(gtx, 13, wReg, v.tok("dt_color-content-ter"), oneSpace(s))
				}),
			)
		}()
		return D{Size: sz}
	})
	return D{Size: sz}
}

// startHuddleCopy is makeStartHuddleCopy: "Start a huddle in" / "Start a
// huddle with", a no-break space, the channel's filled icon and name,
// "?" (13px bold, one line).
func (v *Huddles) startHuddleCopy(gtx C, isDM bool, icon, name string, fg color.NRGBA) D {
	src := nsHuddles.T("Start a huddle in<slack-channel-name>{channel}</slack-channel-name>?", "channel", "\x00")
	if isDM {
		src = nsHuddles.T("Start a huddle with<person-name>{person}</person-name>?", "person", "\x00")
	}
	lead, tail, _ := strings.Cut(i18n.StripTags(src), "\x00")
	if icon == "channel" {
		icon = "channel-filled"
	} else if icon == "lock" {
		icon = "lock-filled"
	}
	return row(gtx, layout.Middle,
		layout.Rigid(func(gtx C) D { return v.label(gtx, 13, wBold, fg, lead+"\u00a0") }),
		layout.Rigid(func(gtx C) D {
			if icon == "" {
				return D{}
			}
			return v.icon(gtx, icon, 16, fg)
		}),
		layout.Rigid(func(gtx C) D { return v.label(gtx, 13, wBold, fg, name+tail) }),
	)
}

// oneSpace collapses the runs of spaces ICU leaves around plural
// branches ("You huddled here  3 times "), as HTML does.
func oneSpace(s string) string { return strings.Join(strings.Fields(s), " ") }

// outlineFace draws a c-button--outline face (no click of its own):
// height 28 at 13px, 36 at 15px, radius 7 (.p-huddles_suggested_tile__button).
func (v *Huddles) outlineFace(gtx C, label string, small bool, size unit.Sp) D {
	tw := v.th.Measure(gtx, size, wBold, label)
	hgt := gtx.Dp(36)
	if small {
		hgt = gtx.Dp(28)
	}
	sz := image.Pt(max(gtx.Dp(80), tw+2*gtx.Dp(12)), hgt)
	r := image.Rectangle{Max: sz}
	bg := v.tok("dt_color-base-pry")
	rrect(gtx, r, gtx.Dp(7), bg)
	border(gtx, r, gtx.Dp(7), max(1, gtx.Dp(1)), v.tokOver("dt_color-otl-sec", bg))
	g := gtx
	g.Constraints = layout.Exact(sz)
	layout.Center.Layout(g, func(gtx C) D { return v.label(gtx, size, wBold, v.tok("dt_color-content-pry"), label) })
	return D{Size: sz}
}

// dashedBorder strokes r's edge in 4px dashes (border: 1px dashed).
func (v *Huddles) dashedBorder(gtx C, r image.Rectangle, rad int, c color.NRGBA) {
	px1 := max(1, gtx.Dp(1))
	dash, gap := gtx.Dp(4), gtx.Dp(3)
	for x := r.Min.X + rad; x < r.Max.X-rad; x += dash + gap {
		e := min(x+dash, r.Max.X-rad)
		fill(gtx, image.Rect(x, r.Min.Y, e, r.Min.Y+px1), c)
		fill(gtx, image.Rect(x, r.Max.Y-px1, e, r.Max.Y), c)
	}
	for y := r.Min.Y + rad; y < r.Max.Y-rad; y += dash + gap {
		e := min(y+dash, r.Max.Y-rad)
		fill(gtx, image.Rect(r.Min.X, y, r.Min.X+px1, e), c)
		fill(gtx, image.Rect(r.Max.X-px1, y, r.Max.X, e), c)
	}
	// the corners: the rounded border's quarter in each corner square
	for _, q := range []image.Rectangle{
		image.Rect(r.Min.X, r.Min.Y, r.Min.X+rad, r.Min.Y+rad),
		image.Rect(r.Max.X-rad, r.Min.Y, r.Max.X, r.Min.Y+rad),
		image.Rect(r.Min.X, r.Max.Y-rad, r.Min.X+rad, r.Max.Y),
		image.Rect(r.Max.X-rad, r.Max.Y-rad, r.Max.X, r.Max.Y),
	} {
		func() {
			defer clip.Rect(q).Push(gtx.Ops).Pop()
			border(gtx, r, rad, px1, c)
		}()
	}
}

// ---- joining ----

// joinOrStart is use_on_start_or_join_huddle_with_speedbump (lodJ): straight
// to startHuddleWithConfirmation when its "Don't show this message again"
// is set (huddle_start_speedbump for a suggested card, huddle_join_speedbump
// otherwise), else HuddleJoinSpeedbumpModal.
func (v *Huddles) joinOrStart(ch string, h *core.Huddle, entry string) {
	if ch == "" {
		return
	}
	start := entry == "huddles_page_suggested_huddle_card"
	if hdSpeedbumpOff(start) {
		v.begin(ch, entry)
		return
	}
	ph, ok := v.h.(popups.Host)
	if !ok {
		v.begin(ch, entry)
		return
	}
	icon, chName := v.channelIconAndName(ch, nil)
	isDM := false
	if c, ok := v.h.Core().Conv(ch); ok {
		isDM = c.Kind == "im" || c.Kind == "mpim"
	}
	title := nsHuddles.T("Joining a huddle?")
	var body string
	switch {
	case start && isDM:
		body = nsHuddles.T("You’ll be starting a huddle with <b>{memberNames}</b>.", "memberNames", v.truncatedNames(h.History))
	case start:
		body = nsHuddles.T("You’ll be starting a huddle in {channelIcon}{channelName}.", "channelIcon", hdIconText(icon), "channelName", chName)
	case isDM:
		body = nsHuddles.T("You’ll be joining a huddle with <b>{memberNames}</b>.", "memberNames", v.truncatedNames(h.Participants))
	default:
		body = nsHuddles.T("You’ll be joining a huddle with <b>{memberNames}</b> in {channelIcon} {channelName}.",
			"memberNames", v.truncatedNames(h.Participants), "channelIcon", hdIconText(icon), "channelName", chName)
	}
	if start {
		title = nsHuddles.T("Starting a huddle?")
	}
	dontShow := true
	var check widget.Bool
	check.Value = true
	ph.ShowOverlay(popups.NewModal(ph, popups.ModalOpts{
		Title: title,
		Body: func(gtx C) D {
			dontShow = check.Value
			return v.speedbumpBody(gtx, stripB(body), &check)
		},
		Buttons: []*popups.ModalButton{
			{Label: nsHuddles.T("Cancel"), Kind: popups.BtnOutline},
			{Label: nsHuddles.T("OK"), Kind: popups.BtnPrimary, Do: func() bool {
				if dontShow {
					hdSetSpeedbumpOff(start)
				}
				v.begin(ch, entry)
				return true
			}},
		},
		EnterSubmits: true,
	}))
}

// hdIconText is the inline channel icon in a sentence: Clack writes the
// sigil Slack's icon stands for.
func hdIconText(icon string) string {
	switch icon {
	case "channel":
		return "#"
	case "lock":
		return "🔒"
	}
	return ""
}

// stripB drops <b> tags (the modal body is plain text).
func stripB(s string) string { return strings.NewReplacer("<b>", "", "</b>", "").Replace(s) }

// speedbumpBody is HuddleJoinSpeedbumpModal's body: the sentence, then
// (margin-top 20) the checked "Don't show this message again." checkbox.
func (v *Huddles) speedbumpBody(gtx C, body string, check *widget.Bool) D {
	fg := v.tok("dt_color-content-pry")
	return col(gtx,
		layout.Rigid(func(gtx C) D { return v.para(gtx, 15, 22, wReg, fg, 0, body) }),
		vgap(20),
		layout.Rigid(func(gtx C) D {
			return check.Layout(gtx, func(gtx C) D {
				pointerCursor(gtx)
				return row(gtx, layout.Middle,
					layout.Rigid(func(gtx C) D { return v.checkbox(gtx, check.Value) }),
					hgap(8),
					layout.Rigid(func(gtx C) D { return v.label(gtx, 15, wReg, fg, nsHuddles.T("Don’t show this message again.")) }),
				)
			})
		}),
	)
}

// checkbox is Slack's 16px c-input_checkbox: base-inv-hgl-1 with a check
// when on, a 1px otl-sec box when off.
func (v *Huddles) checkbox(gtx C, on bool) D {
	s := gtx.Dp(16)
	r := image.Rect(0, 0, s, s)
	if on {
		rrect(gtx, r, gtx.Dp(4), v.tok("dt_color-base-inv-hgl-1"))
		v.icon(gtx, "check", 16, v.tok("dt_color-content-inv-pry"))
	} else {
		bg := v.tok("dt_color-base-pry")
		rrect(gtx, r, gtx.Dp(4), bg)
		border(gtx, r, gtx.Dp(4), max(1, gtx.Dp(1)), v.tokOver("dt_color-otl-sec", bg))
	}
	return D{Size: image.Pt(s, s)}
}

// openJoinLive is JoinLiveHuddlesModal (boot-render iz7u): "Join huddle in
// {channel}", "You’ll be joining a huddle in {channel} with {a} and N
// others.", Cancel and Join Huddle (which goes on to the speedbump).
func (v *Huddles) openJoinLive(ch string, h huddleRoom) {
	ph, ok := v.h.(popups.Host)
	if !ok {
		v.joinOrStart(ch, &h, "huddles_page_live_huddle_card")
		return
	}
	_, chName := v.channelIconAndName(ch, h.History)
	self := v.selfID()
	var others []string
	for _, id := range h.Participants {
		if id != self {
			others = append(others, v.userName(id))
		}
	}
	title := nsHuddles.T("Join huddle in {channelEntity}", "channelEntity", chName)
	body := title
	if len(others) > 0 {
		second := ""
		if len(others) > 1 {
			second = others[1]
		}
		body = stripA(nsHuddles.T("You’ll be joining a huddle in {channelEntity} with {participants, plural, =0 {{firstParticipant}} =1 {{firstParticipant} and {secondParticipant}} other {{firstParticipant} <a>and # others</a>}}.",
			"channelEntity", chName, "participants", len(others)-1, "firstParticipant", others[0], "secondParticipant", second))
	}
	ph.ShowOverlay(popups.NewModal(ph, popups.ModalOpts{
		Title: title,
		Body: func(gtx C) D {
			return v.para(gtx, 15, 22, wReg, v.tok("dt_color-content-pry"), 0, body)
		},
		Buttons: []*popups.ModalButton{
			{Label: nsHuddles.T("Cancel"), Kind: popups.BtnOutline},
			{Label: nsHuddles.T("Join Huddle"), Kind: popups.BtnPrimary, Do: func() bool {
				v.joinOrStart(ch, &h, "huddles_page_live_huddle_card")
				return true
			}},
		},
	}))
}

// stripA drops <a> tags.
func stripA(s string) string { return strings.NewReplacer("<a>", "", "</a>", "").Replace(s) }

// The speedbump's "Don't show this message again" (Slack keeps it in
// localStorage: huddle_join_speedbump / huddle_start_speedbump), for the
// session.
var hdSpeedbump struct {
	join, start bool
}

func hdSpeedbumpOff(start bool) bool {
	if start {
		return hdSpeedbump.start
	}
	return hdSpeedbump.join
}

func hdSetSpeedbumpOff(start bool) {
	if start {
		hdSpeedbump.start = true
	} else {
		hdSpeedbump.join = true
	}
}
