package popups

import (
	"context"
	"image"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Hooks the ui root may set; nil hooks toast instead.
var (
	// OpenChannelDetails opens the channel details pane for convID.
	OpenChannelDetails func(convID string)
	// OpenPreferences opens the preferences view.
	OpenPreferences func()
	// SignOut signs out (forgets credentials).
	SignOut func()
	// OpenOwnProfile opens the viewer's full profile.
	OpenOwnProfile func()
	// CheckForUpdates is the workspace menu's "Check for updates…" (Linux
	// and Windows, where Clack has no menu bar). Nil: no item.
	CheckForUpdates func()
)

// actDNDUntil is when the viewer's notification snooze ends (zero = not
// snoozed), as set by this package. UI goroutine only.
var actDNDUntil time.Time

func actPaused(now time.Time) bool { return !actDNDUntil.IsZero() && now.Before(actDNDUntil) }

func actPreferences(h Host) func() {
	return func() {
		if OpenPreferences != nil {
			OpenPreferences()
			return
		}
		h.Toast("Preferences aren't available yet")
	}
}

func actSignOut(h Host) func() {
	return func() {
		if SignOut != nil {
			SignOut()
			return
		}
		h.Toast("Sign out isn't available here")
	}
}

func actCopy(h Host, s, msg string) func(gtx layout.Context) {
	return func(gtx layout.Context) {
		copyText(gtx, s)
		if msg != "" {
			h.Toast(msg)
		}
	}
}

// ---- channel menu (header ⋮) ----

// NewChannelMenu builds the channel header menu (right-aligned to anchor).
func NewChannelMenu(h Host, anchor image.Point, convID string) *Menu {
	return newChannelMenu(h, anchor, convID, true)
}

func newChannelMenu(h Host, anchor image.Point, convID string, alignRight bool) *Menu {
	var c core.Conv
	if a := h.Core(); a != nil {
		c, _ = a.Conv(convID)
	}
	if c.ID == "" {
		c.ID = convID
	}
	name := convTitle(h, c)
	if name == "" {
		name = convID
	}
	dm := c.Kind == "im" || c.Kind == "mpim" || strings.HasPrefix(convID, "D")
	details := func() {
		if OpenChannelDetails != nil {
			OpenChannelDetails(convID)
			return
		}
		h.Toast("Channel details aren't available yet")
	}
	link := "https://" + teamDomain(h) + ".slack.com/archives/" + convID

	detailsLabel := "Channel details"
	detailSub := []MenuItem{
		{Label: "Open channel details", Do: details},
		{Label: "View members", Do: details},
		{Label: "Copy channel ID", DoCtx: actCopy(h, convID, "Channel ID copied")},
	}
	if dm {
		detailsLabel = "Conversation details"
		detailSub = []MenuItem{
			{Label: "Open conversation details", Do: details},
			{Label: "Copy conversation ID", DoCtx: actCopy(h, convID, "Conversation ID copied")},
		}
	}
	noun := "channel"
	if dm {
		noun = "conversation"
	}
	starLabel, unstarLabel := "Star "+noun, "Unstar "+noun
	muteLabel, unmuteLabel := "Mute "+noun, "Unmute "+noun
	items := []MenuItem{
		{Label: detailsLabel, Sub: detailSub},
		{Label: "Copy", Sub: []MenuItem{
			{Label: "Copy link", DoCtx: actCopy(h, link, "Link copied")},
			{Label: "Copy name", DoCtx: actCopy(h, name, "Name copied")},
		}},
		{Widget: actLabelRow(h, func() string {
			if cxStarred(h, convID) {
				return unstarLabel
			}
			return starLabel
		}), Do: func() { actToggleStar(h, convID, noun) }},
	}
	if !dm {
		items = append(items, MenuItem{Label: "Move channel", Do: actToast(h, "Moving channels between sections isn't supported yet")})
	}
	items = append(items,
		Sep(),
		MenuItem{Label: "Add a workflow", Do: actToast(h, "Workflows aren't supported in yoloslack")},
		MenuItem{Label: "Edit settings", Do: actToast(h, "Channel settings aren't supported yet")},
		Sep(),
		MenuItem{Label: "Open in split view", Icon: "panel-right", Do: func() {
			// ui-shell's *ui.App exposes OpenSplit (right-hand pane).
			if s, ok := h.(interface{ OpenSplit(route string) }); ok {
				s.OpenSplit("c/" + convID)
				return
			}
			h.Toast("Split view isn't supported yet")
		}},
		Sep(),
	)
	if dm { // Slack's channel menu has no mute row (that's the header bell)
		items = append(items, MenuItem{Widget: actLabelRow(h, func() string {
			if actIsMuted(h, convID) {
				return unmuteLabel
			}
			return muteLabel
		}), Do: func() { actToggleMute(h, convID, noun) }})
	}
	if dm {
		items = append(items, MenuItem{Label: "Close conversation", Do: func() { actLeave(h, convID, "Conversation closed") }})
	} else {
		items = append(items, MenuItem{Label: "Leave channel", Danger: true, Do: func() {
			msg := "You can rejoin this channel at any time."
			if c.Kind == "private" {
				msg = "You won't be able to rejoin this private channel unless someone invites you."
			}
			Confirm(h, "Leave #"+name+"?", msg, "Leave channel", true, nil, func() { actLeave(h, convID, "You left #"+name) })
		}})
	}
	m := NewMenuOpts(h, items, MenuOpts{Anchor: pointRect(anchor), AlignRight: alignRight, Wraps: []MenuWrap{
		// Slack's channel context menu: ChatTabs marks the channel, then
		// extends the trigger's getTemplate ("Open in new Slick tab")
		{Name: "ChannelContextMenu", Props: slackc.Props{"id": convID}},
		{Name: "ContextMenuTriggerWithKeyboardFocus", Props: slackc.Props{
			"desktopAppOnly": false, "showSlackKitContextMenuOnDesktop": true, "overrideDefaultMenu": true,
			"menuAriaLabel": "Channel options", "width": "300px",
		}},
	}})
	tautMenu(h, m, "getContextMenuTemplate", convID) // QuickJoin et al.; free without plugins
	return m
}

// ---- sidebar conversation menu (the shell's row right-click) ----

// ChannelMenuInfo is what Slack's sidebar conversation menu depends on.
// ChannelMenuAction is what popups' channel menu does for a sidebar menu
// key, exactly as NewChannelMenu's rows do it: toggle_star and
// toggle_mute through Slack (stars.add/remove, users.prefs) with their
// toasts, leave after Slack's "Leave #name?" confirmation (private
// channels warn that rejoining needs an invite), close for DMs. nil for
// the keys the caller handles itself.
func ChannelMenuAction(h Host, convID, key string) func() {
	var c core.Conv
	if a := h.Core(); a != nil {
		c, _ = a.Conv(convID)
	}
	dm := c.Kind == "im" || c.Kind == "mpim" || c.Kind == "app" || strings.HasPrefix(convID, "D")
	noun := "channel"
	if dm {
		noun = "conversation"
	}
	switch key {
	case "toggle_star":
		return func() { actToggleStar(h, convID, noun) }
	case "toggle_mute":
		return func() { actToggleMute(h, convID, noun) }
	case "close":
		return func() { actLeave(h, convID, "Conversation closed") }
	case "leave":
		if dm {
			return func() { actLeave(h, convID, "Conversation closed") }
		}
		name := convTitle(h, c)
		if name == "" {
			name = convID
		}
		return func() {
			msg := "You can rejoin this channel at any time."
			if c.Kind == "private" {
				msg = "You won't be able to rejoin this private channel unless someone invites you."
			}
			Confirm(h, "Leave #"+name+"?", msg, "Leave channel", true, nil, func() { actLeave(h, convID, "You left #"+name) })
		}
	}
	return nil
}

type ChannelMenuInfo struct {
	ID, Kind       string // Kind: core's ("channel", "private", "im", "mpim", "app")
	Muted, Starred bool
}

func (c ChannelMenuInfo) dm() bool { return c.Kind == "im" || c.Kind == "mpim" || c.Kind == "app" }

// ChannelMenuItems are Slack's sidebar conversation menu for c, with
// stable keys (view_details, copy_link, copy_name, toggle_star,
// toggle_mute, mark_read, leave or close, and separators). do, when
// non-nil, gives each key's action. Pure: safe on any goroutine, so a
// getTemplate JS calls can build its template from it.
func ChannelMenuItems(c ChannelMenuInfo, do func(key string) func()) []MenuItem {
	noun := "channel"
	if c.dm() {
		noun = "conversation"
	}
	star, mute := "Star "+noun, "Mute "+noun
	if c.Starred {
		star = "Unstar " + noun
	}
	if c.Muted {
		mute = "Unmute " + noun
	}
	items := []MenuItem{
		{Key: "view_details", Label: "View " + noun + " details"},
		{Key: "copy_link", Label: "Copy link"},
		{Key: "copy_name", Label: "Copy name"},
		{Key: "separator_1", Separator: true},
		{Key: "toggle_star", Label: star},
		{Key: "toggle_mute", Label: mute},
		{Key: "mark_read", Label: "Mark as read"},
		{Key: "separator_2", Separator: true},
	}
	if c.dm() {
		items = append(items, MenuItem{Key: "close", Label: "Close conversation"})
	} else {
		items = append(items, MenuItem{Key: "leave", Label: "Leave channel", Danger: true})
	}
	if do != nil {
		for i := range items {
			if !items[i].Separator {
				items[i].Do = do(items[i].Key)
			}
		}
	}
	return items
}

// MenuTemplate is items as Slack's MenuTemplateItem[] in plain data (no
// click functions): what a ContextMenuTriggerWithKeyboardFocus's
// getTemplate returns for a plugin to extend. Keys are the items' Key, or
// a slug of their label.
func MenuTemplate(items []MenuItem) []map[string]any { return tautTemplate(items) }

// ItemsFromTemplate turns a MenuTemplateItem[] a plugin returned (from a
// getTemplate it wrapped) back into items: base's by key, keeping their
// actions, and the plugin's own with their click (MenuFromTemplate's
// conversion).
func ItemsFromTemplate(th *theme.Theme, list []any, base []MenuItem) []MenuItem {
	return menuFromTemplate(th, list, base)
}

// actMuteSet remembers mutes done here until core's conversation catches
// up (UI goroutine only).
var actMuteSet = map[string]bool{}

func actIsMuted(h Host, ch string) bool {
	if m, ok := actMuteSet[ch]; ok {
		return m
	}
	return cxMuted(h, ch)
}

// actToggleStar stars/unstars ch (cxStar, then cxNoteStar on success).
func actToggleStar(h Host, ch, noun string) {
	if h.Core() == nil {
		h.Toast("Not connected yet")
		return
	}
	star := !cxStarred(h, ch)
	run(h, func(ctx context.Context) (struct{}, error) { return struct{}{}, cxStar(ctx, h, ch, star) }, func(_ struct{}, err error) {
		if err != nil {
			h.Toast("Couldn't do that: " + errText(err))
			return
		}
		cxNoteStar(ch, star)
		msg := strings.ToUpper(noun[:1]) + noun[1:] + " unstarred"
		if star {
			msg = strings.ToUpper(noun[:1]) + noun[1:] + " starred"
		}
		h.Toast(msg)
	})
}

// actToggleMute mutes/unmutes ch through cxMute.
func actToggleMute(h Host, ch, noun string) {
	if h.Core() == nil {
		h.Toast("Not connected yet")
		return
	}
	mute := !actIsMuted(h, ch)
	run(h, func(ctx context.Context) (struct{}, error) { return struct{}{}, cxMute(ctx, h, ch, mute) }, func(_ struct{}, err error) {
		if err != nil {
			h.Toast("Couldn't do that: " + errText(err))
			return
		}
		actMuteSet[ch] = mute
		msg := strings.ToUpper(noun[:1]) + noun[1:] + " unmuted"
		if mute {
			msg = strings.ToUpper(noun[:1]) + noun[1:] + " muted"
		}
		h.Toast(msg)
	})
}

// actLabelRow is a plain 28px menu row whose label is read every frame
// (Star/Unstar flips when stars.list arrives).
func actLabelRow(h Host, label func() string) func(gtx layout.Context, hl bool) layout.Dimensions {
	return func(gtx layout.Context, hl bool) layout.Dimensions {
		th := h.Theme()
		cl := colorsOf(th)
		sz := image.Pt(gtx.Constraints.Max.X, dp(gtx, 28))
		fg := cl.Fg
		if hl {
			fill(gtx, image.Rectangle{Max: sz}, cl.Highlight)
			fg = cl.HighText
		}
		at(gtx, image.Pt(dp(gtx, 24), 0), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, sz.X-dp(gtx, 48)), sz.Y)}
			return vcenter(gtx, sz.Y, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, theme.Regular, fg, label())
			})
		})
		return layout.Dimensions{Size: sz}
	}
}

