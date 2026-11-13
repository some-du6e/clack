package views

// Channel details under agents_in_channels_improvements (client-boot M/qt
// Io, on for the test account since 2026-11-13), from Slack's web client
// (CDN build 859cadc0e0ab, 2026-11-13). Off (or unassigned) keeps the
// tabs as details.go draws them.
//
//   - Members (ia-details-popover-members chunk +VDd
//     IaDetailsPopoverMembers, e5 = Io && !e7 && !e9): the roster is
//     people only (with agent_member_channel_count, as before) and its
//     searches leave bots and agents out (includeBots / includeAgents
//     false); the add row is "Add people" (tC) and the search "Find
//     people" ("Person name"). Its MembersFilter (KcBz
//     useAccountTypesFilter) is All, Channel Managers, Members, Guests
//     and, under frontline_users, Frontline, each with its member count
//     in the menu (none for Guests under frontline_users, nor for
//     Frontline); Io drops "Agents & apps" (offered only while searching),
//     and a free team without external members gets no filter. The
//     filters keep the loaded people that Slack's filter queries keep
//     (getInternalWorkspaceMemberFilter: Members "people NOT guests AND
//     NOT external" without managers or frontline workers, Guests "people
//     AND NOT org_members AND NOT external AND NOT frontline", Frontline
//     "frontline"), and an empty result is renderEntirelyEmptyState's
//     title (nN).
//   - Agents & apps (agents-apps-tab chunk Vmvo): AgentsAppsTab renders
//     ReworkedAgentsAppsTab (reworkedTab: a column filling the tab). Its
//     roster is IaDetailsPopoverMembers scoped to agents and apps (e7):
//     "Find agents and apps" ("Agent or app name") and no filter, the
//     "Add agents and apps" row for a viewer who can add apps (IKZu B:
//     under Io, XMW3 canUserInviteInternalUsersToChannel), then the
//     channel's agents and apps (fetchAndUpsertMembersForChannel filter
//     "apps", 50 a page; core.ChannelAgentsAppsPage) as MediumMemberEntity
//     rows (36px avatar, the bold name with its APP/AGENT badge, the
//     title, the hover card on the name). Searching lists the matches in
//     the channel under "Agents & apps in this channel" and the agents and
//     apps outside it (the member search with includeBots/includeAgents,
//     people dropped) under "Not in this channel", each with "Add to
//     Channel" (calculateListKeys). With no agents or apps
//     (membershipCounts app_count / agent_count 0) the roster sizes to its
//     rows (p-ia_details_popover__members--compact: up to 4 rows of 60px)
//     and, while nothing is typed, AppsAgentsSection's call to action
//     follows (+xuY ctaOnly: a CtaField titled "Agents and apps", "Get more
//     done in this channel by adding your team’s AI agents and apps",
//     "Browse Agents and Apps" for a viewer who can add apps, which opens
//     the apps browser, and "Learn More"; the agents-and-apps illustration
//     on indigo-5 at the right).
//   - The channel invite modal (boot-shared ANc2 AddToChannelInitialScreen
//     with entityScope AgentsApps): "Add agents and apps to {channelName}"
//     over the channel's name, the multi-select (ChannelInviteFormSearchMultiSelect: "Agents and apps",
//     "Search agents and apps", agents and apps only, no emails or user
//     groups, those in the channel "Already in this channel", "No agents
//     or apps found matching {query}"), "Explore more agents and apps"
//     (the apps browser), no bulk add controls, and the footer's Add
//     (disabled until something is picked). Add runs conversations.invite.
//
// Not ported: ZiB8's agents with access (canSeeAgentsWithAccess needs
// agent_mention_dm_user_token, which the account isn't assigned);
// yWXb's empty-query Suggestions (agents.sunroof.get's pinned agents and
// flannel's empty-query results, neither of which Clack loads); the
// rows' "…" menu (Edit agent settings, Remove from channel); the native
// channel expert and Slackbot rows; the Members search's out-of-channel
// results (in both groups); renderEntirelyEmptyState's Slack Connect
// description; EtJj's private-channel history prompt; and
// ChannelManagerRestrictedInvitesAlert ("You can only add agents and apps
// from {org} to this channel."), shown when the viewer can't invite
// external members (jBl0 canUserInviteExternalMembersToChannel: y590's
// Slack Connect creation checks, which Clack doesn't load).

