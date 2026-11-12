package ui

import (
	"context"
	"image"
	"log"
	"slices"
	"time"

	"gioui.org/layout"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Agents and apps in a channel, as Slack's client shows them (CDN build
// 274f7c7f1b69, 2026-11-11; core/channel_agents.go has the experiments):
//
//   - the preview's revamped JoinChannelPane (joinpane.go) and
//   - the channel header's AgentCountButton (view-header chunk Ms5K),
//     under channel_header_with_agents: g2K5 puts "agent_count" after the
//     avatar stack (before the huddle button) for a channel that isn't
//     archived, whose viewer isn't restricted out of a private one, and
//     whose agents-and-apps count (RW6b Li) isn't 0. The button is an
//     icon-only avatar stack (p-avatar_stack--details--icon_only--polish:
//     28px tall, 4px padding, the 20px ai-agents glyph, the 13px count 4px
//     after it, content-sec turning content-pry on hover); "–" stands for
//     the count until membershipCounts load. Hovering fetches the agents
//     and apps (edge users/list filter "apps", count 20) for the tooltip:
//     "View all agents & apps of this channel", and "Includes {agents}"
//     (the first three, ordered by recent posting: +z2S
//     getMembersOrderedByRecentPostingInChannel) as its subtitle. A click
//     opens the channel's details on the Agents & apps tab (defaultTab
//     "agents").
//
// Not ported: the invite CTA that replaces it in workspaces under four
// people (YDDa qj, gIKW on) and channel_resources_remove_tabs's resources
// button, which takes its place (Clack has no channel resources).

// nsChannelHeader is AgentCountButton's I18n namespace.
var nsChannelHeader = i18n.NS("channel_header")

// agentApps is one channel's agents and apps as UY14 and Ms5K fetch them.
type agentApps struct {
	ids      []string // fetchChannelPreviewAgentsApps: the API's order
	active   []string // of ids, seen in the last 28 messages, newest first (+z2S)
	history  bool     // the channel has history loaded (B0mF channelHasAnyHistory)
	count    int      // the app count the fetch was made for (Slack refetches on a change)
	loading  bool
	done     bool
	activeAt time.Time
	tip      popups.TipState // the header button's tooltip
}

// agentAppsOf is ch's agents and apps, fetched once per app count (the
// fetchers' args include appCount and agentCount) with fetch true; its
// seen-active list is recomputed at most once a second.
func (a *App) agentAppsOf(ch string, count int, fetch bool) *agentApps {
	if a.chAgents == nil {
		a.chAgents = map[string]*agentApps{}
	}
	st := a.chAgents[ch]
	if st == nil {
		st = &agentApps{}
		a.chAgents[ch] = st
	}
	svc := a.Core()
	if svc == nil {
		return st
	}
	if fetch && !st.loading && (!st.done || st.count != count) {
		st.loading, st.count = true, count
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			ids, err := svc.ChannelAgentApps(ctx, ch)
			a.Post(func() {
				st.loading, st.done = false, true
				if err != nil {
					log.Printf("ui: agents and apps %s: %v", ch, err)
					return
				}
				st.ids, st.activeAt = ids, time.Time{}
				a.Invalidate()
			})
		}()
	}
	if st.done && time.Since(st.activeAt) > time.Second {
		st.active, st.history = svc.MembersSeenActive(ch, st.ids)
		st.activeAt = time.Now()
	}
	return st
}

// showAgentCount is g2K5's agent_count condition for ch.
func (a *App) showAgentCount(ch string, conv *convInfo) bool {
	svc := a.Core()
	if svc == nil || conv == nil || !svc.ChannelHeaderWithAgents() || conv.Archived {
		return false
	}
	if conv.Kind != "channel" && conv.Kind != "private" {
		return false
	}
	// !isCurrentUserRestricted && (!private || member)
	if svc.ViewerRestricted() || (conv.Kind == "private" && conv.Preview) {
		return false
	}
	n, ok := svc.AgentAppCount(ch)
	return !ok || n != 0 // undefined !== 0: shown with "–"
}

