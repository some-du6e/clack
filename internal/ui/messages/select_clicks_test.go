package messages

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/slack"
)

// Clicks in message text select as Chromium's SelectionController does
// (third_party/blink/renderer/core/editing/selection_controller.cc,
// HandleMousePressEvent): the press's count is the platform's (Gio patch
// 48, the window's count), 2 is HandleDoubleClick's word, 3 and more
// HandleTripleClick's paragraph, its break included (selection_adjuster.cc
// kParagraph: "Include the paragraph break"). A drag after a double or
// triple press extends by that granule, keeping the first one
// (ExtendSelectionAsDirectional). A double click on whitespace selects the
// whitespace. Taps (touch, pen) take multi-click side effects only in
// editable text (mouse_down_allows_multi_click_), and message text isn't.
// On Windows a double click's word takes the whitespace after it
// (settings.json5 selectTrailingWhitespaceEnabled). Synthetic text only.

const tsSelClicks = "1700000400.000100"

const selClicksText = "hello brave world\nsecond line here"

func newSelClicksEnv(t *testing.T) *ctxEnv {
	t.Helper()
	m := slack.Message{Type: "message", TS: tsSelClicks, User: "UALICE", Text: selClicksText}
	return newCtxEnvIn(t, tGen, []slack.Message{m})
}

// selAt is the view point in the middle of the rune after offset off of
// sub's first rune in the message's plain text (sub must be there).
func (e *ctxEnv) selAt(sub string, off int) f32.Point {
	e.t.Helper()
	r := e.row(tsSelClicks)
	pl := r.rv.TextRange(0, r.rv.PlainLen())
	i := strings.Index(pl, sub)
	if i < 0 {
		e.t.Fatalf("%q not in %q", sub, pl)
	}
	a, b := e.at(tsSelClicks, int32(i+off)), e.at(tsSelClicks, int32(i+off+1))
	if b.Y != a.Y {
		return a.Add(f32.Pt(2, 0))
	}
	return a.Add(b).Mul(.5)
}

// selEvents queues evs, each at the env's clock plus its own Time, and
// lays out frames.
func (e *ctxEnv) selEvents(evs ...pointer.Event) {
	e.clock += time.Second
	q := make([]event.Event, 0, len(evs))
	for _, ev := range evs {
		ev.Time += e.clock
		q = append(q, ev)
	}
	e.r.Queue(q...)
	for k := 0; k < 3; k++ {
		e.frame()
	}
}

func selPress(p f32.Point, at time.Duration) pointer.Event {
	return pointer.Event{Kind: pointer.Press, Position: p, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Time: at}
}

func selRelease(p f32.Point, at time.Duration) pointer.Event {
	return pointer.Event{Kind: pointer.Release, Position: p, Source: pointer.Mouse, Time: at}
}

func selDrag(p f32.Point, at time.Duration) pointer.Event {
	return pointer.Event{Kind: pointer.Move, Position: p, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Time: at}
}

func selMove(p f32.Point, at time.Duration) pointer.Event {
	return pointer.Event{Kind: pointer.Move, Position: p, Source: pointer.Mouse, Time: at}
}

// clicks makes n clicks at p, gap apart, each held 30 ms.
func selClicks(p f32.Point, n int, gap time.Duration) []pointer.Event {
	evs := []pointer.Event{selMove(p, 0)}
	for i := range n {
		at := 10*time.Millisecond + time.Duration(i)*gap
		evs = append(evs, selPress(p, at), selRelease(p, at+30*time.Millisecond))
	}
	return evs
}

func (e *ctxEnv) selected() string { return e.v.s.selectedText(e.h) }

// Two clicks 450 ms apart are a double click by the platform's rule
// (Chromium's 500 ms on Linux, GetDoubleClickTime on Windows, AppKit's
// count on macOS), not two clicks by a fixed 400 ms.
func TestSelectionDoubleClickByPlatformTime(t *testing.T) {
	setTrailingSpace(t, false) // Linux's and macOS's word
	e := newSelClicksEnv(t)
	e.selEvents(selClicks(e.selAt("brave", 2), 2, 450*time.Millisecond)...)
	if got := e.selected(); got != "brave" {
		t.Errorf("two clicks 450 ms apart selected %q, want \"brave\"", got)
	}
	e.selEvents(selClicks(e.selAt("brave", 2), 2, 600*time.Millisecond)...)
	if got := e.selected(); got != "" {
		t.Errorf("two clicks 600 ms apart selected %q, want a caret", got)
	}
}

