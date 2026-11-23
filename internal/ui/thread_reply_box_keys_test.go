package ui

// Keyboard focus into a thread's reply box scrolled out of view brings it
// back, each key as Slack's thread pane does it (ThreadsFlexpane,
// boot-shared 4M8N, over the keyboard list FOmE):
//   - End in the thread list is its onKeyDownEnd: focusInput, whose
//     focusOnKey("input") scrolls to the "input" row unless the row's top
//     is in the view (BaseList isItemInView) and focuses the box (texty's
//     focus doesn't scroll: preventScroll). A slice short of the thread's
//     end has no row: the pane jumps to the latest reply (jumpToReply,
//     highlighted) and focuses the box once its row is in.
//   - ↓ past the last reply makes the "input" row the active item
//     (focusNextItem: scrollToKey unless in view), and its focus is
//     onItemFocusEnter's focusInput.
//   - Tab is the browser's: the box's editor is the next tabbable, and
//     Chromium scrolls what it focuses into view.
//
// The reply bar, the menu's "Reply…", T and the toolbar's Reply focus the
// box as its row mounts (InputContainer.componentDidMount, texty's focus
// with preventScroll; the focus manager's THREAD_MESSAGE_INPUT transition,
// which would scroll, only runs for a thread view on the client's mount
// or with its input focused before), so a thread opened at its start
// stays there (TestThreadsAuditOpenLikeSlack).
//
// The pane's scrollbar is that one scroll container's: Slack's legacy
// Scrollbar over the virtual list, its track 8px wide and 4px in from the
// list's edges, the content every row with the box's among them, the bar
// over the box and shown while the pointer is anywhere in the pane
// (.c-scrollbar:hover), so it can be dragged from there.

import (
	"fmt"
	"image"
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/messages"
)

// listFocused gives the open thread's list the keyboard focus with its
// newest reply selected (Up in the empty box with Accessibility's "Move
// focus to the message list": focusList) and waits for it.
func (x *rfEnv) listFocused(c *convo, tv *messages.ThreadView) {
	x.t.Helper()
	x.a.focusList(c)
	x.until("the thread list's keyboard focus", 5*time.Second, func() bool { return tv.KeyboardSelected() != "" })
	x.settle(200 * time.Millisecond)
}