import (
	"context"
	"image"
	"slices"
	"strconv"
	"strings"
	"time"

	"gioui.org/io/key"
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

var (
	// nsChannelInvite is the channel invite modal's I18n namespace (tN,
	// tF, as, al and yWXb's Z).
	nsChannelInvite = i18n.NS("channel_invite")
	// nsChannelTabs is AppsAgentsSection's (+xuY et).
	nsChannelTabs = i18n.NS("channel_tabs")
)

const (
	// cdAgentsHelpURL is AppsAgentsSection's Learn More (articleId
	// 0x53d1c385bb).
	cdAgentsHelpURL = "https://slack.com/help/articles/360001537467"
	// cdAgentsCTAImage is the CtaField's side image (+xuY PC7T, the 2x
	// agents-and-apps illustration).
	cdAgentsCTAImage = "https://a.slack-edge.com/bv1-13/agents-and-apps-2x-b1d3496.png"
	// cdRosterRowH is p-ia_details_popover__members_list_item's height.
	cdRosterRowH = 60
)

// Members filter values (yPoO m), in KcBz's order.
const (
	cdFiltEveryone = iota
	cdFiltManagers
	cdFiltMembers
	cdFiltGuests
	cdFiltFrontline
)

// cdAgents is the state behind the Io parts.
type cdAgents struct {
	// Members: KcBz's filter, rebuilt when its options change
	filt     dirDropdown
	filtKind []int
	filtKey  string
	kind     int // the filter drawn this frame

	// Agents & apps: the roster
	roster  async[cdMembersPage]
	find    *search
	deb     debounce
	out     async[[]core.User] // the member search outside the channel
	outQ    string
	added   []core.User // added from "Not in this channel" (eT)
	adding  string
	addOp   async[bool]
	addRow  widget.Clickable
	browse  widget.Clickable
	learn   widget.Clickable
	addBtns map[string]*widget.Clickable
	list    widget.List
	appsN   int // the app count the roster was loaded for
}

func (d *ChannelDetails) ag() *cdAgents {
	if d.agents == nil {
		d.agents = &cdAgents{find: newSearchIn(nsChannelDetails, "Find agents and apps"), addBtns: map[string]*widget.Clickable{}}
		d.agents.list.Axis = layout.Vertical
	}
	return d.agents
}

// agentsImproved is M/qt Io for this conversation's details.
func (d *ChannelDetails) agentsImproved() bool {
	c := d.h.Core()
	return c != nil && cdGroup(d.kind) != "im" && c.AgentsInChannelsImprovements()
}

// ---- Members (e5) ----

// membersAddLabel is tC for the Members roster.
func (d *ChannelDetails) membersAddLabel() string {
	if d.agentsImproved() {
		return nsChannelDetails.T("Add people")
	}
	if cdGroup(d.kind) != "channel" {
		return "Add people" // a group DM takes no agents
	}
	return "Add people or agents"
}

// membersSearch sets the Members search's placeholder: "Find people"
// under Io (e1), else Clack's "Find people or agents".
func (d *ChannelDetails) membersSearch() {
	if d.agentsImproved() {
		d.find.ph, d.find.ns = "Find people", nsChannelDetails
	} else {
		d.find.ph, d.find.ns = "Find people or agents", ""
	}
}

// membersFilter is KcBz's MembersFilter under Io: its options (rebuilt
// when the counts change) and the selected filter. ok is false when the
// filter has no options (a free team without external members).
func (d *ChannelDetails) membersFilter() (dd *dirDropdown, kind int, ok bool) {
	ag := d.ag()
	c := d.h.Core()
	mpdm := cdGroup(d.kind) == "mpim"
	if c.OnFreePlan() {
		return &ag.filt, cdFiltEveryone, false
	}
	counts, _, _ := d.counts.get()
	managers := len(d.managerIDs())
	frontline := c.Experiment("frontline_users") == "on"
	all := counts.People
	members := max(0, all-managers-counts.Guests)
	kinds := []int{cdFiltEveryone}
	if !mpdm {
		kinds = append(kinds, cdFiltManagers)
	}
	kinds = append(kinds, cdFiltMembers, cdFiltGuests)
	if frontline {
		kinds = append(kinds, cdFiltFrontline)
	}
	everyone := "Everyone"
	if c.Experiment("agent_member_channel_count") == "on" { // lx
		everyone = "All"
	}
	labels := make([]string, len(kinds))
	nums := make([]string, len(kinds))
	for i, k := range kinds {
		switch k {
		case cdFiltEveryone:
			labels[i], nums[i] = nsChannelDetails.T(everyone), cdFilterCount(all)
		case cdFiltManagers:
			labels[i], nums[i] = nsChannelDetails.T("Channel Managers"), cdFilterCount(managers)
		case cdFiltMembers:
			labels[i], nums[i] = nsChannelDetails.T("Members"), cdFilterCount(members)
		case cdFiltGuests:
			labels[i] = nsChannelDetails.T("Guests")
			if !frontline {
				nums[i] = cdFilterCount(counts.Guests)
			}
		case cdFiltFrontline:
			labels[i] = nsChannelDetails.T("Frontline")
		}
	}
	key := strings.Join(labels, "\x00") + "\x00" + strings.Join(nums, "\x00")
	if key != ag.filtKey {
		sel := 0
		if ag.filt.sel < len(ag.filtKind) {
			if i := slices.Index(kinds, ag.filtKind[ag.filt.sel]); i >= 0 {
				sel = i
			}
		}
		ag.filt.labels, ag.filt.counts, ag.filt.sel = labels, nums, sel
		ag.filt.items = nil
		ag.filtKind, ag.filtKey = kinds, key
	}
	ag.filt.applied = ag.filt.sel != 0
	return &ag.filt, ag.filtKind[min(ag.filt.sel, len(ag.filtKind)-1)], true
}

// cdFilterCount is KcBz formatMemberCount: "12.3k" from 10,000, else the
// number with separators.
func cdFilterCount(n int) string {
	if n >= 10000 {
		v := float64(n) / 1000
		s := strconv.FormatFloat(v, 'f', max(0, 3-len(strconv.Itoa(n/1000))), 64)
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
		return s + "k"
	}
	return humanCount(n)
}

// filteredImproved is the Members roster's people under the KcBz filter
// kind and the search q (name, real name, handle or title).
func (d *ChannelDetails) filteredImproved(us []core.User, q string, kind int) []core.User {
	if q == "" && kind == cdFiltEveryone {
		return us
	}
	k := "io\x00" + q + "\x00" + strconv.Itoa(kind) + "\x00" + humanCount(len(us)) + "\x00" + strconv.Itoa(len(d.managerIDs()))
	if k == d.filtKey && d.filt != nil {
		return d.filt
	}
	managers := d.managerIDs()
	lq := strings.ToLower(q)
	out := make([]core.User, 0, 16)
	for _, u := range us {
		mgr := slices.Contains(managers, u.ID)
		switch kind {
		case cdFiltManagers:
			if !mgr {
				continue
			}
		case cdFiltMembers:
			if u.Guest || u.Frontline || u.External || mgr {
				continue
			}
		case cdFiltGuests:
			if !u.Guest || u.Frontline || u.External {
				continue
			}
		case cdFiltFrontline:
			if !u.Frontline {
				continue
			}
		}
		if lq != "" && !cdUserMatches(u, lq) {
			continue
		}
		out = append(out, u)
	}
	d.filtKey, d.filt = k, out
	return out
}

func cdUserMatches(u core.User, lq string) bool {
	return strings.Contains(strings.ToLower(u.Name), lq) || strings.Contains(strings.ToLower(u.RealName), lq) ||
		strings.Contains(strings.ToLower(u.Handle), lq) || strings.Contains(strings.ToLower(u.Title), lq)
}

// membersEmptyImproved is renderEntirelyEmptyState's nN for the filter.
func (d *ChannelDetails) membersEmptyImproved(gtx C, q string, kind int) D {
	return layout.Inset{Top: 24, Bottom: 24, Left: cdPadX, Right: cdPadX}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		switch {
		case q != "":
			return d.cdRichCentered(gtx, "members-empty", nsChannelDetails.RT("No matches found for <strong>{searchTerm}</strong>", "searchTerm", q), "searchTerm")
		case kind == cdFiltManagers:
			return d.centered(gtx, 15, wBold, d.p.Text, nsChannelDetails.T("No Channel Managers found"))
		case kind == cdFiltMembers:
			return d.centered(gtx, 15, wBold, d.p.Text, nsChannelDetails.T("No Members found"))
		case kind == cdFiltGuests:
			return d.centered(gtx, 15, wBold, d.p.Text, nsChannelDetails.T("No Guests found"))
		}
		return d.cdRichCentered(gtx, "members-empty", nsChannelDetails.RT("<strong>No matches found</strong>"))
	})
}

