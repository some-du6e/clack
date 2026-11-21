package core

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// Reads that land after the socket's writes, and the order the revisions
// they bring go out in (store/read_revisions.go, read_changes.go). These
// pin the merge with msg-latency too: its SetLatestSince and RefreshRange
// carry the same read hooks, over what they keep (store's
// TestRefreshRangeRead), and announce after (TestReadsAnnounce).

func pageJSON(msgs ...map[string]any) string {
	b, _ := json.Marshal(map[string]any{"ok": true, "has_more": false, "messages": msgs})
	return string(b)
}

// updatesOf drains evs until they go quiet and returns the texts the
// updates of ts carried, in order.
func updatesOf(evs <-chan Event, ts string) []string {
	var out []string
	for {
		select {
		case ev := <-evs:
			if ev.Type == "update" && ev.TS == ts && ev.Msg != nil {
				out = append(out, ev.Msg.Text)
			}
		case <-time.After(150 * time.Millisecond):
			return out
		}
	}
}

func liveVote(ts string, n int) map[string]any {
	return map[string]any{"type": "message", "ts": ts, "user": "U1", "text": fmt.Sprintf("votes: %d", n)}
}

// A window read that went out before the vote's chat.update keeps the
// socket's revision. The page, read before the change, brings the older
// one: it reaches no copy (an Activity row's, fetched by id, included)
// and goes out as no change, as PutFetched and SetThreadSince keep what
// the socket changed while they were out.
func TestWindowReadKeepsSocketRevision(t *testing.T) {
	a, f := testApp(t)
	const ch, ts = "C1", "1762791000.000100"
	a.S.PutFetched(ch, []slack.Message{{Type: "message", TS: ts, User: "U1", Text: "votes: 1"}}, a.S.LiveSeq()) // Activity's copy
	f.reply["conversations.history"] = pageJSON(liveVote(ts, 1))
	f.hooks["conversations.history"] = func(url.Values) {
		a.handle(rtm("message", map[string]any{"subtype": "message_changed", "hidden": true, "channel": ch, "ts": "1762791001.000100",
			"message": liveVote(ts, 2), "previous_message": liveVote(ts, 1)}))
	}
	evs, stop := a.Subscribe()
	defer stop()
	if _, err := a.History(context.Background(), ch, ""); err != nil {
		t.Fatal(err)
	}
	if m, ok := a.S.FindHeld(ch, ts); !ok || m.Text != "votes: 2" {
		t.Errorf("the store's copy after the read: %q (%v), want the socket's votes: 2", m.Text, ok)
	}
	if w := a.S.Timeline(ch); w == nil || len(w.Msgs) != 1 || w.Msgs[0].Text != "votes: 2" {
		t.Errorf("the window after the read: %+v", w)
	}
	if c, ok := a.MessageChange(ch, ts); !ok || c.Msg == nil || c.Msg.Text != "votes: 2" {
		t.Errorf("the change table after the read: %+v (%v)", c, ok)
	}
	for _, s := range updatesOf(evs, ts) {
		if s != "votes: 2" {
			t.Errorf("an update carried %q after the socket's votes: 2", s)
		}
	}
}

// A window read bringing a new revision of a message the store held only
// as a copy fetched by id (an Activity row's, read before a socket gap)
// gives it to that copy and announces it, through History and Refresh:
// every view keeping copies takes it.
func TestWindowReadAnnouncesRevision(t *testing.T) {
	a, f := testApp(t)
	const ch, ts = "C1", "1762791100.000100"
	a.S.PutFetched(ch, []slack.Message{{Type: "message", TS: ts, User: "U1", Text: "votes: 1"}}, a.S.LiveSeq())
	for i, read := range []func() error{
		func() error { _, err := a.History(context.Background(), ch, ""); return err },
		func() error { _, err := a.Refresh(context.Background(), ch); return err },
	} {
		want := fmt.Sprintf("votes: %d", i+2)
		f.mu.Lock()
		f.reply["conversations.history"] = pageJSON(liveVote(ts, i+2))
		f.mu.Unlock()
		start := a.MessageChangeSeq()
		if err := read(); err != nil {
			t.Fatal(err)
		}
		if m, ok := a.S.FindHeld(ch, ts); !ok || m.Text != want {
			t.Errorf("read %d: the store's copy %q (%v), want %q", i, m.Text, ok, want)
		}
		if c, ok := a.MessageChange(ch, ts); !ok || c.Seq <= start || c.Msg == nil || c.Msg.Text != want {
			t.Errorf("read %d: the change table has %+v (%v), want %q announced", i, c, ok, want)
		}
	}
}

