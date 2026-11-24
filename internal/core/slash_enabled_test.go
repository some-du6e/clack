package core

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// isSlashCommandEnabled (P+cE mE) as CheckCommand ports it
// (slash_enabled.go), against the rules of web client 66e72b57ade1.

// allow answers edge permissions/info (the fake's "info") with perms.
func allow(f *fakeSlack, perms ...string) {
	b, _ := json.Marshal(map[string]any{"ok": true, "allowed": perms})
	f.mu.Lock()
	f.reply["info"] = string(b)
	f.mu.Unlock()
}

// check is CheckCommand for cmd in ch once ch's grants are in, as the
// channel's views have asked for them when it opened.
func check(a *App, ch, cmd, ts string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = a.LoadChannelGrants(ctx, ch)
	o := CommandOpts{Channel: ch, Command: cmd, ThreadTS: ts}
	return a.CheckCommand(ctx, &o)
}

func isKind(err error, kind string) bool {
	var ce *CommandError
	return errors.As(err, &ce) && ce.Kind == kind
}

// /topic (iJOl X, MgZJ Bw): a channel needs SET_CHANNEL_TOPIC; a DM the
// viewer can post to needs none; archived, a Slack Connect DM, or no
// conversation at all are refused in their own words.
func TestCheckCommandTopic(t *testing.T) {
	a, f := testApp(t)
	restricted := "It looks like you don’t have permission to change the channel topic. Please check with your workspace admin."
	a.S.PutConv(slack.Conversation{ID: "C0TOPICNO", Name: "no", IsChannel: true, IsMember: true})
	a.S.PutConv(slack.Conversation{ID: "C0TOPICOK", Name: "ok", IsChannel: true, IsMember: true})
	a.S.PutConv(slack.Conversation{ID: "C0ARCHIVED", Name: "old", IsChannel: true, IsMember: true, IsArchived: true})
	a.S.PutConv(slack.Conversation{ID: "D0EXTSHARE", IsIM: true, User: "U2", IsExtShared: true})
	allow(f)
	if got := feedbackOf(check(a, "C0TOPICNO", "/topic", "")); got != restricted {
		t.Errorf("without SET_CHANNEL_TOPIC: %q", got)
	}
	if err := check(a, "D1", "/topic", ""); err != nil {
		t.Errorf("a DM: %v", err)
	}
	allow(f, PermSetChannelTopic)
	if err := check(a, "C0TOPICOK", "/topic", ""); err != nil {
		t.Errorf("with SET_CHANNEL_TOPIC: %v", err)
	}
	if got := feedbackOf(check(a, "C0ARCHIVED", "/topic", "")); got != restricted {
		t.Errorf("archived: %q", got)
	}
	if got := feedbackOf(check(a, "D0EXTSHARE", "/topic", "")); got != "Regrettably, direct messages do not have topics." {
		t.Errorf("a Slack Connect DM: %q", got)
	}
	if got := feedbackOf(check(a, "C0NOWHERE1", "/topic", "")); got != "It’s not possible to set a topic here, unfortunately." {
		t.Errorf("no conversation: %q", got)
	}
}

// /rename (kfQA be): a DM or group DM, a meeting's or a record's
// channel, or no RENAME_CHANNEL, each in the check's words.
func TestCheckCommandRename(t *testing.T) {
	a, f := testApp(t)
	noPerm := "You don’t have permission to rename."
	a.S.PutConv(slack.Conversation{ID: "C0RENAMENO", Name: "no", IsChannel: true, IsMember: true})
	a.S.PutConv(slack.Conversation{ID: "C0RENAMEOK", Name: "ok", IsChannel: true, IsMember: true})
	a.S.PutConv(slack.Conversation{ID: "C0MEETING1", Name: "mc:standup", IsChannel: true, IsMember: true})
	a.S.PutConv(slack.Conversation{ID: "C0RECORD01", Name: "rec", IsChannel: true, IsMember: true,
		Properties: json.RawMessage(`{"record_channel":{"record_id":"R:1"}}`)})
	a.S.PutConv(slack.Conversation{ID: "C0SALESFRC", Name: "sf", IsChannel: true, IsMember: true,
		Properties: json.RawMessage(`{"record_channel":{"record_id":"S:org:001"}}`)})
	a.S.PutConv(slack.Conversation{ID: "G0GROUPDM1", Name: "mpdm-a--b-1", IsMPIM: true, IsMember: true})
	a.S.PutConv(slack.Conversation{ID: "D0DIRECT01", IsIM: true, User: "U1"}) // jq: a channel-shaped id
	allow(f)
	for ch, want := range map[string]string{
		"C0RENAMENO": noPerm,
		"D1":         noPerm, // not channel-shaped (jq): InvalidParams
		"D0DIRECT01": "Direct messages don’t even have a name! Ergo, regrettably, you can’t change it.",
		"G0GROUPDM1": "Direct messages don’t even have a name! Ergo, regrettably, you can’t change it.",
		"C0MEETING1": "This channel can’t be renamed.",
		"C0RECORD01": "This channel can’t be renamed.",
		"C0NOWHERE2": noPerm, // InvalidParams
	} {
		if got := feedbackOf(check(a, ch, "/rename", "")); got != want {
			t.Errorf("%s: %q, want %q", ch, got, want)
		}
	}
	allow(f, PermRenameChannel)
	for _, ch := range []string{"C0RENAMEOK", "C0SALESFRC"} {
		if err := check(a, ch, "/rename", ""); err != nil {
			t.Errorf("%s with RENAME_CHANNEL: %v", ch, err)
		}
	}
}

