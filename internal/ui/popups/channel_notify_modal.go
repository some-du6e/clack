package popups

// The bell menu's More options › Advanced settings: Slack's
// ChannelNotificationsPrefsModal (boot-shared RemI, build a84662d2d135,
// 2026-11-08), "CUSTOMIZE-CHANNEL-NOTIFICATIONS-MODAL". A 600px c-sk-modal
// with no footer: the header is a 40px tile with the setting's icon (n9iP
// Km at 24px), the title "Advanced settings" ("Advanced options" without
// temp_mute) and the subtitle n9iP tz ("Notifications set to all new
// posts", "… just mentions", "… nothing", "Notifications are muted").
// The body, top to bottom:
//
//   - after a failed write: a boxed error alert, "Unable to update your
//     notification preferences. Please try again later."
//   - YF16 hasIncoherentEverythingNotificationConfig (All new posts with
//     desktop and mobile push both off): a warning, "If you want to be
//     notified about **all new posts**, you’ll need to turn desktop or
//     mobile notifications on."
//   - "Desktop notifications" / "You’ll see a banner when new messages
//     come in." (desktop_push_enabled: the channel's override, else
//     global_desktop_push_enabled, on when unset).
//   - "Mobile notifications" (on unless the raw mobile override is
//     "nothing"; off writes nothing, on writes the desktop setting).
//   - Unmuted: "Show advanced mobile overrides" / "Hide …" with a caret
//     (disabled while mobile is off), which unfolds "Get different push
//     notifications on mobile" with the All new posts / Just mentions
//     radios ("Same as desktop" when they match, else "Messages and
//     threads you follow" / "@you, @channel, @here" or "@you"), then a
//     divider.
//   - Unmuted, desktop All new posts: "Badge every new message in your
//     sidebar" / "Show a badge (1) on the channel name for every new
//     message." (badge_all_unreads).
//   - Unmuted: "Follow every thread" / "Get notified about every new
//     thread and its replies." (follow_all_threads).
//   - Unmuted, desktop Just mentions: "Get broadcast messages" / "Be
//     notified anytime someone uses **@channel** or **@here**." (@everyone
//     in #general; on = suppress_at_channel false).
//
// Every switch writes at once, one pref per call: AvPY
// setChannelNotificationOverrideAndCallApi, users.prefs.setNotifications
// {name, value, channel_id, global: false, sync: false}
// (core.SetChannelNotifyPref), optimistic until Slack answers.
//
// Not drawn: the speedbump Slack shows when the browser or macOS blocks
// notifications ("Desktop notifications are disabled…", Enable Browser
// Notifications / Open System Settings / Download App). Clack can't read
// macOS's per-app notification setting (Slack's desktop app asks its
// Electron host, MACOS_NOTIFICATIONS_DISABLED), so desktop push is taken
// as allowed.

