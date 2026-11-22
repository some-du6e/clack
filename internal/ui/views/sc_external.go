package views

// Directories › External: Slack's ExternalOrganizationsView (slack-connect
// hub chunk SjEJ, build a09b625d90aa), its rows as useExternalOrganizations
// Explorer lays them out with no query:
//
//   - the loading list while the connected or the available teams load;
//   - the hero banner (SlackConnectHeroBannerCta) unless dismissed
//     (users.interactions.list HUB_NEXT_EDU_BANNER / DISMISS, read once):
//     "Work with people outside {org} in Slack", its picture (hidden at
//     500px or narrower), the close button only when there are any
//     connections (dismissing records the interaction), and two cards:
//     "Create a channel with external people" (when the viewer may create
//     channels; disabled without canUserInviteExternalMembers, its
//     tooltip saying why) and "Talk one-on-one" (disabled without the DM
//     eligibility), then "Contact an admin to use Slack Connect…" for a
//     non-admin who has a disabled card;
//   - for an admin with no connections, "Set {org} up for safe, secure
//     collaboration" and the three admin CTAs (Customize settings, Manage
//     requests, Manage connections), links to the admin pages;
//   - "Your connections" (externalTeams/list connected), "N conversations";
//   - the tutorial (How to create channels with external people) and the
//     social proof (the quote carousel and the logo farm): before the
//     available teams when nothing is connected, after them otherwise;
//   - "All {org} connections" / "Other {org} connections" (available,
//     20 a page, Load More while next_marker);
//
// and with a query the organizations externalTeams/search finds
// ("Organizations"), or "No results found". A row opens Slack's
// external organization page (navigateToExternalConnections {teamId},
// sc_org.go) in the secondary pane.