// A thread read that went out before a socket write keeps the write. The
// replies answer's root, read before message_replied counted a new reply,
// before message_locked turned replies off, or before the viewer took a
// link preview off, mustn't put the older root back in any copy, nor go
// out as a change after the write's (store.MutateNoted notes each write
// under the lock it makes it with).
func TestThreadReadKeepsSocketWrites(t *testing.T) {
	const ch, root, reply = "C1", "1762791200.000100", "1762791210.000100"
	rootAt := func(n int) map[string]any {
		return map[string]any{"type": "message", "ts": root, "thread_ts": root, "user": "U1", "text": "a synthetic root",
			"reply_count": n, "reply_users": []string{"U2"}, "reply_users_count": 1, "latest_reply": reply,
			"attachments": []any{map[string]any{"id": 1, "from_url": "https://example.invalid/a", "title": "a synthetic preview"}}}
	}
	replyMsg := map[string]any{"type": "message", "ts": reply, "thread_ts": root, "user": "U2", "text": "a synthetic reply"}
	for _, tc := range []struct {
		name  string
		write func(a *App)
		kept  func(m slack.Message) string // "" when m keeps the write
	}{
		{"message_replied", func(a *App) {
			a.handle(rtm("message", repliedEvent(ch, slack.Message{TS: root, User: "U1", Text: "a synthetic root"}, 2, []string{"U2"}, "1762791220.000100")))
		}, func(m slack.Message) string {
			if m.ReplyCount != 2 {
				return fmt.Sprintf("%d replies, the socket said 2", m.ReplyCount)
			}
			return ""
		}},
		{"message_locked", func(a *App) {
			a.handle(rtm("message", map[string]any{"subtype": "message_locked", "hidden": true, "channel": ch, "thread_ts": root, "is_locked": true}))
		}, func(m slack.Message) string {
			if !m.IsLocked {
				return "replies on, the socket turned them off"
			}
			return ""
		}},
		{"DeleteAttachment", func(a *App) {
			_ = a.DeleteAttachment(context.Background(), ch, root, "1")
		}, func(m slack.Message) string {
			if len(m.Attachments) != 0 {
				return fmt.Sprintf("%d link previews, the viewer took it off", len(m.Attachments))
			}
			return ""
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, f := testApp(t)
			var w slack.Message
			b, _ := json.Marshal(rootAt(1))
			if err := json.Unmarshal(b, &w); err != nil {
				t.Fatal(err)
			}
			a.S.SetLatest(ch, []slack.Message{w}, false) // the open channel's root; its thread never read
			f.reply["conversations.replies"] = pageJSON(rootAt(1), replyMsg)
			f.reply["chat.deleteAttachment"] = `{"ok":true}`
			var wrote uint64
			f.hooks["conversations.replies"] = func(url.Values) {
				tc.write(a)
				wrote = a.MessageChangeSeq()
			}
			if _, err := a.Replies(context.Background(), ch, root); err != nil {
				t.Fatal(err)
			}
			if m, ok := a.S.FindHeld(ch, root); !ok {
				t.Fatal("the store holds no root")
			} else if s := tc.kept(m); s != "" {
				t.Errorf("the root after the read: %s", s)
			}
			th, _ := a.S.Thread(ch, root)
			if len(th) == 0 || th[0].TS != root {
				t.Fatalf("the thread after the read: %+v", th)
			}
			if s := tc.kept(th[0]); s != "" {
				t.Errorf("the thread's root after the read: %s", s)
			}
			if c, ok := a.MessageChange(ch, root); ok && c.Seq > wrote {
				t.Errorf("the read announced the root (change %d) after the write (%d)", c.Seq, wrote)
			}
		})
	}
}

