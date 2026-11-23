package ui

// The keyboard focus round's review (keyboard-focus-fixes): each through
// the real panes on fakeslack, synthetic content only.
//   - E on the viewer's own selected message opens its edit with the
//     message's text alone, in each OS's order of the key and its text;
//   - typing on a channel's Files tab goes nowhere, not into the hidden
//     Messages input;
//   - Activity's option+down keeps the focus in the inbox, and a thread
//     opened with Enter hands it back when it closes;
//   - keyboard mode comes from a Tab, F6 or arrow moving the focus to
//     something that isn't a text field, not a caret arrow;
//   - two keys in one frame land in order at the caret of a draft.

import (
	"io"
	"strings"
	"testing"
	"time"

	"gioui.org/io/key"
	"gioui.org/io/transfer"
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/messages"
)

// kfKeyOrder is the order a platform sends a printable key's events in.
type kfKeyOrder string

const (
	kfWindows kfKeyOrder = "Windows" // WM_KEYDOWN, then WM_CHAR, then WM_KEYUP, in one frame
	kfX11     kfKeyOrder = "X11"     // xkb's key, then its text; the release in the next frame
	kfMacOS   kfKeyOrder = "macOS"   // interpretKeyEvents' text, then the key
)

// kfPressTyped sends key name with its character text in order o, at the
// router's selection as the window's input method does.
func (e *perfEnv) kfPressTyped(name key.Name, text string, o kfKeyOrder) {
	press := key.Event{Name: name, State: key.Press}
	release := key.Event{Name: name, State: key.Release}
	edit := key.EditEvent{Range: e.r.EditorState().Selection.Range, Text: text}
	switch o {
	case kfWindows:
		e.r.Queue(press, edit, release)
	case kfX11:
		e.r.Queue(press, edit)
		e.frame()
		e.r.Queue(release)
	case kfMacOS:
		e.r.Queue(edit, press, release)
	}
}

// kfUpToList gives the focused empty input's list the focus with ↑, as
// Accessibility's "Move focus to the message list, only if the message
// field is empty" does: an arrow moving the focus to the list, which is
// keyboard mode's way on. listFocused says when the list has it.
func (e *perfEnv) kfUpToList(t *testing.T, listFocused func() bool) {
	t.Helper()
	was := e.a.prefs.ComposerUpArrow
	e.a.prefs.ComposerUpArrow = 2
	t.Cleanup(func() { e.a.prefs.ComposerUpArrow = was })
	e.key(key.NameUpArrow, 0)
	e.until("↑ in the empty input focusing the list", 5*time.Second, listFocused)
	e.settle(200 * time.Millisecond)
	if !e.a.KeyboardMode() {
		t.Fatal("↑ moved the focus from the input to the list and keyboard mode stayed off; Slack's FocusEventManager turns it on")
	}
}

// kfSelectReply gives thread root's list the keyboard focus at reply ts:
// ↑ from its empty reply box (kfUpToList), then ↑ to the reply.
func (e *perfEnv) kfSelectReply(t *testing.T, ch, root, ts string, tv *messages.ThreadView) {
	t.Helper()
	e.a.FocusThreadComposer(ch, root)
	e.settle(300 * time.Millisecond)
	e.kfUpToList(t, func() bool { return e.kfListFocused(tv) })
	for i := 0; i < 40 && tv.KeyboardSelected() != ts; i++ {
		e.key(key.NameUpArrow, 0)
		e.settle(50 * time.Millisecond)
	}
	if tv.KeyboardSelected() != ts {
		t.Fatalf("↑ never selected the reply (at %q)", tv.KeyboardSelected())
	}
}

