package composer

import (
	"image"
	"testing"
	"time"
)

// The composer is editable text in Slack (a contenteditable), so its
// clicks select as Blink's SelectionController does there: 2 a word (on
// Windows with the whitespace after it, settings.json5
// selectTrailingWhitespaceEnabled), 3 and more the paragraph with its
// break (selection_adjuster.cc kParagraph). Taps select words here too:
// multi-click selection from touch is allowed in editable text.

// edPoint is the window point in the middle of rune i of the composer's
// text.
func edPoint(c *Composer, i int) image.Point {
	a, b := c.ed.PosRect(i), c.ed.PosRect(i+1)
	org := c.Origin.Add(c.ui.InputOffset()).Add(image.Pt(0, c.top))
	return image.Pt((a.Min.X+b.Min.X)/2, (a.Min.Y+a.Max.Y)/2).Add(org)
}

func edClicksEnv(t *testing.T, text string) (*plusEnv, *Composer) {
	t.Helper()
	e := newPlusEnv(t)
	c := e.open(tChGen, "")
	d := NewDoc()
	d.Insert(0, text, Attr{})
	c.ed.SetDoc(d)
	e.frame()
	e.frame()
	return e, c
}

func TestEditorTripleClickTakesParagraphBreak(t *testing.T) {
	e, c := edClicksEnv(t, "hello brave world\nsecond line")
	p := edPoint(c, 7)
	t0 := e.ptr
	for i := range 3 {
		e.queueAt(p, t0+time.Duration(i)*100*time.Millisecond, 30*time.Millisecond)
		e.frame()
	}
	e.frame()
	e.ptr = t0 + time.Second
	if got := c.ed.SelectedText(); got != "hello brave world\n" {
		t.Errorf("triple click selected %q, want the paragraph with its break", got)
	}
}

func TestEditorTrailingSpaceOnWindows(t *testing.T) {
	setEditorTrailingSpace(t, true)
	e, c := edClicksEnv(t, "hello brave world")
	p := edPoint(c, 8)
	t0 := e.ptr
	for i := range 2 {
		e.queueAt(p, t0+time.Duration(i)*100*time.Millisecond, 30*time.Millisecond)
		e.frame()
	}
	e.frame()
	e.ptr = t0 + time.Second
	if got := c.ed.SelectedText(); got != "brave " {
		t.Errorf("a double click with Windows' trailing whitespace selected %q, want \"brave \"", got)
	}
}
