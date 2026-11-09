package composer

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// @channel / @here / @everyone in the composer (broadcast.go): the @ rows
// follow the viewer's permissions/info, and a send Slack would refuse
// stays in the composer with Slackbot's explanation. Synthetic workspace,
// fake API (taut_test.go's fakeSlack).

const tChOpen = "C0OPEN" // a public channel that isn't #general

// noBroadcastPerms is permissions/info for a regular Hack Club member.
var noBroadcastPerms = map[string]any{"ok": true, "allowed": []string{}, "allowed_per_team": map[string]any{}}

func broadcastApp(t *testing.T, perms map[string]any) (*core.App, *fakeSlack) {
	t.Helper()
	app, fs := syntheticApp(t, func(s *store.Store) {
		s.PutConv(slack.Conversation{ID: tChOpen, Name: "open", IsChannel: true, IsMember: true, NumMembers: 30})
		for _, ch := range []string{tChGen, tChOpen} { // open channels: their timelines are loaded
			s.PrependOlder(ch, []slack.Message{{Type: "message", TS: "1700000000.000100", User: "UALICE", Text: "synthetic"}}, false)
		}
	})
	if perms != nil {
		fs.mu.Lock()
		fs.reply = map[string]any{"edge/permissions/info": perms}
		fs.mu.Unlock()
	}
	return app, fs
}

// toastHost records toasts.
type toastHost struct {
	slickHost
	mu     sync.Mutex
	toasts []string
}

func (h *toastHost) Toast(s string) {
	h.mu.Lock()
	h.toasts = append(h.toasts, s)
	h.mu.Unlock()
}

