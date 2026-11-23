package ui

// The in-list reply box's review findings (thread-reply-box-in-list,
// 2026-11-22), each through the real pane:
//   - a short thread with a footer in its box's place (an archived
//     channel's bar, a non-member's join pane) sat in a frame loop: its
//     layout guessed the footer at the pane's bottom, drew it under the
//     thread and asked for another frame, every frame; the join pane's
//     menu anchors came from that guess;
//   - the composer's own changes to the text (a pasted link's slug landing
//     from chat.slugifyUrl, shortcodes converting once the emoji set is in)
//     counted as the reader's edits, and brought a scrolled-away box back
//     under the reader;
//   - the autocomplete stayed drawn over the replies after its box
//     scrolled away;
//   - one thread drawn by two containers in a frame (the side pane and
//     Activity's persistent detail panel) had each layout guess from the
//     other's and ask for frame after frame.

import (
	"image"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/app"
	"gioui.org/io/key"
	"gioui.org/io/transfer"
	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/idleprobe"
)

// convoAsks counts layoutConvo's own requests for a frame (App.Invalidate's
// callers), as replyBoxFirstFrame does.
func convoAsks(t *testing.T, a *App) *atomic.Int32 {
	t.Helper()
	var asked atomic.Int32
	invalidateHook = func(b *App) {
		if b == a && strings.Contains(idleprobe.Caller(1), "layoutConvo") {
			asked.Add(1)
		}
	}
	t.Cleanup(func() { invalidateHook = nil })
	return &asked
}

// TestThreadsAuditReplyBoxFooterIdle: a short thread whose channel has a
// footer in the reply box's place (an archived channel's bar, the join
// pane of a public channel the viewer isn't in) draws the footer right
// under the thread and then sits idle: its layout asks for no frame once
// the footer is where the last frame put it. The join pane's buttons, the
// "Join" menu's anchors, are where the pane draws them, under the thread.
func TestThreadsAuditReplyBoxFooterIdle(t *testing.T) {
	for _, tc := range []struct {
		what string
		ch   string
		foot footerKind
	}{
		{"archived", fakeslack.ArchivedChannelID, footArchived},
		{"not a member", fakeslack.PreviewChannelID, footPreview},
	} {
		t.Run(tc.what, func(t *testing.T) {
			fs, err := fakeslack.Start(fakeslack.Small)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { fs.Close() })
			ids := fs.UserIDs()
			root, _ := fs.PostQuiet(tc.ch, ids[2], "synthetic: a short thread")
			fs.PostReplyQuiet(tc.ch, root, ids[3], "synthetic: its first reply")
			fs.PostReplyQuiet(tc.ch, root, ids[2], "synthetic: its second reply")
			e := startEnv(t, fs, t.TempDir())
			t.Cleanup(func() {
				if e.a.data != nil {
					e.a.data.stopTaut()
				}
			})
			e.a.focused = true
			e.waitReady()
			e.a.OpenThread(tc.ch, root)
			var c *convo
			e.until("the short thread with its footer under it", 20*time.Second, func() bool {
				c = e.a.convos[tc.ch+"/"+root]
				tv := e.threadView(tc.ch, root)
				if c == nil || tv == nil || e.a.footerFor(c) != tc.foot || c.compH == 0 {
					return false
				}
				_, ok := tv.NewestRowBottom()
				return ok && len(tv.RowRects()) == 3
			})
			e.settle(800 * time.Millisecond)
			tv := e.threadView(tc.ch, root)
			bottom, _ := tv.NewestRowBottom()
			if pane := tv.ListRect(); bottom+c.compH >= pane.Max.Y {
				t.Fatalf("the thread (down to y %d) and its %d px footer don't fit the pane (y %d): not a short thread", bottom, c.compH, pane.Max.Y)
			}
			asked := convoAsks(t, e.a)
			for i := 0; i < 30; i++ {
				e.frame()
				time.Sleep(5 * time.Millisecond)
			}
			if n := asked.Load(); n > 0 {
				t.Errorf("idle, the pane's layout asked for %d frames in 30: it guessed its footer elsewhere than it drew it (frame after frame at vsync)", n)
			}
			if tc.foot != footPreview {
				return
			}
			jp := c.jp
			if jp == nil || jp.joinRect.Empty() {
				t.Fatal("the join pane has no Join button")
			}
			for _, r := range []struct {
				name string
				r    image.Rectangle
			}{{"Join", jp.joinRect}, {"the Join menu's caret", jp.caretRect}, {"Details", jp.detailsRect}} {
				if r.r.Empty() {
					continue
				}
				// the footer's top is the thread's end (any spacer after
				// its last reply included)
				if lo, hi := bottom, bottom+c.compH+e.px(40); r.r.Min.Y < lo || r.r.Max.Y > hi {
					t.Errorf("%s is at y %d to %d; the pane draws its %d px footer under the thread, which ends at y %d (the menu anchors there)",
						r.name, r.r.Min.Y, r.r.Max.Y, c.compH, bottom)
				}
			}
		})
	}
}

