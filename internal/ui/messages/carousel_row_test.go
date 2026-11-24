package messages

import (
	"encoding/json"
	"fmt"
	"image"
	"strings"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// Synthetic fixtures only.

const tsCarousel = "1762600000.000100"

// carouselBlocks is a carousel of n cards, titled Pick 0 to Pick n-1.
func carouselBlocks(n int, extra string) json.RawMessage {
	var cs []string
	for i := range n {
		cs = append(cs, fmt.Sprintf(`{"type":"card","block_id":"c%d","title":{"type":"plain_text","text":"Pick %d"}}`, i, i))
	}
	return json.RawMessage(`[{"type":"carousel","block_id":"cr"` + extra + `,"elements":[` + strings.Join(cs, ",") + `]}]`)
}

// A carousel in a channel row, whose body lays out with loose constraints
// as every row's does, draws its cards across the column and takes its
// right arrow's click and a sideways wheel. Before, relayout's size left
// the carousel's box out: the cards drew, and took input, in a zero-width
// clip, and the body was blank (bk-gallery-shots 45315967597).
func TestCarouselInARow(t *testing.T) {
	e := newCtxEnvIn(t, tGen, []slack.Message{{Type: "message", TS: tsCarousel, User: "UALICE", Text: "Weekly picks", Blocks: carouselBlocks(4, "")}})
	r := e.row(tsCarousel)
	if r == nil || r.rv == nil {
		t.Fatal("no carousel row")
	}
	cs := r.rv.CarouselRects()
	if len(cs) != 1 || len(cs[0].Cards) != 4 {
		t.Fatalf("the row's carousels %+v, want one of 4 cards", cs)
	}
	c := cs[0]
	// the body's rect is its View's laid out size
	if r.bodyRect.Dx() < c.R.Max.X || r.bodyRect.Dy() < c.R.Max.Y || c.R.Dx() < 340 {
		t.Fatalf("the row's body %v doesn't hold the carousel %v, the column wide", r.bodyRect, c.R)
	}
	if c.Right.Empty() {
		t.Fatalf("no right arrow in a column of %dpx for 4 cards", c.R.Dx())
	}
	s := e.v.s
	top := s.pos[s.sel.keyIdx[tsCarousel]]
	at := func(p image.Point) f32.Point {
		return f32.Pt(float32(p.X+r.bodyRect.Min.X)+.5, float32(top+r.bodyRect.Min.Y+p.Y)+.5)
	}
	e.press(at(c.Right.Min.Add(c.Right.Size().Div(2))), pointer.ButtonPrimary, 0)
	for range 20 { // the 160ms scroll
		e.frame()
	}
	after := r.rv.CarouselRects()[0].Scroll
	if after <= 0 {
		t.Errorf("the right arrow's click: scroll %v, want a page", after)
	}
	p := at(image.Pt(c.R.Min.X+40, c.R.Min.Y+c.R.Dy()/2))
	e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p},
		pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: p, Scroll: f32.Pt(-60, 0)})
	e.frame()
	e.frame()
	if got := r.rv.CarouselRects()[0].Scroll; got != after-60 {
		t.Errorf("a sideways wheel of 60px back: scroll %v, want %v", got, after-60)
	}
}

// A carousel root's plain text is the message's own text: the blocks give
// none (a card's is nothing but in a message summary, a carousel's its
// label only when it shows), and convertMessageToPlainText falls back to
// getMessageText. A visible label is the carousel's text.
func TestBroadcastPreambleCarouselRoot(t *testing.T) {
	for _, c := range []struct {
		extra, want string
	}{
		{"", "Weekly picks"},
		{`,"label":"Picks","is_label_visible":true`, "Picks"},
	} {
		sm := slack.Message{TS: "1700000001.000100", ThreadTS: "1700000001.000100", Text: "Weekly picks", Blocks: carouselBlocks(2, c.extra)}
		root := core.Message{TS: sm.TS, ThreadTS: sm.ThreadTS, Text: sm.Text, Doc: richtext.FromMessage(nilRes{}, &sm)}
		if len(root.Doc.Blocks) != 1 || root.Doc.Blocks[0].Kind != richtext.CarouselBlock {
			t.Fatalf("%q: the root's blocks %+v, want the carousel", c.extra, root.Doc.Blocks)
		}
		s := &listState{}
		s.snap.msgs = []entry{{m: root, rev: 1}}
		r := &row{m: core.Message{TS: "1700000002.000200", ThreadTS: root.TS, Broadcast: true}}
		if got := r.rootPreview(&rowCtx{list: s, usersRev: 1}); got != c.want {
			t.Errorf("%q: the preamble's preview %q, want %q", c.extra, got, c.want)
		}
	}
}
