package ui

// The focused view container, as Slack's focus manager keeps it
// (getFocusedContainer): the reply bar counts no "Unread" for the thread
// the focused secondary view shows (MessageWrapper), and Mod+F finds in
// the thread of the focused container: the split view's, and the one the
// Activity tab's own pane draws (find_in_thread). Synthetic people and
// text only.

import (
	"testing"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/messages"
)

// TestThreadsAuditReplyBarCountsNoneForTheFocusedPane: an unread thread's
// reply bar shows "Unread"; with the thread open in the side pane and the
// pane focused (its reply box) the bar counts none; the focus back in
// the channel's input, the mark is back. The window is in the background,
// so nothing is marked read meanwhile.
func TestThreadsAuditReplyBarCountsNoneForTheFocusedPane(t *testing.T) {
	e, fs := replyBarEnv(t)
	ch := fs.ChannelIDs()[2]
	fs.SetThreadsByChannel(map[string]int{ch: 1})
	replier := fs.UserIDs()[3]
	root, _ := fs.PostQuiet(ch, fakeslack.SelfID, "a synthetic question for the pane")
	e.openChannel(ch)
	cv := replyBarRows(e, ch, root)
	fs.PushFollowedReplyLive(ch, root, replier, "a synthetic answer, unread", false)
	e.until("the thread's Unread mark", 20*time.Second, func() bool { _, u, _ := cv.ReplyBarState(root); return u })

	e.a.OpenThread(ch, root)
	e.until("the thread pane with its reply box", 15*time.Second, func() bool {
		tv := e.threadView(ch, root)
		c := e.a.convos[ch+"/"+root]
		return tv != nil && len(tv.RowRects()) > 0 && c != nil && c.comp != nil
	})
	e.a.FocusThreadComposer(ch, root)
	if !e.untilSoft(5*time.Second, func() bool { _, u, _ := cv.ReplyBarState(root); return !u }) {
		t.Fatal("with the thread open in the focused side pane its reply bar still says Unread; Slack's counts none (isSecondaryViewFocused)")
	}
	e.a.focusChannelComposer(ch)
	e.until("the mark back with the channel's input focused", 5*time.Second, func() bool { _, u, _ := cv.ReplyBarState(root); return u })
}

// findFocused reports whether tv's search bar has the key focus.
func findFocused(src layout.Context, tv *messages.ThreadView) bool {
	return tv != nil && tv.FindFocused(src)
}

// TestThreadsAuditFindInTheSplitViewsThread: Mod+F with the split view's
// thread focused opens that thread's search bar (getFocusedContainer
// Split), not the channel's search.
func TestThreadsAuditFindInTheSplitViewsThread(t *testing.T) {
	x := openTopEnv(t)
	x.a.data.core.SetExperimentGroup("find_in_thread", "on")
	x.root = fakeslack.ThreadOpenRead
	x.wheelTo(x.root)
	x.a.OpenSplit("c/" + x.ch + "/t/" + x.root)
	var tv *messages.ThreadView
	x.until("the split view's thread drawn", 20*time.Second, func() bool {
		x.a.inPane(paneSplit, func() { tv = x.threadView(x.ch, x.root) })
		return x.a.splitOpen() && tv != nil && len(tv.RowRects()) > 0
	})
	x.settle(500 * time.Millisecond)
	if !x.a.split.focus {
		t.Fatal("the split view didn't take the focus")
	}
	x.key("F", key.ModShortcut)
	x.until("the split view's thread search bar focused", 3*time.Second, func() bool {
		return findFocused(layout.Context{Source: x.r.Source()}, tv)
	})
}

// TestThreadsAuditFindInTheActivityPanesThread: on the Activity tab with
// its persistent detail panel (the Primary view beside the inbox), Mod+F
// with the reply box of the thread the pane draws focused opens that
// thread's search bar (getFocusedContainer Primary on the ActivityInbox
// tab showing a Thread), not the channel's search.
func TestThreadsAuditFindInTheActivityPanesThread(t *testing.T) {
	e := newActShell(t)
	crPersistent(e)
	e.ca.SetExperimentGroup("find_in_thread", "on")
	// the fixture's thread item (fa-thread) is this thread: its messages
	// where conversations.replies serves them (the views fixture answers
	// them only to the Threads view's getView)
	ch, ts := fakeslack.ViewsChannel, fakeslack.ViewsThreadUnread
	e.fs.PostAtQuiet(ch, ts, "U0FAKE00001", "a synthetic question for the team")
	e.fs.ReplyAtQuiet(ch, ts, "1762600100.000100", fakeslack.SelfID, "a synthetic answer")
	e.fs.ReplyAtQuiet(ch, ts, "1762600200.000100", "U0FAKE00002", "a synthetic follow-up")
	cmOpenActivity(e)
	for i := 0; i < 40 && e.activity().OpenedItem() != "fa-thread"; i++ {
		e.activity().StepItem(1, false)
		e.frames(5)
	}
	if k := e.activity().OpenedItem(); k != "fa-thread" {
		t.Fatalf("stepping through the inbox never opened the thread item (at %q)", k)
	}
	var tl threadList
	e.until("the pane's thread drawn with its reply box", func() bool {
		c := e.a.convos[ch+"/"+ts]
		if c == nil {
			return false
		}
		var ok bool
		tl, ok = c.list.(threadList)
		_, box := c.comp.(composerBox)
		return ok && box && len(tl.RowRects()) > 0
	})
	if e.a.secondary.open() {
		t.Fatalf("the item opened as a secondary view (%v), not in the Activity tab's own pane", e.a.secondary.kind)
	}
	e.a.FocusThreadComposer(ch, ts)
	e.frames(5)
	src := layout.Context{Source: e.rt.Source()}
	if cb, ok := e.a.convos[ch+"/"+ts].comp.(composerBox); !ok || !src.Focused(cb.FocusTag()) {
		t.Fatal("the pane's reply box didn't take the focus")
	}
	e.key("F", key.ModShortcut)
	e.until("the pane's thread search bar focused", func() bool { return findFocused(src, tl.ThreadView) })
}