// A read's revision goes out as the store holds the message when it goes
// out. A change the socket made after the read (an edit, a reaction, a
// deletion), whose own update went out first, is never undone by it: the
// change table and every view keep the socket's.
func TestReadRevisionOrderedWithSocket(t *testing.T) {
	const ch, ts = "C1", "1762791300.000100"
	msg := func(text string) slack.Message { return slack.Message{Type: "message", TS: ts, User: "U1", Text: text} }
	read := func(t *testing.T) *App {
		a, _ := testApp(t)
		a.S.PutFetched(ch, []slack.Message{msg("votes: 1")}, a.S.LiveSeq())
		a.S.PutFetched(ch, []slack.Message{msg("votes: 2")}, a.S.LiveSeq()) // a read brings votes: 2, not announced yet
		return a
	}
	t.Run("edit", func(t *testing.T) {
		a := read(t)
		a.handle(rtm("message", map[string]any{"subtype": "message_changed", "hidden": true, "channel": ch, "ts": "1762791301.000100",
			"message": liveVote(ts, 3), "previous_message": liveVote(ts, 2)}))
		a.announceReads()
		if c, _ := a.MessageChange(ch, ts); c.Msg == nil || c.Msg.Text != "votes: 3" {
			t.Errorf("after the edit and the read's announcement: %+v, want the edit's votes: 3", c.Msg)
		}
	})
	t.Run("reaction", func(t *testing.T) {
		a := read(t)
		a.handle(rtm("reaction_added", map[string]any{"user": "U2", "reaction": "tada", "item": map[string]any{"type": "message", "channel": ch, "ts": ts}}))
		a.announceReads()
		c, _ := a.MessageChange(ch, ts)
		if c.Msg == nil || c.Msg.Text != "votes: 2" || len(c.Msg.Reactions) != 1 {
			t.Errorf("after the reaction and the read's announcement: %+v, want votes: 2 with the tada", c.Msg)
		}
	})
	t.Run("deletion", func(t *testing.T) {
		a := read(t)
		a.handle(rtm("message", map[string]any{"subtype": "message_deleted", "hidden": true, "channel": ch, "deleted_ts": ts}))
		a.announceReads()
		if c, ok := a.MessageChange(ch, ts); !ok || !c.Deleted {
			t.Errorf("after the deletion and the read's announcement: %+v (%v), want it deleted", c, ok)
		}
	})
}

// An older page (scrolling up) is a read like the newest one: a reaction
// the socket brought while it was out stays, a message the socket
// changed meanwhile keeps the socket's copy, and a revision the store
// held another of (an Activity row's copy) reaches every copy and goes
// out as a change.
func TestOlderPageRead(t *testing.T) {
	a, f := testApp(t)
	const ch, newest, old, edited = "C1", "1762791490.000100", "1762791400.000100", "1762791410.000100"
	a.S.SetLatest(ch, []slack.Message{{Type: "message", TS: newest, User: "U1", Text: "the newest synthetic message"}}, true)
	a.S.PutFetched(ch, []slack.Message{
		{Type: "message", TS: old, User: "U1", Text: "votes: 1", Reactions: []slack.Reaction{{Name: "+1", Count: 1, Users: []string{"U1"}}}},
		{Type: "message", TS: edited, User: "U2", Text: "a synthetic draft"},
	}, a.S.LiveSeq())
	b, _ := json.Marshal(map[string]any{"ok": true, "has_more": false, "messages": []any{
		map[string]any{"type": "message", "ts": old, "user": "U1", "text": "votes: 2", "reactions": []any{map[string]any{"name": "+1", "count": 1, "users": []string{"U1"}}}},
		map[string]any{"type": "message", "ts": edited, "user": "U2", "text": "a synthetic draft"},
	}})
	f.reply["conversations.history"] = string(b)
	f.hooks["conversations.history"] = func(url.Values) {
		a.handle(rtm("reaction_added", map[string]any{"user": "U2", "reaction": "+1", "item": map[string]any{"type": "message", "channel": ch, "ts": old}}))
		a.handle(rtm("message", map[string]any{"subtype": "message_changed", "hidden": true, "channel": ch, "ts": "1762791411.000100",
			"message":          map[string]any{"type": "message", "ts": edited, "user": "U2", "text": "a synthetic plan, final"},
			"previous_message": map[string]any{"type": "message", "ts": edited, "user": "U2", "text": "a synthetic draft"}}))
	}
	start := a.MessageChangeSeq()
	p, err := a.History(context.Background(), ch, newest)
	if err != nil {
		t.Fatal(err)
	}
	// the list gets what the read kept, not the raw page
	got := map[string]Message{}
	for _, m := range p.Messages {
		got[m.TS] = m
	}
	if m := got[old]; m.Text != "votes: 2" || len(m.Reactions) != 1 || m.Reactions[0].Count != 2 {
		t.Errorf("the older page History hands the list: the vote %q %+v, want votes: 2 with both +1s", m.Text, m.Reactions)
	}
	if m := got[edited]; m.Text != "a synthetic plan, final" {
		t.Errorf("the older page History hands the list: the edited message %q, want the socket's", m.Text)
	}
	w := a.S.Timeline(ch)
	if w == nil {
		t.Fatal("no window")
	}
	byTS := map[string]slack.Message{}
	for _, m := range w.Msgs {
		byTS[m.TS] = m
	}
	if m := byTS[old]; m.Text != "votes: 2" || len(m.Reactions) != 1 || m.Reactions[0].Count != 2 {
		t.Errorf("the older page's vote in the window: %q %+v, want votes: 2 with both +1s", m.Text, m.Reactions)
	}
	if m := byTS[edited]; m.Text != "a synthetic plan, final" {
		t.Errorf("the message edited while the page was out: %q in the window, want the socket's", m.Text)
	}
	if m, ok := a.S.FindHeld(ch, old); !ok || m.Text != "votes: 2" {
		t.Errorf("the store's copy of the vote: %q", m.Text)
	}
	if c, ok := a.MessageChange(ch, old); !ok || c.Seq <= start || c.Msg == nil || c.Msg.Text != "votes: 2" {
		t.Errorf("the vote's revision wasn't announced: %+v (%v)", c, ok)
	}
	if c, _ := a.MessageChange(ch, edited); c.Msg == nil || c.Msg.Text != "a synthetic plan, final" {
		t.Errorf("the edit's latest change: %+v", c.Msg)
	}
}

