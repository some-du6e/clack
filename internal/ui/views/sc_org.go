package views

// The external organization page an External row opens: Slack's
// ExternalOrganizationView (chunk external-organization-view zJKG, build
// a09b625d90aa), a secondary view (toUnifiedDirectoryExternalOrganization)
// titled "About {teamName}", its body Organization:
//
//   - the page header (p-organization__page_header: white, a bottom
//     border, 20px under it): TeamImageBanner (the team's image blurred
//     20px across a 150px band that the header overlaps by 90px), the
//     160px team icon (4px white border, radius 20, elevation_3), then
//     16px down the name (28/34 black) with Verified (the 20px
//     verified-filled badge in #4a154b, its tooltip "This organization’s
//     identity has been verified by Slack.");
//   - 20px in, the search ("Search for a channel or person", medium);
//   - while use_organization_entities loads, the conversation skeletons;
//     then "Your channels", "Your conversations" (ConversationList: a
//     13px title, the bordered list of up to 60 rows), "People you might
//     know at {teamName}" (MemberGrid: cards 192 to 311px wide, 16px
//     apart, up to 24), "Other channels", and with nothing to show
//     HubSearchEmptyState ("No results").
//
// Rows open the conversation (ConversationList's onClick, ef.e
// {channelId}); a member card opens the profile.

import (
	"context"
	"image"
	"strconv"
	"strings"
	"sync"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// scOrgListLimit is ConversationList's limit (ev).
const scOrgListLimit = 60

type scOrgInfo struct {
	team  core.ExternalTeam
	convs []core.Conv
}

type scOrgSearch struct {
	convs   []core.Conv
	members []core.User
}

// scOrgCount is a channel row's users/counts.
type scOrgCount struct {
	people int
	byTeam map[string]int
}

// ExternalOrg is the external organization page.
type ExternalOrg struct {
	base
	d    *Directories // the directory kit: member cards, lists, states
	team string

	info  async[scOrgInfo]
	srch  async[scOrgSearch]
	q     string
	in    *search
	deb   debounce
	clear widget.Clickable

	list     widget.List
	rows     map[string]*widget.Clickable
	closeBtn widget.Clickable
	back     widget.Clickable
	closeTip popups.TipState
	verified widget.Clickable
	verTip   popups.TipState
	twk      wake // the tooltips' hover-delay frame

	mu     sync.Mutex
	counts map[string]scOrgCount
	asked  map[string]bool
}

// NewExternalOrg builds the page for team id teamID.
func NewExternalOrg(h Host, teamID string) *ExternalOrg {
	o := &ExternalOrg{base: newBase(h), d: NewDirectories(h), team: teamID, rows: map[string]*widget.Clickable{},
		counts: map[string]scOrgCount{}, asked: map[string]bool{}}
	o.list.Axis = layout.Vertical
	o.closeTip.Wake, o.verTip.Wake = tipWake(h, &o.twk), tipWake(h, &o.twk)
	o.in = newSearchIn(nsSCNext, "Search for a channel or person")
	return o
}

// TeamID is the page's organization.
func (o *ExternalOrg) TeamID() string { return o.team }

// Title is ExternalOrganizationView's titleContent: "About {teamName}".
func (o *ExternalOrg) Title() string {
	return i18n.Plain(nsSC.RT("About {teamName}", "teamName", o.teamName()))
}

func (o *ExternalOrg) teamName() string {
	if st, ok, _ := o.info.get(); ok && st.team.Name != "" {
		return st.team.Name
	}
	if c := o.h.Core(); c != nil {
		if t, ok := c.ExternalTeamByID(o.team); ok {
			return t.Name
		}
	}
	return ""
}

func (o *ExternalOrg) teamOf() core.ExternalTeam {
	if st, ok, _ := o.info.get(); ok && st.team.ID != "" {
		return st.team
	}
	if c := o.h.Core(); c != nil {
		if t, ok := c.ExternalTeamByID(o.team); ok {
			return t
		}
	}
	return core.ExternalTeam{ID: o.team}
}

func (o *ExternalOrg) start() {
	c := o.h.Core()
	if c == nil || o.info.started() {
		return
	}
	team := o.team
	o.info.load(o.h, true, func(ctx context.Context) (scOrgInfo, error) {
		t, cs, err := c.ExternalOrgInfo(ctx, team)
		return scOrgInfo{team: t, convs: cs}, err
	})
}

// setQuery is the search input's value (use_organization_entities query).
func (o *ExternalOrg) setQuery(q string) {
	q = strings.TrimSpace(q)
	if q == o.q {
		return
	}
	o.q = q
	o.list.Position = layout.Position{}
	c := o.h.Core()
	if q == "" || c == nil {
		return
	}
	team := o.team
	o.srch.load(o.h, false, func(ctx context.Context) (scOrgSearch, error) {
		cs, us, err := c.SearchExternalOrg(ctx, team, q, core.ExternalOrgLimit)
		return scOrgSearch{convs: cs, members: us}, err
	})
}

// entities is use_organization_entities: the conversations split into
// the viewer's channels, IMs and MPIMs, and the others; the members; and
// whether anything still loads.
func (o *ExternalOrg) entities() (member, ims, other []core.Conv, members []core.User, loading bool) {
	info, infoOK, _ := o.info.get()
	loading = !infoOK || o.info.busy()
	var convs []core.Conv
	if o.q != "" {
		st, ok, _ := o.srch.get()
		loading = loading || !ok || o.srch.busy()
		convs, members = st.convs, st.members
	} else {
		convs = info.convs
	}
	seen := map[string]bool{}
	for _, c := range convs {
		if seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		switch {
		case c.Kind == "im" || c.Kind == "mpim":
			ims = append(ims, c)
		case c.Member:
			member = append(member, c)
		default:
			other = append(other, c)
		}
	}
	return member, ims, other, members, loading
}

// count is the row's users/counts, asked for once.
func (o *ExternalOrg) count(ch string) (scOrgCount, bool) {
	o.mu.Lock()
	n, ok := o.counts[ch]
	ask := !ok && !o.asked[ch]
	if ask {
		o.asked[ch] = true
	}
	o.mu.Unlock()
	if ask {
		if c := o.h.Core(); c != nil {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				people, by, err := c.OrgChannelCounts(ctx, ch)
				if err != nil {
					return
				}
				o.mu.Lock()
				o.counts[ch] = scOrgCount{people: people, byTeam: by}
				o.mu.Unlock()
				o.h.Invalidate()
			}()
		}
	}
	return n, ok
}

