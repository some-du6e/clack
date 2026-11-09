package composer

import (
	"context"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme/pixeltest"
)

// The rest of Slack's broadcast send (broadcast.go, broadcast_warning.go):
// the "Notify N people?" confirmation, the large-channel stop and the
// channel's enable_at_channel pref. Team prefs are fixtures shaped like
// Hack Club's team.prefs.get (warn_before_at_channel "always",
// loud_channel_mentions_limit 10000; read-only probe 2026-11-08).

// warnPrefs installs team prefs on app.
func warnPrefs(app *core.App, kv map[string]any) {
	m := map[string]json.RawMessage{}
	for k, v := range kv {
		b, _ := json.Marshal(v)
		m[k] = b
	}
	app.SetTeamPrefs(m)
}

// people answers edge users/counts with n people.
func people(fs *fakeSlack, n int) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.reply == nil {
		fs.reply = map[string]any{}
	}
	fs.reply["edge/users/counts"] = map[string]any{"ok": true, "counts": map[string]any{"people": n, "members": n}}
}

func TestBroadcastWarningCopy(t *testing.T) {
	for _, tc := range []struct {
		name               string
		kw                 string
		mpim, scheduled    bool
		people, timezones  int
		title, body, label string
		channel            bool
	}{
		{name: "@channel", kw: "channel", people: 29, title: "Notify 29 people?",
			body: "Using <!channel> pings everyone in the channel. It’s rather noisy! Are you sure you want to send this?", label: "Send"},
		{name: "@channel, time zones", kw: "channel", people: 12344, timezones: 23, title: "Notify 12,344 people across 23 time zones?",
			body: "Using <!channel> pings everyone in the channel. It’s rather noisy! Are you sure you want to send this?", label: "Send"},
		{name: "one time zone", kw: "channel", people: 9, timezones: 1, title: "Notify 9 people?", label: "Send",
			body: "Using <!channel> pings everyone in the channel. It’s rather noisy! Are you sure you want to send this?"},
		{name: "one person", kw: "everyone", people: 1, title: "Notify 1 person?", label: "Send",
			body: "Using <!everyone> pings everyone in the channel. It’s rather noisy! Are you sure you want to send this?"},
		{name: "@here", kw: "here", people: 29, timezones: 5, title: "Notify everyone currently online in ", channel: true, label: "Send",
			body: "Using <!here> pings everyone in the channel who’s currently online. It’s a bit noisy! Are you sure you want to send this?"},
		{name: "@here, group DM", kw: "here", mpim: true, people: 7, title: "Notify everyone currently online?", label: "Send",
			body: "Using <!here> pings everyone in the channel who’s currently online. It’s a bit noisy! Are you sure you want to send this?"},
		{name: "scheduled @channel", kw: "channel", scheduled: true, people: 29, title: "Notify everyone in ", channel: true, label: "Schedule Message",
			body: "Using <!channel> will notify about 29 people when this message sends (the number of members can change day by day). This type of mention can be a bit noisy — please confirm that this is what you’d like to do."},
		{name: "scheduled, time zones", kw: "channel", scheduled: true, people: 29, timezones: 3, title: "Notify everyone in this channel across 3 timezones?", label: "Schedule Message",
			body: "Using <!channel> will notify about 29 people when this message sends (the number of members can change day by day). This type of mention can be a bit noisy — please confirm that this is what you’d like to do."},
		{name: "scheduled, group DM", kw: "channel", mpim: true, scheduled: true, people: 6, title: "Notify everyone?", label: "Schedule Message",
			body: "Using <!channel> will notify 6 people when this message sends. This type of mention can be a bit noisy — please confirm that this is what you’d like to do."},
		{name: "scheduled @here", kw: "here", scheduled: true, people: 29, title: "Notify everyone who’ll be online in ", channel: true, label: "Schedule Message",
			body: "Using <!here> will notify everyone in the channel who’s online at the time this message is sent. It can be a bit noisy — please confirm that this is what you’d like to do."},
		{name: "scheduled @here, group DM", kw: "here", mpim: true, scheduled: true, people: 6, title: "Notify everyone who’ll be online?", label: "Schedule Message",
			body: "Using <!here> will notify everyone who’s online at the time this message is sent. It can be a bit noisy — please confirm that this is what you’d like to do."},
	} {
		c := broadcastWarningCopy(tc.kw, tc.mpim, tc.people, tc.timezones, tc.scheduled)
		if c.Title != tc.title || c.Channel != tc.channel || c.Body != tc.body || c.Button != tc.label {
			t.Errorf("%s:\n got %q %v %q %q\nwant %q %v %q %q", tc.name, c.Title, c.Channel, c.Body, c.Button, tc.title, tc.channel, tc.body, tc.label)
		}
	}
	if got := adminNote("https://x.slack.com/admin/settings#permissions"); got != "Pssst, admins… You can control this warning in <https://x.slack.com/admin/settings#permissions|Workspace settings>." {
		t.Errorf("admin note %q", got)
	}
}

