package ui

import (
	"fmt"
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"

	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/hovercard"
	"github.com/some-du6e/clack/internal/ui/richview"
)

// A canvas's channel card follows its mention (Quip's positioners
// measure the bubble's anchor again after every change to the document;
// hovercard's package comment). hover-card-anchor (α.127) made it follow
// only while the pointer was on the mention: with the pointer resting on
// the card, a collaborator's block inserted above the mention moved the
// mention and left the card where the mention had been. These open the
// card from a real hover over a canvas on the fake Slack, rest the
// pointer on the card and change the canvas under it.

const (
	hccFile    = "F0CANVFOL01"
	hccChannel = "C0FAKE00000"
	hccMark    = "Talk in "
	hccAdded   = "A collaborator added this line above the mention."
)

// painted is a body's text and when it last painted.
type painted struct {
	text string
	at   time.Time
}

type hccEnv struct {
	*perfEnv
	t     *testing.T
	ctl   *hovercard.Controller
	views map[*richview.View]painted // the painted bodies
	body  *richview.View             // the mention's
	card  image.Rectangle            // the open card, as it first drew
	gen   int
}

// newHCCEnv opens a long canvas full-page with a channel mention near its
// top, over a live socket (a collaborator's edit is document_update), and
// opens the mention's card from a real hover.
func newHCCEnv(t *testing.T) *hccEnv {
	t.Setenv("CLACK_REDUCE_MOTION", "1") // wheel scrolls land at once
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := &hccEnv{perfEnv: startEnv(t, fs, t.TempDir()), t: t, views: map[*richview.View]painted{}}
	richview.PaintedHook = func(v *richview.View) { e.views[v] = painted{v.TextRange(0, v.PlainLen()), time.Now()} }
	t.Cleanup(func() { richview.PaintedHook = nil })
	e.waitReady()
	e.ctl = &e.a.hover.ctl
	cv, ok := e.a.Core().Conv(hccChannel)
	if !ok {
		t.Fatalf("no %s", hccChannel)
	}
	blocks := []canvas.Block{
		{ID: "temp:C:FOL0001", Kind: canvas.Paragraph, Spans: []richtext.Span{{Kind: richtext.Text, Text: "Notes for the week."}}},
		{ID: "temp:C:FOL0002", Kind: canvas.Paragraph, Spans: []richtext.Span{{Kind: richtext.Text, Text: hccMark},
			{Kind: richtext.Channel, ID: hccChannel, Text: "#" + cv.Name}, {Kind: richtext.Text, Text: " today."}}},
	}
	for i := 0; i < 40; i++ { // long enough to scroll
		blocks = append(blocks, canvas.Block{ID: fmt.Sprintf("temp:C:FOL1%03d", i), Kind: canvas.Paragraph,
			Spans: []richtext.Span{{Kind: richtext.Text, Text: fmt.Sprintf("Filler paragraph %d of the long canvas.", i)}}})
	}
	fs.AddCanvas(fakeslack.FakeCanvas{ID: hccFile, Title: "Follow", Doc: canvas.Doc{Blocks: blocks}})
	c := e.a.data.core
	c.SetRealtime(true)
	e.until("the socket", 20*time.Second, func() bool { return c.RTM != nil && c.RTM.Connected() })
	e.a.OpenCanvasFull(hccFile)
	e.until("the mention's paragraph", 20*time.Second, func() bool { e.body = e.find(hccMark); return e.body != nil })
	e.settle(500 * time.Millisecond)
	e.body = e.find(hccMark) // names that came late may have made it anew
	m := e.mention()
	e.move(f32.Pt(float32(m.Min.X+m.Dx()/2), float32(m.Min.Y+m.Dy()/2)))
	e.until("the canvas card", 5*time.Second, func() bool {
		req, _, open := e.ctl.Open()
		return open && req.Canvas && req.ChannelID == hccChannel && e.a.hover.cvc != nil && !e.a.hover.cvc.Rect().Empty()
	})
	e.card = e.a.hover.cvc.Rect()
	_, e.gen, _ = e.ctl.Open()
	if e.card.Min.Y < m.Max.Y {
		t.Fatalf("card %v not under its mention %v", e.card, m)
	}
	return e
}

// find is the body showing text that painted last (nil: none).
func (e *hccEnv) find(text string) *richview.View {
	var got *richview.View
	var at time.Time
	for v, p := range e.views {
		if strings.Contains(p.text, text) && p.at.After(at) {
			got, at = v, p.at
		}
	}
	return got
}

// at is the mention's body in the window, as the last frame laid it out.
func (e *hccEnv) at() image.Rectangle {
	full, _, ok := e.r.AreaBounds(e.body)
	if !ok {
		e.t.Fatal("the mention's paragraph wasn't drawn")
	}
	return full
}

// mention is the #channel's window box (its first line), learned from a
// fresh pointer event on its body.
func (e *hccEnv) mention() image.Rectangle {
	e.t.Helper()
	b := e.at()
	e.move(f32.Pt(float32(b.Min.X+4), float32(b.Min.Y+b.Dy()/2)))
	org, ok := e.body.WindowOrigin()
	if !ok || !e.body.Pointer() {
		e.t.Fatalf("the pointer at %v isn't on the mention's paragraph %v", b.Min, b)
	}
	var r image.Rectangle
	for _, l := range e.body.LinkRects() {
		if l.Kind == richview.HitChannel && l.ID == hccChannel && (r.Empty() || l.R.Min.Y == r.Min.Y) {
			r = r.Union(l.R)
		}
	}
	if r.Empty() {
		e.t.Fatal("no #channel in the mention's paragraph")
	}
	return r.Add(org)
}