// Page reads (tests).

// OrgLoaded reports the page's reads (and the search's, with a query) in.
func (o *ExternalOrg) OrgLoaded() bool {
	_, _, _, _, loading := o.entities()
	return !loading
}

// OrgRows is the page's rows as their keys, in order.
func (o *ExternalOrg) OrgRows() []string {
	var out []string
	for _, r := range o.rowList() {
		out = append(out, r.key)
	}
	return out
}

// OrgSearch types q into the page's search.
func (o *ExternalOrg) OrgSearch(q string) { o.in.ed.SetText(q); o.setQuery(q) }

// OrgCaption is a channel row's membership caption as drawn.
func (o *ExternalOrg) OrgCaption(ch string, other bool) string {
	return i18n.Plain(o.membership(ch, other))
}

// ---- rows ----

func (o *ExternalOrg) rowList() []scRow {
	member, ims, other, members, loading := o.entities()
	var rows []scRow
	add := func(k string, f func(gtx C) D) { rows = append(rows, scRow{key: k, draw: f}) }
	add("organization-page-header", o.pageHeader)
	add("filter-input", o.searchRow)
	if loading {
		for _, k := range []string{"connected", "convos"} {
			add("connect-hub-skeleton-"+k, func(gtx C) D { return o.skeleton(gtx) })
		}
	}
	list := func(key, title string, cs []core.Conv, others bool) {
		if len(cs) == 0 {
			return
		}
		cs = cs[:min(len(cs), scOrgListLimit)]
		add(key+"-title", func(gtx C) D { return o.listTitle(gtx, title, 8) })
		for i, c := range cs {
			add(key+"-"+c.ID, func(gtx C) D { return o.convRow(gtx, c, others, i == 0, i == len(cs)-1) })
		}
	}
	if !loading {
		list("conversation-list-member", nsSCNext.T("Your channels"), member, false)
		list("conversation-list-imsOrMpims", nsSCNext.T("Your conversations"), ims, false)
	}
	if len(members) > 0 {
		title := i18n.Plain(nsSCNext.RT("People you might know at {teamName}", "teamName", o.teamName()))
		add("members-list-title", func(gtx C) D { return o.listTitle(gtx, title, 16) })
		add("member-grid", func(gtx C) D { return o.memberGrid(gtx, members) })
	}
	if loading {
		add("connect-hub-skeleton-available", func(gtx C) D { return o.skeleton(gtx) })
	}
	if !loading {
		list("conversation-list-nonmember", nsSCNext.T("Other channels"), other, true)
	}
	if len(member)+len(ims)+len(other)+len(members) == 0 {
		add("search-no-results", func(gtx C) D {
			return layout.Inset{Top: 96, Bottom: 96}.Layout(gtx, func(gtx C) D {
				return o.d.emptyState(gtx, "", i18n.Plain(nsSCNext.RT("No results")),
					i18n.Plain(nsSCNext.RT("You might want to try using different keywords or checking for typos.")), nil, "")
			})
		})
	}
	return rows
}

