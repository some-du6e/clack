package richview

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
)

// A View's own selection (one that isn't the message list's, External)
// takes clicks as Chromium's SelectionController does: the press's count
// is the platform's (Gio patch 48), 2 selects a word, 3 and more the
// paragraph with its break, a drag after a double or triple press extends
// by that granule keeping the first, a double click on whitespace selects
// it, taps take no multi-click selection (text that isn't editable), and on
// Windows a double click's word takes the whitespace after it. See
// messages/select_clicks_test.go for the same rules in the message list.

func selClicksHarness(t *testing.T) *harness {
	t.Helper()
	return newHarness(doc(para(txt("hello brave world")), para(txt("second line here"))), &recHandler{})
}

// vclicks is n clicks at p, gap apart, each held 30 ms, from at.
func vclicks(p f32.Point, n int, at, gap time.Duration) []event.Event {
	evs := []event.Event{pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p, Time: at}}
	for i := range n {
		t := at + 10*time.Millisecond + time.Duration(i)*gap
		evs = append(evs, press(p, t), release(p, t+30*time.Millisecond))
	}
	return evs
}

func vdrag(p f32.Point, at time.Duration) pointer.Event {
	return pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: p, Time: at}
}

func TestViewDoubleClickByPlatformTime(t *testing.T) {
	setTrailingSpace(t, false) // Linux's and macOS's word
	hs := selClicksHarness(t)
	p := center(findFrag(t, &hs.v.d, "brave"))
	hs.send(vclicks(p, 2, time.Second, 450*time.Millisecond)...)
	if got := hs.v.SelectedText(); got != "brave" {
		t.Errorf("two clicks 450 ms apart selected %q, want \"brave\"", got)
	}
	hs.send(vclicks(p, 2, 3*time.Second, 600*time.Millisecond)...)
	if got := hs.v.SelectedText(); got != "" {
		t.Errorf("two clicks 600 ms apart selected %q, want a caret", got)
	}
}

func TestViewTripleClickTakesParagraphBreak(t *testing.T) {
	hs := selClicksHarness(t)
	pl := string(hs.v.d.plain)
	want := pl[:strings.Index(pl, "second")]
	if !strings.HasSuffix(want, "\n") {
		t.Fatalf("paragraphs not apart by a line break in %q", pl)
	}
	hs.send(vclicks(center(findFrag(t, &hs.v.d, "brave")), 3, time.Second, 100*time.Millisecond)...)
	if got := hs.v.SelectedText(); got != want {
		t.Errorf("triple click selected %q, want %q (the paragraph with its break)", got, want)
	}
}

func TestViewDoubleClickDragExtendsByWord(t *testing.T) {
	setTrailingSpace(t, false) // Linux's and macOS's word
	hs := selClicksHarness(t)
	p := center(findFrag(t, &hs.v.d, "brave"))
	w := findFrag(t, &hs.v.d, "world")
	in := f32.Pt(w.x+w.w*.3, float32(w.top+w.bot)/2)
	h := center(findFrag(t, &hs.v.d, "hello"))
	t0 := time.Second
	hs.send(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p, Time: t0},
		press(p, t0+10*time.Millisecond), release(p, t0+40*time.Millisecond),
		press(p, t0+120*time.Millisecond),
		vdrag(in.Sub(f32.Pt(4, 0)), t0+150*time.Millisecond), vdrag(in, t0+170*time.Millisecond))
	if got := hs.v.SelectedText(); got != "brave world" {
		t.Errorf("a double press dragged into \"world\" selected %q, want \"brave world\"", got)
	}
	hs.send(vdrag(h, t0+200*time.Millisecond), release(h, t0+220*time.Millisecond))
	if got := hs.v.SelectedText(); got != "hello brave" {
		t.Errorf("dragged back into \"hello\" selected %q, want \"hello brave\"", got)
	}
}

func TestViewDoubleClickOnSpaceSelectsIt(t *testing.T) {
	hs := selClicksHarness(t)
	h := findFrag(t, &hs.v.d, "hello")
	space := int32(strings.Index(string(hs.v.d.plain), " brave"))
	// the first point whose offset is the space's: the right of "hello"'s
	// "o", where the caret goes after it (the gap after the frag is
	// "brave"'s start)
	y := float32(h.top+h.bot) / 2
	var p f32.Point
	for x := h.x; x <= h.x+h.w; x += .25 {
		if hs.v.offsetAt(f32.Pt(x, y)) == space {
			p = f32.Pt(x+.25, y)
			break
		}
	}
	if p == (f32.Point{}) {
		t.Fatalf("no point in %q's frag at the space's offset %d", "hello", space)
	}
	hs.send(vclicks(p, 2, time.Second, 100*time.Millisecond)...)
	if got := hs.v.SelectedText(); got != " " {
		t.Errorf("a double click on the space selected %q, want \" \"", got)
	}
}

func TestViewTouchDoubleTapSelectsNoWord(t *testing.T) {
	hs := selClicksHarness(t)
	p := center(findFrag(t, &hs.v.d, "brave"))
	var evs []event.Event
	for i := range 2 {
		at := time.Second + time.Duration(i)*150*time.Millisecond
		evs = append(evs,
			pointer.Event{Kind: pointer.Press, Source: pointer.Touch, PointerID: 2, Buttons: pointer.ButtonPrimary, Position: p, Time: at},
			pointer.Event{Kind: pointer.Release, Source: pointer.Touch, PointerID: 2, Position: p, Time: at + 40*time.Millisecond})
	}
	hs.send(evs...)
	if got := hs.v.SelectedText(); got != "" {
		t.Errorf("a double tap selected %q, want nothing", got)
	}
}

func TestViewFourthClickIsSingleOnLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the fourth click is a single only by Linux's rule (ui::MouseEvent's cycle of 3)")
	}
	hs := selClicksHarness(t)
	hs.send(vclicks(center(findFrag(t, &hs.v.d, "brave")), 4, time.Second, 100*time.Millisecond)...)
	if got := hs.v.SelectedText(); got != "" {
		t.Errorf("four quick clicks selected %q, want a caret (the fourth is a single)", got)
	}
}

func TestViewTrailingSpaceOnWindows(t *testing.T) {
	setTrailingSpace(t, true)
	hs := selClicksHarness(t)
	hs.send(vclicks(center(findFrag(t, &hs.v.d, "brave")), 2, time.Second, 100*time.Millisecond)...)
	if got := hs.v.SelectedText(); got != "brave " {
		t.Errorf("a double click with Windows' trailing whitespace selected %q, want \"brave \"", got)
	}
}
