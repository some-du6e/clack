package core

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// Synthetic prefs and experiments only.

func railFor(p store.Prefs, groups map[string]string) RailState {
	return buildRail(railFacts{exp: func(n string) string { return groups[n] }, prefs: p})
}

var testGroups = map[string]string{"activity_inbox": "on", "agent_tasks": "on", "agents_landing_page": "on", "later_todos": "on"}

func roomy(string) int { return 68 }

func TestRailDefaults(t *testing.T) {
	r := railFor(store.Prefs{HasStarredItem: true}, testGroups)
	// pl6F Lk order, filtered by dk8L (no Slackbot/Today/Search/Agentforce here)
	if got := strings.Join(r.Rail, ","); got != "home,dms,activity-inbox,unified-files,later,platform" {
		t.Fatalf("default rail = %s", got)
	}
	show, over := OrganizeRail(r, 1000, roomy)
	if strings.Join(show, ",") != strings.Join(r.Rail, ",") || len(over) != 0 {
		t.Errorf("everything fits: show=%v over=%v", show, over)
	}
	if r.Tabs[TabLater].Label != "To-dos" || r.Tabs[TabPlatform].Label != "Agents & tools" {
		t.Errorf("labels: %+v %+v", r.Tabs[TabLater], r.Tabs[TabPlatform])
	}
	// without has_starred_item (or overdue to-dos) there is no Later tab
	if r := railFor(store.Prefs{}, testGroups); strings.Contains(strings.Join(r.Available, ","), "later") {
		t.Errorf("later without saved items: %v", r.Available)
	}
}

func TestRailUserPrefsAndMore(t *testing.T) {
	// the user's Navigation prefs: Activity before DMs, Files and To-dos off
	p := store.Prefs{HasStarredItem: true, TabRailOrderSet: true, TabRailOrder: []string{"activity-inbox", "dms", "bogus", "dms"},
		TabRailCustomization: map[string]bool{"unified-files": false, "later": false, "home": false}} // Home can't be turned off
	r := railFor(p, testGroups)
	if got := strings.Join(r.Available, ","); got != "home,activity-inbox,dms,unified-files,later,platform" {
		t.Fatalf("order (Ozeq splice) = %s", got)
	}
	show, over := OrganizeRail(r, 1000, roomy)
	if got := strings.Join(show, ","); got != "home,activity-inbox,dms,platform,browse" {
		t.Errorf("rail = %s", got)
	}
	if got := strings.Join(over, ","); got != "unified-files,later" {
		t.Errorf("More = %s", got)
	}
}

func TestRailOverflowsByHeight(t *testing.T) {
	r := railFor(store.Prefs{HasStarredItem: true}, testGroups)
	// room for 3 tabs and a sliver: More takes the third tab's place
	show, over := OrganizeRail(r, 3*68+10, roomy)
	if got := strings.Join(show, ","); got != "home,dms,browse" {
		t.Errorf("rail = %s", got)
	}
	if got := strings.Join(over, ","); got != "activity-inbox,unified-files,later,platform" {
		t.Errorf("More = %s", got)
	}
}

func TestRailLabelsAndActivityVariants(t *testing.T) {
	r := railFor(store.Prefs{}, map[string]string{})
	if got := strings.Join(r.Rail, ","); got != "home,dms,activity,unified-files" { // no agent_tasks: platform hidden
		t.Errorf("rail = %s", got)
	}
	if !strings.Contains(strings.Join(r.Available, ","), "platform") || r.Tabs[TabPlatform].Label != "Tools" || r.Tabs[TabLater].Label != "Later" {
		t.Errorf("available %v, labels %+v %+v", r.Available, r.Tabs[TabPlatform], r.Tabs[TabLater])
	}
	if r := railFor(store.Prefs{}, map[string]string{"unified_global_triage": "on"}); strings.Contains(strings.Join(r.Rail, ","), "activity") {
		t.Errorf("activity moved into Home still a tab: %v", r.Rail)
	}
}

func TestHomeBadgeRule(t *testing.T) {
	inbox := RailState{ActivityInbox: true, DMsInRail: true}
	if n, dot := inbox.HomeBadge(true, 3); n != 0 || !dot {
		t.Errorf("inbox + DMs tab: count=%d dot=%v (want a dot only)", n, dot)
	}
	if n, dot := inbox.HomeBadge(false, 3); n != 0 || dot {
		t.Errorf("no channel unreads: %d %v", n, dot)
	}
	optOut := RailState{ActivityInbox: true, DMsOptOut: true}
	if n, _ := optOut.HomeBadge(false, 2); n != 2 {
		t.Errorf("DMs opted out and no DMs tab: Home counts DMs, got %d", n)
	}
	classic := RailState{}
	if n, _ := classic.HomeBadge(true, 4); n != 4 {
		t.Errorf("no inbox, no DMs tab: count %d", n)
	}
}

func TestRailPrefsParsed(t *testing.T) {
	raw := map[string]json.RawMessage{
		"tab_rail_customization":       json.RawMessage(`"{\"later\":false}"`),
		"tab_rail_customization_order": json.RawMessage(`"[\"activity-inbox\",\"home\"]"`),
		"has_starred_item":             json.RawMessage(`true`),
	}
	p, _ := store.ParsePrefs(raw)
	if !p.TabRailOrderSet || strings.Join(p.TabRailOrder, ",") != "activity-inbox,home" || p.TabRailCustomization["later"] || !p.HasStarredItem {
		t.Errorf("parsed %+v", p)
	}
}

func icon(url88 string) json.RawMessage {
	b, _ := json.Marshal(map[string]any{"image_34": "https://img.example/34.png", "image_88": url88,
		"image_132": "https://img.example/132.png", "image_default": false})
	return b
}

