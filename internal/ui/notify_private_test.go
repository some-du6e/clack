package ui

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Private channels in notifications (Karim, 2026-11-14: "pruvate channels
// in notifs do not show up properly, they are like CKJOIRJWEIO"). Slack
// titles a notification with getDisplayNameForChannel (a private
// channel's bare name, "#name" for a public one) and writes its text's
// channel mentions with channelToText; neither ever shows a raw id
// (notify_conv.go). These run over fakeslack's thread-header fixture,
// whose conversations.info serves the private channel th-secret-plans.

// privNoteApp is noteApp over a fakeslack with the thread-header
// conversations: the private th-secret-plans (fakeslack.ThreadPrivateID),
// the public th-launch-notes, a DM and a group DM. The sidebar's rows
// (b.m.Convs) start empty, as between core learning a conversation and
// the next snapshot.
func privNoteApp(t *testing.T) (*App, *core.App, *noteRec) {
	t.Helper()
	sz := fakeslack.Small
	sz.ThreadHeader = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	fs.SetPrefs(map[string]any{"mute_sounds": true})
	ca := core.New(slack.New(fs.Creds()), store.New(""))
	t.Cleanup(ca.Shutdown)
	if _, err := ca.RefreshPrefs(context.Background()); err != nil {
		t.Fatal(err)
	}
	images := imgcache.New(func(ctx context.Context, url string) ([]byte, error) { return nil, errors.New("offline") }, "", nil)
	a := New(Options{ConfigDir: t.TempDir(), NoRealtime: true}, images)
	sh := theme.NewShaper()
	a.th.SetShapers(sh, sh)
	b := newBackend(ca, "")
	b.app = a
	b.ctx, b.cancel = context.WithCancel(context.Background())
	t.Cleanup(b.cancel)
	a.data = b
	storeTeam(ca)
	rec := &noteRec{}
	a.notify = rec
	return a, ca, rec
}

// fixtureConv is id's conversation as fakeslack serves it.
func fixtureConv(t *testing.T, ca *core.App, id string) slack.Conversation {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := ca.C.ConversationInfo(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return *c
}

// notifyIn delivers Ada's message mentioning the viewer in ch (the
// default "mentions" level notifies for it).
func notifyIn(a *App, ch, kind, ts, text string) {
	a.data.handle(a, core.Event{Type: "message", Channel: ch, TS: ts, ConvKind: kind, Msg: noteMsg(ts, "", text), Mention: true})
}

// quiet drains posted work for a while and reports the notifications.
func quiet(a *App, rec *noteRec) int {
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		a.drain()
		time.Sleep(20 * time.Millisecond)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return len(rec.notes)
}

// noIDTitle fails when the toast's title text carries the channel's id.
func noIDTitle(t *testing.T, n *desktopNote, id string) {
	t.Helper()
	if strings.Contains(n.Title, id) {
		t.Errorf("the title shows the channel id: %q", n.Title)
	}
	x := n.toastXML("")
	if !strings.Contains(x, "<text>"+toastEscape(n.Title)+"</text>") {
		t.Errorf("the toast's title text isn't %q: %s", n.Title, x)
	}
}

// A message in a private channel is titled with its bare name, as
// getDisplayNameForChannel names it, on both ways the name can be
// missing from what Clack read before: the conversation core just
// learned (users.conversations, a join, the lookup core does for the
// message) isn't in the sidebar's rows yet (the title was the sender's
// form, "New message from Ada"), and a conversation whose object came
// without its name (named by its id: "New message in C0THPRIVAT1").
func TestNotifyPrivateChannelTitle(t *testing.T) {
	const priv = fakeslack.ThreadPrivateID
	t.Run("known to core, not in the sidebar rows", func(t *testing.T) {
		a, ca, rec := privNoteApp(t)
		ca.S.PutConv(fixtureConv(t, ca, priv))
		notifyIn(a, priv, "private", "1762520200.000100", "hello")
		n := rec.wait(t, a, 1)
		if n.Title != "New message in th-secret-plans" || n.Body != "Ada: hello" || n.Channel != priv {
			t.Errorf("notified %q / %q in %s, Slack \"New message in th-secret-plans\" / \"Ada: hello\"", n.Title, n.Body, n.Channel)
		}
		noIDTitle(t, n, priv)
	})
	t.Run("a stub named by its id", func(t *testing.T) {
		a, ca, rec := privNoteApp(t)
		ca.S.PutConv(slack.Conversation{ID: priv, Name: priv, IsGroup: true, IsPrivate: true, IsMember: true})
		a.data.m.Convs[priv] = &convInfo{ID: priv, Name: priv, Kind: "private"}
		notifyIn(a, priv, "private", "1762520200.000200", "hello")
		n := rec.wait(t, a, 1)
		if n.Title != "New message in th-secret-plans" {
			t.Errorf("notified %q, Slack \"New message in th-secret-plans\" (the lookup's name, not the id)", n.Title)
		}
		noIDTitle(t, n, priv)
		if c, ok := ca.Conv(priv); !ok || c.Name != "th-secret-plans" {
			t.Errorf("core kept %+v after the lookup, want its name", c)
		}
	})
	// the other kinds keep Slack's titles: a public channel's "#name", a
	// DM's sender, a group DM's members
	t.Run("public, DM and group DM", func(t *testing.T) {
		a, ca, rec := privNoteApp(t)
		pub := fixtureConv(t, ca, fakeslack.ThreadPublicID)
		ca.S.PutConv(pub)
		a.data.m.Convs[pub.ID] = &convInfo{ID: pub.ID, Name: pub.Name, Kind: "channel"}
		notifyIn(a, pub.ID, "channel", "1762520200.000300", "hello")
		if n := rec.wait(t, a, 1); n.Title != "New message in #th-launch-notes" || n.Body != "Ada: hello" {
			t.Errorf("public: %q / %q, Slack \"New message in #th-launch-notes\" / \"Ada: hello\"", n.Title, n.Body)
		}
		notifyIn(a, fakeslack.ThreadDMID, "im", "1762520200.000400", "hello")
		if n := rec.wait(t, a, 2); n.Title != "New message from Ada" || n.Body != "hello" {
			t.Errorf("DM: %q / %q, Slack \"New message from Ada\" / \"hello\"", n.Title, n.Body)
		}
		a.data.m.Convs[fakeslack.ThreadGroupDMID] = &convInfo{ID: fakeslack.ThreadGroupDMID, Name: "alex, robin", Kind: "mpim"}
		notifyIn(a, fakeslack.ThreadGroupDMID, "mpim", "1762520200.000500", "hello")
		if n := rec.wait(t, a, 3); n.Title != "New message in alex, robin" || n.Body != "Ada: hello" {
			t.Errorf("group DM: %q / %q, Slack \"New message in alex, robin\" / \"Ada: hello\"", n.Title, n.Body)
		}
	})
}

// A followed thread's reply (the server's desktop_notification) in a
// private channel the sidebar's rows don't have was dropped as an unknown
// conversation; Slack's store has it, and so does core (or a lookup).
func TestNotifyPrivateThreadReply(t *testing.T) {
	const priv, root = fakeslack.ThreadPrivateID, "1762520000.000100"
	push := func(a *App, ch, ts string) {
		raw, _ := json.Marshal(map[string]any{"type": "desktop_notification", "channel": ch, "ts": ts, "msg": ts, "thread_ts": root,
			"content": "Robin Example: a synthetic reply", "title": "Thread in th-secret-plans"})
		a.data.handle(a, core.Event{Type: "desktop_notification", Channel: ch, Raw: raw})
	}
	check := func(t *testing.T, n *desktopNote) {
		t.Helper()
		if n.Title != "New message in th-secret-plans" || n.Body != "Robin Example: a synthetic reply" || n.Channel != priv || n.ThreadTS != root {
			t.Errorf("notified %q / %q (%s, thread %s), Slack \"New message in th-secret-plans\" with the event's content", n.Title, n.Body, n.Channel, n.ThreadTS)
		}
		noIDTitle(t, n, priv)
	}
	t.Run("known to core, not in the sidebar rows", func(t *testing.T) {
		a, ca, rec := privNoteApp(t)
		ca.S.PutConv(fixtureConv(t, ca, priv))
		push(a, priv, "1762520300.000100")
		check(t, rec.wait(t, a, 1))
	})
	t.Run("not in the store", func(t *testing.T) {
		a, _, rec := privNoteApp(t)
		push(a, priv, "1762520300.000200")
		check(t, rec.wait(t, a, 1))
	})
	t.Run("a conversation nobody can look up", func(t *testing.T) {
		a, _, rec := privNoteApp(t)
		push(a, "C0NOPE00001", "1762520300.000300")
		if n := quiet(a, rec); n != 0 {
			t.Errorf("%d notifications for a conversation conversations.info doesn't know, want none (an unknown conversation)", n)
		}
	})
}

// A notification's text writes channel mentions as Slack's channelToText
// does: "#name" for a channel it can name (looked up first when the store
// can't yet: the text read "#C0THPRIVAT1"), "#private-channel" for one
// the viewer can't see or nobody knows; never the id.
func TestNotifyChannelMentionText(t *testing.T) {
	a, ca, rec := privNoteApp(t)
	pub := fixtureConv(t, ca, fakeslack.ThreadPublicID)
	ca.S.PutConv(pub)
	a.data.m.Convs[pub.ID] = &convInfo{ID: pub.ID, Name: pub.Name, Kind: "channel"}
	notifyIn(a, pub.ID, "channel", "1762520400.000100", "see <#"+fakeslack.ThreadPrivateID+"> and <#C0NOPE00001>, or <#"+pub.ID+">")
	n := rec.wait(t, a, 1)
	if want := "Ada: see #th-secret-plans and #private-channel, or #th-launch-notes"; n.Body != want {
		t.Errorf("body %q, Slack %q", n.Body, want)
	}
	if strings.Contains(n.Body, fakeslack.ThreadPrivateID) || strings.Contains(n.Body, "C0NOPE00001") {
		t.Errorf("the body shows a channel id: %q", n.Body)
	}
}

// getDisplayNameForChannel's names for the title, with
// formatChannelName's fallback for a channel whose name isn't known.
func TestNotifyChannelNameFallback(t *testing.T) {
	for _, c := range []struct {
		conv *convInfo
		want string
	}{
		{&convInfo{ID: "C0PRIV00001", Kind: "private", Name: "th-secret-plans"}, "th-secret-plans"},
		{&convInfo{ID: "C0PUB000001", Kind: "channel", Name: "th-launch-notes"}, "#th-launch-notes"},
		{&convInfo{ID: "C0PRIV00001", Kind: "private", Name: "C0PRIV00001"}, "unknown-channel"},
		{&convInfo{ID: "C0PUB000001", Kind: "channel"}, "#unknown-channel"},
		{&convInfo{ID: "G0LEGACY001", Kind: "private", Name: " "}, "unknown-channel"},
	} {
		if got := notifyChannelName(c.conv); got != c.want {
			t.Errorf("%+v: %q, Slack %q", c.conv, got, c.want)
		}
	}
}