// A thread's unread reply deleted while subscriptions.thread.getView was
// out leaves the card, and its unread count with it: the New line sits
// on the first unread reply still there, not on a read one.
func TestThreadsUnreadLeavesDeleted(t *testing.T) {
	a, f := testApp(t)
	const ch, root, r1, r2 = "C1", "1762791600.000100", "1762791610.000100", "1762791620.000100"
	reply := func(ts, text string) map[string]any {
		return map[string]any{"type": "message", "ts": ts, "thread_ts": root, "user": "U2", "text": text}
	}
	b, _ := json.Marshal(map[string]any{"ok": true, "has_more": false, "total_unread_replies": 1,
		"threads_state": map[string]any{"has_unreads": true, "mention_count": 0},
		"threads": []any{map[string]any{
			"root_msg":       map[string]any{"type": "message", "ts": root, "thread_ts": root, "user": "U1", "text": "a synthetic root", "channel": ch, "reply_count": 2},
			"latest_replies": []any{reply(r1, "a read reply"), reply(r2, "an unread reply")},
			"unread_replies": []any{reply(r2, "an unread reply")},
		}}})
	f.reply["subscriptions.thread.getView"] = string(b)
	f.hooks["subscriptions.thread.getView"] = func(url.Values) {
		a.handle(rtm("message", map[string]any{"subtype": "message_deleted", "hidden": true, "channel": ch, "deleted_ts": r2,
			"previous_message": reply(r2, "an unread reply")}))
	}
	p, err := a.Threads(context.Background(), "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Threads) != 1 {
		t.Fatalf("%d threads", len(p.Threads))
	}
	th := p.Threads[0]
	if len(th.Replies) != 1 || th.Replies[0].TS != r1 {
		t.Errorf("the card's replies: %d, want the read one alone", len(th.Replies))
	}
	if th.UnreadCount != 0 {
		t.Errorf("the card counts %d unread replies; its one unread reply was deleted", th.UnreadCount)
	}
}

