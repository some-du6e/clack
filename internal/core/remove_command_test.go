package core

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// bulkAddServerCommands (slash_store.go): a server command named like a
// client command never reaches the store, an alias goes to its command's
// aliases, a name without "/" is skipped, and the store's keys are the
// canonical name, or app and canonical name with ia4_slash_command_fe.
func TestServerCommandStore(t *testing.T) {
	in := []slack.Command{
		{Name: "/remove", CanonicalName: "/remove", Type: "core"},
		{Name: "/kick", CanonicalName: "/kick", AliasOf: "/remove", Type: "core"},
		{Name: "/leave", Type: "core"},
		{Name: "/part", AliasOf: "/leave", Type: "core"},
		{Name: "/topic", Type: "core"},
		{Name: "/remind", Type: "core"},
		{Name: "/rem", AliasOf: "/remind", Type: "core"},
		{Name: "no-slash", Type: "app", AppID: "A1"},
		{Name: "/poll", Type: "app", AppID: "A1", Desc: "first"},
		{Name: "/poll", Type: "app", AppID: "A2", Desc: "second"},
		{Name: "/poll", Type: "app", AppID: "A1", Desc: "first again"}, // keyBy app-name: A1's replaced in place
		{Name: "/giphy", Type: "service"},
	}
	names := func(cs []slack.Command) (out []string) {
		for _, c := range cs {
			out = append(out, c.AppID+c.Name+"="+c.Desc)
		}
		return out
	}
	got := serverCommandStore(in, false)
	if want := []string{"/remind=", "A2/poll=second", "/giphy="}; !slices.Equal(names(got), want) {
		t.Errorf("without ia4: %v, want %v", names(got), want)
	}
	if i := slices.IndexFunc(got, func(c slack.Command) bool { return c.Name == "/remind" }); i < 0 || !slices.Equal(got[i].Aliases, []string{"/rem"}) {
		t.Errorf("/remind's aliases: %+v", got)
	}
	got = serverCommandStore(in, true)
	if want := []string{"/remind=", "A1/poll=first again", "A2/poll=second", "/giphy="}; !slices.Equal(names(got), want) {
		t.Errorf("with ia4: %v, want %v", names(got), want)
	}
	for _, c := range got {
		if localCommandNames[canonicalCommand(c)] || c.AliasOf != "" {
			t.Errorf("stored %+v", c)
		}
	}
}

// removeApp is a channel the viewer is in, its people, the grants and the
// membership answers removeCommand reads.
func removeApp(t *testing.T, perms ...string) (*App, *fakeSlack) {
	t.Helper()
	a, f := testApp(t)
	a.S.PutUsers([]slack.User{{ID: "U0ALICE001", Name: "alice", Profile: slack.Profile{DisplayName: "alice"}},
		{ID: "U0BOBBY001", Name: "bob", Profile: slack.Profile{DisplayName: "bob"}},
		{ID: "U0EXTERN01", Name: "ext", Profile: slack.Profile{DisplayName: "ext"}, TeamID: "T9"}})
	a.S.PutConv(slack.Conversation{ID: "C0LOUNGE01", Name: "lounge", IsChannel: true, IsMember: true})
	allow(f, perms...)
	f.reply["membership"] = `{"ok":true,"members":["U0ALICE001"],"non_members":["U0BOBBY001"]}`
	return a, f
}

func mentionOf(id string) []CommandParam { return []CommandParam{{Type: "mention", Value: id}} }