// Layout draws the page: the 50px view header, then the list.
func (o *ExternalOrg) Layout(gtx C) D {
	o.frame()
	o.d.frame()
	o.start()
	if o.clear.Clicked(gtx) {
		o.in.ed.SetText("")
		o.deb.set("", gtx.Now)
		o.deb.sent = ""
		o.setQuery("")
	}
	q, changed, submitted := o.in.update(gtx)
	if changed {
		o.deb.set(q, gtx.Now)
	}
	if submitted {
		o.deb.set(q, gtx.Now)
		o.deb.sent = q
		o.setQuery(q)
	} else if dq, ok := o.deb.dueVia(o.h, gtx, dirDebounce); ok {
		o.setQuery(dq)
	}
	size := gtx.Constraints.Max
	fill(gtx, image.Rectangle{Max: size}, o.p.Bg)
	hh := gtx.Dp(50)
	o.header(gtx, size.X, hh)
	body := gtx
	body.Constraints = layout.Exact(image.Pt(size.X, max(0, size.Y-hh)))
	st := op.Offset(image.Pt(0, hh)).Push(gtx.Ops)
	rows := o.rowList()
	n := len(rows)
	o.d.scrollList(body, &o.list, n+1, func(gtx C, i int) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		if i == n {
			return D{Size: image.Pt(gtx.Constraints.Max.X, gtx.Dp(20))}
		}
		r := rows[i]
		if r.key == "organization-page-header" {
			return r.draw(gtx)
		}
		return layout.Inset{Left: 20, Right: 20}.Layout(gtx, r.draw)
	})
	st.Pop()
	return D{Size: size}
}

// header is the view header: the back button when the pane has a back
// stack, "About {teamName}" (18/24 black), the close button.
func (o *ExternalOrg) header(gtx C, w, hh int) {
	if o.back.Clicked(gtx) {
		if b, ok := o.h.(SecondaryBacker); ok {
			b.GoSecondaryBack()
		}
	}
	if o.closeBtn.Clicked(gtx) {
		o.h.CloseSecondary()
	}
	line := max(1, gtx.Dp(1))
	fill(gtx, image.Rect(0, hh-line, w, hh), o.p.Border)
	bs := gtx.Dp(28)
	x := gtx.Dp(12)
	y := (hh - line - bs) / 2
	if b, ok := o.h.(SecondaryBacker); ok && b.SecondaryBack() {
		at := op.Offset(image.Pt(x, y)).Push(gtx.Ops)
		o.iconButton(gtx, &o.back, "caret-left-full", 28, 20, o.p.Sec)
		at.Pop()
		x += bs + gtx.Dp(8)
	}
	cx := w - gtx.Dp(12) - bs
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(max(0, cx-gtx.Dp(8)-x), hh)}
	rec := op.Record(gtx.Ops)
	d := o.th.Layout(g, themeText(18, 24, wBlack, o.p.Text), o.Title())
	call := rec.Stop()
	t := op.Offset(image.Pt(x, (hh-line-d.Size.Y)/2)).Push(gtx.Ops)
	call.Add(gtx.Ops)
	t.Pop()
	at := op.Offset(image.Pt(cx, y)).Push(gtx.Ops)
	dd := o.iconButton(gtx, &o.closeBtn, "close", 28, 20, o.p.Sec)
	popups.Tooltip(gtx, o.th, &o.closeTip, o.closeBtn.Hovered(), dd.Size, popups.BelowRight, i18n.T("general", "Close"), "")
	at.Pop()
}