import (
	"context"
	"image"
	"image/color"
	"math"
	"strings"
	"sync"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/slackasset"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

var (
	nsSCHub  = i18n.NS("slack_connect_hub")
	nsSC     = i18n.NS("slack_connect")
	nsSCNext = i18n.NS("slack_connect_hub_next")
)

// scBannerComponent is el.G7.HubNextEduBanner (dY9S).
const scBannerComponent = "HUB_NEXT_EDU_BANNER"

// scTeams is a teams list with its channel counts.
type scTeams struct {
	teams  []core.ExternalTeam
	counts map[string]core.ExternalTeamCount
	next   string
}

type scQuote struct {
	quote, designation, name, image string
}

// scExternal is the External tab's state.
type scExternal struct {
	d *Directories

	hub       async[core.SCHub]
	conn      async[scTeams]
	avail     async[scTeams]
	dismissed async[bool]
	srch      async[scTeams]
	q         string
	off       bool    // dismissed this session
	in        *search // "Search for people by their name, company, or email address"
	deb       debounce
	clear     widget.Clickable

	list      widget.List
	rows      map[string]*widget.Clickable
	more      widget.Clickable
	admin     [3]widget.Clickable
	bannerX   widget.Clickable
	ctaCh     widget.Clickable
	ctaDM     widget.Clickable
	tipCh     popups.TipState
	tipDM     popups.TipState
	contact   widget.Clickable
	prevQ     widget.Clickable
	nextQ     widget.Clickable
	dots      [3]widget.Clickable
	quote     int
	slideFrom int
	slideAt   time.Time
	shownAt   time.Time // the carousel's 7 s timer
	qwk, twk  wake      // the next quote's frame, the tooltips' (never a mid-layout InvalidateCmd)
	mu        sync.Mutex
}

func newSCExternal(d *Directories) *scExternal {
	e := &scExternal{d: d, rows: map[string]*widget.Clickable{}}
	e.list.Axis = layout.Vertical
	e.tipCh.Wake, e.tipDM.Wake = tipWake(d.h, &e.twk), tipWake(d.h, &e.twk)
	e.in = newSearchIn(nsSC, "Search for people by their name, company, or email address")
	return e
}

// start loads the tab's reads the first time it shows.
func (e *scExternal) start() {
	c := e.d.h.Core()
	if c == nil || e.hub.started() {
		return
	}
	e.hub.load(e.d.h, true, func(ctx context.Context) (core.SCHub, error) { return c.LoadSlackConnectHub(ctx), nil })
	e.dismissed.load(e.d.h, true, func(ctx context.Context) (bool, error) {
		m, err := c.UserInteractions(ctx, [2]string{scBannerComponent, "DISMISS"})
		if err != nil {
			return false, nil // loadUserInteractions' catch: logged, the banner shows
		}
		_, ok := m[core.InteractionKey(scBannerComponent, "DISMISS", "")]
		return ok, nil
	})
	e.conn.load(e.d.h, true, func(ctx context.Context) (scTeams, error) { return scLoadTeams(ctx, c, "connected", 60, "", nil) })
	e.avail.load(e.d.h, true, func(ctx context.Context) (scTeams, error) { return scLoadTeams(ctx, c, "available", 20, "", nil) })
}

// scLoadTeams is fetchAndUpsertExternalTeamsList, then the counts of the
// teams that have none (fetchAndUpsertExternalTeamsCounts); prev is the
// list so far (Load More).
func scLoadTeams(ctx context.Context, c *core.App, filter string, count int, marker string, prev *scTeams) (scTeams, error) {
	ts, next, err := c.ExternalTeams(ctx, filter, count, marker)
	if err != nil {
		return scTeams{}, err
	}
	out := scTeams{counts: map[string]core.ExternalTeamCount{}, next: next}
	if prev != nil {
		out.teams = append(out.teams, prev.teams...)
		for k, v := range prev.counts {
			out.counts[k] = v
		}
	}
	// a team a page repeats is listed once (the teams store upserts by id)
	seen := make(map[string]bool, len(out.teams)+len(ts))
	for _, t := range out.teams {
		seen[t.ID] = true
	}
	for _, t := range ts {
		if !seen[t.ID] {
			seen[t.ID] = true
			out.teams = append(out.teams, t)
		}
	}
	var need []string
	for _, t := range ts {
		if _, ok := out.counts[t.ID]; !ok {
			need = append(need, t.ID)
		}
	}
	if len(need) > 0 {
		if cs, err := c.ExternalTeamCounts(ctx, need, count); err == nil {
			for k, v := range cs {
				out.counts[k] = v
			}
		}
	}
	return out, nil
}

// rowsOf is useExternalTeamsList's teamIdChannelCountList: an available
// team the viewer already shares with is left out.
func (t scTeams) rowsOf(available bool) []scTeamRow {
	var out []scTeamRow
	for _, x := range t.teams {
		n, known := t.counts[x.ID]
		if available && known && n.SharesWith() {
			continue
		}
		out = append(out, scTeamRow{team: x, count: n.Total})
	}
	return out
}

type scTeamRow struct {
	team  core.ExternalTeam
	count int
}

// setQuery is the search: externalTeams/search {query} for the
// organizations (fetchAndUpsertExternalTeamsWithQuery), with their counts.
func (e *scExternal) setQuery(q string) {
	q = strings.TrimSpace(q)
	if q == e.q {
		return
	}
	e.q = q
	e.list.Position = layout.Position{}
	c := e.d.h.Core()
	if q == "" || c == nil {
		return
	}
	e.srch.load(e.d.h, false, func(ctx context.Context) (scTeams, error) {
		ts, err := c.SearchExternalTeams(ctx, q)
		if err != nil {
			return scTeams{}, err
		}
		out := scTeams{teams: ts, counts: map[string]core.ExternalTeamCount{}}
		var ids []string
		for _, t := range ts {
			ids = append(ids, t.ID)
		}
		if len(ids) > 0 {
			if cs, err := c.ExternalTeamCounts(ctx, ids, len(ids)); err == nil {
				out.counts = cs
			}
		}
		return out, nil
	})
}

// Hub reads (tests).

// SCExternalLoaded reports the tab's lists, its rights and the banner's
// interaction all in.
func (d *Directories) SCExternalLoaded() bool {
	e := d.ext
	_, a, _ := e.hub.get()
	_, b, _ := e.conn.get()
	_, c, _ := e.avail.get()
	_, x, _ := e.dismissed.get()
	return a && b && c && x && !e.conn.busy() && !e.avail.busy()
}

// SCExternalRows is the tab's rows as their keys and labels, in order.
func (d *Directories) SCExternalRows() []string {
	var out []string
	for _, r := range d.ext.rowList() {
		out = append(out, r.key)
	}
	return out
}

// SCExternalLoadMore is the available list's Load More.
func (d *Directories) SCExternalLoadMore() { d.ext.loadMore() }

// SCExternalSearch types q into the tab's search.
func (d *Directories) SCExternalSearch(q string) { d.ext.in.ed.SetText(q); d.ext.setQuery(q) }

// SCOpenTeam is an organization row's click.
func (d *Directories) SCOpenTeam(id string) { d.ext.openTeam(id) }

// SCCTA reports the banner's cards: whether each is drawn and disabled,
// and the contact-admin line.
func (d *Directories) SCCTA() (channel, channelOff, dmOff bool, contact string) {
	h, _, _ := d.ext.hub.get()
	return h.CreateChannel, !h.Invite.OK, !h.DMEligible, d.ext.contactText(h)
}

// SCScrollToEnd scrolls the tab's list to its last rows (the tutorial and
// the social proof).
func (d *Directories) SCScrollToEnd() { d.ext.list.Position = layout.Position{First: 1 << 20} }

// SCQuoteDue is when the carousel moves on (the shown quote's 7 s), and
// the frame the tab asked the host for, with its timer still out.
func (d *Directories) SCQuoteDue() (next, asked time.Time, pending bool) {
	asked, pending = d.ext.qwk.due()
	return d.ext.shownAt.Add(7 * time.Second), asked, pending
}

// SCNextQuote is the carousel's › (now on the frame's clock).
func (d *Directories) SCNextQuote(now time.Time) { d.ext.goQuote(d.ext.quote+1, now) }

// SCInviteFilterOpen opens Invitations' Filter by menu.
func (d *Directories) SCInviteFilterOpen() { d.inv.filter.open = true }

// SCDismissBanner is the banner's close button.
func (d *Directories) SCDismissBanner() { d.ext.dismissBanner() }

func (e *scExternal) loadMore() {
	st, loaded, _ := e.avail.get()
	c := e.d.h.Core()
	if !loaded || e.avail.busy() || st.next == "" || c == nil {
		return
	}
	prev := st
	e.avail.load(e.d.h, true, func(ctx context.Context) (scTeams, error) {
		return scLoadTeams(ctx, c, "available", 20, prev.next, &prev)
	})
}

// ExternalOrgOpener opens the external organization page in the
// secondary pane (toUnifiedDirectoryExternalOrganization).
type ExternalOrgOpener interface {
	OpenExternalOrg(teamID string)
}

// openTeam is navigateToExternalConnections {teamId}: the organization's
// page (ExternalOrganizationView, sc_org.go) beside the hub.
func (e *scExternal) openTeam(id string) {
	if o, ok := e.d.h.(ExternalOrgOpener); ok {
		o.OpenExternalOrg(id)
	}
}

func (e *scExternal) dismissBanner() {
	e.off = true
	c := e.d.h.Core()
	if c == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = c.RecordUserInteraction(ctx, scBannerComponent, "DISMISS") // recordUserInteraction
	}()
}

// contactText is ExternalConnectionsContactAdminLink for the banner's
// disabled cards (none for an admin).
func (e *scExternal) contactText(h core.SCHub) string {
	if h.AdminContent {
		return ""
	}
	ch := h.CreateChannel && !h.Invite.OK
	dm := !h.DMEligible
	switch {
	case ch && dm:
		return nsSC.T("Contact an admin to use Slack Connect")
	case ch:
		return nsSC.T("Contact an admin to use Slack Connect in channels")
	case dm:
		return nsSC.T("Contact an admin to use Slack Connect in DMs")
	}
	return ""
}

// ---- rows ----

type scRow struct {
	key  string
	draw func(gtx C) D
}

