package views

import (
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/slackc"
)

// TestDetailsDialogInspectCard: in an inspect session, channel details'
// stacked Leave dialog is an element of its own (Slack's SK modal, its
// 520 dp card), recorded over the details card it sits on, so a pick on
// the dialog is the dialog, not the card behind it (review of a614d3b3:
// its picks outlined the whole details card).
func TestDetailsDialogInspectCard(t *testing.T) {
	srv := &cdTestServer{people: 4}
	e := newCDTestEnv(t, srv, "C9")
	e.until("info", e.loaded)
	e.d.leaveRow.Click()
	e.until("leave dialog", func() bool { return e.d.x.dlg.kind == cdDlgLeave })
	var f slackc.InspectFrame
	frame := func() {
		e.ops.Reset()
		gtx := layout.Context{Ops: &e.ops, Source: e.r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(1440, 900)), Now: time.Now()}
		slackc.InspectBegin(&f, e.r.Source())
		lm := slackc.InspectOverlay("overlay (ui.detailsOverlay)") // as the shell's drawOverlay records it
		e.d.Layout(gtx)
		lm.EndCardOr(gtx, e.d.Rect())
		slackc.InspectEnd()
		e.r.Frame(&e.ops)
	}
	frame()
	frame()
	at := f32.Pt(720, 450) // the window's middle: inside the centred dialog
	e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: at})
	hits := f.ReadHits(e.r.Source(), func(time.Duration) (f32.Point, bool) { return at, true })
	best := -1
	for _, h := range hits {
		best = max(best, h.Node)
	}
	if best < 0 {
		t.Fatal("nothing under the dialog")
	}
	n := &f.Nodes[best]
	if n.Kind != slackc.InspectElement || !strings.Contains(strings.Join(n.Classes(), " "), "c-sk-modal") || n.Size.X != 520 {
		t.Errorf("picked over the Leave dialog: kind %v %q %v, size %v; want its c-sk-modal card, 520 wide", n.Kind, n.Name, n.Classes(), n.Size)
	}
}
