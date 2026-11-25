package messages

// A jump into a window from the newest messages (a permalink, a search
// result, an Activity or saved item) and back ("Latest messages") must
// leave the conversation as it was:
//
//   - Unread. MessagePane.isUnreadLineVisible (client-boot-shared) is
//     !isLoading && (!isGhostLastRead && getUnreadLinePlacement() === 2 ||
//     !unreadCount && reachedEnd && isAtBottom), and a slice without the
//     divider (MessageList's unreadIndex null) places it 1 or 3, never 2.
//     The list counted a window without the "New" line as the line seen,
//     and marked the channel read once it was back at the newest messages.
//   - With its open inline edit. Slack's edit is messageEditWindow's entry
//     and a draft, so coming back shows the editor with what was typed;
//     the window's sync dropped the row, and its editor with it, and left
//     Taut's editing flag set (patchMessageText then skipped the message
//     for good).
//
// Synthetic history on the fake.

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// guardEnv is a channel of 240 messages 90 s apart (each by self when
// self(i)), its list at its newest messages; setup runs before the view
// opens.
func guardEnv(t *testing.T, ch string, self func(i int) bool, setup func(app *core.App, fs *fakeslack.Server, tss []string)) (*winEnv, *fakeslack.Server) {
	t.Helper()
	app, fs, _ := shotsApp(t)
	app.S.PutConv(slack.Conversation{ID: ch, Name: "jump-guards", IsChannel: true, IsMember: true, NumMembers: 12})
	e := &winEnv{h: &ctxHost{coreHost: coreHost{app: app, th: theme.New(false)}}}
	base := dayBase(240, 90)
	for i := 0; i < 240; i++ {
		ts := fmt.Sprintf("%d.000100", base+int64(i)*90)
		user, text := "UBEN00001", fmt.Sprintf("message %d", i)
		if self != nil && self(i) {
			user, text = fakeslack.SelfID, fmt.Sprintf("my draft note %d", i)
		}
		fs.PostQuietAt(ch, user, text, ts)
		e.tss = append(e.tss, ts)
	}
	if setup != nil {
		setup(app, fs, e.tss)
	}
	e.v = NewChannelView(e.h, ch)
	e.rt = newRouted(e.v, 900, 700)
	e.await(t, "the newest messages", func() bool { return e.drawn(e.newest()) && e.v.AtBottom() })
	return e, fs
}

// windowAndBack jumps to message 40 (the window around it, as a permalink
// does) and back with "Latest messages".
func (e *winEnv) windowAndBack(t *testing.T) {
	t.Helper()
	e.v.Highlight(e.tss[40])
	e.await(t, "the window around message 40", func() bool {
		return e.v.InWindow() && e.drawn(e.tss[40]) && e.v.s.pendingJump == ""
	})
	e.frames(300 * time.Millisecond)
	e.await(t, `the "Latest messages" pill`, func() bool { return !e.v.LatestMessagesRect().Empty() })
	e.rt.press(centre(e.v.LatestMessagesRect()), true)
	e.await(t, `"Latest messages": the newest messages`, func() bool {
		return !e.v.InWindow() && e.drawn(e.newest()) && e.v.AtBottom()
	})
}

// frames lays the list out for d of wall-clock time.
func (e *winEnv) frames(d time.Duration) {
	for start := time.Now(); time.Since(start) < d; time.Sleep(15 * time.Millisecond) {
		e.rt.frame()
	}
}

// TestWindowJumpBackKeepsUnread: a channel with 48 unread messages, the
// "New" line above the view; a permalink into the window around message
// 40 and "Latest messages" back don't mark it read (default prefs:
// mark_msgs_read_immediately off). The line seen, then the newest
// messages, do.
func TestWindowJumpBackKeepsUnread(t *testing.T) {
	const ch = "C0WINUNRD1"
	e, fs := guardEnv(t, ch, nil, func(app *core.App, _ *fakeslack.Server, tss []string) {
		app.S.SetCounts(&slack.Counts{Channels: []slack.CountEntry{{ID: ch, LastRead: tss[190], Latest: tss[239], HasUnreads: true}}})
	})
	app := e.h.app
	if e.v.s.unreadIndex() < 0 {
		t.Fatalf("control: the newest messages have no \"New\" line: %s", e.state())
	}
	if e.v.s.unreadSeen {
		t.Fatalf("control: the \"New\" line counts as seen at the newest messages' end: %s", e.state())
	}
	marks := func() int64 { return fs.CallCount("conversations.mark") }
	e.windowAndBack(t)
	e.frames(2 * time.Second) // the list marks at most every 1.5 s, off the UI goroutine
	if n := marks(); n != 0 {
		t.Errorf("a jump into a window without the \"New\" line and back marked the channel read (%d conversations.mark): %s", n, e.state())
	}
	if c := app.Count(ch); !c.Unread || c.LastRead != e.tss[190] {
		t.Errorf("after the window and back: unread %v, last read %s, want unread at %s", c.Unread, c.LastRead, e.tss[190])
	}

	// control: the line on screen, then the newest messages: marked
	e.v.Highlight(e.tss[191])
	e.await(t, "the \"New\" line in view", func() bool { return e.v.s.unreadSeen })
	e.v.ScrollToBottom()
	for dl := time.Now().Add(10 * time.Second); marks() == 0 && time.Now().Before(dl); time.Sleep(15 * time.Millisecond) {
		e.rt.frame()
	}
	if marks() == 0 {
		t.Errorf("control: the \"New\" line seen and the newest messages on screen didn't mark the channel read: %s", e.state())
	}
}