func actLeave(h Host, convID, okMsg string) {
	a := h.Core()
	if a == nil {
		h.Toast("Not connected yet")
		return
	}
	run(h, func(ctx context.Context) (struct{}, error) { return struct{}{}, a.Leave(ctx, convID) }, func(_ struct{}, err error) {
		if err != nil {
			h.Toast("Couldn't do that: " + errText(err))
			return
		}
		h.Toast(okMsg)
		h.Navigate("threads")
	})
}

// ---- team menu (Hack Club ▾) ----

// NewTeamMenu builds the workspace menu (320 wide) below anchor.
func NewTeamMenu(h Host, anchor image.Point) *Menu {
	snap, _ := actSnapshot(h)
	team := snap.Team
	if team.Name == "" {
		team.Name = "Hack Club"
	}
	domain := teamDomain(h) + ".slack.com"
	if snap.EnterpriseID != "" || team.Domain == "" || team.Domain == "hackclub" {
		domain = teamDomain(h) + ".enterprise.slack.com"
	}
	header := func(gtx layout.Context, hl bool) layout.Dimensions {
		th := h.Theme()
		cl := colorsOf(th)
		sz := image.Pt(gtx.Constraints.Max.X, dp(gtx, 52))
		fg, fg2, chev := cl.Fg, cl.Fg2, cl.Fg2
		if hl {
			fill(gtx, image.Rectangle{Max: sz}, cl.Highlight)
			fg, fg2, chev = cl.HighText, cl.HighText, cl.HighText
		}
		x := dp(gtx, 24)
		at(gtx, image.Pt(x, (sz.Y-dp(gtx, 36))/2), func(gtx layout.Context) layout.Dimensions {
			return avatar(gtx, h, team.Icon, 36)
		})
		x += dp(gtx, 48)
		tw := maxInt(1, sz.X-x-dp(gtx, 24+15+8))
		at(gtx, image.Pt(x, dp(gtx, 6)), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(tw, dp(gtx, 22))}
			return text(gtx, th, 15, theme.Bold, fg, team.Name)
		})
		at(gtx, image.Pt(x, dp(gtx, 27)), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(tw, dp(gtx, 20))}
			return text(gtx, th, 13, theme.Regular, fg2, domain)
		})
		at(gtx, image.Pt(sz.X-dp(gtx, 24+15), (sz.Y-dp(gtx, 15))/2), func(gtx layout.Context) layout.Dimensions {
			return icon(gtx, "chevron-right", 15, chev)
		})
		return layout.Dimensions{Size: sz}
	}
	items := []MenuItem{
		{Widget: header, Do: actToast(h, team.Name+" is the only workspace you're signed in to")},
		Sep(),
		{Label: "Invite people to " + team.Name, Do: actToast(h, "Invites aren't supported in yoloslack")},
		Sep(),
		{Label: "Preferences", Do: actPreferences(h)},
	}
	if f := CheckForUpdates; f != nil {
		items = append(items, MenuItem{Key: "check-for-updates", Label: "Check for updates…", Do: f})
	}
	items = append(items, []MenuItem{
		{Label: "Tools", Sub: []MenuItem{
			{Label: "Workflows", Do: actToast(h, "Workflows aren't supported in yoloslack")},
			{Label: "Analytics", Do: actToast(h, "Analytics aren't supported in yoloslack")},
		}},
		Sep(),
		{Label: "Sign in on mobile", Do: actToast(h, "Use the Slack mobile app to sign in on your phone")},
		{Label: "Join or leave workspaces", External: true, Do: actToast(h, "Manage workspaces at app.slack.com")},
		{Label: "Sign out", Do: actSignOut(h)},
		{Label: "Open the Slack App", Do: actToast(h, "You're already in a Slack app")},
	}...)
	return NewMenuOpts(h, items, MenuOpts{Anchor: pointRect(anchor), Width: 320})
}

