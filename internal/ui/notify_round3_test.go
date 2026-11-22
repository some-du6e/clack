package ui

// thread-reply-toast round 3 (2026-11-22): the notification checks as
// Slack's client has them (client-boot Fpyc, build 548618a2169f2a90), the
// toast Slick's filters see, the Threads link, and the reply bar's
// Unread mark in the frame the reply first draws (Karim: "i swear i saw
// your new reply before the Unread thing popped up"). Synthetic people and
// text only.

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/font"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/logtag"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// A desktop_notification without a ts of its own (only msg) is a reply to
// its thread_ts in Slack's gA (isReply of {ts: undefined, thread_ts}),
// and neither too new (19: "a > r" of undefined) nor read (20): it
// shows. Clack put msg in ts's place and dropped it as 19 when msg was
// newer than every message the client took.
func TestDesktopNotificationWithoutTSLikeSlack(t *testing.T) {
	e, fs, _, rec := followEnv(t)
	logs := captureLog(t)
	e.a.focused = false
	ch := fs.ChannelIDs()[2]
	root, _ := fs.PostQuiet(ch, fakeslack.SelfID, "a synthetic question the viewer asked")
	msg := fmt.Sprintf("%d.000100", time.Now().Unix()+3600) // newer than anything the store took
	ev := fs.FollowedReplyNotification(ch, root, msg, fs.UserIDs()[3], "a synthetic answer with no ts of its own")
	delete(ev, "ts")
	fs.Push(ev)
	if noteWith(e, rec, "no ts of its own", 10*time.Second) == nil {
		t.Errorf("a desktop_notification without its own ts didn't notify (its notify lines: %q)", logs.matching("notify: "))
	}
}

