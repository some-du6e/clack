package core

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// Synthetic fixtures only: Slack's sidebar rules that read drafts, huddles,
// the Slackbot link and the Starred / External connections sections
// (sidebar_rules.go, web client build 2c0d02ca130f).

// A closed DM stays a candidate while it has a draft (lJBI's
// hasDraftByChannelId: Slack's drafts.channelIds, attached drafts
// addressed to the conversation itself, not a reply) and leaves when the
// draft goes; composer drafts and drafts addressed to user ids alone have
// no channel key, so they keep nothing.
func TestClosedDMKeptByDraft(t *testing.T) {
	a, _ := dmApp(t) // D1 and D3 open; D2, D4, D5 closed and read
	if got := sectionsByType(a)["direct_messages"]; got != "D1,D3" {
		t.Fatalf("direct messages = %q, want the open D1,D3", got)
	}
	evs, stop := a.Subscribe()
	defer stop()
	a.putDraft(Draft{ID: "Dr1", Channel: "D2", LastUpdated: "1762500000000"})
	// the set moved: the sections are rebuilt
	next(t, evs, "sidebar_changed")
	a.putDraft(Draft{ID: "Dr2", Channel: "D4", ThreadTS: "1762400000.000100"}) // a reply
	a.putDraft(Draft{ID: "Dr3", Users: []string{"U5"}})                        // user ids only: no key
	a.putDraft(Draft{ID: "Dr4", Channel: "D5", FromComposer: true})            // a composer draft
	if got := sectionsByType(a)["direct_messages"]; got != "D1,D3,D2" {        // no latest message: last
		t.Errorf("with drafts = %q, want D2 kept by its draft only", got)
	}
	if k := a.drafts.sidebar; k != "D2" {
		t.Errorf("sidebar drafts = %q, want D2", k)
	}
	// a second draft there moves nothing; the last one going drops the DM
	a.putDraft(Draft{ID: "Dr5", Channel: "D2"})
	a.dropDraft("Dr1")
	if got := sectionsByType(a)["direct_messages"]; got != "D1,D3,D2" {
		t.Errorf("one of two drafts gone = %q", got)
	}
	a.dropDraft("Dr5")
	next(t, evs, "sidebar_changed")
	if got := sectionsByType(a)["direct_messages"]; got != "D1,D3" {
		t.Errorf("drafts gone = %q, want D2 closed again", got)
	}
	// drafts.list replaces the cache: a group DM with one stays too
	a.S.PutConv(slack.Conversation{ID: "G1", Name: "mpdm-alice--bob--me-1", IsMPIM: true, Members: []string{"USELF", "U1", "U2"}})
	a.mu.Lock()
	a.dms.open["G1"] = false
	a.mu.Unlock()
	if got := sectionsByType(a)["direct_messages"]; strings.Contains(got, "G1") {
		t.Fatalf("closed group DM listed: %q", got)
	}
	a.mu.Lock()
	a.drafts.byID = map[string]Draft{"Dr6": {ID: "Dr6", Channel: "G1"}}
	moved := a.sidebarDraftsMovedLocked()
	a.mu.Unlock()
	if !moved || !strings.Contains(sectionsByType(a)["direct_messages"], "G1") {
		t.Errorf("closed group DM with a draft: moved %v, %q", moved, sectionsByType(a)["direct_messages"])
	}
}

// A closed DM with a huddle on stays (huddleChannelIds, the huddle UI's
// rooms through SetHuddleChannels) and leaves once the huddle ends.
func TestClosedDMKeptByHuddle(t *testing.T) {
	a, _ := dmApp(t)
	live := map[string]bool{}
	a.SetHuddleChannels(func() map[string]bool { return live })
	if got := sectionsByType(a)["direct_messages"]; got != "D1,D3" {
		t.Fatalf("no huddle = %q", got)
	}
	live["D4"] = true
	if got := sectionsByType(a)["direct_messages"]; got != "D1,D3,D4" {
		t.Errorf("huddle in closed D4 = %q, want D4 kept", got)
	}
	if !a.selectionShapesSidebar("D2") || a.selectionShapesSidebar("D4") {
		t.Error("selecting a closed DM shapes the sidebar unless something keeps it")
	}
	delete(live, "D4")
	if got := sectionsByType(a)["direct_messages"]; got != "D1,D3" {
		t.Errorf("huddle over = %q", got)
	}
	// a deleted member's or an archived DM stays out, huddle or draft
	a.S.PutUsers([]slack.User{{ID: "U5", Name: "erin", Deleted: true}})
	a.S.PutConv(slack.Conversation{ID: "D2", IsIM: true, User: "U2", IsArchived: true})
	live["D5"], live["D2"] = true, true
	a.putDraft(Draft{ID: "Dr1", Channel: "D5"})
	if got := sectionsByType(a)["direct_messages"]; got != "D1,D3" {
		t.Errorf("deleted member / archived = %q", got)
	}
}

