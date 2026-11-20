package canvasview

import (
	"image"
	"slices"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/inspect"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// pickHost is the window as the inspect tool sees it, for the canvas on
// its own.
type pickHost struct{ th *theme.Theme }

func (h pickHost) Theme() *theme.Theme { return h.th }
func (h pickHost) Describer() func(*inspect.Report, *inspect.Redactor) {
	return func(*inspect.Report, *inspect.Redactor) {}
}
func (h pickHost) ConvKind(string) string    { return "" }
func (h pickHost) Regions() []inspect.Region { return nil }
func (h pickHost) Toast(string)              {}
func (h pickHost) Post(func())               {}
func (h pickHost) Invalidate()               {}
func (h pickHost) ConfigDir() string         { return "" }
func (h pickHost) Frozen(bool)               {}

// Karim's "i cant select the canvas title": the inspect picker reaches
// the title inside the canvas document, Quip's title section
// (div.section[data-section-style=48] > div.content), outlined as the
// column-wide title line, and not the header-image band above it. Both
// title draws: the document's opening heading (the kitchen sink) and the
// file's title over a document without one (the DM canvas).
func TestInspectPicksCanvasTitle(t *testing.T) {
	for _, c := range []struct {
		id      string
		heading bool
	}{{fakeslack.CanvasChannel, true}, {fakeslack.CanvasDM, false}} {
		t.Run(c.id, func(t *testing.T) { inspectPicksTitle(t, c.id, c.heading) })
	}
}

func inspectPicksTitle(t *testing.T, id string, heading bool) {
	h, _ := newEnv(t, false)
	v := New(h, id, Tab, Actions{}) // no view bar: the document starts at the top
	sz := image.Pt(1100, 700)
	var r input.Router
	ops := new(op.Ops)
	tool := inspect.New(pickHost{th: h.th}, key.Filter{Name: "D", Required: key.ModShortcut | key.ModAlt | key.ModShift})
	gtxNow := func() layout.Context {
		return layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(sz), Now: time.Now(), Source: r.Source()}
	}
	frame := func() {
		ops.Reset()
		gtx := gtxNow()
		g := tool.Begin(gtx)
		v.Layout(g)
		tool.End(gtx)
		r.Frame(ops)
	}
	deadline := time.Now().Add(20 * time.Second)
	for v.Doc() == nil {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the canvas")
		}
		frame()
		time.Sleep(10 * time.Millisecond)
	}
	for i := 0; i < 5; i++ {
		frame()
	}
	if titleSection(v.blocks) != heading {
		t.Fatalf("the fixture canvas opens with a title heading: %v, want %v", titleSection(v.blocks), heading)
	}

	// The record: the section, the content inside it, the column wide
	// and the title line tall, at the title's place.
	var f slackc.InspectFrame
	ops.Reset()
	gtx := gtxNow()
	slackc.InspectBegin(&f, r.Source())
	v.Layout(gtx)
	slackc.InspectEnd()
	r.Frame(ops)
	x0, cw := v.column(gtx, sz.X)
	sec, cnt := -1, -1
	for i := range f.Nodes {
		n := &f.Nodes[i]
		if n.Kind != slackc.InspectElement {
			continue
		}
		switch {
		case slices.Equal(n.Classes(), []string{"section"}) && slices.Equal(n.Attrs(), []string{"data-section-style", "48"}):
			sec = i
		case slices.Equal(n.Classes(), []string{"content"}) && sec >= 0 && n.Parent == sec:
			cnt = i
		}
	}
	if sec < 0 || cnt < 0 {
		t.Fatalf("no div.section[data-section-style=48] > div.content recorded (section %d, content %d)", sec, cnt)
	}
	for _, i := range []int{sec, cnt} {
		n := &f.Nodes[i]
		if n.Tag() != "div" || !n.Sized() || n.Size.X != cw || n.Size.Y < gtx.Dp(titleLine) {
			t.Errorf("%s %v sized %v (has an area: %v), want %d wide, at least %d tall", n.Tag(), n.Classes(), n.Size, n.Sized(), cw, gtx.Dp(titleLine))
		}
	}
	th := f.Nodes[sec].Size.Y

	// The picker: down the title's first characters.
	tool.Toggle()
	frame()
	frame()
	if !tool.Active() {
		t.Fatal("no inspect session")
	}
	top := gtx.Dp(docTop)
	x := x0 + 4
	var seen []string
	picked := false
	for y := top - 30; y < top+th; y += 2 {
		r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(x), float32(y))})
		frame()
		label, rect, ok := tool.Hovered()
		if ok && (len(seen) == 0 || seen[len(seen)-1] != label) {
			seen = append(seen, label)
		}
		title := ok && label == "div.content"
		switch {
		case y < top && title:
			t.Errorf("the header-image band (y %d) picks the title, outlined %v", y, rect)
		case y >= top+1 && y < top+th-1 && !title:
			t.Errorf("the title at %d,%d doesn't pick div.content: %q (%v)", x, y, label, ok)
		case title:
			picked = true
			if want := image.Rect(x0, top, x0+cw, top+th); rect != want {
				t.Errorf("the title outlined %v, want %v", rect, want)
			}
		}
	}
	if !picked {
		t.Errorf("the canvas title never picked; outlined: %q", seen)
	}
}
