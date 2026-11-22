package messages

// Round 4's review MINORs on the list's scrolling, against Slack's
// BaseKeyboardNavigableList (client-boot FOmE), MessageList (AEjl) and
// AnchorLayout (dxC+). Synthetic history on the fake (winEnv).

import (
	"fmt"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/core"
)

// focus gives the list keyboard focus at message ts.
func (e *winEnv) focus(t *testing.T, ts string) {
	t.Helper()
	e.v.FocusMessage(ts, func() {})
	e.await(t, "the keyboard on message "+ts, func() bool { return e.v.KeyboardSelected() == ts })
	for k := 0; k < 3; k++ { // the focus frame holds that frame's events
		e.rt.frame()
	}
}

// press is key name pressed and released.
func (e *winEnv) press(name key.Name) {
	e.rt.r.Queue(key.Event{Name: name, State: key.Press}, key.Event{Name: name, State: key.Release})
}

// top is message ts's top under the list's top in the last layout.
func (e *winEnv) top(ts string) (int, bool) {
	for _, r := range e.v.s.laid {
		if r.key == ts {
			return r.y, true
		}
	}
	return 0, false
}

// TestWindowEndKeyLeavesWindow: End is MessageList's onEnd,
// scrollToMostRecentMessage, which a window short of the newest messages
// takes to them (jumpToTimestamp({ts: ""})). It went to the window's end
// and started its newer pages.
func TestWindowEndKeyLeavesWindow(t *testing.T) {
	e := newWinEnv(t)
	e.focus(t, e.tss[40])
	e.press(key.NameEnd)
	e.await(t, "End from the window", func() bool { return !e.v.InWindow() && e.drawn(e.newest()) })
}

// TestPageUpMovesAPageLess21: a page is getPageHeight, the list's height
// less 21px (FOmE). It was the whole height.
func TestPageUpMovesAPageLess21(t *testing.T) {
	e := newHistEnv(t)
	e.focus(t, e.newest())
	key0, y0 := "", 0
	for _, r := range e.v.s.laid {
		if e.index(r.key) >= 0 {
			key0, y0 = r.key, r.y
			break
		}
	}
	if key0 == "" || y0 >= 21 {
		t.Fatalf("no message row at the list's top to measure (%s)", e.state())
	}
	e.press(key.NamePageUp)
	for k := 0; k < 3; k++ {
		e.rt.frame()
	}
	y1, ok := e.top(key0)
	if want := e.rt.h - 21; !ok || y1-y0 != want {
		t.Errorf("PageUp moved message %d from %dpx to %dpx (drawn %v): %dpx, want the height less 21, %dpx", e.index(key0), y0, y1, ok, y1-y0, want)
	}
}

// TestPageKeyTakesTheDOMNames: the empty composer hands MessagePane's
// keydown names over (KeyboardEvent.key: "PageUp", "End"), and PageKey
// pages the list by them as its own keys do. It read them as Gio's key
// names, which are symbols, and did nothing.
func TestPageKeyTakesTheDOMNames(t *testing.T) {
	e := newHistEnv(t)
	key0, y0 := "", 0
	for _, r := range e.v.s.laid {
		if e.index(r.key) >= 0 {
			key0, y0 = r.key, r.y
			break
		}
	}
	if key0 == "" || y0 >= 21 {
		t.Fatalf("no message row at the list's top to measure (%s)", e.state())
	}
	e.v.PageKey("PageUp")
	for k := 0; k < 3; k++ {
		e.rt.frame()
	}
	if y1, ok := e.top(key0); !ok || y1-y0 != e.rt.h-21 {
		t.Fatalf("PageKey(PageUp) moved message %d from %dpx to %dpx (drawn %v), want a page (%dpx) down", e.index(key0), y0, y1, ok, e.rt.h-21)
	}
	e.v.PageKey("End")
	e.await(t, "PageKey(End) back at the end", func() bool { return e.v.AtBottom() && e.drawn(e.newest()) })
}

// TestNewMessageAfterPageUpKeepsPlace: setKeys's stickToBottom reads the
// scrollTop the reader just set (dxC+): a page up from the end, then a new
// message before the next layout, leaves the list where the page put it.
// Clack read the gap to the end from the layout before, at the end, and
// the message took the list back down (PageUp, the scrollbar, the
// toolbar's wheel and ScrollPx write the place between layouts).
func TestNewMessageAfterPageUpKeepsPlace(t *testing.T) {
	e := newHistEnv(t)
	if !e.v.AtBottom() || !e.drawn(e.newest()) {
		t.Fatalf("the list didn't open at its end (%s)", e.state())
	}
	e.v.ScrollPx(-(e.rt.h - 21)) // PageUp's page
	ts := fmt.Sprintf("%d.000100", time.Now().Unix())
	e.v.s.hb.apply(core.Event{Type: "message", Channel: winEndChan,
		Msg: &core.Message{TS: ts, Channel: winEndChan, User: "UBEN00001", Text: "a new message"}})
	for k := 0; k < 3; k++ {
		e.rt.frame()
	}
	if e.v.AtBottom() || e.drawn(ts) {
		t.Errorf("a page up from the end, then a new message before the next layout: the list went back to its end (%s)", e.state())
	}
}

