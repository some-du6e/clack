package popups

import (
	"context"
	"image"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
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
	star := "Star channel"
	if dm {
		star = "Star conversation"
	}
	mute := "Mute channel"
	if dm {
		mute = "Mute conversation"
	}
	if c.Muted {
		mute = "Un" + strings.ToLower(mute[:1]) + mute[1:]
	}
	items := []MenuItem{
		{Label: detailsLabel, Sub: detailSub},
		{Label: "Copy", Sub: []MenuItem{
			{Label: "Copy link", DoCtx: actCopy(h, link, "Link copied")},
			{Label: "Copy name", DoCtx: actCopy(h, name, "Name copied")},
		}},
		{Label: star, Do: actToast(h, "Starring isn't supported yet")},
	}
	if !dm {
		items = append(items, MenuItem{Label: "Move channel", Do: actToast(h, "Moving channels between sections isn't supported yet")})
	}
	items = append(items,
		Sep(),
		MenuItem{Label: "Add a workflow", Do: actToast(h, "Workflows aren't supported in yoloslack")},
		MenuItem{Label: "Edit settings", Do: actToast(h, "Channel settings aren't supported yet")},
		Sep(),
		MenuItem{Label: "Open in split view", Icon: "panel-right", Do: actToast(h, "Split view isn't supported yet")},
		Sep(),
		MenuItem{Label: mute, Do: actToast(h, "Muting isn't supported yet")},
	)
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
	return NewMenuOpts(h, items, MenuOpts{Anchor: pointRect(anchor), AlignRight: true})
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
		{Label: "Tools", Sub: []MenuItem{
			{Label: "Workflows", Do: actToast(h, "Workflows aren't supported in yoloslack")},
			{Label: "Analytics", Do: actToast(h, "Analytics aren't supported in yoloslack")},
		}},
		Sep(),
		{Label: "Sign in on mobile", Do: actToast(h, "Use the Slack mobile app to sign in on your phone")},
		{Label: "Join or leave workspaces", External: true, Do: actToast(h, "Manage workspaces at app.slack.com")},
		{Label: "Sign out", Do: actSignOut(h)},
		{Label: "Open the Slack App", Do: actToast(h, "You're already in a Slack app")},
	}
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
		return func() {
			a := h.Core()
			if a == nil {
				h.Toast("Not connected yet")
				return
			}
			n := minutes(time.Now())
			run(h, func(ctx context.Context) (struct{}, error) { return struct{}{}, a.SetDND(ctx, n) }, func(_ struct{}, err error) {
				if err != nil {
					h.Toast("Couldn't do that: " + errText(err))
					return
				}
				actDNDUntil = time.Now().Add(time.Duration(n) * time.Minute)
				h.Toast("Notifications paused")
			})
		}
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
		MenuItem{Label: "Custom…", Do: actToast(h, "Custom pause times aren't supported yet")},
		Sep(),
		MenuItem{Label: "Edit your working hours", Do: actToast(h, "Working hours aren't supported yet")},
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
			h.ShowOverlay(NewSetStatusModal(h))
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
		{Label: "Sign out of " + teamName, Do: actSignOut(h)},
	}
	menu = NewMenuOpts(h, items, MenuOpts{Anchor: pointRect(anchor), Side: Above})
	return menu
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
