package ui

// A thread pane never shows a reply as its root (Karim, α.105, Windows:
// in a bot's digest of the most recently active threads, each entry a
// quote of a thread's latest reply with a link to it, a click on the
// link opened a thread pane whose first and only message was the reply;
// the real parent wasn't there). Slack's openPermalink is jumpToMessage
// ({channelId, ts, threadTs}) (boot-shared 0S61, wc0P): with the link's
// thread_ts the parent's thread opens at the reply, highlighted; without
// one the message is looked up first (the store, then messages.list) and
// a reply still opens in its parent's thread. Clack's link click made
// the link's message the thread's root. The same holds for the other
// ways in: a slack://channel deep link naming a message or a thread, a
// thread pane opened with a reply's ts, a followed thread's toast.
//
// Against fakeslack's thread header fixtures (synthetic; nothing talks to
// Slack): the digest, posted in another conversation by a bot, quotes the
// public fixture thread's replies.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"log"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/huddleui"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/shortcut"
)

const digestBotID = "B0DIGEST001"

// digestLink is Slack's permalink to message ts of ch (getPermalink), with
// the thread's ts when thread is set (?thread_ts=…&cid=…).
func digestLink(ch, ts, thread string) string {
	u := "https://fakeclub.slack.com/archives/" + ch + "/p" + strings.ReplaceAll(ts, ".", "")
	if thread != "" {
		u += "?thread_ts=" + thread + "&cid=" + ch
	}
	return u
}

// postDigest posts the digest bot's message in ch: a rich_text heading
// and one quote per link, each ending in a "Source" link to the message.
func postDigest(fs *fakeslack.Server, ch string, links ...string) string {
	els := []any{map[string]any{"type": "rich_text_section", "elements": []any{
		map[string]any{"type": "text", "text": "Top 10 most recently active threads", "style": map[string]any{"bold": true}}}}}
	for i, l := range links {
		els = append(els, map[string]any{"type": "rich_text_quote", "elements": []any{
			map[string]any{"type": "text", "text": fmt.Sprintf("synthetic digest entry %d (", i+1)},
			map[string]any{"type": "link", "url": l, "text": "Source"},
			map[string]any{"type": "text", "text": ")"}}})
	}
	b, _ := json.Marshal([]any{map[string]any{"type": "rich_text", "block_id": "digest", "elements": els}})
	return fs.PostBotBlocksQuiet(ch, digestBotID, "Thread digest", "Top 10 most recently active threads", b)
}

// linkPoint waits for the link to url among conversation ch's visible
// messages and returns the middle of its first line box.
func (e *perfEnv) linkPoint(t *testing.T, ch, url string) image.Point {
	t.Helper()
	return e.linkPointIn(t, "", ch, url)
}

// openState describes the route and the thread pane, for failures.
func (e *perfEnv) openState() string {
	s := e.a.secondary
	return fmt.Sprintf("route %q, secondary kind %d %s/%s at %q", e.a.router.Current(), s.kind, s.channel, s.ts, s.at)
}

// replyDrawnAsRoot reports a frame's violation: a thread pane with reply
// as its root that drew any of its rows (the reply, alone).
func (e *perfEnv) replyDrawnAsRoot(ch, reply string) string {
	if tv := e.threadView(ch, reply); tv != nil {
		if rows := tv.Senders(); len(rows) > 0 {
			return fmt.Sprintf("the thread pane with the reply %s as its root drew %d row(s), the first %s", reply, len(rows), rows[0].TS)
		}
	}
	return ""
}

