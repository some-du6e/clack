package core

import (
	"slices"

	"github.com/some-du6e/yoloslackgoclient/internal/store"
)

// The left rail (Slack's tab rail), ported from its web client (build
// a84662d2d135; see docs/SIDEBAR-RULES.md, "Rail"):
//
//   - tab ids and their default order: client-boot pl6F (k6, Lk)
//   - the user's order: tab_rail_customization_order, valid ids only,
//     deduplicated, with every default tab it lacks spliced in at its
//     default index (boot-shared Ozeq)
//   - which tabs exist for the user: client-boot dk8L (get_tab_visibility)
//   - which of those sit in the rail: tab_rail_customization merged over
//     the defaults (client-boot 5ydT, Y2EP); Home and Activity cannot be
//     turned off (cR2m)
//   - visibleTabs = order ∩ available; tabRailList = visibleTabs shown in
//     the rail (boot-shared G1Mx). Everything else in visibleTabs goes under
//     More (boot-render 4v2a), as do rail tabs that don't fit the height.

// Rail tab ids (pl6F k6).
const (
	TabSlackbot      = "slackbot"
	TabToday         = "today"
	TabHome          = "home"
	TabDMs           = "dms"
	TabActivity      = "activity"
	TabActivityInbox = "activity-inbox"
	TabActivityProto = "activity-inbox-sidebar-prototype"
	TabSearch        = "search"
	TabSalesHome     = "sales-home"
	TabFiles         = "unified-files"
	TabLater         = "later"
	TabAgents        = "agents"
	TabPlatform      = "platform"
	TabPlayground    = "slack-kit-playground"
	TabBrowse        = "browse" // More
)

// railDefaultOrder is pl6F's Lk.
var railDefaultOrder = []string{TabSlackbot, TabToday, TabHome, TabDMs, TabActivity, TabActivityInbox, TabActivityProto,
	TabSearch, TabSalesHome, TabFiles, TabLater, TabAgents, TabPlatform, TabPlayground}

// RailTab is one tab in the rail or under More.
type RailTab struct {
	ID       string // Slack's tab id (Tab*)
	Label    string // mBDz.A
	Subtitle string // the More menu's second line (BrowsePeekMenu)
}

// RailState is the rail as Slack would build it, before fitting it to the
// window height (OrganizeRail).
type RailState struct {
	Rail      []string           // tabRailList: tabs the user keeps in the rail, in order
	Available []string           // visibleTabs: every tab the user has, in order
	Tabs      map[string]RailTab // labels for every tab above and More
	// Home tab badge inputs (InjD useGetMentions)
	ActivityInbox bool // activity_inbox: Activity is the activity-inbox tab
	DMsInRail     bool // tab_rail_customization dms (iHjP.a)
	DMsOptOut     bool // activity_dms_opt_out with the activity inbox (5Ns/ Vn)
	Compact       bool // ia4_compact_mode
}

type railFacts struct {
	exp                      func(string) string
	prefs                    store.Prefs
	overdueTodos             int  // YeRq.BK (Clack does not know it: 0)
	simplifiedOnboarding     bool // m01E.l: onboarding in its first 7 days (false for Clack)
	agentforceAccess         bool // LxPK.TI (no Agentforce on Hack Club)
	slackbotTab, todayTab    bool // Clack has neither view
	salesHome, kitPlayground bool
}

func (f railFacts) on(name string) bool { return f.exp(name) == "on" }