func (e *scExternal) rowList() []scRow {
	d := e.d
	hub, hubOK, _ := e.hub.get()
	conn, connOK, _ := e.conn.get()
	avail, availOK, _ := e.avail.get()
	dismissed, disOK, _ := e.dismissed.get()
	var rows []scRow
	add := func(k string, f func(gtx C) D) { rows = append(rows, scRow{key: k, draw: f}) }
	if e.q != "" {
		return e.searchRows()
	}
	loadingConn, loadingAvail := !connOK || e.conn.busy() && len(conn.teams) == 0, !availOK || e.avail.busy() && len(avail.teams) == 0
	if loadingConn || loadingAvail {
		add("list-loading", func(gtx C) D { return d.skeleton(gtx, 6, 36) })
	}
	cr, ar := conn.rowsOf(false), avail.rowsOf(true)
	has := len(cr) > 0 || len(ar) > 0
	banner := disOK && !dismissed && !e.off && hubOK
	if banner {
		add("nux-banner", func(gtx C) D { return e.banner(gtx, hub, has) })
	}
	admin := hub.AdminContent && !loadingConn && !loadingAvail && !has
	if admin {
		title := nsSC.T("Set {selfOrgOrTeamName} up for safe, secure collaboration", "selfOrgOrTeamName", hub.TeamName)
		add("admin", func(gtx C) D { return e.section(gtx, title, !banner) })
		for i, k := range []string{"customize-settings", "manage-requests", "manage-connections"} {
			add(k, func(gtx C) D { return e.adminCTA(gtx, i, hub) })
		}
	}
	if !loadingConn && len(cr) > 0 {
		add("connected-teams", func(gtx C) D { return e.section(gtx, nsSC.T("Your connections"), !banner && !admin) })
		for i, r := range cr {
			add("connected-"+r.team.ID, func(gtx C) D { return e.teamRow(gtx, r, i == 0, i == len(cr)-1) })
		}
	}
	tutorial := func() {
		add("tutorial", func(gtx C) D { return e.tutorial(gtx, hub.TeamName) })
		add("social-proof", func(gtx C) D { return e.socialProof(gtx) })
	}
	if len(cr) == 0 && !loadingConn {
		tutorial()
	}
	if !loadingAvail && len(ar) > 0 {
		title := nsSC.T("All {selfOrgOrTeamName} connections", "selfOrgOrTeamName", hub.TeamName)
		if len(cr) > 0 {
			title = nsSC.T("Other {selfOrgOrTeamName} connections", "selfOrgOrTeamName", hub.TeamName)
		}
		add("available-teams", func(gtx C) D { return e.section(gtx, title, !banner && !admin && len(cr) > 0) })
		for i, r := range ar {
			add("available-"+r.team.ID, func(gtx C) D { return e.teamRow(gtx, r, i == 0, i == len(ar)-1) })
		}
	}
	if avail.next != "" {
		add("load-more-available-orgs", func(gtx C) D {
			return layout.Inset{Top: 4, Bottom: 8}.Layout(gtx, func(gtx C) D {
				return layout.Center.Layout(gtx, func(gtx C) D {
					if e.avail.busy() {
						return popups.LoadingButtonWake(gtx, d.th, &e.more, popups.BtnMedium, 80, tipWake(d.h, &e.twk))
					}
					return d.button(gtx, &e.more, btnOutline, false, nsBrowsers.T("Load More"))
				})
			})
		})
	}
	if len(cr) > 0 && !loadingConn {
		tutorial()
	}
	return rows
}

func (e *scExternal) searchRows() []scRow {
	d := e.d
	st, loaded, err := e.srch.get()
	var rows []scRow
	add := func(k string, f func(gtx C) D) { rows = append(rows, scRow{key: k, draw: f}) }
	if !loaded || e.srch.busy() {
		add("list-loading", func(gtx C) D { return d.skeleton(gtx, 6, 36) })
		return rows
	}
	rs := st.rowsOf(false)
	if err != nil || len(rs) == 0 {
		add("search-results-empty", func(gtx C) D {
			return dirListState(gtx, func(gtx C) D {
				return d.emptyState(gtx, "", nsSC.T("No results found"),
					nsSC.T("You might want to check for typos or try different keywords. If you’re trying to see if someone is on Slack, make sure you’ve entered a full email address."), nil, "")
			})
		})
		return rows
	}
	add("org-search-results-label", func(gtx C) D { return e.section(gtx, nsSC.T("Organizations"), true) })
	for i, r := range rs {
		add("search-"+r.team.ID, func(gtx C) D { return e.teamRow(gtx, r, i == 0, i == len(rs)-1) })
	}
	return rows
}

// page is the tab: ExplorerView's search row (the input, the header
// actions: Create Channel when the viewer may invite external people),
// 16px, the list.
func (e *scExternal) page(gtx C) D {
	d := e.d
	if e.clear.Clicked(gtx) {
		e.in.ed.SetText("")
		e.deb.set("", gtx.Now)
		e.deb.sent = ""
		e.setQuery("")
	}
	q, changed, submitted := e.in.update(gtx)
	if changed {
		e.deb.set(q, gtx.Now)
	}
	if submitted {
		e.deb.set(q, gtx.Now)
		e.deb.sent = q
		e.setQuery(q)
	} else if dq, ok := e.deb.dueVia(e.d.h, gtx, dirDebounce); ok {
		e.setQuery(dq)
	}
	hub, _, _ := e.hub.get()
	return col(gtx,
		vgap(16),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: dirPad, Right: dirPad}.Layout(gtx, func(gtx C) D {
				return row(gtx, layout.Middle,
					layout.Flexed(1, func(gtx C) D {
						return dirSearchField(&d.base, gtx, e.in, dirFieldStyle{H: 36, Icon: 20, PadL: 12, Gap: 16, Border: dirRing(&d.base, d.p.Surface), Over: d.p.ReadFill}, &e.clear)
					}),
					layout.Rigid(func(gtx C) D {
						if !hub.CreateChannel || !hub.Invite.OK { // the channel CTA's disabled: not drawn
							return D{}
						}
						if e.ctaCh.Clicked(gtx) {
							if cc, ok := d.h.(interface{ OpenCreateChannel() }); ok {
								cc.OpenCreateChannel()
							}
						}
						return layout.Inset{Left: 12}.Layout(gtx, func(gtx C) D {
							return d.button(gtx, &e.ctaCh, btnOutline, false, nsSCHub.T("Create Channel"))
						})
					}),
				)
			})
		}),
		vgap(16),
		layout.Flexed(1, e.layout),
	)
}