import (
	"context"
	"image"
	"image/color"
	"strconv"
	"strings"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// ChannelNotifyModal is the channel notifications modal for one channel.
type ChannelNotifyModal struct {
	h        Host
	ch       string
	modal    *Modal
	clicks   map[string]*widget.Clickable
	showOver bool              // the mobile overrides are unfolded
	failed   bool              // the last write failed (the error alert)
	pending  map[string]string // writes in flight, shown before Slack answers
}

// NewChannelNotifyModal builds the modal for ch; show it with ShowOverlay.
func NewChannelNotifyModal(h Host, ch string) *ChannelNotifyModal {
	m := &ChannelNotifyModal{h: h, ch: ch, clicks: map[string]*widget.Clickable{}, pending: map[string]string{}}
	m.modal = NewModal(h, ModalOpts{Width: 600, Body: m.layout, HeadH: 0.01, Hero: m.header, HeroH: 92})
	return m
}

func (m *ChannelNotifyModal) Layout(gtx layout.Context) layout.Dimensions { return m.modal.Layout(gtx) }
func (m *ChannelNotifyModal) Dismissed() bool                             { return m.modal.Dismissed() }
func (m *ChannelNotifyModal) Dismiss()                                    { m.modal.Dismiss() }

// cnState is what the modal shows, with writes in flight applied.
type cnState struct {
	Muted       bool
	Desktop     string // er: everything | mentions_dms | nothing
	DesktopPush bool   // e_
	MobileRaw   string // ec: the channel's raw mobile override
	Mobile      string // ef: the calculated mobile setting
	Badge       bool   // em
	Follow      bool   // ed
	Broadcast   bool   // eu: !suppress_at_channel
	General     bool
}

func (m *ChannelNotifyModal) state() cnState {
	a := m.h.Core()
	if a == nil {
		return cnState{Desktop: "mentions_dms", Mobile: "mentions_dms", DesktopPush: true, Broadcast: true}
	}
	n := a.NotifyPrefs(m.ch)
	for k, v := range m.pending {
		on := v == "true"
		switch k {
		case "desktop_push_enabled":
			n.DesktopPush = v
		case "mobile":
			n.Mobile = v
		case "badge_all_unreads":
			n.BadgeAllUnreads = on
		case "follow_all_threads":
			n.FollowAllThreads = on
		case "suppress_at_channel":
			n.SuppressAtChannel = on
		}
	}
	p := a.Prefs()
	s := cnState{
		Muted:       n.Muted || actIsMuted(m.h, m.ch),
		Desktop:     calcNotify(n.Desktop, p.GlobalDesktop),
		DesktopPush: !p.NoDesktopPush,
		MobileRaw:   n.Mobile,
		Mobile:      calcNotify(n.Mobile, p.GlobalMobile),
		Badge:       n.BadgeAllUnreads,
		Follow:      n.FollowAllThreads,
		Broadcast:   !n.SuppressAtChannel,
	}
	if n.DesktopPush != "" { // getCalculatedDesktopPushNotificationsEnabledSetting
		s.DesktopPush = n.DesktopPush == "true"
	}
	if c, ok := a.Conv(m.ch); ok {
		s.General = c.IsGeneral
	}
	return s
}

// Subtitle is the header's second line (n9iP tz).
func (m *ChannelNotifyModal) Subtitle() string {
	s := m.state()
	switch { // n9iP's (channel_sidebar)
	case s.Muted:
		return nsSidebar.T("Notifications are muted")
	case s.Desktop == "everything":
		return nsSidebar.T("Notifications set to all new posts")
	case s.Desktop == "nothing":
		return nsSidebar.T("Notifications set to nothing")
	}
	return nsSidebar.T("Notifications set to just mentions")
}

// Rows are the switches and links shown, in order: desktop, mobile,
// overrides, badge, follow, broadcast.
func (m *ChannelNotifyModal) Rows() []string {
	s := m.state()
	rows := []string{"desktop", "mobile"}
	if !s.Muted {
		rows = append(rows, "overrides")
		if s.Desktop == "everything" {
			rows = append(rows, "badge")
		}
		rows = append(rows, "follow")
		if s.Desktop == "mentions_dms" {
			rows = append(rows, "broadcast")
		}
	}
	return rows
}

// On reports a switch's state (desktop, mobile, badge, follow, broadcast).
func (m *ChannelNotifyModal) On(row string) bool {
	s := m.state()
	switch row {
	case "desktop":
		return s.DesktopPush
	case "mobile":
		return s.MobileRaw != "nothing"
	case "badge":
		return s.Badge
	case "follow":
		return s.Follow
	case "broadcast":
		return s.Broadcast
	}
	return false
}

// Failed reports whether the error alert shows.
func (m *ChannelNotifyModal) Failed() bool { return m.failed }

// Pending reports whether a write is in flight.
func (m *ChannelNotifyModal) Pending() bool { return len(m.pending) > 0 }

// Toggle flips a switch the way its onChange does (eL, eP, eR, eE, eM).
func (m *ChannelNotifyModal) Toggle(row string) {
	s := m.state()
	switch row {
	case "desktop":
		m.write("desktop_push_enabled", strconv.FormatBool(!s.DesktopPush))
	case "mobile":
		if s.MobileRaw != "nothing" {
			m.showOver = false
			m.write("mobile", "nothing")
		} else {
			m.write("mobile", s.Desktop) // er || "mentions_dms": er is never empty
		}
	case "badge":
		m.write("badge_all_unreads", strconv.FormatBool(!s.Badge))
	case "follow":
		m.write("follow_all_threads", strconv.FormatBool(!s.Follow))
	case "broadcast":
		m.write("suppress_at_channel", strconv.FormatBool(s.Broadcast))
	}
}

// ToggleOverrides unfolds or folds the mobile overrides (eq; the link is
// disabled while mobile notifications are off).
func (m *ChannelNotifyModal) ToggleOverrides() {
	if m.state().MobileRaw != "nothing" {
		m.showOver = !m.showOver
	}
}

// OverridesShown reports whether the mobile overrides are unfolded.
func (m *ChannelNotifyModal) OverridesShown() bool { return m.showOver }

// SetMobile picks a mobile override radio (eN; the radio's onChange runs
// only for the unchecked one).
func (m *ChannelNotifyModal) SetMobile(v string) {
	if v != m.state().Mobile {
		m.write("mobile", v)
	}
}

func (m *ChannelNotifyModal) write(name, value string) {
	a := m.h.Core()
	if a == nil {
		return
	}
	m.failed = false
	m.pending[name] = value
	ch := m.ch
	run(m.h, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, a.SetChannelNotifyPref(ctx, ch, name, value)
	}, func(_ struct{}, err error) {
		if m.pending[name] == value {
			delete(m.pending, name)
		}
		if err != nil {
			m.failed = true
		}
		m.h.Invalidate()
	})
}

