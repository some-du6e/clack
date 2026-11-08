package core

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
)

// Synthetic fixtures only. Each case mirrors a branch of Slack's web client
// cited in sidebar_rules.go.

var dormantProps = json.RawMessage(`{"is_dormant":true}`)

// rulesApp: channels general (C1, active), old (C2, dormant), loud (C3,
// dormant with a mention), quiet (C4, muted, read), news (C5, unread).
func rulesApp(t *testing.T, p store.Prefs) *App {
	a, _ := testApp(t)
	a.S.PutConv(slack.Conversation{ID: "C2", Name: "old", IsChannel: true, Properties: dormantProps})
	a.S.PutConv(slack.Conversation{ID: "C3", Name: "loud", IsChannel: true, Properties: dormantProps})
	a.S.PutConv(slack.Conversation{ID: "C4", Name: "quiet", IsChannel: true})
	a.S.PutConv(slack.Conversation{ID: "C5", Name: "news", IsChannel: true})
	a.S.SetCounts(&slack.Counts{Channels: []slack.CountEntry{
		{ID: "C1", Latest: "100.0", LastRead: "100.0"},
		{ID: "C2", Latest: "50.0", LastRead: "50.0"},
		{ID: "C3", Latest: "60.0", LastRead: "10.0", HasUnreads: true, MentionCount: 1},
		{ID: "C4", Latest: "70.0", LastRead: "70.0"},
		{ID: "C5", Latest: "80.0", LastRead: "10.0", HasUnreads: true},
	}})
	a.S.SetPrefs(p, map[string]bool{"C4": true})
	select {
	case <-a.ready:
	default:
		close(a.ready)
	}
	return a
}

func TestActiveOnlyHidesDormantChannels(t *testing.T) {
	// hide_inactive_channels -> "active": dormant channels go, unless they
	// have a badge (improve_mute_pref); muted read channels go too.
	a := rulesApp(t, store.Prefs{SidebarBehavior: "hide_inactive_channels"})
	if got := sectionsByType(a)["channels"]; got != "C1,C3,C5" { // general, loud, news
		t.Errorf("Active only = %s", got)
	}
	// filter_dormant_channels off: "active" hides nothing dormant
	a.setExperiments(map[string]string{"improve_mute_pref": "on"})
	if got := sectionsByType(a)["channels"]; got != "C1,C3,C5,C2" { // old is back; muted quiet still hidden
		t.Errorf("without filter_dormant_channels = %s", got)
	}
}

func TestAllConversationsKeepsEverything(t *testing.T) {
	a := rulesApp(t, store.Prefs{SidebarBehavior: ""}) // "All"
	// improve_mute_pref on hides muted items unless shown outside menus
	if got := sectionsByType(a)["channels"]; got != "C1,C3,C5,C2" {
		t.Errorf("All (muted hidden) = %s", got)
	}
	a.S.SetPrefs(store.Prefs{ShowMutedOutside: true}, map[string]bool{"C4": true})
	if got := sectionsByType(a)["channels"]; got != "C1,C3,C5,C2,C4" { // muted last
		t.Errorf("All + show muted = %s", got)
	}
}

func TestUnreadsOnlyAndMentionsOnly(t *testing.T) {
	a := rulesApp(t, store.Prefs{SidebarBehavior: "hide_read_channels"})
	if got := sectionsByType(a)["channels"]; got != "C3,C5" {
		t.Errorf("Unreads only = %s", got)
	}
	a.S.SetPrefs(store.Prefs{SidebarBehavior: "show_mentions_only"}, map[string]bool{"C4": true})
	if got := sectionsByType(a)["channels"]; got != "C3" {
		t.Errorf("Mentions only = %s", got)
	}
}

func TestCustomBySectionPrefs(t *testing.T) {
	// "Custom by section": sidebar_behavior hide_read_channels_unless_starred
	// makes each section's channel_sections "sidebar" pref count.
	p := store.Prefs{SidebarBehavior: "hide_read_channels_unless_starred",
		SectionPrefs: map[string]map[string]string{"LCH": {"sidebar": "all", "sort": "recent"}}}
	a := rulesApp(t, p)
	x := slack.Section{ID: "LCH", Type: "channels"}
	a.S.Sections = []slack.Section{x}
	// all + recent: newest first, muted hidden (improve_mute_pref)
	if got := sectionsByType(a)["channels"]; got != "C1,C5,C3,C2" {
		t.Errorf("custom all/recent = %s", got)
	}
	// without a section pref the Channels section defaults to "hid"
	a.S.SetPrefs(store.Prefs{SidebarBehavior: "hide_read_channels_unless_starred"}, map[string]bool{"C4": true})
	if got := sectionsByType(a)["channels"]; got != "C3,C5" {
		t.Errorf("unless starred default = %s", got)
	}
	// the "sidebar" pref is ignored under any other sidebar_behavior
	a.S.SetPrefs(store.Prefs{SidebarBehavior: "", SectionPrefs: map[string]map[string]string{"LCH": {"sidebar": "mentions"}}}, map[string]bool{"C4": true})
	if got := sectionsByType(a)["channels"]; got != "C1,C3,C5,C2" { // "All" (muted hidden)
		t.Errorf("pref leaked outside custom mode: %s", got)
	}
}

