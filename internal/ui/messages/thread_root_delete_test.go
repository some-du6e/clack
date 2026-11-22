package messages

// The thread header after its root is deleted (thread-header-hover-2,
// review of round 1): the header reads the root from the store's copy
// (rootMsg) wherever the pane's slice is, so the copy has to go when the
// root does, as Slack's setMessageDeleted drops it: the name links
// nothing and the ⋮ menu reads no root. Two ways a root goes: a root with
// no replies deleted, and a tombstoned root whose last reply is deleted
// (Slack then sends message_deleted for the tombstone; core's
// TestRealtimeTombstone). Synthetic: fakeslack's threads audit fixtures.

import (
	"image"
	"net/url"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// rdPane is the thread pane of root in the audit channel, laid out each
// frame through a router.
func rdPane(t *testing.T, host *auditHost, root string) (*ThreadView, func()) {
	t.Helper()
	tv := NewThreadView(host, fakeslack.ThreadsAuditChannel, root)
	var r input.Router
	ops := new(op.Ops)
	frame := func() {
		ops.Reset()
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(480, 700)),
			Now: time.Now(), Source: r.Source()}
		tv.Layout(gtx)
		r.Frame(ops)
	}
	return tv, frame
}

// rdLinked waits for the header to name the channel as a link.
func rdLinked(t *testing.T, tv *ThreadView, frame func()) {
	t.Helper()
	until(t, 15*time.Second, "the header's channel name as a link", frame, func() bool {
		sub, _, ok := tv.HeaderName()
		_, rok := tv.root()
		return ok && sub.Link && rok
	})
}

// rdUnlinked waits for the header to drop the deleted root.
func rdUnlinked(t *testing.T, what string, tv *ThreadView, frame func()) {
	t.Helper()
	dl := time.Now().Add(10 * time.Second)
	for time.Now().Before(dl) {
		frame()
		sub, _, ok := tv.HeaderName()
		if _, rok := tv.root(); !rok && (!ok || !sub.Link) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	sub, _, ok := tv.HeaderName()
	r, rok := tv.root()
	t.Errorf("%s: the header still reads the deleted root (%s, root held %v, name drawn %v, a link %v); Slack's setMessageDeleted drops it", what, r.TS, rok, ok, sub.Link)
}

func rdDeleted(ch, ts, thread string) map[string]any {
	prev := map[string]any{"type": "message", "ts": ts, "user": "U0FAKE00002"}
	if thread != "" {
		prev["thread_ts"] = thread
	}
	return map[string]any{"type": "message", "subtype": "message_deleted", "hidden": true, "channel": ch, "deleted_ts": ts,
		"event_ts": ts, "previous_message": prev}
}

// A root with no replies, deleted while its pane is open.
func TestThreadHeaderRootDeleted(t *testing.T) {
	app, fs := auditEnv(t)
	liveCore(t, app, fs)
	ch := fakeslack.ThreadsAuditChannel
	root, ev := fs.PostQuiet(ch, fs.UserIDs()[1], "a synthetic message nobody replies to")
	fs.Push(ev)
	rootMsg := slack.Message{Type: "message", TS: root, User: fs.UserIDs()[1], Text: "a synthetic message nobody replies to"}
	fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method == "conversations.replies" && f.Get("channel") == ch && f.Get("ts") == root {
			return map[string]any{"ok": true, "messages": []slack.Message{rootMsg}, "has_more": false}, true
		}
		return nil, false
	})
	t.Cleanup(func() { fs.SetHook(nil) })
	host := &auditHost{coreHost: coreHost{app: app, th: theme.New(false)}}
	tv, frame := rdPane(t, host, root)
	rdLinked(t, tv, frame)
	fs.Push(rdDeleted(ch, root, ""))
	rdUnlinked(t, "a root with no replies deleted", tv, frame)
}

// A tombstoned root whose last reply is deleted: Slack sends the reply's
// message_deleted, then the tombstone's.
func TestThreadHeaderTombstoneRootDeleted(t *testing.T) {
	app, fs := auditEnv(t)
	liveCore(t, app, fs)
	ch, root := fakeslack.ThreadsAuditChannel, fakeslack.ThreadsAuditOne
	reply := fakeslack.ThreadsAuditReply(root, 0)
	host := &auditHost{coreHost: coreHost{app: app, th: theme.New(false)}}
	tv, frame := rdPane(t, host, root)
	rdLinked(t, tv, frame)
	fs.Push(map[string]any{"type": "message", "subtype": "message_changed", "hidden": true, "channel": ch, "event_ts": root,
		"message": map[string]any{"type": "message", "subtype": "tombstone", "hidden": true, "ts": root, "thread_ts": root,
			"user": "USLACKBOT", "text": "This message was deleted.", "reply_count": 1, "latest_reply": reply},
		"previous_message": map[string]any{"type": "message", "ts": root, "thread_ts": root, "user": "U0FAKE00002"}})
	until(t, 10*time.Second, "the tombstone in the pane", frame, func() bool {
		r, ok := tv.s.threadRoot()
		return ok && r.Tombstone
	})
	fs.Push(rdDeleted(ch, reply, root))
	fs.Push(map[string]any{"type": "message", "subtype": "message_deleted", "hidden": true, "channel": ch, "deleted_ts": root, "event_ts": root,
		"previous_message": map[string]any{"type": "message", "subtype": "tombstone", "ts": root, "thread_ts": root, "user": "USLACKBOT"}})
	rdUnlinked(t, "a tombstoned root whose last reply went", tv, frame)
}
