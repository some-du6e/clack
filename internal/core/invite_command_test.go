package core

// Slack's local /invite (invite_command.go): in a channel it never sends
// chat.command. One person: conversations.invite {channel, users}; a
// user group: {channel, subteams}; several under invite_slash_command_multi,
// each invited and the failures joined (all failed: the command's
// feedback; some: Slackbot's message); without multi only the first; a
// private channel's people go to the invite modal; the permission texts;
// nobody named opens the invite modal; the Channel Expert's text; a DM
// keeps chat.command.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

func inviteCmdApp(t *testing.T) (*App, *fakeSlack) {
	a, f := testApp(t)
	a.S.PutConv(slack.Conversation{ID: "C2", Name: "eng", IsChannel: true, IsMember: true})
	a.S.PutConv(slack.Conversation{ID: "G3", Name: "secret", IsChannel: true, IsPrivate: true, IsMember: true})
	f.reply["chat.command"] = `{"ok":true}`
	f.reply["conversations.invite"] = `{"ok":true}`
	f.reply["permissions.info"] = `{"ok":true,"allowed":["INVITE_TO_CHANNEL","INVITE_EXTERNAL_TO_CHANNEL"]}`
	a.S.TeamName = "Hack Club"
	a.SetExperimentGroup("invite_slash_command_in_threads", "on")
	return a, f
}

func countCalls(f *fakeSlack, method string) (n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c == method {
			n++
		}
	}
	return n
}

func feedbackOf(err error) string {
	var ce *CommandError
	if errors.As(err, &ce) && ce.Kind == CmdCustom {
		return ce.Feedback
	}
	return ""
}