// focusWithin runs frames until the open thread's reply box has the key
// focus, at most d, and says whether it got it.
func (x *rfEnv) focusWithin(d time.Duration) bool {
	for dl := time.Now().Add(d); ; {
		x.frame()
		if x.threadFocused() {
			return true
		}
		if time.Now().After(dl) {
			return false
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// boxAway opens the read thread at its end, gives its list the keyboard
// focus, and wheels the list up until the reply box is out of view.
func boxAway(t *testing.T) (*rfEnv, *convo, *messages.ThreadView) {
	t.Helper()
	x := openTopEnv(t)
	x.openAt(fakeslack.ThreadOpenRead)
	x.holdBox(t, "opened at its end", 300*time.Millisecond)
	c := x.a.convos[x.ch+"/"+x.root]
	tv := x.threadView(x.ch, x.root)
	x.listFocused(c, tv)
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
	if tv.KeyboardSelected() == "" || x.threadFocused() {
		t.Fatalf("scrolled away, the list has the keyboard focus %v and the box %v", tv.KeyboardSelected() != "", x.threadFocused())
	}
	return x, c, tv
}

// TestThreadsAuditReplyBoxKeysBringItBack: with the reply box scrolled out
// of view and the thread list focused, End, ↓ past the last reply and Tab
// each give the box the focus and bring the list to the thread's end, the
// box right under the last reply.
func TestThreadsAuditReplyBoxKeysBringItBack(t *testing.T) {
	for _, k := range []struct {
		what string
		key  key.Name
	}{{"End", key.NameEnd}, {"↓ past the last reply", key.NameDownArrow}, {"Tab", key.NameTab}} {
		t.Run(k.what, func(t *testing.T) {
			x, _, tv := boxAway(t)
			x.key(k.key, 0)
			if !x.focusWithin(time.Second) {
				t.Fatalf("%s from the thread list left the reply box unfocused (the list's keyboard focus: %v)", k.what, tv.KeyboardSelected() != "")
			}
			x.holdBox(t, k.what+" into the box out of view", 500*time.Millisecond)
		})
	}
}

// TestThreadsAuditReplyBoxEndKeepsItsTop: End with the box's top in view
// and its bottom cut by the pane's focuses it where it is (isItemInView
// looks at the row's top, and texty's focus doesn't scroll), while Tab,
// Chromium's focus scroll, brings the rest of the box in.
func TestThreadsAuditReplyBoxEndKeepsItsTop(t *testing.T) {
	x := openTopEnv(t)
	x.openAt(fakeslack.ThreadOpenRead)
	x.holdBox(t, "opened at its end", 300*time.Millisecond)
	c := x.a.convos[x.ch+"/"+x.root]
	tv := x.threadView(x.ch, x.root)
	paneBottom := c.compRect.Max.Y
	at := x.paneWheelPoint()
	cut := func(what string) image.Rectangle {
		x.t.Helper()
		x.listFocused(c, tv) // at the thread's end again
		x.until(what+": the box cut at the pane's bottom", 20*time.Second, func() bool {
			if c.compRect.Max.Y > paneBottom+x.px(8) {
				return true
			}
			x.scroll(at, -12)
			x.settle(200 * time.Millisecond)
			return false
		})
		x.settle(400 * time.Millisecond)
		r := c.compRect
		if r.Min.Y >= paneBottom {
			t.Fatalf("%s: the wheel took the whole box out of view (%v, the pane ends at y %d)", what, r, paneBottom)
		}
		return r
	}

	r := cut("End")
	before := x.paneTops()
	x.key(key.NameEnd, 0)
	if !x.focusWithin(time.Second) {
		t.Fatal("End left the reply box unfocused")
	}
	x.settle(300 * time.Millisecond)
	if c.compRect.Min.Y != r.Min.Y {
		t.Errorf("End moved the box's top from y %d to %d; Slack's focusOnKey leaves a row whose top shows where it is", r.Min.Y, c.compRect.Min.Y)
	}
	after := x.paneTops()
	for ts, y := range before {
		if ay, ok := after[ts]; ok && ay != y {
			t.Errorf("End moved %s from y %d to %d", x.otLabel(ts), y, ay)
			break
		}
	}

	cut("Tab")
	x.key(key.NameTab, 0)
	if !x.focusWithin(time.Second) {
		t.Fatal("Tab left the reply box unfocused")
	}
	x.holdBox(t, "Tab into the box cut at the pane's bottom", 500*time.Millisecond)
}

// TestThreadsAuditReplyBoxEndJumpsToTheEnd: End in a list whose slice
// stops short of the thread's end (its "loading" row in the box's place,
// the page past it held) jumps to the latest reply, highlighted, and the
// reply box takes the focus once its row is in, under the last reply.
func TestThreadsAuditReplyBoxEndJumpsToTheEnd(t *testing.T) {
	x := openTopEnv(t)
	x.openAt(fakeslack.ThreadOpenUnread)
	x.settle(500 * time.Millisecond)
	c := x.a.convos[x.ch+"/"+x.root]
	tv := x.threadView(x.ch, x.root)
	// the page past the slice waits while the list sits at the slice's
	// end (a poll's read may come first: the next three wait)
	x.fs.SetFault("conversations.replies", fakeslack.Fault{Delay: 4 * time.Second, Times: 3})
	t.Cleanup(func() { x.fs.SetFault("conversations.replies", fakeslack.Fault{}) })
	asked := x.fs.CallCount("conversations.replies")
	x.listFocused(c, tv) // the slice's newest selected: its end, the "loading" row
	x.until("the page past the slice asked for", 10*time.Second, func() bool { return x.fs.CallCount("conversations.replies") > asked })
	if !tv.HidesComposer() {
		t.Fatal("the reply box shows before the slice reaches the thread's end")
	}
	latest := fakeslack.ThreadOpenReply(x.root, fakeslack.ThreadOpenReplies-1)
	x.key(key.NameEnd, 0)
	flashed := false
	x.until("the reply box focused at the thread's end", 30*time.Second, func() bool {
		if tv.Highlighted() == latest {
			flashed = true
		}
		return x.threadFocused()
	})
	x.holdBox(t, "End from a slice short of the end", 500*time.Millisecond)
	if !flashed {
		t.Errorf("End's jump to the latest reply (%s) didn't highlight it, as jumpToReply does by default", x.otLabel(latest))
	}
}

// TestThreadsAuditReplyBoxScrollbar: the thread pane's scrollbar at its
// end runs over the reply box: the bar ends 4px above the pane's bottom,
// 8px wide 4px in from its right (Slack's track); it shows with the
// pointer over the box too; it is view²/content tall (at least 50px) for
// the content a drag moves (content/track px a px: onDragStart's speed),
// the box's row in it; and a drag that starts on the bar over the box
// scrolls the list, to its top and back to its end.
func TestThreadsAuditReplyBoxScrollbar(t *testing.T) {
	x := openTopEnv(t)
	x.openAt(fakeslack.ThreadOpenRead)
	x.holdBox(t, "opened at its end", 300*time.Millisecond)
	c := x.a.convos[x.ch+"/"+x.root]
	tv := x.threadView(x.ch, x.root)
	list, box := tv.ListRect(), replyBoxRect(c)
	inset, w := x.px(4), x.px(8)

	// the pointer over the replies, past the 1.2 s a scroll shows it for
	x.movePtr(x.paneWheelPoint())
	x.settle(1400 * time.Millisecond)
	th := tv.ScrollThumb()
	if th.Empty() {
		t.Fatal("no scrollbar with the pointer over the thread")
	}
	if th.Max.X != list.Max.X-inset || th.Dx() != w {
		t.Errorf("the bar is x %d to %d; Slack's track is %d px wide, %d px in from the pane's right (x %d to %d)",
			th.Min.X, th.Max.X, w, inset, list.Max.X-inset-w, list.Max.X-inset)
	}
	if th.Max.Y != list.Max.Y-inset {
		t.Errorf("at the thread's end the bar ends at y %d; Slack's track ends %d px above the pane's bottom (y %d)", th.Max.Y, inset, list.Max.Y-inset)
	}
	on := th.Intersect(box)
	if on.Empty() {
		t.Fatalf("at the thread's end the bar (%v) isn't over the reply box (%v)", th, box)
	}

	// the pointer over the box, clear of the bar: still shown
	x.movePtr(image.Pt(box.Min.X+box.Dx()/3, (box.Min.Y+box.Max.Y)/2))
	x.settle(1400 * time.Millisecond)
	if tv.ScrollThumb().Empty() {
		t.Error("the pointer over the reply box hides the bar; Slack's shows over the whole scroll container (.c-scrollbar:hover)")
	}

	// a drag from the bar where it is over the box
	grab := image.Pt((on.Min.X+on.Max.X)/2, (on.Min.Y+on.Max.Y)/2)
	before := x.paneTops()
	x.rzPress(grab)
	dy := x.px(20)
	x.rzMove(grab.Sub(image.Pt(0, dy)))
	x.frame()
	after := x.paneTops()
	shift, moved := 0, ""
	for ts, y := range before {
		if ay, ok := after[ts]; ok {
			shift, moved = ay-y, ts
			break
		}
	}
	if shift <= 0 {
		x.rzRelease(grab)
		t.Fatalf("a %d px drag up from the bar over the box moved the replies %d px (%s); it scrolls the list in Slack's (one scroll container)", dy, shift, x.otLabel(moved))
	}
	track := list.Dy() - 2*inset
	content := shift * track / dy
	want := max(x.px(50), list.Dy()*list.Dy()/content)
	if d := th.Dy() - want; d < -2 || d > 2 {
		t.Errorf("the bar is %d px tall; Slack's view²/content (at least 50 px) is %d for the %d px of content a drag moves (%d px for %d)", th.Dy(), want, content, shift, dy)
	}

	// on past the track's top: the thread's top
	x.rzMove(image.Pt(grab.X, list.Min.Y-x.px(400)))
	x.frame()
	x.settle(300 * time.Millisecond)
	if _, ok := x.paneTops()[x.root]; !ok {
		t.Errorf("the bar dragged past the track's top, the root isn't drawn (%s)", x.drawnTops(x.paneTops()))
	}
	if th := tv.ScrollThumb(); th.Min.Y != list.Min.Y+inset {
		t.Errorf("at the thread's top the bar starts at y %d; Slack's track starts %d px under the pane's top (y %d)", th.Min.Y, inset, list.Min.Y+inset)
	}
	// and back past the bottom: the end, the box under the last reply
	x.rzMove(image.Pt(grab.X, list.Max.Y+x.px(400)))
	x.frame()
	x.rzRelease(image.Pt(grab.X, list.Max.Y+x.px(400)))
	x.settle(300 * time.Millisecond)
	x.holdBox(t, fmt.Sprintf("the bar dragged back past the bottom (y %d)", list.Max.Y), 500*time.Millisecond)
}

// TestThreadsAuditReplyBoxInSplitView: a thread in the split view is Slack's same
// ThreadsFlexpane (client-thread-view renders it in the split container),
// so its reply box is its list's last row there too: right under the last
// reply at the thread's end, scrolled away with the replies, and back
// with End from its list.
func TestThreadsAuditReplyBoxInSplitView(t *testing.T) {
	x := openTopEnv(t)
	x.root = fakeslack.ThreadOpenRead
	x.a.OpenSplit("c/" + x.ch + "/t/" + x.root)
	x.until("the split view's thread drawn", 20*time.Second, func() bool {
		return x.a.splitOpen() && len(x.paneTops()) > 0
	})
	x.holdBox(t, "the split view's thread at its end", 500*time.Millisecond)
	c := x.a.convos[x.ch+"/"+x.root]
	tv := x.threadView(x.ch, x.root)
	if l := tv.ListRect(); !l.In(x.a.split.card) {
		t.Fatalf("the thread's list (%v) isn't in the split view's card (%v)", l, x.a.split.card)
	}
	x.listFocused(c, tv)
	paneBottom := c.compRect.Max.Y
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
	x.key(key.NameEnd, 0)
	if !x.focusWithin(time.Second) {
		t.Fatal("End from the split view's thread list left its reply box unfocused")
	}
	x.holdBox(t, "End in the split view's thread", 500*time.Millisecond)
}
