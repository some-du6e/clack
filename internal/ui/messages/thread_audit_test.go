package messages

// The thread pane against Slack's ThreadsFlexpane (docs/THREADS-AUDIT.md)
// on internal/fakeslack's thread audit fixtures (#th-audit; synthetic,
// nothing talks to Slack): the "New" marker where getThreadRowsAndKeys
// puts it (before the first unread reply that isn't the viewer's, or on
// the "N replies" separator when every reply is unread), the separator
// counting the root's reply_count, marking read 350 ms after the pane
// opens with the user's attention (and never without it), the marker
// staying as the ghost, Esc marking read, the start position
// (getThreadStartPosition), a reopen deciding the marker afresh, and
// the "N new replies" banner.

import (
	"context"
	"image"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// auditHost is a pane host whose user attention the test sets.
type auditHost struct {
	coreHost
	attention atomic.Bool
}

func (h *auditHost) UserAttention() bool { return h.attention.Load() }

func auditEnv(t *testing.T) (*core.App, *fakeslack.Server) {
	t.Helper()
	SetTaut(nil)
	sz := fakeslack.Small
	sz.ThreadsAudit = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	app := core.New(slack.New(fs.Creds()), store.New(""))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- app.Boot(ctx) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("boot: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("boot timed out")
	}
	return app, fs
}

// auditPane opens a fixture thread in a 480 x h pane and draws it until
// it has loaded.
func auditPane(t *testing.T, app *core.App, root string, attention bool, h int) (*ThreadView, *auditHost, func()) {
	t.Helper()
	host := &auditHost{coreHost: coreHost{app: app, th: theme.New(false)}}
	host.attention.Store(attention)
	tv := NewThreadView(host, fakeslack.ThreadsAuditChannel, root)
	var r input.Router
	ops := new(op.Ops)
	frame := func() {
		ops.Reset()
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(480, h)),
			Now: time.Now(), Source: r.Source()}
		tv.Layout(gtx)
		r.Frame(ops)
	}
	until(t, 15*time.Second, "the thread loads", frame, func() bool { return tv.s.snap.loaded && len(tv.s.items) > 2 })
	frame()
	return tv, host, frame
}

// auditRows names the pane's rows: root, sep (the "N replies"
// separator), new (the "New" divider), +unread / +ghost for a marker, and
// the replies r0, r1… by index.
func auditRows(tv *ThreadView, root string) string {
	idx := map[string]int{}
	for i := 0; i < 50; i++ {
		idx[fakeslack.ThreadsAuditReply(root, i)] = i
	}
	mark := map[uint8]string{thrMarkNone: "", thrMarkUnread: "+unread", thrMarkGhost: "+ghost"}
	var out []string
	for _, it := range tv.s.items {
		switch it.kind {
		case itReplies:
			out = append(out, "sep"+mark[it.mark])
		case itUnread:
			out = append(out, "new"+mark[it.mark])
		case itMsg:
			if it.key == root {
				out = append(out, "root")
			} else {
				out = append(out, "r"+strconv.Itoa(idx[it.key]))
			}
		}
	}
	return strings.Join(out, " ")
}

// labels runs one frame and returns every label the pane drew.
func auditLabels(frame func()) []string {
	var mu sync.Mutex
	var got []string
	labelHook = func(txt string, _ textStyle) { mu.Lock(); got = append(got, txt); mu.Unlock() }
	frame()
	labelHook = nil
	return got
}

func drewLabel(ls []string, want string) bool {
	for _, l := range ls {
		if l == want {
			return true
		}
	}
	return false
}

func TestThreadsAuditMarkers(t *testing.T) {
	app, fs := auditEnv(t)
	for _, c := range []struct{ name, root, want string }{
		{"read to the viewer's own reply", fakeslack.ThreadsAuditUnread, "root sep r0 r1 r2 new+unread r3 r4 r5"},
		{"every reply unread", fakeslack.ThreadsAuditAllUnread, "root sep+unread r0 r1"},
		{"the first unread reply is the viewer's", fakeslack.ThreadsAuditOwnFirst, "root sep r0 new+unread r1"},
		{"read", fakeslack.ThreadsAuditRead, "root sep r0 r1 r2"},
		{"not followed", fakeslack.ThreadsAuditHidden, "root sep r0 r1 r2"},
	} {
		t.Run(c.name, func(t *testing.T) {
			tv, _, frame := auditPane(t, app, c.root, false, 700)
			if got := auditRows(tv, c.root); got != c.want {
				t.Errorf("rows %q, want %q", got, c.want)
			}
			if strings.Contains(c.want, "+unread") && !drewLabel(auditLabels(frame), "New") {
				t.Error("the marker drew no \"New\"")
			}
		})
	}
	if m := fs.ThreadsAuditMarks(); len(m) != 0 {
		t.Errorf("without the user's attention the pane marked %v", m)
	}
}

// ThreadSeparatorRowGeneric counts the root's reply_count, not the
// replies loaded.
func TestThreadsAuditReplyCount(t *testing.T) {
	app, _ := auditEnv(t)
	_, _, frame := auditPane(t, app, fakeslack.ThreadsAuditHidden, false, 700)
	if ls := auditLabels(frame); !drewLabel(ls, "7 replies") {
		t.Errorf("the separator didn't say \"7 replies\" (three loaded, reply_count 7): %q", ls)
	}
}