// cdRichCentered draws rt() parts centred: <strong> and the values of
// bold (placeholders Slack fills with a <strong> element) bold, the rest
// regular, 15px.
func (d *ChannelDetails) cdRichCentered(gtx C, key string, parts []i18n.Part, bold ...string) D {
	ch := make([]layout.FlexChild, 0, len(parts))
	for _, p := range parts {
		if p.Text == "" {
			continue
		}
		w := wReg
		if t := strings.ToLower(p.Tag); t == "strong" || t == "b" || (p.Arg != "" && slices.Contains(bold, p.Arg)) {
			w = wBold
		}
		text, wt := p.Text, w
		ch = append(ch, layout.Rigid(func(gtx C) D { return d.label(gtx, 15, wt, d.p.Text, text) }))
	}
	return layout.Center.Layout(gtx, func(gtx C) D { return row(gtx, layout.Baseline, ch...) })
}

// ---- Agents & apps (ReworkedAgentsAppsTab) ----

// canAddAgentsApps is IKZu B under Io: a channel the viewer is in, not
// archived, not a guest, where they can add internal members (XMW3).
func (d *ChannelDetails) canAddAgentsApps(cv core.Conv) bool {
	c := d.h.Core()
	if c == nil || cdGroup(d.kind) != "channel" || cv.IsArchived || c.ViewerRestricted() {
		return false
	}
	inf, _, _ := d.info.get()
	return cv.Member || inf.IsMember
}

func (d *ChannelDetails) loadAgentRoster(marker string) {
	ag := d.ag()
	var prev []core.User
	if marker != "" {
		cur, _, _ := ag.roster.get()
		prev = append([]core.User(nil), cur.users...)
	}
	c, id := d.h.Core(), d.id
	ag.roster.load(d.h, true, func(ctx context.Context) (cdMembersPage, error) {
		us, next, err := c.ChannelAgentsAppsPage(ctx, id, marker)
		if err != nil {
			return cdMembersPage{}, err
		}
		return cdMembersPage{users: append(prev, us...), next: next}, nil
	})
}

// agentsAppsCount is membershipCounts' app_count + agent_count (552O E6,
// qg), 0 until the counts load.
func (d *ChannelDetails) agentsAppsCount() int {
	if mc, ok, err := d.counts.get(); ok && err == nil {
		return mc.Apps
	}
	if n, ok := d.h.Core().AgentAppCount(d.id); ok {
		return n
	}
	return 0
}

// cdIsAgentOrApp is Wv58's agent-and-app side of the roster partition.
func cdIsAgentOrApp(u core.User) bool {
	return u.ID == core.SlackbotUserID || u.IsBot || u.Agent || u.Agentforce || u.AppUser
}

