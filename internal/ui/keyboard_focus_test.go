package ui

// Keyboard focus as Slack's client moves it, through the real panes on
// fakeslack:
//   - typing with nothing focused goes into the channel's message input,
//     as MessagePaneInput's document keydown does (shouldInputStealKeyEvent:
//     not Space; Mod+V too), and a key typed with a message selected in a
//     channel's or a thread's list goes into that conversation's input,
//     the selected message's own keys excepted;
//   - Escape in the thread pane: from its list's keyboard focus it marks
//     the thread read, then closes the pane (ThreadsFlexpane onEsc, the
//     view's close), and an open autocomplete or an inline edit keep it
//     (closed, cancelled), the edit's end giving the list the focus at its
//     message in keyboard mode (stopEditingMessage);
//   - a wheel that scrolls the selected message away drops the list's
//     keyboard focus (BaseKeyboardNavigableList onScroll after onWheel);
//   - a thread opened in keyboard mode gives its list the focus (the
//     navigation's THREAD_MESSAGE_LIST), and one opened after a click its
//     reply box. Keyboard mode comes on as Slack's does: an arrow (↑ in an
//     empty input) moving the focus to the list.

import (
	"image"
	"io"
	"strings"
	"testing"
	"time"

	"gioui.org/io/key"
	"gioui.org/io/transfer"
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/shortcut"
)

// kfBoxFocused reports whether c's composer has the key focus.
func (e *perfEnv) kfBoxFocused(c *convo) bool {
	if c == nil || c.comp == nil {
		return false
	}
	cb, ok := c.comp.(composerBox)
	return ok && e.r.Source().Focused(cb.FocusTag())
}

// kfNothingFocused takes the key focus off everything (a browser's focus
// on the page body) and fails unless it stays off.
func (e *perfEnv) kfNothingFocused(t *testing.T) {
	t.Helper()
	e.r.Source().Execute(key.FocusCmd{})
	e.settle(200 * time.Millisecond)
	if !e.r.Source().Focused(nil) {
		t.Fatal("the key focus came back to something")
	}
}

// kfClearDraft empties c's composer (composer drafts are process-wide).
func (e *perfEnv) kfClearDraft(c *convo) {
	if c == nil || c.comp == nil {
		return
	}
	c.comp.Focus()
	e.settle(100 * time.Millisecond)
	e.key("A", shortcut.Mod())
	e.key(key.NameDeleteBackward, 0)
	e.settle(150 * time.Millisecond)
}

// kfListFocused reports whether a thread's list has the key focus.
func (e *perfEnv) kfListFocused(tv *messages.ThreadView) bool {
	return tv != nil && tv.ListKeyFocus(layout.Context{Source: e.r.Source()})
}

// TestThreadsAuditTypingGoesToTheInput: with nothing focused, Space stays
// nobody's, a letter goes into the channel's message input with the focus,
// and Mod+V pastes there.
func TestThreadsAuditTypingGoesToTheInput(t *testing.T) {
	x := openTopEnv(t)
	c := x.a.convos[x.ch+"/"]
	box := x.box(x.ch, "")
	t.Cleanup(func() { x.kfClearDraft(c) })
	x.kfNothingFocused(t)
	before := box.DocText()
	x.typeText(" ")
	x.settle(200 * time.Millisecond)
	if got := box.DocText(); got != before || !x.r.Source().Focused(nil) {
		t.Fatalf("Space with nothing focused: the input went from %q to %q, nothing focused %v; Slack's input takes no Space", before, got, x.r.Source().Focused(nil))
	}
	x.typeText("h")
	x.until("the input focused by the typing", 2*time.Second, func() bool { return x.kfBoxFocused(c) })
	if got, want := strings.TrimSpace(box.DocText()), strings.TrimSpace(before)+"h"; got != want {
		t.Fatalf("typed h with nothing focused: the channel's input holds %q, want %q", got, want)
	}

	x.kfClearDraft(c)
	x.kfNothingFocused(t)
	x.key("V", key.ModShortcut)
	x.until("Mod+V asking for the clipboard", 2*time.Second, x.r.ClipboardRequested)
	x.r.Queue(transfer.DataEvent{Type: "application/text", Open: func() io.ReadCloser { return io.NopCloser(strings.NewReader("synthetic pasted words")) }})
	x.until("the paste in the channel's input", 2*time.Second, func() bool {
		return strings.Contains(box.DocText(), "synthetic pasted words") && x.kfBoxFocused(c)
	})
}

