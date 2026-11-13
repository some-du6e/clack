package popups

import (
	"image"
	"math"
	"strconv"

	"gioui.org/layout"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// Slack's SharedChannelTooltipContent (boot-shared Un00, web client build
// of 2026-11-12): the tooltip BaseChannelName puts over an externally
// shared channel's name in the sidebar (hasSharedChannelTooltip, the row's
// channelSharedTooltipState sJGu "externally-shared": is_ext_shared with
// at least one external team the client knows, 6a40 y). Up to three of the
// external teams' icons (qVVL TeamIcon, 20px) in
// .c-channel_entity__shared_icon_external_team_list (flex, centred, margin
// 4px 0; each icon margin-right 4px), a fourth tile "+N" (at most "9+";
// .c-channel_entity__shared_icon_additional_team_icon: 20px, radius 4,
// foreground_max_solid behind inverted text, 11.1111px) when there are
// more, then the sentence. A truncated name puts the channel's name in
// the sentence ("{externalTeamName} is in {channelName}").

// SharedTeam is one external organization the tooltip shows.
type SharedTeam struct {
	Name string
	Icon string // the team icon's image URL ("" for the default icon)
}

// SharedChannelTip shows the tooltip for a widget of size sz at the
// current origin while hovered (after st's delay). teams are the first
// three external teams, count all of them; channel is the channel's name
// as the sidebar shows it, used when truncated.
func SharedChannelTip(gtx layout.Context, h Host, th *theme.Theme, st *TipState, hovered bool, sz image.Point, teams []SharedTeam, count int, channel string, truncated bool) {
	if len(teams) == 0 {
		hovered = false // nothing to say yet (the teams load): the tip's hover state still follows
	}
	TooltipBox(gtx, th, st, hovered, sz, Above, func(gtx layout.Context) layout.Dimensions {
		return sharedTipBox(gtx, h, th, teams, count, sharedChannelTipText(teams, count, channel, truncated), st.MaxWidth)
	})
}

// sharedChannelTipText is the sentence for teams (the first three of
// count external teams).
func sharedChannelTipText(teams []SharedTeam, count int, channel string, truncated bool) string {
	n := func(i int) string { return pfTeamName(teams[i].Name) }
	switch {
	case len(teams) == 1:
		if truncated {
			return nsChannels.T("{externalTeamName} is in {channelName}", "externalTeamName", n(0), "channelName", channel)
		}
		return nsChannels.T("{externalTeamName} is in this channel", "externalTeamName", n(0))
	case len(teams) == 2:
		if truncated {
			return nsChannels.T("{externalTeamName1} and {externalTeamName2} are in {channelName}",
				"externalTeamName1", n(0), "externalTeamName2", n(1), "channelName", channel)
		}
		return nsChannels.T("{externalTeamName1} and {externalTeamName2} are in this channel",
			"externalTeamName1", n(0), "externalTeamName2", n(1))
	case count <= 3:
		if truncated {
			return nsChannels.T("{externalTeamName1}, {externalTeamName2}, and {externalTeamName3} are in {channelName}",
				"externalTeamName1", n(0), "externalTeamName2", n(1), "externalTeamName3", n(2), "channelName", channel)
		}
		return nsChannels.T("{externalTeamName1}, {externalTeamName2}, and {externalTeamName3} are in this channel",
			"externalTeamName1", n(0), "externalTeamName2", n(1), "externalTeamName3", n(2))
	}
	if truncated {
		return nsChannels.T("{externalTeamName1}, {externalTeamName2}, {externalTeamName3}, and {numAdditionalExternalTeams, plural, =1 {# other external organization} other {# other external organizations}} are in {channelName}",
			"externalTeamName1", n(0), "externalTeamName2", n(1), "externalTeamName3", n(2), "numAdditionalExternalTeams", count-3, "channelName", channel)
	}
	return nsChannels.T("{externalTeamName1}, {externalTeamName2}, {externalTeamName3}, and {numAdditionalExternalTeams, plural, =1 {# other external organization} other {# other external organizations}} are in this channel",
		"externalTeamName1", n(0), "externalTeamName2", n(1), "externalTeamName3", n(2), "numAdditionalExternalTeams", count-3)
}

// sharedTipBox lays the tooltip's content out at the origin (the tip's
// 8px 12px padding included) and returns its size.
func sharedTipBox(gtx layout.Context, h Host, th *theme.Theme, teams []SharedTeam, count int, msg string, maxWidth unit.Dp) layout.Dimensions {
	cl := colorsOf(th)
	padX, padY := dp(gtx, 12), dp(gtx, 8)
	maxW := dp(gtx, 320)
	if maxWidth > 0 {
		maxW = gtx.Dp(maxWidth) - 2*padX
	}
	icon, gap := dp(gtx, 20), dp(gtx, 4)
	n := len(teams)
	more := count > 3 && n == 3
	if more {
		n++
	}
	rowW := n * (icon + gap) // each child's margin-right 4px, the last's too
	tc, td := record(loose(gtx, image.Pt(maxW, 1<<20)), func(gtx layout.Context) layout.Dimensions {
		return para(gtx, th, 13, 18, theme.Bold, cl.TooltipText, msg, 0)
	})
	w := max(rowW, td.Size.X)
	rowTop := padY + dp(gtx, 4)
	textTop := rowTop + icon + dp(gtx, 4)
	sz := image.Pt(w+2*padX, textTop+td.Size.Y+padY)
	x := padX + (w-rowW)/2
	for _, t := range teams {
		sharedTeamIcon(gtx, h, th, t, image.Pt(x, rowTop), 20)
		x += icon + gap
	}
	if more {
		r := image.Rectangle{Min: image.Pt(x, rowTop), Max: image.Pt(x+icon, rowTop+icon)}
		rrect(gtx, r, dp(gtx, 4), rgb(0x616061)) // sk_foreground_max_solid
		label := "9+"
		if count-3 < 9 {
			label = "+" + strconv.Itoa(count-3)
		}
		at(gtx, r.Min, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(r.Size())
			return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 11.1111, theme.Regular, rgb(0xffffff), label)
			})
		})
	}
	macroAt(gtx, tc, image.Pt((sz.X-td.Size.X)/2, textTop))
	return layout.Dimensions{Size: sz}
}

