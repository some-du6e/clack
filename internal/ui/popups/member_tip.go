package popups

import (
	"image"
	"image/color"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/theme"
)

var nsMembers = i18n.NS("members")

// The composer's tooltip over an app's or a bot's slug (boot-shared
// TextyTooltip, build 3889f0c93d30): for a member who isn't human
// TextyTooltip renders renderComposerAppHoverCard({memberId, renderCard,
// fallback}), registered as ComposerAppHoverCard (+VWx) →
// AppMentionHoverCard (kLO3). That shows the agent card only when
// useAppMentionHoverCardApp (qBeu) finds an app, which it does only under
// revamp_agent_app_hover_card (HLQ9 Ug, read with ze), and
// useAppHoverCardRoute (S70I) then picks AgentCard; everything else gets
// the fallback: a Popover at "top" over the slug (allowance 0, no delay)
// holding TooltipTip with c-tooltip__tip--member around SmallMemberEntity
// ({id, showAvatar, showPresence, invert}).
//
// SmallMemberEntity (client-boot 29M7) in that tip, from its CSS:
// .c-tooltip__tip--member {padding: 6px 12px 6px 6px; max-width: 500px};
// .c-base_entity--small {display: flex; align-items: center;
// line-height: 20px}, the avatar (x_small, 20px) 8px before the text;
// MemberName (XWLGa) bold (strong) with its badge 4px after it
// (c-member_name__indicator: BaseAppBadge 2C9J "APP", "WORKFLOW",
// "AGENT" or "AGENTFORCE", c-app_badge--inverse: 14 tall, 10px bold,
// padding 1px 3px, radius 2, color #fff on --dt_color-surf-pry); the
// presence (PresenceIcon, inverted: #fff) and the secondary name
// (margin 0 4px) after it. getMemberEntityNames (iFvT): the primary name
// is getPrefCompliantMemberName; the secondary is the real name when a
// display name is set and display_real_names is off, else the display
// name (none when unset).

// MemberTipApplies reports whether a composer slug for u gets the member
// tooltip: a member who isn't human (getMemberIsHuman: an app's or a
// bot's user). AppMentionHoverCard's agent card needs
// revamp_agent_app_hover_card, which the test account isn't in
// (experiments.getByUser, 2026-11-13), and Clack has no agent card, so
// every app gets the tooltip.
func MemberTipApplies(u core.User) bool { return u.IsBot || u.BotID != "" }

// memberBadge is getBadgeText.
func memberBadge(u core.User) string {
	switch {
	case u.Agentforce:
		return nsMembers.T("AGENTFORCE")
	case u.Agent:
		return nsMembers.T("AGENT")
	case u.Workflow:
		return nsMembers.T("WORKFLOW")
	case u.IsBot || u.BotID != "":
		return nsMembers.T("APP")
	}
	return ""
}

// MemberTip draws the tip's content for userID at the origin, padding
// included, and returns its size (TooltipBox draws the tip under it).
func MemberTip(gtx layout.Context, h Host, userID string) layout.Dimensions {
	th := h.Theme()
	cl := colorsOf(th)
	u, ok := lookupUser(h, userID)
	padT, padR, padB, padL := dp(gtx, 6), dp(gtx, 12), dp(gtx, 6), dp(gtx, 6)
	lineH := dp(gtx, 20)
	maxW := dp(gtx, 500) - padL - padR
	x := padL
	fg := cl.TooltipText
	if !ok {
		// MemberName's MissingText: the unknown member's bar
		srcBar(gtx, th, x, padT+dp(gtx, 6), srcSkelWidths[1], 1)
		return layout.Dimensions{Size: image.Pt(padL+dp(gtx, 120)+padR, padT+lineH+padB)}
	}
	at(gtx, image.Pt(x, padT), func(gtx layout.Context) layout.Dimensions {
		return avatar(gtx, h, pfAvatarURL(u, false), 20)
	})
	x += dp(gtx, 20) + dp(gtx, 8)
	name := u.Name
	if c := h.Core(); c != nil {
		if n, ok := c.S.MemberName(userID); ok && n != "" {
			name = n
		}
	}
	nameMax := max(1, maxW-(x-padL))
	d := at(gtx, image.Pt(x, padT), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(nameMax, lineH)}
		return vcenter(gtx, lineH, func(gtx layout.Context) layout.Dimensions {
			return th.Layout(gtx, theme.TextStyle{Size: 13, Weight: theme.Bold, Color: fg, LineHeight: 20, MaxLines: 1}, name)
		})
	})
	x += d.Size.X
	if b := memberBadge(u); b != "" {
		x += dp(gtx, 4)
		bw := textW(gtx, th, 10, theme.Bold, b) + dp(gtx, 6)
		bh := dp(gtx, 14)
		by := padT + (lineH-bh)/2
		rrect(gtx, image.Rect(x, by, x+bw, by+bh), dp(gtx, 2), memberBadgeBg(th))
		at(gtx, image.Pt(x+dp(gtx, 3), by), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, bh, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 10, theme.Bold, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, b)
			})
		})
		x += bw
	}
	if pfShowsPresence(u) {
		// PresenceIcon (invert: c-presence--inverted, #fff), the
		// primary content's next item
		x += dp(gtx, 4)
		g := dp(gtx, 9)
		at(gtx, image.Pt(x, padT+(lineH-g)/2), func(gtx layout.Context) layout.Dimensions {
			white := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
			if pfIsSlackbot(u) {
				pfHeart(gtx, dp(gtx, 10), white)
				return layout.Dimensions{}
			}
			r := image.Rect(0, 0, g, g)
			if u.Presence == "active" {
				rrect(gtx, r, g/2, white)
			} else {
				stroke(gtx, r, g/2, maxInt(1, g/6), white)
			}
			return layout.Dimensions{}
		})
		x += g + dp(gtx, 4) // .c-member__presence margin-right 4
	}
	sec := ""
	realNames := false
	if c := h.Core(); c != nil {
		realNames = c.S.Prefs.DisplayRealNames
	}
	switch {
	case !realNames && u.DisplaySet:
		sec = u.RealName
	case u.DisplaySet:
		sec = u.DisplayName
	}
	if sec != "" && sec != name {
		if !pfShowsPresence(u) {
			x += dp(gtx, 4) // .c-member__secondary-name margin 0 4px (no presence before it)
		}
		d := at(gtx, image.Pt(x, padT), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(max(1, maxW-(x-padL)), lineH)}
			return vcenter(gtx, lineH, func(gtx layout.Context) layout.Dimensions {
				return th.Layout(gtx, theme.TextStyle{Size: 13, Weight: theme.Regular, Color: fg, LineHeight: 20, MaxLines: 1}, sec)
			})
		})
		x += d.Size.X + dp(gtx, 4)
	}
	return layout.Dimensions{Size: image.Pt(x+padR, padT+lineH+padB)}
}

// memberBadgeBg is .c-app_badge's background, --dt_color-surf-pry (the
// theme's, else Slack's #1d1c1d0f / #f8f8f80f), over the tip as the page
// composites it.
func memberBadgeBg(th *theme.Theme) color.NRGBA {
	c := chipTok{"var(--dt_color-surf-pry)", rgba(0x1d1c1d, 0x0f/255.0), rgba(0xf8f8f8, 0x0f/255.0)}.of(th)
	return theme.Over(c, colorsOf(th).Tooltip)
}