// TestThreadsAuditTypingFromTheList: a message selected in the channel's
// list keeps its own keys (l: copy link, which any ordinary message binds;
// messages/kbnav.go) but hands a letter to the channel's input; one
// selected in a thread's list hands it to the reply box.
func TestThreadsAuditTypingFromTheList(t *testing.T) {
	x := openTopEnv(t)
	c := x.a.convos[x.ch+"/"]
	box := x.box(x.ch, "")
	t.Cleanup(func() { x.kfClearDraft(c) })
	x.a.kbRelated = kbRelNav // as ↑ from the input moves there: keyboard mode, which the message keys need
	x.a.focusList(c)
	x.until("the channel's list selecting its newest message", 5*time.Second, func() bool { return x.cv().KeyboardSelected() != "" })
	x.until("keyboard mode", 2*time.Second, func() bool { return x.a.KeyboardMode() })
	before := box.DocText()
	x.typeText("l")
	x.settle(200 * time.Millisecond)
	if got := box.DocText(); got != before || x.cv().KeyboardSelected() == "" {
		t.Fatalf("l on a selected message (its own key): the input went from %q to %q, the list selects %q", before, got, x.cv().KeyboardSelected())
	}
	x.typeText("k")
	x.until("the channel's input focused by the typing", 2*time.Second, func() bool { return x.kfBoxFocused(c) })
	if got, want := strings.TrimSpace(box.DocText()), strings.TrimSpace(before)+"k"; got != want {
		t.Fatalf("typed k with a message selected: the channel's input holds %q, want %q", got, want)
	}

	x.root = fakeslack.ThreadOpenRead
	x.wheelTo(x.root)
	x.click(x.replyBar())
	x.until("the thread drawn", 20*time.Second, func() bool {
		tv := x.threadView(x.ch, x.root)
		return tv != nil && tv.HasMessages() && x.threadFocused()
	})
	tc := x.a.convos[x.ch+"/"+x.root]
	t.Cleanup(func() { x.kfClearDraft(tc) })
	tv := x.threadView(x.ch, x.root)
	x.a.focusList(tc)
	x.until("the thread's list selecting a reply", 5*time.Second, func() bool { return x.kfListFocused(tv) })
	tbefore := x.box(x.ch, x.root).DocText()
	x.typeText("q")
	x.until("the reply box focused by the typing", 2*time.Second, func() bool { return x.kfBoxFocused(tc) })
	if got, want := strings.TrimSpace(x.box(x.ch, x.root).DocText()), strings.TrimSpace(tbefore)+"q"; got != want {
		t.Fatalf("typed q with a reply selected: the reply box holds %q, want %q", got, want)
	}
}

// TestThreadsAuditEscapeInThePane: Escape with the thread list's keyboard
// focus marks the thread read (the pane stays), the next closes the pane;
// with the reply box's autocomplete open it closes the list only; in an
// inline edit it cancels the edit, and in keyboard mode the list takes the
// focus at the message. Neither of the last two marks anything read.
func TestThreadsAuditEscapeInThePane(t *testing.T) {
	e := threadsAuditEnv(t, false)
	ch := fakeslack.ThreadsAuditChannel
	e.openChannel(ch)
	open := func(root string) (*convo, *messages.ThreadView) {
		t.Helper()
		e.a.OpenThread(ch, root)
		e.until("the thread", 15*time.Second, func() bool {
			tv := e.threadView(ch, root)
			return tv != nil && tv.ContentHeight() > 0
		})
		e.settle(300 * time.Millisecond)
		return e.a.convos[ch+"/"+root], e.threadView(ch, root)
	}

	// the list's keyboard focus: the view's Escape
	root := fakeslack.ThreadsAuditAllUnread
	c, tv := open(root)
	e.a.focusList(c)
	e.until("the thread's list focused", 5*time.Second, func() bool { return e.kfListFocused(tv) })
	marks, channelMarks := len(e.fs.ThreadsAuditMarks()), e.fs.CallCount("conversations.mark")
	e.key(key.NameEscape, 0)
	e.until("Escape from the list marks the thread read", 10*time.Second, func() bool { return len(e.fs.ThreadsAuditMarks()) == marks+1 })
	if e.a.secondary.kind != secThread {
		t.Fatal("the first Escape from the list closed the pane; Slack marks the thread read first")
	}
	if n := e.fs.CallCount("conversations.mark"); n != channelMarks {
		t.Errorf("Escape from the thread's list marked the channel read (%d conversations.mark)", n-channelMarks)
	}
	e.settle(500 * time.Millisecond)
	e.key(key.NameEscape, 0)
	e.until("the second Escape closes the pane", 5*time.Second, func() bool { return e.a.secondary.kind == secNone })

	// the reply box's autocomplete: closes, nothing else
	root = fakeslack.ThreadsAuditRead
	c, _ = open(root)
	e.a.FocusThreadComposer(ch, root)
	e.settle(300 * time.Millisecond)
	cb, _ := c.comp.(composerBox)
	t.Cleanup(func() { e.kfClearDraft(c) })
	e.typeText(":sm")
	e.until("the emoji autocomplete open", 5*time.Second, cb.PopupOpen)
	marks, channelMarks = len(e.fs.ThreadsAuditMarks()), e.fs.CallCount("conversations.mark")
	e.key(key.NameEscape, 0)
	e.settle(400 * time.Millisecond)
	if cb.PopupOpen() {
		t.Error("Escape left the reply box's autocomplete open")
	}
	if e.a.secondary.kind != secThread {
		t.Error("Escape with the autocomplete open closed the pane")
	}
	if n, m := len(e.fs.ThreadsAuditMarks()), e.fs.CallCount("conversations.mark"); n != marks || m != channelMarks {
		t.Errorf("Escape with the autocomplete open marked %d threads and %d channels read", n-marks, m-channelMarks)
	}
	e.kfClearDraft(c)

	// an inline edit of the viewer's reply: cancelled; in keyboard mode
	// the list then has the focus at it
	root = fakeslack.ThreadsAuditOwnFirst
	mine := fakeslack.ThreadsAuditReply(root, 0)
	c, tv = open(root)
	e.a.FocusThreadComposer(ch, root)
	e.settle(300 * time.Millisecond)
	e.kfUpToList(t, func() bool { return e.kfListFocused(tv) }) // keyboard mode: ↑ moves the focus to the list
	tv.Edit(mine)
	e.until("the inline edit open", 5*time.Second, func() bool { return tv.EditComposer() != nil })
	e.settle(300 * time.Millisecond)
	marks, channelMarks = len(e.fs.ThreadsAuditMarks()), e.fs.CallCount("conversations.mark")
	e.key(key.NameEscape, 0)
	e.until("Escape cancels the inline edit", 3*time.Second, func() bool { return tv.EditComposer() == nil })
	if e.a.secondary.kind != secThread {
		t.Error("Escape in the inline edit closed the pane")
	}
	if n, m := len(e.fs.ThreadsAuditMarks()), e.fs.CallCount("conversations.mark"); n != marks || m != channelMarks {
		t.Errorf("Escape in the inline edit marked %d threads and %d channels read", n-marks, m-channelMarks)
	}
	e.until("the list focused at the edited reply (keyboard mode)", 3*time.Second, func() bool { return tv.KeyboardSelected() == mine && e.kfListFocused(tv) })
}

