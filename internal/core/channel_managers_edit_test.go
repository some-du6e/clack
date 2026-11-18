package core

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// Synthetic fixtures only: the roster menu's Channel Manager items
// (IaDetailsPopoverMemberItemActionsMenu's q and T over yfw4 B) and the
// admin.roles writes behind them (rwVl, HVho).

// managersApp is testApp with a public channel C2 the viewer is in on a
// paid team, U1 its manager, and a guest, an external member, a bot and
// a deactivated member to ask about.
func managersApp(t *testing.T, grants string) (*App, *fakeSlack) {
	t.Helper()
	a, f := testApp(t)
	a.setTeamPlan("std", "")
	a.S.PutConv(slack.Conversation{ID: "C2", Name: "managed", IsChannel: true, IsMember: true})
	a.S.PutUsers([]slack.User{
		{ID: "UGUEST", Name: "guest", IsRestricted: true},
		{ID: "UXTRN", Name: "partner", TeamID: "TOTHER", Profile: slack.Profile{Team: "TOTHER"}},
	})
	a.setChannelManagers("C2", []string{"U1"})
	f.reply["info"] = `{"ok":true,"allowed":[` + grants + `]}`
	waitGrants(t, a, "C2")
	return a, f
}

func TestChannelManagerItemsLikeSlack(t *testing.T) {
	a, _ := managersApp(t, `"ASSIGN_CHANNEL_MANAGER"`)
	if !a.CanEditChannelManagers("C2") {
		t.Fatal("ASSIGN_CHANNEL_MANAGER doesn't let a member edit the managers")
	}
	for _, c := range []struct {
		id           string
		remove, make bool
	}{
		{"U1", true, false},      // a manager: Remove from Channel Managers
		{"U2", false, true},      // a member: Make Channel Manager
		{"USELF", false, true},   // the viewer's own row too
		{"UGUEST", false, false}, // is_restricted
		{"UXTRN", false, false},  // external
		{"UB", false, false},     // a bot under agent_channel_header (the account's on)
		{channelExpertID, false, false},
	} {
		if got := a.CanRemoveFromChannelManagers("C2", c.id); got != c.remove {
			t.Errorf("%s: Remove from Channel Managers %v, want %v", c.id, got, c.remove)
		}
		if got := a.CanMakeChannelManager("C2", c.id); got != c.make {
			t.Errorf("%s: Make Channel Manager %v, want %v", c.id, got, c.make)
		}
	}
	// agent_channel_header off: a bot may be made a manager
	a.SetExperimentGroup(ExpAgentChannelHeader, "off")
	if !a.CanMakeChannelManager("C2", "UB") {
		t.Error("agent_channel_header off: a bot can't be made a manager")
	}
	a.SetExperimentGroup(ExpAgentChannelHeader, "on")
	// hall_pass: a channel visitor can't be made one
	a.SetExperimentGroup(ExpHallPass, "on")
	a.S.PutConv(slack.Conversation{ID: "C2", Name: "managed", IsChannel: true, IsMember: true,
		Properties: json.RawMessage(`{"channel_visitor_ids":["U2"]}`)})
	if a.CanMakeChannelManager("C2", "U2") {
		t.Error("a channel visitor can be made a manager")
	}
	a.SetExperimentGroup(ExpHallPass, "off")
	if !a.CanMakeChannelManager("C2", "U2") {
		t.Error("without hall_pass channel_visitor_ids counts")
	}
	// a channel the viewer isn't in (read with conversations.info), not
	// an admin: nothing
	a.S.PutExtConv(slack.Conversation{ID: "C3", Name: "elsewhere", IsChannel: true})
	waitGrants(t, a, "C3")
	if a.CanEditChannelManagers("C3") || a.CanMakeChannelManager("C3", "U2") {
		t.Error("a non-member edits the channel's managers")
	}
	// a DM, a group DM, an unknown conversation: never
	a.S.PutConv(slack.Conversation{ID: "G1", IsMPIM: true, IsMember: true})
	for _, ch := range []string{"D1", "G1", "C9"} {
		if a.CanEditChannelManagers(ch) {
			t.Errorf("%s: managers editable", ch)
		}
	}
	// a free team: never
	a.setTeamPlan("", "")
	if a.CanEditChannelManagers("C2") || a.CanRemoveFromChannelManagers("C2", "U1") {
		t.Error("free team: managers editable")
	}
}

// Without the grant only a workspace admin (acting as one in the
// channel, member or not) may edit the managers.
func TestChannelManagerItemsWithoutGrant(t *testing.T) {
	a, _ := managersApp(t, ``)
	if a.CanEditChannelManagers("C2") || a.CanMakeChannelManager("C2", "U2") || a.CanRemoveFromChannelManagers("C2", "U1") {
		t.Error("no grant, no admin: the items show")
	}
	u := *mustUser(a.S, "USELF")
	u.IsAdmin = true
	a.S.PutUsers([]slack.User{u})
	a.S.Lock()
	a.S.Self.IsAdmin = true
	a.S.Unlock()
	a.S.PutExtConv(slack.Conversation{ID: "C3", Name: "elsewhere", IsChannel: true})
	if !a.CanEditChannelManagers("C2") || !a.CanEditChannelManagers("C3") {
		t.Error("a workspace admin may not edit the managers")
	}
	if !a.CanMakeChannelManager("C2", "U2") || !a.CanRemoveFromChannelManagers("C2", "U1") {
		t.Error("a workspace admin doesn't get the items")
	}
}