// /archive (2Uru Jz, HT3j iz): unknown unless the viewer may archive the
// channel: ARCHIVE_CHANNEL, a member (or an admin), not #general, a DM,
// archived, a file's, or hosted elsewhere; a Slack Connect channel only
// for its creator here.
func TestCheckCommandArchive(t *testing.T) {
	a, f := testApp(t)
	a.S.PutConv(slack.Conversation{ID: "C0ARCHNOPE", Name: "nope", IsChannel: true, IsMember: true})
	allow(f)
	if err := check(a, "C0ARCHNOPE", "/archive", ""); !isKind(err, CmdUnknown) {
		t.Errorf("without ARCHIVE_CHANNEL: %v", err)
	}
	allow(f, PermArchiveChannel)
	ok := map[string]slack.Conversation{
		"C0ARCHIVE1": {Name: "a", IsChannel: true, IsMember: true},
		"C0ARCHSELF": {Name: "c", IsChannel: true, IsMember: true, IsExtShared: true, Creator: "USELF"},
	}
	refused := map[string]slack.Conversation{
		"C0ARCHGEN1": {Name: "general", IsChannel: true, IsMember: true, IsGeneral: true},
		"C0ARCHMAND": {Name: "all", IsChannel: true, IsMember: true, IsOrgMandatory: true},
		"C0ARCHOLD1": {Name: "old", IsChannel: true, IsMember: true, IsArchived: true},
		"C0ARCHOUT1": {Name: "out", IsChannel: true},
		"C0ARCHHOST": {Name: "away", IsChannel: true, IsMember: true, ConversationHostID: "E0THEIRS1"},
		"C0ARCHEXT1": {Name: "ext", IsChannel: true, IsMember: true, IsExtShared: true, Creator: "U1"},
		"C0ARCHFILE": {Name: "f", IsChannel: true, IsMember: true, IsFile: true},
	}
	for id, c := range ok {
		c.ID = id
		a.S.PutConv(c)
	}
	for id, c := range refused {
		c.ID = id
		a.S.PutConv(c)
	}
	for id := range ok {
		if err := check(a, id, "/archive", ""); err != nil {
			t.Errorf("%s: %v", id, err)
		}
	}
	for id := range refused {
		if err := check(a, id, "/archive", ""); !isKind(err, CmdUnknown) {
			t.Errorf("%s: %v, want unknown", id, err)
		}
	}
	a.S.PutConv(slack.Conversation{ID: "D0DIRECT02", IsIM: true, User: "U1", IsMember: true})
	if err := check(a, "D0DIRECT02", "/archive", ""); !isKind(err, CmdUnknown) {
		t.Errorf("a DM: %v", err)
	}
	// an admin needn't be a member
	u := a.S.Self
	u.IsAdmin = true
	a.S.PutUsers([]slack.User{u})
	if err := check(a, "C0ARCHOUT1", "/archive", ""); err != nil {
		t.Errorf("an admin outside the channel: %v", err)
	}
}