// layout draws the tab's list.
func (e *scExternal) layout(gtx C) D {
	e.start()
	if e.more.Clicked(gtx) {
		e.loadMore()
	}
	if e.bannerX.Clicked(gtx) {
		e.dismissBanner()
	}
	rows := e.rowList()
	n := len(rows)
	return e.d.scrollList(gtx, &e.list, n+1, func(gtx C, i int) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		if i == n {
			return D{Size: image.Pt(gtx.Constraints.Max.X, gtx.Dp(24))}
		}
		r := rows[i]
		full := r.key == "nux-banner" || r.key == "tutorial" || r.key == "social-proof"
		if full {
			return r.draw(gtx)
		}
		return layout.Inset{Left: dirPad, Right: dirPad}.Layout(gtx, r.draw)
	})
}

// section is the section-label row: 13px bold content-sec, padding 16px 0
// 8px (none above the first).
func (e *scExternal) section(gtx C, title string, first bool) D {
	top := unit.Dp(16)
	if first {
		top = 0
	}
	return layout.Inset{Top: top, Bottom: 8}.Layout(gtx, func(gtx C) D {
		return e.d.para(gtx, 13, 18, wBold, e.d.p.Sec, 0, title)
	})
}

// teamRow is ExternalOrganization (MediumTeamListEntity): the 36px team
// avatar, the bold name, "{count} conversations"; it opens the team page.
func (e *scExternal) teamRow(gtx C, r scTeamRow, first, last bool) D {
	d := e.d
	rc := dirClick(e.rows, r.team.ID)
	if rc.Clicked(gtx) {
		e.openTeam(r.team.ID)
	}
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	m := op.Record(gtx.Ops)
	dims := layout.Inset{Top: 12, Bottom: 12, Left: 16, Right: 16}.Layout(gtx, func(gtx C) D {
		return row(gtx, layout.Middle,
			layout.Rigid(func(gtx C) D { return scTeamAvatar(&d.base, gtx, r.team, 36) }),
			layout.Flexed(1, func(gtx C) D {
				return layout.Inset{Left: 12}.Layout(gtx, func(gtx C) D {
					return col(gtx,
						layout.Rigid(func(gtx C) D { return d.label(gtx, 15, wBold, d.p.Text, r.team.Name) }),
						layout.Rigid(func(gtx C) D {
							return d.label(gtx, 13, wReg, d.p.Muted, nsSC.T("{channelCount, plural, =1 {# conversation} other {# conversations}}", "channelCount", r.count))
						}),
					)
				})
			}),
		)
	})
	call := m.Stop()
	dirRowShape(&d.base, gtx, dims.Size, first, last)
	if rc.Hovered() { // p-external_organization:hover: foreground .01
		fill(gtx, image.Rectangle{Max: dims.Size}, argb(0x031d1c1d))
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

// scTeamAvatar is a team's square avatar (radius 6 at 36px), its initial
// on a gray tile when there's no icon.
func scTeamAvatar(b *base, gtx C, t core.ExternalTeam, size unit.Dp) D {
	s := gtx.Dp(size)
	sz := image.Pt(s, s)
	rad := gtx.Dp(size * 6 / 36)
	if t.Icon != "" && !t.IconDefault {
		return dirImage(b, gtx, t.Icon, sz, rad)
	}
	rrect(gtx, image.Rectangle{Max: sz}, rad, b.p.Track)
	init := ""
	for _, r := range t.Name {
		init = strings.ToUpper(string(r))
		break
	}
	g := gtx
	g.Constraints = layout.Exact(sz)
	layout.Center.Layout(g, func(gtx C) D { return b.label(gtx, unit.Sp(float32(size)*0.5), wBold, b.p.Sec, init) })
	return D{Size: sz}
}

// adminCTA is ExternalConnectionsCta (kAuK) i: the icon well, the bold
// title over its subtext, the new-window icon; it opens the admin page.
func (e *scExternal) adminCTA(gtx C, i int, hub core.SCHub) D {
	d := e.d
	c := &e.admin[i]
	type cta struct{ text, sub, icon, path string }
	ctas := [3]cta{
		{nsSCHub.T("Customize settings"), nsSCHub.T("Set permissions and view options for added security"), "settings", "manage/slack-connect/settings"},
		{nsSCHub.T("Manage requests"), nsSCHub.T("Review and approve connection requests from your team"), "paper-plane", "manage/slack-connect/requests-pending-invites"},
		{nsSCHub.T("Manage connections"), nsSCHub.T("Streamline approvals with trusted partners"), "workspace", "manage/slack-connect/connections"},
	}
	x := ctas[i]
	if c.Clicked(gtx) {
		if o, ok := d.h.(interface{ OpenURL(string) }); ok {
			o.OpenURL(scAdminURL(d.h.Core(), x.path))
		}
	}
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	m := op.Record(gtx.Ops)
	dims := layout.Inset{Top: 12, Bottom: 12, Left: 12, Right: 12}.Layout(gtx, func(gtx C) D {
		return row(gtx, layout.Middle,
			layout.Rigid(func(gtx C) D {
				s := gtx.Dp(36)
				rrect(gtx, image.Rect(0, 0, s, s), gtx.Dp(4), d.p.ReadFill)
				g := gtx
				g.Constraints = layout.Exact(image.Pt(s, s))
				layout.Center.Layout(g, func(gtx C) D { return d.icon(gtx, x.icon, 20, d.p.Text) })
				return D{Size: image.Pt(s, s)}
			}),
			layout.Flexed(1, func(gtx C) D {
				return layout.Inset{Left: 12, Right: 32}.Layout(gtx, func(gtx C) D {
					return col(gtx,
						layout.Rigid(func(gtx C) D { return d.label(gtx, 15, wBold, d.p.Text, x.text) }),
						layout.Rigid(func(gtx C) D { return d.label(gtx, 13, wReg, d.p.Muted, x.sub) }),
					)
				})
			}),
			layout.Rigid(func(gtx C) D { return d.icon(gtx, "external-link", 16, d.p.Muted) }),
		)
	})
	call := m.Stop()
	dirRowShape(&d.base, gtx, dims.Size, i == 0, i == 2)
	gg := gtx
	gg.Constraints = layout.Exact(dims.Size)
	c.Layout(gg, func(gtx C) D {
		pointerCursor(gtx)
		return D{Size: dims.Size}
	})
	call.Add(gtx.Ops)
	return dims
}

// scAdminURL is the org's admin site path ((0,h.Zl)(org) + path on a grid,
// the workspace's /admin otherwise).
func scAdminURL(c *core.App, path string) string {
	if c == nil {
		return ""
	}
	base := strings.TrimSuffix(c.C.Creds.EnterpriseURL, "/")
	if base == "" {
		base = strings.TrimSuffix(c.C.Creds.TeamURL, "/")
		if path == "manage/slack-connect/settings" {
			return base + "/admin/settings#permissions"
		}
	}
	return base + "/" + path
}

// ---- the hero banner ----

// banner is SlackConnectHeroBannerCta: the wrapper (horchata-0, light;
// #4cbf9d1a dark) centring a 960px column with 16px sides; the content row
// (headline 22/30 black, margin 8px 0; subtext 15/22 secondary; the 224x188
// picture with 30px 35px margins); the CTA cards (padding-bottom 32).
func (e *scExternal) banner(gtx C, hub core.SCHub, has bool) D {
	d := e.d
	w := gtx.Constraints.Max.X
	bg := rgb(0xfff6e6) // --dt_color-plt-horchata-0
	if d.th != nil && d.th.Dark {
		bg = flatten(argb(0x1a4cbf9d), d.p.Bg)
	}
	call, dims := e.recordBanner(gtx, hub, has, w)
	fill(gtx, image.Rect(0, 0, w, dims.Y), bg)
	call.Add(gtx.Ops)
	return D{Size: image.Pt(w, dims.Y)}
}

func (e *scExternal) recordBanner(gtx C, hub core.SCHub, has bool, w int) (op.CallOp, image.Point) {
	d := e.d
	m := op.Record(gtx.Ops)
	cw := min(w, gtx.Dp(960)+2*gtx.Dp(16))
	x := (w - cw) / 2
	off := op.Offset(image.Pt(x, 0)).Push(gtx.Ops) // popped before Stop: a stack op may not cross the macro
	g := gtx
	g.Constraints = layout.Constraints{Min: image.Pt(cw, 0), Max: image.Pt(cw, math.MaxInt32/2)}
	dd := layout.Inset{Left: 16, Right: 16}.Layout(g, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return col(gtx,
			layout.Rigid(func(gtx C) D {
				showImg := gtx.Constraints.Max.X+2*gtx.Dp(16) > gtx.Dp(500)
				return row(gtx, layout.Middle,
					layout.Flexed(1, func(gtx C) D {
						return col(gtx,
							layout.Rigid(func(gtx C) D {
								return layout.Inset{Top: 8, Bottom: 8}.Layout(gtx, func(gtx C) D {
									return d.para(gtx, 22, 30, wBlack, d.p.Text, 0, nsSCHub.T("Work with people outside {selfOrgOrTeamName} in Slack", "selfOrgOrTeamName", hub.TeamName))
								})
							}),
							layout.Rigid(func(gtx C) D {
								return d.para(gtx, 15, 22, wReg, d.p.Sec, 0, nsSCHub.T("Move your conversations out of siloed email threads and collaborate with external people, clients, vendors, and partners in Slack."))
							}),
						)
					}),
					layout.Rigid(func(gtx C) D {
						if !showImg {
							return D{}
						}
						return layout.Inset{Top: 30, Bottom: 30, Left: 35, Right: 35}.Layout(gtx, func(gtx C) D {
							return dirImage(&d.base, gtx, slackasset.SCHubHero, image.Pt(gtx.Dp(224), gtx.Dp(188)), 0)
						})
					}),
				)
			}),
			layout.Rigid(func(gtx C) D { return e.ctas(gtx, hub) }),
			layout.Rigid(func(gtx C) D {
				t := e.contactText(hub)
				if t == "" {
					return D{}
				}
				if e.contact.Clicked(gtx) {
					if o, ok := d.h.(interface{ OpenURL(string) }); ok {
						o.OpenURL("https://slack.com/help/articles/360035280511")
					}
				}
				return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					return layout.Center.Layout(gtx, func(gtx C) D { return d.linkButton(gtx, &e.contact, 15, t) })
				})
			}),
		)
	})
	if has { // the close button, only with connections
		func() {
			defer op.Offset(image.Pt(cw-gtx.Dp(16+36), gtx.Dp(16))).Push(gtx.Ops).Pop()
			d.iconButton(gtx, &e.bannerX, "close", 36, 20, d.p.Sec)
		}()
	}
	off.Pop()
	return m.Stop(), image.Pt(cw, dd.Size.Y)
}

