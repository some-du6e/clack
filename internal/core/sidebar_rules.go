package core

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
)

// Slack's sidebar rules, ported from its web client (build a84662d2d135,
// 2026-11-08; public CDN chunks a.slack-edge.com/bv1-13-br/). Module ids
// are webpack ids in gantry-v2-async-client-boot-{shared,render} and
// modern.vendor:
//
//   - candidates (boot-render lJBI / application XkwK): member, unarchived
//     channels; DMs and group DMs only when open (is_open), unread, with a
//     draft or a huddle; never DMs with deleted users or the USEARCH /
//     UCHANNELEXPERT system users.
//   - each section's filter (boot-render 0Eub + boot-shared aQqV, ij26):
//     the channel_sections pref's "sidebar" value when the global
//     sidebar_behavior is "hide_read_channels_unless_starred" (Custom by
//     section), otherwise derived from sidebar_behavior.
//   - per-row hiding (boot-render lJBI): "active" hides channels Slack's
//     server marked dormant (properties.is_dormant, modern.vendor QzJY)
//     when the filter_dormant_channels experiment is on; "hid" hides read
//     rows; "mentions" keeps only rows with a badge; muted rows leave the
//     sidebar unless show_muted_items_outside_sidebar_menus.
//   - sort (boot-shared O/sM, s0VO; boot-render lJBI): per-section "sort"
//     pref or the global channel_sort; DM sections default to recent
//     activity under the dm_recency_sort_rollout experiment. Rows go in
//     groups (mentions first with boost_mentions, Slackbot, self-DM, …),
//     muted rows after the rest.

// sidebarExp is the experiments the rules depend on.
type sidebarExp struct {
	filterDormant bool // filter_dormant_channels
	improveMute   bool // improve_mute_pref
	dmRecency     bool // dm_recency_sort_rollout: without_edu | with_edu
	dmHistory     bool // programmable_sidebar_dm_history
}

// defaultSidebarExp is what experiments.getByUser answered for the test
// account on 2026-11-08, used until (or unless) Clack's own call answers.
var defaultSidebarExp = sidebarExp{filterDormant: true, improveMute: true, dmRecency: true}

func (a *App) setExperiments(groups map[string]string) {
	g := groups["dm_recency_sort_rollout"]
	e := sidebarExp{
		filterDormant: groups["filter_dormant_channels"] == "on",
		improveMute:   groups["improve_mute_pref"] == "on",
		dmRecency:     g == "without_edu" || g == "with_edu",
		dmHistory:     groups["programmable_sidebar_dm_history"] == "on",
	}
	a.mu.Lock()
	a.exp = &e
	a.expGroups = groups
	a.mu.Unlock()
}

// experiment returns the viewer's group for an experiment; before (or
// without) experiments.getByUser it falls back to what the test account had
// on 2026-11-08 (defaultExperimentGroups).
func (a *App) experiment(name string) string {
	a.mu.Lock()
	g := a.expGroups
	a.mu.Unlock()
	if g == nil {
		return defaultExperimentGroups[name]
	}
	return g[name]
}

// Experiment returns the viewer's group for a Slack experiment
// (experiments.getByUser), or the test account's from 2026-11-08 before
// that call answers.
func (a *App) Experiment(name string) string { return a.experiment(name) }

// SetExperimentGroup sets one experiment's group, over what
// experiments.getByUser answered (or the defaults), as Slack's
// force_* overrides do.
func (a *App) SetExperimentGroup(name, group string) {
	a.mu.Lock()
	g := make(map[string]string, len(defaultExperimentGroups)+1)
	src := a.expGroups
	if src == nil {
		src = defaultExperimentGroups
	}
	for k, v := range src {
		g[k] = v
	}
	g[name] = group
	a.mu.Unlock()
	a.setExperiments(g)
}