// shouldSuppressNotificationInActiveChannel: a message mentioning the
// viewer in the conversation open in the focused window, showing its
// messages, stays quiet (16), and so do the conversation's thread replies
// that notify the viewer (a mention, an @here): Slack's messageReceived
// hands maybeShowNotification the socket's own message, which carries no
// _hidden_reply (only the store's copies do). With the reply's thread open
// in the side pane its desktop_notification is on screen too (16). One
// whose text holds an at-command, a streamed reply as its stream settles
// (the store's copy), and one while the conversation shows another tab,
// notify.
func TestOnScreenNotificationsLikeSlack(t *testing.T) {
	t.Setenv("CLACK_HOME", t.TempDir())
	sz := fakeslack.Small
	sz.Files = true // #files-gallery and its gallery tabs (tabs_edit_test)
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	ch := fakeslack.FilesChannelID
	// tabs_edit_test's conversation: its gallery's tabs past Messages
	fs.Permissions = map[string][]string{ch: {core.PermManageChannelTabs}}
	fs.SetGalleryTabList(
		fakeslack.GalleryTab(fakeslack.FilesTabID, "files", "", "", ""),
		fakeslack.GalleryTab(tbCanvasTab, "canvas", "", fakeslack.CanvasChannel, ""),
		fakeslack.GalleryTab(tbListTab, "list", "", fakeslack.ListTodos, ""),
	)
	e := startEnv(t, fs, t.TempDir())
	e.size = image.Pt(1400, 800)
	e.waitReady()
	c := e.a.data.core
	c.SetRealtime(true)
	e.until("the socket", 20*time.Second, func() bool { return c.RTM != nil && c.RTM.Connected() })
	rec := &timedNotes{}
	e.a.notify = rec
	logs := captureLog(t)
	e.openChannel(ch)
	e.until("the conversation's view", 15*time.Second, func() bool { return e.channelView(ch) != nil })
	if !e.untilSoft(20*time.Second, func() bool { return e.a.data.tabsDone[ch] && len(e.a.channelTabs(ch)) > 1 }) {
		var keys []string
		for _, tb := range e.a.channelTabs(ch) {
			keys = append(keys, tb.key+":"+tb.typ)
		}
		t.Fatalf("setup: the conversation's tabs never came (tabsDone %v, tabs %q, route %q)", e.a.data.tabsDone[ch], keys, e.a.router.Current())
	}
	e.a.focused = true
	sender := fs.UserIDs()[3]
	lines := func(ts string) []string {
		var out []string
		for _, l := range logs.matching("notify: ") {
			if strings.HasSuffix(l, " "+logtag.ID(ch)+"/"+logtag.ID(ts)) {
				out = append(out, l)
			}
		}
		return out
	}
	line := func(ts string) string {
		if ls := lines(ts); len(ls) > 0 {
			return ls[0]
		}
		return ""
	}
	post := func(text string, extra map[string]any) string {
		ts, ev := fs.PostQuiet(ch, sender, text)
		for k, v := range extra {
			ev[k] = v
		}
		fs.Push(ev)
		e.until("its decision line", 10*time.Second, func() bool { return line(ts) != "" })
		return ts
	}
	// mentions: the conversation notifies on mentions only (1 before 16)
	me := "<@" + fakeslack.SelfID + "> "
	quiet := post(me+"a synthetic plain line", nil)
	if l := line(quiet); !strings.HasPrefix(l, "notify: skip 16 ") {
		t.Errorf("a plain message in the open conversation: %q, want skip 16", l)
	}
	if l := line(post("<!channel> a synthetic announcement", nil)); !strings.HasPrefix(l, "notify: show ") {
		t.Errorf("a message with an at-command in the open conversation: %q, want it shown (containsAtCommand)", l)
	}
	root, _ := fs.PostQuiet(ch, sender, "a synthetic thread root")
	rootSec, _ := strconv.ParseInt(strings.Split(root, ".")[0], 10, 64)
	reply := fmt.Sprintf("%d.000900", rootSec+2) // after the root and anything posted in its second
	fs.Push(map[string]any{"type": "message", "channel": ch, "user": sender, "ts": reply, "event_ts": reply, "thread_ts": root,
		"text": "<@" + fakeslack.SelfID + "> a synthetic question for you in the thread"})
	e.until("the reply's decision line", 10*time.Second, func() bool { return line(reply) != "" })
	if l := line(reply); !strings.HasPrefix(l, "notify: skip 16 ") {
		t.Errorf("a thread reply mentioning the viewer, its conversation open: %q, want skip 16 (the socket's message has no _hidden_reply)", l)
	}
	here := fmt.Sprintf("%d.000800", rootSec+2) // a thread the viewer doesn't follow: no desktop_notification comes
	fs.Push(map[string]any{"type": "message", "channel": ch, "user": sender, "ts": here, "event_ts": here, "thread_ts": root,
		"text": "<!here> a synthetic heads-up in the thread"})
	e.until("the @here reply's decision line", 10*time.Second, func() bool { return line(here) != "" })
	if l := line(here); !strings.HasPrefix(l, "notify: skip 16 ") {
		t.Errorf("an @here reply in a thread the viewer doesn't follow, its conversation open: %q, want skip 16", l)
	}
	// the viewer's own thread open in the side pane, the conversation on
	// its Messages tab: the reply's message and its desktop_notification
	// are both on screen
	mine, _ := fs.PostQuiet(ch, fakeslack.SelfID, "a synthetic question the viewer asked")
	e.a.OpenThread(ch, mine)
	e.until("the thread's pane", 15*time.Second, func() bool {
		return e.a.secondary.kind == secThread && e.a.secondary.open() && e.threadView(ch, mine) != nil
	})
	e.a.focused = true
	paneTS := fs.PushFollowedReplyLive(ch, mine, sender, me+"can you check", false)
	e.until("the side pane reply's two decision lines", 10*time.Second, func() bool { return len(lines(paneTS)) >= 2 })
	for _, l := range lines(paneTS) {
		if !strings.HasPrefix(l, "notify: skip 16 ") {
			t.Errorf("a reply mentioning the viewer in the thread open in the side pane: %q, want skip 16 for its message and its desktop_notification", l)
		}
	}
	if n := noteWith(e, rec, "can you check", 300*time.Millisecond); n != nil {
		t.Error("the reply in the thread open in the side pane notified")
	}
	// a streamed thread reply mentioning the viewer: held while it streams
	// (33), it notifies as its stream settles, its conversation on screen
	// or not, since Slack's messageChanged hands maybeShowNotification the
	// store's copy (getMessage), whose _hidden_reply keeps a reply the
	// conversation's view doesn't show off the screen
	c.SetExperimentGroup("defer_notification_3p_streaming", "on")
	streamTS := fmt.Sprintf("%d.000700", rootSec+2)
	streamed := func(state, text string) map[string]any {
		return map[string]any{"type": "message", "channel": ch, "user": sender, "ts": streamTS, "thread_ts": root, "text": text, "streaming_state": state}
	}
	fs.Push(streamed("in_progress", me+"a synthetic answer, streaming"))
	e.until("the streamed reply's decision line", 10*time.Second, func() bool { return line(streamTS) != "" })
	fs.Push(map[string]any{"type": "message", "subtype": "message_changed", "channel": ch, "hidden": true, "ts": streamTS,
		"message": streamed("completed", me+"a synthetic answer, settled")})
	e.until("the settled reply's decision line", 10*time.Second, func() bool { return len(lines(streamTS)) >= 2 })
	if ls := lines(streamTS); !strings.HasPrefix(ls[0], "notify: skip 33 ") || !strings.HasPrefix(ls[1], "notify: show ") {
		t.Errorf("a streamed thread reply mentioning the viewer, its conversation open: %q, want skip 33 while it streams and shown as it settles (the store's copy has _hidden_reply)", ls)
	}
	if e.a.chTab == nil {
		e.a.chTab = map[string]int{}
	}
	e.a.chTab[ch] = 1 // the Files tab, not its messages
	e.frame()
	other := post(me+"a synthetic line while another tab shows", nil)
	if l := line(other); !strings.HasPrefix(l, "notify: show ") {
		t.Errorf("a message while the conversation shows its Files tab: %q, want it shown (the Messages tab isn't on screen)", l)
	}
	if n := noteWith(e, rec, "a synthetic plain line", 300*time.Millisecond); n != nil {
		t.Error("the plain message in the open conversation notified")
	}
}