// draftJS patches the viewer's messages' text: a plugin on, so Taut runs.
var draftJS = pluginJS(`
class MaskDraft extends TautPlugin {
  static id = 'MaskDraft'
  static pluginName = 'Mask draft (test)'
  static description = 'test: patchMessageText upper-cases draft'
  static authors = ['clack']
  static category = 'messages'
  static defaultConfig = { enabled: true }
  start() {
    this.api.messages.patchMessageText((t) => t.replace(/draft/g, 'DRAFT'))
  }
}
return MaskDraft`)

// editEnv is guardEnv with the viewer's message 235 (my draft note 235)
// and draftJS on, the message drawn patched; editing is the main window's
// messageEditWindow entry in the plugins' store (its ts, "" for none),
// which Taut posts to JS a little after it changes.
func editEnv(t *testing.T, ch string) (e *winEnv, editing func() string) {
	t.Helper()
	e, _ = guardEnv(t, ch, func(i int) bool { return i == 235 }, func(app *core.App, _ *fakeslack.Server, _ []string) {
		patchedEnv(t, app, draftJS)
		until(t, 5*time.Second, "the messages slice patched", func() {}, func() bool { return tautOf().SlicePatched("messages") })
	})
	e.await(t, "the plugin's text on the viewer's message", func() bool {
		r := e.v.s.rows[e.tss[235]]
		return r != nil && strings.Contains(r.m.Text, "my DRAFT note")
	})
	rt, err := tautOf().JS()
	if err != nil {
		t.Fatal(err)
	}
	editing = func() string {
		v, err := rt.Eval("edit_probe.js", `String(globalThis.__taut.getRawState()?.messageEditWindow?.main?.ts ?? "")`)
		if err != nil {
			t.Fatal(err)
		}
		return v.String()
	}
	return e, editing
}

// TestWindowJumpKeepsTheInlineEdit: an inline edit open with text typed
// in it, a permalink into a window and "Latest messages" back: the editor
// is there again with the text, on its row. Taut's editing flag
// (messageEditWindow) follows the editor: set while it's open, the window
// too, cleared once it closes.
func TestWindowJumpKeepsTheInlineEdit(t *testing.T) {
	e, editing := editEnv(t, "C0WINEDIT1")
	mine := e.tss[235]
	e.v.Edit(mine)
	e.await(t, "the editor", func() bool {
		ed := e.v.EditComposer()
		return ed != nil && strings.Contains(ed.DocText(), "note 235")
	})
	// control: Taut hears it (the mirror posts the slice to JS later)
	e.await(t, "messageEditWindow.main at the edited message", func() bool { return editing() == mine })
	ed := e.v.EditComposer()
	ed.InsertText(" and more")
	want := ed.DocText()
	if !strings.Contains(want, "and more") {
		t.Fatalf("control: the typed text isn't in the editor: %q", want)
	}
	e.windowAndBack(t)
	e.frames(300 * time.Millisecond)
	got := e.v.EditComposer()
	if got == nil {
		t.Fatalf("after a window and back the inline edit is gone (Slack keeps it: messageEditWindow and its draft): %s", e.state())
	}
	if got.DocText() != want {
		t.Errorf("after a window and back the editor holds %q, want %q", got.DocText(), want)
	}
	if r := e.v.s.rows[mine]; r == nil || r.edit != got {
		t.Errorf("the editor isn't on the message's row")
	}
	e.frames(300 * time.Millisecond)
	if ts := editing(); ts != mine {
		t.Errorf("back with the editor open, messageEditWindow.main is %q, want %s", ts, mine)
	}

	// it takes the keyboard when asked (Edit again); Escape closes it and
	// Taut hears the edit closed
	e.v.Edit(mine)
	e.frames(200 * time.Millisecond)
	e.press(key.NameEscape)
	e.await(t, "the editor closed", func() bool { return e.v.EditComposer() == nil })
	e.await(t, "messageEditWindow.main cleared", func() bool { return editing() == "" })
}
