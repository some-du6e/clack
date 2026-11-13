package canvasview

// alpha.24's crash (Karim, macOS: "I opened the clack testing canvas and
// stuff crashed") and the boundary that keeps a canvas panic inside the
// canvas. The fixture is made by hand with the structure of #clack's
// channel canvas: a title with an animated custom emoji, the same text as
// its opening H1, and one line of Quip's new-document default content.

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/gif"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/richtext"
)

const partyCatURL = "https://emoji.example/party-cat/0123456789abcdef.gif"

const shortCanvasHTML = `<div class="quip-canvas-content">` +
	`<h1 id="temp:C:CLT1h1">Testing things <control id="temp:C:CLT1e1" data-remapped="true"><img src='https://slack-imgs.com/?c=1&o1=gu&url=emoji/party-cat' alt="party-cat" data-is-slack style='width: 30px; height: 30px'></control></h1>` +
	`<p id="temp:C:CLT1p1" class="default-content line">Time to document your genius!</p></div>`

// animatedGIF is a 3-frame 64px GIF (the custom emoji).
func animatedGIF(t *testing.T) []byte {
	t.Helper()
	pal := color.Palette{color.Transparent, color.NRGBA{R: 0xe0, G: 0x1e, B: 0x5a, A: 0xff}, color.NRGBA{R: 0x36, G: 0xc5, B: 0xf0, A: 0xff}}
	g := &gif.GIF{}
	for i := 0; i < 3; i++ {
		im := image.NewPaletted(image.Rect(0, 0, 64, 64), pal)
		for y := 8 * i; y < 64; y++ {
			for x := 0; x < 64; x++ {
				im.SetColorIndex(x, y, uint8(1+(x/8+i)%2))
			}
		}
		g.Image = append(g.Image, im)
		g.Delay = append(g.Delay, 4)
		g.Disposal = append(g.Disposal, gif.DisposalBackground)
	}
	var b bytes.Buffer
	if err := gif.EncodeAll(&b, g); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// shortCanvas is the fixture as core.Canvas returns it (the emoji
// resolved to the workspace's animated image, title_blocks spans).
func shortCanvas(t *testing.T) *core.CanvasDoc {
	t.Helper()
	doc, err := canvas.Parse(shortCanvasHTML)
	if err != nil || len(doc.Blocks) != 2 || doc.Blocks[0].Kind != canvas.Heading1 {
		t.Fatalf("fixture: %v %+v", err, doc.Blocks)
	}
	doc.Walk(func(b *canvas.Block) bool {
		for i := range b.Spans {
			if b.Spans[i].Kind == richtext.Emoji {
				b.Spans[i].URL = partyCatURL
			}
		}
		return true
	})
	return &core.CanvasDoc{Doc: doc, Files: map[string]core.CanvasFile{}, At: time.Now(), Info: core.CanvasInfo{
		ID: "F0CLACKT001", Title: "Testing things :party-cat:", Creator: "U0FAKE00001", Editable: true, Updated: time.Now().Add(-18 * time.Hour).Unix(),
		TitleSpans: []richtext.Span{{Kind: richtext.Text, Text: "Testing things "}, {Kind: richtext.Emoji, ID: "party-cat", URL: partyCatURL}, {Kind: richtext.Text, Text: " "}},
	}}
}

type pointerRig struct {
	t      *testing.T
	v      *View
	router input.Router
	sz     image.Point
}

func (r *pointerRig) frame() {
	r.t.Helper()
	ops := new(op.Ops)
	gtx := gtxFor(ops, r.sz)
	gtx.Source = r.router.Source()
	r.v.Layout(gtx)
	r.router.Frame(ops)
}

func (r *pointerRig) click(x, y float32) {
	r.t.Helper()
	pt := f32.Pt(x, y)
	r.router.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pt},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pt},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pt},
	)
	r.frame()
	r.frame()
}