// AllThreadsRowElement: the Threads link is bold while the threads have
// unread replies (has_unreads, or a channel's unread_count_by_channel)
// and its badge is channel_badges' thread_mentions. Clack drew it plain,
// with no badge.
func TestThreadsLinkLikeSlack(t *testing.T) {
	e, fs, c, _ := followEnv(t)
	ch := fs.ChannelIDs()[2]
	// a row's badge draws just before its label (sidebarRowBody): the
	// Threads link's is the "37" right before "Threads", not the rail's
	drawn := func() (threads, huddles font.Weight, badge bool) {
		var mu sync.Mutex
		prev := ""
		theme.DrawHookOpts = func(s string, o theme.LabelOpts) {
			mu.Lock()
			defer mu.Unlock()
			switch s {
			case "Threads":
				threads = o.Font.Weight
				badge = badge || prev == "37"
			case "Huddles":
				huddles = o.Font.Weight
			}
			prev = s
		}
		e.frame()
		theme.DrawHookOpts = nil
		return
	}
	state := func(unread bool, mentions int) {
		by := map[string]int{}
		if unread {
			by[ch] = 1
		}
		fs.Push(map[string]any{"type": "update_thread_state", "has_unreads": unread, "mention_count": mentions,
			"unread_count_by_channel": by, "mention_count_by_channel": map[string]int{}, "org_wide": c.IsEnterprise(),
			"channel_badges": map[string]int{"thread_mentions": mentions}})
		e.until("the threads' state", 10*time.Second, func() bool { return c.ThreadsState().Unread == unread })
		e.settle(100 * time.Millisecond)
	}
	state(false, 0)
	if th, hu, _ := drawn(); th != hu {
		t.Fatalf("with nothing unread the Threads link draws at %v, Huddles at %v", th, hu)
	}
	state(true, 37)
	th, hu, badge := drawn()
	if th <= hu {
		t.Errorf("with unread threads the Threads link draws at %v, no bolder than Huddles' %v (hasUnreads)", th, hu)
	}
	if !badge {
		t.Error("with 37 thread mentions the Threads link has no badge (channel_badges' thread_mentions)")
	}
}