func (m *ChannelNotifyModal) click(k string) *widget.Clickable {
	c := m.clicks[k]
	if c == nil {
		c = &widget.Clickable{}
		m.clicks[k] = c
	}
	return c
}

// header is the title bar: the icon tile, the title and the subtitle
// (padding 20 28; the close button sits at its right).
func (m *ChannelNotifyModal) header(gtx layout.Context) layout.Dimensions {
	th := m.h.Theme()
	cl := colorsOf(th)
	s := m.state()
	padX, tile := dp(gtx, 28), dp(gtx, 40)
	ty := dp(gtx, 20)
	at(gtx, image.Pt(padX, ty+(dp(gtx, 52)-tile)/2), func(gtx layout.Context) layout.Dimensions {
		rrect(gtx, image.Rect(0, 0, tile, tile), dp(gtx, 4), cl.BgAlt) // iconWrapper: base-sec, radius 4
		in := (tile - dp(gtx, 24)) / 2
		at(gtx, image.Pt(in, in), func(gtx layout.Context) layout.Dimensions {
			return icon(gtx, NotifyIcon(s.Desktop, s.Muted), 24, cl.Fg)
		})
		return layout.Dimensions{Size: image.Pt(tile, tile)}
	})
	x := padX + tile + dp(gtx, 12)
	maxW := maxInt(1, gtx.Constraints.Max.X-x-dp(gtx, 64))
	at(gtx, image.Pt(x, ty), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(maxW, dp(gtx, 30))}
		return para(gtx, th, 22, 30, theme.Black, cl.Fg, nsPrefs.T("Advanced settings"), 1)
	})
	at(gtx, image.Pt(x, ty+dp(gtx, 30)), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(maxW, dp(gtx, 22))}
		return para(gtx, th, 15, 22, theme.Regular, cl.Fg2, m.Subtitle(), 1)
	})
	return layout.Dimensions{Size: gtx.Constraints.Max}
}

// rtRuns are an rt() string's runs: <strong> bold, the placeholder named
// badge the mention badge, other placeholders (the @channel and @here
// tokens) bold.
func rtRuns(parts []i18n.Part, badge string) []cnRun {
	out := make([]cnRun, 0, len(parts))
	for _, p := range parts {
		switch {
		case badge != "" && p.Arg == badge:
			out = append(out, cnRun{s: p.Text, badge: true})
		case p.Arg != "" || p.Tag == "strong" || p.Tag == "b":
			out = append(out, cnRun{s: p.Text, bold: true})
		default:
			out = append(out, cnRun{s: p.Text})
		}
	}
	return out
}

// cnRun is a piece of a wrapping line: plain or bold text, or the mention
// badge.
type cnRun struct {
	s     string
	bold  bool
	badge bool
}