// agentCountButton draws AgentCountButton at the origin, btn px tall.
func (a *App) agentCountButton(gtx layout.Context, ch string, btn int) layout.Dimensions {
	th := a.th
	c := &th.C
	click := a.clicks.get("hdr.agents")
	if clicked(click) {
		a.OpenDetailsTab(ch, "Agents & apps")
	}
	n, ok := a.Core().AgentAppCount(ch)
	hovered := click.Hovered()
	st := a.agentAppsOf(ch, n, hovered) // onMouseEnter / onFocus: fetch
	label := "–"
	if ok {
		label = i18n.FormatNumber(i18n.Locale(), float64(n))
	}
	fs := unit.Sp(13)
	cw := th.Measure(gtx, fs, theme.Bold, label)
	pad, is := gtx.Dp(4), gtx.Dp(20)
	w := pad + is + gtx.Dp(4) + cw + pad
	d := clickArea(gtx, click, func(gtx layout.Context, s image.Point, hov bool) {
		if hov { // --polish: no outline, only the hover fill
			theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(8), c.BgHover)
		}
	}, func(gtx layout.Context) layout.Dimensions {
		col := c.FgMuted
		if hovered {
			col = c.Fg
		}
		at(gtx, image.Pt(pad, (btn-is)/2), image.Pt(is, is), func(gtx layout.Context) layout.Dimensions {
			return icons.Get("agents")(gtx, 20, col) // Slack's ai-agents
		})
		at(gtx, image.Pt(pad+is+gtx.Dp(4), 0), image.Pt(cw+2, btn), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, btn, func(gtx layout.Context) layout.Dimensions {
				return th.Label(gtx, fs, theme.Bold, col, label)
			})
		})
		return layout.Dimensions{Size: image.Pt(w, btn)}
	})
	popups.TooltipLines(gtx, th, &st.tip, hovered, d.Size, popups.Below, a.agentCountTip(ch, st))
	return d
}

// agentCountTip is the button's tooltip: the title, and "Includes" the
// first three agents and apps (by recent posting) once they're fetched.
// Ms5K translates them only under agents_in_channels_improvements (M/qt
// Io); otherwise it draws the English literals.
func (a *App) agentCountTip(ch string, st *agentApps) []popups.TipLine {
	tr := a.Core() != nil && a.Core().AgentsInChannelsImprovements()
	names := a.agentAppNames(agentsByRecentPosting(st), 3)
	const title = "View all agents & apps of this channel"
	if len(names) == 0 {
		if tr {
			return []popups.TipLine{{Text: nsChannelHeader.T(title)}}
		}
		return []popups.TipLine{{Text: title}}
	}
	if !tr {
		return []popups.TipLine{{Text: title}, {Text: "Includes " + i18n.Listify(names, false), Subtitle: true}}
	}
	var lines []popups.TipLine
	for _, p := range nsChannelHeader.RT(title+"<t1>Includes {agents}</t1>", "agents", i18n.Listify(names, false)) {
		if p.Tag == "t1" && len(lines) > 0 && lines[len(lines)-1].Subtitle {
			lines[len(lines)-1].Text += p.Text
			continue
		}
		if p.Tag != "t1" && len(lines) > 0 && !lines[len(lines)-1].Subtitle {
			lines[len(lines)-1].Text += p.Text
			continue
		}
		lines = append(lines, popups.TipLine{Text: p.Text, Subtitle: p.Tag == "t1"})
	}
	return lines
}

// agentsByRecentPosting is +z2S getMembersOrderedByRecentPostingInChannel
// over the fetched ids: those seen active first (newest first), then the
// rest in the API's order. (Slack's only counts their own posts there,
// not thread replies or mentions; Clack's seen-active list is the closest
// it keeps.)
func agentsByRecentPosting(st *agentApps) []string {
	if st == nil || len(st.ids) < 2 || len(st.active) == 0 {
		if st == nil {
			return nil
		}
		return st.ids
	}
	out := append([]string{}, st.active...)
	for _, id := range st.ids {
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// agentAppNames is ids' display names (Emmo I), the first n; Slackbot is
// archives' "Slackbot".
func (a *App) agentAppNames(ids []string, n int) []string {
	svc := a.Core()
	var out []string
	for _, id := range ids {
		if len(out) == n {
			break
		}
		if id == core.SlackbotUserID {
			out = append(out, nsArchives.T("Slackbot"))
			continue
		}
		if svc != nil {
			if u, ok := svc.User(id); ok && u.Name != "" {
				out = append(out, u.Name)
			}
		}
	}
	return out
}
