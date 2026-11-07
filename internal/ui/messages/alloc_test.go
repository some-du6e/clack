package messages

import (
	"image"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// themeHost is nilHost with a real theme (shaping, no core, no images).
type themeHost struct {
	nilHost
	th *theme.Theme
}

func (h themeHost) Theme() *theme.Theme { return h.th }

// rowAllocsBefore is the steady-state allocations of one frame of
// rowAllocsFrame before the Taut boundaries (CI, go1.24, linux/amd64).
// The unpatched, unstyled row must not allocate more.
const rowAllocsBefore = -1

func testRowEntry(ts string) entry {
	return entry{rev: 1, m: core.Message{TS: ts, Channel: "C1", User: "U1", Name: "Alice", Text: "hello <@U2>",
		Doc: richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.Paragraph, Spans: []richtext.Span{
			{Text: "hello "}, {Kind: richtext.User, ID: "U2", Text: "@bob"}, {Text: " and a longer tail of words", Style: richtext.Bold},
		}}}},
		Reactions: []core.Reaction{{Name: "tada", Count: 2, Users: []string{"U1", "U2"}, Self: true}, {Name: "eyes", Count: 1, Users: []string{"U3"}}},
	}}
}

// rowAllocsFrame lays out a header row (hovered) and a grouped row
// (hovered, so the gutter time and the add-reaction button draw) once.
func rowAllocsFrame() func() {
	th := theme.New(false)
	h := themeHost{th: th}
	e1, e2 := testRowEntry("1700000000.000100"), testRowEntry("1700000010.000100")
	r1, r2 := newRow(&e1), newRow(&e2)
	rc := &rowCtx{h: h, th: th, st: styleFor(h), c: colorsFor(th), now: time.Unix(1700000100, 0), selfID: "U1", usersRev: 1,
		origin: func(p image.Point) image.Point { return p }}
	ops := new(op.Ops)
	return func() {
		ops.Reset()
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(800, 2000)), Now: rc.now}
		rc.header, rc.hovered = true, true
		r1.layout(gtx, rc)
		rc.header = false
		r2.layout(gtx, rc)
	}
}

func TestRowLayoutAllocs(t *testing.T) {
	frame := rowAllocsFrame()
	for i := 0; i < 10; i++ {
		frame()
	}
	n := testing.AllocsPerRun(200, frame)
	t.Logf("row layout: %v allocs/frame (before: %v)", n, rowAllocsBefore)
	if rowAllocsBefore < 0 {
		t.Errorf("baseline run: set rowAllocsBefore = %v", n)
	} else if n > rowAllocsBefore {
		t.Errorf("unpatched row layout allocates %v per frame, %v before the Taut boundaries", n, rowAllocsBefore)
	}
}