// updateAgentsTab runs the roster's clicks, search and adds.
func (d *ChannelDetails) updateAgentsTab(gtx C, cv core.Conv) {
	ag := d.ag()
	c, id := d.h.Core(), d.id
	if !ag.roster.started() {
		d.loadAgentRoster("")
	}
	// the count grew (an add elsewhere): refetch the first page
	if n := d.agentsAppsCount(); n > ag.appsN && ag.appsN > 0 && !ag.roster.busy() {
		d.loadAgentRoster("")
	}
	ag.appsN = d.agentsAppsCount()
	q, changed, _ := ag.find.update(gtx)
	if changed {
		ag.deb.set(q, gtx.Now)
	}
	if dq, due := ag.deb.due(gtx, 100*time.Millisecond); due && dq != "" && d.canAddAgentsApps(cv) {
		ag.outQ = dq
		ag.out.load(d.h, true, func(ctx context.Context) ([]core.User, error) { return c.SearchUsers(ctx, dq, 100) })
	}
	if q == "" {
		ag.outQ = ""
	}
	if ag.addRow.Clicked(gtx) {
		d.openAgentInvite()
	}
	if ag.browse.Clicked(gtx) {
		if ph, ok := d.h.(popups.Host); ok {
			popups.OpenAppsBrowser(ph, id)
		}
	}
	if ag.learn.Clicked(gtx) {
		hostHandler{d.h}.OpenURL(cdAgentsHelpURL)
	}
	for uid, b := range ag.addBtns {
		if b.Clicked(gtx) && ag.adding == "" {
			ag.adding = uid
			ag.addOp.load(d.h, false, func(ctx context.Context) (bool, error) { return true, c.Invite(ctx, id, uid) })
		}
	}
	if ag.adding != "" {
		if _, done, err := ag.addOp.get(); done {
			uid := ag.adding
			ag.adding = ""
			if err != nil {
				d.h.Toast(cdErr(err))
			} else if u, ok := c.User(uid); ok {
				ag.added = append([]core.User{u}, ag.added...) // a9: into the roster, out of the results
			}
			d.counts.load(d.h, true, func(ctx context.Context) (core.MemberCounts, error) { return c.MemberCounts(ctx, id) })
		}
	}
}

// cdRosterKey is one row of the reworked roster.
type cdRosterKey struct {
	kind int // 0 member, 1 add row, 2 in-channel heading, 3 out heading, 4 "No matches", 5 out-of-channel
	u    core.User
}

// agentsRosterKeys is calculateListKeys for e7.
func (d *ChannelDetails) agentsRosterKeys(cv core.Conv) (keys []cdRosterKey, searching bool) {
	ag := d.ag()
	rp, _, _ := ag.roster.get()
	in := make([]core.User, 0, len(ag.added)+len(rp.users))
	seen := map[string]bool{}
	for _, u := range append(append([]core.User(nil), ag.added...), rp.users...) {
		if !seen[u.ID] && cdIsAgentOrApp(u) {
			seen[u.ID] = true
			in = append(in, u)
		}
	}
	canAdd := d.canAddAgentsApps(cv)
	q := ag.find.text()
	if q == "" {
		if canAdd {
			keys = append(keys, cdRosterKey{kind: 1})
		}
		for _, u := range in {
			keys = append(keys, cdRosterKey{u: u})
		}
		return keys, false
	}
	lq := strings.ToLower(q)
	var matches, out []cdRosterKey
	for _, u := range in {
		if cdUserMatches(u, lq) {
			matches = append(matches, cdRosterKey{u: u})
		}
	}
	if res, ok, err := ag.out.get(); ok && err == nil && ag.outQ != "" {
		for _, u := range res {
			if seen[u.ID] || !cdIsAgentOrApp(u) || u.Deleted || u.ID == core.SlackbotUserID {
				continue // a_: people dropped
			}
			out = append(out, cdRosterKey{kind: 5, u: u})
		}
	}
	if len(matches) > 0 || len(out) > 0 {
		if canAdd {
			keys = append(keys, cdRosterKey{kind: 1})
		}
	}
	if len(matches) == 0 && len(out) > 0 {
		keys = append(keys, cdRosterKey{kind: 2}, cdRosterKey{kind: 4})
	}
	if len(matches) > 0 {
		keys = append(keys, cdRosterKey{kind: 2})
		keys = append(keys, matches...)
	}
	if len(out) > 0 {
		keys = append(keys, cdRosterKey{kind: 3})
		keys = append(keys, out...)
	}
	return keys, true
}

