package messages

import (
	"fmt"
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// The wheel over the message actions (toolbar_scroll.go). Slack's hover
// toolbar sits in the message row, inside the list's scroller, so the
// wheel over its buttons scrolls the message pane as it does over the row
// (Karim: "i cant seem to scroll when hovering over any of those icons").
// The same goes for the simplified layout's "Message actions" button.

const tWheelCh = "C0WHEELS1"

// wheelEnv is a channel of 40 messages, three times as tall as the
// 900×700 view, at its newest message, over a real input.Router. It
// returns the messages' ts, oldest first.
func wheelEnv(t *testing.T) (*ctxEnv, []string) {
	t.Helper()
	app, _ := newTestApp(t)
	app.S.PutConv(slack.Conversation{ID: tWheelCh, Name: "wheel", IsChannel: true, IsMember: true, NumMembers: 3})
	var msgs []slack.Message
	var tss []string
	for i := 0; i < 40; i++ {
		ts := fmt.Sprintf("%d.000100", 1700000100+i*60)
		tss = append(tss, ts)
		msgs = append(msgs, slack.Message{Type: "message", TS: ts, User: []string{"UALICE", "UBOB"}[i%2],
			Text: fmt.Sprintf("message %d of the channel the wheel scrolls", i)})
	}
	app.S.SetLatest(tWheelCh, msgs, false)
	e := &ctxEnv{t: t, h: &ctxHost{coreHost: coreHost{app: app, th: theme.New(false)}}, now: time.Now()}
	e.v = NewChannelView(e.h, tWheelCh)
	e.v.Preview = true
	newest := tss[len(tss)-1]
	until(t, 5*time.Second, "the newest row", e.frame, func() bool { return e.row(newest) != nil })
	for k := 0; k < 3; k++ {
		e.frame()
	}
	if !e.v.s.laidOut || e.v.s.list.Position.BeforeEnd {
		t.Fatalf("the list didn't open at its newest message: %+v", e.v.s.list.Position)
	}
	if e.row(tss[0]) != nil {
		t.Fatal("every row fits: nothing to scroll")
	}
	return e, tss
}

// rowTop is the y of ts's row in the last frame, if it was on screen.
func rowTop(s *listState, ts string) (int, bool) {
	for i := s.visible[0]; i < s.visible[1] && i < len(s.items); i++ {
		if s.items[i].key == ts && s.heights[i] > 0 {
			return s.pos[i], true
		}
	}
	return 0, false
}

// wheel turns the wheel by dy at p (a notch's distance when notched,
// otherwise a precise one, a touchpad's) and lays the next frame out at
// now.
func (e *ctxEnv) wheel(p f32.Point, dy float32, notched bool, now time.Time) {
	e.clock += 10 * time.Millisecond
	e.r.Queue(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: p, Scroll: f32.Pt(0, dy), Notched: notched, Time: e.clock})
	e.frameAt(now)
}

// hoverToolbar puts the pointer on row ts, then on its toolbar's button b,
// and returns that point.
func (e *ctxEnv) hoverToolbar(t *testing.T, ts string, b int) f32.Point {
	t.Helper()
	s := e.v.s
	now := e.now.Add(time.Second)
	e.moveTo(rowPoint(t, s, ts), now)
	e.frameAt(now.Add(20 * time.Millisecond))
	if s.tbFor < 0 {
		t.Fatalf("no toolbar over row %s", ts)
	}
	p := toolbarButtonAt(t, s, b)
	e.moveTo(p, e.now.Add(20*time.Millisecond))
	e.frameAt(e.now.Add(20 * time.Millisecond))
	if s.tbFor < 0 || !s.tb[b].Hovered() {
		t.Fatalf("the pointer isn't on toolbar button %d (toolbar %d)", b, s.tbFor)
	}
	return p
}

// toEnd puts the list back at its newest message.
func (e *ctxEnv) toEnd() {
	e.v.s.list.Position = layout.Position{}
	for k := 0; k < 3; k++ {
		e.frameAt(e.now.Add(20 * time.Millisecond))
	}
}

