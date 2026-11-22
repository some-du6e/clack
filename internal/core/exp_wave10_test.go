package core

import (
	"context"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// The roster's canRemoveMember (+VDd tu) under agent_channel_header:
// on, an app or bot is canUserRemoveAppFromChannel (QvQO I: the viewer
// can post there, then canUserKickFromChannel); off (and for people in
// both groups) canUserKickMemberFromChannel (8ZZ9 R6). Both need the
// REMOVE_FROM_CHANNEL grant (REMOVE_FROM_PRIVATE_CHANNEL in a private
// channel), never take Slackbot, and the kick check never takes the
// viewer.
func TestCanRemoveMemberFromChannel(t *testing.T) {
	a, f := testApp(t)
	ro := true
	a.S.PutConv(slack.Conversation{ID: "C2", Name: "lab", IsChannel: true, IsMember: true})
	a.S.PutConv(slack.Conversation{ID: "C3", Name: "secret", IsChannel: true, IsPrivate: true, IsMember: true})
	a.S.PutConv(slack.Conversation{ID: "C4", Name: "announce", IsChannel: true, IsMember: true, IsReadOnly: &ro})
	a.S.PutConv(slack.Conversation{ID: "C5", Name: "old", IsChannel: true, IsMember: true, IsArchived: true})
	a.S.PutConv(slack.Conversation{ID: "C6", Name: "general", IsChannel: true, IsMember: true, IsGeneral: true})
	a.S.PutUsers([]slack.User{{ID: SlackbotUserID, Name: "slackbot", IsBot: true}})
	f.reply["info"] = `{"ok":true,"allowed":[]}`
	for _, ch := range []string{"C2", "C3", "C4", "C5", "C6"} {
		waitGrants(t, a, ch)
	}

	for _, g := range []string{"on", "off", ""} {
		a.setExperiments(map[string]string{ExpAgentChannelHeader: g})
		if a.CanRemoveMemberFromChannel("C2", "UB") || a.CanRemoveMemberFromChannel("C2", "U1") {
			t.Errorf("%q: removable without REMOVE_FROM_CHANNEL", g)
		}
	}

	forgetGrants(a)
	f.reply["info"] = `{"ok":true,"allowed":["REMOVE_FROM_CHANNEL"]}`
	for _, ch := range []string{"C2", "C3", "C4", "C5", "C6"} {
		waitGrants(t, a, ch)
	}
	for _, tc := range []struct {
		group, ch, member string
		want              bool
	}{
		{"on", "C2", "UB", true},
		{"on", "C2", "U1", true},             // a person: the kick check
		{"on", "C2", "USELF", false},         // never the viewer
		{"on", "C2", SlackbotUserID, false},  // never Slackbot
		{"on", "C4", "UB", false},            // canUserRemoveAppFromChannel: the viewer can't post
		{"on", "C4", "U1", true},             // a person in a read-only channel: the kick check
		{"on", "C3", "UB", false},            // private: REMOVE_FROM_PRIVATE_CHANNEL
		{"on", "C5", "UB", false},            // archived
		{"on", "C6", "UB", false},            // #general (isChannelKickable)
		{"off", "C2", "UB", true},            // off: the kick check for the bot too
		{"off", "C4", "UB", true},            // which doesn't ask whether the viewer can post
		{"off", "C2", SlackbotUserID, false}, // Slackbot in both
		{"", "C4", "UB", true},               // unassigned is off
	} {
		a.setExperiments(map[string]string{ExpAgentChannelHeader: tc.group})
		if got := a.CanRemoveMemberFromChannel(tc.ch, tc.member); got != tc.want {
			t.Errorf("%q %s %s: removable %v, want %v", tc.group, tc.ch, tc.member, got, tc.want)
		}
	}

	forgetGrants(a)
	f.reply["info"] = `{"ok":true,"allowed":["REMOVE_FROM_PRIVATE_CHANNEL"]}`
	waitGrants(t, a, "C3")
	a.setExperiments(map[string]string{ExpAgentChannelHeader: "on"})
	if !a.CanRemoveMemberFromChannel("C3", "UB") {
		t.Error("REMOVE_FROM_PRIVATE_CHANNEL doesn't remove from a private channel")
	}

	// a guest removes no one
	u := *mustUser(a.S, "USELF")
	u.IsRestricted = true
	a.S.PutUsers([]slack.User{u})
	if a.CanRemoveMemberFromChannel("C3", "UB") || a.CanRemoveMemberFromChannel("C3", "U1") {
		t.Error("a guest may remove members")
	}
}

// Enterprise Grid: the org's MANAGE_PUBLIC_CHANNELS grant (getCanManage,
// 9WQJ LI) removes from a public channel without the channel's grant.
func TestCanRemoveMemberGridManager(t *testing.T) {
	a, f := testApp(t)
	a.setTeamPlan("enterprise", "E1")
	a.S.PutConv(slack.Conversation{ID: "C2", Name: "lab", IsChannel: true, IsMember: true})
	f.reply["info"] = `{"ok":true,"allowed":["MANAGE_PUBLIC_CHANNELS"]}`
	dl := time.Now().Add(5 * time.Second)
	for !a.TeamGrant(PermManagePublicChannels) { // the team's grants (permissions/info without a channel)
		if time.Now().After(dl) {
			t.Fatal("no team permissions/info answer")
		}
		time.Sleep(5 * time.Millisecond)
	}
	waitGrants(t, a, "C2")
	a.setExperiments(map[string]string{ExpAgentChannelHeader: "on"})
	if !a.CanRemoveMemberFromChannel("C2", "UB") {
		t.Error("MANAGE_PUBLIC_CHANNELS doesn't remove from a public channel")
	}
}

// submitKickFromChannel is conversations.kick {channel, user}.
func TestKickFromChannel(t *testing.T) {
	a, f := testApp(t)
	f.reply["conversations.kick"] = `{"ok":true}`
	if err := a.KickFromChannel(context.Background(), "C1", "UB"); err != nil {
		t.Fatal(err)
	}
	if p := f.last("conversations.kick"); p.Get("channel") != "C1" || p.Get("user") != "UB" {
		t.Errorf("conversations.kick args %v", p)
	}
	f.reply["conversations.kick"] = `{"ok":false,"error":"not_in_channel"}`
	if err := a.KickFromChannel(context.Background(), "C1", "UB"); ErrCode(err) != "not_in_channel" {
		t.Errorf("kick error %v", err)
	}
}

// RemoveMemberDialog's facts: the channel's privacy and record channel,
// the member's externality and the two experiments.
func TestRemoveMemberFacts(t *testing.T) {
	a, _ := testApp(t)
	a.S.PutConv(slack.Conversation{ID: "C3", Name: "secret", IsChannel: true, IsPrivate: true, IsMember: true})
	a.setExperiments(map[string]string{ExpAgentChannelHeader: "on", "sc_remove_full_access_user": "on"})
	f := a.RemoveMemberFacts("C3", "UB")
	if !f.Private || f.SfChannel || f.External || !f.AgentHeader || !f.ScRemoveFull {
		t.Errorf("private channel facts %+v", f)
	}
	a.setExperiments(map[string]string{})
	if f := a.RemoveMemberFacts("C1", "UB"); f.Private || f.AgentHeader || f.ScRemoveFull {
		t.Errorf("public channel, experiments unassigned: %+v", f)
	}
}

// agentConfigLocked is kzB8 r: a channel whose agent only its managers may
// manage locks Agentforce's removal for everyone else, under
// enable_native_channel_expert. Slack renamed the gate from
// ncea_allow_channel_managers_to_lock_configuration (build 8f19be21ed7e,
// the 2026-11-21 drift), so the old name turns nothing on.
func TestAgentConfigLockedFollowsNativeChannelExpert(t *testing.T) {
	a, _ := testApp(t)
	c := &slack.Conversation{ID: "C7", Name: "agents", IsChannel: true, IsMember: true,
		Properties: []byte(`{"who_can_manage_channel_agent":"channel_managers"}`)}
	a.SetExperimentGroup("ncea_allow_channel_managers_to_lock_configuration", "on")
	if a.agentConfigLocked(c) {
		t.Error("the old gate name locks the configuration")
	}
	a.SetExperimentGroup(ExpNativeChannelExpert, "on")
	if !a.agentConfigLocked(c) {
		t.Error("enable_native_channel_expert on: not locked for a viewer who isn't a manager")
	}
	everyone := &slack.Conversation{ID: "C8", Name: "open", IsChannel: true, IsMember: true,
		Properties: []byte(`{"who_can_manage_channel_agent":"everyone"}`)}
	if a.agentConfigLocked(everyone) {
		t.Error("a channel anyone may configure is locked")
	}
	a.SetExperimentGroup(ExpNativeChannelExpert, "")
	if a.agentConfigLocked(c) {
		t.Error("unassigned (Slack's null): locked")
	}
}