// confirmRecorder answers the warning with ok and records what it saw.
type confirmRecorder struct {
	mu   sync.Mutex
	asks []broadcastAsk
	ok   bool
}

func (r *confirmRecorder) confirm(_ context.Context, a broadcastAsk) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.asks = append(r.asks, a)
	return r.ok
}

func (r *confirmRecorder) n() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.asks)
}

// warnSend runs a channel composer's send (includeBroadcastKeywordWarning)
// of d with confirm answering.
func warnSend(t *testing.T, app *core.App, ch, ts string, d *Doc, confirm *confirmRecorder) error {
	t.Helper()
	withTaut(t, nil)
	o := &outgoing{doc: d, text: d.Mrkdwn(), blocks: d.RichTextBlocks()}
	j := &sendJob{api: coreAPI{app}, ch: ch, ts: ts, o: o, draftID: ch, view: ViewContextMessagePane, warn: ts == "", confirm: confirm.confirm}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return j.run(ctx)
}

// TestBroadcastConfirmJob: with warn_before_at_channel "always", a
// keyword the viewer may use in a channel of more than 5 people asks
// first; cancelling stops the send quietly (Shouty rooster), accepting
// posts it once. Threads and small channels never ask; neither does a
// team without the pref.
func TestBroadcastConfirmJob(t *testing.T) {
	app, fs := broadcastApp(t, nil) // allowed
	people(fs, 30)

	// no pref: Slack never warns
	never := &confirmRecorder{ok: false}
	if err := warnSend(t, app, tChOpen, "", broadcastDoc("channel"), never); err != nil || never.n() != 0 {
		t.Fatalf("without the pref: %v, %d asks", err, never.n())
	}
	posted := len(fs.called("chat.postMessage"))

	warnPrefs(app, map[string]any{"warn_before_at_channel": "always", "loud_channel_mentions_limit": 10000})
	cancel := &confirmRecorder{ok: false}
	err := warnSend(t, app, tChOpen, "", broadcastDoc("channel"), cancel)
	if !isShoutyRooster(err) {
		t.Fatalf("cancelled: %v, want Shouty rooster in effect", err)
	}
	if n := len(fs.called("chat.postMessage")); n != posted {
		t.Fatalf("posted after Edit Message (%d)", n-posted)
	}
	if cancel.n() != 1 || cancel.asks[0].Channel != tChOpen || !slices.Equal(cancel.asks[0].Used, []string{"channel"}) || cancel.asks[0].Scheduled {
		t.Fatalf("asks %+v", cancel.asks)
	}

	accept := &confirmRecorder{ok: true}
	if err := warnSend(t, app, tChOpen, "", broadcastDoc("here"), accept); err != nil {
		t.Fatalf("accepted: %v", err)
	}
	posts := fs.called("chat.postMessage")
	if accept.n() != 1 || len(posts) != posted+1 || !strings.Contains(posts[len(posts)-1].Get("blocks"), `"range":"here"`) {
		t.Fatalf("accepted: %d asks, %d posts", accept.n(), len(posts)-posted)
	}

	// a thread composer doesn't ask (Slack refuses the keyword there)
	thread := &confirmRecorder{ok: true}
	if err := warnSend(t, app, tChOpen, "1700000000.000100", broadcastDoc("channel"), thread); !isProhibitedBroadcast(err) || thread.n() != 0 {
		t.Errorf("thread: %v, %d asks", err, thread.n())
	}
	// plain text "@channel" is no keyword
	d := NewDoc()
	d.Insert(0, "type @channel to ping", Attr{})
	if err := warnSend(t, app, tChOpen, "", d, thread); err != nil || thread.n() != 0 {
		t.Errorf("plain text: %v, %d asks", err, thread.n())
	}
	// 5 people or fewer: no warning
	app2, fs2 := broadcastApp(t, nil)
	people(fs2, 5)
	warnPrefs(app2, map[string]any{"warn_before_at_channel": "always"})
	few := &confirmRecorder{ok: false}
	if err := warnSend(t, app2, tChOpen, "", broadcastDoc("channel"), few); err != nil || few.n() != 0 {
		t.Errorf("5 people: %v, %d asks", err, few.n())
	}
	// a keyword Slack refuses never asks: the refusal stands
	app3, fs3 := broadcastApp(t, noBroadcastPerms)
	people(fs3, 30)
	warnPrefs(app3, map[string]any{"warn_before_at_channel": "always"})
	refused := &confirmRecorder{ok: true}
	if err := warnSend(t, app3, tChOpen, "", broadcastDoc("channel"), refused); !isProhibitedBroadcast(err) || refused.n() != 0 {
		t.Errorf("not allowed: %v, %d asks", err, refused.n())
	}
}