// Every read that writes messages into the store announces what it
// brought: an announceReads after it, in its own block or one around it,
// before any return that follows it. The revisions a read notes wait in
// the store until core announces them, and a read that returns first (a
// later page failing in a loop of pages) strands them until another
// read's announcement. This holds msg-latency's reads to it once merged:
// SetLatestSince in fetchLatest, RefreshRange in refreshOlder,
// PrependOlderAt in History's older pages.
func TestReadsAnnounce(t *testing.T) {
	reads := map[string]bool{"SetLatestRead": true, "SetLatestSince": true, "SetThreadSince": true, "PutFetched": true,
		"PutFetchedAsked": true, "PrependOlderSince": true, "PrependOlderAt": true, "RefreshRange": true,
		"AddThreadPageSince": true,                  // a replies read's page (thread_pages.go)
		"SetLatestReadAt":    true, "CatchUp": true} // a newest page and a catch-up past a stale end (reached_end.go)
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	found := 0
	isRead := func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return false
		}
		sel, ok := c.Fun.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		x, ok := sel.X.(*ast.SelectorExpr)
		return ok && x.Sel.Name == "S" && reads[sel.Sel.Name]
	}
	// has reports whether n holds a call f wants, not looking into
	// function literals (another function's body)
	has := func(n ast.Node, f func(ast.Node) bool) bool {
		hit := false
		ast.Inspect(n, func(m ast.Node) bool {
			if hit {
				return false
			}
			if _, ok := m.(*ast.FuncLit); ok && m != n {
				return false
			}
			if f(m) {
				hit = true
			}
			return !hit
		})
		return hit
	}
	isAnnounce := func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return false
		}
		sel, ok := c.Fun.(*ast.SelectorExpr)
		return ok && sel.Sel.Name == "announceReads"
	}
	isReturn := func(n ast.Node) bool { _, ok := n.(*ast.ReturnStmt); return ok }
	// announced reports whether the statements after the one at i of
	// blocks[len-1] announce before they return, else whether the blocks
	// around it do
	var announced func(blocks []*ast.BlockStmt, at []int) bool
	announced = func(blocks []*ast.BlockStmt, at []int) bool {
		if len(blocks) == 0 {
			return false
		}
		b, i := blocks[len(blocks)-1], at[len(at)-1]
		for _, st := range b.List[i+1:] {
			if has(st, isAnnounce) {
				return true
			}
			if has(st, isReturn) {
				return false
			}
		}
		return announced(blocks[:len(blocks)-1], at[:len(at)-1])
	}
	var walk func(body *ast.BlockStmt, name string)
	walk = func(body *ast.BlockStmt, name string) {
		var blocks []*ast.BlockStmt
		var at []int
		var visit func(n ast.Node) bool
		visit = func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.FuncLit:
				walk(n.Body, name+" (a func literal)")
				return false
			case *ast.BlockStmt:
				blocks, at = append(blocks, n), append(at, 0)
				for i, st := range n.List {
					at[len(at)-1] = i
					ast.Inspect(st, visit)
				}
				blocks, at = blocks[:len(blocks)-1], at[:len(at)-1]
				return false
			}
			if isRead(n) {
				found++
				c := n.(*ast.CallExpr)
				if !announced(blocks, at) {
					t.Errorf("%s: %s in %s writes a read into the store and nothing after it, before it returns, announces what it brought (announceReads)",
						fset.Position(c.Pos()), c.Fun.(*ast.SelectorExpr).Sel.Name, name)
				}
			}
			return true
		}
		ast.Inspect(body, visit)
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Body != nil {
				walk(fn.Body, fn.Name.Name)
			}
		}
	}
	if found < 5 {
		t.Fatalf("found %d reads; the list of the store's read calls is out of date", found)
	}
}

func (f *fakeSlack) count(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, m := range f.calls {
		if m == method {
			n++
		}
	}
	return n
}