// The thread rule by canonical name: D's commands run in a DM's thread
// too, not in a huddle's channel; an alias counts as its command; W's
// commands are unknown in a file's conversation.
func TestCheckCommandThreads(t *testing.T) {
	a, f := testApp(t)
	allow(f, PermSetChannelTopic)
	a.S.PutConv(slack.Conversation{ID: "C0HUDDLE01", Name: "h", IsChannel: true, IsMember: true,
		Properties: json.RawMessage(`{"huddles":{"is_huddle_channel":true}}`)})
	a.S.PutConv(slack.Conversation{ID: "C0FILECONV", IsChannel: true, IsMember: true, IsFile: true})
	if err := check(a, "D1", "/topic", "1.0"); err != nil {
		t.Errorf("/topic in a DM's thread: %v", err)
	}
	if err := check(a, "C0HUDDLE01", "/topic", "1.0"); !isKind(err, CmdUnsupportedInThread) {
		t.Errorf("/topic in a huddle channel's thread: %v", err)
	}
	if err := check(a, "C1", "/kick", "1.0"); err != nil {
		t.Errorf("/kick (/remove) in a thread: %v", err)
	}
	if err := check(a, "C1", "/part", "1.0"); !isKind(err, CmdUnsupportedInThread) {
		t.Errorf("/part (/leave) in a thread: %v", err)
	}
	if err := check(a, "C0FILECONV", "/topic", ""); !isKind(err, CmdUnknown) {
		t.Errorf("/topic in a file's conversation: %v", err)
	}
}

// A guest: /open is unknown, and so is a custom command under
// commands_only_regular (not for a member, and not other commands).
func TestCheckCommandGuests(t *testing.T) {
	a, _ := testApp(t)
	a.mu.Lock()
	a.cmdCache = []slack.Command{{Name: "/lunch", Type: "custom"}, {Name: "/poll", Type: "app"}}
	a.mu.Unlock()
	a.SetTeamPrefs(map[string]json.RawMessage{"commands_only_regular": json.RawMessage(`true`)})
	if err := check(a, "C1", "/lunch", ""); err != nil {
		t.Errorf("a member's custom command: %v", err)
	}
	u := a.S.Self
	u.IsRestricted = true
	a.S.PutUsers([]slack.User{u})
	for _, cmd := range []string{"/open", "/join", "/lunch"} {
		if err := check(a, "C1", cmd, ""); !isKind(err, CmdUnknown) {
			t.Errorf("a guest's %s: %v", cmd, err)
		}
	}
	if err := check(a, "C1", "/poll", ""); err != nil {
		t.Errorf("a guest's app command: %v", err)
	}
}

// A command's teams (the store's `if (t.teams)`): an empty list refuses
// too; none at all isn't checked. The workspace pick: a typed command
// installed here runs here with its app (ia4), a workspace-agnostic one
// with neither.
func TestCheckCommandTeams(t *testing.T) {
	a, _ := testApp(t)
	a.SetExperimentGroup("ia4_slash_command_fe", "on")
	a.mu.Lock()
	a.cmdCache = []slack.Command{
		{Name: "/here", AppID: "A1", Teams: []string{"T1"}},
		{Name: "/none", AppID: "A2", Teams: []string{}},
		{Name: "/plain", AppID: "A3"},
		{Name: "/agnostic", AppID: "A4", Teams: []string{"T1"}, WorkspaceAgnostic: true},
	}
	a.mu.Unlock()
	pick := func(cmd string) (CommandOpts, error) {
		o := CommandOpts{Channel: "C1", Command: cmd}
		err := a.CheckCommand(context.Background(), &o)
		return o, err
	}
	if o, err := pick("/here"); err != nil || o.TeamID != "T1" || o.AppID != "A1" || !o.Checked {
		t.Errorf("/here: %+v %v", o, err)
	}
	if _, err := pick("/none"); feedbackOf(err) != "This command is not available in this channel" {
		t.Errorf("/none: %v", err)
	}
	if o, err := pick("/plain"); err != nil || o.TeamID != "" || o.AppID != "" {
		t.Errorf("/plain: %+v %v", o, err)
	}
	if o, err := pick("/agnostic"); err != nil || o.TeamID != "" || o.AppID != "" {
		t.Errorf("/agnostic: %+v %v", o, err)
	}
	// a conversation the store doesn't hold has no workspaces
	o := CommandOpts{Channel: "C0NOWHERE3", Command: "/here"}
	if err := a.CheckCommand(context.Background(), &o); feedbackOf(err) != "This command is not available in this channel" {
		t.Errorf("/here outside the store: %v", err)
	}
}