// TestThreadsAuditReplyBoxSlugKeepsPlace: a link pasted into the reply box
// becomes a link slug once chat.slugifyUrl answers, which may take
// seconds; the reader scrolls up meanwhile, and the slug landing in the
// box out of view leaves the list where the reader put it. It's the
// composer's change (applySlugs), not the reader's edit, which alone
// brings a scrolled-away box back.
func TestThreadsAuditReplyBoxSlugKeepsPlace(t *testing.T) {
	x := openTopEnv(t)
	x.openAt(fakeslack.ThreadOpenRead)
	x.holdBox(t, "opened at its end", 300*time.Millisecond)
	c := x.a.convos[x.ch+"/"+x.root]
	if !x.threadFocused() {
		t.Fatal("the reply bar's open left the reply box unfocused")
	}
	t.Cleanup(x.clearDraft)
	x.fs.SetFault("chat.slugifyUrl", fakeslack.Fault{Delay: 6 * time.Second, Times: 1})
	t.Cleanup(func() { x.fs.SetFault("chat.slugifyUrl", fakeslack.Fault{}) })
	asked := x.fs.CallCount("chat.slugifyUrl")
	x.key("V", key.ModShortcut)
	x.frame()
	x.frame()
	if !x.r.ClipboardRequested() {
		t.Fatal("Ctrl+V in the reply box didn't ask for the clipboard")
	}
	const link = "https://example.com/docs/synthetic-reply-box-slug"
	x.r.Queue(transfer.DataEvent{Type: "application/text", Open: func() io.ReadCloser { return io.NopCloser(strings.NewReader(link)) }})
	x.until("chat.slugifyUrl asked for the pasted link", 5*time.Second, func() bool { return x.fs.CallCount("chat.slugifyUrl") > asked })
	box := x.box(x.ch, x.root)
	pasted := box.DocText()
	if !strings.Contains(pasted, "example.com") {
		t.Fatalf("the box holds %q after the paste", pasted)
	}
	// up while the answer waits: the box goes with the replies
	paneBottom := c.compRect.Max.Y
	at := x.paneWheelPoint()
	x.until("the box scrolled away", 10*time.Second, func() bool {
		if c.compRect.Min.Y >= paneBottom {
			return true
		}
		x.scroll(at, -400)
		x.settle(300 * time.Millisecond)
		return false
	})
	x.settle(300 * time.Millisecond)
	if box.DocText() != pasted {
		t.Fatal("the slug landed before the list was scrolled up: hold chat.slugifyUrl longer")
	}
	before := x.paneTops()
	x.until("the pasted link became a slug", 15*time.Second, func() bool { return box.DocText() != pasted })
	x.settle(600 * time.Millisecond)
	after := x.paneTops()
	for ts, y := range before {
		if ay, ok := after[ts]; !ok || ay != y {
			t.Fatalf("the slug landing moved the list under the reader: %s from y %d to %d (drawn %v); before %s, after %s",
				x.otLabel(ts), y, ay, ok, x.drawnTops(before), x.drawnTops(after))
		}
	}
	if c.compRect.Min.Y < paneBottom {
		t.Errorf("the slug landing brought the box back (its top at y %d, the pane ends at %d)", c.compRect.Min.Y, paneBottom)
	}
}

