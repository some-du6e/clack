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
// frame after it.

import (
	"image"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
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
			replyBoxFirstFrame(t, e, tc.root, tc.loading, true)
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
		size := image.Pt(e.size.X, e.size.Y-(pane-(h+compH/2)))
		t.Logf("pane %d px, thread %d px, composer %d px: a %v window", pane, h, compH, size)
		e.shutdown()

		e = threadsAuditEnv(t, false)
		e.size = size
		e.openChannel(ch)
		e.settle(500 * time.Millisecond)
		replyBoxFirstFrame(t, e, fakeslack.ThreadsAuditRead, true, false)
	})
}

// replyBoxFirstFrame opens root's thread as its reply bar does and runs
// frames, one at a time with no input, until one draws the newest reply,
// then checks the reply box in that frame and the next. loading holds the
// replies back so FlexpaneLoading draws first (no box with it); fits is
// whether the thread fits over its box.
func replyBoxFirstFrame(t *testing.T, e *perfEnv, root string, loading, fits bool) {
	t.Helper()
	ch := fakeslack.ThreadsAuditChannel
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
		h := tv.ContentHeight()
		if (h > 0) != fits {
			t.Errorf("%s: content height %d, want a thread that fits over its box: %v", when, h, fits)
		}
		if fits && bottom != tv.Origin.Y+h {
			t.Errorf("%s: the newest reply ends at y=%d, top-aligned under the header it ends at %d", when, bottom, tv.Origin.Y+h)
		}
	}
	spinner := 0
	for dl := time.Now().Add(15 * time.Second); ; {
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
	e.frame()
	check("the frame after it")
}