// ---- user menu (avatar, bottom-left; opens upward) ----

// NewUserMenu builds the viewer's menu (300 wide) above anchor.
func NewUserMenu(h Host, anchor image.Point) *Menu {
	var self core.User
	if a := h.Core(); a != nil {
		self = a.Self()
	}
	snap, _ := actSnapshot(h)
	teamName := snap.Team.Name
	if teamName == "" {
		teamName = "Hack Club"
	}
	away := self.Presence == "away"
	hasStatus := self.StatusText != "" || self.StatusEmoji != ""
	now := time.Now()
	paused := actPaused(now)

	var menu *Menu
	var clearBtn widget.Clickable
	cleared := false
	clearStatus := func() {
		cleared = true
		actDo(h, "Status cleared", func(ctx context.Context, a *core.App) error { return a.SetStatus(ctx, "", "", 0) })
	}

	header := func(gtx layout.Context, _ bool) layout.Dimensions {
		th := h.Theme()
		cl := colorsOf(th)
		sz := image.Pt(gtx.Constraints.Max.X, dp(gtx, 56))
		x := dp(gtx, 24)
		at(gtx, image.Pt(x, dp(gtx, 10)), func(gtx layout.Context) layout.Dimensions {
			return avatar(gtx, h, self.Avatar, 36)
		})
		x += dp(gtx, 48)
		tw := maxInt(1, sz.X-x-dp(gtx, 24))
		at(gtx, image.Pt(x, dp(gtx, 9)), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(tw, dp(gtx, 22))}
			return text(gtx, th, 15, theme.Black, cl.Fg, userName(self))
		})
		label := "Active"
		if away {
			label = "Away"
		}
		at(gtx, image.Pt(x, dp(gtx, 30)), func(gtx layout.Context) layout.Dimensions {
			h20 := dp(gtx, 18)
			at(gtx, image.Pt(0, (h20-dp(gtx, 8))/2), func(gtx layout.Context) layout.Dimensions {
				return presenceDot(gtx, th, !away, 8)
			})
			at(gtx, image.Pt(dp(gtx, 14), 0), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, h20, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 13, theme.Regular, cl.Fg2, label)
				})
			})
			return layout.Dimensions{}
		})
		return layout.Dimensions{Size: sz}
	}

	status := func(gtx layout.Context, hl bool) layout.Dimensions {
		th := h.Theme()
		cl := colorsOf(th)
		sz := image.Pt(gtx.Constraints.Max.X, dp(gtx, 55))
		box := image.Rect(dp(gtx, 20), dp(gtx, 8), sz.X-dp(gtx, 20), dp(gtx, 48))
		border := cl.InputBorder
		if hl {
			border = cl.Focus
		}
		rrect(gtx, box, dp(gtx, 4), border)
		rrect(gtx, box.Inset(1), dp(gtx, 4)-1, cl.Bg)
		bh := box.Dy()
		x := box.Min.X + dp(gtx, 12)
		right := box.Max.X - dp(gtx, 12)
		if hasStatus {
			if clearBtn.Clicked(gtx) && !cleared {
				clearStatus()
				if menu != nil {
					menu.closeAll()
				}
			}
			cs := dp(gtx, 24)
			right = box.Max.X - dp(gtx, 6) - cs
			at(gtx, image.Pt(right, box.Min.Y+(bh-cs)/2), func(gtx layout.Context) layout.Dimensions {
				return iconButton(gtx, &clearBtn, "close-small", 24, 16, cl.Fg2, cl.BgHover, 4)
			})
			right -= dp(gtx, 4)
			if self.StatusEmoji != "" {
				at(gtx, image.Pt(x, box.Min.Y+(bh-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions {
					return emojiImage(gtx, h, self.StatusEmoji, 20)
				})
			}
		} else {
			at(gtx, image.Pt(x, box.Min.Y+(bh-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, "emoji", 20, cl.Fg3)
			})
		}
		x += dp(gtx, 28)
		label, col := "Update your status", cl.Fg3
		if hasStatus {
			label, col = self.StatusText, cl.Fg
			if label == "" {
				label = strings.Trim(self.StatusEmoji, ":")
			}
		}
		at(gtx, image.Pt(x, box.Min.Y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, right-x), bh)}
			return vcenter(gtx, bh, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, theme.Regular, col, label)
			})
		})
		return layout.Dimensions{Size: sz}
	}

	presence := MenuItem{Widget: actRichRow(h, "Set yourself as ", "away"), Do: func() {
		actDo(h, "You're set to away", func(ctx context.Context, a *core.App) error { return a.SetPresence(ctx, "away") })
	}}
	if away {
		presence = MenuItem{Widget: actRichRow(h, "Set yourself as ", "active"), Do: func() {
			actDo(h, "You're set to active", func(ctx context.Context, a *core.App) error { return a.SetPresence(ctx, "auto") })
		}}
	}

	snooze := func(minutes func(now time.Time) int) func() {
		return func() { actSnooze(h, minutes(time.Now())) }
	}
	fixed := func(n int) func(time.Time) int { return func(time.Time) int { return n } }
	var notif []MenuItem
	if paused {
		notif = append(notif, MenuItem{Label: "Resume notifications", Do: func() {
			a := h.Core()
			if a == nil {
				h.Toast("Not connected yet")
				return
			}
			run(h, func(ctx context.Context) (struct{}, error) { return struct{}{}, a.SetDND(ctx, 0) }, func(_ struct{}, err error) {
				if err != nil {
					h.Toast("Couldn't do that: " + errText(err))
					return
				}
				actDNDUntil = time.Time{}
				h.Toast("Notifications resumed")
			})
		}}, Sep())
	}
	notif = append(notif,
		MenuItem{Header: "Pause notifications for…"},
		MenuItem{Label: "30 minutes", Do: snooze(fixed(30))},
		MenuItem{Label: "1 hour", Do: snooze(fixed(60))},
		MenuItem{Label: "2 hours", Do: snooze(fixed(120))},
		MenuItem{Label: "Until tomorrow", Do: snooze(func(now time.Time) int { return actMinutesUntil(now, actTomorrow9(now)) })},
		MenuItem{Label: "Until next week", Do: snooze(func(now time.Time) int { return actMinutesUntil(now, actNextMonday9(now)) })},
		MenuItem{Label: "Custom…", Do: func() { h.ShowOverlay(actNewPauseModal(h)) }},
		Sep(),
		MenuItem{Label: "Edit your working hours", Do: func() { openPreferences(h, "availability") }},
		MenuItem{Note: "You can choose to always allow notifications from VIPs in preferences."},
	)
	notifState := "On"
	if paused {
		notifState = "Paused"
	}

	items := []MenuItem{
		{Widget: header},
		{Widget: status, DoCtx: func(gtx layout.Context) {
			if hasStatus && (cleared || clearBtn.Clicked(gtx)) {
				if !cleared {
					clearStatus()
				}
				return
			}
			h.ShowOverlay(NewSetStatusModalFromMenu(h))
		}},
		presence,
		{Label: "Notifications", Right: notifState, Sub: notif},
		Sep(),
		{Label: "Profile", Do: func() {
			if OpenOwnProfile != nil {
				OpenOwnProfile()
				return
			}
			h.Toast("Profile isn't available yet")
		}},
		{Label: "Preferences", Do: actPreferences(h)},
		{Label: "Account settings", External: true, Do: actToast(h, "Account settings live at app.slack.com")},
		Sep(),
		{Key: "sign-out", Label: "Sign out of " + teamName, Do: actSignOut(h)},
	}
	menu = NewMenuOpts(h, items, MenuOpts{Anchor: pointRect(anchor), Side: Above})
	return menu
}