// ctas is the two HeroBannerCTA cards (white .7, radius 12, min-height
// 130, padding 16; 24px apart): the channel one only when the viewer may
// create channels.
func (e *scExternal) ctas(gtx C, hub core.SCHub) D {
	d := e.d
	if e.ctaCh.Clicked(gtx) && hub.Invite.OK {
		if cc, ok := d.h.(interface{ OpenCreateChannel() }); ok {
			cc.OpenCreateChannel()
		}
	}
	type card struct {
		title, sub, btn   string
		primary, disabled bool
		tip               string
		c                 *widget.Clickable
		ts                *popups.TipState
	}
	var cards []card
	if hub.CreateChannel {
		tip := nsSCHub.T("You don’t have permission to create channels with people outside your company.")
		if len(hub.Invite.Reasons) == 1 && hub.Invite.Reasons[0] == core.SCReasonAllowedWorkspacePref {
			tip = nsSCHub.T("Admins don’t allow external people to join channels in this workspace.")
		}
		btn := nsSCHub.T("Create Channel")
		cards = append(cards, card{nsSCHub.T("Create a channel with external people"),
			nsSCHub.T("Work with multiple people and organizations outside of {selfOrgOrTeamName}", "selfOrgOrTeamName", hub.TeamName),
			btn, true, !hub.Invite.OK, tip, &e.ctaCh, &e.tipCh})
	}
	cards = append(cards, card{nsSCHub.T("Talk one-on-one"), nsSCHub.T("Talk one-on-one with anyone outside of {selfOrgOrTeamName}", "selfOrgOrTeamName", hub.TeamName),
		nsSCHub.T("Start a DM"), false, !hub.DMEligible, nsSCHub.T("You don’t have permission to start a DM with people outside your company."), &e.ctaDM, &e.tipDM})
	ch := make([]layout.FlexChild, 0, 2*len(cards))
	for i, k := range cards {
		if i > 0 {
			ch = append(ch, hgap(24))
		}
		ch = append(ch, layout.Flexed(1, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			m := op.Record(gtx.Ops)
			dims := layout.UniformInset(16).Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return col(gtx,
					layout.Rigid(func(gtx C) D {
						return layout.Inset{Bottom: 4}.Layout(gtx, func(gtx C) D { return d.para(gtx, 15, 22, wBold, d.p.Text, 0, k.title) })
					}),
					layout.Rigid(func(gtx C) D {
						return layout.Inset{Bottom: 16}.Layout(gtx, func(gtx C) D { return d.para(gtx, 13, 18, wReg, d.p.Faint, 0, k.sub) })
					}),
					layout.Rigid(func(gtx C) D {
						kind := btnOutline
						if k.primary {
							kind = btnPrimary
						}
						bd := scButton(&d.base, gtx, k.c, kind, k.disabled, k.btn)
						if k.disabled && k.tip != "" {
							popups.Tooltip(gtx, d.th, k.ts, k.c.Hovered(), bd.Size, popups.Above, k.tip, "")
						}
						return bd
					}),
				)
			})
			call := m.Stop()
			h := max(dims.Size.Y, gtx.Dp(130))
			bg := argb(0xb3ffffff)
			if d.th != nil && d.th.Dark {
				bg = theme.WithAlpha(d.p.Surface, 0xb3)
			}
			rrect(gtx, image.Rect(0, 0, dims.Size.X, h), gtx.Dp(12), bg)
			call.Add(gtx.Ops)
			return D{Size: image.Pt(dims.Size.X, h)}
		}))
	}
	return layout.Inset{Bottom: 32}.Layout(gtx, func(gtx C) D { return row(gtx, layout.Start, ch...) })
}