// checkUnreads: 350 ms after the pane opens with the user's attention the
// thread is marked read to its newest reply, once; the marker stays as
// the ghost.
func TestThreadsAuditMarksRead(t *testing.T) {
	app, fs := auditEnv(t)
	root := fakeslack.ThreadsAuditUnread
	tv, host, frame := auditPane(t, app, root, false, 700)
	end := time.Now().Add(700 * time.Millisecond)
	for time.Now().Before(end) {
		frame()
		time.Sleep(20 * time.Millisecond)
	}
	if m := fs.ThreadsAuditMarks(); len(m) != 0 {
		t.Fatalf("marked without the user's attention: %v", m)
	}
	host.attention.Store(true)
	start := time.Now()
	until(t, 10*time.Second, "subscriptions.thread.mark", frame, func() bool { return len(fs.ThreadsAuditMarks()) > 0 })
	if d := time.Since(start); d < 300*time.Millisecond {
		t.Errorf("marked %v after the attention came back, Slack waits 350 ms", d)
	}
	want := fakeslack.ThreadMark{Channel: fakeslack.ThreadsAuditChannel, ThreadTS: root, TS: fakeslack.ThreadsAuditReply(root, 5)}
	if m := fs.ThreadsAuditMarks()[0]; m != want {
		t.Errorf("marked %+v, want %+v", m, want)
	}
	until(t, 10*time.Second, "the marker turns into the ghost", frame, func() bool { return strings.Contains(auditRows(tv, root), "new+ghost") })
	for i := 0; i < 20; i++ {
		frame()
		time.Sleep(20 * time.Millisecond)
	}
	if n := len(fs.ThreadsAuditMarks()); n != 1 {
		t.Errorf("marked %d times, want once", n)
	}
	// reopened (Slack mounts a new pane): read, so no marker at all
	tv.Reopen()
	until(t, 5*time.Second, "the reopened pane without a marker", frame, func() bool { return auditRows(tv, root) == "root sep r0 r1 r2 r3 r4 r5" })
}

// onEsc: with something to mark Esc marks the thread read and drops the
// marker; then it has nothing to mark (the pane's Escape closes it).
func TestThreadsAuditEsc(t *testing.T) {
	app, fs := auditEnv(t)
	root := fakeslack.ThreadsAuditAllUnread
	tv, _, frame := auditPane(t, app, root, false, 700)
	if !tv.Esc() {
		t.Fatal("Esc on a thread with unread replies didn't mark it")
	}
	until(t, 10*time.Second, "subscriptions.thread.mark", frame, func() bool { return len(fs.ThreadsAuditMarks()) == 1 })
	if m := fs.ThreadsAuditMarks()[0]; m.TS != fakeslack.ThreadsAuditReply(root, 1) {
		t.Errorf("Esc marked to %s, want the newest reply", m.TS)
	}
	until(t, 5*time.Second, "no marker after Esc", frame, func() bool { return auditRows(tv, root) == "root sep r0 r1" })
	until(t, 5*time.Second, "the read state back from core", frame, func() bool { return !tv.s.shouldMarkThreadRead() })
	if tv.Esc() {
		t.Error("a second Esc marked again")
	}
}

// getThreadStartPosition: a thread not followed opens at the root; a
// followed one at its first unread reply, 100px below the top, or at
// the reply it was read to.
func TestThreadsAuditStartPosition(t *testing.T) {
	app, _ := auditEnv(t)
	for _, c := range []struct {
		name, root, key string
		offset          bool
	}{
		{"not followed", fakeslack.ThreadsAuditLong, fakeslack.ThreadsAuditLong, false},
		{"the first unread reply", fakeslack.ThreadsAuditUnread, fakeslack.ThreadsAuditReply(fakeslack.ThreadsAuditUnread, 3), true},
		{"every reply unread", fakeslack.ThreadsAuditAllUnread, fakeslack.ThreadsAuditReply(fakeslack.ThreadsAuditAllUnread, 0), true},
		{"read", fakeslack.ThreadsAuditRead, fakeslack.ThreadsAuditReply(fakeslack.ThreadsAuditRead, 2), true},
	} {
		t.Run(c.name, func(t *testing.T) {
			tv, _, frame := auditPane(t, app, c.root, false, 260)
			if key, off := tv.s.threadStart(""); key != c.key || off != c.offset {
				t.Errorf("starts at %s (offset %v), want %s (offset %v)", key, off, c.key, c.offset)
			}
			frame()
			if c.root == fakeslack.ThreadsAuditLong {
				if p := tv.s.list.Position; p.First != 0 || p.Offset != 0 {
					t.Errorf("40 replies, not followed: the pane opened at %+v, Slack at the root", p)
				}
			}
		})
	}
}

// ThreadUnreadBannerGeneric: with the marker scrolled above the rows, "N
// new replies"; it goes once the thread is marked read.
func TestThreadsAuditBanner(t *testing.T) {
	app, fs := auditEnv(t)
	root := fakeslack.ThreadsAuditUnread
	tv, host, frame := auditPane(t, app, root, false, 120)
	tv.s.list.Position = layout.Position{} // the newest replies
	frame()
	frame()
	if ls := auditLabels(frame); !drewLabel(ls, "3 new replies") {
		t.Errorf("no \"3 new replies\" with the marker above the rows: %q (rows %d-%d)", ls, tv.s.visible[0], tv.s.visible[1])
	}
	host.attention.Store(true)
	until(t, 10*time.Second, "marked read", frame, func() bool { return len(fs.ThreadsAuditMarks()) > 0 })
	until(t, 5*time.Second, "the banner gone", frame, func() bool { return !drewLabel(auditLabels(frame), "3 new replies") })
}