// slickProbe is a Slick plugin in build:plugin's and build.mjs's shapes:
// a renderer that does nothing and a main half with body.
func slickProbe(id, caps, body string) (renderer, main string) {
	renderer = fmt.Sprintf(`// slick-plugin {"id":%q,"name":%q,"description":"probe"}
(globalThis.__slickPlugins ??= []).push((require) => {
var import_slick = require("$slick");
class Probe extends import_slick.SlickPlugin {
  static id = %q;
  static pluginName = %q;
  static settings = {};
  static requiredAPIs = [];
  async start() {}
}
return Probe;
});
`, id, id, id, id)
	main = fmt.Sprintf(`// slick-main {"id":%q}
(globalThis.__slickMainPlugins ??= []).push((require) => {
const plugin = { id: %q, capabilities: %s, %s };
return plugin;
});
`, id, id, caps, body)
	return renderer, main
}

// Slick's notification filters see the toast about to show, as Slick's
// Notification.prototype.show patch does: a followed thread's reply's
// title and body, and one they don't permit doesn't show. Over
// showNotification's arguments they saw an empty title and body for it
// (Slack's {message: the event, …} has neither), and couldn't keep it.
func TestSlickFiltersSeeTheToast(t *testing.T) {
	e, fs, _, rec := followEnv(t)
	logs := captureLog(t)
	e.until("Taut and Slick", 60*time.Second, func() bool { return e.a.Taut() != nil && e.a.Slick() != nil })
	sl := e.a.Slick()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	id := fmt.Sprintf("ToastProbe%d", time.Now().UnixNano())
	renderer, main := slickProbe(id, `["notifications"]`, `
  async ready(ctx) {
    globalThis.__toastSeen = [];
    globalThis.__toastSeenFn = () => globalThis.__toastSeen;
    return ctx.notifications.filter((o) => {
      globalThis.__toastSeen.push({ title: String(o.title ?? ""), body: String(o.body ?? "") });
      return !String(o.body ?? "").includes("keep this one quiet");
    });
  }`)
	for _, code := range []string{renderer, main} {
		if _, err := sl.Import(ctx, []byte(code)); err != nil {
			t.Fatal(err)
		}
	}
	if err := sl.SetEnabled(ctx, id, true); err != nil {
		t.Fatal(err)
	}
	rt, err := e.a.Taut().JS()
	if err != nil {
		t.Fatal(err)
	}
	e.until("the filter registered", 20*time.Second, func() bool {
		_, err := rt.Call(ctx, "__toastSeenFn") // ready() defines it as it adds its filter
		return err == nil
	})
	e.a.focused = false
	ch := fs.ChannelIDs()[2]
	replier := fs.UserIDs()[3]
	root, _ := fs.PostQuiet(ch, fakeslack.SelfID, "a synthetic question the viewer asked")
	fs.PushFollowedReplyLive(ch, root, replier, "a synthetic answer the filter sees", false)
	shown := noteWith(e, rec, "the filter sees", 15*time.Second)
	if shown == nil {
		t.Fatalf("no toast for the reply the filter permits (its notify lines: %q)", logs.matching("notify: "))
	}
	quietTS := fs.PushFollowedReplyLive(ch, root, replier, "please keep this one quiet", false)
	if n := noteWith(e, rec, "keep this one quiet", 3*time.Second); n != nil {
		t.Error("the reply the filter doesn't permit showed")
	}
	raw, err := rt.Call(ctx, "__toastSeenFn")
	if err != nil {
		t.Fatal(err)
	}
	var seen []struct{ Title, Body string }
	_ = json.Unmarshal(raw, &seen)
	found := false
	for _, s := range seen {
		if strings.Contains(s.Body, "the filter sees") {
			found = true
			if s.Title != shown.Title || s.Body != shown.Body {
				t.Errorf("the filter saw %q / %q, want the toast's %q / %q", s.Title, s.Body, shown.Title, shown.Body)
			}
		}
	}
	if !found {
		t.Errorf("the filter never saw the reply's toast (it saw %s)", raw)
	}
	kept := "notify: a Slick filter kept " + logtag.ID(ch) + "/" + logtag.ID(quietTS) + " from showing"
	if !contains(logs.matching("notify: "), kept) {
		t.Errorf("no %q in clack.log", kept)
	}
}

