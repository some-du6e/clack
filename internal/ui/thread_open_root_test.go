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
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/richview"
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
	var r image.Rectangle
	e.until("the link to "+url, 15*time.Second, func() bool {
		for _, lr := range e.mcLinks(ch, "") {
			if lr.Kind == richview.HitLink && lr.URL == url {
				r = lr.R
				return true
			}
		}
		return false
	})
	return image.Pt(r.Min.X+r.Dx()/2, r.Min.Y+r.Dy()/2)
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

// waitParentThread runs frames until the thread pane shows thread root
// of ch at reply, failing (with what's open) when it doesn't, or when
// any frame on the way draws reply as a thread's root.
func (e *perfEnv) waitParentThread(t *testing.T, what, ch, root, reply string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		if bad := e.replyDrawnAsRoot(ch, reply); bad != "" {
			t.Fatalf("%s: %s (%s)", what, bad, e.openState())
		}
		s := e.a.secondary
		if s.kind == secThread && s.channel == ch && s.ts == root && s.at == reply {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: %s; want the parent thread %s/%s at the reply %s (highlighted)", what, e.openState(), ch, root, reply)
		}
		e.frame()
		time.Sleep(time.Millisecond)
	}
	if r := e.a.router.Current(); r != Route("c/"+ch+"/t/"+root) {
		t.Errorf("%s: route %q, want the parent thread's c/%s/t/%s", what, r, ch, root)
	}
	e.until(what+": the reply placed and highlighted", 10*time.Second, func() bool {
		if bad := e.replyDrawnAsRoot(ch, reply); bad != "" {
			t.Fatalf("%s: %s (%s)", what, bad, e.openState())
		}
		tv := e.threadView(ch, root)
		return tv != nil && tv.JumpPending() == "" && tv.Highlighted() == reply
	})
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

func captureLog(t *testing.T) *logLines {
	l := &logLines{}
	prev, flags := log.Writer(), log.Flags()
	log.SetOutput(l)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(prev)
		log.SetFlags(flags)
	})
	return l
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
	if cv := e.channelView(pub); cv == nil || cv.Highlighted() != root {
		t.Errorf("the conversation beside the thread doesn't highlight the root (highlightRoot): %s", e.openState())
	}
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
	asked := e.fs.CallCount("messages.list")
	e.clickAt(e.linkPoint(t, digest, link), time.Second)
	e.waitParentThread(t, "a click on the reply's link (no thread_ts)", pub, root, reply)
	if e.fs.CallCount("messages.list") == asked {
		t.Errorf("the reply's thread_ts wasn't looked up with messages.list (jumpToMessage:resolveThreadTs)")
	}
	if cv := e.channelView(pub); cv == nil || cv.Highlighted() != root {
		t.Errorf("the conversation beside the thread doesn't highlight the root (highlightRoot): %s", e.openState())
	}

	// clack.log: the jump and the open, from the link, with tags only
	jumps, opens := logs.matching("ui: jumpToMessage: "), logs.matching("ui: thread opened: ")
	want := logTag(pub) + " " + logTag(reply) + " thread " + logTag(root)
	if len(jumps) == 0 || !strings.Contains(strings.Join(jumps, "\n"), want) {
		t.Errorf("clack.log has no jumpToMessage line for the reply in its thread (%q): %q", want, jumps)
	}
	if len(opens) != 1 || !strings.Contains(opens[0], logTag(pub)+" "+logTag(root)) ||
		!strings.Contains(opens[0], "permalink from ui/messages.(*richHandler).OpenURL") {
		t.Errorf("clack.log's thread opens: %q, want one, of the parent thread, from the message's link", opens)
	}
	for _, line := range append(jumps, opens...) {
		for _, plain := range []string{pub, root, reply, "synthetic", "Docs are ready", "Ship it"} {
			if strings.Contains(line, plain) {
				t.Errorf("clack.log line %q has %q in it: ids go in as tags, text never", line, plain)
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
	logs := captureLog(t)
	n := a.messageNote(pub, reply, root) // desktopNotificationAfter's payload for {ts, thread_ts}
	n.Title, n.Body = "New message in #th-launch-notes", "Robin Example: synthetic"
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