// leaveCommand (OOqF): this conversation, or the one the first mention
// names (a member's DM); text, a member without a DM or another
// workspace's channel are refused; one the store doesn't hold is an
// unknown error; a DM closes.
func TestLeaveCommand(t *testing.T) {
	a, _ := testApp(t)
	// member-shaped ids (looksLikeMemberId: U and 8 more)
	a.S.PutUsers([]slack.User{{ID: "U0ALICE001", Name: "alice2"}, {ID: "U0NODM0001", Name: "nodm"}})
	a.S.PutConv(slack.Conversation{ID: "D0ALICE001", IsIM: true, User: "U0ALICE001"})
	a.S.PutConv(slack.Conversation{ID: "C0OTHERTM1", Name: "theirs", IsChannel: true, IsMember: true, ContextTeamID: "T9"})
	mention := func(id string) []CommandParam { return []CommandParam{{Type: "mention", Value: id}} }
	if lt, err := a.LeaveCommand("/leave", "C1", nil); err != nil || lt != (LeaveTarget{Conv: "C1"}) {
		t.Errorf("no arguments: %+v %v", lt, err)
	}
	if lt, err := a.LeaveCommand("/leave", "D1", nil); err != nil || lt != (LeaveTarget{Conv: "D1", Close: true}) {
		t.Errorf("in a DM: %+v %v", lt, err)
	}
	if lt, err := a.LeaveCommand("/close", "C1", mention("U0ALICE001")); err != nil || lt != (LeaveTarget{Conv: "D0ALICE001", Close: true}) {
		t.Errorf("a member with a DM: %+v %v", lt, err)
	}
	invalid := "A valid channel name is required."
	if _, err := a.LeaveCommand("/leave", "C1", []CommandParam{{Type: "string", Value: "hi"}}); feedbackOf(err) != invalid {
		t.Errorf("text: %v", err)
	}
	if _, err := a.LeaveCommand("/leave", "C1", mention("U0NODM0001")); feedbackOf(err) != invalid {
		t.Errorf("a member without a DM: %v", err)
	}
	if _, err := a.LeaveCommand("/part", "C1", mention("C0OTHERTM1")); feedbackOf(err) != "*/part* can’t leave channels outside this workspace." {
		t.Errorf("another workspace's channel: %v", err)
	}
	_, err := a.LeaveCommand("/leave", "C1", mention("C0NOWHERE4"))
	if got := CommandFailureText(err, "/leave"); got != "*/leave* failed with an unknown error." {
		t.Errorf("a channel the store doesn't hold: %q", got)
	}
}

// closeChannelApi's submitClose: conversations.close for a DM or group DM.
func TestCloseConversation(t *testing.T) {
	a, f := testApp(t)
	f.reply["conversations.close"] = `{"ok":true}`
	if err := a.CloseConversation(context.Background(), "D1"); err != nil {
		t.Fatal(err)
	}
	if p := f.last("conversations.close"); p.Get("channel") != "D1" {
		t.Errorf("conversations.close %v", p)
	}
	if a.dmOpen("D1") {
		t.Error("the DM is still open")
	}
}

// Slack's read with nothing held (mLk1 getPermissionForChannel): the
// check asks permissions/info and refuses meanwhile; once the answer is
// in, the same command runs. LoadChannelGrants waits for that answer,
// asking once.
func TestCommandGrantMissRefuses(t *testing.T) {
	a, f := testApp(t)
	allow(f, PermRenameChannel)
	a.S.PutConv(slack.Conversation{ID: "C0RENAMECO", Name: "cold", IsChannel: true, IsMember: true})
	o := CommandOpts{Channel: "C0RENAMECO", Command: "/rename"}
	if got := feedbackOf(a.CheckCommand(context.Background(), &o)); got != "You don’t have permission to rename." {
		t.Errorf("before permissions/info answered: %q", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.LoadChannelGrants(ctx, "C0RENAMECO"); err != nil {
		t.Fatal(err)
	}
	if n := countCalls(f, "info"); n != 1 {
		t.Errorf("%d permissions/info calls", n)
	}
	o = CommandOpts{Channel: "C0RENAMECO", Command: "/rename"}
	if err := a.CheckCommand(ctx, &o); err != nil {
		t.Errorf("after the answer: %v", err)
	}
	if !a.ChannelGrant("C0RENAMECO", PermRenameChannel) || a.ChannelGrant("C0RENAMECO", PermArchiveChannel) {
		t.Error("RENAME_CHANNEL only")
	}
	// a failed ask stores nothing: the wait says so
	f.mu.Lock()
	f.reply["info"] = `{"ok":false,"error":"fatal_error"}`
	f.mu.Unlock()
	if err := a.LoadChannelGrants(ctx, "C0OTHERONE"); err == nil {
		t.Error("a failed permissions/info loaded")
	}
}