// A triple click selects the paragraph and its break.
func TestSelectionTripleClickTakesParagraphBreak(t *testing.T) {
	e := newSelClicksEnv(t)
	e.selEvents(selClicks(e.selAt("brave", 2), 3, 100*time.Millisecond)...)
	if got := e.selected(); got != "hello brave world\n" {
		t.Errorf("triple click selected %q, want the paragraph with its break", got)
	}
	e.selEvents(selClicks(e.selAt("line", 1), 3, 100*time.Millisecond)...)
	if got := e.selected(); got != "second line here" {
		t.Errorf("triple click in the last paragraph selected %q, want it (no break after it)", got)
	}
}

// A drag after a double press extends by whole words, keeping the word
// it started on, either way.
func TestSelectionDoubleClickDragExtendsByWord(t *testing.T) {
	setTrailingSpace(t, false) // Linux's and macOS's word
	e := newSelClicksEnv(t)
	p := e.selAt("brave", 2)
	evs := []pointer.Event{selMove(p, 0), selPress(p, 10*time.Millisecond), selRelease(p, 40*time.Millisecond), selPress(p, 120*time.Millisecond)}
	e.selEvents(append(evs, selDrag(e.selAt("world", 1), 160*time.Millisecond), selDrag(e.selAt("world", 2), 180*time.Millisecond))...)
	if got := e.selected(); got != "brave world" {
		t.Errorf("a double press dragged into \"world\" selected %q, want \"brave world\"", got)
	}
	e.selEvents(selDrag(e.selAt("hello", 3), 0), selRelease(e.selAt("hello", 3), 20*time.Millisecond))
	if got := e.selected(); got != "hello brave" {
		t.Errorf("dragged back into \"hello\" selected %q, want \"hello brave\"", got)
	}
}

// A double click on the space between two words selects the space.
func TestSelectionDoubleClickOnSpaceSelectsIt(t *testing.T) {
	e := newSelClicksEnv(t)
	e.selEvents(selClicks(e.selAt("o brave", 1), 2, 100*time.Millisecond)...)
	if got := e.selected(); got != " " {
		t.Errorf("a double click on the space selected %q, want \" \"", got)
	}
}

// A double tap (a touch's, or a pen's) in message text selects no word:
// Chromium allows multi-click selection from touch only in editable text.
func TestSelectionTouchDoubleTapSelectsNoWord(t *testing.T) {
	e := newSelClicksEnv(t)
	p := e.selAt("brave", 2)
	var evs []pointer.Event
	for i := range 2 {
		at := 10*time.Millisecond + time.Duration(i)*150*time.Millisecond
		evs = append(evs,
			pointer.Event{Kind: pointer.Press, Position: p, Source: pointer.Touch, PointerID: 2, Buttons: pointer.ButtonPrimary, Time: at},
			pointer.Event{Kind: pointer.Release, Position: p, Source: pointer.Touch, PointerID: 2, Time: at + 40*time.Millisecond})
	}
	e.selEvents(evs...)
	if got := e.selected(); got != "" {
		t.Errorf("a double tap selected %q, want nothing (multi-click selection from touch is for editable text)", got)
	}
}

// Four quick clicks on Linux: Chromium's count goes 1, 2, 3 and round
// again, so the fourth is a single click (a caret); Windows counts on
// and AppKit too, and their fourth keeps the paragraph (3 and more).
func TestSelectionFourthClickIsSingleOnLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the fourth click is a single only by Linux's rule (ui::MouseEvent's cycle of 3)")
	}
	e := newSelClicksEnv(t)
	e.selEvents(selClicks(e.selAt("brave", 2), 4, 100*time.Millisecond)...)
	if got := e.selected(); got != "" {
		t.Errorf("four quick clicks selected %q, want a caret (the fourth is a single)", got)
	}
}

// On Windows a double click's word takes the whitespace after it.
func TestSelectionTrailingSpaceOnWindows(t *testing.T) {
	setTrailingSpace(t, true)
	e := newSelClicksEnv(t)
	e.selEvents(selClicks(e.selAt("brave", 2), 2, 100*time.Millisecond)...)
	if got := e.selected(); got != "brave " {
		t.Errorf("a double click with Windows' trailing whitespace selected %q, want \"brave \"", got)
	}
}