// defaultExperimentGroups mirrors defaultSidebarExp for the rail's
// experiments (rail.go).
var defaultExperimentGroups = map[string]string{
	"filter_dormant_channels": "on", "improve_mute_pref": "on", "dm_recency_sort_rollout": "without_edu",
	"activity_inbox": "on", "agent_tasks": "on", "agents_landing_page": "on", "later_todos": "on",
	"reskin_custom_responses": "on", // Slackbot custom responses as the team (CustomResponseReskin)
}

func (a *App) sidebarExperiments() sidebarExp {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.exp == nil {
		return defaultSidebarExp
	}
	return *a.exp
}

// Section filters (the channel_sections "sidebar" pref values).
const (
	filterAll      = "all"
	filterActive   = "active"
	filterHideRead = "hid"
	filterMentions = "mentions"
)

func isWindowFilter(f string) bool { return f == "today" || f == "d7" || f == "d30" }

// windowDays is how many days before today a DM history window reaches.
var windowDays = map[string]int{"today": 0, "d7": 6, "d30": 29}

// sidebarBehavior normalises the sidebar_behavior pref (hHm/).
func sidebarBehavior(p store.Prefs) string {
	switch b := p.SidebarBehavior; b {
	case "", "hide_read_channels", "hide_read_channels_unless_starred", "show_mentions_only", "hide_inactive_channels":
		return b
	}
	return ""
}

// paidTeam: Hack Club is on Enterprise Grid.
const paidTeam = true

// defaultSectionFilter is aQqV.I: a section's filter from the global
// sidebar_behavior.
func defaultSectionFilter(typ, behavior string, e sidebarExp) string {
	c := filterAll
	if e.filterDormant {
		c = filterActive
	}
	dm := typ == "direct_messages"
	if e.dmHistory && paidTeam && dm && (behavior == "hide_inactive_channels" || behavior == "hide_read_channels_unless_starred") {
		return "d7"
	}
	switch behavior {
	case "hide_inactive_channels":
		return filterActive
	}
	if typ == "coding_channels" {
		return c
	}
	switch behavior {
	case "hide_read_channels":
		return filterHideRead
	case "":
		return filterAll
	case "show_mentions_only":
		if dm {
			return filterHideRead
		}
		return filterMentions
	case "hide_read_channels_unless_starred":
		switch typ {
		case "standard", "stars":
			return c
		case "channels", "direct_messages", "org_channels", "recent_apps", "shared_channels", "slack_connect":
			return filterHideRead
		}
		return c
	}
	return filterAll
}

// sectionFilter is the getChannelSectionPref(id, "sidebar") case of 0Eub.
func sectionFilter(typ, id string, p store.Prefs, e sidebarExp) string {
	behavior := sidebarBehavior(p)
	pref := p.SectionPrefs[id]["sidebar"]
	dmHist := e.dmHistory && typ == "direct_messages"
	l := pref // ij26.K
	switch {
	case isWindowFilter(l) && !dmHist:
		l = filterActive
	case dmHist && (l == filterActive || l == filterAll):
		l = "d30"
	}
	known := l == filterAll || l == filterHideRead || l == filterMentions || l == filterActive || isWindowFilter(l)
	if paidTeam && l != "" && known && behavior == "hide_read_channels_unless_starred" {
		if typ == "direct_messages" && l == filterMentions {
			return filterHideRead
		}
		return l
	}
	return defaultSectionFilter(typ, behavior, e)
}

// rowState is what the row filter and the sort need about a conversation.
type rowState struct {
	unread, badge, muted, dormant bool
	latest                        string
}

