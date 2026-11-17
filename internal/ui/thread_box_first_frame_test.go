package ui

// The thread pane's reply box comes with the thread's first replies
// (dogfood, α.104: clicking "13 replies" opened the pane with the last
// reply on its bottom edge and no reply box, which came only with a
// scroll). Slack's reply input is the last row of the thread's list
// (getThreadRowsAndKeys' "input" row, boot-shared eTbq; renderRow's case
// "input"; .p-threads_footer__input_container{min-height:82px;margin:12px
// 16px 0}), so it draws with the rows, and a followed, read thread opens at
// its end with the input right under the last reply (getThreadStartPosition,
// AnchorLayout's bracketScrollTop). Clack's ui root decided on the box from
// the list as the last frame left it: in the frame the replies landed the
// list still had none (HidesComposer) and drew over the whole pane, and
// nothing asked for another frame, so an idle window kept that frame.
//
// Each case opens a #th-audit thread the way its reply bar does (OpenThread,
// FocusThreadComposer) and runs frames one at a time with no input: the
// first frame that draws the newest reply must draw the box right under it
// (the composer starting where the list ends, the box's border 12px down),
// with a thread that fits top-aligned under the header, and so must the
// frame after it. That first frame laid the composer out where it wasn't
// yet (the pane's bottom edge, its height unknown), so it must ask for
// the frame after it, which lays the composer out where it is: what it
// placed in window coordinates as it laid out (its popups, its plugin
// parts) is right then, and an idle window draws that frame only if asked.

import (
	"image"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/idleprobe"
	"github.com/some-du6e/clack/internal/ui/messages"
)

func TestThreadReplyBoxWithFirstReplies(t *testing.T) {
	ch := fakeslack.ThreadsAuditChannel
	for _, tc := range []struct {
		name    string
		root    string
		loading bool // FlexpaneLoading draws before the replies land
	}{
		{"read thread", fakeslack.ThreadsAuditRead, false},
		{"read thread, loading first", fakeslack.ThreadsAuditRead, true},
		{"one reply", fakeslack.ThreadsAuditOne, false},
		{"one reply, loading first", fakeslack.ThreadsAuditOne, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := threadsAuditEnv(t, false)
			e.openChannel(ch)
			e.settle(500 * time.Millisecond)
			replyBoxFirstFrame(t, e, tc.root, tc.loading, true, 0)
		})
	}

	// The report's thread: followed and read, so it opens at its end, and
	// tall enough to fit the pane but not the pane less its box. The
	// pane is made that tall: h + half the composer.
	t.Run("fits the pane but not over the box", func(t *testing.T) {
		e := threadsAuditEnv(t, false)
		e.openChannel(ch)
		e.settle(500 * time.Millisecond)
		// the pane's bottom: where a thread taller than the pane has its box
		e.a.OpenThread(ch, fakeslack.ThreadsAuditLong)
		long := e.a.convoFor(ch, fakeslack.ThreadsAuditLong)
		e.until("the long thread's box", 15*time.Second, func() bool {
			tv := e.threadView(ch, fakeslack.ThreadsAuditLong)
			if tv == nil {
				return false
			}
			_, _, root := tv.RowText(fakeslack.ThreadsAuditLong)
			return root && tv.ContentHeight() == 0 && !replyBoxRect(long).Empty()
		})
		e.settle(300 * time.Millisecond)
		bottom := long.compRect.Max.Y
		e.a.OpenThread(ch, fakeslack.ThreadsAuditRead)
		read := e.a.convoFor(ch, fakeslack.ThreadsAuditRead)
		var tv *messages.ThreadView
		e.until("the read thread under its header", 15*time.Second, func() bool {
			tv = e.threadView(ch, fakeslack.ThreadsAuditRead)
			return tv != nil && tv.ContentHeight() > 0 && !replyBoxRect(read).Empty()
		})
		e.settle(300 * time.Millisecond)
		pane, h, compH := bottom-tv.Origin.Y, tv.ContentHeight(), read.compH
		if compH <= 0 || h+compH/2 >= pane {
			t.Fatalf("pane %d px, thread %d px, composer %d px: can't make a smaller pane the thread fits only without its box", pane, h, compH)
		}
		less := pane - (h + compH/2)
		size := image.Pt(e.size.X, e.size.Y-less)
		t.Logf("pane %d px, thread %d px, composer %d px: a %v window", pane, h, compH, size)
		e.shutdown()

		e = threadsAuditEnv(t, false)
		e.size = size
		e.openChannel(ch)
		e.settle(500 * time.Millisecond)
		replyBoxFirstFrame(t, e, fakeslack.ThreadsAuditRead, true, false, bottom-less)
	})
}