// submitAddChannelManagers: admin.roles.addMembers {role_id Rl0A,
// role_scopes, user_ids, reason add-channel-managers}, ten users a
// request; who the requests took join the store, failed_for_some_users
// names the rest.
func TestAddChannelManagersLikeSlack(t *testing.T) {
	a, f := managersApp(t, `"ASSIGN_CHANNEL_MANAGER"`)
	ctx := context.Background()
	f.reply["admin.roles.addMembers"] = `{"ok":true}`
	if err := a.AddChannelManagers(ctx, "C2", []string{"U2"}); err != nil {
		t.Fatal(err)
	}
	p := f.last("admin.roles.addMembers")
	if p.Get("role_id") != "Rl0A" || p.Get("role_scopes") != "C2" || p.Get("user_ids") != "U2" || p.Get("reason") != "add-channel-managers" {
		t.Errorf("admin.roles.addMembers params %v", p)
	}
	if got := a.ChannelManagerIDs("C2"); !slices.Equal(got, []string{"U1", "U2"}) {
		t.Errorf("store %v, want [U1 U2]", got)
	}
	if !a.IsMemberChannelManager("C2", "U2") || a.CanMakeChannelManager("C2", "U2") || !a.CanRemoveFromChannelManagers("C2", "U2") {
		t.Error("the new manager's items didn't follow the store")
	}

	// twelve users: two requests, ten then two
	var ids []string
	for i := 0; i < 12; i++ {
		ids = append(ids, "UM"+string(rune('A'+i)))
	}
	before := countCalls(f, "admin.roles.addMembers")
	if err := a.AddChannelManagers(ctx, "C2", ids); err != nil {
		t.Fatal(err)
	}
	if n := countCalls(f, "admin.roles.addMembers") - before; n != 2 {
		t.Errorf("%d requests for 12 users, want 2", n)
	}
	if p := f.last("admin.roles.addMembers"); p.Get("user_ids") != "UMK,UML" {
		t.Errorf("the second batch %q", p.Get("user_ids"))
	}

	// failed_for_some_users: the named users fail, the others are added
	a.setChannelManagers("C2", []string{"U1"})
	f.reply["admin.roles.addMembers"] = `{"ok":false,"error":"failed_for_some_users","invalid_user_ids":["U3"]}`
	err := a.AddChannelManagers(ctx, "C2", []string{"U2", "U3"})
	var ue *UpdateChannelManagersError
	if !errors.As(err, &ue) || ue.Message != "error_adding_channel_managers" ||
		!slices.Equal(ue.Failed, []string{"U3"}) || !slices.Equal(ue.Succeeded, []string{"U2"}) {
		t.Fatalf("partial failure %#v", err)
	}
	if got := a.ChannelManagerIDs("C2"); !slices.Equal(got, []string{"U1", "U2"}) {
		t.Errorf("store after a partial failure %v", got)
	}

	// any other error fails every user of the request
	a.setChannelManagers("C2", []string{"U1"})
	f.reply["admin.roles.addMembers"] = `{"ok":false,"error":"not_permitted"}`
	err = a.AddChannelManagers(ctx, "C2", []string{"U4"})
	if !errors.As(err, &ue) || !slices.Equal(ue.Failed, []string{"U4"}) || len(ue.Succeeded) != 0 || ErrCode(err) != "not_permitted" {
		t.Fatalf("refused add %#v", err)
	}
	if got := a.ChannelManagerIDs("C2"); !slices.Equal(got, []string{"U1"}) {
		t.Errorf("a refused add changed the store: %v", got)
	}
}

// submitRemoveChannelManagers: admin.roles.removeMembers with reason
// remove-channel-managers; who it took leave the store.
func TestRemoveChannelManagersLikeSlack(t *testing.T) {
	a, f := managersApp(t, `"ASSIGN_CHANNEL_MANAGER"`)
	ctx := context.Background()
	a.setChannelManagers("C2", []string{"U1", "U2"})
	f.reply["admin.roles.removeMembers"] = `{"ok":true}`
	if err := a.RemoveChannelManagers(ctx, "C2", []string{"U1"}); err != nil {
		t.Fatal(err)
	}
	p := f.last("admin.roles.removeMembers")
	if p.Get("role_id") != "Rl0A" || p.Get("role_scopes") != "C2" || p.Get("user_ids") != "U1" || p.Get("reason") != "remove-channel-managers" {
		t.Errorf("admin.roles.removeMembers params %v", p)
	}
	if got := a.ChannelManagerIDs("C2"); !slices.Equal(got, []string{"U2"}) {
		t.Errorf("store %v, want [U2]", got)
	}
	if a.CanRemoveFromChannelManagers("C2", "U1") || !a.CanMakeChannelManager("C2", "U1") {
		t.Error("the removed manager's items didn't follow the store")
	}
	f.reply["admin.roles.removeMembers"] = `{"ok":false,"error":"not_permitted"}`
	err := a.RemoveChannelManagers(ctx, "C2", []string{"U2"})
	var ue *UpdateChannelManagersError
	if !errors.As(err, &ue) || ue.Message != "error_removing_channel_managers" || !slices.Equal(ue.Failed, []string{"U2"}) {
		t.Fatalf("refused remove %#v", err)
	}
	if got := a.ChannelManagerIDs("C2"); !slices.Equal(got, []string{"U2"}) {
		t.Errorf("a refused remove changed the store: %v", got)
	}
}