// actSnooze pauses notifications for n minutes (core.SetDND).
func actSnooze(h Host, n int) {
	a := h.Core()
	if a == nil {
		h.Toast("Not connected yet")
		return
	}
	run(h, func(ctx context.Context) (struct{}, error) { return struct{}{}, a.SetDND(ctx, n) }, func(_ struct{}, err error) {
		if err != nil {
			h.Toast("Couldn't do that: " + errText(err))
			return
		}
		actDNDUntil = time.Now().Add(time.Duration(n) * time.Minute)
		h.Toast("Notifications paused")
	})
}

// actNewPauseModal is Slack's "Pause Notifications" dialog (pause.png):
// 520 wide, "Pause notifications until…" + a full-width 36px date field
// ("November 6th, 2026", input look), "Time" + a full-width time select,
// footer with only Save → core.SetDND(minutes until then).
func actNewPauseModal(h Host) *Modal {
	now := time.Now()
	start := dtCeil(now, 60)
	day := &dtDate{Day: dtDay(start), Min: dtDay(now), Style: dtLong, Plain: true}
	tm := &dtTime{Mins: dtMins(start), NoIcon: true}
	tm.From = func() int {
		if n := time.Now(); dtSameDay(day.Day, n) {
			return dtMins(n)
		}
		return 0
	}
	var m *Modal
	body := func(gtx layout.Context) layout.Dimensions {
		th := h.Theme()
		cl := colorsOf(th)
		org := dtModalOrigin(gtx, m)
		w := gtx.Constraints.Max.X
		lbl := func(y int, s string) {
			at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, dp(gtx, 22), func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 15, theme.Bold, cl.Fg, s)
				})
			})
		}
		lbl(dp(gtx, 2), "Pause notifications until…")
		y := dp(gtx, 34)
		at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			return day.Layout(gtx, h, org.Add(image.Pt(0, y)), w, dtLarge)
		})
		lbl(dp(gtx, 90), "Time")
		y = dp(gtx, 120)
		at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			return tm.Layout(gtx, h, org.Add(image.Pt(0, y)), w, dtLarge)
		})
		return layout.Dimensions{Size: image.Pt(w, dp(gtx, 176))}
	}
	m = NewModal(h, ModalOpts{Title: "Pause Notifications", PadX: 24, HeadH: 66, Body: body, Buttons: []*ModalButton{
		{Label: "Save", Kind: BtnPrimary, Do: func() bool {
			until, n := dtAt(day.Day, tm.Mins), time.Now()
			if !until.After(n) {
				h.Toast("Choose a time in the future")
				return false
			}
			actSnooze(h, actMinutesUntil(n, until))
			return true
		}},
	}})
	return m
}