// cnRuns lays runs out as one wrapping paragraph (word by word).
func cnRuns(gtx layout.Context, th *theme.Theme, cl *Colors, runs []cnRun, size, lhDp float32, col color.NRGBA, w int) layout.Dimensions {
	lh := dp(gtx, lhDp)
	x, y := 0, 0
	space := textW(gtx, th, size, theme.Regular, " ")
	for _, r := range runs {
		if r.badge {
			bw := maxInt(dp(gtx, 18), textW(gtx, th, 12, theme.Bold, r.s)+dp(gtx, 10))
			if x > 0 && x+bw > w {
				x, y = 0, y+lh
			}
			bh := dp(gtx, 18)
			by := y + (lh-bh)/2
			rrect(gtx, image.Rect(x, by, x+bw, by+bh), bh/2, cl.Badge)
			at(gtx, image.Pt(x, by), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Exact(image.Pt(bw, bh))
				return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 12, theme.Bold, cl.BadgeText, r.s)
				})
			})
			x += bw
			continue
		}
		wt := theme.Regular
		if r.bold {
			wt = theme.Bold
		}
		for _, wd := range strings.SplitAfter(r.s, " ") {
			if wd == "" {
				continue
			}
			// a CJK run breaks between its characters (cjkChunks)
			for _, word := range cjkChunks(strings.TrimRight(wd, " ")) {
				ww := textW(gtx, th, size, wt, word)
				if x > 0 && x+ww > w {
					x, y = 0, y+lh
				}
				at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
						return text(gtx, th, size, wt, col, word)
					})
				})
				x += ww
			}
			if strings.HasSuffix(wd, " ") {
				x += space
			}
		}
	}
	return layout.Dimensions{Size: image.Pt(w, y+lh)}
}

// cnAlert is a c-alert box (radius 8, padding 12 16, the level's icon).
func cnAlert(gtx layout.Context, th *theme.Theme, cl *Colors, errLevel bool, runs []cnRun, w int) int {
	bg, fg, ic := theme.WithAlpha(rgb(0xffc600), 0x2e), rgb(0x6b5000), "warning" // surf-hgl-3, content-hgl-3
	if th.Dark {
		bg, fg = theme.WithAlpha(rgb(0xa07300), 0x2e), rgb(0xdea700)
	}
	if errLevel {
		bg, fg = theme.WithAlpha(rgb(0xff81aa), 0x21), cl.Danger // surf-imp, content-imp
		if th.Dark {
			bg = theme.WithAlpha(rgb(0xb41541), 0x40)
		}
	}
	px, py := dp(gtx, 16), dp(gtx, 12)
	tx := px + dp(gtx, 20) + dp(gtx, 8)
	call, d := record(gtx, func(gtx layout.Context) layout.Dimensions {
		return cnRuns(gtx, th, cl, runs, 15, 22, cl.Fg, maxInt(1, w-tx-px))
	})
	h := d.Size.Y + 2*py
	rrect(gtx, image.Rect(0, 0, w, h), dp(gtx, 8), bg)
	at(gtx, image.Pt(px, py+dp(gtx, 1)), func(gtx layout.Context) layout.Dimensions { return icon(gtx, ic, 20, fg) })
	at(gtx, image.Pt(tx, py), func(gtx layout.Context) layout.Dimensions {
		call.Add(gtx.Ops)
		return d
	})
	return h
}

// switchRow is a label (15/22 bold, an optional 15/22 content-ter
// subtext) with the switch at its right: .label padding 8 0 4, the whole
// row clickable.
func (m *ChannelNotifyModal) switchRow(gtx layout.Context, th *theme.Theme, cl *Colors, key, label string, sub []cnRun, w int) int {
	on := m.On(key)
	c := m.click(key)
	for c.Clicked(gtx) {
		m.Toggle(key)
		on = m.On(key)
	}
	tw := dp(gtx, 28)
	lw := w - tw - dp(gtx, 16)
	call, d := record(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(lw, 1<<20)}
		y := 0
		dl := para(gtx, th, 15, 22, theme.Bold, cl.Fg, label, 0)
		y += dl.Size.Y
		if len(sub) > 0 {
			ds := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
				return cnRuns(gtx, th, cl, sub, 15, 22, cl.Fg3, lw)
			})
			y += ds.Size.Y
		}
		return layout.Dimensions{Size: image.Pt(lw, y)}
	})
	top, bot := dp(gtx, 8), dp(gtx, 4)
	h := top + d.Size.Y + bot
	gtx.Constraints = layout.Exact(image.Pt(w, h))
	clickBox(gtx, c, nil, func(gtx layout.Context) layout.Dimensions {
		at(gtx, image.Pt(0, top), func(gtx layout.Context) layout.Dimensions {
			call.Add(gtx.Ops)
			return d
		})
		at(gtx, image.Pt(w-tw, top+(d.Size.Y-dp(gtx, 16))/2), func(gtx layout.Context) layout.Dimensions {
			return Toggle(gtx, th, on, false)
		})
		return layout.Dimensions{Size: image.Pt(w, h)}
	})
	return h
}

