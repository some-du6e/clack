package ui

// thread-reply-toast round 2 (Karim, 2026-11-22, Clack for Windows
// α.137/α.138: "istg i did not get this notif in windows"): a followed
// thread's reply in a conversation core holds for its lookup still toasts
// and reaches Activity, in order with the conversation's earlier messages;
// every notification decision writes its line (notify_decisions.go);
// a desktop_notification for a message the client never took is dropped
// as Slack drops it (19); and the plugins' showNotification gets what
// Slack's maybeShowNotification hands it. Synthetic people and text only.

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/logtag"
)

// followEnv is a live client of fakeslack, its socket up and its desktop
// notifications recorded.
func followEnv(t *testing.T) (*perfEnv, *fakeslack.Server, *core.App, *timedNotes) {
	t.Helper()
	t.Setenv("CLACK_HOME", t.TempDir()) // no plugin state shared with other tests
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	c := e.a.data.core
	c.SetRealtime(true)
	e.until("the socket", 20*time.Second, func() bool { return c.RTM != nil && c.RTM.Connected() })
	rec := &timedNotes{}
	e.a.notify = rec
	return e, fs, c, rec
}

// noteWith waits up to d for the recorded notification whose body holds
// text, and returns it (nil if none came).
func noteWith(e *perfEnv, rec *timedNotes, text string, d time.Duration) *desktopNote {
	var got *desktopNote
	e.untilSoft(d, func() bool {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		for _, n := range rec.notes {
			if strings.Contains(n.Body, text) {
				got = n
				return true
			}
		}
		return false
	})
	return got
}

// A followed thread's reply in a conversation core doesn't know yet, whose
// conversations.info is slow: core holds the conversation's events until
// the lookup answers (pump_later.go), and the reply's desktop_notification
// waits behind them, as Slack's routeMessages dispatches the socket's
// events in order once the message before it has its channel. The group
// DM's first message toasts first, the reply second, and the reply's
// thread reaches Activity. Let through at once (α.137), the reply's toast
// went out ahead of the message before it.
func TestFollowedReplyBehindAHeldConversation(t *testing.T) {
	e, fs, c, rec := followEnv(t)
	e.a.focused = false // the viewer is in another app
	evs, stop := c.Subscribe()
	t.Cleanup(stop)
	var mu sync.Mutex
	var acts []core.ActivityItem
	go func() {
		for ev := range evs {
			if ev.Type == "activity" && ev.Activity != nil {
				mu.Lock()
				acts = append(acts, *ev.Activity)
				mu.Unlock()
			}
		}
	}()
	// a group DM made after the client started (another device's): core
	// doesn't know it, fakeslack's conversations.info does
	members := []string{fakeslack.SelfID, fs.UserIDs()[3], fs.UserIDs()[4]}
	const gdm = "G0HELDGDM01"
	fs.AddClosedMPDM(gdm, "mpdm-synth-held-1", members)
	if _, known := c.S.Conv(gdm); known {
		t.Fatal("core knows the group DM already: the case holds nothing")
	}
	root, _ := fs.PostQuiet(gdm, fakeslack.SelfID, "a synthetic question the viewer asked in the group DM")
	fs.SetFault("conversations.info", fakeslack.Fault{Delay: 800 * time.Millisecond})
	first := "a synthetic first line in the group DM"
	_, ev := fs.PostQuiet(gdm, members[1], first)
	fs.Push(ev)
	reply := "a synthetic answer in the viewer's thread there"
	fs.PushFollowedReplyLive(gdm, root, members[2], reply, false)

	n1, n2 := noteWith(e, rec, first, 20*time.Second), noteWith(e, rec, reply, 20*time.Second)
	if n1 == nil || n2 == nil {
		t.Fatalf("notifications: the first message's %v, the reply's %v; want both", n1 != nil, n2 != nil)
	}
	rec.mu.Lock()
	i1, i2 := -1, -1
	for i, n := range rec.notes {
		switch n {
		case n1:
			i1 = i
		case n2:
			i2 = i
		}
	}
	rec.mu.Unlock()
	if i2 < i1 {
		t.Errorf("the reply's notification (#%d) went out ahead of the group DM's first message's (#%d), which came first", i2, i1)
	}
	if !e.untilSoft(10*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, it := range acts {
			if it.Type == "thread_v2" && it.Channel == gdm && it.ThreadTS == root {
				return true
			}
		}
		return false
	}) {
		t.Error("no Activity item for the reply's thread")
	}
}

// notifyLine is a decision line as clack.log has it: an action, Slack's
// reason (a number, or a word for a check Slack logs by text) and the
// tags (logtag's "-" for an empty id), nothing else.
var notifyLine = regexp.MustCompile(`^notify: (show|skip [0-9]+|skip [a-z][a-z-]*) ([a-z]+:[0-9a-f]{6}|-)/(ts:[0-9a-f]{6}|-)$`)