// reworkedAgentsTab is ReworkedAgentsAppsTab.
func (d *ChannelDetails) reworkedAgentsTab(gtx C, inf core.ChannelDetails) D {
	ag := d.ag()
	cv := d.conv(inf, true)
	d.updateAgentsTab(gtx, cv)
	rp, loaded, err := ag.roster.get()
	keys, searching := d.agentsRosterKeys(cv)
	hasAgents := d.agentsAppsCount() > 0                                          // y
	cta := !searching && !hasAgents                                               // S
	compact := !hasAgents                                                         // shouldSizeRosterToContent
	if loaded && err == nil && rp.next != "" && !ag.roster.busy() && !searching { // loadPost
		if p := ag.list.Position; p.First+p.Count >= len(keys)-5 {
			d.loadAgentRoster(rp.next)
		}
	}
	roster := func(gtx C) D {
		return col(gtx,
			layout.Rigid(func(gtx C) D {
				dims := layout.Inset{Top: 16, Bottom: 16, Left: cdPadX, Right: cdPadX}.Layout(gtx, func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					return dirSearchField(&d.base, gtx, ag.find, dirFieldStyle{H: 36, Icon: 16, PadL: 8, Gap: 8, Border: d.p.Faint, Over: d.p.Surface}, nil)
				})
				if p := ag.list.Position; p.First > 0 || p.Offset > 0 {
					fill(gtx, image.Rect(0, dims.Size.Y-max(1, gtx.Dp(1)), gtx.Constraints.Max.X, dims.Size.Y), dirRing(&d.base, d.p.Surface))
				}
				return dims
			}),
			layout.Flexed(1, func(gtx C) D { return d.agentsRosterList(gtx, keys, searching, loaded, err) }),
		)
	}
	if !compact || searching {
		return roster(gtx)
	}
	// compact: the list is as tall as its rows (at most 4, 60px each, +1)
	rows := min(len(keys), 4)
	if !loaded {
		rows = 4
	}
	listH := rows*gtx.Dp(cdRosterRowH) + max(1, gtx.Dp(1))
	return col(gtx,
		layout.Rigid(func(gtx C) D {
			g := gtx
			g.Constraints.Max.Y = min(gtx.Constraints.Max.Y, gtx.Dp(68)+listH)
			g.Constraints.Min.Y = 0
			return col(g,
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Top: 16, Bottom: 16, Left: cdPadX, Right: cdPadX}.Layout(gtx, func(gtx C) D {
						gtx.Constraints.Min.X = gtx.Constraints.Max.X
						return dirSearchField(&d.base, gtx, ag.find, dirFieldStyle{H: 36, Icon: 16, PadL: 8, Gap: 8, Border: d.p.Faint, Over: d.p.Surface}, nil)
					})
				}),
				layout.Rigid(func(gtx C) D {
					gtx.Constraints.Max.Y = min(gtx.Constraints.Max.Y, listH)
					gtx.Constraints.Min.Y = gtx.Constraints.Max.Y
					return d.agentsRosterList(gtx, keys, searching, loaded, err)
				}),
			)
		}),
		layout.Rigid(func(gtx C) D {
			if !cta {
				return D{}
			}
			return d.agentsCTA(gtx, cv)
		}),
	)
}

// agentsRosterList is the roster's list (or its loading placeholders and
// empty state).
func (d *ChannelDetails) agentsRosterList(gtx C, keys []cdRosterKey, searching, loaded bool, err error) D {
	ag := d.ag()
	switch {
	case !loaded:
		return layout.Inset{Left: cdPadX - 20, Right: cdPadX - 20}.Layout(gtx, func(gtx C) D { return d.skeleton(gtx, 4, 36) })
	}
	_ = err // a1 logs a failed page and stops paging
	if searching && len(keys) == 0 && !ag.out.busy() && ag.deb.q == ag.deb.sent {
		// ng with e7: renderEntirelyEmptyState's description only
		return layout.Inset{Top: 24, Bottom: 24, Left: cdPadX, Right: cdPadX}.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return d.cdRichCentered(gtx, "agents-empty", nsChannelDetails.RT("No matches found for <strong>{searchTerm}</strong>", "searchTerm", ag.find.text()), "searchTerm")
		})
	}
	self := d.selfID()
	return d.scrollList(gtx, &ag.list, len(keys), func(gtx C, i int) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		k := keys[i]
		switch k.kind {
		case 1:
			return d.hoverable(gtx, &ag.addRow, false, 0, func(gtx C) D {
				return layout.Inset{Top: 12, Bottom: 12, Left: cdPadX, Right: cdPadX}.Layout(gtx, func(gtx C) D {
					return d.cdTileRow(gtx, "user-plus", func(gtx C) D { return d.label(gtx, 15, wBold, d.p.Text, nsChannelDetails.T("Add agents and apps")) })
				})
			})
		case 2:
			return d.rosterHeading(gtx, nsChannelDetails.T("Agents & apps in this channel"), false)
		case 3:
			return d.rosterHeading(gtx, nsChannelDetails.T("Not in this channel"), true)
		case 4:
			// tw: the user glyph on foreground_soft_solid, bold at .5
			return layout.Inset{Top: 16, Bottom: 16, Left: 12 + 16, Right: 12 + 16}.Layout(gtx, func(gtx C) D {
				return row(gtx, layout.Middle,
					layout.Rigid(func(gtx C) D {
						s := gtx.Dp(36)
						rrect(gtx, image.Rect(0, 0, s, s), gtx.Dp(4), d.p.Track)
						g := gtx
						g.Constraints = layout.Exact(image.Pt(s, s))
						layout.Center.Layout(g, func(gtx C) D { return d.icon(gtx, "user", 20, d.p.Muted) })
						return D{Size: image.Pt(s, s)}
					}),
					hgap(12),
					layout.Rigid(func(gtx C) D {
						return d.label(gtx, 15, wBold, theme.WithAlpha(d.p.Text, 0x80), nsChannelDetails.T("No matches"))
					}),
				)
			})
		case 5:
			return d.agentRosterRow(gtx, k.u, self, true)
		}
		return d.agentRosterRow(gtx, k.u, self, false)
	})
}