// TestBroadcastLargeChannelRefused: on Grid, loud_channel_mentions_limit
// takes @channel/@here away in channels with at least that many people;
// Slackbot says so. Admins are exempt.
func TestBroadcastLargeChannelRefused(t *testing.T) {
	app, fs := broadcastApp(t, nil) // CAN_AT_CHANNEL
	people(fs, 10000)
	warnPrefs(app, map[string]any{"loud_channel_mentions_limit": 10000, "warn_before_at_channel": "always"})
	ask := &confirmRecorder{ok: true}
	if err := warnSend(t, app, tChOpen, "", broadcastDoc("here"), ask); !isProhibitedBroadcast(err) {
		t.Fatalf("large channel: %v, want refused", err)
	}
	if ask.n() != 0 {
		t.Errorf("asked %d times before refusing", ask.n())
	}
	m, ok := slackbotSaid(app, tChOpen)
	if !ok || m.Text != "Usage of *@here* is restricted in this large channel." {
		t.Fatalf("Slackbot said %q (%v)", m.Text, ok)
	}
	if n := len(fs.called("chat.postMessage")); n != 0 {
		t.Errorf("%d posts", n)
	}
	if got := app.BroadcastKeywordsFor(tChOpen); len(got) != 0 {
		t.Errorf("@ rows offer %v in a stopped channel", got)
	}

	admin, fsA := broadcastApp(t, nil)
	people(fsA, 10000)
	warnPrefs(admin, map[string]any{"loud_channel_mentions_limit": 10000})
	self := admin.S.Self
	self.IsAdmin = true
	admin.S.PutUsers([]slack.User{self})
	if err := warnSend(t, admin, tChOpen, "", broadcastDoc("channel"), &confirmRecorder{}); err != nil {
		t.Fatalf("admin: %v", err)
	}
	if n := len(fsA.called("chat.postMessage")); n != 1 {
		t.Errorf("admin: %d posts", n)
	}
}

// TestBroadcastDisabledRefused: a channel whose enable_at_channel is off
// refuses @here and @channel with Slack's "has been disabled" line; the
// pref is read with channels.prefs.get before the send.
func TestBroadcastDisabledRefused(t *testing.T) {
	app, fs := broadcastApp(t, nil)
	people(fs, 3) // also makes fs.reply
	fs.mu.Lock()
	fs.reply["channels.prefs.get"] = map[string]any{"ok": true, "channel_id": tChOpen, "pref_name": "enable_at_channel", "pref_value": map[string]any{"enabled": false}}
	fs.mu.Unlock()
	if err := runSend(t, app, tChOpen, "", broadcastDoc("here")); !isProhibitedBroadcast(err) {
		t.Fatalf("disabled: %v", err)
	}
	m, _ := slackbotSaid(app, tChOpen)
	if m.Text != "*@here* has been disabled in this channel, so your message cannot be sent." {
		t.Errorf("Slackbot said %q", m.Text)
	}
	reqs := fs.called("channels.prefs.get")
	if len(reqs) == 0 || reqs[0].Get("channel_id") != tChOpen || reqs[0].Get("pref_name") != "enable_at_channel" {
		t.Fatalf("channels.prefs.get %v", reqs)
	}
	if n := len(fs.called("chat.postMessage")); n != 0 {
		t.Errorf("%d posts", n)
	}

	// Hack Club's answer: pref_value {} is enabled
	fs.mu.Lock()
	fs.reply["channels.prefs.get"] = map[string]any{"ok": true, "channel_id": tChOpen, "pref_name": "enable_at_channel", "pref_value": map[string]any{}}
	fs.mu.Unlock()
	if err := runSend(t, app, tChOpen, "", broadcastDoc("channel")); err != nil {
		t.Fatalf("enabled: %v", err)
	}
	// no keyword, no lookup
	n := len(fs.called("channels.prefs.get"))
	d := NewDoc()
	d.Insert(0, "hello", Attr{})
	if err := runSend(t, app, tChOpen, "", d); err != nil {
		t.Fatal(err)
	}
	if len(fs.called("channels.prefs.get")) != n {
		t.Error("channels.prefs.get for a message without keywords")
	}
}