// The row filter (lJBI): in an expanded section a draft keeps a row any
// filter would hide, a dormant one under "active" and a muted one too; a
// huddle keeps a read row but not a dormant one under "active" or a muted
// one; a collapsed section keeps its unread rows only.
func TestDraftAndHuddleKeepRows(t *testing.T) {
	// rulesApp: general (C1, read), old (C2, dormant, read), loud (C3,
	// dormant, mention), quiet (C4, muted, read), news (C5, unread)
	a := rulesApp(t, store.Prefs{SidebarBehavior: "hide_read_channels"})
	live := map[string]bool{}
	a.SetHuddleChannels(func() map[string]bool { return live })
	if got := sectionsByType(a)["channels"]; got != "C3,C5" {
		t.Fatalf("Unreads only = %s", got)
	}
	a.putDraft(Draft{ID: "Dr1", Channel: "C1"})
	live["C2"] = true
	if got := sectionsByType(a)["channels"]; got != "C1,C3,C5,C2" { // general, loud, news, old
		t.Errorf("Unreads only with a draft in general and a huddle in old = %s", got)
	}
	// "active": the huddle doesn't save dormant old, a draft does
	a.S.SetPrefs(store.Prefs{SidebarBehavior: "hide_inactive_channels"}, map[string]bool{"C4": true})
	if got := sectionsByType(a)["channels"]; got != "C1,C3,C5" {
		t.Errorf("Active only, huddle in dormant old = %s", got)
	}
	a.putDraft(Draft{ID: "Dr2", Channel: "C2"})
	if got := sectionsByType(a)["channels"]; got != "C1,C3,C5,C2" {
		t.Errorf("Active only, draft in dormant old = %s", got)
	}
	// muted quiet, hidden (improve_mute_pref): a huddle doesn't bring it
	// back, a draft does (muted rows sort last)
	live["C4"] = true
	if got := sectionsByType(a)["channels"]; strings.Contains(got, "C4") {
		t.Errorf("huddle brought muted quiet back: %s", got)
	}
	a.putDraft(Draft{ID: "Dr3", Channel: "C4"})
	if got := sectionsByType(a)["channels"]; got != "C1,C3,C5,C2,C4" {
		t.Errorf("draft in muted quiet = %s", got)
	}
	// collapsed: unread rows only, whatever keeps the others open (loud,
	// dormant, goes under "active" there whatever its mention)
	for _, s := range a.Snapshot().Sections {
		if s.Type == "channels" {
			if got := strings.Join(s.CollapsedChannels, ","); got != "C5" {
				t.Errorf("collapsed = %s, want the unread news", got)
			}
			if strings.Contains(strings.Join(s.HiddenRead, ","), "C1") {
				t.Errorf("general, kept by its draft, is listed hidden: %v", s.HiddenRead)
			}
		}
	}
	// improve_mute_pref off: a draft or a huddle still keeps a read row
	a.setExperiments(map[string]string{"filter_dormant_channels": "on"})
	a.S.SetPrefs(store.Prefs{SidebarBehavior: "hide_read_channels"}, map[string]bool{"C4": true})
	delete(live, "C4")
	a.dropDraft("Dr3")
	if got := sectionsByType(a)["channels"]; got != "C1,C3,C5,C2" {
		t.Errorf("without improve_mute_pref = %s", got)
	}
}

// OpEF AZ: Slackbot's DM leaves the sidebar while Slackbot's own tab or
// link shows (Clack shows neither, so it stays in Apps), open or not, and
// gets no default section even while it is the open conversation.
func TestSlackbotDMHiddenWithItsSurfaces(t *testing.T) {
	a, _ := testApp(t)
	a.S.PutUsers([]slack.User{{ID: slackbotID, Name: "slackbot", IsBot: true}})
	a.S.PutConv(slack.Conversation{ID: "DSB", IsIM: true, User: slackbotID})
	a.S.SetCounts(&slack.Counts{IMs: []slack.CountEntry{{ID: "DSB", Latest: tsAgo(time.Hour), LastRead: tsAgo(time.Hour)}}})
	close(a.ready)
	listed := func() bool {
		for _, s := range a.Snapshot().Sections {
			for _, id := range s.Channels {
				if id == "DSB" {
					return true
				}
			}
		}
		return false
	}
	if !listed() {
		t.Fatal("Slackbot's DM isn't listed without Slackbot's tab or link")
	}
	if a.slackbotDMHidden() {
		t.Error("Clack shows Slackbot's tab or link")
	}
	a.mu.Lock()
	a.slackbotView = true
	a.mu.Unlock()
	if listed() {
		t.Error("Slackbot's DM listed while its link shows")
	}
	a.S.SetCounts(&slack.Counts{IMs: []slack.CountEntry{{ID: "DSB", Latest: tsAgo(time.Minute), LastRead: tsAgo(time.Hour), HasUnreads: true, MentionCount: 1}}})
	if listed() {
		t.Error("an unread Slackbot DM listed while its link shows")
	}
	a.SetSelected("DSB")
	if listed() {
		t.Error("the open Slackbot DM got a row while its link shows")
	}
	a.SetSelected("")
	a.mu.Lock()
	a.slackbotView = false
	a.mu.Unlock()
	if !listed() {
		t.Error("Slackbot's DM didn't come back")
	}
}

