// SPDX-License-Identifier: Unlicense OR MIT

package widget

import (
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/font/gofont"
	"gioui.org/io/event"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"
)

// Clack: an Editor's clicks select as Blink's SelectionController does in
// an <input> or a <textarea> (third_party/blink/renderer/core/editing/
// selection_controller.cc): 2 selects the word (the punctuation next to it
// is a word of its own, ICU's word breaks; on Windows the whitespace after
// it comes along, settings.json5 selectTrailingWhitespaceEnabled), 3 and
// more the paragraph with its break, not the visual line (kParagraph), and
// a drag after a double or triple press extends by that granule, keeping
// the first (ExtendSelectionAsDirectional).

type clickEd struct {
	t     *testing.T
	r     input.Router
	gtx   layout.Context
	e     *Editor
	shape *text.Shaper
	clock time.Duration
}

func newClickEd(t *testing.T, s string, single bool, width int) *clickEd {
	t.Helper()
	c := &clickEd{t: t, e: new(Editor), shape: text.NewShaper(text.NoSystemFonts(), text.WithCollection(gofont.Collection())), clock: time.Second}
	c.e.SingleLine = single
	c.e.SetText(s)
	c.gtx = layout.Context{Ops: new(op.Ops), Constraints: layout.Exact(image.Pt(width, 200)), Locale: english, Source: c.r.Source()}
	c.gtx.Execute(key.FocusCmd{Tag: c.e})
	for range 3 {
		c.frame()
	}
	return c
}

func (c *clickEd) frame() {
	c.gtx.Ops.Reset()
	c.e.Layout(c.gtx, c.shape, font.Font{}, unit.Sp(10), op.CallOp{}, op.CallOp{})
	c.r.Frame(c.gtx.Ops)
}

// at is a point inside rune i (between its caret position and the next
// one's, on its line).
func (c *clickEd) at(i int) f32.Point {
	a, b := c.e.text.closestToRune(i), c.e.text.closestToRune(i+1)
	x := float32(a.x.Round())
	if b.y == a.y {
		x = float32(a.x.Round()+b.x.Round()) / 2
	} else {
		x += 2
	}
	return f32.Pt(x, float32(a.y-a.ascent.Ceil()/2))
}

func (c *clickEd) send(evs ...pointer.Event) {
	q := make([]event.Event, 0, len(evs))
	for _, e := range evs {
		e.Source = pointer.Mouse
		e.Time += c.clock
		q = append(q, e)
	}
	c.clock += 2 * time.Second
	c.r.Queue(q...)
	for range 3 {
		c.frame()
	}
}

func (c *clickEd) selected() string { return c.e.SelectedText() }

func edClicks(p f32.Point, n int) []pointer.Event {
	evs := []pointer.Event{{Kind: pointer.Move, Position: p}}
	for i := range n {
		at := 10*time.Millisecond + time.Duration(i)*100*time.Millisecond
		evs = append(evs,
			pointer.Event{Kind: pointer.Press, Buttons: pointer.ButtonPrimary, Position: p, Time: at},
			pointer.Event{Kind: pointer.Release, Position: p, Time: at + 30*time.Millisecond})
	}
	return evs
}

func TestClackEditorDoubleClickWord(t *testing.T) {
	setSelectTrailingSpace(t, false) // Linux's and macOS's word
	c := newClickEd(t, "hello, brave world", true, 400)
	c.send(edClicks(c.at(2), 2)...)
	if got := c.selected(); got != "hello" {
		t.Errorf("a double click on \"hello,\" selected %q, want \"hello\" (the comma is a word of its own)", got)
	}
	// the comma's left: the caret goes before it, and the word on that
	// boundary is the next (Blink's kNextWordIfOnBoundary); its right
	// half's caret is after it, before the space
	a := c.e.text.closestToRune(5)
	c.send(edClicks(f32.Pt(float32(a.x.Round())+.4, float32(a.y-a.ascent.Ceil()/2)), 2)...)
	if got := c.selected(); got != "," {
		t.Errorf("a double click on the comma's left selected %q, want \",\"", got)
	}
}

func TestClackEditorDoubleClickDragByWord(t *testing.T) {
	setSelectTrailingSpace(t, false) // Linux's and macOS's word
	c := newClickEd(t, "hello brave world", true, 400)
	p := c.at(8)
	c.send(
		pointer.Event{Kind: pointer.Move, Position: p},
		pointer.Event{Kind: pointer.Press, Buttons: pointer.ButtonPrimary, Position: p, Time: 10 * time.Millisecond},
		pointer.Event{Kind: pointer.Release, Position: p, Time: 40 * time.Millisecond},
		pointer.Event{Kind: pointer.Press, Buttons: pointer.ButtonPrimary, Position: p, Time: 120 * time.Millisecond},
		pointer.Event{Kind: pointer.Move, Buttons: pointer.ButtonPrimary, Position: c.at(13), Time: 150 * time.Millisecond},
		pointer.Event{Kind: pointer.Release, Position: c.at(13), Time: 170 * time.Millisecond})
	if got := c.selected(); got != "brave world" {
		t.Errorf("a double press dragged into \"world\" selected %q, want \"brave world\"", got)
	}
}

func TestClackEditorTripleClickParagraph(t *testing.T) {
	const first = "the first paragraph wraps over a few lines here\n"
	c := newClickEd(t, first+"second", false, 120)
	i := len("the first paragraph wraps over a ")
	if a, b := c.e.text.closestToRune(0), c.e.text.closestToRune(i); a.y == b.y {
		t.Fatalf("the first paragraph doesn't wrap at 120 px")
	}
	c.send(edClicks(c.at(i+1), 3)...)
	if got := c.selected(); got != first {
		t.Errorf("a triple click on the first paragraph's later line selected %q, want the paragraph with its break %q", got, first)
	}
}

func TestClackEditorTrailingSpaceOnWindows(t *testing.T) {
	setSelectTrailingSpace(t, true)
	c := newClickEd(t, "hello brave world", true, 400)
	c.send(edClicks(c.at(8), 2)...)
	if got := c.selected(); got != "brave " {
		t.Errorf("a double click with Windows' trailing whitespace selected %q, want \"brave \"", got)
	}
}