func TestPickTeamIcon(t *testing.T) {
	if u := pickTeamIcon(icon("https://img.example/88.png"), 36); u != "https://img.example/88.png" {
		t.Errorf("36px button: %s", u)
	}
	if u := pickTeamIcon(json.RawMessage(`{"image_88":"x","image_default":true}`), 36); u != "" {
		t.Errorf("default icon should draw initials: %q", u)
	}
	if u := pickTeamIcon(icon("y"), 100); u != "https://img.example/132.png" { // largest when none is big enough
		t.Errorf("200px: %s", u)
	}
}

func TestGridWorkspaceIconAndRotation(t *testing.T) {
	a, f := testApp(t)
	a.C.Creds.EnterpriseID, a.C.Creds.Team = "E1", "T1"
	a.S.PutUsers([]slack.User{{ID: "USELF", Name: "me", EnterpriseUser: &slack.EnterpriseUser{Teams: []string{"T1"}}}})
	f.reply["team.prefs.get"] = `{"ok":true,"prefs":{"use_workspace_icon_for_single_workspace_users":true}}`
	f.reply["team.info"] = `{"ok":true,"team":{"id":"T1","icon":{"image_88":"https://img.example/shrimp-1.png"}}}`
	close(a.ready)
	evs, cancel := a.Subscribe()
	defer cancel()
	a.refreshTeamIcon(context.Background())
	if got := f.last("team.info").Get("team"); got != "T1" {
		t.Errorf("single workspace + pref: asked team.info for %q, want the workspace", got)
	}
	if got := a.Snapshot().Team.Icon; got != "https://img.example/shrimp-1.png" {
		t.Fatalf("icon = %s", got)
	}
	// live: team_icon_change replaces it at once
	a.handle(rtm("team_icon_change", map[string]any{"team_id": "T1", "icon": map[string]any{"image_88": "https://img.example/shrimp-2.png"}}))
	next(t, evs, "team")
	if got := a.Snapshot().Team.Icon; got != "https://img.example/shrimp-2.png" {
		t.Errorf("after team_icon_change: %s", got)
	}
	// polling: the next team.info brings the third icon
	f.reply["team.info"] = `{"ok":true,"team":{"id":"T1","icon":{"image_88":"https://img.example/shrimp-3.png"}}}`
	a.refreshTeamIcon(context.Background())
	next(t, evs, "team")
	if got := a.Snapshot().Team.Icon; got != "https://img.example/shrimp-3.png" {
		t.Errorf("after a poll: %s", got)
	}
	// two workspaces: the org icon
	a.S.PutUsers([]slack.User{{ID: "USELF", Name: "me", EnterpriseUser: &slack.EnterpriseUser{Teams: []string{"T1", "T2"}}}})
	if got := a.iconTeam(); got != "E1" {
		t.Errorf("two workspaces: icon team %s, want the org", got)
	}
}

// A Slackbot custom response's sender, as Slack's reskin draws it: the org
// on Enterprise Grid (selfTeamIds.enterpriseId || teamId), with its name and
// the icon for a 36px avatar; the workspace's until the org has been read.
func TestCustomResponseTeam(t *testing.T) {
	a, f := testApp(t)
	a.C.Creds.EnterpriseID, a.C.Creds.Team = "E1", "T1"
	a.S.Lock()
	a.S.TeamID, a.S.TeamName = "T1", "Synthetic Workspace"
	a.S.Unlock()
	a.setTeamIcon("T1", icon("https://img.example/ws-88.png"))
	if name, ic := a.CustomResponseTeam(36); name != "Synthetic Workspace" || ic != "https://img.example/ws-88.png" {
		t.Errorf("before the org is read: %q %q, want the workspace's", name, ic)
	}
	// two workspaces: the rail asks for the org, and so do custom responses
	a.S.PutUsers([]slack.User{{ID: "USELF", Name: "me", EnterpriseUser: &slack.EnterpriseUser{Teams: []string{"T1", "T2"}}}})
	f.reply["team.prefs.get"] = `{"ok":true,"prefs":{}}`
	f.reply["team.info"] = `{"ok":true,"team":{"id":"E1","name":"Synthetic Org","icon":{"image_88":"https://img.example/org-88.png"}}}`
	a.refreshTeamIcon(context.Background())
	if got := f.last("team.info").Get("team"); got != "E1" {
		t.Errorf("team.info for %q, want the org", got)
	}
	if name, ic := a.CustomResponseTeam(36); name != "Synthetic Org" || ic != "https://img.example/org-88.png" {
		t.Errorf("custom response sender %q %q, want the org's", name, ic)
	}
	// the experiment gates it; Slack's force overrides (and BringBackSlackbot) turn it off
	if !a.CustomResponseReskin() {
		t.Error("reskin_custom_responses should default to on (Hack Club has it)")
	}
	a.SetExperimentGroup("reskin_custom_responses", "off")
	if a.CustomResponseReskin() || a.Experiment("filter_dormant_channels") != "on" {
		t.Error("SetExperimentGroup: the group didn't change, or the other defaults were lost")
	}
	// isCustomResponse needs Slackbot as the author, not just the subtype
	if m := a.message("C1", &slack.Message{TS: "1.1", User: "USLACKBOT", Subtype: SubtypeSlackbotResponse}); !m.CustomResponse() {
		t.Error("Slackbot's slackbot_response isn't a custom response")
	}
	if m := a.message("C1", &slack.Message{TS: "1.2", User: "UOTHER", Subtype: SubtypeSlackbotResponse}); m.CustomResponse() {
		t.Error("a slackbot_response from someone else is drawn as the team")
	}
}
