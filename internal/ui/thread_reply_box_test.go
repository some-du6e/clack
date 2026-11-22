package ui

// A thread's reply box is the thread list's last row, as Slack's is:
// getThreadRowsAndKeys (boot-shared eTbq) ends the rows with "input" once
// the slice reaches the thread's end ("loading" before), renderRow draws
// the reply input there, and getPersistentKeys keeps that row mounted
// when it scrolls out of view. So the box scrolls with the replies, sits
// right under the last one at the thread's end, and takes room no reply
// needs: as the end loads the rows above stay where they were (AnchorLayout
// setKeys keeps the row on its line), and a box that grows at the end
// keeps the list there (setHeight sticks to the bottom within 2px). Clack
// drew the box under the list instead: when the end loaded the box came
// and covered the rows at the bottom (the gap thread-open-top found,
// 2026-11-22). Typing into the box while it is scrolled away brings it
// back, as Chromium scrolls a focused editor's caret into view after an
// edit (its own focus doesn't: Slack's texty focuses with preventScroll).
//
// Through the real pane on fakeslack's #open-top (Size.ThreadOpen): 80
// one-line replies served a page of 28 at a time.

import (
	"fmt"
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/shortcut"
)

// boxUnderNewest says how the last frame misses Slack's thread end ("" when
// it doesn't): the thread's last reply drawn whole, the reply box 12px
// under it (.p-threads_footer__input_container's margin-top), the box
// inside the window.
func (x *rfEnv) boxUnderNewest() string {
	tv := x.threadView(x.ch, x.root)
	c := x.a.convos[x.ch+"/"+x.root]
	if tv == nil || c == nil {
		return "no thread pane"
	}
	last := fakeslack.ThreadOpenReply(x.root, fakeslack.ThreadOpenReplies-1)
	if _, ok := x.paneTops()[last]; !ok {
		return "the thread's last reply isn't drawn"
	}
	bottom, ok := tv.NewestRowBottom()
	if !ok {
		return "the newest reply isn't drawn"
	}
	box := replyBoxRect(c)
	if box.Empty() {
		return "no reply box"
	}
	if gap := box.Min.Y - bottom; gap != x.px(slackThreadGap) {
		return fmt.Sprintf("the box's top is %d px under the last reply's bottom (y %d), Slack's %d", gap, bottom, x.px(slackThreadGap))
	}
	if box.Max.Y > x.size.Y {
		return fmt.Sprintf("the box ends at y %d, past the window's %d", box.Max.Y, x.size.Y)
	}
	return ""
}