// rosterHeading is a p-ia_details_popover__members_list_item_heading:
// 13px bold foreground_max_solid, padding 0 20 0 28; "Not in this
// channel" adds a top hairline, foreground_min_solid and 16px above.
func (d *ChannelDetails) rosterHeading(gtx C, label string, out bool) D {
	top := unit.Dp(8)
	if out {
		top = 16
	}
	m := op.Record(gtx.Ops)
	dims := layout.Inset{Top: top, Bottom: 8, Left: cdPadX, Right: 20}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return d.para(gtx, 13, 18, wBold, d.p.Meta, 1, label)
	})
	call := m.Stop()
	if out {
		fill(gtx, image.Rectangle{Max: dims.Size}, d.p.ReadFill)
		fill(gtx, image.Rect(0, 0, dims.Size.X, max(1, gtx.Dp(1))), dirRing(&d.base, d.p.Surface))
	}
	call.Add(gtx.Ops)
	return dims
}

// agentRosterRow is IADetailsPopoverMemberItemComponent for an agent or
// app: MediumMemberEntity (the name with its badge and hover card, the
// title below) and, outside the channel, foreground_min_solid with "Add
// to Channel".
func (d *ChannelDetails) agentRosterRow(gtx C, u core.User, self string, out bool) D {
	ag := d.ag()
	u = d.viewUser(u)
	c := dirClick(d.memberClk, "ag:"+u.ID)
	if c.Clicked(gtx) {
		openProfileCard(d.h, d.press.at, u.ID)
	}
	badge := core.BadgeApp
	if u.Agent || u.Agentforce {
		badge = core.BadgeAgent
	}
	full, disp := cdMemberNames(u, self)
	body := func(gtx C) D {
		if out {
			fill(gtx, image.Rectangle{Max: image.Pt(gtx.Constraints.Max.X, gtx.Dp(cdRosterRowH))}, d.p.ReadFill)
		}
		gtx.Constraints.Min.Y = gtx.Dp(cdRosterRowH)
		return layout.Inset{Left: cdPadX, Right: cdPadX}.Layout(gtx, func(gtx C) D {
			return layout.W.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return row(gtx, layout.Middle,
					layout.Rigid(func(gtx C) D { return d.avatar(gtx, u.Avatar, 36) }),
					hgap(12),
					layout.Flexed(1, func(gtx C) D {
						return dirFill(gtx, func(gtx C) D {
							return layout.Inset{Top: -4, Bottom: -4}.Layout(gtx, func(gtx C) D {
								return col(gtx,
									layout.Rigid(func(gtx C) D {
										return row(gtx, layout.Middle,
											layout.Rigid(func(gtx C) D {
												nd := d.para(gtx, 15, 22, wBold, d.p.Text, 1, full)
												d.memberTrigger(u.ID).Layout(gtx, u.ID, nd.Size)
												return nd
											}),
											layout.Rigid(func(gtx C) D {
												return layout.Inset{Left: 4}.Layout(gtx, func(gtx C) D { return cdAppBadge(&d.base, gtx, badge) })
											}),
											layout.Rigid(func(gtx C) D {
												if disp == "" {
													return D{}
												}
												return layout.Inset{Left: 4}.Layout(gtx, func(gtx C) D { return d.para(gtx, 15, 22, wReg, d.p.Text, 1, disp) })
											}),
										)
									}),
									layout.Rigid(func(gtx C) D {
										if u.Title == "" {
											return D{}
										}
										return d.para(gtx, 15, 22, wReg, d.p.Sec, 1, u.Title)
									}),
								)
							})
						})
					}),
					layout.Rigid(func(gtx C) D {
						if !out {
							return D{}
						}
						if ag.adding == u.ID {
							return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D { return d.label(gtx, 13, wReg, d.p.Muted, "…") })
						}
						b := dirClick(ag.addBtns, u.ID)
						return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D {
							return d.button(gtx, b, btnOutline, true, nsChannelDetails.T("Add to Channel"))
						})
					}),
				)
			})
		})
	}
	return d.hoverable(gtx, c, false, 0, body)
}

