package messages

// A day pill takes input on its own 28px box only (Slack's label is
// pointer-events: none, the pill pointer-events: all; its ring and shadow
// aren't hit). In a narrow pane the hover toolbar of the first message
// under a divider reaches the pill's side: its buttons there, just past
// the pill, take the pointer. Clack's pill area reached 8dp past the
// pill, over the toolbar, and swallowed hover and clicks there. Synthetic
// data.

import (
	"fmt"
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

func TestDayPillInputIsItsOwnBoxLikeSlack(t *testing.T) {
	t.Cleanup(uipref.Reset)
	uipref.Reset()
	const ch, w, h = "C0PILLIN01", 600, 700
	day0 := time.Date(2026, 11, 10, 9, 0, 0, 0, time.Local)
	var msgs []slack.Message
	for d := 0; d < 2; d++ {
		for i := 0; i < 3; i++ {
			at := day0.AddDate(0, 0, d).Add(time.Duration(i) * 10 * time.Minute)
			msgs = append(msgs, slack.Message{Type: "message", TS: fmt.Sprintf("%d.000100", at.Unix()), User: []string{"UALICE", "UBOB"}[i%2],
				Text: fmt.Sprintf("synthetic message %d of day %d", i, d)})
		}
	}
	app, _ := newTestApp(t)
	app.S.PutConv(slack.Conversation{ID: ch, Name: "pills", IsChannel: true, IsMember: true, NumMembers: 3})
	app.S.SetLatest(ch, msgs, false)
	e := &ctxEnv{t: t, h: &ctxHost{coreHost: coreHost{app: app, th: theme.New(false)}}, now: time.Now()}
	e.v = NewChannelView(e.h, ch)
	e.v.Preview = true
	frameAt := func(now time.Time) {
		e.ops.Reset()
		e.now = now
		gtx := layout.Context{Ops: &e.ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(w, h)), Now: now, Source: e.r.Source()}
		e.v.Layout(gtx)
		e.r.Frame(&e.ops)
	}
	frame := func() { frameAt(e.now.Add(20 * time.Millisecond)) }
	move := func(p f32.Point) {
		e.clock += 10 * time.Millisecond
		e.r.Queue(pointer.Event{Kind: pointer.Move, Position: p, Source: pointer.Mouse, Time: e.clock})
		frameAt(e.now.Add(time.Second))
		frameAt(e.now.Add(20 * time.Millisecond))
	}
	first := msgs[3].TS // the second day's first message, under its divider
	until(t, 5*time.Second, "the rows", frame, func() bool { return e.row(first) != nil })
	for i := 0; i < 3; i++ {
		frame()
	}
	s := e.v.s
	k := s.sel.keyIdx[first]
	d := k - 1
	if d < 0 || s.items[d].kind != itDay {
		t.Fatalf("no day divider right over the second day's first message (item %d)", d)
	}
	var pill image.Rectangle
	for _, dc := range s.dayCalls {
		if dc.item == d {
			pill = dc.r.Add(image.Pt(0, s.pos[d]))
		}
	}
	if pill.Empty() {
		t.Fatal("the divider's pill wasn't drawn: the case tests nothing")
	}
	move(rowPoint(t, s, first))
	if s.tbFor != k {
		t.Fatalf("no toolbar over the hovered message (toolbar on %d, want %d)", s.tbFor, k)
	}
	// a point on the toolbar's buttons, past the pill's box but within
	// 8dp of it
	pad := int(mt.toolbarPad)
	btns := image.Rect(s.tbRect.Min.X+pad+2, s.tbRect.Min.Y+pad+2, s.tbRect.Max.X-pad-2, s.tbRect.Max.Y-pad-2)
	band := pill.Inset(-8).Intersect(btns)
	var p image.Point
	found := false
	for y := band.Min.Y; y < band.Max.Y && !found; y++ {
		for x := band.Min.X; x < band.Max.X && !found; x++ {
			if q := image.Pt(x, y); !q.In(pill) {
				p, found = q, true
			}
		}
	}
	if !found {
		t.Fatalf("the toolbar %v doesn't reach within 8dp of the pill %v: the case tests nothing", s.tbRect, pill)
	}
	move(f32.Pt(float32(p.X)+.5, float32(p.Y)+.5))
	if s.tbFor != k {
		t.Fatalf("the toolbar went (on %d) with the pointer on it", s.tbFor)
	}
	if s.tbHov < 0 && s.quick.hov < 0 {
		t.Errorf("the pointer at %v, on the toolbar %v and %dpx from the pill %v, hovers none of its buttons: the pill's area took it",
			p, s.tbRect, pillGap(p, pill), pill)
	}
}

// pillGap is how far p is from r's edge (0 inside).
func pillGap(p image.Point, r image.Rectangle) int {
	dx := max(r.Min.X-p.X, 0, p.X-r.Max.X+1)
	dy := max(r.Min.Y-p.Y, 0, p.Y-r.Max.Y+1)
	return max(dx, dy)
}
