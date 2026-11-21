package messages

// Round 3's review, a MINOR: a press and a drag on a video player (its
// scrubber, its volume) also started the list's text selection, which
// then grew across rows and auto-scrolled at the list's edges. On the
// round-3 head (0cbe7e63) these run the same way.

import (
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// tileMedia takes a lone video's tile and draws nothing on it.
type tileMedia struct{}

func (tileMedia) MediaTile(f *richtext.File, files int, overflow bool) bool {
	return f != nil && f.IsVideo && files == 1 && !overflow
}

func (tileMedia) LayoutMedia(gtx layout.Context, s richview.MediaSlot) layout.Dimensions {
	return layout.Dimensions{Size: gtx.Constraints.Max}
}

// mediaClickHost is the click tests' host whose rows have players.
type mediaClickHost struct{ *fileClickHost }

func (mediaClickHost) MessageMedia(pane, ch, ts string, h MediaRowHost) richview.Media {
	return tileMedia{}
}

func TestVideoTilePressStartsNoSelection(t *testing.T) {
	fs := filesFake(t)
	app, _ := newTestApp(t)
	filesSeed(fs)(app.S)
	h := mediaClickHost{&fileClickHost{coreHost: coreHost{app: app, th: theme.New(false)}}}
	cv := NewChannelView(h, fakeslack.FilesChannelID)
	cv.Preview = true
	var r input.Router
	ops := new(op.Ops)
	now := time.Now()
	size := image.Pt(1600, 9000)
	frame := func() {
		ops.Reset()
		now = now.Add(20 * time.Millisecond)
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(size), Now: now, Source: r.Source()}
		cv.Layout(gtx)
		r.Frame(ops)
	}
	var tile image.Rectangle
	until(t, 8*time.Second, "the video's tile", frame, func() bool {
		for _, fr := range cv.FileRects() {
			if fr.File.IsVideo && fr.File.ID == fakeslack.FilesVideoID {
				// the lone video's message: its only file
				n := 0
				for _, o := range cv.FileRects() {
					if o.TS == fr.TS {
						n++
					}
				}
				if n == 1 {
					tile = fr.R
					return true
				}
			}
		}
		return false
	})
	for k := 0; k < 6; k++ {
		frame()
	}
	c := tile.Min.Add(tile.Max).Div(2)
	at := 2 * time.Second
	pt := func(p image.Point) f32.Point { return f32.Pt(float32(p.X), float32(p.Y)) }
	r.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pt(c), Time: at},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pt(c), Time: at + time.Millisecond},
		// a move with the button held: the router makes it the drag
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pt(c.Sub(image.Pt(0, 200))), Time: at + 50*time.Millisecond},
	)
	frame()
	frame()
	if cv.s.sel.dragging {
		t.Error("a press on the video's tile started the list's text selection")
	}
	r.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pt(c.Sub(image.Pt(0, 200))), Time: at + 80*time.Millisecond})
	frame()
}
