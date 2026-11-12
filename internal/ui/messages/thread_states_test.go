package messages

// The thread pane's states and the reply bar's drafts (docs/THREADS-AUDIT.md
// "Loading", "Load error", "Drafts and scheduled replies", "j"), against
// Slack's CDN build 4d0c78085232: ThreadsFlexpane and ThreadsError
// (client-boot-shared puxw), FlexpaneLoading (usav), ReplyBar's
// getLinkTextWithScheduledDrafts (v8ac) and ThreadsFlexpane's onKeyDown.
// Synthetic only: internal/fakeslack, nothing talks to Slack.

import (
	"context"
	"image"
	"net/url"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func TestReplyBarLinkLikeSlack(t *testing.T) {
	for _, c := range []struct {
		replies, sched int
		draft, open    bool
		text, icon     string
	}{
		{3, 0, false, false, "3 replies", ""},
		{1, 0, true, false, "1 reply & 1 draft", ""},
		{3, 0, true, true, "3 replies", ""},
		{3, 2, false, false, "3 replies & 2 scheduled", ""},
		{3, 2, false, true, "3 replies & 2 scheduled", ""},
		{3, 1, true, false, "3 replies, 1 scheduled, 1 draft", ""},
		{3, 1, true, true, "3 replies & 1 scheduled", ""},
		{0, 2, true, false, "2 scheduled & 1 draft", ""},
		{0, 2, true, true, "2 scheduled", ""},
		{0, 0, true, false, "1 draft", "pencil"},
		{0, 0, true, true, "", "pencil"},
		{0, 1, false, false, "1 scheduled", "scheduled-send"},
	} {
		text, icon := replyBarLink(c.replies, c.draft, c.sched, c.open)
		if text != c.text || icon != c.icon {
			t.Errorf("%d replies, draft %v, %d scheduled, open %v: %q (%q), Slack %q (%q)", c.replies, c.draft, c.sched, c.open, text, icon, c.text, c.icon)
		}
	}
	// ThreadReplies / ReplyBar: no bar without anything to show, none
	// for the open thread whose only content is the draft in its composer
	for _, c := range []struct {
		replies, sched int
		draft, open    bool
		want           bool
	}{
		{0, 0, false, false, false}, {0, 0, true, false, true}, {0, 0, true, true, false}, {0, 1, false, true, true}, {2, 0, false, true, true},
	} {
		if got := replyBarShown(c.replies, c.draft, c.sched, c.open); got != c.want {
			t.Errorf("shown(%d replies, draft %v, %d scheduled, open %v) = %v, want %v", c.replies, c.draft, c.sched, c.open, got, c.want)
		}
	}
}

// The channel's reply bars count the thread's draft and scheduled
// replies, and a thread with a draft but no replies gets a bar.
func TestReplyBarDraftsLikeSlack(t *testing.T) {
	app, _, ts := shotsApp(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := app.SaveDraft(ctx, core.DraftOpts{Channel: tShots, ThreadTS: ts["root"], Text: "half a reply"}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.SaveDraft(ctx, core.DraftOpts{Channel: tShots, ThreadTS: ts["b1"], Text: "a first reply"}); err != nil {
		t.Fatal(err)
	}
	host := coreHost{app: app, th: theme.New(false)}
	v := NewChannelView(host, tShots)
	v.Preview = true
	f := frameFn(v)
	link := func(key string) string {
		if r := v.s.rows[ts[key]]; r != nil {
			return r.replyLink
		}
		return ""
	}
	until(t, 5*time.Second, "the reply bars", f, func() bool { return link("root") != "" && link("b1") != "" })
	if got := link("root"); got != "3 replies & 1 draft" {
		t.Errorf("root's reply bar %q, want \"3 replies & 1 draft\"", got)
	}
	if got := link("b1"); got != "1 draft" {
		t.Errorf("b1 (no replies, a draft): %q, want \"1 draft\" after the pencil", got)
	}
	// its description: getThreadLatestDraftText of the draft just saved
	// (last_updated_ts is in milliseconds)
	if r := v.s.rows[ts["b1"]]; r == nil {
		t.Error("no b1 row")
	} else if d := r.replyBarNoReplies(time.Now(), false); d != "Last edited today at "+clock(draftUpdated(r.draftAt)) && d != "Last edited 1 minute ago" {
		t.Errorf("b1's description %q (draft updated %q)", d, r.draftAt)
	}
	// a scheduled reply joins the count
	at := time.Now().Add(48 * time.Hour).Unix()
	if _, err := app.SaveDraft(ctx, core.DraftOpts{Channel: tShots, ThreadTS: ts["root"], Text: "later", DateScheduled: at}); err != nil {
		t.Fatal(err)
	}
	until(t, 5*time.Second, "the scheduled reply", f, func() bool { return link("root") == "3 replies, 1 scheduled, 1 draft" })
}

// thread fetches held or failed by the fake's hook
type repliesGate struct {
	mu      sync.Mutex
	hold    chan struct{}
	failure string // the error conversations.replies answers with ("" passes)
	calls   atomic.Int32
}

func (g *repliesGate) hook(method string, f url.Values) (any, bool) {
	if method != "conversations.replies" {
		return nil, false
	}
	g.calls.Add(1)
	g.mu.Lock()
	hold := g.hold
	g.mu.Unlock()
	if hold != nil {
		<-hold
	}
	g.mu.Lock()
	failure := g.failure // what the test set while the call was held
	g.mu.Unlock()
	if failure != "" {
		return map[string]any{"ok": false, "error": failure}, true
	}
	// the root alone (the store has the message; the fake doesn't know
	// the shots channel)
	return map[string]any{"ok": true, "has_more": false, "messages": []any{map[string]any{"type": "message", "ts": f.Get("ts"),
		"user": "UBEN00001", "text": "root", "thread_ts": f.Get("ts")}}}, true
}

func paneLabels(f func()) []string {
	var mu sync.Mutex
	var got []string
	labelHook = func(txt string, _ textStyle) { mu.Lock(); got = append(got, txt); mu.Unlock() }
	f()
	labelHook = nil
	return got
}

func TestThreadPaneLoadingAndErrorLikeSlack(t *testing.T) {
	app, fs, ts := shotsApp(t)
	g := &repliesGate{hold: make(chan struct{})}
	fs.SetHook(g.hook)
	host := coreHost{app: app, th: theme.New(false)}

	// isLoading: FlexpaneLoading's "Loading thread…", no skeleton rows
	tv := NewThreadView(host, tShots, ts["b1"])
	f := frameFn(tv)
	until(t, 5*time.Second, "the fetch", f, func() bool { return g.calls.Load() > 0 })
	ls := paneLabels(f)
	if !drewLabel(ls, "Loading thread…") || tv.HidesComposer() {
		t.Errorf("loading: labels %q, composer hidden %v; want \"Loading thread…\" over the pane, the composer kept", ls, tv.HidesComposer())
	}

	// a failed fetchReplies: ThreadsError, and no composer
	g.mu.Lock()
	g.failure = "internal_error"
	close(g.hold)
	g.hold = nil
	g.mu.Unlock()
	until(t, 10*time.Second, "the error", f, func() bool { return tv.HidesComposer() })
	ls = paneLabels(f)
	for _, want := range []string{"Couldn’t load thread", "If the problem persists, please ", "drop us a line."} {
		if !drewLabel(ls, want) {
			t.Errorf("ThreadsError lacks %q (drew %q)", want, ls)
		}
	}
	// Try Again fetches again
	g.mu.Lock()
	g.failure = ""
	g.mu.Unlock()
	n := g.calls.Load()
	tv.s.retryBtn.Click()
	until(t, 10*time.Second, "the retry", f, func() bool { return g.calls.Load() > n && !tv.HidesComposer() && tv.s.snap.loaded })

	// thread_not_found: "This message was deleted.", nothing else
	g.mu.Lock()
	g.failure = "thread_not_found"
	g.mu.Unlock()
	tv2 := NewThreadView(host, tShots, ts["b2"])
	f2 := frameFn(tv2)
	until(t, 10*time.Second, "the deleted thread", f2, func() bool { return tv2.HidesComposer() })
	ls = paneLabels(f2)
	if !drewLabel(ls, "This message was deleted.") || drewLabel(ls, "drop us a line.") || drewLabel(ls, "Couldn’t load thread") {
		t.Errorf("thread_not_found drew %q", ls)
	}
}

// Replies still loading after the root is known: getThreadRowsAndKeys'
// "loading" row, "Loading replies…" in 96px.
func TestThreadLoadingRepliesRow(t *testing.T) {
	app, _, ts := shotsApp(t)
	host := coreHost{app: app, th: theme.New(false)}
	tv := NewThreadView(host, tShots, ts["c1"])
	s := tv.s
	s.snap = snap{msgs: []entry{{rev: 1, m: core.Message{TS: ts["c1"], Channel: tShots, User: "UCARA0001", Text: "x"}}}}
	s.build(host, time.Now())
	if n := len(s.items); n == 0 || s.items[n-1].kind != itLoading {
		t.Fatalf("items %+v: want the loading row last", s.items)
	}
	ops := new(op.Ops)
	gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(480, 400)), Now: time.Now()}
	var got []string
	labelHook = func(txt string, st textStyle) {
		if st.italic {
			got = append(got, txt)
		}
	}
	d := s.loadingRepliesRow(gtx, host, colorsFor(host.th), 480)
	labelHook = nil
	ok := len(got) > 0
	for _, l := range got {
		ok = ok && l == "Loading replies\u2026" // measured, then drawn
	}
	if d.Size.Y != 96 || !ok {
		t.Errorf("row %v, italic labels %q", d.Size, got)
	}
	s.snap.loaded = true
	s.build(host, time.Now())
	for _, it := range s.items {
		if it.kind == itLoading {
			t.Error("a loaded thread keeps its loading row")
		}
	}
}

// Mod+J (ThreadsFlexpane onKeyDown): firstUnreadTs's row to the top, or
// "No unread messages" for a thread the pane opened unfollowed.
func TestThreadJumpToUnreadLikeSlack(t *testing.T) {
	app, _ := auditEnv(t)
	tv, _, _ := auditPane(t, app, fakeslack.ThreadsAuditUnread, false, 300)
	want := fakeslack.ThreadsAuditReply(fakeslack.ThreadsAuditUnread, 3) // read to r2
	if got := tv.s.firstUnreadTs(); got != want {
		t.Fatalf("firstUnreadTs %q, want r3 %q", got, want)
	}
	if !tv.JumpToUnread() {
		t.Fatal("nothing to jump to")
	}
	idx := -1
	for i, it := range tv.s.items {
		if it.kind == itMsg && it.key == want {
			idx = i
		}
	}
	if p := tv.s.list.Position; p.First != idx || p.Offset != 0 || !p.BeforeEnd {
		t.Errorf("position %+v, want r3 (item %d) at the top", p, idx)
	}
	if len(tv.announced) != 0 {
		t.Errorf("announced %q", tv.announced)
	}

	// read to the end: originalLastRead itself
	tr, _, _ := auditPane(t, app, fakeslack.ThreadsAuditRead, false, 300)
	if tr.s.firstUnreadTs() != tr.s.thr.open || tr.s.thr.open == "" {
		t.Errorf("a read thread: firstUnreadTs %q, want its lastRead %q", tr.s.firstUnreadTs(), tr.s.thr.open)
	}

	tl, _, _ := auditPane(t, app, fakeslack.ThreadsAuditLong, false, 300)
	if tl.JumpToUnread() || !reflect.DeepEqual(tl.announced, []string{"No unread messages"}) {
		t.Errorf("an unfollowed thread: announced %q, want \"No unread messages\"", tl.announced)
	}
}