// TestThreadsAuditReplyBoxAutocompleteGoesWithIt: the emoji autocomplete
// open over the reply box closes as the box scrolls out of view, rather
// than float over the replies (its list is drawn deferred, unclipped);
// typing on brings the box back with the autocomplete open again.
func TestThreadsAuditReplyBoxAutocompleteGoesWithIt(t *testing.T) {
	x := openTopEnv(t)
	x.openAt(fakeslack.ThreadOpenRead)
	x.holdBox(t, "opened at its end", 300*time.Millisecond)
	c := x.a.convos[x.ch+"/"+x.root]
	if !x.threadFocused() {
		t.Fatal("the reply bar's open left the reply box unfocused")
	}
	t.Cleanup(x.clearDraft)
	box := x.box(x.ch, x.root)
	x.typeText(":sm")
	x.until("the emoji autocomplete open", 5*time.Second, box.PopupOpen)
	paneBottom := c.compRect.Max.Y
	// the wheel near the pane's top, clear of the open list (above the box)
	l := x.threadView(x.ch, x.root).ListRect()
	at := image.Pt((l.Min.X+l.Max.X)/2, l.Min.Y+x.px(60))
	x.until("the box scrolled away", 10*time.Second, func() bool {
		if c.compRect.Min.Y >= paneBottom {
			return true
		}
		x.scroll(at, -400)
		x.settle(300 * time.Millisecond)
		return false
	})
	x.settle(300 * time.Millisecond)
	if box.PopupOpen() {
		t.Fatal("the autocomplete stays open with its box scrolled out of view: its list floats over the replies")
	}
	x.typeText("i")
	x.frame()
	x.holdBox(t, "typed on into the box out of view", 300*time.Millisecond)
	x.until("the autocomplete open again", 5*time.Second, box.PopupOpen)
}

// frameTwice is frame with c laid out a second time after the window, as
// another container drawing the same thread would (Activity's persistent
// detail panel: ThreadPane lays out the convo the side pane does), at org
// in a container of size sz.
func (x *rfEnv) frameTwice(c *convo, org, sz image.Point) {
	a := x.a
	a.drain()
	x.now = time.Now()
	gtx := x.context()
	a.beginFullFrame()
	a.frame(gtx)
	g := gtx
	g.Constraints = layout.Exact(sz)
	st := op.Offset(org).Push(gtx.Ops)
	a.layoutConvo(g, c, org)
	st.Pop()
	a.noteFullFrame(x.size, gtx.Metric, app.Insets{}, false)
	x.r.Frame(&x.ops)
}

// TestThreadsAuditReplyBoxTwoContainers: one thread drawn in two
// containers in a frame (the side pane and, shorter, a second one, as
// Activity's persistent detail panel draws the thread the side pane
// shows) settles: each layout guesses its box's place from its own last
// frame, so neither asks for frame after frame.
func TestThreadsAuditReplyBoxTwoContainers(t *testing.T) {
	x := openTopEnv(t)
	x.openAt(fakeslack.ThreadOpenRead)
	x.holdBox(t, "opened at its end", 300*time.Millisecond)
	c := x.a.convos[x.ch+"/"+x.root]
	pane := x.threadView(x.ch, x.root).ListRect()
	org, sz := image.Pt(pane.Min.X, 0), image.Pt(pane.Dx(), x.size.Y*2/3)
	for i := 0; i < 20; i++ { // both layouts' first frames
		x.frameTwice(c, org, sz)
		time.Sleep(5 * time.Millisecond)
	}
	asked := convoAsks(t, x.a)
	for i := 0; i < 30; i++ {
		x.frameTwice(c, org, sz)
		time.Sleep(5 * time.Millisecond)
	}
	if n := asked.Load(); n > 0 {
		t.Errorf("drawn in two containers a frame, the thread's layouts asked for %d frames in 30: each guessed its box where the other drew it", n)
	}
}