// removeCommand (p8LL) for each of its answers.
func TestRemoveCommand(t *testing.T) {
	a, f := removeApp(t, PermRemoveFromChannel)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.LoadChannelGrants(ctx, "C0LOUNGE01"); err != nil {
		t.Fatal(err)
	}
	// nobody named, or not a mention of someone who looks like a member
	for _, p := range [][]CommandParam{nil, {{Type: "string", Value: "alice"}}, mentionOf("C0LOUNGE01"), mentionOf("U1")} {
		if _, err := a.RemoveCommand("/kick", "C0LOUNGE01", p, "x"); feedbackOf(err) != "Please specify someone to remove!" {
			t.Errorf("%v: %v", p, err)
		}
	}
	// the membership is unknown until channels/membership answers: not a
	// member, and it is asked for
	if _, err := a.RemoveCommand("/kick", "C0LOUNGE01", mentionOf("U0ALICE001"), "@alice"); feedbackOf(err) != "*@alice* is not a member of this channel." {
		t.Errorf("before the membership answer: %v", err)
	}
	waitFor(t, "the membership answer", 5*time.Second, func() bool { m, ok := a.cachedMembership("C0LOUNGE01", "U0ALICE001"); return ok && m.Known })
	if rt, err := a.RemoveCommand("/kick", "C0LOUNGE01", mentionOf("U0ALICE001"), "@alice"); err != nil || rt != (RemoveTarget{Member: "U0ALICE001"}) {
		t.Errorf("a member: %+v %v", rt, err)
	}
	a.ChannelMembershipInfo(ctx, "C0LOUNGE01", []string{"U0BOBBY001"})
	if _, err := a.RemoveCommand("/kick", "C0LOUNGE01", mentionOf("U0BOBBY001"), "@bob"); feedbackOf(err) != "*@bob* is not a member of this channel." {
		t.Errorf("a non-member: %v", err)
	}
	// the viewer: maybeLeaveChannel
	if rt, err := a.RemoveCommand("/remove", "C0LOUNGE01", mentionOf("USELF"), "@me"); err == nil || rt.Leave {
		t.Errorf("USELF isn't member-shaped (looksLikeMemberId): %+v %v", rt, err)
	}
	a.S.PutUsers([]slack.User{{ID: "U0SELF0001", Name: "me2"}})
	a.S.Lock()
	a.S.Self.ID = "U0SELF0001" // a member-shaped viewer
	a.S.Unlock()
	if rt, err := a.RemoveCommand("/remove", "C0LOUNGE01", mentionOf("U0SELF0001"), "@me2"); err != nil || !rt.Leave {
		t.Errorf("the viewer: %+v %v", rt, err)
	}
	a.S.Lock()
	a.S.Self.ID = "USELF"
	a.S.Unlock()
	// the Channel Expert, and a member users/info doesn't know
	if _, err := a.RemoveCommand("/kick", "C0LOUNGE01", mentionOf("UCHANNELEXPERT"), "@agent"); feedbackOf(err) != "*this agent* can be removed in Channel Settings." {
		t.Errorf("the channel expert: %v", err)
	}
	a.S.PutNonexistent([]string{"U0GONE0001"})
	if _, err := a.RemoveCommand("/kick", "C0LOUNGE01", mentionOf("U0GONE0001"), "@gone"); feedbackOf(err) != "*@gone* is not a recognized member name." {
		t.Errorf("a non-existent member: %v", err)
	}
	if n := countCalls(f, "chat.command") + countCalls(f, "conversations.kick"); n != 0 {
		t.Errorf("%d calls from the checks", n)
	}
}