// scButton is a medium c-button, drawn disabled (base-ter fill, content-ter
// text, no pointer) when off; its clicks still report hover for the tip.
func scButton(b *base, gtx C, c *widget.Clickable, kind btnKind, disabled bool, label string) D {
	if !disabled {
		return b.button(gtx, c, kind, false, label)
	}
	return c.Layout(gtx, func(gtx C) D {
		tw := b.th.Measure(gtx, 15, wBold, label)
		sz := image.Pt(tw+2*gtx.Dp(16), gtx.Dp(36))
		rrect(gtx, image.Rectangle{Max: sz}, gtx.Dp(8), b.p.Track)
		g := gtx
		g.Constraints = layout.Exact(sz)
		layout.Center.Layout(g, func(gtx C) D { return b.label(gtx, 15, wBold, b.p.Faint, label) })
		return D{Size: sz}
	})
}

// ---- the tutorial ----

// tutorial is SlackConnectInstructionSection: the header (18/24 black),
// the three numbered steps (22px circles in content-hgl-1 joined by dashed
// lines, 76% wide) over their texts (12/18 bold header, 12/18 text).
func (e *scExternal) tutorial(gtx C, team string) D {
	d := e.d
	steps := [3][2]string{
		{nsSCHub.T("Create a channel"), nsSCHub.T("Invite people from outside {selfOrgOrTeamName} to the channel using their email address.", "selfOrgOrTeamName", team)},
		{nsSCHub.T("Partners accept your invite"), nsSCHub.T("Upon accepting your invite, they’ll set up the shared channel in their own workspace.")},
		{nsSCHub.T("Work securely and efficiently"), nsSCHub.T("Your invitees will only have access to the conversations they’re invited to.")},
	}
	w := gtx.Constraints.Max.X
	return layout.Inset{Top: 32, Bottom: 32}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = w
		return layout.Inset{Top: 20, Bottom: 20, Left: 40, Right: 40}.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Top: 28}.Layout(gtx, func(gtx C) D {
						return d.label(gtx, 18, wBlack, d.p.Text, nsSCHub.T("How to create channels with external people"))
					})
				}),
				layout.Rigid(func(gtx C) D { return D{Size: image.Pt(0, gtx.Dp(32))} }),
				layout.Rigid(func(gtx C) D {
					cw := min(gtx.Constraints.Max.X, gtx.Dp(684))
					gtx.Constraints = layout.Exact(image.Pt(cw, gtx.Constraints.Max.Y))
					gtx.Constraints.Min.Y = 0
					return layout.Inset{Bottom: 28}.Layout(gtx, func(gtx C) D {
						return col(gtx,
							layout.Rigid(func(gtx C) D { return e.stepNumbers(gtx, cw) }),
							layout.Rigid(func(gtx C) D {
								return layout.Inset{Top: 16}.Layout(gtx, func(gtx C) D {
									ch := []layout.FlexChild{}
									for i, s := range steps {
										if i > 0 {
											ch = append(ch, hgap(24))
										}
										ch = append(ch, layout.Flexed(1, func(gtx C) D {
											gtx.Constraints.Min.X = gtx.Constraints.Max.X
											return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
												layout.Rigid(func(gtx C) D {
													return layout.Inset{Bottom: 4}.Layout(gtx, func(gtx C) D { return d.centered(gtx, 12, wBold, d.p.Text, s[0]) })
												}),
												layout.Rigid(func(gtx C) D {
													return d.th.Layout(gtx, theme.TextStyle{Size: 12, LineHeight: 18, Color: d.p.Muted, Align: text.Middle}, s[1])
												}),
											)
										}))
									}
									return row(gtx, layout.Start, ch...)
								})
							}),
						)
					})
				}),
			)
		})
	})
}