// actRichRow is a 28px menu row "plain **bold**" with Slack's highlight.
func actRichRow(h Host, plain, bold string) func(gtx layout.Context, hl bool) layout.Dimensions {
	return func(gtx layout.Context, hl bool) layout.Dimensions {
		th := h.Theme()
		cl := colorsOf(th)
		sz := image.Pt(gtx.Constraints.Max.X, dp(gtx, 28))
		fg := cl.Fg
		if hl {
			fill(gtx, image.Rectangle{Max: sz}, cl.Highlight)
			fg = cl.HighText
		}
		x := dp(gtx, 24)
		at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, sz.Y, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, theme.Regular, fg, plain)
			})
		})
		x += textW(gtx, th, 15, theme.Regular, plain)
		at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, sz.Y, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, theme.Bold, fg, bold)
			})
		})
		return layout.Dimensions{Size: sz}
	}
}

// ---- composer + menu ----

// NewComposerPlusMenu builds the composer's + menu (360 wide, above
// anchor). onAction receives files | gif | snippet | workflow | upload.
func NewComposerPlusMenu(h Host, anchor image.Point, onAction func(action string)) *Menu {
	act := func(name string) func() {
		return func() {
			if onAction != nil {
				onAction(name)
				return
			}
			h.Toast("That isn't supported yet")
		}
	}
	items := []MenuItem{
		{Label: "File, canvas, or list", Icon: "files", Do: act("files")},
		{Label: "GIF", Icon: "image", Do: act("gif")},
		{Label: "Text snippet", Icon: "code-block", Hint: "Ctrl+Shift+Enter", Do: act("snippet")},
		{Label: "Workflow", Icon: "workflow", Do: act("workflow")},
		{Label: "Upload from your computer", Icon: "upload", Do: act("upload")},
	}
	return NewMenuOpts(h, items, MenuOpts{Anchor: pointRect(anchor), Side: Above, Width: 360})
}