// Every notification decision writes one line with Slack's reason: a
// followed thread's reply is a skip 6 on the message path (a reply that
// doesn't mention the viewer) and a show on the desktop_notification's;
// one whose thread is on screen in the focused window is a skip 16; a
// desktop_notification that isn't a reply is a skip 7. No line holds a
// name or text, only tags.
func TestNotifyDecisionLogLikeSlack(t *testing.T) {
	e, fs, c, rec := followEnv(t)
	logs := captureLog(t)
	// two channels the viewer is in: fakeslack numbers a thread's quiet
	// replies from its root's second, so two threads' first replies in one
	// channel can share a ts (Slack's never do), and the second read as
	// shown (21)
	ch, ch2 := fs.ChannelIDs()[2], fs.ChannelIDs()[3]
	for _, id := range []string{ch, ch2} {
		if _, known := c.S.Conv(id); !known {
			t.Fatalf("core doesn't know %s", id)
		}
	}
	replier := fs.UserIDs()[3]
	at := func(ts string) string { return logtag.ID(ch) + "/" + logtag.ID(ts) }
	wantLine := func(what, line string) {
		t.Helper()
		if !e.untilSoft(10*time.Second, func() bool {
			for _, l := range logs.matching("notify: ") {
				if l == line {
					return true
				}
			}
			return false
		}) {
			t.Errorf("%s: no %q in clack.log (its notify lines: %q)", what, line, logs.matching("notify: "))
		}
	}

	// a followed thread's reply, nothing on screen
	e.a.focused = false
	root, _ := fs.PostQuiet(ch, fakeslack.SelfID, "a synthetic question the viewer asked")
	ts := fs.PushFollowedReplyLive(ch, root, replier, "a synthetic answer, number 1", false)
	wantLine("the reply's message", "notify: skip 6 "+at(ts))
	wantLine("the reply's desktop_notification", "notify: show "+at(ts))
	if noteWith(e, rec, "number 1", 10*time.Second) == nil {
		t.Error("no notification for the reply")
	}

	// a desktop_notification that isn't a thread's reply
	solo := fmt.Sprintf("%d.000300", time.Now().Unix())
	fs.Push(map[string]any{"type": "desktop_notification", "channel": ch, "ts": solo, "msg": solo, "thread_ts": solo,
		"user": replier, "content": "a synthetic line", "title": "Fake Club", "subtitle": "#x"})
	wantLine("a desktop_notification that isn't a reply", "notify: skip 7 "+at(solo))

	// the thread on screen in the focused window
	root2, _ := fs.PostQuiet(ch2, fakeslack.SelfID, "another synthetic question")
	e.a.OpenThread(ch2, root2)
	e.until("the thread's pane", 15*time.Second, func() bool {
		return e.a.secondary.kind == secThread && e.a.secondary.open() && e.threadView(ch2, root2) != nil
	})
	e.a.focused = true
	ts2 := fs.PushFollowedReplyLive(ch2, root2, replier, "a synthetic answer, number 2", false)
	wantLine("the open thread's reply", "notify: skip 16 "+logtag.ID(ch2)+"/"+logtag.ID(ts2))

	for _, l := range logs.matching("notify: ") {
		if strings.HasPrefix(l, "notify: show ") || strings.HasPrefix(l, "notify: skip ") {
			if !notifyLine.MatchString(l) {
				t.Errorf("a decision line %q holds more than its reason and tags", l)
			}
		}
	}
}

// A desktop_notification for a reply newer than every message the client
// took is dropped as Slack drops it (notifications.maxTs: "Message is
// greater than latest in store, treating as duplicate", 19), with its
// line; once the reply's own message has come, its desktop_notification
// toasts. Clack had no such check: more permissive than Slack.
func TestFollowedReplyNotificationAheadOfItsMessage(t *testing.T) {
	e, fs, c, rec := followEnv(t)
	logs := captureLog(t)
	e.a.focused = false
	ch := fs.ChannelIDs()[2]
	replier := fs.UserIDs()[3]
	root, _ := fs.PostQuiet(ch, fakeslack.SelfID, "a synthetic question the viewer asked")
	ts := fmt.Sprintf("%d.000100", time.Now().Unix()+3600) // newer than anything the store took
	text := "a synthetic answer whose message hasn't come"
	fs.Push(fs.FollowedReplyNotification(ch, root, ts, replier, text))
	skip := "notify: skip 19 " + logtag.ID(ch) + "/" + logtag.ID(ts)
	e.untilSoft(3*time.Second, func() bool {
		for _, l := range logs.matching("notify: ") {
			if l == skip {
				return true
			}
		}
		return false
	})
	if n := noteWith(e, rec, text, 1500*time.Millisecond); n != nil {
		t.Errorf("a desktop_notification for a message the client never took notified (Slack drops it: 19)")
	}
	if !contains(logs.matching("notify: "), skip) {
		t.Errorf("no %q in clack.log (its notify lines: %q)", skip, logs.matching("notify: "))
	}
	// the reply's own message (core takes it into maxTs as it comes, before
	// it announces it), then its desktop_notification
	evs, stop := c.Subscribe()
	t.Cleanup(stop)
	fs.Push(map[string]any{"type": "message", "channel": ch, "user": replier, "text": text, "ts": ts, "event_ts": ts,
		"thread_ts": root, "parent_user_id": fakeslack.SelfID})
	announced := time.After(10 * time.Second)
	for heard := false; !heard; {
		select {
		case ev := <-evs:
			heard = ev.Type == "message" && ev.Channel == ch && ev.TS == ts
		case <-announced:
			t.Fatal("core never announced the reply's message")
		}
	}
	fs.Push(fs.FollowedReplyNotification(ch, root, ts, replier, text))
	if noteWith(e, rec, text, 10*time.Second) == nil {
		t.Error("no notification for the reply once its message had come")
	}
}