// pageHeader is p-organization__page_header.
func (o *ExternalOrg) pageHeader(gtx C) D {
	w := gtx.Constraints.Max.X
	t := o.teamOf()
	banner := gtx.Dp(150)
	icon := gtx.Dp(160)
	pad := gtx.Dp(20)
	top := banner - gtx.Dp(90)
	nameY := top + icon + gtx.Dp(16)
	m := op.Record(gtx.Ops)
	nd := dirAt(gtx, image.Pt(pad, nameY), func(gtx C) D {
		gtx.Constraints = layout.Constraints{Max: image.Pt(max(0, w-2*pad), gtx.Dp(200))}
		return row(gtx, layout.Middle,
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Left: 16}.Layout(gtx, func(gtx C) D {
					return o.para(gtx, 28, 34, wBlack, o.p.Text, 2, t.Name)
				})
			}),
			layout.Rigid(func(gtx C) D {
				if !t.IsVerified {
					return D{}
				}
				return layout.Inset{Left: 4}.Layout(gtx, func(gtx C) D {
					return o.verified.Layout(gtx, func(gtx C) D {
						d := o.icon(gtx, "verified-filled", 20, argb(0xff4a154b))
						popups.Tooltip(gtx, o.th, &o.verTip, o.verified.Hovered(), d.Size, popups.Below,
							nsSCNext.T("This organization’s identity has been verified by Slack."), "")
						return d
					})
				})
			}),
		)
	})
	body := m.Stop()
	h := nameY + nd.Size.Y + gtx.Dp(16) + max(1, gtx.Dp(1))
	fill(gtx, image.Rect(0, 0, w, h), o.p.Surface)
	o.banner(gtx, t, image.Pt(w, banner))
	// the icon: elevation_3, a 4px white border, radius 20 (the image's 16)
	ir := image.Rect(pad, top, pad+icon, top+icon)
	rrect(gtx, ir.Add(image.Pt(0, gtx.Dp(4))).Inset(-gtx.Dp(2)), gtx.Dp(22), argb(0x14000000))
	rrect(gtx, ir, gtx.Dp(20), argb(0xffffffff))
	in := ir.Inset(gtx.Dp(4))
	dirAt(gtx, in.Min, func(gtx C) D { return o.teamImage(gtx, t, in.Dx(), gtx.Dp(16), true) })
	body.Add(gtx.Ops)
	fill(gtx, image.Rect(0, h-max(1, gtx.Dp(1)), w, h), o.p.Border)
	return D{Size: image.Pt(w, h+gtx.Dp(20))}
}

// banner is TeamImageBanner: the team image scaled across the band and
// blurred (drawn from a tiny copy, which the scaler smooths), or the
// gray tile behind a team without an image.
func (o *ExternalOrg) banner(gtx C, t core.ExternalTeam, sz image.Point) {
	defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
	url := dirFirst(t.IconLarge, t.Icon)
	if url != "" && !t.IconDefault && o.h.Images() != nil {
		if img, ok := o.h.Images().Get(url, 8); ok {
			ext := gtx.Dp(40)
			dirAt(gtx, image.Pt(-ext, -ext), func(gtx C) D {
				s := sz.X + 2*ext
				gtx.Constraints = layout.Exact(image.Pt(s, s))
				return theme.Image(gtx, img, image.Pt(s, s), 0)
			})
			return
		}
	}
	fill(gtx, image.Rectangle{Max: sz}, argb(0xffbbbabb))
}

// teamImage is the team's avatar (allowTeamInitials), size px square.
func (o *ExternalOrg) teamImage(gtx C, t core.ExternalTeam, size, rad int, large bool) D {
	sz := image.Pt(size, size)
	url := t.Icon
	if large {
		url = dirFirst(t.IconLarge, t.Icon)
	}
	if url != "" && !t.IconDefault {
		return dirImage(&o.base, gtx, url, sz, rad)
	}
	rrect(gtx, image.Rectangle{Max: sz}, rad, argb(0xffbbbabb))
	init := ""
	for _, r := range t.Name {
		init = strings.ToUpper(string(r))
		break
	}
	g := gtx
	g.Constraints = layout.Exact(sz)
	layout.Center.Layout(g, func(gtx C) D {
		return o.label(gtx, unit.Sp(float32(size)/gtx.Metric.PxPerSp*0.5), wBold, argb(0xffffffff), init)
	})
	return D{Size: sz}
}