// keepRow is lJBI's per-row filter for a row in a section with filter f
// (not in edit mode; Clack has no drafts or huddle state in core).
// collapsed is the section's state R: a collapsed section keeps its unread
// rows only, muted ones included.
func keepRow(r rowState, f string, windowHidden, collapsed bool, p store.Prefs, e sidebarExp) bool {
	hideDormant := f == filterActive
	showRead := f == filterAll
	hideAllButMentions := f == filterMentions
	hideRead := f == filterHideRead || hideAllButMentions || windowHidden
	hideMuted := p.ShowMutedOutside // getSidebarShouldHideMutedItems
	if e.improveMute {
		hideMuted = !p.ShowMutedOutside
	}
	if e.improveMute && r.badge && !collapsed {
		return true
	}
	if e.filterDormant && hideDormant && r.dormant {
		return false
	}
	if e.improveMute && hideMuted && r.muted && !collapsed {
		return false
	}
	if showRead && !collapsed {
		return true
	}
	if !e.improveMute && r.badge {
		return true
	}
	if !hideMuted && r.muted && !e.improveMute {
		return false
	}
	if hideRead && hideAllButMentions && !r.badge {
		return false
	}
	if (hideRead || collapsed) && !r.unread {
		return false
	}
	return true
}

// windowCutoff is the unix time a DM history window starts (local midnight
// minus the window's days).
func windowCutoff(f string, now time.Time) int64 {
	d := windowDays[f]
	y, m, day := now.Date()
	return time.Date(y, m, day-d, 0, 0, 0, 0, now.Location()).Unix()
}

// sectionSort is getChannelSectionSortOrder (O/sM): "default"
// (alphabetical), "recent" or "priority".
func sectionSort(typ, id string, p store.Prefs, e sidebarExp) string {
	secPref := p.SectionPrefs[id]["sort"]
	dm := typ == "direct_messages"
	dmRecency := e.dmRecency || (e.dmHistory && p.ChannelSort == "default")
	if dmRecency && dm && secPref == "" || typ == "coding_channels" && secPref == "" {
		return "recent"
	}
	global := p.ChannelSort
	if paidTeam {
		switch secPref {
		case "priority", "default", "recent":
			return secPref
		}
	}
	switch global {
	case "priority":
		return "priority"
	case "recent":
		return "recent"
	case "recent_dms":
		if dm {
			return "recent"
		}
	}
	return "default"
}

// Sort groups (lJBI's eL), in display order.
const (
	grpSlackbotMention = iota
	grpYouMention
	grpMentions
	grpPrivateMentions
	grpImsMentions
	grpSlackbot
	grpYou
	grpInviter
	grpOnlineDms
	grpMpDms
	grpEverythingElse
	grpPrivate
	grpIms
	grpYouNewTeam
)

// sortRow is one row being sorted.
type sortRow struct {
	id, name, kind, user string
	priority             float64
	rowState
}

// sortGroup places a row (lJBI's group function; inviter and online-first
// DMs do not apply: Clack has no inviter, and online-first sorting needs a
// team under 50 people).
func sortGroup(r sortRow, inDMSection bool, sortOrder, self string, p store.Prefs) int {
	// lJBI: l=s&&G&&!v; v needs blended priorities (programmable_sidebar, off)
	mention := p.BoostMentions && r.badge
	if r.kind == "private" && p.SeparatePrivate {
		if mention {
			return grpPrivateMentions
		}
		return grpPrivate
	}
	if r.kind == "im" && (r.user == "USLACKBOT") {
		if mention {
			return grpSlackbotMention
		}
		return grpSlackbot
	}
	if r.kind == "im" && r.user == self && self != "" {
		switch {
		case inDMSection: // shouldBumpInviter is set for DM sections
			return grpYouNewTeam
		case mention:
			return grpYouMention
		}
		return grpYou
	}
	if (r.kind == "im" || r.kind == "mpim") && (p.UndoIntermix || inDMSection) {
		if mention {
			return grpImsMentions
		}
		return grpIms
	}
	if mention {
		return grpMentions
	}
	return grpEverythingElse
}