// waitParentThread runs frames until the thread pane draws thread root
// of ch whole, from the root, with the reply highlighted, failing (with
// what's open and drawn) when it doesn't, or when any frame on the way
// draws reply as a thread's root.
func (e *perfEnv) waitParentThread(t *testing.T, what, ch, root, reply string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		if bad := e.replyDrawnAsRoot(ch, reply); bad != "" {
			t.Fatalf("%s: %s (%s)", what, bad, e.openState())
		}
		rows := e.paneRows("", ch, root)
		if threadDrawn(rows, root) && lit(rows, reply) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: the thread %s/%s drew %s; %s; want it from its root, the reply %s highlighted",
				what, ch, root, drawnState(rows), e.openState(), reply)
		}
		e.frame()
		time.Sleep(time.Millisecond)
	}
	if r := e.a.router.Current(); r != Route("c/"+ch+"/t/"+root) {
		t.Errorf("%s: route %q, want the parent thread's c/%s/t/%s", what, r, ch, root)
	}
	if s := e.a.secondary; s.kind != secThread || s.channel != ch || s.ts != root || s.at != reply {
		t.Errorf("%s: %s; want the secondary pane at the reply (toThread's replyTs)", what, e.openState())
	}
}

// logLines records what the process logs while a test runs (the lines
// whose prefix is one of prefixes).
type logLines struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	lines []string
}

func (l *logLines) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

// captureLog records the log from now on; the lines still go where they
// went (a failure's output keeps them).
func captureLog(t *testing.T) *logLines {
	l := &logLines{}
	prev, flags := log.Writer(), log.Flags()
	log.SetOutput(io.MultiWriter(prev, l))
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(prev)
		log.SetFlags(flags)
	})
	return l
}