// stepNumbers is the number row: 76% of the column, shifted 8px right,
// three 22px circles with 8px margins and dashed lines between.
func (e *scExternal) stepNumbers(gtx C, cw int) D {
	d := e.d
	rw := cw * 76 / 100
	x0 := (cw-rw)/2 + gtx.Dp(8)
	s := gtx.Dp(22)
	h := s
	slot := rw / 3
	for i := 0; i < 3; i++ {
		cx := x0 + slot*i + slot/2
		r := image.Rect(cx-s/2, 0, cx+s/2, s)
		theme.Circle(gtx, image.Pt(cx, s/2), s, d.p.Link)
		func() {
			defer op.Offset(r.Min).Push(gtx.Ops).Pop()
			g := gtx
			g.Constraints = layout.Exact(r.Size())
			layout.Center.Layout(g, func(gtx C) D { return d.label(gtx, 12, wReg, rgb(0xffffff), string(rune('1'+i))) })
		}()
		if i < 2 { // the dashed line to the next circle
			a, b := cx+s/2+gtx.Dp(8), cx+slot-s/2-gtx.Dp(8)
			dash, gap := gtx.Dp(3), gtx.Dp(3)
			for x := a; x < b; x += dash + gap {
				fill(gtx, image.Rect(x, s/2, min(x+dash, b), s/2+max(1, gtx.Dp(1))), d.p.Faint)
			}
		}
	}
	return D{Size: image.Pt(cw, h)}
}

// ---- the social proof ----

var scQuotes = [3]scQuote{
	{"Talking to a client by connecting in a channel was as easy <b>as talking to someone internally,</b> without having to jump workspaces.",
		"Co-founder and CEO, Spark 64", "Ming Cheuk", slackasset.SCPersonMing},
	{"Clients are much more responsive in our channel with them. That’s a win for us because it <b>increases our efficiency</b> in getting the job done.",
		"Managing partner, IQ Accountants", "Kyelie Baxter", slackasset.SCPersonKyelie},
	{"Having an external Slack channel with our prospects and customers really <b>helps us stand out</b> and communicate that our customers are our <div>No. 1 priority.</div>",
		"Corporate Account Executive, Snowflake", "Michael Westra", slackasset.SCPersonMichael},
}

// socialProof is SlackConnectSocialProof: the wrapper (white .7, padding
// 48px 24px 80px), the QuoteCarousel and the LogoFarm ("Join the 100,000+
// organizations working in Slack Connect", four 80px logos).
func (e *scExternal) socialProof(gtx C) D {
	d := e.d
	w := gtx.Constraints.Max.X
	m := op.Record(gtx.Ops)
	dims := layout.Inset{Top: 48, Bottom: 80, Left: 24, Right: 24}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D { return e.carousel(gtx) }),
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Top: 48}.Layout(gtx, func(gtx C) D {
					return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx C) D {
							return d.centered(gtx, 18, wBlack, d.p.Text, i18n.StripTags(nsSC.T("Join the 100,000+ organizations working in Slack Connect")))
						}),
						layout.Rigid(func(gtx C) D {
							return layout.Inset{Top: 32}.Layout(gtx, func(gtx C) D {
								lw := min(gtx.Constraints.Max.X, gtx.Dp(904))
								gtx.Constraints.Min.X, gtx.Constraints.Max.X = lw, lw
								openAI := slackasset.SCLogoOpenAI
								if d.th != nil && d.th.Dark {
									openAI = slackasset.SCLogoOpenAIDark
								}
								logos := []struct {
									url  string
									w, h int
								}{{slackasset.SCLogoAirbnb, 375, 188}, {openAI, 2200, 598}, {slackasset.SCLogoTime, 931, 289}, {slackasset.SCLogoHulu, 3125, 1027}}
								ch := []layout.FlexChild{}
								for _, l := range logos {
									ch = append(ch, layout.Flexed(1, func(gtx C) D {
										gtx.Constraints.Min.X = gtx.Constraints.Max.X
										return layout.Center.Layout(gtx, func(gtx C) D {
											iw := gtx.Dp(80)
											return dirImage(&d.base, gtx, l.url, image.Pt(iw, iw*l.h/l.w), 0)
										})
									}))
								}
								return row(gtx, layout.Middle, ch...)
							})
						}),
					)
				})
			}),
		)
	})
	call := m.Stop()
	bg := argb(0xb3ffffff)
	if d.th != nil && d.th.Dark {
		bg = theme.WithAlpha(d.p.Surface, 0xb3)
	}
	fill(gtx, image.Rect(0, 0, w, dims.Size.Y), bg)
	call.Add(gtx.Ops)
	return D{Size: image.Pt(w, dims.Size.Y)}
}