func TestInviteCommandLocal(t *testing.T) {
	ctx := context.Background()
	run := func(a *App, ch, text string) (CommandResult, error) {
		return a.Command(ctx, CommandOpts{Channel: ch, Command: "/invite", Text: text})
	}
	t.Run("one person", func(t *testing.T) {
		a, f := inviteCmdApp(t)
		if _, err := run(a, "C2", "<@U1>"); err != nil {
			t.Fatal(err)
		}
		if countCalls(f, "chat.command") != 0 {
			t.Error("the local /invite sent chat.command")
		}
		if p := f.last("conversations.invite"); p.Get("channel") != "C2" || p.Get("users") != "U1" {
			t.Errorf("conversations.invite %v", p)
		}
	})
	t.Run("a user group", func(t *testing.T) {
		a, f := inviteCmdApp(t)
		if _, err := run(a, "C2", "<!subteam^S1|@eng>"); err != nil {
			t.Fatal(err)
		}
		if p := f.last("conversations.invite"); p.Get("subteams") != "S1" || p.Has("users") {
			t.Errorf("conversations.invite %v", p)
		}
	})
	t.Run("multi off: the first only", func(t *testing.T) {
		a, f := inviteCmdApp(t)
		a.SetExperimentGroup("invite_slash_command_multi", "off")
		if _, err := run(a, "C2", "<@U1> <@U2>"); err != nil {
			t.Fatal(err)
		}
		if n := countCalls(f, "conversations.invite"); n != 1 || f.last("conversations.invite").Get("users") != "U1" {
			t.Errorf("%d invites, last %v", n, f.last("conversations.invite"))
		}
	})
	t.Run("multi on: everyone, failures joined", func(t *testing.T) {
		a, f := inviteCmdApp(t)
		a.SetExperimentGroup("invite_slash_command_multi", "on")
		f.reply["conversations.invite"] = `{"ok":false,"error":"already_in_channel"}`
		_, err := run(a, "C2", "<@U1> <@U2>")
		if n := countCalls(f, "conversations.invite"); n != 2 {
			t.Errorf("%d invites, want 2", n)
		}
		got := feedbackOf(err)
		if !strings.Contains(got, "Alice is already in this channel.") || !strings.Contains(got, "Bob B is already in this channel.") || !strings.Contains(got, "\n") {
			t.Errorf("feedback %q", got)
		}
		if CommandFailureText(err, "/invite") != got {
			t.Errorf("failure text %q", CommandFailureText(err, "/invite"))
		}
	})
	t.Run("a private channel opens the invite modal", func(t *testing.T) {
		a, f := inviteCmdApp(t)
		res, err := run(a, "G3", "<@U1>")
		if err != nil {
			t.Fatal(err)
		}
		if res.Invite == nil || res.Invite.AddPeople != "G3" || len(res.Invite.AddUsers) != 1 || res.Invite.AddUsers[0] != "U1" {
			t.Errorf("result %+v", res.Invite)
		}
		if countCalls(f, "conversations.invite") != 0 {
			t.Error("a private channel's member was invited directly")
		}
	})
	t.Run("private without permission", func(t *testing.T) {
		a, f := inviteCmdApp(t)
		f.reply["permissions.info"] = `{"ok":true,"allowed":[]}`
		_, err := run(a, "G3", "<@U1>")
		if got := feedbackOf(err); got != "You don’t have permission to invite people to this channel." {
			t.Errorf("feedback %q (%v)", got, err)
		}
	})
	t.Run("not a member", func(t *testing.T) {
		a, _ := inviteCmdApp(t)
		_, err := run(a, "C1", "<@U1>")
		if got := feedbackOf(err); got != "You don’t have permission to invite people from *Hack Club* to this channel." {
			t.Errorf("feedback %q (%v)", got, err)
		}
	})
	t.Run("nobody named: the invite modal", func(t *testing.T) {
		a, _ := inviteCmdApp(t)
		res, err := run(a, "C2", "")
		if err != nil || res.Invite == nil || res.Invite.AddPeople != "C2" || len(res.Invite.AddUsers) != 0 {
			t.Errorf("result %+v, %v", res.Invite, err)
		}
		a.SetExperimentGroup("invite_slash_command_in_threads", "off")
		_, err = run(a, "C2", "")
		if got := feedbackOf(err); got != "A valid member name is required." {
			t.Errorf("off: %q", got)
		}
	})
	t.Run("the Channel Expert", func(t *testing.T) {
		a, _ := inviteCmdApp(t)
		_, err := run(a, "C2", "<@UCHANNELEXPERT>")
		if got := feedbackOf(err); got != "*this agent* can be added in Channel Settings." {
			t.Errorf("feedback %q", got)
		}
	})
	t.Run("a DM keeps chat.command", func(t *testing.T) {
		a, f := inviteCmdApp(t)
		if _, err := run(a, "D1", "<@U2>"); err != nil {
			t.Fatal(err)
		}
		if countCalls(f, "chat.command") != 1 {
			t.Error("/invite in a DM didn't go to chat.command")
		}
	})
	t.Run("off: one person at a time", func(t *testing.T) {
		a, _ := inviteCmdApp(t)
		a.SetExperimentGroup("invite_slash_command_in_threads", "off")
		_, err := run(a, "C2", "<@U1> <@U2>")
		if got := feedbackOf(err); got != "*/invite* can only invite one user. Try the invite shortcut to add multiple users!" {
			t.Errorf("feedback %q", got)
		}
	})
}

func TestInviteErrorTexts(t *testing.T) {
	a, _ := inviteCmdApp(t)
	for code, want := range map[string]string{
		"user_is_restricted":                    "*Alice* is a Multi-Channel Guest. Only your Workspace Admin can invite them to a public channel.",
		"cant_invite":                           "*Alice* can’t be invited to this channel right now.",
		"not_owner":                             "*Alice* can’t be invited to this channel because they’re from outside your organization.",
		"cant_invite_self":                      "All of the users in that user group are already in this channel.",
		"method_not_supported_for_channel_type": "A valid channel name is required.",
		"weird_error":                           "Hmm, something went wrong. Try again?",
	} {
		if got := a.inviteErrorText(code, "U1", "C2"); got != want {
			t.Errorf("%s: %q", code, got)
		}
	}
}
