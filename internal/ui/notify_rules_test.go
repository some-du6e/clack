package ui

// The message notification rules as Slack's client decides them
// (client-boot Fpyc shouldShowNotificationForChannelMessage and
// shouldShowNotificationForDirectMessage, build a09b625d90aa; the table in
// docs/NOTIFY-RULES.md), each case one where Clack decided otherwise.
// Synthetic people and text only.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/logtag"
)

// notifyRulesEnv is a live client of a fakeslack with the DM badges'
// fixtures (the Slackbot DM), its window in the background, every
// conversation at Mentions with the keyword "synthkeyword", and three
// channels with overrides: muted, ignoring @channel (suppress_at_channel)
// and at Everything. decide pushes a message event and returns its
// decision line.
func notifyRulesEnv(t *testing.T) (e *perfEnv, fs *fakeslack.Server, muted, quiet, loud string, decide func(ev map[string]any) string) {
	t.Helper()
	t.Setenv("CLACK_HOME", t.TempDir())
	sz := fakeslack.Small
	sz.DMBadges = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	chs := fs.ChannelIDs()
	muted, quiet, loud = chs[3], chs[4], chs[5]
	prefs, _ := json.Marshal(map[string]any{
		"global": map[string]any{"global_desktop": "mentions_dms", "global_mpdm_desktop": "mentions_dms", "global_keywords": "synthkeyword"},
		"channels": map[string]any{
			muted: map[string]any{"muted": true},
			quiet: map[string]any{"suppress_at_channel": true},
			loud:  map[string]any{"desktop": "everything"},
		},
	})
	fs.SetPrefs(map[string]any{"all_notifications_prefs": string(prefs)})
	e = startEnv(t, fs, t.TempDir())
	e.waitReady()
	c := e.a.data.core
	c.SetRealtime(true)
	e.until("the socket", 20*time.Second, func() bool { return c.RTM != nil && c.RTM.Connected() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := c.RefreshPrefs(ctx); err != nil {
		t.Fatal(err)
	}
	if p := c.Prefs(); !p.ChannelNotify[muted].Muted || !p.ChannelNotify[quiet].SuppressAtChannel || len(c.S.HighlightWords()) == 0 {
		t.Fatalf("setup: the prefs didn't take (muted %v, suppress_at_channel %v, keywords %q)",
			p.ChannelNotify[muted].Muted, p.ChannelNotify[quiet].SuppressAtChannel, c.S.HighlightWords())
	}
	c.EnsureUsers(ctx, []string{fakeslack.AppPersonID})
	if u, ok := c.S.RawUser(fakeslack.AppPersonID); !ok || !u.IsAppUser {
		t.Fatal("setup: the app user isn't held")
	}
	e.a.notify = &timedNotes{}
	e.a.focused = false
	logs := captureLog(t)
	decide = func(ev map[string]any) string {
		t.Helper()
		ch, ts := ev["channel"].(string), ev["ts"].(string)
		fs.Push(ev)
		at := " " + logtag.ID(ch) + "/" + logtag.ID(ts)
		var line string
		if !e.untilSoft(10*time.Second, func() bool {
			for _, l := range logs.matching("notify: ") {
				if strings.HasSuffix(l, at) {
					line = l
					return true
				}
			}
			return false
		}) {
			return "(no decision line)"
		}
		return line
	}
	return e, fs, muted, quiet, loud, decide
}

// message is a message event of ch from user, with extra fields, at a ts
// of its own (in ch's history).
func ruleMessage(fs *fakeslack.Server, ch, user, text string, extra map[string]any) map[string]any {
	_, ev := fs.PostQuiet(ch, user, text)
	for k, v := range extra {
		ev[k] = v
	}
	return ev
}

func wantDecision(t *testing.T, what, line, want string) {
	t.Helper()
	if !strings.HasPrefix(line, "notify: "+want+" ") {
		t.Errorf("%s: %q, Slack's %q", what, line, "notify: "+want)
	}
}

// shouldShowNotificationForChannelMessage's mentions
// (checkMessageAndUpdateMentions): a user group the viewer is in, a
// mention in an attachment, and @here only while they're active count; a
// channel that ignores @channel ignores the user groups too; a bot's or an
// app's keyword doesn't count (shouldIgnoreHighlightWords) but a
// workflow's does; a muted channel still notifies for the viewer's own
// mention and keywords (12 only without them).
func TestChannelNotifyMentionsLikeSlack(t *testing.T) {
	e, fs, muted, quiet, _, decide := notifyRulesEnv(t)
	c := e.a.data.core
	ch := fs.ChannelIDs()[2]
	sender := fs.UserIDs()[7]
	me := "<@" + fakeslack.SelfID + "> "
	fs.Push(map[string]any{"type": "subteam_self_added", "subteam_id": fakeslack.WorkspaceGroupID})
	e.until("the viewer's group", 10*time.Second, func() bool { return c.S.InUsergroup(fakeslack.WorkspaceGroupID) })
	group := "<!subteam^" + fakeslack.WorkspaceGroupID + "> "

	wantDecision(t, "a mention of the viewer's user group", decide(ruleMessage(fs, ch, sender, group+"a synthetic heads-up for the crew", nil)), "show")
	wantDecision(t, "a mention of the viewer in an attachment", decide(ruleMessage(fs, ch, sender, "",
		map[string]any{"attachments": []any{map[string]any{"fallback": "a synthetic request", "text": me + "a synthetic review request"}}})), "show")

	setPresence := func(p string) {
		fs.Push(map[string]any{"type": "presence_change", "user": fakeslack.SelfID, "presence": p})
		e.until("the viewer "+p, 10*time.Second, func() bool {
			c.S.RLock()
			defer c.S.RUnlock()
			return c.S.Presence[fakeslack.SelfID] == p
		})
	}
	setPresence("away")
	wantDecision(t, "@here while the viewer is away", decide(ruleMessage(fs, ch, sender, "<!here> a synthetic ping", nil)), "skip 1")
	setPresence("active")
	wantDecision(t, "@here while the viewer is active", decide(ruleMessage(fs, ch, sender, "<!here> another synthetic ping", nil)), "show")

	wantDecision(t, "@channel where the channel ignores it", decide(ruleMessage(fs, quiet, sender, "<!channel> a synthetic announcement", nil)), "skip 1")
	wantDecision(t, "a user group's mention where the channel ignores @channel", decide(ruleMessage(fs, quiet, sender, group+"a synthetic crew note", nil)), "skip 1")

	bot := func(workflow bool) map[string]any {
		return ruleMessage(fs, ch, "", "a synthkeyword digest", map[string]any{"subtype": "bot_message", "bot_id": "B0FAKEBOT9", "username": "synthbot",
			"bot_profile": map[string]any{"id": "B0FAKEBOT9", "name": "synthbot", "app_id": "A0FAKEBOT9", "team_id": fakeslack.TeamID, "is_workflow_bot": workflow}})
	}
	wantDecision(t, "a keyword in a bot's message", decide(bot(false)), "skip 9")
	wantDecision(t, "a keyword in a workflow's message", decide(bot(true)), "show")
	wantDecision(t, "a keyword in an app's message", decide(ruleMessage(fs, ch, fakeslack.AppPersonID, "a synthkeyword from the app",
		map[string]any{"bot_id": "B0FAKEAPP9"})), "skip 10")

	wantDecision(t, "the viewer's mention in a muted channel", decide(ruleMessage(fs, muted, sender, me+"a synthetic question", nil)), "show")
	wantDecision(t, "a keyword in a muted channel", decide(ruleMessage(fs, muted, sender, "a synthkeyword note", nil)), "show")
	wantDecision(t, "@channel in a muted channel", decide(ruleMessage(fs, muted, sender, "<!channel> a synthetic announcement", nil)), "skip 12")
}

// shouldShowNotificationForChannelMessage's other rules at Everything: a
// reply sent to the channel too is a reply (isReply: 6 without a
// mention), a join never notifies (8), a topic change does, a canvas's
// mention needs the viewer's (1), and an ephemeral one outside a file
// channel goes through the rules like any other. A Slackbot DM's
// forwarded email doesn't notify (22), and neither does a DM's reply sent
// to the conversation too (6).
func TestNotifyRulesLikeSlack(t *testing.T) {
	_, fs, _, _, loud, decide := notifyRulesEnv(t)
	sender := fs.UserIDs()[7]
	root, _ := fs.PostQuiet(loud, sender, "a synthetic thread root")
	wantDecision(t, "a reply sent to the channel too, at Everything", decide(ruleMessage(fs, loud, sender, "a synthetic broadcast",
		map[string]any{"subtype": "thread_broadcast", "thread_ts": root})), "skip 6")
	wantDecision(t, "a join at Everything", decide(ruleMessage(fs, loud, sender, "<@"+sender+"> has joined the channel",
		map[string]any{"subtype": "channel_join"})), "skip 8")
	wantDecision(t, "a topic change at Everything", decide(ruleMessage(fs, loud, sender, "<@"+sender+"> set the channel topic: synthetic plans",
		map[string]any{"subtype": "channel_topic", "topic": "synthetic plans"})), "show")
	wantDecision(t, "a canvas's mention of someone else at Everything", decide(ruleMessage(fs, loud, sender, "mentioned someone in a synthetic canvas",
		map[string]any{"subtype": "document_mention"})), "skip 1")
	wantDecision(t, "an ephemeral message at Everything", decide(ruleMessage(fs, loud, sender, "a synthetic note only you can see",
		map[string]any{"is_ephemeral": true})), "show")

	dm := fakeslack.DMBadgeSlackbotID
	wantDecision(t, "an email forwarded to Slackbot", decide(ruleMessage(fs, dm, fakeslack.SlackbotID, "", map[string]any{"upload": true,
		"files": []any{map[string]any{"id": "F0FAKEMAIL1", "name": "synthetic.eml", "title": "A synthetic email", "mode": "email", "filetype": "email"}}})), "skip 22")
	partnerDM := "D0FAKE00000"
	partner := fs.UserIDs()[1]
	dmRoot, _ := fs.PostQuiet(partnerDM, partner, "a synthetic question in a DM")
	wantDecision(t, "a DM's reply sent to the conversation too", decide(ruleMessage(fs, partnerDM, partner, "a synthetic broadcast in a DM",
		map[string]any{"subtype": "thread_broadcast", "thread_ts": dmRoot})), "skip 6")
}