// replyBoxFirstFrame opens root's thread as its reply bar does and runs
// frames, one at a time with no input, until one draws the newest reply,
// then checks the reply box in that frame and the next. loading holds the
// replies back so FlexpaneLoading draws first (no box with it); fits is
// whether the thread fits over its box, and paneBottom, when it doesn't,
// the window y of the pane's bottom edge (0: not checked).
func replyBoxFirstFrame(t *testing.T, e *perfEnv, root string, loading, fits bool, paneBottom int) {
	t.Helper()
	ch := fakeslack.ThreadsAuditChannel
	// layoutConvo's own requests for a frame (App.Invalidate's callers)
	var asked atomic.Int32
	app := e.a
	invalidateHook = func(b *App) {
		if b == app && strings.Contains(idleprobe.Caller(1), "layoutConvo") {
			asked.Add(1)
		}
	}
	t.Cleanup(func() { invalidateHook = nil })
	if loading {
		e.fs.SetFault("conversations.replies", fakeslack.Fault{Delay: 400 * time.Millisecond, Times: 1})
	}
	e.a.OpenThread(ch, root)
	e.a.FocusThreadComposer(ch, root)
	if !loading {
		time.Sleep(300 * time.Millisecond) // the replies are in before the pane's first frame
	}
	c := e.a.convos[ch+"/"+root]
	tv := e.threadView(ch, root)
	if c == nil || tv == nil {
		t.Fatal("the reply bar's click made no thread pane")
	}
	check := func(when string) {
		t.Helper()
		bottom, ok := tv.NewestRowBottom()
		if !ok {
			t.Fatalf("%s: the newest reply isn't on screen", when)
		}
		box := replyBoxRect(c)
		if box.Empty() {
			t.Fatalf("%s: no reply box", when)
		}
		if c.compRect.Min.Y != bottom {
			t.Errorf("%s: the composer starts at y=%d, the newest reply ends at y=%d", when, c.compRect.Min.Y, bottom)
		}
		if gap := box.Min.Y - bottom; gap != slackThreadGap {
			t.Errorf("%s: %d px between the newest reply and the reply box, Slack %d", when, gap, slackThreadGap)
		}
		// what layoutConvo decided: the box under the thread (fitH, the
		// pane's header and rows), or at the pane's bottom
		if (c.fitH > 0) != fits {
			t.Errorf("%s: the box %d px down under the thread (0: at the pane's bottom), want a thread that fits over its box: %v", when, c.fitH, fits)
		}
		if fits && bottom != tv.Origin.Y+c.fitH {
			t.Errorf("%s: the newest reply ends at y=%d, top-aligned under the header it ends at %d", when, bottom, tv.Origin.Y+c.fitH)
		}
		if !fits && paneBottom > 0 && c.compRect.Max.Y != paneBottom {
			t.Errorf("%s: the composer ends at y=%d, want the pane's bottom, %d", when, c.compRect.Max.Y, paneBottom)
		}
	}
	spinner := 0
	for dl := time.Now().Add(15 * time.Second); ; {
		asked.Store(0)
		e.frame()
		if _, ok := tv.NewestRowBottom(); ok {
			break
		}
		if !replyBoxRect(c).Empty() {
			t.Fatal("the reply box drew while the thread loaded (Slack's list has no input row then)")
		}
		if time.Now().After(dl) {
			t.Fatalf("no reply drawn after %d frames", spinner)
		}
		spinner++
		time.Sleep(2 * time.Millisecond)
	}
	if loading && spinner == 0 {
		t.Error("the replies drew in the pane's first frame: FlexpaneLoading never did")
	}
	check("the frame that drew the replies")
	if asked.Load() == 0 {
		t.Error("the frame that drew the replies laid the composer out at the pane's bottom and asked for no frame to lay it out where it is")
	}
	e.frame()
	check("the frame after it")
	settled := false
	for i := 0; i < 5 && !settled; i++ {
		asked.Store(0)
		e.frame()
		settled = asked.Load() == 0
	}
	if !settled {
		t.Error("layoutConvo kept asking for frames")
	}
}