// agentsCTA is AppsAgentsSection's renderCtaField inside its FieldGroup
// (p-field_group: margin 16 28, radius 12, 1px border; p-field--cta: the
// inner 16px 20px, the 15/22 bold title, the text 4px above and 12px
// below, the buttons 12px apart; the 148px side image on indigo-5).
func (d *ChannelDetails) agentsCTA(gtx C, cv core.Conv) D {
	ag := d.ag()
	canAdd := d.canAddAgentsApps(cv) // R
	return layout.Inset{Top: 16, Bottom: 16, Left: cdPadX, Right: cdPadX}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		m := op.Record(gtx.Ops)
		imgW := gtx.Dp(148)
		g := gtx
		g.Constraints.Max.X = max(0, gtx.Constraints.Max.X-imgW)
		g.Constraints.Min.X = g.Constraints.Max.X
		inner := layout.Inset{Top: 16, Bottom: 16, Left: 20, Right: 20}.Layout(g, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return col(gtx,
				layout.Rigid(func(gtx C) D {
					return cdLine22(gtx, func(gtx C) D { return d.para(gtx, 15, 22, wBold, d.p.Text, 0, nsChannelTabs.T("Agents and apps")) })
				}),
				vgap(4),
				layout.Rigid(func(gtx C) D {
					return d.para(gtx, 15, 22, wReg, d.p.Text, 0, nsChannelTabs.T("Get more done in this channel by adding your team’s AI agents and apps"))
				}),
				vgap(12),
				layout.Rigid(func(gtx C) D {
					ch := []layout.FlexChild{}
					if canAdd {
						ch = append(ch, layout.Rigid(func(gtx C) D {
							return d.button(gtx, &ag.browse, btnOutline, true, nsChannelTabs.T("Browse Agents and Apps"))
						}), hgap(12))
					}
					ch = append(ch, layout.Rigid(func(gtx C) D {
						return d.button(gtx, &ag.learn, btnOutline, true, nsChannelTabs.T("Learn More"))
					}))
					return row(gtx, layout.Middle, ch...)
				}),
			)
		})
		call := m.Stop()
		w := gtx.Constraints.Max.X
		h := inner.Size.Y
		r := image.Rect(0, 0, w, h)
		rad := gtx.Dp(12)
		line := dirRing(&d.base, d.p.Surface)
		rrect(gtx, r, rad, d.p.Surface)
		func() {
			defer clip.UniformRRect(r, rad).Push(gtx.Ops).Pop()
			side := image.Rect(w-imgW, 0, w, h)
			fill(gtx, side, theme.PaletteShade("indigo", 1, d.th.Dark))
			// background-size: cover
			if d.h.Images() != nil {
				if img, ok := d.h.Images().Get(cdAgentsCTAImage, max(imgW, h)); ok {
					func() {
						defer op.Offset(side.Min).Push(gtx.Ops).Pop()
						theme.Image(gtx, img, side.Size(), 0)
					}()
				}
			}
			call.Add(gtx.Ops)
		}()
		border(gtx, r, rad, max(1, gtx.Dp(1)), line)
		return D{Size: r.Max}
	})
}

// ---- the channel invite modal, scoped to agents and apps ----

// openAgentInvite is the roster's "Add agents and apps" (a3): the
// channel invite modal with entityScope AgentsApps, canAddAgents and the
// "Add agents and apps to {channelName}" title.
func (d *ChannelDetails) openAgentInvite() {
	d.openDialog(cdDlgAddPeople)
	a := &cdAddPeople{agents: true, q: newSearchIn(nsChannelInvite, "Search agents and apps"), tokClk: map[string]*widget.Clickable{},
		resClk: map[string]*widget.Clickable{}, members: map[string]bool{}}
	ag := d.ag()
	rp, _, _ := ag.roster.get()
	for _, u := range append(append([]core.User(nil), rp.users...), ag.added...) {
		a.members[u.ID] = true
	}
	d.x.dlg.add = a
}

// agentInviteDialog is AddToChannelInitialScreen under AgentsApps.
func (d *ChannelDetails) agentInviteDialog(gtx C, full image.Point, inf core.ChannelDetails, a *cdAddPeople) {
	dl := &d.x.dlg
	cv := d.conv(inf, true)
	name, _ := d.title(cv)
	name = "#" + name
	dl.subtitle = name
	// the results menu opens under the picker: track how far down it is
	// (the title bar, then the rows above it; laid out in this order)
	first := true
	track := func(w W) W {
		return func(gtx C) D {
			if first {
				a.pickY, first = dl.titleH, false
			}
			dims := w(gtx)
			a.pickY += dims.Size.Y
			return dims
		}
	}
	body := []W{
		track(func(gtx C) D { return d.para(gtx, 15, 22, wBold, d.p.Text, 1, nsChannelInvite.T("Agents and apps")) }),
		track(cdSpace(4)),
		func(gtx C) D { return d.addPeoplePicker(gtx, a) },
	}
	if dl.err != "" {
		body = append(body, cdSpace(12), d.cdAlert(dl.err, false))
	}
	body = append(body, cdSpace(16), func(gtx C) D {
		return layout.W.Layout(gtx, func(gtx C) D {
			return d.button(gtx, &a.explore, btnOutline, false, nsChannelInvite.T("Explore more agents and apps"))
		})
	}, cdSpace(8))
	title := nsChannelDetails.T("Add agents and apps to {channelName}", "channelName", name)
	d.skModal(gtx, full, title, true, body, []W{d.cdDlgButton(&dl.ok, btnPrimary, nsChannelInvite.T("Add"), len(a.picked) == 0 || dl.pending)})
}

// updateAgentInvite runs the scoped modal: Explore more closes it and
// opens the apps browser (eN); Add invites the picked agents and apps.
func (d *ChannelDetails) updateAgentInvite(gtx C, a *cdAddPeople) {
	dl := &d.x.dlg
	c, id := d.h.Core(), d.id
	if a.explore.Clicked(gtx) {
		d.closeDialog()
		if ph, ok := d.h.(popups.Host); ok {
			popups.OpenAppsBrowser(ph, id)
		}
		return
	}
	if dl.ok.Clicked(gtx) && len(a.picked) > 0 && !dl.pending {
		ids := make([]string, len(a.picked))
		for i, u := range a.picked {
			ids[i] = u.ID
		}
		dl.pending, dl.err = true, ""
		dl.op.load(d.h, false, func(ctx context.Context) (bool, error) { return true, c.Invite(ctx, id, ids...) })
	}
	if dl.pending {
		if _, done, err := dl.op.get(); done {
			dl.pending = false
			if err != nil { // ChannelInviteFormInlineError
				dl.err = cdErr(err)
				return
			}
			ag := d.ag()
			ag.added = append(append([]core.User(nil), a.picked...), ag.added...) // onMembersAdded
			d.counts.load(d.h, true, func(ctx context.Context) (core.MemberCounts, error) { return c.MemberCounts(ctx, id) })
			d.closeDialog()
			d.loadAgentRoster("")
			gtx.Execute(key.FocusCmd{Tag: &ag.find.ed})
		}
	}
}