// sharedTeamIcon is BaseTeamIcon (KITx) at size dp: the icon's image with
// .c-team_icon's radius (clamp(4px, min(22.222%, 12px), 12px)) and its
// inset 1px #00000014 ring, or the default icon: the team's initials (bold,
// size/1.8) on foreground_max_solid.
func sharedTeamIcon(gtx layout.Context, h Host, th *theme.Theme, t SharedTeam, pt image.Point, size float32) {
	px := dp(gtx, size)
	r := image.Rectangle{Min: pt, Max: pt.Add(image.Pt(px, px))}
	rad := min(max(dp(gtx, 4), px*22222/100000), dp(gtx, 12))
	if t.Icon != "" && h.Images() != nil {
		if img, ok := h.Images().Get(t.Icon, px*2); ok {
			at(gtx, pt, func(gtx layout.Context) layout.Dimensions { return theme.Image(gtx, img, r.Size(), rad) })
			ringRRect(gtx, r, rad)
			return
		}
	}
	rrect(gtx, r, rad, rgb(0x616061))
	if in := pfInitials(t.Name); in != "" {
		at(gtx, pt, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(r.Size())
			return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, float32(math.Round(float64(size)/1.8)), theme.Bold, rgb(0xffffff), in)
			})
		})
	}
	ringRRect(gtx, r, rad)
}

// ringRRect is .c-team_icon:after's inset 1px rgba(0,0,0,.08) ring.
func ringRRect(gtx layout.Context, r image.Rectangle, rad int) {
	stroke(gtx, r, rad, border1(gtx), rgba(0x000000, 0x14/255.0))
}