// available is dk8L.get_tab_visibility for the tabs Clack can show.
func (f railFacts) available(tab string) bool {
	movedIntoHome := f.on("unified_global_triage") || f.on("activity_in_home_prototype") // 6rqO Jz, j
	inbox := f.on("activity_inbox")                                                      // 5Ns/ Lh
	withSidebar := inbox && f.exp("activity_with_sidebar") == "on"                       // 5Ns/ lu
	mentionOK := !f.simplifiedOnboarding                                                 // or has_received_mention_or_reaction
	switch tab {
	case TabHome:
		return true // JMmn.R is false
	case TabActivity:
		return !movedIntoHome && !inbox && mentionOK
	case TabActivityInbox:
		return !movedIntoHome && inbox && !withSidebar && mentionOK
	case TabActivityProto:
		return !movedIntoHome && inbox && withSidebar && mentionOK
	case TabDMs:
		return true
	case TabFiles:
		return true // isEligibleForFilesTabNuxV2Treatment || shouldShowTabs
	case TabLater: // OpEF shouldShowLaterLink
		return f.overdueTodos > 0 || f.prefs.HasStarredItem
	case TabAgents:
		return f.agentforceAccess && !f.on("agentforce_deprecation")
	case TabPlatform:
		return true
	case TabSlackbot:
		return f.slackbotTab
	case TabToday:
		return f.todayTab
	case TabSalesHome:
		return f.salesHome
	case TabPlayground:
		return f.kitPlayground
	}
	return false // search: () => false
}

// railDefaults is Y2EP: whether a tab sits in the rail without a pref.
func (f railFacts) railDefault(tab string) bool {
	switch tab {
	case TabFiles:
		return !f.simplifiedOnboarding || f.prefs.HasUploaded
	case TabPlatform:
		return f.on("agent_tasks") // HLQ9.rh
	}
	return true
}

// railForced is cR2m: tabs whose Navigation checkbox is disabled (always in
// the rail).
func railForced(tab string) bool {
	switch tab {
	case TabHome, TabActivity, TabActivityInbox, TabActivityProto:
		return true
	}
	return false
}

