package composer

import (
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// /rename, /archive and /leave are Slack's client commands (Zrd3: BasZ,
// Oc3D, OOqF) once isSlashCommandEnabled lets them run (core.CheckCommand
// before any client command): their dialogs open here and nothing goes to
// chat.command; refused, Slackbot says why.

// plainGeneral makes C0GENERAL an ordinary channel the viewer is in.
func plainGeneral(s *store.Store) {
	s.PutConv(slack.Conversation{ID: tChGen, Name: "general", IsChannel: true, IsMember: true, NumMembers: 12})
}

// grants answers edge permissions/info with perms.
func grants(fs *fakeSlack, perms ...string) {
	fs.answer("edge/permissions/info", map[string]any{"ok": true, "allowed": perms, "allowed_per_team": map[string]any{}})
}

// grantsIn is grants with ch's answer in before anything is typed, as the
// channel's views asked for it when it opened (isSlashCommandEnabled
// reads the store without waiting).
func grantsIn(t *testing.T, c *Composer, fs *fakeSlack, ch string, perms ...string) {
	t.Helper()
	grants(fs, perms...)
	srWarm(t, c.api.a, ch, nil)
}

func saysSlackbot(t *testing.T, c *Composer, want string) {
	t.Helper()
	waitUI(t, "Slackbot's "+want, func() bool {
		m, ok := slackbotSaid(c.api.a, tChGen)
		return ok && m.Text == want
	})
}

func TestSlashRenameArchiveLeave(t *testing.T) {
	t.Run("refused", func(t *testing.T) {
		c, _, fs := localComposer(t, "", plainGeneral)
		grantsIn(t, c, fs, tChGen)
		c.ed.SetDoc(cmdDoc("/rename new-name"))
		c.submit()
		saysSlackbot(t, c, "You don’t have permission to rename.")
		c.ed.SetDoc(cmdDoc("/archive"))
		c.submit()
		saysSlackbot(t, c, "*/archive* is not a valid command. In Slack, all messages that start with the \"/\" character are interpreted as commands.\n\n"+
			"If you are trying to send a message and not run a command, try preceding the \"/\" with an empty space.")
		c.ed.SetDoc(cmdDoc("/topic a new topic"))
		c.submit()
		saysSlackbot(t, c, "It looks like you don’t have permission to change the channel topic. Please check with your workspace admin.")
		if n := len(fs.called("chat.command")) + len(fs.called("conversations.setTopic")); n != 0 {
			t.Errorf("%d calls for refused commands", n)
		}
	})
	t.Run("allowed", func(t *testing.T) {
		c, h, fs := localComposer(t, "", plainGeneral)
		grantsIn(t, c, fs, tChGen, "RENAME_CHANNEL", "ARCHIVE_CHANNEL")
		c.ed.SetDoc(cmdDoc("/rename Q3 Plans"))
		c.submit()
		waitUI(t, "the rename modal", func() bool {
			m, ok := shown[*popups.RenameChannelModal](h)
			return ok && m.Name() == "Q3 Plans"
		})
		c.ed.SetDoc(cmdDoc("/archive"))
		c.submit()
		waitUI(t, "the archive modal", func() bool {
			_, ok := shown[*popups.ArchiveChannelModal](h)
			return ok
		})
		// a public channel is left at once (maybeLeaveChannel)
		fs.mu.Lock()
		fs.reply["conversations.leave"] = map[string]any{"ok": true}
		fs.mu.Unlock()
		c.ed.SetDoc(cmdDoc("/part"))
		c.submit()
		waitUI(t, "conversations.leave", func() bool { return len(fs.called("conversations.leave")) == 1 })
		if p := fs.called("conversations.leave")[0]; p.Get("channel") != tChGen {
			t.Errorf("left %v", p)
		}
		if n := len(fs.called("chat.command")); n != 0 {
			t.Errorf("%d chat.command calls", n)
		}
	})
	t.Run("general", func(t *testing.T) {
		c, h, fs := localComposer(t, "")
		c.ed.SetDoc(cmdDoc("/leave"))
		c.submit()
		waitUI(t, "#general's alert", func() bool {
			m, ok := shown[*popups.Modal](h) // its title runs up to the channel entity
			return ok && m.Title() == "Membership is required in "
		})
		if n := len(fs.called("conversations.leave")) + len(fs.called("chat.command")); n != 0 {
			t.Errorf("%d calls for #general", n)
		}
	})
}

// /remove (/kick) is Slack's client command (p8LL removeCommand): it never
// reaches chat.command. With the person mentioned a member of the channel
// and REMOVE_FROM_CHANNEL, the roster's Remove from channel confirmation
// opens for them (openRemoveMemberDialog), in a thread too; nothing is
// removed until its Remove. Its refusals are Slackbot's.
func TestSlashKick(t *testing.T) {
	const alice = "U000ALICE" // member-shaped (looksLikeMemberId)
	seed := func(s *store.Store) {
		plainGeneral(s)
		s.PutUsers([]slack.User{{ID: alice, Name: "alice", Profile: slack.Profile{DisplayName: "alice", RealName: "Alice"}}})
	}
	for _, ts := range []string{"", "1700000000.000100"} {
		t.Run("thread="+ts, func(t *testing.T) {
			c, h, fs := localComposer(t, ts, seed)
			grantsIn(t, c, fs, tChGen, "REMOVE_FROM_CHANNEL")
			fs.answer("edge/channels/membership", map[string]any{"ok": true, "members": []string{alice}, "non_members": []string{}})
			srWarm(t, c.api.a, tChGen, []string{alice}) // as the member autocomplete asked when alice was picked
			c.ed.SetDoc(cmdDoc("/kick ", &Atom{Kind: AtomUser, ID: alice, Text: "@alice"}, " "))
			c.submit()
			waitUI(t, "the remove dialog", func() bool {
				d, ok := shown[*popups.RemoveMemberDialog](h)
				return ok && strings.Contains(d.Copy.Title, "alice")
			})
			if n := len(fs.called("chat.command")) + len(fs.called("conversations.kick")); n != 0 {
				t.Errorf("%d chat.command or conversations.kick calls before Remove", n)
			}
			if ts != "" {
				return
			}
			c.ed.SetDoc(cmdDoc("/remove"))
			c.submit()
			saysSlackbot(t, c, "Please specify someone to remove!")
			if n := len(fs.called("chat.command")); n != 0 {
				t.Errorf("%d chat.command calls", n)
			}
		})
	}
}