// A touchpad's scroll over each toolbar button moves the list by its
// distance, as it does over the row; down at the end of the list it does
// nothing.
func TestToolbarWheelScrollsList(t *testing.T) {
	t.Cleanup(uipref.Reset)
	uipref.Reset()
	e, tss := wheelEnv(t)
	s := e.v.s
	row, ref := tss[len(tss)-2], tss[len(tss)-6]
	for _, b := range []int{tbAddReact, tbReply, tbMore} {
		p := e.hoverToolbar(t, row, b)
		y0, ok := rowTop(s, ref)
		if !ok {
			t.Fatalf("row %s isn't on screen", ref)
		}
		e.wheel(p, -100, false, e.now.Add(16*time.Millisecond))
		y1, _ := rowTop(s, ref)
		if y1-y0 != 100 || !s.list.Position.BeforeEnd {
			t.Errorf("a 100px scroll up over toolbar button %d moved the list %dpx (position %+v), want 100", b, y1-y0, s.list.Position)
		}
		e.toEnd()
	}

	// at the end, down over the toolbar: nothing to scroll, the list stays
	// at its end
	p := e.hoverToolbar(t, row, tbReply)
	y0, _ := rowTop(s, ref)
	e.wheel(p, 100, false, e.now.Add(16*time.Millisecond))
	if y1, _ := rowTop(s, ref); y1 != y0 || s.list.Position.BeforeEnd {
		t.Errorf("down at the end over the toolbar: moved %dpx, position %+v", y1-y0, s.list.Position)
	}
}

// A wheel notch over a toolbar button scrolls the list frame by frame
// exactly as the same notch over the row does: the same distance, on the
// same animated curve (Gio patch 9, Chromium's wheel animation).
func TestToolbarWheelNotchLikeRow(t *testing.T) {
	t.Cleanup(uipref.Reset)
	uipref.Reset()
	trace := func(overToolbar bool) []int {
		e, tss := wheelEnv(t)
		s := e.v.s
		row, ref := tss[len(tss)-2], tss[len(tss)-6]
		var p f32.Point
		if overToolbar {
			p = e.hoverToolbar(t, row, tbReply)
		} else {
			now := e.now.Add(time.Second)
			p = rowPoint(t, s, row)
			e.moveTo(p, now)
			e.frameAt(now.Add(20 * time.Millisecond))
			if s.tbFor < 0 || image.Pt(int(p.X), int(p.Y)).Sub(s.origin).In(s.tbRect) {
				t.Fatalf("the row's toolbar isn't up, or is under the pointer")
			}
		}
		y0, _ := rowTop(s, ref)
		t0 := e.now.Add(16 * time.Millisecond)
		e.wheel(p, -100, true, t0)
		var out []int
		for k := 0; k <= 24; k++ {
			if k > 0 {
				e.frameAt(t0.Add(time.Duration(k) * 16 * time.Millisecond))
			}
			y, ok := rowTop(s, ref)
			if !ok {
				t.Fatalf("row %s left the screen", ref)
			}
			out = append(out, y-y0)
		}
		return out
	}
	onRow, onBar := trace(false), trace(true)
	if last := onRow[len(onRow)-1]; last != 100 {
		t.Fatalf("a 100px notch over the row moved the list %dpx: %v", last, onRow)
	}
	animated := false
	for _, d := range onRow {
		if d > 0 && d < 100 {
			animated = true
		}
	}
	if !animated {
		t.Fatalf("the notch over the row didn't animate: %v", onRow)
	}
	if fmt.Sprint(onBar) != fmt.Sprint(onRow) {
		t.Errorf("a notch over the toolbar moved the list\n  %v\nover the row\n  %v", onBar, onRow)
	}
}

// The simplified layout's persistent "Message actions" button: the wheel
// over it scrolls the list too.
func TestPersistActionsWheelScrollsList(t *testing.T) {
	t.Cleanup(uipref.Reset)
	uipref.Reset()
	setPersist(t, true)
	e, tss := wheelEnv(t)
	s := e.v.s
	row, ref := tss[len(tss)-2], tss[len(tss)-6]
	var p f32.Point
	found := false
	for i := s.visible[0]; i < s.visible[1] && i < len(s.items); i++ {
		if s.items[i].key == row && s.heights[i] > 0 {
			// persist.go's button: 28px, 16px from the right, top
			// min(50% - 14px, 12px)
			y := s.pos[i] + min(s.heights[i]/2-14, 12)
			p = f32.Pt(float32(900-16-28+14+s.origin.X), float32(y+14+s.origin.Y))
			found = s.pbtn[row] != nil
		}
	}
	if !found {
		t.Fatalf("no Message actions button on row %s", row)
	}
	now := e.now.Add(time.Second)
	e.moveTo(p, now)
	e.frameAt(now.Add(20 * time.Millisecond))
	if !s.pbtn[row].Hovered() {
		t.Fatal("the pointer isn't on the Message actions button")
	}
	y0, _ := rowTop(s, ref)
	e.wheel(p, -100, false, e.now.Add(16*time.Millisecond))
	if y1, _ := rowTop(s, ref); y1-y0 != 100 {
		t.Errorf("a 100px scroll up over the Message actions button moved the list %dpx, want 100", y1-y0)
	}
}