func (o *ExternalOrg) searchRow(gtx C) D {
	return layout.Inset{Bottom: 0}.Layout(gtx, func(gtx C) D {
		return dirSearchField(&o.d.base, gtx, o.in, dirFieldStyle{H: 36, Icon: 20, PadL: 12, Gap: 16, Border: dirRing(&o.d.base, o.p.Surface), Over: o.p.ReadFill}, &o.clear)
	})
}

// skeleton is SlackConnectHubSkeleton (conversations, four cards, a title).
func (o *ExternalOrg) skeleton(gtx C) D {
	return layout.Inset{Top: 32}.Layout(gtx, func(gtx C) D { return o.d.skeleton(gtx, 4, 0) })
}

// listTitle is a list's h3 (13/18 bold, 16px in) 32px under what's above.
func (o *ExternalOrg) listTitle(gtx C, title string, below unit.Dp) D {
	return layout.Inset{Top: 32, Bottom: below, Left: 16, Right: 16}.Layout(gtx, func(gtx C) D {
		return o.para(gtx, 13, 18, wBold, o.p.Text, 1, title)
	})
}

// convRow is ConversationRow: ChannelRow, ImRow or MpimRow, padded 16px
// in the bordered list (p-organizations__conversations).
func (o *ExternalOrg) convRow(gtx C, c core.Conv, others, first, last bool) D {
	rc := dirClick(o.rows, c.ID)
	if rc.Clicked(gtx) {
		o.h.Navigate("c/" + c.ID)
	}
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	m := op.Record(gtx.Ops)
	dims := layout.UniformInset(16).Layout(gtx, func(gtx C) D {
		switch c.Kind {
		case "im":
			return o.imRow(gtx, c)
		case "mpim":
			return o.mpimRow(gtx, c)
		}
		return o.channelRow(gtx, c, others)
	})
	call := m.Stop()
	dirRowShape(&o.d.base, gtx, dims.Size, first, last)
	if rc.Hovered() {
		fill(gtx, image.Rectangle{Max: dims.Size}, o.p.ReadFill)
	}
	if !last {
		fill(gtx, image.Rect(0, dims.Size.Y-max(1, gtx.Dp(1)), dims.Size.X, dims.Size.Y), argb(0xffdddddd))
	}
	gg := gtx
	gg.Constraints = layout.Exact(dims.Size)
	rc.Layout(gg, func(gtx C) D {
		pointerCursor(gtx)
		return D{Size: dims.Size}
	})
	call.Add(gtx.Ops)
	return dims
}

// channelRow is ChannelRow: the name (bold, its # or lock), "{count}
// members", then the caption: OrganizationDetails · the purpose.
func (o *ExternalOrg) channelRow(gtx C, c core.Conv, others bool) D {
	n, _ := o.count(c.ID)
	return col(gtx,
		layout.Rigid(func(gtx C) D {
			ic := "hash"
			if c.Kind == "private" {
				ic = "lock"
			}
			return row(gtx, layout.Middle,
				layout.Rigid(func(gtx C) D { return o.icon(gtx, ic, 15, o.p.Text) }),
				hgap(2),
				layout.Flexed(1, func(gtx C) D { return o.para(gtx, 15, 22, wBold, o.p.Text, 1, c.Name) }),
			)
		}),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Bottom: 4}.Layout(gtx, func(gtx C) D {
				return o.label(gtx, 15, wReg, o.p.Text, nsSCNext.T("{count, plural, =1 {# members} other {# members}}", "count", n.people))
			})
		}),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 2}.Layout(gtx, func(gtx C) D { return o.caption(gtx, c, others) })
		}),
	)
}

// membership is OrganizationDetails' text: "<strong>N members</strong>
// are from {team}" (for Other channels the teams besides the viewer's:
// one by name, else "other organizations").
func (o *ExternalOrg) membership(ch string, others bool) []i18n.Part {
	n, _ := o.count(ch)
	t := o.teamOf()
	cnt := n.byTeam[o.team]
	name := t.Name
	if others {
		ids := o.otherTeams(n)
		cnt = 0
		for _, id := range ids {
			cnt += n.byTeam[id]
		}
		name = nsSCNext.T("{count, plural, =1 { { teamName } } other {other organizations}}", "count", len(ids), "teamName", t.Name)
		name = strings.TrimSpace(name)
	}
	parts := nsSCNext.RT("<strong>{formattedMemberCountForDisplay} {count, plural, =1 {member</strong> is from} other {members</strong> are from}}",
		"formattedMemberCountForDisplay", dirNumber(cnt), "count", cnt)
	return append(parts, i18n.Part{Text: " " + name})
}