// TestShrinkingNearEndKeepsToIt: setContainerHeight (dxC+): the list's
// height changes (the composer grows a line) while it's within
// STICKY_EPSILON of the newest messages' end: it stays at the end. Gio
// kept its top, and the last message's bottom went under the composer.
func TestShrinkingNearEndKeepsToIt(t *testing.T) {
	e := newHistEnv(t)
	e.v.ScrollPx(-20)
	e.rt.frame()
	e.rt.frame()
	if e.v.AtBottom() {
		t.Fatal("20px up from the end, the list still at it")
	}
	e.rt.h -= 30 // the composer grew a line
	e.await(t, "the list at its end after it shrank 20px from it", func() bool { return e.v.AtBottom() })
}

// TestDeletedLineRowKeepsNextInPlace: setKeys keeps findAnchor's row, the
// first row ending below the anchor line that's still there (dxC+): the
// row on the line deleted, the one after it stays where it was and the
// rows above move down into the gap. Clack kept the list's place by its
// first item, and every row below the deleted one moved up.
func TestDeletedLineRowKeepsNextInPlace(t *testing.T) {
	e := newHistEnv(t)
	e.v.ScrollPx(-600)
	for k := 0; k < 3; k++ {
		e.rt.frame()
	}
	s := e.v.s
	line, _ := s.lineRow(s.linePx)
	next, y0, after := "", 0, false
	for _, r := range s.laid {
		if after && e.index(r.key) >= 0 {
			next, y0 = r.key, r.y
			break
		}
		after = after || r.key == line
	}
	if line == "" || next == "" {
		t.Fatalf("no row on the anchor line with one after it (%s)", e.state())
	}
	s.hb.apply(core.Event{Type: "delete", Channel: winEndChan, TS: line})
	e.await(t, "the row on the line deleted", func() bool { return !e.drawn(line) })
	if y1, ok := e.top(next); !ok || y1 != y0 {
		t.Errorf("the row on the line (message %d) deleted: message %d, after it, went from %dpx to %dpx (drawn %v)",
			e.index(line), e.index(next), y0, y1, ok)
	}
}

// TestEndStopsTheWheelCurve: a programmatic scroll ends the reader's
// running one (Chromium cancels a scroll animation on a scrollTop write):
// End inside a wheel notch's curve leaves the list at its end. The rest of
// the curve took it off again.
func TestEndStopsTheWheelCurve(t *testing.T) {
	e := newHistEnv(t)
	e.focus(t, e.newest())
	e.rt.evT += 30 * time.Millisecond
	e.rt.r.Queue(pointer.Event{Kind: pointer.Scroll, Position: f32.Pt(450, 350), Scroll: f32.Pt(0, -120), Source: pointer.Mouse,
		Notched: true, Time: e.rt.evT}) // a notch up: its curve runs some 200ms
	e.rt.frame()
	e.press(key.NameEnd)
	for k := 0; k < 20; k++ {
		e.rt.frame()
	}
	if !e.v.AtBottom() {
		t.Errorf("End inside a wheel notch's curve: the rest of the curve took the list off its end (%s)", e.state())
	}
}

// TestPluginJumpStartsAFlash: a plugin's jumpToReply (the model's jump)
// starts a new flash, as highlight and JumpTo do. It kept the earlier
// flash's marks: hlSeen ended it in the first layout that didn't draw its
// message yet, and a stale hlRestart kept its clock from restarting.
func TestPluginJumpStartsAFlash(t *testing.T) {
	s := &listState{highlight: "1762500100.000100", hlSeen: true}
	s.snap.jump = "1762500200.000100"
	now := time.Now()
	s.takeJump(now)
	if s.highlight != "1762500200.000100" || s.pendingJump != "1762500200.000100" || !s.highlightAt.Equal(now) {
		t.Fatalf("the plugin's jump: highlight %q, pending %q", s.highlight, s.pendingJump)
	}
	if s.hlSeen || !s.hlRestart {
		t.Errorf("the plugin's jump kept the earlier flash's marks (seen %v, restart %v): its flash ends before it draws", s.hlSeen, s.hlRestart)
	}
}