// holdBox runs frames until the thread's end shows as Slack's does
// (boxUnderNewest), then fails unless every frame for d does.
func (x *rfEnv) holdBox(t *testing.T, what string, d time.Duration) {
	t.Helper()
	x.until(what+": the last reply and the box under it", 20*time.Second, func() bool { return x.boxUnderNewest() == "" })
	for dl := time.Now().Add(d); time.Now().Before(dl); {
		x.frame()
		if miss := x.boxUnderNewest(); miss != "" {
			t.Fatalf("%s: %s (%s)", what, miss, x.drawnTops(x.paneTops()))
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// paneWheelPoint is a point in the middle of the thread pane's list.
func (x *rfEnv) paneWheelPoint() image.Point {
	r := x.threadView(x.ch, x.root).ListRect()
	return image.Pt((r.Min.X+r.Max.X)/2, (r.Min.Y+r.Max.Y)/2)
}

// openAt opens root's thread from its reply bar (the channel wheeled to it)
// and waits for the pane to draw it.
func (x *rfEnv) openAt(root string) {
	x.t.Helper()
	x.root = root
	x.wheelTo(root)
	x.click(x.replyBar())
	x.until("the thread drawn", 20*time.Second, func() bool {
		s := x.a.secondary
		return s.kind == secThread && !s.hidden && s.ts == root && len(x.paneTops()) > 0
	})
}

// clearDraft deletes what a test typed into the open thread's box, so no
// later test in the process starts with it (composer drafts are
// process-wide).
func (x *rfEnv) clearDraft() {
	x.key("A", shortcut.Mod())
	x.key(key.NameDeleteBackward, 0)
	x.settle(200 * time.Millisecond)
}

// TestThreadsAuditReplyBoxUnderTheLastReply: at the thread's end its last
// reply is drawn whole with the reply box right under it, never covered:
// a thread read to its end opens there, and an unread one scrolled down
// to it (the pages past its slice loading on the way) ends there too.
func TestThreadsAuditReplyBoxUnderTheLastReply(t *testing.T) {
	t.Run("opened at its end", func(t *testing.T) {
		x := openTopEnv(t)
		x.openAt(fakeslack.ThreadOpenRead)
		x.holdBox(t, "opened at its end", 1500*time.Millisecond)
	})
	t.Run("scrolled down to its end", func(t *testing.T) {
		x := openTopEnv(t)
		x.openAt(fakeslack.ThreadOpenUnread)
		at := x.paneWheelPoint()
		x.until("the wheel at the thread's end", 60*time.Second, func() bool {
			if x.boxUnderNewest() == "" {
				return true
			}
			x.scroll(at, 400)
			x.settle(300 * time.Millisecond)
			return false
		})
		x.settle(500 * time.Millisecond)
		x.holdBox(t, "scrolled down to its end", 1500*time.Millisecond)
	})
}

// TestThreadsAuditReplyBoxArrivesInPlace: the list at the end of a slice
// short of the thread's end, its "loading" row at the bottom; the page
// past it lands and the reply box's row takes the loading row's place at
// the end of what loaded. Every reply drawn before is drawn where it was,
// none covered: Slack's setKeys keeps the rows, and the box comes below
// them, in the list.
func TestThreadsAuditReplyBoxArrivesInPlace(t *testing.T) {
	x := openTopEnv(t)
	x.openAt(fakeslack.ThreadOpenUnread)
	x.settle(500 * time.Millisecond)
	tv := x.threadView(x.ch, x.root)
	c := x.a.convos[x.ch+"/"+x.root]
	// the page past the slice (loadPost) waits while the list sits at the
	// slice's end (a poll's read may come first: the next three wait)
	x.fs.SetFault("conversations.replies", fakeslack.Fault{Delay: 4 * time.Second, Times: 3})
	t.Cleanup(func() { x.fs.SetFault("conversations.replies", fakeslack.Fault{}) })
	asked := len(x.fs.ThreadOpenRequests())
	tv.ScrollToBottom() // the jump-to-bottom button: the slice's end, its "loading" row
	x.until("the page past the slice asked for", 10*time.Second, func() bool { return len(x.fs.ThreadOpenRequests()) > asked })
	x.settle(400 * time.Millisecond)
	before := x.paneTops()
	sliceEnd := fakeslack.ThreadOpenReply(x.root, 76) // reply 77: the around read's last
	if _, ok := before[sliceEnd]; !ok {
		t.Fatalf("at the slice's end the pane doesn't draw its last reply, %s (%s)", x.otLabel(sliceEnd), x.drawnTops(before))
	}
	if !tv.HidesComposer() || x.a.convos[x.ch+"/"+x.root].inputShown {
		t.Fatalf("the reply box shows before the slice reaches the thread's end")
	}
	x.until("the thread's end in, with its reply box", 20*time.Second, func() bool { return !tv.HidesComposer() && !replyBoxRect(c).Empty() })
	x.settle(600 * time.Millisecond)
	after := x.paneTops()
	var moved []string
	for ts, y := range before {
		if ay, ok := after[ts]; !ok || ay != y {
			moved = append(moved, fmt.Sprintf("%s %d→%d (drawn %v)", x.otLabel(ts), y, ay, ok))
		}
	}
	if len(moved) > 0 {
		t.Errorf("as the end loaded and the reply box came, replies moved or went under the box: %s; Slack's rows stay where they were and the box comes below them. Before: %s; after: %s",
			strings.Join(moved, ", "), x.drawnTops(before), x.drawnTops(after))
	}
	low, list := 0, tv.ListRect()
	for _, r := range tv.RowRects() {
		if r.R.Overlaps(list) && r.R.Max.Y > low {
			low = r.R.Max.Y
		}
	}
	if box := replyBoxRect(c); box.Min.Y < low {
		t.Errorf("the reply box (y %d to %d) is over the replies drawn (down to y %d)", box.Min.Y, box.Max.Y, low)
	}
}

// TestThreadsAuditReplyBoxGrowsAtTheEnd: at the thread's end a draft that
// grows a line at a time keeps the last reply drawn whole right above the
// box, the box inside the window (Slack's setHeight at the bottom sticks
// to it), and so does the box shrinking back.
func TestThreadsAuditReplyBoxGrowsAtTheEnd(t *testing.T) {
	x := openTopEnv(t)
	x.openAt(fakeslack.ThreadOpenRead)
	x.holdBox(t, "opened at its end", 300*time.Millisecond)
	c := x.a.convos[x.ch+"/"+x.root]
	if !x.threadFocused() {
		t.Fatal("the reply bar's open left the reply box unfocused")
	}
	t.Cleanup(x.clearDraft)
	h0 := replyBoxRect(c).Dy()
	for i := 1; i <= 5; i++ {
		x.typeText(fmt.Sprintf("draft line %d", i))
		x.frame()
		x.key(key.NameReturn, key.ModShift)
		x.settle(200 * time.Millisecond)
		if miss := x.boxUnderNewest(); miss != "" {
			t.Fatalf("after %d lines of draft: %s (%s)", i, miss, x.drawnTops(x.paneTops()))
		}
	}
	if h := replyBoxRect(c).Dy(); h < h0+x.px(60) {
		t.Fatalf("five lines of draft grew the box from %d px to %d px", h0, h)
	}
	x.clearDraft()
	x.holdBox(t, "the draft emptied", 500*time.Millisecond)
	if h := replyBoxRect(c).Dy(); h != h0 {
		t.Errorf("the emptied box is %d px tall, %d px before the draft", h, h0)
	}
}

// TestThreadsAuditReplyBoxTypingBringsItBack: the reader scrolls a thread
// up from its end and the reply box goes with the replies, out of view;
// typing into it (it kept the focus) brings the list back to its end, the
// box under the last reply.
func TestThreadsAuditReplyBoxTypingBringsItBack(t *testing.T) {
	x := openTopEnv(t)
	x.openAt(fakeslack.ThreadOpenRead)
	x.holdBox(t, "opened at its end", 300*time.Millisecond)
	c := x.a.convos[x.ch+"/"+x.root]
	if !x.threadFocused() {
		t.Fatal("the reply bar's open left the reply box unfocused")
	}
	paneBottom := c.compRect.Max.Y // at the thread's end the box ends at the pane's bottom
	at := x.paneWheelPoint()
	x.until("the box scrolled away with the replies", 20*time.Second, func() bool {
		if c.compRect.Min.Y >= paneBottom {
			return true
		}
		x.scroll(at, -400)
		x.settle(300 * time.Millisecond)
		return false
	})
	x.settle(500 * time.Millisecond)
	if !x.threadFocused() {
		t.Fatal("scrolled away, the reply box lost the focus")
	}
	t.Cleanup(x.clearDraft)
	x.typeText("t")
	x.frame()
	x.holdBox(t, "typed into the box out of view", 500*time.Millisecond)
	if got := x.box(x.ch, x.root).DocText(); !strings.Contains(got, "t") {
		t.Errorf("the box holds %q after the key", got)
	}
}