// TestThreadsAuditWheelDropsTheListFocus: the wheel scrolling the
// selected message out of the channel's list drops the list's keyboard
// focus, and a letter typed then goes to the channel's input.
func TestThreadsAuditWheelDropsTheListFocus(t *testing.T) {
	x := openTopEnv(t)
	c := x.a.convos[x.ch+"/"]
	t.Cleanup(func() { x.kfClearDraft(c) })
	x.a.focusList(c)
	x.until("the channel's list selecting its newest message", 5*time.Second, func() bool { return x.cv().KeyboardSelected() != "" })
	sel := x.cv().KeyboardSelected()
	r := x.cv().ListRect()
	at := image.Pt((r.Min.X+r.Max.X)/2, (r.Min.Y+r.Max.Y)/2)
	x.until("the wheel took the selected message out of view", 20*time.Second, func() bool {
		if _, ok := x.row(sel); !ok {
			return true
		}
		x.scroll(at, -400)
		x.settle(300 * time.Millisecond)
		return false
	})
	x.settle(400 * time.Millisecond)
	if s := x.cv().KeyboardSelected(); s != "" {
		t.Fatalf("the wheel scrolled the selected message away and the list keeps the keyboard focus on %s; Slack's blurs it", s)
	}
	before := strings.TrimSpace(x.box(x.ch, "").DocText())
	x.typeText("w")
	x.until("the letter in the channel's input", 2*time.Second, func() bool {
		return strings.TrimSpace(x.box(x.ch, "").DocText()) == before+"w" && x.kfBoxFocused(c)
	})
}

// TestThreadsAuditKeyboardModeOpensToTheList: a thread opened in keyboard
// mode (an arrow pressed, no click since) gives its list the focus with a
// message selected, not its reply box; one opened after a click focuses
// its box (focusInputOnMount).
func TestThreadsAuditKeyboardModeOpensToTheList(t *testing.T) {
	x := openTopEnv(t)
	// a click first: the reply bar's open focuses the box
	x.root = fakeslack.ThreadOpenRead
	x.wheelTo(x.root)
	x.click(x.replyBar())
	x.until("the thread with its reply box focused", 20*time.Second, x.threadFocused)
	x.a.CloseSecondary()
	x.settle(300 * time.Millisecond)

	// keyboard mode (↑ moving the focus from the empty input to the list):
	// the menu's Reply in thread (setFocusInput, then the thread opens)
	// focuses the thread's list
	x.kfUpToList(t, func() bool { return x.cv().KeyboardSelected() != "" })
	x.a.SetThreadFocusInput(x.ch, x.root)
	x.a.OpenThread(x.ch, x.root)
	var tv *messages.ThreadView
	x.until("the thread drawn", 20*time.Second, func() bool {
		tv = x.threadView(x.ch, x.root)
		return tv != nil && tv.HasMessages()
	})
	x.settle(500 * time.Millisecond)
	if !x.kfListFocused(tv) || x.threadFocused() {
		t.Fatalf("opened in keyboard mode, the thread's list has the focus %v and its reply box %v; Slack's navigation focuses the list (THREAD_MESSAGE_LIST)", x.kfListFocused(tv), x.threadFocused())
	}
	if tv.KeyboardSelected() == "" {
		t.Error("the thread's list has the focus with no message selected")
	}
}