// sortSectionRows orders a section like Slack: unmuted rows, then muted
// ones; within each, by group, then by the section's sort.
func sortSectionRows(rows []sortRow, inDMSection bool, sortOrder, self string, p store.Prefs) []string {
	type key struct {
		muted bool
		group int
	}
	keys := make(map[string]key, len(rows))
	for _, r := range rows {
		keys[r.id] = key{r.muted, sortGroup(r, inDMSection, sortOrder, self, p)}
	}
	col := collate.New(language.AmericanEnglish, collate.Numeric) // QJub: Intl.Collator("en-US",{numeric:true})
	alpha := func(a, b sortRow) int {
		if a.name == b.name {
			return 0
		}
		return col.CompareString(a.name, b.name)
	}
	cmp := alpha
	switch sortOrder {
	case "recent":
		cmp = func(a, b sortRow) int {
			la, lb := tsFloat(a.latest), tsFloat(b.latest)
			if la == lb {
				return alpha(a, b)
			}
			if la > lb {
				return -1
			}
			return 1
		}
	case "priority":
		cmp = func(a, b sortRow) int {
			pa, pb := a.priority, b.priority
			if pa == 0 {
				pa = -1
			}
			if pb == 0 {
				pb = -1
			}
			if pa != pb {
				if pa > pb {
					return -1
				}
				return 1
			}
			return alpha(a, b)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		ki, kj := keys[rows[i].id], keys[rows[j].id]
		if ki.muted != kj.muted {
			return !ki.muted
		}
		if ki.group != kj.group {
			return ki.group < kj.group
		}
		return cmp(rows[i], rows[j]) < 0
	})
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.id
	}
	return out
}

func tsFloat(ts string) float64 {
	f, _ := strconv.ParseFloat(ts, 64)
	return f
}

// systemDMUsers are users whose DMs never show in the sidebar.
var systemDMUsers = map[string]bool{"USEARCH": true, "UCHANNELEXPERT": true}

// dmCandidate reports whether a DM or group DM is a sidebar candidate.
func (a *App) dmCandidate(id string) bool {
	c, ok := a.S.Conv(id)
	if !ok {
		return false
	}
	if c.IsArchived {
		return false
	}
	if c.IsIM {
		if systemDMUsers[c.User] {
			return false
		}
		if u, ok := a.S.User(c.User); ok && u.Deleted {
			return false
		}
	}
	if a.dmOpen(id) {
		return true
	}
	e, _ := a.S.Count(id)
	return e.HasUnreads // closed but unread: Slack still lists it
}

// applySectionRules filters and sorts every section in place.
func (a *App) applySectionRules(out []Section, names map[string]string, now time.Time) {
	a.S.RLock()
	p := a.S.Prefs
	muted := a.S.Muted
	self := a.S.Self.ID
	a.S.RUnlock()
	e := a.sidebarExperiments()
	sel := a.selectedID()
	for i := range out {
		sec := &out[i]
		f := sectionFilter(sec.Type, sec.ID, p, e)
		order := sectionSort(sec.Type, sec.ID, p, e)
		inDM := sec.Type == "direct_messages"
		var cutoff int64
		if inDM && isWindowFilter(f) {
			cutoff = windowCutoff(f, now)
		}
		rows := make([]sortRow, 0, len(sec.Channels))
		expanded, folded := map[string]bool{}, map[string]bool{}
		for _, id := range sec.Channels {
			c, ok := a.sidebarConv(id)
			if !ok {
				continue
			}
			cnt, hasCount := a.S.Count(id)
			r := sortRow{id: id, name: names[id], user: c.User, priority: c.Priority, rowState: rowState{
				unread: cnt.HasUnreads || cnt.MentionCount > 0, badge: cnt.MentionCount > 0,
				muted: muted[id], dormant: c.Dormant(), latest: cnt.Latest,
			}}
			switch {
			case c.IsIM:
				r.kind = "im"
			case c.IsMPIM:
				r.kind = "mpim"
			case c.IsPrivate || c.IsGroup:
				r.kind = "private"
			default:
				r.kind = "channel"
			}
			windowHidden := cutoff > 0 && (c.IsIM || c.IsMPIM) && hasCount && (cnt.Latest == "" || tsFloat(cnt.Latest) < float64(cutoff))
			expanded[id] = keepRow(r.rowState, f, windowHidden, false, p, e)
			folded[id] = keepRow(r.rowState, f, windowHidden, true, p, e)
			if id == sel { // lJBI: the open conversation's row always shows, collapsed or not
				expanded[id], folded[id] = true, true
			}
			if expanded[id] || folded[id] {
				rows = append(rows, r)
			}
		}
		sec.Channels, sec.CollapsedChannels = []string{}, nil
		for _, id := range sortSectionRows(rows, inDM, order, self, p) {
			if expanded[id] {
				sec.Channels = append(sec.Channels, id)
			}
			if folded[id] {
				sec.CollapsedChannels = append(sec.CollapsedChannels, id)
			}
		}
	}
}