// railOrder is Ozeq.j.
func railOrder(p store.Prefs) []string {
	if !p.TabRailOrderSet {
		return slices.Clone(railDefaultOrder)
	}
	var out []string
	for _, t := range p.TabRailOrder {
		if slices.Contains(railDefaultOrder, t) && !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	for i, t := range railDefaultOrder {
		if !slices.Contains(out, t) {
			if i > len(out) {
				i = len(out)
			}
			out = slices.Insert(out, i, t)
		}
	}
	return out
}

func buildRail(f railFacts) RailState {
	visible := []string{}
	for _, t := range railOrder(f.prefs) {
		if f.available(t) {
			visible = append(visible, t)
		}
	}
	inRail := func(t string) bool { // 5ydT: {...defaults, ...pref}, forced tabs true
		if railForced(t) {
			return true
		}
		if v, ok := f.prefs.TabRailCustomization[t]; ok {
			return v
		}
		return f.railDefault(t)
	}
	var rail []string
	for _, t := range visible {
		if inRail(t) {
			rail = append(rail, t)
		}
	}
	return RailState{
		Rail: rail, Available: visible, Tabs: railLabels(f),
		ActivityInbox: f.on("activity_inbox"), DMsInRail: inRail(TabDMs),
		DMsOptOut: f.on("activity_inbox") && f.prefs.ActivityDMsOptOut, Compact: f.prefs.CompactRail,
	}
}

// railLabels is mBDz.A plus the More menu's subtitles.
func railLabels(f railFacts) map[string]RailTab {
	later := RailTab{ID: TabLater, Label: "Later", Subtitle: "Saved messages to review later"}
	if f.on("later_todos") { // 6owI.tj
		later = RailTab{ID: TabLater, Label: "To-dos", Subtitle: "A place to manage your to-dos"}
	}
	platform := RailTab{ID: TabPlatform, Label: "Tools", Subtitle: "Create and find workflows and apps"}
	if f.on("agents_landing_page") { // M/qt.Wp (frontline users keep "Tools"; Clack has none)
		platform = RailTab{ID: TabPlatform, Label: "Agents & tools", Subtitle: "Manage agents, apps, and workflows"}
	}
	activity := RailTab{Label: "Activity", Subtitle: "Updates on what’s happening"}
	tabs := map[string]RailTab{
		TabHome:     {ID: TabHome, Label: "Home"},
		TabDMs:      {ID: TabDMs, Label: "DMs", Subtitle: "Direct messages with your team"},
		TabFiles:    {ID: TabFiles, Label: "Files", Subtitle: "Canvases, lists and attachments"},
		TabLater:    later,
		TabPlatform: platform,
		TabAgents:   {ID: TabAgents, Label: "Agentforce", Subtitle: "Browse and team up with AI agents"},
		TabBrowse:   {ID: TabBrowse, Label: "More"},
	}
	for _, id := range []string{TabActivity, TabActivityInbox, TabActivityProto} {
		t := activity
		t.ID = id
		tabs[id] = t
	}
	return tabs
}

// Rail returns the rail from the user's Navigation prefs and experiments.
func (a *App) Rail() RailState {
	a.S.RLock()
	p := a.S.Prefs
	a.S.RUnlock()
	return buildRail(railFacts{exp: a.experiment, prefs: p})
}

// RailTabHeight is a tab's height budget in dp (4v2a: 68, 40 compact; some
// locales 80).
func RailTabHeight(compact bool) int {
	if compact {
		return 40
	}
	return 68
}

// OrganizeRail is useTabRailOrganizer (boot-render 4v2a): rail tabs go in
// order while they fit in avail dp (each costs height(tab)); every other
// available tab goes under More, and More is appended when that is not
// empty, giving up the last tab if it no longer fits.
func OrganizeRail(r RailState, avail int, height func(tab string) int) (show, overflow []string) {
	s := avail
	queue := slices.Clone(r.Rail)
	for len(queue) > 0 && s >= height(queue[0]) {
		t := queue[0]
		queue = queue[1:]
		show = append(show, t)
		s -= height(t)
	}
	rest := func() []string {
		var out []string
		for _, t := range r.Available {
			if !slices.Contains(show, t) {
				out = append(out, t)
			}
		}
		return out
	}
	overflow = rest()
	if len(overflow) > 0 {
		if s < RailTabHeight(r.Compact) && len(show) > 1 {
			show = show[:len(show)-1]
			overflow = rest()
		}
		show = append(show, TabBrowse)
	}
	return show, overflow
}

// HomeBadge is the Home tab's badge (InjD useGetMentions). Home counts DM
// mentions only when the DMs tab is not in the rail (and, with the activity
// inbox, only when DMs are opted out of it); otherwise it shows a dot when
// channels (not DMs) have unread messages (rHEx homeBadgeCount).
func (r RailState) HomeBadge(channelsUnread bool, dmCount int) (count int, dot bool) {
	badge := !r.DMsInRail && dmCount > 0
	if r.ActivityInbox {
		badge = badge && r.DMsOptOut
	}
	if badge {
		return dmCount, false
	}
	if r.ActivityInbox {
		return 0, (!r.DMsOptOut || r.DMsInRail || dmCount == 0) && channelsUnread
	}
	return 0, channelsUnread
}

// RailCustomization is 5ydT.b: tab_rail_customization merged over each
// tab's default, forced tabs true. Setting one entry and writing the map
// back is setTabRailCustomizationByTabName.
func (a *App) RailCustomization() map[string]bool {
	a.S.RLock()
	p := a.S.Prefs
	a.S.RUnlock()
	f := railFacts{exp: a.experiment, prefs: p}
	m := map[string]bool{}
	for _, t := range railDefaultOrder {
		m[t] = f.railDefault(t)
	}
	for t, v := range p.TabRailCustomization {
		m[t] = v
	}
	for _, t := range railDefaultOrder {
		if railForced(t) {
			m[t] = true
		}
	}
	return m
}

// RailOrder is the full tab order (Ozeq.j) tab_rail_customization_order
// is written as.
func (a *App) RailOrder() []string {
	a.S.RLock()
	p := a.S.Prefs
	a.S.RUnlock()
	return railOrder(p)
}

// RailForced reports a tab Navigation can't turn off (cR2m).
func RailForced(tab string) bool { return railForced(tab) }
