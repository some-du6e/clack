package core

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// useCanEditTabs (mwX5 Ln) over the channel managers store (hRtB), which
// only the managers reads fill (evGV) and which outlives a restart.
func TestCanEditTabsLikeSlack(t *testing.T) {
	a, f := testApp(t)
	props := json.RawMessage(`{"who_can_manage_tabs":"channel_managers"}`)
	a.S.PutConv(slack.Conversation{ID: "C2", Name: "managed", IsChannel: true, IsMember: true, Creator: "USELF", Properties: props})
	a.S.PutConv(slack.Conversation{ID: "C3", Name: "old", IsChannel: true, IsMember: true, IsArchived: true})
	a.setTeamPlan("std", "")
	f.reply["info"] = `{"ok":true,"allowed":[]}`

	if !a.CanEditTabs("C1") {
		t.Error("everyone may edit a channel's tabs without who_can_manage_tabs")
	}
	if a.CanEditTabs("C3") || a.CanEditTabs("C9") {
		t.Error("an archived or unknown channel's tabs are editable")
	}
	waitGrants(t, a, "C2")
	if a.CanEditTabs("C2") {
		t.Error("channel_managers on a paid team: the creator who isn't a manager may edit")
	}
	// an admin doesn't pass on a paid team either (Slack lets only
	// managers and who may assign them)
	a.S.Lock()
	a.S.Self.IsAdmin = true
	a.S.Unlock()
	u := *mustUser(a.S, "USELF")
	u.IsAdmin = true
	a.S.PutUsers([]slack.User{u})
	if !a.CanEditTabs("C2") {
		t.Error("canUserEditChannelManagers: a workspace admin may edit the tabs")
	}
	u.IsAdmin = false
	a.S.PutUsers([]slack.User{u})
	a.S.Lock()
	a.S.Self.IsAdmin = false
	a.S.Unlock()

	// the details' managers read fills the store: now a manager
	f.reply["admin.roles.entity.listAssignments"] = `{"ok":true,"role_assignments":[{"role_id":"Rl0A","users":["USELF","U1"]}]}`
	if _, err := a.Managers(context.Background(), "C2"); err != nil {
		t.Fatal(err)
	}
	if got := f.last("admin.roles.entity.listAssignments"); got.Get("entity_id") != "C2" || got.Get("reason") != "fetch-channel-managers" {
		t.Errorf("listAssignments args = %v", got)
	}
	if !a.CanEditTabs("C2") {
		t.Error("a channel manager may not edit the tabs")
	}

	// ASSIGN_CHANNEL_MANAGER lets a member in too
	a.setChannelManagers("C2", []string{"U1"})
	forgetGrants(a)
	f.reply["info"] = `{"ok":true,"allowed":["ASSIGN_CHANNEL_MANAGER"]}`
	waitGrants(t, a, "C2")
	if !a.CanEditTabs("C2") {
		t.Error("ASSIGN_CHANNEL_MANAGER doesn't let the viewer edit the tabs")
	}
	forgetGrants(a)
	f.reply["info"] = `{"ok":true,"allowed":[]}`
	waitGrants(t, a, "C2")

	// a free team: the creator (or an admin) may
	a.setTeamPlan("", "")
	if !a.CanEditTabs("C2") {
		t.Error("free team: the channel's creator may not edit the tabs")
	}
	a.S.PutConv(slack.Conversation{ID: "C4", Name: "theirs", IsChannel: true, IsMember: true, Creator: "U2", Properties: props})
	if a.CanEditTabs("C4") {
		t.Error("free team: a member who didn't create the channel may edit")
	}

	// Grid with no who_can_manage_tabs: the MANAGE_CHANNEL_TABS grant
	a.setTeamPlan("enterprise", "E1")
	waitGrants(t, a, "C1")
	if a.CanEditTabs("C1") {
		t.Error("Grid: tabs editable without MANAGE_CHANNEL_TABS")
	}
	forgetGrants(a)
	f.reply["info"] = `{"ok":true,"allowed":["MANAGE_CHANNEL_TABS"]}`
	waitGrants(t, a, "C1")
	if !a.CanEditTabs("C1") {
		t.Error("Grid: MANAGE_CHANNEL_TABS doesn't let the viewer edit")
	}
}

// The store is persisted (persistenceEnabled): a restart reads the
// managers the last session fetched.
func TestChannelManagersPersist(t *testing.T) {
	dir := t.TempDir()
	f := newFake(t)
	f.reply["admin.roles.entity.listAssignments"] = `{"ok":true,"role_assignments":[{"role_id":"Rl0A","users":["USELF"]}]}`
	mk := func() *App {
		c := slack.New(slack.Creds{Token: "xoxc-test", UserID: "USELF", Team: "T1", TeamURL: f.srv.URL + "/"})
		c.EdgeURL = f.srv.URL + "/edge/"
		s := store.New(dir)
		s.PutUsers([]slack.User{{ID: "USELF", Name: "me"}})
		s.PutConv(slack.Conversation{ID: "C2", Name: "managed", IsChannel: true, IsMember: true,
			Properties: json.RawMessage(`{"who_can_manage_tabs":"channel_managers"}`)})
		a := New(c, s)
		a.S.Self = *mustUser(s, "USELF")
		a.setTeamPlan("std", "")
		return a
	}
	a := mk()
	if _, err := a.Managers(context.Background(), "C2"); err != nil {
		t.Fatal(err)
	}
	b := mk()
	dl := time.Now().Add(2 * time.Second)
	for !b.CanEditTabs("C2") {
		if time.Now().After(dl) {
			t.Fatalf("the restarted app forgot the managers: %v", b.ChannelManagerIDs("C2"))
		}
		time.Sleep(10 * time.Millisecond)
	}
}