// The plugins' showNotification gets what Slack's maybeShowNotification
// hands it for a followed thread's reply: {message: the
// desktop_notification event, modelObject: its conversation, options:
// {skip_formatting, launch_uri, image_uri, avatar_image}}. A patch that
// hides a blocked sender by message.user (Slick's BetterBlock) hides the
// reply; the body is the content the patch passes on. Clack passed the
// body text as message: no plugin could see who sent it.
func TestFollowedReplyNotificationArgsLikeSlack(t *testing.T) {
	e, fs, _, rec := followEnv(t)
	e.until("Taut", 60*time.Second, func() bool { return e.a.Taut() != nil })
	tt := e.a.Taut()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	blocked := fs.UserIDs()[4]
	pluginID := fmt.Sprintf("NotifyArgsProbe%d", time.Now().UnixNano())
	id, err := tt.Import(ctx, []byte(`(() => {
const TautPlugin = globalThis.TautPlugin
class NotifyArgsProbe extends TautPlugin {
  static id = '`+pluginID+`'
  static pluginName = 'Notify args probe'
  static description = 'Records showNotification arguments and hides a blocked sender, as BetterBlock does'
  static category = 'notifications'
  static authors = ['test']
  static defaultConfig = { enabled: false }
  start() {
    globalThis.__notifyArgs = []
    globalThis.__notifyArgsSeen = () => globalThis.__notifyArgs
    this.off = this.api.redux.patchThunk('showNotification', (original) => (args) => {
      globalThis.__notifyArgs.push(JSON.parse(JSON.stringify(args ?? null)))
      if (args?.message?.user === '`+blocked+`') return () => Promise.resolve()
      return original(args)
    })
  }
  stop() { this.off?.() }
}
return NotifyArgsProbe
})()`), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := tt.SetEnabled(ctx, id, true); err != nil {
		t.Fatal(err)
	}
	e.until("the showNotification patch", 10*time.Second, func() bool { return tt.ThunkPatched("showNotification") })
	rt, err := tt.JS()
	if err != nil || rt == nil {
		t.Fatalf("no JS runtime: %v", err)
	}
	e.a.focused = false
	ch := fs.ChannelIDs()[2]
	root, _ := fs.PostQuiet(ch, fakeslack.SelfID, "a synthetic question the viewer asked")
	hidden := "a synthetic answer from someone the viewer blocked"
	fs.PushFollowedReplyLive(ch, root, blocked, hidden, false)
	shown := "a synthetic answer from someone else"
	okTS := fs.PushFollowedReplyLive(ch, root, fs.UserIDs()[3], shown, false)

	if noteWith(e, rec, shown, 15*time.Second) == nil {
		t.Fatal("no notification for the reply nobody hides")
	}
	if n := noteWith(e, rec, hidden, 500*time.Millisecond); n != nil {
		t.Error("the blocked sender's reply notified: the patch saw no message.user to hide it by")
	}
	raw, err := rt.Call(ctx, "__notifyArgsSeen")
	if err != nil {
		t.Fatal(err)
	}
	var seen []struct {
		Message     json.RawMessage `json:"message"`
		ModelObject struct {
			ID string `json:"id"`
		} `json:"modelObject"`
		Options map[string]any `json:"options"`
	}
	_ = json.Unmarshal(raw, &seen)
	found := false
	for _, s := range seen {
		var full map[string]any
		if json.Unmarshal(s.Message, &full) != nil {
			continue // not an object: the body text Clack passed
		}
		if full["msg"] != okTS && full["ts"] != okTS {
			continue
		}
		found = true
		if full["type"] != "desktop_notification" || !strings.Contains(fmt.Sprint(full["content"]), shown) {
			t.Errorf("showNotification's message %s, want the desktop_notification event with its content", s.Message)
		}
		if s.ModelObject.ID != ch {
			t.Errorf("showNotification's modelObject is %q, want the conversation %s", s.ModelObject.ID, ch)
		}
		if s.Options["skip_formatting"] != true || s.Options["launch_uri"] == nil || s.Options["avatar_image"] == nil {
			t.Errorf("showNotification's options %v, want skip_formatting with the event's launch_uri, avatar_image (and image_uri when it has one)", s.Options)
		}
	}
	if !found {
		t.Errorf("no showNotification call carried the reply's desktop_notification as its message (%d calls: %s)", len(seen), raw)
	}
}