// The messages the views fetch again by id go out together: three
// channels asked for one after another within a moment share one
// messages.list (its message_ids takes every channel, which the client
// splits ten a call, two at a time: Slack's UnknownFetchManager), and a
// message asked for while its fetch is out isn't asked for again.
func TestRefetchBatch(t *testing.T) {
	a, f := testApp(t)
	want := map[string]string{"C1": "1762791700.000100", "C2": "1762791710.000100", "C3": "1762791720.000100"}
	answer := map[string]any{}
	for ch, ts := range want {
		answer[ch] = []any{map[string]any{"type": "message", "ts": ts, "user": "U1", "text": "a synthetic message in " + ch}}
	}
	b, _ := json.Marshal(map[string]any{"ok": true, "messages": answer})
	f.reply["messages.list"] = string(b)
	release := make(chan struct{})
	var once sync.Once
	free := func() { once.Do(func() { close(release) }) }
	t.Cleanup(free) // before the fake server closes: a failed wait never leaves its handlers blocked
	f.hooks["messages.list"] = func(url.Values) { <-release }
	for _, ch := range []string{"C1", "C2", "C3"} {
		a.RefetchMessages(ch, []string{want[ch]})
	}
	waitFor(t, "the fetch out", 5*time.Second, func() bool { return f.count("messages.list") >= 1 })
	// any other request for them would be out by now; C1's, out, isn't
	// asked for again
	time.Sleep(300 * time.Millisecond)
	a.RefetchMessages("C1", []string{want["C1"]})
	free()
	for ch, ts := range want {
		waitFor(t, "the message announced in "+ch, 5*time.Second, func() bool { c, ok := a.MessageChange(ch, ts); return ok && c.Msg != nil })
	}
	time.Sleep(300 * time.Millisecond)
	if n := f.count("messages.list"); n != 1 {
		t.Errorf("%d messages.list calls, want one for the three channels' messages", n)
	}
	ids := f.last("messages.list").Get("message_ids")
	for ch := range want {
		if !strings.Contains(ids, ch) {
			t.Errorf("messages.list's message_ids %s lack %s", ids, ch)
		}
	}
}

// A failed fetch by id is asked for again by core once its backoff is
// over, with no view asking: a page with nothing changing draws no frame
// to ask from, and the row would shimmer until something else drew one.
func TestRefetchRetriesByItself(t *testing.T) {
	a, f := testApp(t)
	const ch, ts = "C1", "1762791750.000100"
	f.reply["messages.list"] = `{"ok":false,"error":"fatal_error"}` // not one the client retries
	ok, _ := json.Marshal(map[string]any{"ok": true, "messages": map[string]any{ch: []any{
		map[string]any{"type": "message", "ts": ts, "user": "U1", "text": "a synthetic message fetched again"}}}})
	f.hooks["messages.list"] = func(url.Values) {
		f.mu.Lock()
		f.reply["messages.list"] = string(ok) // the next one answers
		f.mu.Unlock()
	}
	a.RefetchMessages(ch, []string{ts})
	waitFor(t, "the message after core's own retry", 8*time.Second, func() bool {
		c, ok := a.MessageChange(ch, ts)
		return ok && c.Msg != nil
	})
	if n := f.count("messages.list"); n != 2 {
		t.Errorf("%d messages.list calls, want the failed one and core's retry", n)
	}
}

// A message Slack answers without (deleted since: Slack's isNonExistent)
// is gone for the views: the change table says so, and nothing removes
// the Activity entries about it (the row draws no body).
func TestRefetchGone(t *testing.T) {
	a, f := testApp(t)
	const ch, ts = "C1", "1762791800.000100"
	f.reply["messages.list"] = `{"ok":true,"messages":{}}`
	evs, stop := a.Subscribe()
	defer stop()
	a.RefetchMessages(ch, []string{ts})
	next(t, evs, "gone")
	c, ok := a.MessageChange(ch, ts)
	if !ok || !c.Deleted || c.RemoveSeq != 0 {
		t.Fatalf("after Slack answered without the message: %+v (%v); want it gone, no entry removed", c, ok)
	}
}

// A failed fetch backs off: the message isn't asked for again for a
// while, longer than the client's own refusal of an identical failed
// request (slack/guard.go, a second after the first failure).
func TestRefetchBackoff(t *testing.T) {
	a, f := testApp(t)
	const ch, ts = "C1", "1762791900.000100"
	f.reply["messages.list"] = `{"ok":false,"error":"internal_error"}`
	a.RefetchMessages(ch, []string{ts})
	waitFor(t, "the first fetch", 5*time.Second, func() bool { return f.count("messages.list") == 1 })
	time.Sleep(1300 * time.Millisecond)
	a.RefetchMessages(ch, []string{ts})
	time.Sleep(200 * time.Millisecond)
	if n := f.count("messages.list"); n != 1 {
		t.Fatalf("%d messages.list calls 1.3 s after a failure, want no second yet", n)
	}
	time.Sleep(time.Second)
	a.RefetchMessages(ch, []string{ts})
	waitFor(t, "the fetch after the backoff", 5*time.Second, func() bool { return f.count("messages.list") == 2 })
}
