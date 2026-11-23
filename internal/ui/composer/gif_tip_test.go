package composer

import (
	"context"
	"errors"
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// gifImgHost is ovHost with an image cache (offline: the chips draw their
// skeleton).
type gifImgHost struct {
	*ovHost
	imgs *imgcache.Cache
}

func (h gifImgHost) Images() *imgcache.Cache { return h.imgs }

// A draft GIF's "Remove GIF" tooltip (RemoveUnfurlButton) hidden by the
// TooltipManager (Escape, a menu opening) comes back once the pointer has
// left the button and returned: the chip tells the tooltip when the
// pointer is off it.
func TestRemoveGifTipComesBackAfterHide(t *testing.T) {
	app, _ := fakeslackApp(t)
	th := theme.New(false)
	h := gifImgHost{ovHost: &ovHost{slickHost: &slickHost{app: app, th: th}},
		imgs: imgcache.New(func(context.Context, string) ([]byte, error) { return nil, errors.New("offline") }, "", nil)}
	a := &attachments{h: h}
	g := &draftGif{}
	var r input.Router
	var ops op.Ops
	now := time.Unix(1000, 0)
	frame := func() {
		ops.Reset()
		gtx := layout.Context{Ops: &ops, Source: r.Source(), Now: now, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(300, 200))}
		t := op.Offset(image.Pt(40, 40)).Push(gtx.Ops)
		a.layoutGif(gtx, th, g, 64)
		t.Pop()
		r.Frame(&ops)
		now = now.Add(20 * time.Millisecond)
	}
	var at time.Duration
	move := func(p f32.Point) {
		at += 30 * time.Millisecond
		r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p, Time: at})
	}
	rest := func(d time.Duration) {
		for end := now.Add(d); now.Before(end); {
			frame()
		}
	}
	// the remove button: 20px at the chip's top right, 10px out (64-20+10, -10)
	onBtn, off := f32.Pt(40+54+10, 40-10+10), f32.Pt(250, 180)
	frame()
	move(onBtn)
	rest(300 * time.Millisecond)
	if !g.tip.Showing(now) {
		t.Fatal("control: resting on the remove button shows no \"Remove GIF\" tooltip")
	}
	popups.HideTooltips()
	rest(100 * time.Millisecond)
	if g.tip.Showing(now) {
		t.Fatal("the tooltip stayed up after HideTooltips")
	}
	move(off)
	rest(100 * time.Millisecond)
	move(onBtn)
	rest(300 * time.Millisecond)
	if !g.tip.Showing(now) {
		t.Error("off the button and back, the \"Remove GIF\" tooltip stays hidden")
	}
}