// rest puts the pointer on the card, dy below its top, and checks the card
// stays (no trigger reports there: the card is what holds it). e.card is
// where it is then, placed by its mention's layout (Follow).
func (e *hccEnv) rest(dy int) f32.Point {
	p := f32.Pt(float32(e.card.Min.X+40), float32(e.card.Min.Y+dy))
	e.move(p)
	e.settle(400 * time.Millisecond)
	if !e.ctl.OverCard() || !e.open() {
		e.t.Fatalf("the card closed with the pointer resting on it at %v (card %v)", p, e.card)
	}
	if r := e.a.hover.cvc.Rect(); r != e.card {
		e.t.Logf("the card went from %v to %v when the pointer left its mention", e.card, r)
		e.card = r
	}
	return p
}

// open reports whether the card that opened is still open (not closed,
// not another one).
func (e *hccEnv) open() bool {
	req, gen, open := e.ctl.Open()
	return open && req.Canvas && gen == e.gen && e.a.hover.cvc != nil
}

// insert has a collaborator insert n paragraphs right above the mention,
// and waits for the mention's paragraph to move down.
func (e *hccEnv) insert(n int) image.Point {
	e.t.Helper()
	before := e.at()
	md := hccAdded
	for i := 1; i < n; i++ {
		md += fmt.Sprintf("\n\nAnother added line, %d.", i)
	}
	e.fs.InsertCanvasAsOther(hccFile, 1, md)
	e.until("the collaborator's edit", 20*time.Second, func() bool { return e.find(hccAdded) != nil })
	e.until("the mention's paragraph to move", 5*time.Second, func() bool {
		full, _, ok := e.r.AreaBounds(e.body)
		return ok && full.Min.Y > before.Min.Y
	})
	return e.at().Min.Sub(before.Min)
}

// A collaborator inserts a block above the mention while the pointer
// rests on the card: the card goes with its mention and stays open.
func TestHoverCardAnchorCanvasFollowsEdit(t *testing.T) {
	e := newHCCEnv(t)
	e.rest(e.card.Dy() * 3 / 4) // low on the card: it moves down under the pointer
	d := e.insert(1)
	e.settle(300 * time.Millisecond)
	if !e.open() {
		t.Fatal("the card closed when the mention moved under it")
	}
	if e.find(hccMark) != e.body {
		t.Fatal("the edit replaced the mention's paragraph (a new trigger)")
	}
	got := e.a.hover.cvc.Rect()
	if want := e.card.Add(d); got != want {
		t.Errorf("card %v after the mention moved by %v, want %v (it opened at %v)", got, d, want, e.card)
	}
	if !e.ctl.OverCard() {
		t.Error("the pointer is off the card")
	}
}

// The mention moves the card out from under the resting pointer: the card
// goes with it, and the pointer leaving it starts the bubble's 250ms
// dismiss, as Chromium's hover update after the layout tells the bubble.
func TestHoverCardAnchorCanvasSlidesOffPointer(t *testing.T) {
	e := newHCCEnv(t)
	e.rest(4) // at the card's top edge: any move down leaves the pointer
	d := e.insert(3)
	moved := false
	var closedAt, followedAt time.Time
	e.until("the card to close", 3*time.Second, func() bool {
		if !e.open() {
			closedAt = time.Now()
			return true
		}
		if !moved && e.a.hover.cvc.Rect() == e.card.Add(d) {
			moved, followedAt = true, time.Now()
		}
		return false
	})
	if !moved {
		t.Errorf("the card closed (or another opened) without following its mention by %v", d)
	}
	if moved && closedAt.Sub(followedAt) < hovercard.HideDelay-50*time.Millisecond {
		t.Errorf("the card closed %v after it slid off the pointer, want the %v dismiss", closedAt.Sub(followedAt), hovercard.HideDelay)
	}
}

// A wheel on the card scrolls nothing under it (Quip's bubble is in the
// document's body, outside the canvas's scroller), so the card stays by
// its mention; the same wheel off the card scrolls the canvas.
func TestHoverCardAnchorCanvasTakesWheel(t *testing.T) {
	e := newHCCEnv(t)
	p := e.rest(e.card.Dy() / 2)
	before := e.at()
	e.scroll(image.Pt(int(p.X), int(p.Y)), 200)
	e.settle(400 * time.Millisecond)
	if got := e.at(); got != before {
		t.Errorf("a wheel on the card scrolled the canvas: its paragraph went from %v to %v", before, got)
	}
	if !e.open() || e.a.hover.cvc.Rect() != e.card {
		t.Errorf("the card moved or closed under a wheel on it (now %v, opened at %v)", e.a.hover.cvc, e.card)
	}
	// off the card: the canvas scrolls (the wheel above means something)
	off := image.Pt(e.card.Max.X+200, e.card.Max.Y+150)
	e.scroll(off, 200)
	e.settle(400 * time.Millisecond)
	if full, _, ok := e.r.AreaBounds(e.body); ok && full == before {
		t.Errorf("a wheel on the canvas at %v didn't scroll it", off)
	}
}
