package ui

import (
	"image"
	"image/color"
	"slices"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// JoinChannelPane's revamped card (boot-shared UY14 under
// channel_join_experience_with_agents, CDN build 274f7c7f1b69,
// 2026-11-11; CSS join-channel-pane.module.lazy.less, hHrC, and
// client-boot-styles' .p-message_pane_input__preview--revamped):
//
//   - the card: --dt_color-ctr-sec on a 1px otl-ter border, radius 10,
//     padding 16, left-aligned (.previewRevamp: a flex column);
//   - .previewTopRow (wrap, justify-content flex-end, align center, gap 8):
//     the facepile (margin-right auto) of up to three of the channel's
//     people and its agents seen active (36px avatars overlapping by 8,
//     radius 8, a 2px ctr-sec ring); the title (flex 1 1 200px): "Join
//     #name" (18px black) over the purpose (15px content-sec, 2 lines);
//     the actions: only the Join button (the split button for OtvL Y), no
//     Details. With faces the title takes its own row below
//     (data-placement below-row); with none, once the counts and the
//     agents are in, it sits beside the actions;
//   - .previewCaption (13px content-ter): "{people} are here." and
//     "{agents} are also here.", from membershipCounts and the fetched
//     agents and apps (agent_count.go), each naming two, or all when
//     those are all there are.
//
// Every string is the archives namespace's (eB = new I18n("archives")).

// joinCardInfo is UY14's data hook for a channel.
type joinCardInfo struct {
	people     []string // memberIds: the facepile's people
	agentFaces []string // agentAppIds: agents seen active (Slackbot last)
	agentNames []string // agentAppNameIds: the agents the caption names
	memberN    int      // memberCount (membershipCounts member_count); -1 unknown
	agentN     int      // agentAppCount
	settled    bool     // facesSettled
}

// joinCardData follows UY14's hook: only for a public channel (aK5v rW).
func (a *App) joinCardData(ch string, ci *convInfo) joinCardInfo {
	d := joinCardInfo{memberN: -1}
	svc := a.Core()
	if svc == nil || ci.Kind != "channel" {
		d.settled = true // !n
		return d
	}
	people, pok := svc.PeopleCount(ch)
	apps, aok := svc.AgentAppCount(ch)
	st := a.agentAppsOf(ch, apps, true)
	fetched := st.done && st.ids != nil // l !== null
	p := st.ids                         // ed.A(c, native expert, Slackbot): Clack keeps neither extra
	var active []string                 // _
	if fetched && st.history {
		active = st.active
	}
	g := len(p) // Nl ?? p.length
	if aok {
		g = apps
	}
	var faces []string // f: the first two active, Slackbot after them
	for _, id := range active {
		if id != core.SlackbotUserID && len(faces) < 2 {
			faces = append(faces, id)
		}
	}
	if slices.Contains(active, core.SlackbotUserID) {
		faces = append(faces, core.SlackbotUserID)
	}
	faces = faces[:min(len(faces), g)]
	if pok {
		d.memberN = people
		d.people = ci.Members[:min(len(ci.Members), 3, people)] // cYPn's 3 relevant members, capped at the count
	}
	if fetched && st.history {
		var names []string // ed.A(f, _, p): the union in that order
		for _, id := range slices.Concat(faces, active, p) {
			if !slices.Contains(names, id) {
				names = append(names, id)
			}
		}
		d.agentNames = names[:min(len(names), 3, g)]
	}
	d.agentFaces, d.agentN = faces, g
	d.settled = pok && fetched
	return d
}

// joinCaptions are eq ("… are here.") and eQ ("… are also here."), ""
// when not shown.
func (a *App) joinCaptions(d joinCardInfo) (members, agents string) {
	es := a.memberNames(d.people)
	if d.memberN >= 0 && len(es) > 0 {
		if len(es) == d.memberN {
			members = nsArchives.T("{memberCount, plural, =1 {{memberNames} is here.} other {{memberNames} are here.}}",
				"memberCount", d.memberN, "memberNames", i18n.Listify(es, false))
		} else {
			named := es[:min(2, len(es))]
			members = nsArchives.T("{namedMemberCount, plural, =1 {{namedMembers} and {otherMemberCount, plural, =1 {# more person} other {# more people}} are here.} other {{namedMembers}, and {otherMemberCount, plural, =1 {# more person} other {# more people}} are here.}}",
				"namedMemberCount", len(named), "namedMembers", i18n.ListifyNoConj(named), "otherMemberCount", d.memberN-len(named))
		}
	}
	el := a.agentAppNames(d.agentNames, len(d.agentNames))
	if d.agentN > 0 && len(el) > 0 {
		if len(el) == d.agentN {
			agents = nsArchives.T("{agentAppCount, plural, =1 {{agentAppNames} is also here.} other {{agentAppNames} are also here.}}",
				"agentAppCount", d.agentN, "agentAppNames", i18n.Listify(el, false))
		} else {
			named := el[:min(2, len(el))]
			agents = nsArchives.T("{namedAgentAppCount, plural, =1 {{namedAgentApps} and {otherAgentAppCount, plural, =1 {# other agent or app} other {# other agents or apps}} are also here.} other {{namedAgentApps}, and {otherAgentAppCount, plural, =1 {# other agent or app} other {# other agents or apps}} are also here.}}",
				"namedAgentAppCount", len(named), "namedAgentApps", i18n.ListifyNoConj(named), "otherAgentAppCount", d.agentN-len(named))
		}
	}
	return members, agents
}

// memberNames is ids' display names (Emmo I), skipping unknown users.
func (a *App) memberNames(ids []string) []string {
	svc := a.Core()
	var out []string
	for _, id := range ids {
		if svc == nil {
			break
		}
		if u, ok := svc.User(id); ok && u.Name != "" {
			out = append(out, u.Name)
		}
	}
	return out
}

// cardTok is a --dt_color-* token of Slack's stylesheet in th's mode.
func cardTok(th *theme.Theme, name string) color.NRGBA {
	c, _ := css.ParseColor(css.SlackVars(th.Dark)["--dt_color-"+name])
	return c
}

// layoutJoinCard draws the revamped card at the pane's width; org is its
// window origin.
func (a *App) layoutJoinCard(gtx layout.Context, c *convo, ci *convInfo, org image.Point, temp bool) layout.Dimensions {
	jp := c.jp
	th := a.th
	W := gtx.Constraints.Max.X
	border, pad := gtx.Dp(1), gtx.Dp(16)
	cw := max(W-2*(border+pad), 1)
	gap := gtx.Dp(8)
	d := a.joinCardData(c.ch, ci)
	faces := slices.Concat(d.people, d.agentFaces)
	beside := d.settled && len(faces) == 0

	// the actions: the Join (split) button
	am := op.Record(gtx.Ops)
	var ad layout.Dimensions
	caretX := 0
	if temp {
		ad, caretX = a.splitJoinButton(gtx, jp)
	} else {
		ad = popups.Button(gtx, th, &jp.join, popups.BtnPrimary, popups.BtnMedium, nsArchives.T("Join Channel"), "", 0)
	}
	actCall := am.Stop()

	// the title: "Join #name" (thread: "To reply to this thread, join
	// #name") over the purpose
	titleW := cw
	if beside && cw-ad.Size.X-gap >= gtx.Dp(200) {
		titleW = cw - ad.Size.X - gap
	}
	tm := op.Record(gtx.Ops)
	tg := gtx
	tg.Constraints = layout.Constraints{Max: image.Pt(titleW, gtx.Constraints.Max.Y)}
	src := "Join {channel}"
	if c.ts != "" {
		src = "To reply to this thread, join {channel}"
	}
	hd := a.joinHeadline(tg, nsArchives.RT(src, "channel", ci.Name), ci, 18, theme.Black, 26, "channel")
	titleH := hd.Size.Y
	if ci.Purpose != "" {
		t := op.Offset(image.Pt(0, titleH)).Push(gtx.Ops)
		dd := th.Layout(tg, theme.TextStyle{Size: 15, LineHeight: 22, Weight: theme.Regular, Color: cardTok(th, "content-sec"), MaxLines: 2}, ci.Purpose)
		t.Pop()
		titleH += dd.Size.Y
	}
	titleCall := tm.Stop()

	var rows []func(y int) int // each draws at y and returns its height
	var actAt image.Point
	switch {
	case beside && titleW < cw: // [title][actions], centred on each other
		h := max(titleH, ad.Size.Y)
		actAt = image.Pt(cw-ad.Size.X, (h-ad.Size.Y)/2)
		rows = append(rows, func(y int) int {
			place(gtx, titleCall, image.Pt(0, y+(h-titleH)/2))
			place(gtx, actCall, actAt.Add(image.Pt(0, y)))
			return h
		})
	case beside: // too narrow: the actions wrap under the title
		actAt = image.Pt(cw-ad.Size.X, titleH+gap)
		rows = append(rows, func(y int) int {
			place(gtx, titleCall, image.Pt(0, y))
			place(gtx, actCall, actAt.Add(image.Pt(0, y)))
			return titleH + gap + ad.Size.Y
		})
	default: // the facepile and the actions, then the title below the row
		fs := gtx.Dp(36)
		h := max(fs, ad.Size.Y)
		actAt = image.Pt(cw-ad.Size.X, (h-ad.Size.Y)/2)
		rows = append(rows, func(y int) int {
			a.joinFacepile(gtx, faces, image.Pt(0, y+(h-fs)/2))
			place(gtx, actCall, actAt.Add(image.Pt(0, y)))
			return h + gap
		}, func(y int) int {
			place(gtx, titleCall, image.Pt(0, y))
			return titleH
		})
	}
	members, agents := a.joinCaptions(d)
	if caption := joinWords(members, agents); caption != "" {
		rows = append(rows, func(y int) int {
			g := gtx
			g.Constraints = layout.Constraints{Max: image.Pt(cw, gtx.Constraints.Max.Y)}
			t := op.Offset(image.Pt(0, y)).Push(gtx.Ops)
			dd := th.Layout(g, theme.TextStyle{Size: 13, LineHeight: 19, Color: cardTok(th, "content-ter")}, caption)
			t.Pop()
			return dd.Size.Y
		})
	}
	if a.previewBack(c.ch) != "" {
		rows = append(rows, func(y int) int {
			g := gtx
			g.Constraints = layout.Constraints{Max: image.Pt(cw, gtx.Constraints.Max.Y)}
			t := op.Offset(image.Pt(0, y)).Push(gtx.Ops)
			dd := a.previewBackLink(g, jp, false)
			t.Pop()
			return dd.Size.Y
		})
	}

	// the rows go in after the card's background, which needs their height
	inner := op.Record(gtx.Ops)
	y := 0
	for _, r := range rows {
		y += r(y)
	}
	innerCall := inner.Stop()
	h := y + 2*(border+pad)
	box := image.Rectangle{Max: image.Pt(W, h)}
	rad := gtx.Dp(10)
	theme.RRect(gtx, box, rad, cardTok(th, "otl-ter"))
	theme.RRect(gtx, box.Inset(border), rad-border, cardTok(th, "ctr-sec"))
	o := image.Pt(border+pad, border+pad)
	place(gtx, innerCall, o)

	// window rects of the buttons (the menu's anchor, tests)
	jr := image.Rectangle{Min: actAt, Max: actAt.Add(ad.Size)}.Add(o).Add(org) // the actions are in the first row
	jp.detailsRect, jp.joinRect, jp.caretRect = image.Rectangle{}, jr, image.Rectangle{}
	if temp {
		jp.joinRect.Max.X = jr.Min.X + caretX
		jp.caretRect = image.Rectangle{Min: image.Pt(jr.Min.X+caretX, jr.Min.Y), Max: jr.Max}
	}
	a.caretTooltip(gtx, jp, org, temp)
	return layout.Dimensions{Size: box.Size()}
}

// joinFacepile is Z: the faces at p, each 36px with radius 8
// (clamp(4px, min(22.222%, 12px), 12px)) on a 2px ctr-sec ring, the next
// overlapping the one before by 8px.
func (a *App) joinFacepile(gtx layout.Context, ids []string, p image.Point) {
	s := gtx.Dp(36)
	r := gtx.Dp(8)
	ring := gtx.Dp(2)
	bg := cardTok(a.th, "ctr-sec")
	x := p.X
	for _, id := range ids {
		t := op.Offset(image.Pt(x, p.Y)).Push(gtx.Ops)
		theme.RRect(gtx, image.Rect(-ring, -ring, s+ring, s+ring), r+ring, bg)
		a.avatar(gtx, a.userAvatar(id), "", s, r)
		t.Pop()
		x += s - gtx.Dp(8)
	}
}

// joinWords joins the non-empty captions with a space ([eq, " ", eQ]).
func joinWords(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + " " + b
}