// frameModal lays the dialog out once (the header and the body draw).
func frameModal(t *testing.T, d *broadcastWarning) {
	t.Helper()
	ops := new(op.Ops)
	gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(1200, 900)), Now: time.Now()}
	slackc.Frame(gtx)
	if dims := d.modal.Layout(gtx); dims.Size.X == 0 {
		t.Error("the dialog drew nothing")
	}
}

// TestBroadcastConfirmComposer: the composer's own send opens the dialog
// (its title from conversations.info's counts, last_seen written);
// Edit Message puts the message back with no toast and posts nothing,
// Send posts it.
func TestBroadcastConfirmComposer(t *testing.T) {
	app, fs := broadcastApp(t, nil)
	people(fs, 30) // also makes fs.reply
	fs.mu.Lock()
	fs.reply["conversations.info"] = map[string]any{"ok": true, "channel": map[string]any{"id": tChOpen, "timezone_count": 4,
		"display_counts": map[string]any{"display_counts": 31, "guest_counts": 0}}}
	fs.mu.Unlock()
	warnPrefs(app, map[string]any{"warn_before_at_channel": "always"})
	withTaut(t, nil)
	h := &toastHost{slickHost: slickHost{app: app, th: theme.New(false)}}
	clearDraft(tChOpen, "")
	c := New(h, tChOpen, "")
	t.Cleanup(func() { c.Close(); clearDraft(tChOpen, "") })

	var mu sync.Mutex
	var opened []*broadcastWarning
	onBroadcastWarning = func(d *broadcastWarning) { mu.Lock(); opened = append(opened, d); mu.Unlock() }
	t.Cleanup(func() { onBroadcastWarning = nil })
	dialog := func(i int) *broadcastWarning {
		t.Helper()
		var d *broadcastWarning
		waitFor(t, 8*time.Second, "the warning dialog", func() bool {
			runRestores()
			mu.Lock()
			defer mu.Unlock()
			if len(opened) > i {
				d = opened[i]
			}
			return d != nil
		})
		return d
	}

	c.ed.SetDoc(broadcastDoc("channel"))
	c.submit()
	d := dialog(0)
	waitFor(t, 8*time.Second, "conversations.info's counts in the title", func() bool {
		return d.copy().Title == "Notify 30 people across 4 time zones?"
	})
	if got := d.copy().Body; !strings.Contains(got, "<!channel>") {
		t.Errorf("body %q", got)
	}
	reqs := fs.called("conversations.info")
	if len(reqs) == 0 || reqs[0].Get("timezone_count") != "true" || reqs[0].Get("display_counts") != "true" {
		t.Errorf("conversations.info %v", reqs)
	}
	waitFor(t, 8*time.Second, "last_seen_at_channel_warning written", func() bool {
		for _, f := range fs.called("users.prefs.set") {
			if f.Get("name") == "last_seen_at_channel_warning" && f.Get("value") != "" {
				return true
			}
		}
		return false
	})
	frameModal(t, d)
	if !c.ed.Empty() {
		t.Errorf("composer holds %q while the dialog is up", c.ed.Doc().Text())
	}

	d.choose(false) // Edit Message
	waitFor(t, 8*time.Second, "the message back in the composer", func() bool {
		runRestores()
		return strings.Contains(c.ed.Doc().Text(), "ping")
	})
	if n := len(fs.called("chat.postMessage")); n != 0 {
		t.Fatalf("posted after Edit Message (%d)", n)
	}

	c.submit()
	dialog(1).choose(true) // Send
	waitFor(t, 8*time.Second, "the post", func() bool { return len(fs.called("chat.postMessage")) == 1 })
	runRestores()
	if !c.ed.Empty() {
		t.Errorf("composer holds %q after Send", c.ed.Doc().Text())
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.toasts) != 0 {
		t.Errorf("toasts %v", h.toasts)
	}
}

