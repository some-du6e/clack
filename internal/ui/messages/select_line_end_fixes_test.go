package messages

import (
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/slack"
)

// Two things the text column's press band (a press right of a line
// starts a selection at its end) must leave alone (select-line-end's
// review, wf_89deec43-217).

// TestToolbarClickOverRowAbove: the hover toolbar of a grouped row, and
// of every row under Compact, sits 28px above its row, its buttons over
// the row above's last line. A click on Add reaction there is the
// button's: the picker opens and no selection starts (a drag would hide
// the toolbar before the click lands: Karim's α.136 "the buttons don't
// work").
func TestToolbarClickOverRowAbove(t *testing.T) {
	for _, mode := range []string{"grouped", "compact"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "compact" {
				setDense(t, true)
			}
			const ts1, ts2 = "1700000600.000100", "1700000630.000100"
			second := "UALICE" // the same sender half a minute later: a grouped row
			if mode == "compact" {
				second = "UBOB" // Compact: every row
			}
			e := newCtxEnvIn(t, tGen, []slack.Message{
				{Type: "message", TS: ts1, User: "UALICE", Text: "a line the toolbar of the next row covers"},
				{Type: "message", TS: ts2, User: second, Text: "the row whose toolbar is up"},
			})
			s := e.v.s
			p := e.hoverToolbar(t, ts2, tbAddReact)
			i, r, local := s.rowAt(p)
			if i < 0 || s.items[i].key != ts1 || r == nil || local.Y < 0 || int(local.Y) >= r.bodyRect.Dy() || local.X < 0 || int(local.X) > r.selW {
				t.Fatalf("Add reaction at %v isn't over the row above's text (row %d, local %v): the test proves nothing", p, i, local)
			}
			n := len(e.h.overlays)
			e.selEvents(selPress(p, 0), selRelease(p, 40*time.Millisecond))
			if len(e.h.overlays) != n+1 {
				t.Errorf("a click on Add reaction opened %d overlays, want the reaction picker", len(e.h.overlays)-n)
			}
			if s.sel.active || s.sel.dragging {
				t.Errorf("the click on the toolbar started a selection (active %v, dragging %v)", s.sel.active, s.sel.dragging)
			}
		})
	}
}

// TestClickRightOfTextKeepsTyping: a click in the blank right of a
// message's line, then typing: the text goes to the conversation's input
// (the list's hand-off, SetSteal), as Slack's document keydown steals a
// printable key into it. The press gave the list's selection the focus,
// whose keys dropped what was typed.
func TestClickRightOfTextKeepsTyping(t *testing.T) {
	const ts = "1700000700.000100"
	e := newCtxEnvIn(t, tGen, []slack.Message{{Type: "message", TS: ts, User: "UALICE", Text: "short line"}})
	var typed string
	e.v.SetSteal(func(gtx layout.Context, text string, keys []key.Event) event.Tag {
		typed += text
		return nil
	})
	r := e.row(ts)
	p := e.selAtIn(ts, "short", 1)
	far := f32.Pt(float32(r.bodyRect.Min.X+r.selW-10), p.Y)
	if int(far.X) <= r.bodyRect.Max.X+4 {
		t.Fatalf("the press at %v isn't right of the text (body %v): the test proves nothing", far, r.bodyRect)
	}
	e.selEvents(selClicks(far, 1, 0)...)
	for _, c := range "thanks" {
		e.r.Queue(key.EditEvent{Text: string(c)})
	}
	for k := 0; k < 3; k++ {
		e.frame()
	}
	if typed != "thanks" {
		t.Errorf("typing after the click gave the input %q, want %q", typed, "thanks")
	}
}