// The crash: on a short canvas a click under the text starts a new block
// (an empty row appended to the blocks); a click on the title heading's
// line, beside its text, opens the heading instead, which first ends that
// empty session and so drops the row. That ran inside the list's layout,
// after layoutDoc had counted the rows, so the list read v.blocks past its
// end (index out of range in its row func; the deferred Pops then
// panicked with "stack push and pop must not cross macro boundary" and
// the app died).
func TestShortCanvasClickUnderThenOnTitle(t *testing.T) {
	h, _ := newEnv(t, true)
	gifData := animatedGIF(t)
	h.img = imgcache.New(func(ctx context.Context, u string) ([]byte, error) {
		if strings.HasPrefix(u, "https://emoji.example/") {
			return gifData, nil
		}
		b, _, err := h.a.Fetch(ctx, u)
		return b, err
	}, "", nil)
	v := New(h, "F0CLACKT001", Tab, Actions{})
	v.live.Store(true) // no files.open for the hand-made canvas
	v.doc, v.gen = shortCanvas(t), 1
	r := &pointerRig{t: t, v: v, sz: image.Pt(900, 700)}
	for i := 0; i < 20; i++ { // the emoji decodes and animates
		r.frame()
		time.Sleep(10 * time.Millisecond)
	}
	if !animatedAny(h.img) {
		t.Error("the title's custom emoji isn't animating")
	}
	if len(v.blocks) != 2 {
		t.Fatalf("%d blocks", len(v.blocks))
	}
	// Quip's isEventBelowLastSection: a click under the unedited default
	// line focuses it, emptied (no new block)
	r.click(450, 330)
	if s := v.edit.cur; s == nil || s.target != v.blocks[1] || !s.def || len(v.blocks) != 2 {
		t.Fatalf("the click under the default line: blocks %d, session %+v", len(v.blocks), s)
	}
	v.edit.discard(v)
	r.frame()
	// the crash's path: a last line with text, where a click under it
	// starts a new block
	v.blocks[1].b.Default = false
	r.frame()
	for round, title := range []f32.Point{{X: 700, Y: 118}, {X: 600, Y: 110}} { // the title's line, under the 100px header band
		r.click(450, 330) // under the text: a new block
		if v.edit.cur == nil || v.edit.cur.place == nil || len(v.blocks) != 3 {
			t.Fatalf("round %d: the click under the text didn't start a new block (blocks %d, session %+v)", round, len(v.blocks), v.edit.cur)
		}
		r.click(title.X, title.Y) // the title's line: the empty new block goes away
		for i := 0; i < 3; i++ {
			r.frame()
		}
		if v.crashed != nil {
			t.Fatalf("round %d: the canvas crashed: %v", round, v.crashed)
		}
		if len(v.blocks) != 2 || v.edit.cur != nil && v.edit.cur.place != nil {
			t.Fatalf("round %d: %d blocks, session %+v after the click on the title; want 2, no new block", round, len(v.blocks), v.edit.cur)
		}
		v.edit.discard(v)
	}
}

func animatedAny(c *imgcache.Cache) bool {
	for _, px := range []int{20, 22, 26, 32, 34, 40, 44, 52, 64} {
		if c.Animated(partyCatURL, px) {
			return true
		}
	}
	return false
}