// sectionCollapsed is 0Eub's "c" pref: the saved collapsed state ("1"),
// "0" by default.
func (a *App) sectionCollapsed(s slack.Section) bool {
	a.S.RLock()
	c := a.S.Prefs.SectionPrefs[s.ID]["c"]
	a.S.RUnlock()
	return c == "1" || c == "true"
}

// sectionRank is lJBI's R: which section wins when a conversation is in
// several (higher wins; any other type ends the search).
var sectionRank = map[string]int{"stars": 3, "standard": 2, "user_group": 1}

// assignSections picks each conversation's section like lJBI: starred ones
// go to Starred; temporary memberships (temp) to Temporary channels, or the
// default bucket without one; a single listing wins; several pick the
// highest rank among the leading starred/custom/user-group sections; ""
// leaves it to the default buckets.
func assignSections(ordered []slack.Section, starred []string, temp map[string]bool) map[string]string {
	starsID, tempID := "", ""
	in := map[string][]slack.Section{}
	isStar := map[string]bool{}
	for _, id := range starred {
		isStar[id] = true
	}
	for _, s := range ordered {
		if s.Type == "temporary_channels" && tempID == "" {
			tempID = s.ID
		}
		if s.Type == "stars" {
			if starsID == "" {
				starsID = s.ID
			}
			for _, id := range s.ChannelIDsPage.ChannelIDs {
				isStar[id] = true
			}
			continue
		}
		for _, id := range s.ChannelIDsPage.ChannelIDs {
			in[id] = append(in[id], s)
		}
	}
	out := map[string]string{}
	if starsID != "" {
		for id := range isStar {
			out[id] = starsID
		}
	}
	for id := range temp {
		if _, ok := out[id]; ok {
			continue // starred first
		}
		out[id] = tempID // "" (no such section): the default Channels bucket
	}
	for id, list := range in {
		if _, ok := out[id]; ok {
			continue
		}
		if len(list) == 1 {
			out[id] = list[0].ID
			continue
		}
		best := -1
		for i := range list {
			r, ok := sectionRank[list[i].Type]
			if !ok {
				break
			}
			if best < 0 || sectionRank[list[best].Type] < r {
				best = i
			}
		}
		if best >= 0 {
			out[id] = list[best].ID
		}
	}
	return out
}

// sortName is the name Slack sorts a row by (lJBI channelNamesMap): the
// channel or person name; for a group DM the other members' names, sorted
// and joined with ",".
func (a *App) sortName(c Conv) string {
	if c.Kind != "mpim" {
		return c.Name
	}
	self := a.S.SelfID()
	var names []string
	if len(c.Members) > 0 {
		for _, m := range c.Members {
			if m != self {
				names = append(names, a.S.UserName(m))
			}
		}
	} else { // mpdm-alice--bob--me-1: handles, minus our own
		selfName := ""
		if u, ok := a.S.User(self); ok {
			selfName = u.Name
		}
		h := strings.TrimPrefix(c.Name, "mpdm-")
		if i := strings.LastIndex(h, "-"); i > 0 {
			h = h[:i]
		}
		for _, n := range strings.Split(h, "--") {
			if n != "" && n != selfName {
				names = append(names, n)
			}
		}
	}
	collate.New(language.AmericanEnglish, collate.Numeric).SortStrings(names)
	return strings.Join(names, ",")
}