// canUserKickFromChannelWithReason's refusals, in removeCommand's words.
func TestRemoveCommandRefusals(t *testing.T) {
	a, _ := removeApp(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a.S.PutConv(slack.Conversation{ID: "C0SECRET01", Name: "secret", IsChannel: true, IsPrivate: true, IsMember: true})
	a.S.PutConv(slack.Conversation{ID: "C0ALLHANDS", Name: "all-hands", IsChannel: true, IsMember: true, IsGeneral: true})
	a.S.PutConv(slack.Conversation{ID: "C0OLDONE01", Name: "old", IsChannel: true, IsMember: true, IsArchived: true})
	a.S.PutConv(slack.Conversation{ID: "D0ALICE001", IsIM: true, User: "U0ALICE001"})
	for _, ch := range []string{"C0LOUNGE01", "C0SECRET01", "C0ALLHANDS", "C0OLDONE01", "D0ALICE001", ""} {
		_ = a.LoadChannelGrants(ctx, ch)
	}
	for ch, want := range map[string]string{
		"C0LOUNGE01": "Removing members from public channels has been restricted by a Workspace Owner or Admin. Try contacting them for help!",
		"C0SECRET01": "Removing someone from a private channel is a restricted action.",
		"C0ALLHANDS": "All members of this workspace are meant to be in *#all-hands*, so you won’t be able to remove anyone.",
		"C0OLDONE01": "You can’t remove anyone from *#old* while it is archived.",
		"D0ALICE001": "You can’t remove someone from a direct message.",
		"C0NOWHERE1": "Hmm, something went wrong. Try again?", // InvalidParams
	} {
		if _, err := a.RemoveCommand("/kick", ch, mentionOf("U0ALICE001"), "@alice"); feedbackOf(err) != want {
			t.Errorf("%s: %q, want %q", ch, feedbackOf(err), want)
		}
	}
	u := a.S.Self
	u.IsRestricted = true
	a.S.PutUsers([]slack.User{u})
	if _, err := a.RemoveCommand("/kick", "C0LOUNGE01", mentionOf("U0ALICE001"), "@alice"); feedbackOf(err) != "Removing someone from a channel is a restricted action." {
		t.Errorf("a guest: %v", err)
	}
}

// An external member of a Slack Connect channel: refused unless
// sc_remove_full_access_user and the channel is hosted here.
func TestRemoveCommandExternalMember(t *testing.T) {
	a, f := removeApp(t, PermRemoveFromChannel)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a.S.PutConv(slack.Conversation{ID: "C0SHARED01", Name: "shared", IsChannel: true, IsMember: true, IsExtShared: true})
	_ = a.LoadChannelGrants(ctx, "C0SHARED01")
	f.reply["membership"] = `{"ok":true,"members":["U0EXTERN01"],"non_members":[]}`
	a.ChannelMembershipInfo(ctx, "C0SHARED01", []string{"U0EXTERN01"})
	if !a.userOf(mustUser(a.S, "U0EXTERN01")).External {
		t.Skip("the fixture's member isn't external here")
	}
	a.SetExperimentGroup("sc_remove_full_access_user", "off")
	if _, err := a.RemoveCommand("/kick", "C0SHARED01", mentionOf("U0EXTERN01"), "@ext"); feedbackOf(err) != "You can’t remove @ext from this channel, because they’re from another organization." {
		t.Errorf("an external member: %v", err)
	}
	a.SetExperimentGroup("sc_remove_full_access_user", "on")
	if rt, err := a.RemoveCommand("/kick", "C0SHARED01", mentionOf("U0EXTERN01"), "@ext"); err != nil || rt.Member != "U0EXTERN01" {
		t.Errorf("hosted here, with sc_remove_full_access_user: %+v %v", rt, err)
	}
}

// The membership store (membership.go): a read with nothing held asks
// and answers not known; asks of 200 ms go together; a failed ask drops
// the members so the next read asks again; an IM's partner and the viewer
// are its members without asking.
func TestMembershipStore(t *testing.T) {
	a, f := removeApp(t)
	if m := a.membershipOf("C0LOUNGE01", "U0ALICE001"); m.Known {
		t.Fatalf("a cold read: %+v", m)
	}
	waitFor(t, "the ask", 5*time.Second, func() bool { m, ok := a.cachedMembership("C0LOUNGE01", "U0ALICE001"); return ok && m.Known && m.Member })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	before := countCalls(f, "membership")
	done := make(chan map[string]MemberInfo, 2)
	go func() { done <- a.ChannelMembershipInfo(ctx, "C0LOUNGE01", []string{"U0BOBBY001"}) }()
	go func() { done <- a.ChannelMembershipInfo(ctx, "C0LOUNGE01", []string{"U0BOBBY001", "U0ALICE001"}) }()
	r1, r2 := <-done, <-done
	if n := countCalls(f, "membership") - before; n != 1 {
		t.Errorf("%d channels/membership calls for two asks at once", n)
	}
	for _, r := range []map[string]MemberInfo{r1, r2} {
		if r["U0BOBBY001"] != (MemberInfo{Known: true}) {
			t.Errorf("bob: %+v", r)
		}
	}
	if r2["U0ALICE001"] != (MemberInfo{Known: true, Member: true}) {
		t.Errorf("alice, known already: %+v", r2)
	}
	// an answer without someone asked about fails them all
	f.mu.Lock()
	f.reply["membership"] = `{"ok":true,"members":[],"non_members":[]}`
	f.mu.Unlock()
	if got := a.ChannelMembershipInfo(ctx, "C0OTHER001", []string{"U0BOBBY001"}); len(got) != 0 {
		t.Errorf("a short answer: %+v", got)
	}
	if _, ok := a.cachedMembership("C0OTHER001", "U0BOBBY001"); ok {
		t.Error("a failed ask left its member held")
	}
	a.S.PutConv(slack.Conversation{ID: "D0ALICE001", IsIM: true, User: "U0ALICE001"})
	before = countCalls(f, "membership")
	got := a.ChannelMembershipInfo(ctx, "D0ALICE001", []string{"U0ALICE001", "USELF"})
	if got["U0ALICE001"] != (MemberInfo{Known: true, Member: true}) || got["USELF"] != (MemberInfo{Known: true, Member: true}) {
		t.Errorf("an IM's partner and the viewer: %+v", got)
	}
	if countCalls(f, "membership") != before {
		t.Error("asked about an IM's partner")
	}
}

// maybeLeaveChannel's decision (leave_channel.go) for each kind of
// channel, and canUserLeaveChannel's refusals.
func TestLeaveChannelAction(t *testing.T) {
	a, f := testApp(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	agentProps := json.RawMessage(`{"record_channel":{"record_id":"AC:1","record_type":"agent_channel"}}`)
	for _, c := range []slack.Conversation{
		{ID: "C0PUBLIC01", Name: "pub", IsChannel: true, IsMember: true},
		{ID: "C0SECRET01", Name: "secret", IsChannel: true, IsPrivate: true, IsMember: true},
		{ID: "C0OLDSECR1", Name: "old", IsChannel: true, IsPrivate: true, IsMember: true, IsArchived: true},
		{ID: "C0SHARED01", Name: "shared", IsChannel: true, IsPrivate: true, IsMember: true, IsExtShared: true},
		{ID: "C0ALLHANDS", Name: "all", IsChannel: true, IsMember: true, IsGeneral: true},
		{ID: "C0MANDATE1", Name: "must", IsChannel: true, IsMember: true, IsOrgMandatory: true},
		{ID: "C0AGENT001", Name: "agent", IsChannel: true, IsMember: true, Properties: agentProps},
	} {
		a.S.PutConv(c)
	}
	allow(f, PermArchiveChannel)
	_ = a.LoadChannelGrants(ctx, "C0AGENT001")
	counts := func(n int) {
		f.mu.Lock()
		f.reply["counts"] = `{"ok":true,"counts":{"people":` + string(rune('0'+n)) + `}}`
		f.mu.Unlock()
	}
	counts(3)
	for ch, want := range map[string][2]string{
		"C0PUBLIC01": {LeaveNow, ""},
		"C0SECRET01": {LeaveConfirm, ""},
		"C0OLDSECR1": {LeaveClose, ""},
		"C0SHARED01": {LeaveConfirm, ""},
		"C0ALLHANDS": {"", LeaveCantLeaveGeneral},
		"C0MANDATE1": {"", LeaveCantLeaveMandatory},
		"C0AGENT001": {LeaveNow, ""}, // an agent's channel the viewer may archive: counted, 3
	} {
		act, reason, err := a.LeaveChannelAction(ctx, ch)
		if err != nil || act != want[0] || reason != want[1] {
			t.Errorf("%s: %q %q %v, want %q %q", ch, act, reason, err, want[0], want[1])
		}
	}
	counts(1)
	for _, ch := range []string{"C0SECRET01", "C0AGENT001"} {
		if act, _, _ := a.LeaveChannelAction(ctx, ch); act != LeaveConfirmArchive {
			t.Errorf("%s's last member: %q", ch, act)
		}
	}
	if _, _, err := a.LeaveChannelAction(ctx, "C0NOWHERE1"); err == nil {
		t.Error("a channel the store doesn't hold")
	}
	// a guest leaves no public channel, nor their last channel
	u := a.S.Self
	u.IsRestricted = true
	a.S.PutUsers([]slack.User{u})
	if _, reason, _ := a.LeaveChannelAction(ctx, "C0PUBLIC01"); reason != LeaveUserIsRestricted {
		t.Errorf("a guest's public channel: %q", reason)
	}
	u.RestrictedUserType = "frontline_worker"
	a.S.PutUsers([]slack.User{u})
	if _, reason, _ := a.LeaveChannelAction(ctx, "C0SECRET01"); reason != LeaveUserIsFrontline {
		t.Errorf("a frontline member: %q", reason)
	}
	u.IsUltraRestricted, u.RestrictedUserType = true, ""
	a.S.PutUsers([]slack.User{u})
	if _, reason, _ := a.LeaveChannelAction(ctx, "C0SECRET01"); reason != LeaveUltraRestricted {
		t.Errorf("an ultra restricted guest: %q", reason)
	}
}

// closeChannelApi's submitClose closes only what canChannelBeClosed allows:
// an archived private channel's close is no call at all.
func TestCloseConversationRefuses(t *testing.T) {
	a, f := testApp(t)
	a.S.PutConv(slack.Conversation{ID: "C0OLDSECR1", Name: "old", IsChannel: true, IsPrivate: true, IsMember: true, IsArchived: true})
	if err := a.CloseConversation(context.Background(), "C0OLDSECR1"); !errors.Is(err, ErrNotClosable) {
		t.Errorf("a private channel: %v", err)
	}
	if err := a.CloseConversation(context.Background(), "D0NOWHERE1"); !errors.Is(err, ErrNotClosable) {
		t.Errorf("a DM the store doesn't hold: %v", err)
	}
	if n := countCalls(f, "conversations.close"); n != 0 {
		t.Errorf("%d conversations.close calls", n)
	}
}