// TestThreadsAuditEditKeyOpensTheEditClean: E on the viewer's own reply
// selected in a thread's list opens its inline edit holding the reply's
// text alone, in Windows' order of the key and its character, X11's and
// macOS'. On Windows and X11 the key came first: the list took it and
// dropped the focus to open the edit, the character was handed to the
// window's typing redirect on the drop's replay, and the edit opening in
// that frame took it ("e" + the text, which Enter then saved to Slack).
func TestThreadsAuditEditKeyOpensTheEditClean(t *testing.T) {
	e := threadsAuditEnv(t, false)
	ch := fakeslack.ThreadsAuditChannel
	root := fakeslack.ThreadsAuditOwnFirst
	mine := fakeslack.ThreadsAuditReply(root, 0)
	e.openChannel(ch)
	e.a.OpenThread(ch, root)
	e.until("the thread", 15*time.Second, func() bool {
		tv := e.threadView(ch, root)
		return tv != nil && tv.ContentHeight() > 0
	})
	c, tv := e.a.convos[ch+"/"+root], e.threadView(ch, root)
	t.Cleanup(func() { e.kfClearDraft(c) })
	// the edit's text as a click's Edit opens it
	tv.Edit(mine)
	e.until("the edit opened by Edit", 5*time.Second, func() bool { return tv.EditComposer() != nil })
	e.settle(300 * time.Millisecond)
	want := strings.TrimSpace(tv.EditComposer().DocText())
	e.key(key.NameEscape, 0)
	e.until("the edit cancelled", 5*time.Second, func() bool { return tv.EditComposer() == nil })
	if want == "" {
		t.Fatal("the viewer's reply opened an empty edit")
	}
	for _, o := range []kfKeyOrder{kfWindows, kfX11, kfMacOS} {
		e.kfSelectReply(t, ch, root, mine, tv)
		e.kfPressTyped("E", "e", o)
		e.until(string(o)+": the inline edit open", 5*time.Second, func() bool { return tv.EditComposer() != nil })
		e.settle(300 * time.Millisecond)
		if got := strings.TrimSpace(tv.EditComposer().DocText()); got != want {
			t.Fatalf("%s order: E opened the edit holding %q, want the reply's text %q alone", o, got, want)
		}
		e.key(key.NameEscape, 0)
		e.until(string(o)+": the edit cancelled", 5*time.Second, func() bool { return tv.EditComposer() == nil })
		e.settle(200 * time.Millisecond)
	}
}