// carousel is QuoteCarousel: ‹ (hidden on the first), the quote wrapper
// (foreground-min .04, radius 16, padding 24px 16px; the quotes 600px
// wide, sliding 1s ease-in-out), › (hidden on the last), the progress dots
// (8px, 12px apart, 24px under). It moves on every 7 s until the last.
func (e *scExternal) carousel(gtx C) D {
	d := e.d
	now := gtx.Now
	if e.shownAt.IsZero() {
		e.shownAt = now
	}
	if e.quote < len(scQuotes)-1 {
		next := e.shownAt.Add(7 * time.Second)
		if !now.Before(next) {
			e.goQuote(e.quote+1, now)
		} else {
			e.qwk.schedule(d.h, now, next)
		}
	}
	if e.prevQ.Clicked(gtx) && e.quote > 0 {
		e.goQuote(e.quote-1, now)
	}
	if e.nextQ.Clicked(gtx) && e.quote < len(scQuotes)-1 {
		e.goQuote(e.quote+1, now)
	}
	for i := range e.dots {
		if e.dots[i].Clicked(gtx) {
			e.goQuote(i, now)
		}
	}
	arrow := func(c *widget.Clickable, icon string, hidden bool) layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			s := gtx.Dp(36)
			if hidden { // visibility: hidden keeps its room
				return D{Size: image.Pt(s, s)}
			}
			return d.iconButton(gtx, c, icon, 36, 20, d.p.Sec)
		})
	}
	return row(gtx, layout.Middle,
		arrow(&e.prevQ, "caret-left-full", e.quote == 0),
		hgap(8),
		layout.Flexed(1, func(gtx C) D {
			maxW := min(gtx.Constraints.Max.X, gtx.Dp(1000))
			gtx.Constraints.Max.X, gtx.Constraints.Min.X = maxW, maxW
			m := op.Record(gtx.Ops)
			dims := layout.Inset{Top: 24, Bottom: 24, Left: 16, Right: 16}.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D { return e.quotes(gtx) }),
					layout.Rigid(func(gtx C) D {
						return layout.Inset{Top: 24}.Layout(gtx, func(gtx C) D {
							ch := []layout.FlexChild{}
							for i := range scQuotes {
								if i > 0 {
									ch = append(ch, hgap(12))
								}
								ch = append(ch, layout.Rigid(func(gtx C) D {
									return e.dots[i].Layout(gtx, func(gtx C) D {
										pointerCursor(gtx)
										s := gtx.Dp(8)
										c := argb(0x334a154b)
										if i == e.quote {
											c = rgb(0x4a154b)
										}
										if d.th != nil && d.th.Dark {
											c = argb(0x4defe1f5)
											if i == e.quote {
												c = rgb(0xefe1f5)
											}
										}
										rrect(gtx, image.Rect(0, 0, s, s), gtx.Dp(4), c)
										return D{Size: image.Pt(s, s)}
									})
								}))
							}
							return row(gtx, layout.Middle, ch...)
						})
					}),
				)
			})
			call := m.Stop()
			rrect(gtx, image.Rectangle{Max: dims.Size}, gtx.Dp(16), theme4b(&d.base, 0x0a))
			call.Add(gtx.Ops)
			return dims
		}),
		hgap(8),
		arrow(&e.nextQ, "caret-right-full", e.quote == len(scQuotes)-1),
	)
}

func (e *scExternal) goQuote(i int, now time.Time) {
	if i == e.quote {
		return
	}
	e.slideFrom, e.quote, e.slideAt, e.shownAt = e.quote, i, now, now
}

// quotes is the sliding row: each quote is the 90px-high portrait and the
// quote (15/22, margin-left 18) over the name (bold) and designation
// (12/18 tertiary, margin-top 8), a full container width apart.
func (e *scExternal) quotes(gtx C) D {
	d := e.d
	cw := min(gtx.Constraints.Max.X, gtx.Dp(600))
	if gtx.Constraints.Max.X < gtx.Dp(700) {
		cw = min(gtx.Constraints.Max.X, gtx.Dp(500))
	}
	if gtx.Constraints.Max.X < gtx.Dp(500) {
		cw = min(gtx.Constraints.Max.X, gtx.Dp(350))
	}
	pos := float32(e.quote)
	if !e.slideAt.IsZero() {
		t := float32(gtx.Now.Sub(e.slideAt)) / float32(time.Second)
		if t < 1 {
			pos = float32(e.slideFrom) + (float32(e.quote)-float32(e.slideFrom))*easeInOut(t)
			d.h.Invalidate() // the slide's next frame, asked of the host
		}
	}
	one := func(gtx C, q scQuote) D {
		gtx.Constraints = layout.Constraints{Min: image.Pt(cw, 0), Max: image.Pt(cw, gtx.Constraints.Max.Y)}
		return row(gtx, layout.Start,
			layout.Rigid(func(gtx C) D { s := gtx.Dp(90); return dirImage(&d.base, gtx, q.image, image.Pt(s, s), 0) }),
			layout.Flexed(1, func(gtx C) D {
				return layout.Inset{Left: 18}.Layout(gtx, func(gtx C) D {
					return col(gtx,
						layout.Rigid(func(gtx C) D { return d.para(gtx, 15, 22, wReg, d.p.Text, 0, i18n.StripTags(nsSCHub.T(q.quote))) }),
						layout.Rigid(func(gtx C) D {
							return layout.Inset{Top: 8}.Layout(gtx, func(gtx C) D {
								return row(gtx, layout.Baseline,
									layout.Rigid(func(gtx C) D { return d.label(gtx, 12, wBold, d.p.Faint, nsSCHub.T(q.name)) }),
									layout.Rigid(func(gtx C) D { return d.label(gtx, 12, wReg, d.p.Faint, ", "+nsSCHub.T(q.designation)) }),
								)
							})
						}),
					)
				})
			}),
		)
	}
	var h int
	m := op.Record(gtx.Ops)
	for i, q := range scQuotes {
		off := int(math.Round(float64((float32(i) - pos) * float32(cw))))
		func() {
			defer op.Offset(image.Pt(off, 0)).Push(gtx.Ops).Pop()
			h = max(h, one(gtx, q).Size.Y)
		}()
	}
	call := m.Stop()
	defer clip.Rect{Max: image.Pt(cw, h)}.Push(gtx.Ops).Pop() // overflow: hidden
	call.Add(gtx.Ops)
	return D{Size: image.Pt(cw, h)}
}

// easeInOut is CSS ease-in-out (cubic-bezier(.42,0,.58,1)).
func easeInOut(t float32) float32 {
	if t <= 0 {
		return 0
	}
	if t >= 1 {
		return 1
	}
	// solve x(s) = t for the bezier's s, then y(s)
	s := t
	for i := 0; i < 8; i++ {
		x := 3*(1-s)*(1-s)*s*0.42 + 3*(1-s)*s*s*0.58 + s*s*s
		dx := 3*(1-s)*(1-s)*0.42 + 6*(1-s)*s*(0.58-0.42) + 3*s*s*(1-0.58)
		if dx == 0 {
			break
		}
		s -= (x - t) / dx
	}
	return 3*(1-s)*s*s + s*s*s
}

// theme4b is foreground-min at alpha a over the page.
func theme4b(b *base, a uint8) color.NRGBA {
	c := b.p.Text
	c.A = a
	return c
}
