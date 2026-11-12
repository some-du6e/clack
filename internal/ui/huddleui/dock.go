package huddleui

import (
	"image"
	"image/color"
	"time"

	"gioui.org/io/system"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/ui/openurl"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The docked huddle bar: Slack's HuddleBottomBar (render wjeI), which the
// 2026 client (IA4) puts in the client's bottom row while you're in a
// huddle (.p-client__huddle_bottom_bar: under the tab rail and the
// workspace, padding 0 4px 4px; zuQH's huddleBottomBar slot). Its
// .toolbarMainBar is a 12px-round theme-surf-inv-sec card in three
// columns (1fr, 2fr, max-content; gap 16px; padding 8px 8px 8px 12px):
//
//   - with the huddle window closed (56px): the left shows your 40px tile
//     and the active speaker's (or the sharer's), then the title
//     ("Huddle in #channel", "Huddle with {name}"; 13px bold) over the
//     subtitle ("{name} is speaking", "{n} people here", "You’re the
//     only one here"; 13px, content-inv-sec); the middle is the toolbar's
//     center buttons (HuddleToolbarCenterButtons); the right "Open Huddle
//     in new window" and Leave.
//   - with the window open (collapsed, 48px; two columns, 40% and the
//     rest): the headphones and the title (15px bold), then "Bring huddle
//     to front" and Leave, 32px tall.
//
// A click on the card itself opens (or raises) the window. Alerts sit on
// top of it (HuddlesAlertBanner with entryPoint bottom_bar: 12px round,
// 4px above the card): Clack's is the microphone warning.

const (
	dockBar       = 56 // .toolbarMainBar height
	dockCollapsed = 48 // .toolbarMainBarControlsCollapsed
	dockPad       = 4  // .p-client__huddle_bottom_bar padding 0 4px 4px
)

type dockState struct {
	card, open, leave widget.Clickable
	micFix, micRow    widget.Clickable
	clicks            map[string]*widget.Clickable
	tips              map[string]*popups.TipState
	stick             *stickerGrid // the sticker caret's HuddleStickerGrid (hand.go)
	audio             *popups.Menu // the mic's Audio settings menu
	video             *popups.Menu // the camera's Video settings menu (window_camera.go)
	leaveMenu         *popups.Menu // Leave options (leave_options.go)
	quick             *quickPicker // the emoji button's quick picker (quickpicker.go)
	shareAlert        alertState   // the screen share alert above the card (share.go)
	lounge            loungeUI     // HuddleLoungeMusicBanner above the card (lounge.go)
	resp              dockResp     // useResponsiveHuddleToolbarButtons
}

func (d *dockState) click(id string) *widget.Clickable {
	if d.clicks == nil {
		d.clicks = map[string]*widget.Clickable{}
	}
	c := d.clicks[id]
	if c == nil {
		c = new(widget.Clickable)
		d.clicks[id] = c
	}
	return c
}

func (d *dockState) tip(id string) *popups.TipState {
	if d.tips == nil {
		d.tips = map[string]*popups.TipState{}
	}
	t := d.tips[id]
	if t == nil {
		t = new(popups.TipState)
		d.tips[id] = t
	}
	return t
}

// DockHeight is the room the docked bar takes at the bottom of the main
// window (0 when you're not in a huddle).
func (c *Controller) DockHeight(gtx C) int {
	if !c.Active() {
		return 0
	}
	h := gtx.Dp(dockBar)
	if c.WindowOpen() {
		h = gtx.Dp(dockCollapsed)
	}
	aw := gtx.Constraints.Max.X - 2*gtx.Dp(dockPad)
	if l := c.dockLoungeHeight(gtx, c.h.Theme(), aw); l > 0 {
		// the bar shows one banner type at a time, the temporary one
		// (the lounge music) first: the warnings wait
		return h + l + gtx.Dp(dockPad)
	}
	if s := c.Session(); s != nil && s.State().Mic.Problem() {
		h += gtx.Dp(barMicRow) + gtx.Dp(4)
	}
	if a := c.shareAlertHeight(gtx, c.h.Theme(), aw); a > 0 {
		h += a + gtx.Dp(4) // the bottom_bar alert, 4px above the card
	}
	return h + gtx.Dp(dockPad)
}

// LayoutDock draws the docked bar into gtx.Constraints.Max (the window's
// width × DockHeight) on the main window's goroutine.
func (c *Controller) LayoutDock(gtx C, th *theme.Theme) D {
	sz := image.Pt(gtx.Constraints.Max.X, c.DockHeight(gtx))
	if sz.Y == 0 {
		return D{}
	}
	if slackc.Patched("HuddleMiniPanel") { // LetMeSee adds after it
		var room, ch string
		if s := c.Session(); s != nil {
			room, ch = s.RoomID(), s.State().Channel
		}
		return slackc.Render(gtx, "HuddleMiniPanel", slackc.Props{"$key": "huddle-mini-panel", "huddleId": room, "channelId": ch,
			"className": "p-huddle_mini_panel"}, func(gtx C, _ slackc.Props) D { return c.layoutDock(gtx, th, sz) })
	}
	return c.layoutDock(gtx, th, sz)
}

func (c *Controller) layoutDock(gtx C, th *theme.Theme, sz image.Point) D {
	s := c.Session()
	var st huddle.State
	if s != nil {
		st = s.State()
	} else {
		c.mu.Lock()
		st.Channel = c.joining
		c.mu.Unlock()
	}
	d := &c.dock
	collapsed := c.WindowOpen()
	if d.open.Clicked(gtx) || d.card.Clicked(gtx) || d.micRow.Clicked(gtx) {
		c.openOrRaise()
	}
	if d.leave.Clicked(gtx) {
		c.Leave()
	}
	note, micBad := micNoteFor(st)
	if d.micFix.Clicked(gtx) && micBad {
		if note.settings != "" {
			_ = openurl.OpenSettings(note.settings)
		} else if s != nil {
			s.RetryMic()
		}
	}

	ink := dockInks(th)
	y := 0
	lounge := false
	if s != nil { // HuddlesAlertBannerContainer's temporary banner (lounge.go)
		if lh := c.layoutDockLounge(gtx, th, image.Rect(gtx.Dp(dockPad), 0, sz.X-gtx.Dp(dockPad), 0)); lh > 0 {
			y, lounge = lh+gtx.Dp(4), true
		}
	}
	if micBad && s != nil && !lounge { // the bottom_bar alert above the card
		r := image.Rect(gtx.Dp(dockPad), 0, sz.X-gtx.Dp(dockPad), gtx.Dp(barMicRow))
		c.dockMicAlert(gtx, th, r, st, note)
		y = r.Max.Y + gtx.Dp(4)
	}
	if c.shareAlert() != "" && !lounge { // HuddlesAlertBanner, entryPoint bottom_bar
		aw := sz.X - 2*gtx.Dp(dockPad)
		d := at(gtx, image.Pt(gtx.Dp(dockPad), y), image.Pt(aw, sz.Y), func(gtx C) D {
			return c.layoutShareAlertR(gtx, th, &c.dock.shareAlert, aw, 12)
		})
		if d.Size.Y > 0 {
			y += d.Size.Y + gtx.Dp(4)
		}
	}
	bh := gtx.Dp(dockBar)
	if collapsed {
		bh = gtx.Dp(dockCollapsed)
	}
	card := image.Rect(gtx.Dp(dockPad), y, sz.X-gtx.Dp(dockPad), y+bh)
	theme.RRect(gtx, card, gtx.Dp(12), ink.bg)
	at(gtx, card.Min, card.Size(), func(gtx C) D { // cursor: pointer; a click opens the window
		return button(gtx, &d.card, card.Size(), nil, nil)
	})
	inner := image.Rect(card.Min.X+gtx.Dp(12), card.Min.Y+gtx.Dp(8), card.Max.X-gtx.Dp(8), card.Max.Y-gtx.Dp(8))
	title := c.dockTitle(st.Channel)

	// the right controls: the window button, then Leave
	btnH := gtx.Dp(40)
	if collapsed {
		btnH = gtx.Dp(32)
	}
	lw := max(gtx.Dp(80), labelSize(gtx, th, 15, theme.Bold, nsHuddles.T("Leave")).X+2*gtx.Dp(12))
	endAll := s != nil && c.canEndForAll(st)
	lt, seg := lw, 0
	if endAll { // HuddleToolbarLeaveButton, segmented (leave_options.go)
		lt, seg = lw+gtx.Dp(leaveCaretW), segMain
	}
	lx := inner.Max.X - gtx.Dp(4) - lt
	by := inner.Min.Y + (inner.Dy()-btnH)/2
	if endAll {
		cr := image.Rect(lx+lw, by, lx+lt, by+btnH)
		lc := d.click("leavemenu")
		if lc.Clicked(gtx) {
			c.dockClick(gtx, "leavemenu", s, st, cr)
		}
		open := d.leaveMenu != nil && !d.leaveMenu.Dismissed()
		at(gtx, cr.Min, cr.Size(), func(gtx C) D { return leaveCaret(gtx, lc, cr.Size(), open) })
	}
	at(gtx, image.Pt(lx, by), image.Pt(lw, btnH), func(gtx C) D {
		dd := button(gtx, &d.leave, image.Pt(lw, btnH), func(gtx C, hov bool) {
			bg := hudLeave
			if hov {
				bg = hudLeaveHov
			}
			segFill(gtx, image.Rectangle{Max: image.Pt(lw, btnH)}, seg, gtx.Dp(8), bg)
		}, func(gtx C) D {
			centerLabel(gtx, th, image.Pt(lw, btnH), 15, theme.Bold, hudWhite, nsHuddles.T("Leave"))
			return D{}
		})
		popups.Tooltip(gtx, th, d.tip("leave"), d.leave.Hovered(), image.Pt(lw, btnH), popups.Above, nsHuddles.T("Leave Huddle"), leaveShortcut())
		return dd
	})
	ow := gtx.Dp(40)
	if collapsed {
		ow = gtx.Dp(32)
	}
	ox := lx - gtx.Dp(8) - ow
	openTip := nsHuddles.T("Open Huddle in new window")
	if collapsed {
		openTip = nsHuddles.T("Bring huddle to front")
	}
	at(gtx, image.Pt(ox, by), image.Pt(ow, btnH), func(gtx C) D {
		dd := button(gtx, &d.open, image.Pt(ow, btnH), func(gtx C, hov bool) {
			bg := ink.btn
			if hov {
				bg = ink.btnHov
			}
			theme.RRect(gtx, image.Rectangle{Max: image.Pt(ow, btnH)}, gtx.Dp(8), bg)
		}, func(gtx C) D { iconAt(gtx, image.Pt(ow, btnH), "open-in-window", 20, ink.text); return D{} })
		popups.Tooltip(gtx, th, d.tip("open"), d.open.Hovered(), image.Pt(ow, btnH), popups.Above, openTip, "")
		return dd
	})

	if collapsed {
		d.resp = dockResp{} // HuddleToolbarCenterButtons unmounts
		// headphones + the title, 15px bold, in the left 40%
		lr := image.Rect(inner.Min.X, inner.Min.Y, inner.Min.X+inner.Dx()*2/5, inner.Max.Y)
		is := gtx.Dp(20)
		at(gtx, image.Pt(lr.Min.X, lr.Min.Y+(lr.Dy()-is)/2), image.Pt(is, is), func(gtx C) D {
			iconAt(gtx, image.Pt(is, is), "headphones", 20, ink.text)
			return D{Size: image.Pt(is, is)}
		})
		tx := lr.Min.X + is + gtx.Dp(6)
		txt := ellipsize(gtx, th, 15, theme.Bold, title, max(0, lr.Max.X-tx))
		ts := labelSize(gtx, th, 15, theme.Bold, txt)
		at(gtx, image.Pt(tx, lr.Min.Y+(lr.Dy()-ts.Y)/2), ts, func(gtx C) D { return th.Label(gtx, 15, theme.Bold, ink.text, txt) })
		return D{Size: sz}
	}

	// the grid's columns: minmax(0,1fr) minmax(0,2fr) minmax(max-content,
	// 1fr), column-gap 16px; the right one holds the window button and
	// Leave with their 4px margins
	m := gtx.Dp(4)
	col1, col2 := dockColumns(inner.Dx(), inner.Max.X-(ox-m), gtx.Dp(16))
	// HuddleToolbarCenterButtons in the middle column (text-align center;
	// start-aligned when they overflow it), as many as
	// useResponsiveHuddleToolbarButtons lets fit
	now := gtx.Now
	if now.IsZero() {
		now = time.Now()
	}
	comps := d.resp.layout(gtx, now, col2, c.dockComps(st), dockBreakpoints(c.videoAllowed()))
	var ctls []ctl
	for _, k := range comps {
		ctls = append(ctls, k...)
	}
	total := compsWidth(gtx, comps)
	cx := inner.Min.X + col1 + gtx.Dp(16)
	x := cx + max(0, (col2-total)/2)
	leftEnd := inner.Min.X + col1
	h := gtx.Dp(40)
	cy := inner.Min.Y + (inner.Dy()-h)/2
	for _, k := range ctls {
		cw := dockCtlW(gtx, k)
		if k.seg != segCaret {
			x += m
		}
		k := k
		cl := d.click(k.id)
		px := x
		at(gtx, image.Pt(x, cy), image.Pt(cw, h), func(gtx C) D {
			dd := button(gtx, cl, image.Pt(cw, h), func(gtx C, hov bool) {
				bg := ink.btn
				if hov {
					bg = ink.btnHov
				}
				if k.on {
					bg = hudBtnOn
				}
				segFill(gtx, image.Rectangle{Max: image.Pt(cw, h)}, k.seg, gtx.Dp(8), bg)
			}, func(gtx C) D {
				fg := ink.text
				if k.on {
					fg = hudInk
				}
				if k.dim {
					fg.A = 0x66
				}
				icon := k.icon
				if k.hover != "" && (k.on || cl.Hovered()) {
					icon = k.hover
				}
				if k.emoji != "" {
					centerLabel(gtx, th, image.Pt(cw, h), 18, theme.Regular, fg, k.emoji)
				} else {
					iconAt(gtx, image.Pt(cw, h), icon, 20, fg)
				}
				if k.seg == segMain {
					theme.Fill(gtx, image.Rect(cw-max(1, gtx.Dp(1)), (h-gtx.Dp(20))/2, cw, (h+gtx.Dp(20))/2), ink.divider)
				}
				if k.warn {
					warnBadge(gtx, image.Pt(cw, 0), gtx.Dp(14))
				}
				return D{}
			})
			if k.tip != "" {
				popups.Tooltip(gtx, th, d.tip(k.id), cl.Hovered(), image.Pt(cw, h), popups.Above, k.tip, "")
			}
			return dd
		})
		if cl.Clicked(gtx) {
			c.dockClick(gtx, k.id, s, st, image.Rect(px, cy, px+cw, cy+h))
		}
		x += cw
		if k.seg != segMain {
			x += m
		}
	}

	// the left: your tile, the speaker's, the title over the subtitle
	ts := gtx.Dp(40)
	tx := inner.Min.X
	ty := inner.Min.Y + (inner.Dy()-ts)/2
	if s != nil {
		// the speaker's ring, tile and "… is speaking" go when they stop
		// being heard: draw again then (nothing else changes)
		for _, p := range st.Participants {
			if p.Speaking(now) {
				gtx.Execute(op.InvalidateCmd{At: p.SpeakingUntil()})
			}
		}
		for i, p := range c.dockTiles(st, now) {
			if i > 0 {
				tx += gtx.Dp(8)
			}
			if tx+ts > leftEnd {
				break
			}
			p := p
			at(gtx, image.Pt(tx, ty), image.Pt(ts, ts), func(gtx C) D {
				rad := gtx.Dp(8)
				avatar(gtx, th, c.h.Images(), c.avatarURL(p.UserID, ts), image.Pt(ts, ts), rad)
				if p.Speaking(now) {
					theme.Border(gtx, image.Rectangle{Max: image.Pt(ts, ts)}, rad, gtx.Dp(2), hudWhite)
				}
				return D{Size: image.Pt(ts, ts)}
			})
			tx += ts
		}
		tx += gtx.Dp(12)
	}
	if w := leftEnd - tx; w > gtx.Dp(24) {
		sub := c.dockSubtitle(st, now)
		t1 := ellipsize(gtx, th, 13, theme.Bold, title, w)
		t2 := ellipsize(gtx, th, 13, theme.Regular, sub, w)
		s1 := labelSize(gtx, th, 13, theme.Bold, t1)
		s2 := image.Point{}
		if t2 != "" {
			s2 = labelSize(gtx, th, 13, theme.Regular, t2)
		}
		yy := inner.Min.Y + (inner.Dy()-s1.Y-s2.Y)/2
		at(gtx, image.Pt(tx, yy), s1, func(gtx C) D { return th.Label(gtx, 13, theme.Bold, ink.text, t1) })
		if t2 != "" {
			at(gtx, image.Pt(tx, yy+s1.Y), s2, func(gtx C) D { return th.Label(gtx, 13, theme.Regular, ink.sec, t2) })
		}
	}
	if d.stick != nil && d.stick.Dismissed() {
		d.stick = nil
	}
	return D{Size: sz}
}

// dockInk is the bar's theme tokens. It sits in the client's chrome, so in
// light mode it takes the inverted (aubergine) scope like the sidebar.
type dockInk struct{ bg, text, sec, btn, btnHov, divider color.NRGBA }

func dockInks(th *theme.Theme) dockInk {
	p := th.C
	return dockInk{bg: p.SidebarBg, text: p.SidebarTextUnread, sec: p.SidebarText, btn: p.SidebarButton, btnHov: p.SidebarHover, divider: p.SidebarDivider}
}

// dockTitle is the bar's title: "Huddle with {name}" in a DM or group DM,
// else "Huddle in #channel".
func (c *Controller) dockTitle(ch string) string {
	app := c.h.Core()
	if app == nil {
		return nsHuddles.T("Huddle")
	}
	conv, ok := app.Conv(ch)
	if !ok {
		return nsHuddles.T("Huddle")
	}
	switch conv.Kind {
	case "im", "mpim":
		t, _ := c.windowTitle(huddle.State{Channel: ch})
		return t
	}
	if conv.Name == "" {
		return nsHuddles.T("Huddle")
	}
	return nsHuddles.T("Huddle in {channelDisplayName}", "channelDisplayName", "#"+conv.Name) // HuddleBottomBarContent
}

// dockSubtitle is the bar's subtitle: who's sharing, else who's speaking,
// else the head count ("{n} people here" / "You’re the only one here");
// a DM gets none.
func (c *Controller) dockSubtitle(st huddle.State, now time.Time) string {
	var sharers []string
	for _, p := range st.Participants {
		if p.Sharing {
			sharers = append(sharers, c.displayName(p.UserID))
		}
	}
	switch {
	case len(sharers) >= 2:
		return nsHuddles.T("{screenshareUser1} and {screenshareUser2} are screensharing", "screenshareUser1", sharers[0], "screenshareUser2", sharers[1])
	case len(sharers) == 1:
		return nsHuddles.T("{screenshareUser} is screensharing", "screenshareUser", sharers[0])
	}
	for _, p := range st.Participants {
		if !p.Self && p.Speaking(now) {
			return nsHuddles.T("{activeSpeakerName} is speaking", "activeSpeakerName", c.displayName(p.UserID))
		}
	}
	if app := c.h.Core(); app != nil {
		if conv, ok := app.Conv(st.Channel); ok && conv.Kind == "im" {
			return ""
		}
	}
	if n := len(st.Participants); n > 1 {
		return nsHuddles.T("{numberOfPeople} people here", "numberOfPeople", n)
	}
	return nsHuddles.T("You’re the only one here")
}

// dockTiles is your tile and, beside it, the sharer's or the active
// speaker's (not you).
func (c *Controller) dockTiles(st huddle.State, now time.Time) []huddle.Participant {
	var self, other *huddle.Participant
	for i := range st.Participants {
		p := &st.Participants[i]
		switch {
		case p.Self:
			self = p
		case other == nil && (p.Sharing || p.Speaking(now)):
			other = p
		}
	}
	var out []huddle.Participant
	if self != nil {
		out = append(out, *self)
	}
	if other != nil {
		out = append(out, *other)
	}
	return out
}

// dockCtls is HuddleToolbarCenterButtons with the bar's state (the mute
// button, the camera, screen share, the sticker pair, reactions, the
// thread and ⋯), every button.
func (c *Controller) dockCtls(st huddle.State) []ctl {
	var out []ctl
	for _, k := range c.dockComps(st) {
		out = append(out, k...)
	}
	return out
}

// dockComps is HuddleToolbarCenterButtons' k: one entry per component
// (a segmented button is two ctls).
func (c *Controller) dockComps(st huddle.State) [][]ctl {
	// HuddleToolbarMuteButton inBottomBar: segmented, like the window's
	mics := c.micCtls(st, c.dock.audio != nil && !c.dock.audio.Dismissed())
	// HuddleToolbarCameraButton inBottomBar: segmented, its Video settings
	// a popover above the caret (window_camera.go)
	cams := c.cameraCtls(st, c.dock.video != nil && !c.dock.video.Dismissed())
	mine := ""
	for _, p := range st.Participants {
		if p.Self {
			mine = p.Sticker
		}
	}
	face := mine
	if face == "" {
		face = c.handEmoji()
	}
	stick := c.dock.stick != nil && !c.dock.stick.Dismissed()
	caret := "caret-up"
	if stick {
		caret = "caret-down"
	}
	quick := c.dock.quick != nil && !c.dock.quick.Dismissed()
	out := [][]ctl{mics}
	if len(cams) > 0 { // isHuddlesVideoEnabledForOrgWorkspaces
		out = append(out, cams)
	}
	return append(out, []ctl{c.shareCtl(st)},
		[]ctl{
			// the caret is toggled on while the grid is open or a sticker
			// is set (isEmojiMenuOpen || P)
			{id: "hand", emoji: c.emojiText(face), on: mine != "", tip: handTip(mine), seg: segMain},
			{id: "stickers", icon: caret, on: stick || mine != "", tip: nsHuddles.T("Choose sticker"), seg: segCaret},
		},
		[]ctl{{id: "react", icon: "emoji", hover: "emoji-filled", on: quick, tip: nsHuddles.T("Add reaction")}},
		[]ctl{{id: "thread", icon: "threads", tip: nsHuddles.T("Open thread")}},
		[]ctl{{id: "more", icon: "ellipsis-vertical-filled", tip: nsHuddles.T("More actions")}},
	)
}

// useResponsiveHuddleToolbarButtons (gantry-v2-async-client-boot-render
// wjeI, build 2cd9270e786c): HuddleToolbarCenterButtons shows at most
// maxButtons (8, less one without the camera and one without the canvas
// button, which Clack doesn't draw) and fewer as the middle column
// (bottomBarCenterControlsOuter) narrows, by breakpoints: with the
// camera [486 438 342 270 222 150 78], without it [366 318 270 198 150
// 78]. A width at or over the first shows them all; between two, one
// fewer per step; under the last, one. sliceHuddleToolbarButtons keeps
// the first n-1 and the last (More actions).
//
// The column is measured by a ResizeObserver whose callback is debounced
// 250 ms (JjsV getResizeObserver): it stores the width and, when the
// width grew, shows every button again (showAllButtons) before the
// layout effect measures the row. The effect re-picks n when the row
// overflows the column or n's breakpoint changed, and leaves "show all"
// once n is under the maximum, so a column that grew within a step can
// show every button while they fit.
var (
	dockBpsCamera   = []int{486, 438, 342, 270, 222, 150, 78} // H
	dockBpsNoCamera = []int{366, 318, 270, 198, 150, 78}      // V
)

func dockBreakpoints(camera bool) []int {
	if camera {
		return dockBpsCamera
	}
	return dockBpsNoCamera
}

// dockRespDebounce is the observer's debounce.
const dockRespDebounce = 250 * time.Millisecond

// dockResp is the hook's state: r.current (last), numVisibleButtons,
// showAllButtons, and the observer's last size and pending callback.
type dockResp struct {
	init    bool
	max     int
	last    int
	visible int
	showAll bool
	seen    int
	due     time.Time
}

// layout runs a frame of the hook for a column outer px wide and returns
// the components to draw.
func (r *dockResp) layout(gtx C, now time.Time, outer int, comps [][]ctl, bps []int) [][]ctl {
	n := len(comps)
	if !r.init || r.max != n {
		*r = dockResp{init: true, max: n, visible: n, showAll: true, seen: -1}
	}
	if outer != r.seen { // a resize (and the observer's first callback)
		r.seen, r.due = outer, now.Add(dockRespDebounce)
	}
	run := false
	if !r.due.IsZero() {
		if now.Before(r.due) {
			gtx.Execute(op.InvalidateCmd{At: r.due})
		} else {
			r.due = time.Time{}
			prev := r.last
			r.last = outer
			if outer > prev {
				r.showAll = true
			}
			run = true
		}
	}
	shown := func() [][]ctl {
		if r.showAll {
			return comps
		}
		return sliceComps(comps, r.visible)
	}
	if run { // the layout effect
		if t, w := r.last, compsWidth(gtx, shown()); t > 0 && w > 0 {
			a := respCount(gtx, t, n, bps)
			if w > t || a != r.visible {
				r.visible = a
				if r.showAll && a != n {
					r.showAll = false
				}
			}
		}
	}
	return shown()
}

// respCount is the hook's breakpoint pick.
func respCount(gtx C, width, maxButtons int, bps []int) int {
	if len(bps) == 0 {
		return 1
	}
	if width >= gtx.Dp(unit.Dp(bps[0])) {
		return maxButtons
	}
	for a := 0; a < len(bps)-1; a++ {
		if width < gtx.Dp(unit.Dp(bps[a])) && width >= gtx.Dp(unit.Dp(bps[a+1])) {
			return maxButtons - a - 1
		}
	}
	return 1
}

// sliceComps is sliceHuddleToolbarButtons.
func sliceComps(comps [][]ctl, n int) [][]ctl {
	if n >= len(comps) {
		return comps
	}
	out := append([][]ctl(nil), comps[:max(0, n-1)]...)
	if len(comps) > 0 {
		out = append(out, comps[len(comps)-1])
	}
	return out
}

// compsWidth is bottomBarCenterControls' width: the buttons and their
// 4px margins (a segmented pair shares one margin box).
func compsWidth(gtx C, comps [][]ctl) int {
	w := 0
	for _, k := range comps {
		for _, b := range k {
			w += dockCtlW(gtx, b)
		}
		w += 2 * gtx.Dp(4)
	}
	return w
}

// dockColumns resolves .toolbarMainBar's grid columns for content width
// w (px), the right column's max-content right and gap: the first and
// middle columns' widths. The right column's minimum is its content, so
// when a 1fr share is narrower the other two share what's left.
func dockColumns(w, right, gap int) (col1, col2 int) {
	free := max(0, w-2*gap)
	fr := free / 4
	if fr < right {
		fr = max(0, free-right) / 3
	}
	return fr, 2 * fr
}

func dockCtlW(gtx C, k ctl) int {
	switch k.seg {
	case segMain:
		return gtx.Dp(36)
	case segCaret:
		return gtx.Dp(28)
	}
	return gtx.Dp(40)
}

// dockClick runs a center button (main window's goroutine); r is the
// button in window coordinates (the menus' anchor).
func (c *Controller) dockClick(gtx C, id string, s *huddle.Session, st huddle.State, r image.Rectangle) {
	if s == nil {
		return
	}
	ph, _ := c.h.(popups.Host)
	r = r.Add(c.dockOrigin)
	switch id {
	case "mic":
		c.micClick(s, st, ph)
	case "micmenu":
		if ph == nil {
			return
		}
		if c.dock.audio != nil && !c.dock.audio.Dismissed() {
			c.dock.audio.Dismiss()
			c.dock.audio = nil
			break
		}
		c.dock.audio = popups.NewMenuOpts(ph, c.audioMenu(s), popups.MenuOpts{Anchor: r, Side: popups.Above,
			MenuClass: "p-huddle_more_actions_menu"})
		ph.ShowOverlay(c.dock.audio)
	case "videomenu": // Video settings
		if ph == nil {
			return
		}
		if c.dock.video != nil && !c.dock.video.Dismissed() {
			c.dock.video.Dismiss()
			c.dock.video = nil
			break
		}
		c.dock.video = popups.NewMenuOpts(ph, c.videoMenu(s), popups.MenuOpts{Anchor: r, Side: popups.Above,
			MenuClass: "p-huddle_more_actions_menu"})
		ph.ShowOverlay(c.dock.video)
	case "leavemenu": // Leave options
		if ph == nil {
			return
		}
		if c.dock.leaveMenu != nil && !c.dock.leaveMenu.Dismissed() {
			c.dock.leaveMenu.Dismiss()
			c.dock.leaveMenu = nil
			break
		}
		c.dock.leaveMenu = popups.NewMenuOpts(ph, c.leaveOptionsMenu(s), popups.MenuOpts{Anchor: r, Side: popups.Above,
			MenuClass: "p-huddle_more_actions_menu"})
		ph.ShowOverlay(c.dock.leaveMenu)
	case "camera":
		if st.Camera.State == huddle.CameraOn || st.Camera.State == huddle.CameraStarting {
			s.SetCamera(false)
		} else if cameraSupported() {
			s.SetCamera(true)
		}
	case "share": // HuddleToolbarScreenshareButton: the picker opens over the main window
		c.shareClick(s, st, ph, c.post)
	case "hand":
		go func() { _ = c.ToggleHand() }()
	case "stickers": // HuddleStickerGrid above the caret (hand.go)
		if ph == nil {
			return
		}
		if c.dock.stick != nil && !c.dock.stick.Dismissed() {
			c.dock.stick.Dismiss()
			c.dock.stick = nil
			break
		}
		c.dock.stick = newStickerGrid(c, ph, r)
		ph.ShowOverlay(c.dock.stick)
	case "react": // HuddleEmojiMenuTrigger: the quick picker above the button
		if ph == nil {
			return
		}
		if c.dock.quick != nil && !c.dock.quick.Dismissed() {
			c.dock.quick.Dismiss()
			c.dock.quick = nil
			break
		}
		c.dock.quick = newQuickPicker(ph, r, false, func(name string) { _ = s.React(name, "") })
		ph.ShowOverlay(c.dock.quick)
	case "thread":
		if st.ThreadTS != "" {
			c.h.OpenThread(firstNonEmpty(st.ThreadChannel, st.Channel), st.ThreadTS)
		} else {
			c.h.Navigate("c/" + st.Channel)
		}
	case "more":
		if ph == nil {
			return
		}
		ph.ShowOverlay(popups.NewMenuOpts(ph, c.moreMenu(s, st, nil), popups.MenuOpts{Anchor: r, Side: popups.Above,
			MenuClass: "p-huddle_more_actions_menu"}))
	}
	c.h.Invalidate()
}

// SetDockOrigin tells the controller where the dock sits in the window
// (the ui root calls it before LayoutDock; menus anchor to its buttons).
func (c *Controller) SetDockOrigin(p image.Point) { c.dockOrigin = p }

// openOrRaise opens the huddle window, or brings it to the front.
func (c *Controller) openOrRaise() {
	c.mu.Lock()
	w := c.win
	c.mu.Unlock()
	if w == nil {
		c.OpenWindow()
		return
	}
	w.mu.Lock()
	ww := w.w
	w.mu.Unlock()
	if ww != nil {
		go ww.Perform(system.ActionRaise)
	}
}

// dockMicAlert is the microphone warning above the bar: the short reason
// (opens the window) and the fix (the privacy settings, or Retry).
func (c *Controller) dockMicAlert(gtx C, th *theme.Theme, r image.Rectangle, st huddle.State, note micNote) {
	d := &c.dock
	theme.RRect(gtx, r, gtx.Dp(12), hudWarnBg)
	fix := nsHuddles.T("Settings")
	if note.settings == "" {
		fix = nsHuddles.T("Retry")
	}
	rh := r.Dy() - gtx.Dp(8)
	fw := labelSize(gtx, th, 12, theme.Bold, fix).X + gtx.Dp(14)
	txt := micShort(st.Mic)
	ls := labelSize(gtx, th, 12, theme.Bold, txt)
	cw := gtx.Dp(14) + gtx.Dp(6) + ls.X + gtx.Dp(10) + fw
	x := r.Min.X + (r.Dx()-cw)/2
	at(gtx, image.Pt(x, r.Min.Y), image.Pt(gtx.Dp(20)+ls.X, r.Dy()), func(gtx C) D {
		return button(gtx, &d.micRow, image.Pt(gtx.Dp(20)+ls.X, r.Dy()), nil, func(gtx C) D {
			iconAt(gtx, image.Pt(gtx.Dp(14), r.Dy()), "warning", 14, hudWarn)
			at(gtx, image.Pt(gtx.Dp(20), (r.Dy()-ls.Y)/2), ls, func(gtx C) D { return th.Label(gtx, 12, theme.Bold, hudWarn, txt) })
			return D{}
		})
	})
	fx := x + gtx.Dp(20) + ls.X + gtx.Dp(10)
	at(gtx, image.Pt(fx, r.Min.Y+(r.Dy()-rh)/2), image.Pt(fw, rh), func(gtx C) D {
		return button(gtx, &d.micFix, image.Pt(fw, rh), func(gtx C, hov bool) {
			bg := hudBtn
			if hov {
				bg = hudBtnHover
			}
			theme.RRect(gtx, image.Rectangle{Max: image.Pt(fw, rh)}, gtx.Dp(5), bg)
		}, func(gtx C) D { centerLabel(gtx, th, image.Pt(fw, rh), 12, theme.Bold, hudWhite, fix); return D{} })
	})
}