// otherTeams is the by_team teams besides the viewer's team and org.
func (o *ExternalOrg) otherTeams(n scOrgCount) []string {
	c := o.h.Core()
	own := map[string]bool{}
	if c != nil {
		for _, id := range c.SelfTeamIDs() {
			own[id] = true
		}
	}
	var ids []string
	for id := range n.byTeam {
		if !own[id] {
			ids = append(ids, id)
		}
	}
	sortNames(ids)
	return ids
}

// caption is p-conversation_row__caption (13/18, foreground max): the
// team icon (or for Other channels the pile of the other teams' icons),
// the membership text, " · " and the purpose.
func (o *ExternalOrg) caption(gtx C, c core.Conv, others bool) D {
	n, _ := o.count(c.ID)
	sec := o.p.Muted
	var kids []layout.FlexChild
	if others {
		ids := o.otherTeams(n)
		if len(ids) > 0 {
			kids = append(kids, layout.Rigid(func(gtx C) D {
				return layout.Inset{Right: 8}.Layout(gtx, func(gtx C) D { return o.teamPile(gtx, ids) })
			}))
		}
	} else {
		kids = append(kids, layout.Rigid(func(gtx C) D {
			return layout.Inset{Right: 4}.Layout(gtx, func(gtx C) D { return o.teamImage(gtx, o.teamOf(), gtx.Dp(16), gtx.Dp(4), false) })
		}))
	}
	for _, p := range o.membership(c.ID, others) {
		w := wReg
		if p.Tag == "strong" {
			w = wBold
		}
		txt := p.Text
		kids = append(kids, layout.Rigid(func(gtx C) D { return o.label(gtx, 13, w, sec, txt) }))
	}
	if p := strings.TrimSpace(c.Purpose); p != "" {
		kids = append(kids, layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 4, Right: 4}.Layout(gtx, func(gtx C) D { return o.label(gtx, 13, wReg, sec, "·") })
		}))
		kids = append(kids, layout.Flexed(1, func(gtx C) D {
			if spans := o.d.purposeSpans(gtx.Now, p); len(spans) > 0 {
				return dirFill(gtx, func(gtx C) D { return o.d.spanLine(gtx, spans, 13, sec) })
			}
			return dirFill(gtx, func(gtx C) D { return o.label(gtx, 13, wReg, sec, o.d.plainPurpose(p)) })
		}))
	}
	return row(gtx, layout.Middle, kids...)
}

// teamPile is Pile: up to seven 16px team icons, each over the last by
// 4px with a 2px ring.
func (o *ExternalOrg) teamPile(gtx C, ids []string) D {
	s := gtx.Dp(16)
	step := s - gtx.Dp(4)
	c := o.h.Core()
	ids = ids[:min(len(ids), 7)]
	for i, id := range ids {
		t := core.ExternalTeam{ID: id}
		if c != nil {
			if x, ok := c.ExternalTeamByID(id); ok {
				t = x
			}
		}
		dirAt(gtx, image.Pt(i*step, 0), func(gtx C) D {
			rrect(gtx, image.Rect(-gtx.Dp(2), -gtx.Dp(2), s+gtx.Dp(2), s+gtx.Dp(2)), gtx.Dp(6), o.p.Surface)
			return o.teamImage(gtx, t, s, gtx.Dp(4), false)
		})
	}
	return D{Size: image.Pt(step*(len(ids)-1)+s, s)}
}

// imRow is ImRow: the 36px avatar, 12px, the name over the latest
// message ("Nobody has started the conversation — yet." without one).
func (o *ExternalOrg) imRow(gtx C, c core.Conv) D {
	u, _ := o.user(c.User)
	return row(gtx, layout.Middle,
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Right: 12}.Layout(gtx, func(gtx C) D { return o.avatar(gtx, dirFirst(u.Avatar192, u.Avatar), 36) })
		}),
		layout.Flexed(1, func(gtx C) D {
			return col(gtx,
				layout.Rigid(func(gtx C) D { return o.para(gtx, 15, 22, wReg, o.p.Text, 1, displayName(u)) }),
				layout.Rigid(func(gtx C) D { return o.preview(gtx, c.ID) }),
			)
		}),
	)
}