// acRows types text into an autocomplete for ch and returns the row
// names once the async sources (members, permissions) have landed.
func acRows(t *testing.T, app *core.App, ch, text string) []string {
	t.Helper()
	ac := newAutocomplete(&slickHost{app: app}, ch)
	t.Cleanup(ac.Close)
	var mu sync.Mutex
	var q []func()
	ac.later = func(f func()) { mu.Lock(); q = append(q, f); mu.Unlock() }
	d := acDocOf(text)
	var names []string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		fs := q
		q = nil
		mu.Unlock()
		for _, f := range fs {
			f()
		}
		ac.Update(d, d.Len())
		ac.mu.Lock()
		settled := ac.perms.Known && ac.members != nil && ac.groups != nil
		ac.mu.Unlock()
		names = names[:0]
		items, _ := ac.Items()
		for _, it := range items {
			names = append(names, it.Name)
		}
		if settled {
			return names
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("autocomplete sources never landed (rows %v)", names)
	return nil
}

func TestBroadcastAutocompletePermissions(t *testing.T) {
	app, _ := broadcastApp(t, nil) // allowed: CAN_AT_CHANNEL, CAN_AT_EVERYONE
	if got := acRows(t, app, tChGen, "@"); !hasName(got, "here") || !hasName(got, "channel") || !hasName(got, "everyone") {
		t.Errorf("#general with both permissions: rows %v, want @here @channel @everyone", got)
	}
	if got := acRows(t, app, tChOpen, "@ev"); hasName(got, "everyone") {
		t.Errorf("@everyone offered outside #general: %v", got)
	}

	app, fs := broadcastApp(t, noBroadcastPerms)
	for _, ch := range []string{tChGen, tChOpen} {
		got := acRows(t, app, ch, "@")
		for _, k := range []string{"here", "channel", "everyone"} {
			if hasName(got, k) {
				t.Errorf("%s without permissions offers @%s: %v", ch, k, got)
			}
		}
	}
	reqs := fs.called("edge/permissions/info")
	if len(reqs) == 0 {
		t.Fatal("permissions/info never asked")
	}
	var body map[string]any
	_ = json.Unmarshal([]byte(reqs[0].Get("body")), &body)
	if body["user_id"] != tSelf || body["channel_id"] == nil {
		t.Errorf("permissions/info body %v", body)
	}
}

// broadcastDoc is "ping <@keyword> please".
func broadcastDoc(keyword string) *Doc {
	d := NewDoc()
	n := d.Insert(0, "ping ", Attr{})
	n += d.InsertAtom(n, &Atom{Kind: AtomBroadcast, ID: keyword, Text: "@" + keyword})
	d.Insert(n, " please", Attr{})
	return d
}

func runSend(t *testing.T, app *core.App, ch, ts string, d *Doc) error {
	t.Helper()
	withTaut(t, nil)
	o := &outgoing{doc: d, text: d.Mrkdwn(), blocks: d.RichTextBlocks()}
	j := &sendJob{api: coreAPI{app}, ch: ch, ts: ts, o: o, draftID: ch, view: ViewContextMessagePane}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return j.run(ctx)
}

// slackbotSaid returns the last local Slackbot message in ch.
func slackbotSaid(app *core.App, ch string) (slack.Message, bool) {
	tl := app.S.Timeline(ch)
	if tl == nil {
		return slack.Message{}, false
	}
	for i := len(tl.Msgs) - 1; i >= 0; i-- {
		if m := tl.Msgs[i]; m.User == "USLACKBOT" && app.Interactive().Ephemeral(ch, m.TS) {
			return m, true
		}
	}
	return slack.Message{}, false
}

func TestBroadcastSendRefused(t *testing.T) {
	app, fs := broadcastApp(t, noBroadcastPerms)
	err := runSend(t, app, tChOpen, "", broadcastDoc("channel"))
	if !isProhibitedBroadcast(err) {
		t.Fatalf("send error %v, want ProhibitedBroadcastKeywordInEffect", err)
	}
	if n := len(fs.called("chat.postMessage")); n != 0 {
		t.Fatalf("%d chat.postMessage calls, want none", n)
	}
	m, ok := slackbotSaid(app, tChOpen)
	if !ok || m.Text != "A Workspace Owner has restricted the use of *@channel* messages." {
		t.Fatalf("Slackbot said %q (%v)", m.Text, ok)
	}

	if err := runSend(t, app, tChGen, "", broadcastDoc("everyone")); !isProhibitedBroadcast(err) {
		t.Fatalf("@everyone: %v", err)
	}
	if m, _ := slackbotSaid(app, tChGen); m.Text != "A Workspace Owner has restricted the use of *@everyone* messages." {
		t.Errorf("@everyone: Slackbot said %q", m.Text)
	}

	// plain text "@channel" isn't a broadcast mention: sent as typed
	d := NewDoc()
	d.Insert(0, "type @channel to ping", Attr{})
	if err := runSend(t, app, tChOpen, "", d); err != nil {
		t.Fatalf("plain text send: %v", err)
	}
	if n := len(fs.called("chat.postMessage")); n != 1 {
		t.Errorf("%d chat.postMessage calls after the plain one, want 1", n)
	}
}

func TestBroadcastSendAllowed(t *testing.T) {
	app, fs := broadcastApp(t, nil)
	if err := runSend(t, app, tChOpen, "", broadcastDoc("here")); err != nil {
		t.Fatal(err)
	}
	posts := fs.called("chat.postMessage")
	if len(posts) != 1 || !strings.Contains(posts[0].Get("blocks"), `"range":"here"`) {
		t.Fatalf("posts %v", posts)
	}
	// in a thread Slack allows no keyword at all
	err := runSend(t, app, tChOpen, "1700000000.000100", broadcastDoc("channel"))
	if !isProhibitedBroadcast(err) {
		t.Fatalf("thread reply: %v", err)
	}
	if n := len(fs.called("chat.postMessage")); n != 1 {
		t.Errorf("thread reply posted (%d calls)", n)
	}
}

// TestBroadcastComposerKeepsDraft: a refused send goes back in the
// composer, with no "Message not sent" toast (Slackbot already said why).
func TestBroadcastComposerKeepsDraft(t *testing.T) {
	app, fs := broadcastApp(t, noBroadcastPerms)
	withTaut(t, nil)
	h := &toastHost{slickHost: slickHost{app: app, th: theme.New(false)}}
	clearDraft(tChOpen, "")
	c := New(h, tChOpen, "")
	t.Cleanup(func() { c.Close(); clearDraft(tChOpen, "") })
	c.ed.SetDoc(broadcastDoc("here"))
	c.submit()
	deadline := time.Now().Add(5 * time.Second)
	for c.ed.Empty() && time.Now().Before(deadline) {
		runRestores()
		time.Sleep(10 * time.Millisecond)
	}
	if got := c.ed.Doc().Text(); !strings.Contains(got, "ping") {
		t.Fatalf("composer holds %q, want the message back", got)
	}
	if len(fs.called("chat.postMessage")) != 0 {
		t.Error("posted")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.toasts) != 0 {
		t.Errorf("toasts %v", h.toasts)
	}
}

// TestBroadcastDeltaShape: broadcasts cross to plugins as Slack's mention
// ops, which is what NativeAtChannel and bChannel look for.
func TestBroadcastDeltaShape(t *testing.T) {
	delta := broadcastDoc("channel").Delta()
	b, _ := json.Marshal(delta)
	want := `{"attributes":{"slackmention":{"id":"BKchannel","label":"@channel"}},"insert":"@channel"}`
	if !strings.Contains(string(b), want) {
		t.Fatalf("delta %s, want an op %s", b, want)
	}
	var back map[string]any
	_ = json.Unmarshal(b, &back)
	if got := deltaKeywords(back); !slices.Equal(got, []string{"channel"}) {
		t.Errorf("keywords %v", got)
	}
	if _, _, ok := deltaCommand(back); ok {
		t.Error("a message read as a command")
	}

	// NativeAtChannel's relay: "/channel " + the message
	ops := append([]any{map[string]any{"insert": "/channel "}}, back["ops"].([]any)...)
	cmd, text, ok := deltaCommand(map[string]any{"ops": ops})
	if !ok || cmd != "/channel" || text != "ping <!channel> please" {
		t.Errorf("command %q %q %v", cmd, text, ok)
	}
	// bChannel's: one text op; a full-width slash counts too
	cmd, text, ok = deltaCommand(map[string]any{"ops": []any{map[string]any{"insert": "／bchannel tok123\n"}}})
	if !ok || cmd != "/bchannel" || text != "tok123" {
		t.Errorf("bchannel command %q %q %v", cmd, text, ok)
	}
	if _, _, ok := deltaCommand(map[string]any{"ops": []any{map[string]any{"insert": "// not a command\n"}}}); ok {
		t.Error("// read as a command")
	}
}

// TestBroadcastRelayedCommandSkipsCheck: a plugin's command rewrite runs
// as chat.command before the keyword check, as in Slack.
func TestBroadcastRelayedCommandSkipsCheck(t *testing.T) {
	app, fs := broadcastApp(t, noBroadcastPerms)
	d := broadcastDoc("channel")
	ops := append([]any{map[string]any{"insert": "/channel "}}, d.Delta()["ops"].([]any)...)
	o := &outgoing{doc: d, text: d.Mrkdwn(), blocks: d.RichTextBlocks()}
	j := &sendJob{api: coreAPI{app}, ch: tChOpen, o: o}
	err := j.prepareAndSend(context.Background(), prepareSendArgs{ChannelID: tChOpen, Delta: map[string]any{"ops": ops}, SlashCommandAppID: "A08FPGU02A2"})
	if err != nil {
		t.Fatal(err)
	}
	cmds := fs.called("chat.command")
	if len(cmds) != 1 || cmds[0].Get("command") != "/channel" || cmds[0].Get("text") != "ping <!channel> please" || cmds[0].Get("channel") != tChOpen {
		t.Fatalf("chat.command %v", cmds)
	}
	if len(fs.called("chat.postMessage")) != 0 {
		t.Error("also posted")
	}
}