// AgentInviteOpen reports whether the scoped invite modal is open (tests).
func (d *ChannelDetails) AgentInviteOpen() bool {
	return d.x.dlg.kind == cdDlgAddPeople && d.x.dlg.add != nil && d.x.dlg.add.agents
}

// AgentRosterIDs are the reworked roster's member rows, in order (tests).
func (d *ChannelDetails) AgentRosterIDs() []string {
	inf, _, _ := d.info.get()
	keys, _ := d.agentsRosterKeys(d.conv(inf, true))
	var out []string
	for _, k := range keys {
		if k.kind == 0 || k.kind == 5 {
			out = append(out, k.u.ID)
		}
	}
	return out
}

// AgentRosterKinds names the reworked roster's rows (tests): "add",
// "in-heading", "out-heading", "no-matches", "member:<id>", "out:<id>".
func (d *ChannelDetails) AgentRosterKinds() []string {
	inf, _, _ := d.info.get()
	keys, _ := d.agentsRosterKeys(d.conv(inf, true))
	out := make([]string, len(keys))
	for i, k := range keys {
		switch k.kind {
		case 1:
			out[i] = "add"
		case 2:
			out[i] = "in-heading"
		case 3:
			out[i] = "out-heading"
		case 4:
			out[i] = "no-matches"
		case 5:
			out[i] = "out:" + k.u.ID
		default:
			out[i] = "member:" + k.u.ID
		}
	}
	return out
}

// AgentsRosterLoaded reports whether the reworked roster's first page has
// loaded (tests).
func (d *ChannelDetails) AgentsRosterLoaded() bool {
	_, ok, _ := d.ag().roster.get()
	return ok
}

// SetAgentsQuery types q into the reworked roster's search (tests).
func (d *ChannelDetails) SetAgentsQuery(q string) {
	d.ag().find.ed.SetText(q)
	d.h.Invalidate()
}

// PressAgents clicks a reworked-tab control: "add" (the add row),
// "browse", "learn", "explore" (the invite modal's Explore more),
// "invite" (its Add).
func (d *ChannelDetails) PressAgents(name string) {
	ag := d.ag()
	switch name {
	case "add":
		ag.addRow.Click()
	case "browse":
		ag.browse.Click()
	case "learn":
		ag.learn.Click()
	case "explore":
		if a := d.x.dlg.add; a != nil {
			a.explore.Click()
		}
	case "invite":
		d.x.dlg.ok.Click()
	}
	d.h.Invalidate()
}

// MembersFilterLabels are the Members filter's options as drawn (tests;
// nil without a filter).
func (d *ChannelDetails) MembersFilterLabels() []string {
	if !d.agentsImproved() {
		out := make([]string, len(d.memFilter.labels))
		for i := range out {
			out[i] = d.memFilter.text(i)
		}
		return out
	}
	dd, _, ok := d.membersFilter()
	if !ok {
		return nil
	}
	return append([]string(nil), dd.labels...)
}

// SelectMembersFilter picks the Members filter's option i (tests).
func (d *ChannelDetails) SelectMembersFilter(i int) {
	if d.agentsImproved() {
		d.ag().filt.picked = i + 1
	} else {
		d.memFilter.picked = i + 1
	}
	d.h.Invalidate()
}

// MembersPlaceholder and MembersAddLabel are the Members search's
// placeholder and its add row's label (tests).
func (d *ChannelDetails) MembersPlaceholder() string { d.membersSearch(); return d.find.placeholder() }
func (d *ChannelDetails) MembersAddLabel() string    { return d.membersAddLabel() }

// MembersShown are the Members rows after the search and filter (tests).
func (d *ChannelDetails) MembersShown() []string {
	mp, _, _ := d.members.get()
	var us []core.User
	if d.agentsImproved() {
		_, kind, _ := d.membersFilter()
		us = d.filteredImproved(mp.users, d.find.text(), kind)
	} else {
		us = d.filtered(mp.users, d.find.text(), d.memFilter.sel)
	}
	out := make([]string, len(us))
	for i, u := range us {
		out[i] = u.ID
	}
	return out
}

// SetAgentInviteQuery types q into the scoped invite modal's picker
// (tests).
func (d *ChannelDetails) SetAgentInviteQuery(q string) {
	if a := d.x.dlg.add; a != nil && a.agents {
		a.q.ed.SetText(q)
		d.h.Invalidate()
	}
}

// AgentsCTAShown reports whether the reworked tab shows AppsAgentsSection's
// call to action (no agents or apps, nothing typed; tests).
func (d *ChannelDetails) AgentsCTAShown() bool {
	return d.agentsImproved() && d.ag().find.text() == "" && d.agentsAppsCount() == 0
}

// AgentsReworked reports whether the Agents & apps tab is
// ReworkedAgentsAppsTab (tests).
func (d *ChannelDetails) AgentsReworked() bool { return d.agentsImproved() }