// TestBroadcastConfirmThreadComposer: a thread composer never opens the
// dialog (InputContainer doesn't pass includeBroadcastKeywordWarning).
func TestBroadcastConfirmThreadComposer(t *testing.T) {
	app, fs := broadcastApp(t, nil)
	people(fs, 30)
	warnPrefs(app, map[string]any{"warn_before_at_channel": "always"})
	withTaut(t, nil)
	h := &toastHost{slickHost: slickHost{app: app, th: theme.New(false)}}
	const ts = "1700000000.000100"
	clearDraft(tChOpen, ts)
	c := New(h, tChOpen, ts)
	t.Cleanup(func() { c.Close(); clearDraft(tChOpen, ts) })
	opened := 0
	onBroadcastWarning = func(*broadcastWarning) { opened++ }
	t.Cleanup(func() { onBroadcastWarning = nil })
	c.ed.SetDoc(broadcastDoc("channel"))
	c.submit()
	waitFor(t, 8*time.Second, "the refused reply back in the composer", func() bool {
		runRestores()
		return strings.Contains(c.ed.Doc().Text(), "ping")
	})
	if opened != 0 {
		t.Errorf("dialog opened %d times in a thread", opened)
	}
}

// TestBroadcastWarningDraws renders the dialog (GPU, llvmpipe in CI): the
// header strip and its megaphone are on screen. BROADCAST_SHOTS_DIR saves
// light and dark PNGs.
func TestBroadcastWarningDraws(t *testing.T) {
	for _, dark := range []bool{false, true} {
		app, fs := broadcastApp(t, nil)
		people(fs, 30)
		fs.mu.Lock()
		fs.reply["conversations.info"] = map[string]any{"ok": true, "channel": map[string]any{"id": tChOpen, "timezone_count": 7,
			"display_counts": map[string]any{"display_counts": 1234, "guest_counts": 0}}}
		fs.mu.Unlock()
		self := app.S.Self
		self.IsAdmin = true // the "Pssst, admins…" line
		app.S.PutUsers([]slack.User{self})
		th := theme.New(dark)
		h := &slickHost{app: app, th: th}
		d := lastWarning(t, h, app)
		waitFor(t, 8*time.Second, "the audience", func() bool { return strings.Contains(d.copy().Title, "7 time zones") })
		const w, hgt = 900, 700
		var img *image.RGBA
		for i := 0; i < 3; i++ {
			ops := new(op.Ops)
			gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
				Constraints: layout.Exact(image.Pt(w, hgt)), Now: time.Now()}
			slackc.Frame(gtx)
			d.modal.Layout(gtx)
			if i == 2 {
				img = pixeltest.Render(t, w, hgt, ops)
			}
		}
		bg := heroBg
		if dark {
			bg = heroBgDark
		}
		all := img.Bounds()
		if n := pixeltest.Count(img, all, bg, 4); n < 5000 {
			t.Errorf("dark=%v: header strip %d px", dark, n)
		}
		if n := pixeltest.Count(img, all, heroInk, 6); n < 200 {
			t.Errorf("dark=%v: megaphone %d px", dark, n)
		}
		if dir := os.Getenv("BROADCAST_SHOTS_DIR"); dir != "" {
			name := "broadcast-warning-light.png"
			if dark {
				name = "broadcast-warning-dark.png"
			}
			f, err := os.Create(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			_ = png.Encode(f, img)
			_ = f.Close()
		}
		d.modal.Dismiss()
	}
}

// lastWarning opens the dialog through the hook and returns it.
func lastWarning(t *testing.T, h Host, app *core.App) *broadcastWarning {
	t.Helper()
	var got *broadcastWarning
	onBroadcastWarning = func(d *broadcastWarning) { got = d }
	defer func() { onBroadcastWarning = nil }()
	openBroadcastWarning(h, coreAPI{app}, broadcastAsk{Channel: tChOpen, Used: []string{"channel"}}, func(bool) {})
	if got == nil {
		t.Fatal("no dialog")
	}
	return got
}
