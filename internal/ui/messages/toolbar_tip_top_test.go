package messages

// The hover toolbar's tooltips are Slack's BaseTooltip, a popover in the
// body (a portal): over a row at the top of the message list the tip
// above a button shows over the header, outside the list. Clack drew the
// toolbar's tips inside the list's clip, so one over the top row was cut
// off at the list's top (the reaction and status tips were already drawn
// on top of everything). Synthetic data, drawn headlessly (Mesa on CI).

import (
	"fmt"
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

func TestToolbarTipOverTheListTopLikeSlack(t *testing.T) {
	t.Cleanup(uipref.Reset)
	uipref.Reset()
	const ch = "C0TIPTOP01"
	var msgs []slack.Message
	for i := 0; i < 30; i++ {
		msgs = append(msgs, slack.Message{Type: "message", TS: fmt.Sprintf("%d.000100", 1700400000+i*600), User: []string{"UALICE", "UBOB"}[i%2],
			Text: fmt.Sprintf("synthetic message %d", i)})
	}
	// newCtxEnvIn waits for every row on screen: 30 don't fit, so the
	// same env, waiting for the newest
	app, _ := newTestApp(t)
	app.S.PutConv(slack.Conversation{ID: ch, Name: "ctx", IsChannel: true, IsMember: true, NumMembers: 3})
	app.S.SetLatest(ch, msgs, false)
	e := &ctxEnv{t: t, h: &ctxHost{coreHost: coreHost{app: app, th: theme.New(false)}}, now: time.Now()}
	e.v = NewChannelView(e.h, ch)
	e.v.Preview = true
	until(t, 5*time.Second, "the newest row", e.frame, func() bool { return e.row(msgs[len(msgs)-1].TS) != nil })
	// the list sits 150px down a 900px window (the header and tabs above)
	const off, winW, winH = 150, 900, 900
	prev := theme.WindowSize
	theme.WindowSize = func() image.Point { return image.Pt(winW, winH) }
	t.Cleanup(func() { theme.WindowSize = prev })
	e.v.Origin = image.Pt(0, off)
	th := e.h.Theme()
	frameAt := func(now time.Time) {
		e.ops.Reset()
		e.now = now
		paint.FillShape(&e.ops, th.C.Bg, clip.Rect{Max: image.Pt(winW, winH)}.Op())
		tr := op.Offset(image.Pt(0, off)).Push(&e.ops)
		gtx := layout.Context{Ops: &e.ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(winW, 700)), Now: now, Source: e.r.Source()}
		e.v.Layout(gtx)
		tr.Pop()
		e.r.Frame(&e.ops)
	}
	move := func(x, y float32, now time.Time) {
		e.clock += 10 * time.Millisecond
		e.r.Queue(pointer.Event{Kind: pointer.Move, Position: f32.Pt(x, y), Source: pointer.Mouse, Time: e.clock})
		frameAt(now)
	}
	s := e.v.s
	// message 10's row at the list's top
	k := -1
	for i, it := range s.items {
		if it.kind == itMsg && it.key == msgs[10].TS {
			k = i
		}
	}
	if k < 0 {
		t.Fatal("no row for message 10")
	}
	s.list.Position = layout.Position{First: k, BeforeEnd: true}
	now := e.now
	for i := 0; i < 3; i++ {
		now = now.Add(20 * time.Millisecond)
		frameAt(now)
	}
	if s.visible[0] != k || s.pos[k] != 0 {
		t.Fatalf("message 10's row is at %d (first row %d), not at the list's top: the case tests nothing", s.pos[k], s.visible[0])
	}
	p := rowPoint(t, s, msgs[10].TS)
	now = now.Add(time.Second)
	move(p.X, p.Y, now)
	now = now.Add(20 * time.Millisecond)
	frameAt(now)
	if s.tbFor != k {
		t.Fatal("no toolbar over the top row")
	}
	b := toolbarButtonAt(t, s, tbAddReact)
	now = now.Add(time.Second)
	move(b.X, b.Y, now)
	for i := 0; i < 15; i++ { // the tip's 60 ms delay and 80 ms fade
		now = now.Add(20 * time.Millisecond)
		frameAt(now)
	}
	if a := s.tilt.tipAlpha(layout.Context{Now: now}, s.tbTipKey); a < 1 || s.tbTipKey == "" {
		t.Fatalf("the button's tooltip isn't up (key %q, alpha %v): the case tests nothing", s.tbTipKey, a)
	}
	img := pixeltest.Render(t, winW, winH, &e.ops)
	bg := img.RGBAAt(5, off-20)
	inked := 0
	for y := off - 40; y < off; y++ {
		for x := int(b.X) - 20; x <= int(b.X)+20; x++ {
			c := img.RGBAAt(x, y)
			if max(absDiff(int(c.R), int(bg.R)), absDiff(int(c.G), int(bg.G)), absDiff(int(c.B), int(bg.B))) > 60 {
				inked++
			}
		}
	}
	if inked < 100 {
		t.Errorf("over the list's top, above the hovered button, %d pixels of tooltip: it's cut off at the list's top, where Slack's shows over the header", inked)
	}
}