// all is every logged line.
func (l *logLines) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for _, line := range strings.Split(l.buf.String(), "\n") {
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// matching is the logged lines starting with prefix.
func (l *logLines) matching(prefix string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for _, line := range strings.Split(l.buf.String(), "\n") {
		if strings.HasPrefix(line, prefix) {
			out = append(out, line)
		}
	}
	return out
}

// TestReplyPermalinkOpensParentThread: the digest's link to a reply
// carries the thread (Slack's getPermalink adds ?thread_ts=…&cid=… to a
// reply's link). The click opens the parent's thread at the reply,
// highlighted, with the root highlighted in the conversation beside it.
// Clack opened the parent's thread at its top, nothing highlighted.
func TestReplyPermalinkOpensParentThread(t *testing.T) {
	e := threadHeaderEnv(t, false)
	pub, root, reply := fakeslack.ThreadPublicID, fakeslack.ThreadRootTS, fakeslack.ThreadReplyTS(1)
	digest := fakeslack.ThreadPrivateID
	link := digestLink(pub, reply, root)
	postDigest(e.fs, digest, link)
	e.openChannel(digest)
	e.settle(800 * time.Millisecond)
	e.clickAt(e.linkPoint(t, digest, link), time.Second)
	e.waitParentThread(t, "a click on the reply's link (thread_ts)", pub, root, reply)
	e.waitDrawn(t, "the root highlighted in the conversation beside the thread (highlightRoot)", 10*time.Second, "", pub, "",
		func(rows []messages.SenderView) bool { return lit(rows, root) })
}

// TestReplyPermalinkWithoutThreadOpensParentThread is Karim's link: a
// bot built it by hand, with no thread_ts. The reply isn't in the
// session yet (nothing opened its thread), so jumpToMessage asks
// messages.list for it, learns its thread_ts and opens the parent's
// thread at it; no frame draws the reply as a thread's root. clack.log
// says where the open came from, the ids as tags and no message text.
// Clack opened c/<ch>/t/<the reply's ts>: the reply alone, as the root.
func TestReplyPermalinkWithoutThreadOpensParentThread(t *testing.T) {
	e := threadHeaderEnv(t, false)
	pub, root, reply := fakeslack.ThreadPublicID, fakeslack.ThreadRootTS, fakeslack.ThreadReplyTS(0)
	digest := fakeslack.ThreadPrivateID
	link := digestLink(pub, reply, "")
	postDigest(e.fs, digest, link)
	e.openChannel(digest)
	e.settle(800 * time.Millisecond)
	if _, found := e.a.Core().S.Find(pub, reply); found {
		t.Fatalf("the reply is in the session before the click: the lookup this test is about wouldn't run")
	}
	logs := captureLog(t)
	calls := e.recordCalls("messages.list")
	e.clickAt(e.linkPoint(t, digest, link), time.Second)
	e.waitParentThread(t, "a click on the reply's link (no thread_ts)", pub, root, reply)
	// jumpToMessage:resolveThreadTs: the reply, once
	asked := calls.of("messages.list", "", "message_ids")
	var ids []struct {
		Channel    string   `json:"channel"`
		Timestamps []string `json:"timestamps"`
	}
	if len(asked) == 1 {
		_ = json.Unmarshal([]byte(strings.TrimPrefix(asked[0], "message_ids=")), &ids)
	}
	if len(ids) != 1 || ids[0].Channel != pub || !slices.Equal(ids[0].Timestamps, []string{reply}) {
		t.Errorf("messages.list asked %q, want one request for the reply %s of %s (jumpToMessage:resolveThreadTs)", asked, reply, pub)
	}
	e.waitDrawn(t, "the root highlighted in the conversation beside the thread (highlightRoot)", 10*time.Second, "", pub, "",
		func(rows []messages.SenderView) bool { return lit(rows, root) })

	// clack.log: the jump and the open, from the link, with tags only
	jumps, opens := logs.matching("ui: jumpToMessage: "), logs.matching("ui: thread opened: ")
	want := logTag(pub) + " " + logTag(reply) + " thread " + logTag(root)
	if len(jumps) == 0 || !strings.Contains(strings.Join(jumps, "\n"), want) {
		t.Errorf("clack.log has no jumpToMessage line for the reply in its thread (%q): %q", want, jumps)
	}
	if len(opens) != 1 || !strings.Contains(opens[0], logTag(pub)+" "+logTag(root)) ||
		!strings.Contains(opens[0], "permalink from ui/messages.(*richHandler).open ") { // the link's click (a mrkdwn link's OpenMrkdwnLink)
		t.Errorf("clack.log's thread opens: %q, want one, of the parent thread, from the message's link", opens)
	}
	noRawIDs(t, logs, pub, root, reply, digest)
	for _, line := range logs.all() {
		for _, text := range []string{"synthetic", "Docs are ready", "Ship it"} {
			if strings.Contains(line, text) {
				t.Errorf("clack.log line %q has message text %q in it", line, text)
			}
		}
	}
}

// TestThreadPaneNeverShowsReplyAsRoot: whatever hands a thread pane a
// reply's ts, the pane shows the reply's thread at the reply. A
// slack://channel link naming the reply as its thread_ts, with the
// reply not in the session, opens a pane that learns from
// conversations.replies (which answers a reply's ts with its thread)
// that its root is a reply, draws nothing of it and hands on to the
// parent's thread. A reply the session holds never gets a pane of its
// own (OpenThread). Clack drew the reply as the thread's root, alone.
func TestThreadPaneNeverShowsReplyAsRoot(t *testing.T) {
	pub, root := fakeslack.ThreadPublicID, fakeslack.ThreadRootTS
	t.Run("a deep link's thread_ts", func(t *testing.T) {
		e := threadHeaderEnv(t, false)
		reply := fakeslack.ThreadReplyTS(1)
		e.openChannel(fakeslack.ThreadPrivateID)
		e.settle(300 * time.Millisecond)
		e.a.OpenURL("slack://channel?team=" + fakeslack.TeamID + "&id=" + pub + "&thread_ts=" + reply)
		e.waitParentThread(t, "slack://channel?thread_ts=<a reply>", pub, root, reply)
		// the conversation highlights the thread's root, not the reply it
		// was asked to (a message it never holds)
		e.waitDrawn(t, "the root highlighted in the conversation beside the thread", 10*time.Second, "", pub, "",
			func(rows []messages.SenderView) bool { return lit(rows, root) })
		if cv := e.channelView(pub); cv != nil && cv.JumpPending() != "" {
			t.Errorf("the conversation still waits to jump to %s", cv.JumpPending())
		}
	})
	t.Run("a deep link's thread_ts, the reply in the session", func(t *testing.T) {
		e := threadHeaderEnv(t, false)
		reply := fakeslack.ThreadReplyTS(1)
		e.openChannel(pub)
		e.a.OpenThread(pub, root)
		e.waitDrawn(t, "the thread", 15*time.Second, "", pub, root, func(rows []messages.SenderView) bool { return threadDrawn(rows, root) })
		e.a.CloseSecondary()
		e.openChannel(fakeslack.ThreadPrivateID)
		e.settle(300 * time.Millisecond)
		logs := captureLog(t)
		e.a.OpenURL("slack://channel?team=" + fakeslack.TeamID + "&id=" + pub + "&thread_ts=" + reply)
		e.waitParentThread(t, "slack://channel?thread_ts=<a reply core holds>", pub, root, reply)
		e.waitDrawn(t, "the root highlighted in the conversation beside the thread", 10*time.Second, "", pub, "",
			func(rows []messages.SenderView) bool { return lit(rows, root) })
		if cv := e.channelView(pub); cv != nil && cv.JumpPending() != "" {
			t.Errorf("the conversation still waits to jump to %s", cv.JumpPending())
		}
		if c := e.a.convos[pub+"/"+reply]; c != nil {
			t.Errorf("a thread pane was made for the reply %s", reply)
		}
		if got := logs.matching("ui: jumpToMessage: its thread_ts " + logTag(reply) + " is a reply in " + logTag(root)); len(got) != 1 {
			t.Errorf("clack.log's guard lines %q, want one for the reply", logs.matching("ui: "))
		}
	})
	t.Run("a reply the session holds", func(t *testing.T) {
		e := threadHeaderEnv(t, false)
		reply := fakeslack.ThreadReplyTS(0)
		e.openChannel(pub)
		e.a.OpenThread(pub, root)
		e.until("the thread loaded", 15*time.Second, func() bool {
			tv := e.threadView(pub, root)
			return tv != nil && len(tv.Senders()) == 3
		})
		e.a.CloseSecondary()
		e.frame()
		e.a.OpenThread(pub, reply) // a caller with only the reply
		if s := e.a.secondary; s.kind != secThread || s.ts != root || s.at != reply {
			t.Errorf("OpenThread with a reply's ts: %s; want its thread at it", e.openState())
		}
		e.frame() // a pane mounts as it's laid out
		if c := e.a.convos[pub+"/"+reply]; c != nil {
			t.Errorf("OpenThread made a thread pane for the reply %s", reply)
		}
		e.waitParentThread(t, "OpenThread(<a reply>)", pub, root, reply)
	})
}

// TestDeepLinkToReplyOpensParentThread: slack://channel?id=…&message=…
// is handleParsedDeepLink's jumpToMessage({channelId, ts: message}), so a
// reply's message opens its parent's thread at it (messages.list says
// which). Clack opened the conversation and ignored the message.
func TestDeepLinkToReplyOpensParentThread(t *testing.T) {
	e := threadHeaderEnv(t, false)
	pub, root, reply := fakeslack.ThreadPublicID, fakeslack.ThreadRootTS, fakeslack.ThreadReplyTS(1)
	e.openChannel(fakeslack.ThreadPrivateID)
	e.settle(300 * time.Millisecond)
	e.a.OpenURL("slack://channel?team=" + fakeslack.TeamID + "&id=" + pub + "&message=" + reply)
	e.waitParentThread(t, "slack://channel?message=<a reply>", pub, root, reply)
}

// TestFollowedThreadToastOpensParentThread: the server's
// desktop_notification for a reply in a followed thread, as the
// Windows toast's launch arguments carry it back (messageId the reply,
// threadTimestamp the root), opens the parent's thread at the reply,
// highlighted, and clack.log says a notification's click did it.
func TestFollowedThreadToastOpensParentThread(t *testing.T) {
	e := threadHeaderEnv(t, false)
	a := e.a
	pub, root, reply := fakeslack.ThreadPublicID, fakeslack.ThreadRootTS, fakeslack.ThreadReplyTS(1)
	a.Navigate("c/" + fakeslack.ThreadPrivateID)
	e.settle(300 * time.Millisecond)
	storeTeam(a.Core()) // the toast's teamId
	rec := &noteRec{}
	a.notify = rec
	huddleui.PlayHook = func(string) {} // the message sound: no audio device
	t.Cleanup(func() { huddleui.PlayHook = nil })
	logs := captureLog(t)
	// the server's event for the reply, as the socket sends it (msg, no ts)
	raw, _ := json.Marshal(map[string]any{"type": "desktop_notification", "channel": pub, "msg": reply, "thread_ts": root,
		"content": "Robin Example: synthetic", "title": "Thread in #th-launch-notes"})
	a.data.desktopNotification(a, core.Event{Type: "desktop_notification", Channel: pub, Raw: raw})
	n := rec.wait(t, a, 1)
	if n.Title != "New message in #th-launch-notes" {
		t.Errorf("the toast's title %q, want Slack's \"New message in #th-launch-notes\"", n.Title)
	}
	click(t, a, n, "toast", 0) // the toast's launch arguments, parsed back
	e.waitParentThread(t, "a click on a followed thread's toast", pub, root, reply)
	clicks := logs.matching("notify: clicked a notification: ")
	if len(clicks) != 1 || !strings.Contains(clicks[0], logTag(pub)+" "+logTag(reply)+" thread "+logTag(root)) {
		t.Errorf("clack.log's notification clicks: %q, want the reply in its thread", clicks)
	}
	if opens := logs.matching("ui: thread opened: "); len(opens) != 1 || !strings.Contains(opens[0], "a notification's click") {
		t.Errorf("clack.log's thread opens: %q, want one, from the notification's click", opens)
	}
}

// TestReplyPermalinkSplitClickOpensInSplit: the split modifier click
// (⌘⌥, Ctrl+Alt) on a reply's link without thread_ts opens the parent's
// thread in the split view once messages.list answered; the lookup
// doesn't lose the click's container. Clack's split view showed the
// reply as the thread's root.
func TestReplyPermalinkSplitClickOpensInSplit(t *testing.T) {
	e := threadHeaderEnv(t, false)
	pub, root, reply := fakeslack.ThreadPublicID, fakeslack.ThreadRootTS, fakeslack.ThreadReplyTS(1)
	digest := fakeslack.ThreadPrivateID
	link := digestLink(pub, reply, "")
	postDigest(e.fs, digest, link)
	e.openChannel(digest)
	e.settle(800 * time.Millisecond)
	p := e.linkPoint(t, digest, link)
	pt := f32.Pt(float32(p.X), float32(p.Y))
	mods := shortcut.Mod() | key.ModAlt
	e.r.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pt, Time: time.Second},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pt, Modifiers: mods, Time: time.Second + time.Millisecond},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pt, Modifiers: mods, Time: time.Second + 2*time.Millisecond},
	)
	want := Route("c/" + pub + "/t/" + root)
	deadline := time.Now().Add(20 * time.Second)
	for e.a.splitTop() != want {
		if r := e.a.splitTop(); r == Route("c/"+pub+"/t/"+reply) {
			t.Fatalf("the split view shows the reply as a thread's root (%q)", r)
		}
		if rows := e.paneRows(paneSplit, pub, reply); len(rows) > 0 {
			t.Fatalf("the split view drew the reply as a thread's root: %s", drawnState(rows))
		}
		if time.Now().After(deadline) {
			t.Fatalf("the split click on the reply's link: split view %q, %s; want the parent thread %q", e.a.splitTop(), e.openState(), want)
		}
		e.frame()
		time.Sleep(time.Millisecond)
	}
	if s := e.a.secondary; s.kind == secThread {
		t.Errorf("the split click opened a thread in the secondary pane too: %s", e.openState())
	}
	e.waitDrawn(t, "the split view's thread at the reply", 10*time.Second, paneSplit, pub, root, func(rows []messages.SenderView) bool {
		return threadDrawn(rows, root) && lit(rows, reply)
	})
	if rows := e.paneRows(paneSplit, pub, reply); len(rows) > 0 {
		t.Errorf("the split view drew the reply as a thread's root: %s", drawnState(rows))
	}
}