// The title is drawn once: the canvas's opening heading is its title
// section (Quip's extractTitleMetadata), not a block under a separate
// title.
func TestTitleSectionDrawnOnce(t *testing.T) {
	h, _ := newEnv(t, false)
	v := New(h, "F0CLACKT001", Tab, Actions{})
	v.live.Store(true)
	v.doc, v.gen = shortCanvas(t), 1
	r := &pointerRig{t: t, v: v, sz: image.Pt(900, 700)}
	r.frame()
	if !titleSection(v.blocks) {
		t.Fatal("the opening H1 isn't the title section")
	}
	if v.title != nil {
		t.Error("the file title was laid out as well as the title section")
	}
	// a canvas that opens with a paragraph keeps the file's title
	d := shortCanvas(t)
	d.Doc.Blocks = d.Doc.Blocks[1:]
	v2 := New(h, "F0CLACKT002", Tab, Actions{})
	v2.live.Store(true)
	v2.doc, v2.gen = d, 1
	(&pointerRig{t: t, v: v2, sz: image.Pt(900, 700)}).frame()
	if titleSection(v2.blocks) || v2.title == nil {
		t.Error("a canvas without an opening heading lost its title")
	}
	// a blank H1 is still the title section (Quip's isTitleSection; its
	// TitleSectionOverlay shows "Your canvas title"); a blank H2 isn't
	if !titleSection([]*blk{{b: &canvas.Block{Kind: canvas.Heading1, Spans: []richtext.Span{{Kind: richtext.Text, Text: "  "}}}}}) {
		t.Error("a blank opening H1 isn't the title section")
	}
	if titleSection([]*blk{{b: &canvas.Block{Kind: canvas.Heading2, Spans: []richtext.Span{{Kind: richtext.Text, Text: "  "}}}}}) {
		t.Error("a blank H2 counted as the title")
	}
}

// Any panic in a canvas's layout stays in the canvas: the frame's ops are
// rolled back (the caller's own pushes and macros pop cleanly), the panic
// and its stack are logged, the canvas shows its error state, and Try
// again reloads it.
func TestLayoutPanicRecovered(t *testing.T) {
	h, _ := newEnv(t, false)
	v := New(h, fakeslack.CanvasChannel, Full, Actions{})
	s := newShooter(t, image.Pt(900, 700))
	settle(t, s, v, "the canvas", func() bool { return v.Doc() != nil })
	s.frame(func(gtx layout.Context) { v.Layout(gtx) })
	if len(v.blocks) < 3 {
		t.Fatal("no blocks")
	}
	v.blocks = append(v.blocks[:1:1], &blk{}, v.blocks[1]) // a block with no content: nil dereference in layoutBlock

	var logBuf syncBuffer
	prev := log.Writer()
	log.SetOutput(&logBuf)
	defer log.SetOutput(prev)

	ops := new(op.Ops)
	gtx := gtxFor(ops, image.Pt(900, 700))
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("a canvas panic escaped View.Layout: %v", r)
			}
		}()
		// the caller's own state: a transform, a clip and a macro around the view
		outer := op.Offset(image.Pt(10, 20)).Push(ops)
		m := op.Record(ops)
		v.Layout(gtx)
		call := m.Stop()
		outer.Pop()
		call.Add(ops)
	}()
	if v.crashed == nil {
		t.Fatal("no error state after the panic")
	}
	out := logBuf.String()
	if !strings.Contains(out, "panic drawing canvas "+fakeslack.CanvasChannel) || !strings.Contains(out, "layoutBlock") {
		t.Errorf("log lacks the panic and its stack:\n%s", out)
	}
	if strings.Contains(out, "Launch plan") {
		t.Error("the log quotes the canvas")
	}
	// the error state draws on its own, frame after frame
	for i := 0; i < 3; i++ {
		s.frame(func(gtx layout.Context) { v.Layout(gtx) })
	}
	// Try again: reloaded and laid out afresh
	v.retry.Click()
	s.frame(func(gtx layout.Context) { v.Layout(gtx) })
	if v.crashed != nil {
		t.Fatal("Try again didn't leave the error state")
	}
	deadline := time.Now().Add(20 * time.Second)
	for !(v.Doc() != nil && len(v.blocks) > 3) {
		if time.Now().After(deadline) {
			v.mu.Lock()
			t.Fatalf("not reloaded: doc %v err %v loading %v gen %d blocks %d crashed %v", v.doc != nil, v.err, v.loading, v.gen, len(v.blocks), v.crashed)
		}
		s.frame(func(gtx layout.Context) { v.Layout(gtx) })
		time.Sleep(10 * time.Millisecond)
	}
	if v.crashed != nil {
		t.Fatalf("crashed again: %v", v.crashed)
	}
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}
