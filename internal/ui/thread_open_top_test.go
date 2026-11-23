package ui

// Where an opened thread shows, as Slack's code puts it (Karim, #clack
// 2026-11-22, Windows α.140, against Taut: "opening a thread makes it
// appear at the top, intended? maybe not"). Three things could go to the
// top; Slack's rules (build 02bccc7a986523e4):
//
//   - The channel: the reply bar's openThread (client-boot-shared r0Yq)
//     is navigate(toThread({channelId, threadTs})) and setFocusInput, with
//     no highlightRoot, and the toolbar's Reply (0yz+, the comment thunk)
//     is the same: the conversation beside the pane isn't moved. Its list
//     narrows; AnchorLayout.setHeight (dxC+) keeps the row on its anchor
//     line where it was, and with one-line rows every row stays put.
//   - The Threads view: the threads keep the order their load gave them
//     (ThreadsView, client-threads-view 9Ra5); only a new reply or a
//     follow puts a thread on top, at the next refresh. Opening one, and
//     the pane marking it read, moves nothing.
//   - The thread pane: getThreadStartPosition (boot-shared 4M8N) over
//     getThreadStartTs (client-boot 7Kqu): a followed thread starts at
//     its first unread reply 100px under the list's top, or at the reply
//     it was read to (100px too, which for a thread read to its newest
//     reply is its end: bracketScrollTop); a thread the viewer doesn't
//     follow starts at its root, at the top. The thread list (Twp/)
//     anchors there as it mounts (FlexpaneLoading is an overlay over it),
//     so the first frame that draws the thread draws it there.
//
// A notification's click is jumpToMessage: toThread with replyTs and
// highlightRoot, which does move the conversation, to the root on its
// anchor line (64px), and starts the thread at the reply 100px down, both
// highlighted. Search results and links take the same jump; Activity opens
// the thread beside its own list.
//
// fakeslack's #open-top (Size.ThreadOpen): 160 one-line messages, threads
// of 80 one-line replies served a page of 28 at a time, followed and read,
// unread past the first page, unread on it, or not followed.

import (
	"fmt"
	"image"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/desktop"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/views"
)

// openTopEnv opens #open-top at its newest messages in a focused window,
// with the inspect tool, which finds the reply bar and the toolbar's
// Reply where they are drawn.
func openTopEnv(t *testing.T) *rfEnv {
	t.Helper()
	t.Setenv("CLACK_INSPECT", "1")
	sz := fakeslack.Small
	sz.ThreadOpen = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	x := &rfEnv{t: t, ch: fakeslack.ThreadOpenChannel, clk: time.Second}
	x.perfEnv = startEnv(t, fs, t.TempDir())
	t.Cleanup(func() {
		if x.a.data != nil {
			x.a.data.stopTaut()
		}
	})
	if x.a.insp == nil {
		t.Fatal("CLACK_INSPECT=1 but the window has no inspect tool")
	}
	x.a.focused = true // App.Run: the first ConfigEvent of a shown window
	x.waitReady()
	x.openChannel(x.ch)
	x.until("the channel's newest message", 20*time.Second, func() bool {
		_, ok := x.row(fakeslack.ThreadOpenNewest)
		return ok
	})
	x.settle(500 * time.Millisecond)
	return x
}

// otLabel names a fixture message for a failure: root, r<n> for reply n
// of the open thread, m<n> for the channel's message n.
func (x *rfEnv) otLabel(ts string) string {
	if ts != "" && ts == x.root {
		return "root"
	}
	if x.root != "" {
		for i := 0; i < fakeslack.ThreadOpenReplies; i++ {
			if fakeslack.ThreadOpenReply(x.root, i) == ts {
				return fmt.Sprintf("r%d", i+1)
			}
		}
	}
	for i := 0; i < fakeslack.ThreadOpenMsgs; i++ {
		if fakeslack.ThreadOpenMsg(i) == ts {
			return fmt.Sprintf("m%d", i+1)
		}
	}
	return ts
}

// rowTops is where a list draws the message rows it shows: each row's
// top under the list's top.
func rowTops(rects []messages.RowRect, list image.Rectangle) map[string]int {
	out := map[string]int{}
	for _, r := range rects {
		if r.R.Overlaps(list) {
			out[r.TS] = r.R.Min.Y - list.Min.Y
		}
	}
	return out
}