// Starred with nothing starred (lJBI's visibleChannelSections, m1KW's
// banner): shown, empty, with fewer than three custom sections, no VIPs
// and a viewer who isn't frontline; the banner also needs Starred first
// among the server's sections and no starred conversation that would
// show.
func TestStarredEmptyStates(t *testing.T) {
	a, _ := testApp(t)
	sec := func(id, typ, next string, chans ...string) slack.Section {
		x := slack.Section{ID: id, Type: typ, Name: id, Next: next}
		x.ChannelIDsPage.ChannelIDs = chans
		return x
	}
	base := []slack.Section{sec("LS", "stars", "LP"), sec("LP", "standard", "LC"), sec("LC", "channels", "LD"), sec("LD", "direct_messages", "")}
	a.S.Sections = base
	close(a.ready)
	starred := func() (shown, banner bool) {
		for _, s := range a.Snapshot().Sections {
			if s.Type == "stars" {
				return true, s.StarredBanner
			}
			if s.StarredBanner {
				t.Errorf("%s (%s) has the Starred banner", s.ID, s.Type)
			}
		}
		return false, false
	}
	check := func(what string, wantShown, wantBanner bool) {
		t.Helper()
		if shown, banner := starred(); shown != wantShown || banner != wantBanner {
			t.Errorf("%s: Starred shown %v banner %v, want %v %v", what, shown, banner, wantShown, wantBanner)
		}
	}
	// Code channels, made here, comes first; Starred is the server's first
	check("nothing starred", true, true)
	// a starred channel: Starred lists it, no banner
	a.setStarred("C1", true)
	check("starred general", true, false)
	// a starred DM that is closed counts for neither
	a.setSidebarDMs([]slack.SidebarDM{{ID: "D1", IsOpen: false}})
	a.setStarred("C1", false)
	a.setStarred("D1", true)
	check("starred closed DM", true, true)
	a.setStarred("D1", false)
	// Starred lower down: still shown, no banner
	a.S.Sections = []slack.Section{sec("LC", "channels", "LS"), sec("LS", "stars", "LP"), sec("LP", "standard", "LD"), sec("LD", "direct_messages", "")}
	check("Starred second", true, false)
	// three custom sections: hidden
	a.S.Sections = append(append([]slack.Section(nil), base...), sec("LQ", "standard", ""), sec("LR", "standard", ""))
	check("three custom sections", false, false)
	a.setStarred("C1", true)
	check("three custom sections, general starred", true, false)
	a.setStarred("C1", false)
	a.S.Sections = base
	// VIPs: hidden
	a.S.SetRawPrefs(map[string]json.RawMessage{"vip_users": json.RawMessage(`"U2"`)})
	check("VIPs", false, false)
	a.S.SetRawPrefs(map[string]json.RawMessage{})
	check("no VIPs", true, true)
	// a frontline viewer: hidden
	a.S.PutUsers([]slack.User{{ID: "USELF", Name: "me", RestrictedUserType: "frontline_worker"}})
	check("frontline", false, false)
}

// External connections (lJBI: showConnectSection && hasSharedChannels):
// shown while any candidate is externally shared, even when a custom
// section holds it, where the user's list puts it and under Slack's
// static name; nothing shared, no section.
func TestConnectSectionShowsForAnyShared(t *testing.T) {
	a, _ := testApp(t)
	a.S.PutConv(slack.Conversation{ID: "CX", Name: "partners", IsChannel: true, IsExtShared: true})
	sec := func(id, typ, name, next string, chans ...string) slack.Section {
		x := slack.Section{ID: id, Type: typ, Name: name, Next: next}
		x.ChannelIDsPage.ChannelIDs = chans
		return x
	}
	a.S.Sections = []slack.Section{sec("L1", "standard", "Partners", "L2", "CX"), sec("L2", "channels", "", "L3"),
		sec("L3", "slack_connect", "Slack Connect", "L4"), sec("L4", "direct_messages", "", "")}
	a.S.SetRawPrefs(map[string]json.RawMessage{"should_show_connect_section": json.RawMessage("true")})
	close(a.ready)
	layout := func() string {
		var out []string
		for _, s := range a.Snapshot().Sections {
			if s.Type == "coding_channels" {
				continue
			}
			out = append(out, s.Name+"="+strings.Join(s.Channels, ","))
		}
		return strings.Join(out, " ")
	}
	if got := layout(); got != "Partners=CX Channels=C1 External connections= Direct messages=D1" {
		t.Errorf("shared channel in a custom section: %s", got)
	}
	a.S.PutConv(slack.Conversation{ID: "CX", Name: "partners", IsChannel: true})
	if got := layout(); got != "Partners=CX Channels=C1 Direct messages=D1" {
		t.Errorf("nothing shared: %s", got)
	}
}