// The reply bar's Unread mark is there in the frame that first draws the
// reply: once the channel's thread activity is loaded, the reply's
// thread_v2 item, which the server sends right behind the reply's message
// events, goes into the list as core takes it (Slack's PY/r
// upsertChannelActivityItems), with no fetch. Clack fetched the
// channel's activity again on each change of its unread thread count, so
// the reply drew first and its mark came a round trip later.
func TestReplyUnreadMarkInTheReplysFrame(t *testing.T) {
	t.Setenv("CLACK_HOME", t.TempDir())
	sz := fakeslack.Small
	sz.Views = true // activity.feed answers from the feed, the live entries with it (fetchActivitiesForChannel)
	fs, err := fakeslack.Start(sz)
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
	c.SetExperimentGroup("unread_thread_indication_in_replybar", "on")
	e.a.focused = false
	ch := fs.ChannelIDs()[2]
	replier := fs.UserIDs()[3]
	root1, _ := fs.PostQuiet(ch, fakeslack.SelfID, "a synthetic question, the first")
	// the second root a second later: fakeslack numbers a thread's quiet
	// replies from its root's second, so two threads' first replies in one
	// second would share a ts
	sec, _ := strconv.ParseInt(strings.Split(root1, ".")[0], 10, 64)
	for time.Now().Unix() <= sec {
		time.Sleep(20 * time.Millisecond)
	}
	root2, _ := fs.PostQuiet(ch, fakeslack.SelfID, "a synthetic question, the second")
	e.openChannel(ch)
	e.until("both roots", 20*time.Second, func() bool {
		cv := e.channelView(ch)
		if cv == nil {
			return false
		}
		_, _, ok1 := cv.ReplyBarState(root1)
		_, _, ok2 := cv.ReplyBarState(root2)
		return ok1 && ok2
	})
	cv := e.channelView(ch)
	// the threads' state with nothing unread first: the first reply's moves
	// the channel's unread thread count from 0
	fs.Push(map[string]any{"type": "update_thread_state", "has_unreads": false, "mention_count": 0,
		"unread_count_by_channel": map[string]int{}, "mention_count_by_channel": map[string]int{}, "org_wide": c.IsEnterprise()})
	e.until("no unread threads", 10*time.Second, func() bool { return c.ThreadsUnreadIn(ch) == 0 && !c.ThreadsState().Unread })
	e.settle(100 * time.Millisecond)
	feeds := fs.CallCount("activity.feed")
	fs.PushFollowedReplyLive(ch, root1, replier, "a synthetic answer to the first", false)
	if !e.untilSoft(20*time.Second, func() bool { _, u, _ := cv.ReplyBarState(root1); return u }) {
		n, u, ok := cv.ReplyBarState(root1)
		t.Fatalf("setup: the first reply's Unread mark never came (its reply bar: %d avatars, unread %v, laid out %v; ThreadsUnreadIn %d; experiment %q; activity.feed calls %d, %d before the reply; the view is the one drawn: %v)",
			n, u, ok, c.ThreadsUnreadIn(ch), c.Experiment("unread_thread_indication_in_replybar"), fs.CallCount("activity.feed"), feeds, e.channelView(ch) == cv)
	}

	evs, stop := c.Subscribe()
	defer stop()
	fs.PushFollowedReplyLive(ch, root2, replier, "a synthetic answer to the second", false)
	took := time.After(20 * time.Second)
	for got := false; !got; {
		select {
		case ev := <-evs:
			got = ev.Type == "activity" && ev.Activity != nil && ev.Activity.Type == "thread_v2" && ev.Activity.ThreadTS == root2
		case <-took:
			t.Fatal("core never announced the second reply's thread_v2 item")
		}
	}
	for i := 1; ; i++ {
		e.frame()
		if n, unread, _ := cv.ReplyBarState(root2); n > 0 {
			if !unread {
				t.Errorf("the second reply drew (frame %d after its events) without its Unread mark", i)
			}
			break
		}
		if i > 400 {
			t.Fatal("the second reply never drew")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if noteWith(e, rec, "answer to the second", 10*time.Second) == nil {
		t.Error("no toast for the second reply")
	}
}