func (x *rfEnv) channelTops() map[string]int {
	cv := x.cv()
	if cv == nil {
		return nil
	}
	return rowTops(cv.RowRects(), cv.ListRect())
}

func (x *rfEnv) paneTops() map[string]int {
	tv := x.threadView(x.ch, x.root)
	if tv == nil {
		return nil
	}
	return rowTops(tv.RowRects(), tv.ListRect())
}

// drawnTops lists tops for a failure, top first.
func (x *rfEnv) drawnTops(m map[string]int) string {
	type row struct {
		ts string
		y  int
	}
	var rs []row
	for ts, y := range m {
		rs = append(rs, row{ts, y})
	}
	slices.SortFunc(rs, func(a, b row) int { return a.y - b.y })
	var b strings.Builder
	for i, r := range rs {
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "%s@%d", x.otLabel(r.ts), r.y)
	}
	return b.String()
}

// wheelTo scrolls the channel with the wheel until row ts has its top in
// the upper middle of the list, and lets the list come to rest.
func (x *rfEnv) wheelTo(ts string) {
	x.t.Helper()
	dl := time.Now().Add(60 * time.Second)
	for {
		list := x.cv().ListRect()
		h := list.Dy()
		if r, ok := x.row(ts); ok {
			top := r.Min.Y - list.Min.Y
			if top >= h/4 && top <= h/2 {
				break
			}
			x.scroll(x.listPoint(), float32(top-h/3))
		} else {
			x.scroll(x.listPoint(), -float32(h/2))
		}
		if time.Now().After(dl) {
			x.t.Fatalf("the wheel never brought %s to the middle of the channel's list (%s)", x.otLabel(ts), x.drawnTops(x.channelTops()))
		}
		x.settle(400 * time.Millisecond)
	}
	x.settle(700 * time.Millisecond)
	if _, ok := x.row(ts); !ok {
		x.t.Fatalf("%s left the channel's list after the wheel stopped (%s)", x.otLabel(ts), x.drawnTops(x.channelTops()))
	}
}

// otStart is where Slack starts the pane: row key with its top y px under
// the list's top, or with end the list at its end, the newest reply on
// screen and the root not.
type otStart struct {
	key string
	y   int
	end bool
}

// startMiss says how a frame's rows miss want ("" when they don't).
func (x *rfEnv) startMiss(got map[string]int, want otStart) string {
	if want.end {
		if _, ok := got[fakeslack.ThreadOpenReply(x.root, fakeslack.ThreadOpenReplies-1)]; !ok {
			return "without its newest reply: Slack starts a thread read to its newest reply there, at its end"
		}
		if _, ok := got[x.root]; ok {
			return "with its root"
		}
		return ""
	}
	y, ok := got[want.key]
	if !ok {
		return fmt.Sprintf("without %s, where Slack starts it (%dpx under the top)", x.otLabel(want.key), want.y)
	}
	if y != want.y {
		return fmt.Sprintf("%s %dpx under the top; Slack starts it at %dpx", x.otLabel(want.key), y, want.y)
	}
	return ""
}