// TestThreadsAuditTypingOnAChannelTab: on a channel's Files & links tab,
// where Slack draws no message input, typing with nothing focused goes
// nowhere, and Mod+V pastes nothing into the Messages input later. The
// letters went into the hidden input's draft, and the shortcut waited in
// it for the next focus, pasting whatever the clipboard held then.
func TestThreadsAuditTypingOnAChannelTab(t *testing.T) {
	sz := fakeslack.Small
	sz.Files = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	ch := fakeslack.FilesChannelID
	e.openChannel(ch)
	e.until("the channel's tabs and input", 10*time.Second, func() bool {
		c := e.a.convos[ch+"/"]
		return e.a.data.tabsDone[ch] && c != nil && c.comp != nil
	})
	c := e.a.convos[ch+"/"]
	t.Cleanup(func() { e.kfClearDraft(c) })
	box, _ := c.comp.(composerBox)
	before := box.DocText()
	idx := -1
	tabs := e.a.channelTabs(ch)
	for i, tb := range tabs {
		if i > 0 && isFilesTab(tb) {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatal("no Files & links tab")
	}
	e.a.selectChannelTab(ch, idx, tabs[idx])
	e.until("the Files & links view", 5*time.Second, func() bool { return e.a.filesTabView(ch) != nil })
	e.kfNothingFocused(t)
	for _, s := range []string{"t", "e", "s", "t"} {
		e.typeText(s)
		e.frame()
	}
	e.key("V", key.ModShortcut)
	e.settle(400 * time.Millisecond)
	if got := box.DocText(); got != before {
		t.Fatalf("typed on the Files tab with nothing focused: the hidden Messages input went from %q to %q; Slack draws no input there", before, got)
	}
	e.a.chTab[ch] = 0
	e.until("Messages again", 5*time.Second, func() bool { return e.a.filesTabView(ch) == nil })
	c.comp.Focus()
	e.settle(400 * time.Millisecond)
	if e.r.ClipboardRequested() {
		e.r.Queue(transfer.DataEvent{Type: "application/text", Open: func() io.ReadCloser { return io.NopCloser(strings.NewReader("a later clipboard")) }})
		e.settle(300 * time.Millisecond)
		t.Fatalf("Mod+V pressed on the Files tab pasted into the input once it had the focus (now %q)", box.DocText())
	}
}

// TestThreadsAuditActivityKeysStayInTheInbox: option+down onto a thread
// item opens it beside the inbox and leaves the keys to the inbox
// (preventMoveFocus): X picks the row instead of going into the thread's
// reply box, Escape closes the item. Enter opens it with the focus in the
// thread (keyboard mode: its list), and once Escape has closed it the
// inbox has the keys again (onViewClosed).
func TestThreadsAuditActivityKeysStayInTheInbox(t *testing.T) {
	e := newActShell(t)
	ch, ts := fakeslack.ViewsChannel, fakeslack.ViewsThreadUnread
	e.fs.PostAtQuiet(ch, ts, "U0FAKE00001", "a synthetic question for the team")
	e.fs.ReplyAtQuiet(ch, ts, "1762600100.000100", fakeslack.SelfID, "a synthetic answer")
	e.fs.ReplyAtQuiet(ch, ts, "1762600200.000100", "U0FAKE00002", "a synthetic follow-up")
	cmOpenActivity(e)
	e.openRow() // a click: the inbox has the focus
	for i := 0; i < 40 && e.activity().OpenedItem() != "fa-thread"; i++ {
		e.key(key.NameDownArrow, key.ModAlt)
		e.frames(4)
	}
	if k := e.activity().OpenedItem(); k != "fa-thread" {
		t.Fatalf("option+down never opened the thread item (at %q)", k)
	}
	box := func() string {
		if c := e.a.convos[ch+"/"+ts]; c != nil {
			if cb, ok := c.comp.(composerBox); ok {
				return cb.DocText()
			}
		}
		return ""
	}
	e.until("the thread beside the inbox", func() bool {
		c := e.a.convos[ch+"/"+ts]
		if c == nil || !e.a.secondary.open() || e.a.secondary.kind != secThread {
			return false
		}
		tl, ok := c.list.(threadList)
		return ok && tl.HasMessages()
	})
	e.frames(20)
	before := box()
	e.rt.Queue(key.Event{Name: "X", State: key.Press}, key.EditEvent{Text: "x"}, key.Event{Name: "X", State: key.Release})
	e.frames(10)
	if got := box(); got != before {
		t.Fatalf("X after option+down onto a thread item went into the thread's reply box (%q, was %q); Slack's open keeps the keys in the inbox (preventMoveFocus)", got, before)
	}
	e.key(key.NameEscape, 0)
	e.frames(10)
	if e.a.secondary.open() {
		t.Fatal("Escape after option+down left the thread item open; with the keys in the inbox it closes the item")
	}

	// Enter: the thread takes the keys, and gives them back as it closes
	e.key(key.NameReturn, 0)
	e.until("Enter's thread with its list focused", func() bool {
		return e.a.secondary.open() && e.a.threadListFocused(layout.Context{Source: e.rt.Source()})
	})
	for i := 0; i < 3 && e.a.secondary.open(); i++ {
		e.key(key.NameEscape, 0)
		e.frames(10)
	}
	if e.a.secondary.open() {
		t.Fatal("three Escapes left Enter's thread open")
	}
	e.key(key.NameUpArrow, 0)
	e.key(key.NameReturn, 0)
	e.until("↑ and Enter in the inbox after the thread closed", func() bool {
		k := e.activity().OpenedItem()
		return k != "" && k != "fa-thread"
	})
}

// TestThreadsAuditKeyboardModeOnlyFromNavigation: a caret arrow in the
// message input turns no keyboard mode on, so a thread opened after it
// without a click (a notification's) leaves the focus in the input, where
// the next letters go; ↑ moving the focus to the list turns it on.
func TestThreadsAuditKeyboardModeOnlyFromNavigation(t *testing.T) {
	x := openTopEnv(t)
	c := x.a.convos[x.ch+"/"]
	box := x.box(x.ch, "")
	t.Cleanup(func() { x.kfClearDraft(c) })
	c.comp.Focus()
	x.settle(200 * time.Millisecond)
	before := box.DocText()
	x.typeText("on it")
	x.settle(100 * time.Millisecond)
	x.key(key.NameLeftArrow, 0) // the caret, in the input
	x.settle(200 * time.Millisecond)
	if x.a.KeyboardMode() {
		t.Fatal("← moving the caret in the message input turned keyboard mode on; Slack's comes only from a focus moved to something that isn't an input")
	}
	x.root = fakeslack.ThreadOpenRead
	x.wheelTo(x.root)
	x.a.OpenThread(x.ch, x.root) // as a notification's click: no press in the window
	x.until("the thread drawn", 20*time.Second, func() bool {
		tv := x.threadView(x.ch, x.root)
		return tv != nil && tv.HasMessages()
	})
	x.settle(500 * time.Millisecond)
	if !x.kfBoxFocused(c) {
		t.Fatal("the thread opened after a caret arrow took the focus out of the message input")
	}
	x.typeText("!")
	x.settle(200 * time.Millisecond)
	if got := box.DocText(); !strings.Contains(got, "!") || !strings.HasPrefix(got, before) {
		t.Fatalf("the letter typed next went elsewhere: the input holds %q", got)
	}
	x.a.CloseSecondary()
	x.kfClearDraft(c)
	c.comp.Focus()
	x.settle(200 * time.Millisecond)
	x.kfUpToList(t, func() bool { return x.cv().KeyboardSelected() != "" })
}

// TestThreadsAuditTwoKeysInOneFrame: two keys typed before the frame that
// redirects them land in order at the caret of a draft, from nothing
// focused and from the channel's list. Their EditEvents carry the
// window's ranges for its empty input model, {0,0} then {1,1}; the
// second was replayed into the input with its range and went in at its
// offset ("hello" + "a", "b" became "hbelloa").
func TestThreadsAuditTwoKeysInOneFrame(t *testing.T) {
	x := openTopEnv(t)
	c := x.a.convos[x.ch+"/"]
	box := x.box(x.ch, "")
	t.Cleanup(func() { x.kfClearDraft(c) })
	for _, from := range []string{"nothing focused", "the list"} {
		x.kfClearDraft(c)
		box.InsertText("hello")
		x.settle(200 * time.Millisecond)
		before := box.DocText()
		if from == "the list" {
			x.a.focusList(c)
			x.until("the channel's list focused", 5*time.Second, func() bool { return x.cv().KeyboardSelected() != "" })
		} else {
			x.kfNothingFocused(t)
		}
		// k and w: not a selected message's own letters (msgActionKeys)
		x.r.Queue(key.EditEvent{Range: key.Range{Start: 0, End: 0}, Text: "k"}, key.EditEvent{Range: key.Range{Start: 1, End: 1}, Text: "w"})
		x.until(from+": the input focused by the typing", 2*time.Second, func() bool { return x.kfBoxFocused(c) })
		x.settle(200 * time.Millisecond)
		if got, want := strings.TrimSpace(box.DocText()), strings.TrimSpace(before)+"kw"; got != want {
			t.Fatalf("from %s, k and w in one frame into the draft %q: %q, want %q", from, before, got, want)
		}
	}
}