// mpimRow is MpimRow: MemberPile (two 24px avatars corner to corner, or
// one and the count), the members' names, the latest message.
func (o *ExternalOrg) mpimRow(gtx C, c core.Conv) D {
	return row(gtx, layout.Middle,
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Right: 12}.Layout(gtx, func(gtx C) D { return o.memberPile(gtx, c.Members) })
		}),
		layout.Flexed(1, func(gtx C) D {
			name := o.groupNames(c.Members)
			if name == "" {
				name = mpimName(c.Name)
			}
			return col(gtx,
				layout.Rigid(func(gtx C) D { return o.para(gtx, 15, 22, wReg, o.p.Text, 1, name) }),
				layout.Rigid(func(gtx C) D { return o.preview(gtx, c.ID) }),
			)
		}),
	)
}

func (o *ExternalOrg) memberPile(gtx C, members []string) D {
	sz := gtx.Dp(36)
	a := gtx.Dp(24)
	var us []core.User
	for _, id := range members {
		if u, ok := o.user(id); ok && !u.Deleted {
			us = append(us, u)
		}
	}
	many := len(us) > 2
	if many {
		us = us[:1]
	}
	for i, u := range us {
		pt := image.Pt(0, 0)
		if i == 1 || (len(us) == 2 && i == 1) {
			pt = image.Pt(sz-a, sz-a)
		}
		dirAt(gtx, pt, func(gtx C) D { return o.avatar(gtx, u.Avatar, 24) })
	}
	if many {
		n := 0
		for _, id := range members {
			if u, ok := o.user(id); ok && !u.Deleted {
				n++
			}
		}
		dirAt(gtx, image.Pt(sz-a, sz-a), func(gtx C) D {
			r := image.Rect(0, 0, a, a)
			rrect(gtx, r.Inset(-gtx.Dp(2)), gtx.Dp(6), o.p.Surface)
			rrect(gtx, r, gtx.Dp(4), argb(0xffdddddd))
			g := gtx
			g.Constraints = layout.Exact(r.Max)
			return layout.Center.Layout(g, func(gtx C) D { return o.label(gtx, 13, wBold, argb(0xff616061), dirNumber(n)) })
		})
	}
	return D{Size: image.Pt(sz, sz)}
}

// preview is the row's caption: LatestMessagePreview ("You: …" or the
// sender's name, then the text on one line), else Slack's "Nobody has
// started the conversation — yet."
func (o *ExternalOrg) preview(gtx C, ch string) D {
	sec := o.p.Muted
	c := o.h.Core()
	text := nsSCNext.T("Nobody has started the conversation — yet.")
	if c != nil {
		if ts := c.Count(ch).Latest; ts != "" {
			if m, ok := c.StoredMessage(ch, ts); ok {
				who := o.userName(m.User)
				if m.User == o.selfID() {
					who = i18n.T("latest_message_preview", "You")
				}
				body := o.d.plainPurpose(m.Text)
				if who != "" {
					body = who + ": " + body
				}
				text = body
			}
		}
	}
	return layout.Inset{Top: 2}.Layout(gtx, func(gtx C) D { return o.para(gtx, 13, 18, wReg, sec, 1, text) })
}

// memberGrid is MemberGrid: the directory's member cards, 192 to 311px
// wide and 16px apart, up to 24.
func (o *ExternalOrg) memberGrid(gtx C, us []core.User) D {
	us = us[:min(len(us), 24)]
	w := gtx.Constraints.Max.X
	gap := gtx.Dp(16)
	cols := max(1, (w+gap)/(gtx.Dp(192)+gap))
	cw := min((w-gap*(cols-1))/cols, gtx.Dp(311))
	ch := cw + gtx.Dp(dirCardText)
	rows := (len(us) + cols - 1) / cols
	for r := 0; r < rows; r++ {
		for k := 0; k < cols; k++ {
			i := r*cols + k
			if i >= len(us) {
				break
			}
			u := us[i]
			dirAt(gtx, image.Pt(k*(cw+gap), r*(ch+gap)), func(gtx C) D {
				gtx.Constraints = layout.Exact(image.Pt(cw, ch))
				return o.d.personCard(gtx, u, cw, ch)
			})
		}
	}
	return D{Size: image.Pt(w, max(0, rows*(ch+gap)-gap))}
}

// dirNumber is the count as Slack formats it for display (1,234).
func dirNumber(n int) string {
	s := strconv.Itoa(n)
	if n < 1000 && n > -1000 {
		return s
	}
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}