func TestBoostMentionsGroupsFirst(t *testing.T) {
	a := rulesApp(t, store.Prefs{BoostMentions: true, ShowMutedOutside: true})
	// mentions group first, then the rest alphabetically, muted last
	if got := sectionsByType(a)["channels"]; got != "C3,C1,C5,C2,C4" {
		t.Errorf("boost_mentions = %s", got)
	}
}

func TestDMSectionSortsByRecency(t *testing.T) {
	a, _ := testApp(t)
	a.S.PutUsers([]slack.User{{ID: "U3", Name: "carol"}, {ID: "UX", Name: "gone", Deleted: true}})
	a.S.PutConv(slack.Conversation{ID: "D2", IsIM: true, User: "U2"})
	a.S.PutConv(slack.Conversation{ID: "D3", IsIM: true, User: "U3"})
	a.S.PutConv(slack.Conversation{ID: "DS", IsIM: true, User: "USELF"})
	a.S.PutConv(slack.Conversation{ID: "DX", IsIM: true, User: "UX"})      // deleted user: never listed
	a.S.PutConv(slack.Conversation{ID: "DQ", IsIM: true, User: "USEARCH"}) // system user: never listed
	a.S.SetCounts(&slack.Counts{IMs: []slack.CountEntry{{ID: "D1", Latest: "10.0"}, {ID: "D2", Latest: "30.0"},
		{ID: "D3", Latest: "20.0"}, {ID: "DS", Latest: "40.0"}, {ID: "DX", Latest: "50.0"}}})
	close(a.ready)
	// dm_recency_sort_rollout: newest first; the self-DM sorts last in the
	// DM section (youNewTeam group)
	if got := sectionsByType(a)["direct_messages"]; got != "D2,D3,D1,DS" {
		t.Errorf("recent DMs = %s", got)
	}
	a.setExperiments(map[string]string{}) // rollout off: channel_sort "default" = alphabetical
	if got := sectionsByType(a)["direct_messages"]; got != "D1,D2,D3,DS" {
		t.Errorf("alphabetical DMs = %s", got)
	}
}

func TestClosedUnreadDMStillListed(t *testing.T) {
	a, _ := dmApp(t)
	a.S.SetCounts(&slack.Counts{IMs: []slack.CountEntry{{ID: "D4", Latest: "5.0", LastRead: "1.0", HasUnreads: true, MentionCount: 1}}})
	if got := sectionsByType(a)["direct_messages"]; !strings.Contains(got, "D4") || strings.Contains(got, "D2") {
		t.Errorf("closed+unread D4 should show, closed+read D2 not: %s", got)
	}
}

func TestExperimentsFromAPI(t *testing.T) {
	a, f := testApp(t)
	f.reply["experiments.getByUser"] = `{"ok":true,"assignments":{"filter_dormant_channels":{"group":"control"},"dm_recency_sort_rollout":{"group":"with_edu"}}}`
	g, err := a.C.ExperimentGroups(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	a.setExperiments(g)
	if e := a.sidebarExperiments(); e.filterDormant || !e.dmRecency || e.improveMute {
		t.Errorf("experiments = %+v", e)
	}
}

func TestSectionPrefsParsed(t *testing.T) {
	raw := map[string]json.RawMessage{
		"channel_sections":                       json.RawMessage(`"{\"L1\":{\"c\":\"1\",\"sidebar\":\"hid\"},\"L2\":{\"c\":0}}"`),
		"show_muted_items_outside_sidebar_menus": json.RawMessage(`true`),
		"boost_mentions":                         json.RawMessage(`true`),
	}
	p, _ := store.ParsePrefs(raw)
	if p.SectionPrefs["L1"]["c"] != "1" || p.SectionPrefs["L1"]["sidebar"] != "hid" || p.SectionPrefs["L2"]["c"] != "0" || !p.ShowMutedOutside || !p.BoostMentions {
		t.Errorf("parsed %+v", p)
	}
	a, _ := testApp(t)
	a.S.SetPrefs(p, nil)
	a.S.Sections = []slack.Section{{ID: "L1", Type: "standard", Name: "A", Next: "L2"}, {ID: "L2", Type: "standard", Name: "B"}}
	close(a.ready)
	for _, s := range a.Snapshot().Sections {
		if (s.ID == "L1") != s.Collapsed && (s.ID == "L1" || s.ID == "L2") {
			t.Errorf("section %s collapsed=%v", s.ID, s.Collapsed)
		}
	}
}

func TestOrderSectionsLikeSlack(t *testing.T) {
	ids := func(ss []slack.Section) string {
		var out []string
		for _, s := range ss {
			out = append(out, s.ID)
		}
		return strings.Join(out, ",")
	}
	// two heads: the longer chain goes first
	got := ids(orderSections([]slack.Section{{ID: "A", Next: "B"}, {ID: "X"}, {ID: "B", Next: "C"}, {ID: "C"}}))
	if got != "A,B,C,X" {
		t.Errorf("two chains = %s", got)
	}
	// a cycle (every section pointed to) starts at the first section
	got = ids(orderSections([]slack.Section{{ID: "B", Next: "C"}, {ID: "C", Next: "A"}, {ID: "A", Next: "B"}}))
	if got != "B,C,A" {
		t.Errorf("cycle = %s", got)
	}
}