// paneStart runs frames until the pane draws the open thread, and fails
// unless the first frame that draws it, and every frame for hold after,
// draws it where Slack starts it.
func (x *rfEnv) paneStart(what string, want otStart, hold time.Duration) {
	x.t.Helper()
	dl := time.Now().Add(20 * time.Second)
	var since time.Time
	for {
		x.frame()
		s := x.a.secondary
		got := x.paneTops()
		if s.kind == secThread && !s.hidden && s.channel == x.ch && s.ts == x.root && len(got) > 0 {
			if miss := x.startMiss(got, want); miss != "" {
				if since.IsZero() {
					x.t.Fatalf("%s: the first frame that drew the thread drew it %s (%s)", what, miss, x.drawnTops(got))
				}
				x.t.Fatalf("%s: %v after the pane drew its start it drew the thread %s (%s)", what, time.Since(since).Round(time.Millisecond), miss, x.drawnTops(got))
			}
			if since.IsZero() {
				since = time.Now()
			}
			if time.Since(since) >= hold {
				return
			}
		} else if !since.IsZero() {
			x.t.Fatalf("%s: the pane stopped drawing the thread (%s/%s, %d rows)", what, s.channel, s.ts, len(got))
		}
		if time.Now().After(dl) {
			x.t.Fatalf("%s: the pane never drew the thread (%s/%s)", what, s.channel, s.ts)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// sameChannel fails unless the channel draws every row it drew before
// (before: channelTops) where it drew it.
func (x *rfEnv) sameChannel(what string, before map[string]int) {
	x.t.Helper()
	after := x.channelTops()
	var moved []string
	for ts, y := range before {
		if ay, ok := after[ts]; !ok || ay != y {
			moved = append(moved, fmt.Sprintf("%s %d→%d (drawn %v)", x.otLabel(ts), y, ay, ok))
		}
	}
	if len(moved) > 0 {
		slices.Sort(moved)
		x.t.Errorf("%s: the channel moved as the thread opened (%s); Slack leaves it where it was. Before: %s; after: %s",
			what, strings.Join(moved, ", "), x.drawnTops(before), x.drawnTops(after))
	}
}

// atPaneEnd fails unless the pane's list is at its end: scrolling it to
// the bottom moves nothing.
func (x *rfEnv) atPaneEnd(what string) {
	x.t.Helper()
	tv := x.threadView(x.ch, x.root)
	b0, ok := tv.NewestRowBottom()
	if !ok {
		x.t.Fatalf("%s: the newest reply isn't on screen", what)
	}
	tv.ScrollToBottom()
	x.settle(300 * time.Millisecond)
	if b1, ok := tv.NewestRowBottom(); !ok || b1 != b0 {
		x.t.Errorf("%s: the pane wasn't at its end: scrolled to the bottom, the newest reply's bottom went from y %d to %d (%v)", what, b0, b1, ok)
	}
}

// TestThreadsAuditOpenLikeSlack opens each kind of thread the way the
// viewer does, from the channel scrolled to it: the pane starts where
// Slack's does and the channel stays where it was.
func TestThreadsAuditOpenLikeSlack(t *testing.T) {
	for _, tc := range []struct {
		name    string
		root    string
		toolbar bool // the toolbar's Reply, not the reply bar
		reply   int  // the reply Slack starts at, 100px under the top (0: the root at the top)
		end     bool // Slack starts at the newest reply, which shows the end
	}{
		{"followed and read: its end", fakeslack.ThreadOpenRead, false, 0, true},
		{"unread past the first page: reply 51", fakeslack.ThreadOpenUnread, false, 51, false},
		{"unread on the first page: reply 11", fakeslack.ThreadOpenEarly, false, 11, false},
		{"not followed: its root", fakeslack.ThreadOpenFresh, false, 0, false},
		{"toolbar Reply, unread: reply 51", fakeslack.ThreadOpenUnread, true, 51, false},
		{"the channel at its end, followed and read", fakeslack.ThreadOpenNewest, false, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x := openTopEnv(t)
			x.root = tc.root
			want := otStart{key: tc.root}
			switch {
			case tc.end:
				want = otStart{end: true}
			case tc.reply > 0:
				want = otStart{key: fakeslack.ThreadOpenReply(tc.root, tc.reply-1), y: x.px(100)} // startOffset 100
			}
			if tc.root != fakeslack.ThreadOpenNewest {
				x.wheelTo(tc.root)
			}
			before := x.channelTops()
			if _, ok := before[tc.root]; !ok {
				t.Fatalf("the root isn't on screen (%s)", x.drawnTops(before))
			}
			if tc.toolbar {
				x.click(x.toolbarReply(tc.root))
			} else {
				x.click(x.replyBar())
			}
			x.paneStart("opened", want, 1500*time.Millisecond)
			x.sameChannel("opened", before)
			if want.end {
				x.atPaneEnd("opened")
			}
		})
	}
}

// TestThreadsAuditOpenKeepsThreadsOrder: the Threads view lists its
// threads as Slack's client sorts a load, whatever getView's order
// (processThreads, boot-shared zoZ+, and ThreadsView's sortThreads, 8orQ:
// unread threads first, then each group newest activity first), and a
// thread opened beside it, and marked read by the pane, keeps its place,
// the list scrolled where it was.
func TestThreadsAuditOpenKeepsThreadsOrder(t *testing.T) {
	x := openTopEnv(t)
	x.root = fakeslack.ThreadOpenUnread
	view := func() *views.Threads {
		v, _ := x.a.views[Route("threads")].(*views.Threads)
		return v
	}
	key := func(root string) string { return x.ch + "/" + root }
	label := func(keys []string) string {
		var out []string
		for _, k := range keys {
			_, ts, _ := strings.Cut(k, "/")
			out = append(out, x.otLabel(ts))
		}
		return strings.Join(out, " ")
	}
	// getView answers newest reply first (Newest, Early, Unread, Read);
	// Early and Unread have unread replies
	want := []string{key(fakeslack.ThreadOpenEarly), key(fakeslack.ThreadOpenUnread), key(fakeslack.ThreadOpenNewest), key(fakeslack.ThreadOpenRead)}
	x.a.Navigate("threads")
	x.until("the Threads view's threads", 20*time.Second, func() bool {
		v := view()
		if v == nil || !v.Settled() {
			return false
		}
		keys, _, _ := v.Listed()
		return len(keys) == len(want)
	})
	x.settle(time.Second) // the view marks what it shows read 350 ms on (markVisible)
	keys, first, off := view().Listed()
	if !slices.Equal(keys, want) {
		t.Fatalf("the Threads view lists %s; Slack's sortThreads gives %s", label(keys), label(want))
	}
	// the view's Right arrow on the thread (onArrowKeyRight: openThread);
	// the view may have marked it read already, where it shows its newest
	// reply
	x.a.OpenThread(x.ch, x.root)
	x.until("the thread beside the Threads view", 20*time.Second, func() bool {
		s := x.a.secondary
		return s.kind == secThread && !s.hidden && s.ts == x.root && len(x.paneTops()) > 0
	})
	x.until("the thread marked read", 10*time.Second, func() bool {
		for _, m := range x.fs.ThreadOpenMarks() {
			if m.ThreadTS == x.root {
				return true
			}
		}
		return false
	})
	x.settle(2 * time.Second)
	keys2, first2, off2 := view().Listed()
	if !slices.Equal(keys2, keys) {
		t.Errorf("opening %s moved it in the Threads view: %s, before %s; Slack keeps the order", label([]string{key(x.root)}), label(keys2), label(keys))
	}
	if first2 != first || off2 != off {
		t.Errorf("opening a thread scrolled the Threads view from item %d +%dpx to item %d +%dpx; Slack leaves it", first, off, first2, off2)
	}
}

// TestThreadsAuditOpenFromToast: a reply's notification clicked from
// another conversation (toThread with replyTs and highlightRoot): the
// conversation at the root on its anchor line, the thread at the reply
// 100px down (it is past the last read), both highlighted.
func TestThreadsAuditOpenFromToast(t *testing.T) {
	x := openTopEnv(t)
	a := x.a
	x.root = fakeslack.ThreadOpenUnread
	reply := fakeslack.ThreadOpenReply(x.root, 59)
	other := x.fs.ChannelIDs()[1]
	a.Navigate("c/" + other)
	x.until("the other conversation", 10*time.Second, func() bool { return a.router.Current() == Route("c/"+other) })
	x.settle(300 * time.Millisecond)
	fc := newFakeCenter()
	a.notify = newMacNotifier(fc)
	n := a.messageNote(x.ch, reply, x.root)
	n.Title, n.Body = "New reply in #open-top", "Reply 60"
	_ = a.notify.(noteNotifier).NotifyNote(n)
	u := fc.waitShown(t, a, 1)
	fc.respond(a, desktop.UserNotificationEvent{ID: u.ID, Kind: "click", Index: -1})
	x.paneStart("the reply's notification", otStart{key: reply, y: x.px(100)}, 1500*time.Millisecond)
	x.until("the reply and the root placed and highlighted", 10*time.Second, func() bool {
		tv, cv := x.threadView(x.ch, x.root), x.channelView(x.ch)
		return tv != nil && cv != nil && tv.JumpPending() == "" && tv.Highlighted() == reply && cv.JumpPending() == "" && cv.Highlighted() == x.root
	})
	if y, ok := x.channelTops()[x.root]; !ok || y != x.anchorLine() {
		t.Errorf("the conversation drew the root %v at %dpx; Slack's highlightRoot puts it on the anchor line, %dpx (%s)", ok, y, x.anchorLine(), x.drawnTops(x.channelTops()))
	}
}

// TestThreadsAuditOpenAgainAfterAnother: a thread opened, scrolled to its
// top, then another thread opened in the pane and the first opened again
// from its reply bar. Slack's thread list is keyed by its thread (the
// list's key in ThreadsFlexpane.renderMessageList), so the first thread's
// list mounts afresh and starts at getThreadStartPosition again: a thread
// switch saves no scrollmark (componentDidUpdate only clears the new
// thread's; maybeSaveThreadScrollmarks runs as the pane unmounts). The
// thread was marked read as it showed, to the newest reply it held, so the
// new list starts at that reply (bracketed at the end of what it holds):
// the first frame shows it, and no frame the root. The page after it is
// held meanwhile: as it lands the reply box comes under Clack's list,
// where Slack's input row is the list's own last row.
func TestThreadsAuditOpenAgainAfterAnother(t *testing.T) {
	x := openTopEnv(t)
	x.root = fakeslack.ThreadOpenUnread
	x.wheelTo(x.root)
	x.click(x.replyBar())
	x.paneStart("first open", otStart{key: fakeslack.ThreadOpenReply(x.root, 50), y: x.px(100)}, 500*time.Millisecond)
	x.until("the pane marking the thread read", 10*time.Second, func() bool {
		for _, m := range x.fs.ThreadOpenMarks() {
			if m.ThreadTS == x.root {
				return true
			}
		}
		return false
	})
	// the reader scrolls the pane up to the root
	tv := x.threadView(x.ch, x.root)
	pane := tv.ListRect()
	at := image.Pt((pane.Min.X+pane.Max.X)/2, (pane.Min.Y+pane.Max.Y)/2)
	x.until("the pane scrolled up to the root", 60*time.Second, func() bool {
		if y, ok := x.paneTops()[x.root]; ok && y >= 0 {
			return true
		}
		x.scroll(at, -float32(pane.Dy()))
		x.settle(300 * time.Millisecond)
		return false
	})
	x.settle(500 * time.Millisecond)
	first := x.root
	// another thread in the pane
	x.root = fakeslack.ThreadOpenEarly
	x.a.OpenThread(x.ch, x.root)
	x.paneStart("the other thread", otStart{key: fakeslack.ThreadOpenReply(x.root, 10), y: x.px(100)}, 500*time.Millisecond)
	// the first again, from its reply bar
	x.root = first
	read := ""
	for _, m := range x.fs.ThreadOpenMarks() {
		if m.ThreadTS == x.root {
			read = m.TS // the reply the pane marked it read to
		}
	}
	// the page past it (loadPost) waits until the start has been looked at
	// (a poll's read may come first: the next three wait)
	x.fs.SetFault("conversations.replies", fakeslack.Fault{Delay: 2500 * time.Millisecond, Times: 3})
	t.Cleanup(func() { x.fs.SetFault("conversations.replies", fakeslack.Fault{}) })
	x.click(x.replyBar())
	dl := time.Now().Add(20 * time.Second)
	var since time.Time
	for {
		x.frame()
		got := x.paneTops()
		if s := x.a.secondary; s.kind == secThread && s.ts == x.root && len(got) > 0 {
			if _, ok := got[x.root]; ok {
				t.Fatalf("opened again after another thread, the pane drew the thread from its root (%s); Slack's list mounts afresh at the reply it was read to", x.drawnTops(got))
			}
			if since.IsZero() {
				if _, ok := got[read]; !ok {
					t.Fatalf("opened again after another thread, the first frame that drew the thread drew it without %s, the reply it was read to, where Slack's new list starts (%s)", x.otLabel(read), x.drawnTops(got))
				}
				since = time.Now()
			}
			if time.Since(since) >= 1500*time.Millisecond {
				return
			}
		}
		if time.Now().After(dl) {
			t.Fatalf("opened again after another thread, the pane never drew the thread (%s)", x.drawnTops(got))
		}
		time.Sleep(2 * time.Millisecond)
	}
}