func (m *ChannelNotifyModal) layout(gtx layout.Context) layout.Dimensions {
	th := m.h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	s := m.state()
	y := 0
	place := func(f func(gtx layout.Context) int) {
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
		at(g, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			h := f(gtx)
			y += h
			return layout.Dimensions{Size: image.Pt(w, h)}
		})
	}
	if m.failed {
		place(func(gtx layout.Context) int {
			return cnAlert(gtx, th, cl, true, []cnRun{{s: nsPrefs.T("Unable to update your notification preferences. Please try again later.")}}, w) + dp(gtx, 12)
		})
	}
	if s.Desktop == "everything" && !s.DesktopPush && s.MobileRaw == "nothing" {
		place(func(gtx layout.Context) int {
			return cnAlert(gtx, th, cl, false, rtRuns(nsPrefs.RT("If you want to be notified about <strong>all new posts</strong>, you’ll need to turn desktop or mobile notifications on."), ""), w) + dp(gtx, 16)
		})
	}
	place(func(gtx layout.Context) int {
		return m.switchRow(gtx, th, cl, "desktop", nsPrefs.T("Desktop notifications"), []cnRun{{s: nsPrefs.T("You’ll see a banner when new messages come in.")}}, w)
	})
	place(func(gtx layout.Context) int {
		return m.switchRow(gtx, th, cl, "mobile", nsPrefs.T("Mobile notifications"), nil, w)
	})
	if !s.Muted {
		place(func(gtx layout.Context) int { return m.overrides(gtx, th, cl, s, w) })
		place(func(gtx layout.Context) int { // hr.divider: margin 4px 20px
			line := maxInt(1, dp(gtx, 1))
			fill(gtx, image.Rect(0, dp(gtx, 4), w, dp(gtx, 4)+line), cl.Divider)
			return dp(gtx, 8) + line
		})
		if s.Desktop == "everything" {
			place(func(gtx layout.Context) int {
				return m.switchRow(gtx, th, cl, "badge", nsPrefs.T("Badge every new message in your sidebar"),
					rtRuns(nsPrefs.RT("Show a badge ({unreadBadge}) on the channel name for every new message.", "unreadBadge", "1"), "unreadBadge"), w)
			})
		}
		place(func(gtx layout.Context) int {
			return m.switchRow(gtx, th, cl, "follow", nsPrefs.T("Follow every thread"), []cnRun{{s: nsPrefs.T("Get notified about every new thread and its replies.")}}, w)
		})
		if s.Desktop == "mentions_dms" {
			// "@" and getBroadcastKeywordName (F1pD, the broadcast_keywords
			// I18n: es-ES "@canal", "@todos", "@aquí")
			who := "@" + nsBroadcast.T("channel")
			if s.General {
				who = "@" + nsBroadcast.T("everyone")
			}
			place(func(gtx layout.Context) int {
				return m.switchRow(gtx, th, cl, "broadcast", nsPrefs.T("Get broadcast messages"),
					rtRuns(nsPrefs.RT("Be notified anytime someone uses {atChannel} or {atHere}.", "atChannel", who, "atHere", "@"+nsBroadcast.T("here")), ""), w)
			})
		}
	}
	y += dp(gtx, 12) // margin_bottom_75
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// overrides is the "Show advanced mobile overrides" link (margin -8 0 8;
// 16 under it while folded) and, unfolded, the mobile banner.
func (m *ChannelNotifyModal) overrides(gtx layout.Context, th *theme.Theme, cl *Colors, s cnState, w int) int {
	off := s.MobileRaw == "nothing"
	link := m.click("overrides")
	for link.Clicked(gtx) {
		m.ToggleOverrides()
	}
	label, caret := nsPrefs.T("Show advanced mobile overrides"), "caret-down"
	if m.showOver {
		label, caret = nsPrefs.T("Hide advanced mobile overrides"), "caret-up"
	}
	col := cl.Link
	if off {
		col = cl.Fg3
	}
	lh := dp(gtx, 22)
	tw := textW(gtx, th, 15, theme.Regular, label)
	lw := tw + dp(gtx, 4) + dp(gtx, 20)
	y := -dp(gtx, 8)
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(lw, lh))
		draw := func(gtx layout.Context) layout.Dimensions {
			hov := !off && link.Hovered()
			at(gtx, image.Point{}, func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions { return text(gtx, th, 15, theme.Regular, col, label) })
			})
			if hov { // :hover underline
				fill(gtx, image.Rect(0, lh-dp(gtx, 4), tw, lh-dp(gtx, 4)+maxInt(1, dp(gtx, 1))), col)
			}
			at(gtx, image.Pt(tw+dp(gtx, 4), (lh-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions { return icon(gtx, caret, 20, col) })
			return layout.Dimensions{Size: image.Pt(lw, lh)}
		}
		if off {
			return draw(gtx)
		}
		return clickBox(gtx, link, nil, draw)
	})
	y += lh
	if !m.showOver {
		return y + dp(gtx, 16)
	}
	y += dp(gtx, 8)
	// .mobileOverrideBanner: base-sec (dark: surf-pry), radius 4, padding
	// 8 16, margin-bottom 16
	px, py := dp(gtx, 16), dp(gtx, 8)
	iw := w - 2*px
	call, d := record(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(iw, 1<<20)}
		yy := para(gtx, th, 15, 22, theme.Regular, cl.Fg, nsPrefs.T("Get different push notifications on mobile"), 0).Size.Y + dp(gtx, 8)
		same := func(v string) bool { return s.Mobile == v && s.Desktop == v }
		everySub := nsSidebar.T("Messages and threads you follow")
		if same("everything") {
			everySub = nsPrefs.T("Same as desktop")
		}
		mentSub := nsSidebar.T("@you, @channel, @here")
		if !s.Broadcast {
			mentSub = nsSidebar.T("@you")
		}
		if same("mentions_dms") {
			mentSub = nsPrefs.T("Same as desktop")
		}
		for _, r := range []struct{ v, label, sub string }{{"everything", nsSidebar.T("All new posts"), everySub}, {"mentions_dms", nsSidebar.T("Just mentions"), mentSub}} {
			r := r
			c := m.click("mob:" + r.v)
			for c.Clicked(gtx) {
				m.SetMobile(r.v)
			}
			dd := at(gtx, image.Pt(0, yy), func(gtx layout.Context) layout.Dimensions {
				return clickBox(gtx, c, nil, func(gtx layout.Context) layout.Dimensions {
					at(gtx, image.Pt(0, dp(gtx, 2)), func(gtx layout.Context) layout.Dimensions { return radio(gtx, cl, s.Mobile == r.v) })
					tx := dp(gtx, 26)
					gtx.Constraints = layout.Constraints{Max: image.Pt(iw-tx, 1<<20)}
					t := at(gtx, image.Pt(tx, 0), func(gtx layout.Context) layout.Dimensions {
						return para(gtx, th, 15, 22, theme.Regular, cl.Fg, r.label, 0)
					})
					sd := at(gtx, image.Pt(tx, t.Size.Y), func(gtx layout.Context) layout.Dimensions {
						return para(gtx, th, 15, 22, theme.Regular, cl.Fg3, r.sub, 0)
					})
					return layout.Dimensions{Size: image.Pt(iw, t.Size.Y+sd.Size.Y)}
				})
			})
			yy += dd.Size.Y + dp(gtx, 8)
		}
		return layout.Dimensions{Size: image.Pt(iw, yy-dp(gtx, 8))}
	})
	bh := d.Size.Y + 2*py
	bg := cl.BgAlt
	if th.Dark {
		bg = theme.WithAlpha(rgb(0xf8f8f8), 0x0f)
	}
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		rrect(gtx, image.Rect(0, 0, w, bh), dp(gtx, 4), bg)
		at(gtx, image.Pt(px, py), func(gtx layout.Context) layout.Dimensions {
			call.Add(gtx.Ops)
			return d
		})
		return layout.Dimensions{Size: image.Pt(w, bh)}
	})
	return y + bh + dp(gtx, 16)
}
