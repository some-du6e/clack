package popups

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

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/hovercard"
)

// hvReports sweeps the pointer over size through Gio's router and returns
// every request a trigger reported (by user, or bot when there's no
// user).
func hvReports(t *testing.T, size image.Point, draw func(gtx layout.Context)) map[string]hovercard.Request {
	t.Helper()
	var r input.Router
	var ops op.Ops
	var ctl hovercard.Controller
	ptr := image.Point{}
	ctl.Pointer = func() image.Point { return ptr }
	hovercard.SetDefault(&ctl)
	t.Cleanup(func() { hovercard.SetDefault(nil) })
	frame := func() {
		ops.Reset()
		gtx := layout.Context{Ops: &ops, Source: r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(size), Now: time.Now()}
		draw(gtx)
		r.Frame(&ops)
		ctl.Frame(gtx.Now, false, false)
	}
	out := map[string]hovercard.Request{}
	frame()
	for y := 2; y < size.Y; y += 4 {
		for x := 2; x < size.X; x += 8 {
			ptr = image.Pt(x, y)
			r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(x), float32(y))})
			frame()
			if req, ok := ctl.Pending(); ok {
				k := req.UserID
				if k == "" {
					k = "bot:" + req.BotID
				}
				if _, seen := out[k]; !seen {
					out[k] = req
				}
			}
		}
	}
	return out
}

// Search results: the sender's avatar and name are triggers (Slack's
// SearchResult: BaseAvatar showCard at 36px and MessageSender); a bot's
// result with no user opens the bot's card.
func TestHoverTriggerSearchResult(t *testing.T) {
	h := hcEnv(t)
	v := NewSearchView(h, "synthetic")
	v.msgs = []core.Message{
		{Channel: "C0FAKE00001", TS: "1700000001.000100", User: fakeslack.ProfileID, Text: "a synthetic result"},
		{Channel: "C0FAKE00001", TS: "1700000002.000100", BotID: fakeslack.AppBotID, Text: "a synthetic bot result"},
	}
	v.cards = make([]srCard, len(v.msgs))
	th := h.Theme()
	cl := colorsOf(th)
	size := image.Pt(700, 90)
	for i, want := range []string{fakeslack.ProfileID, "bot:" + fakeslack.AppBotID} {
		got := hvReports(t, size, func(gtx layout.Context) {
			v.cardNative(gtx, th, cl, i, size.X, "Sender", "", nil)
		})
		req, ok := got[want]
		if !ok || len(got) != 1 {
			t.Fatalf("result %d reported %v, want %s", i, got, want)
		}
		// the avatar (16px in, 36 wide) and the name (61px in) both report
		av := image.Rect(16, 17, 52, 53)
		if !req.Anchor.Overlaps(av) && req.Anchor.Min.X < 61 {
			t.Errorf("result %d: first trigger %v is neither the avatar nor the name", i, req.Anchor)
		}
	}
	// the avatar alone: sweep only its square
	got := hvReports(t, image.Pt(60, 60), func(gtx layout.Context) {
		v.cardNative(gtx, th, cl, 0, size.X, "Sender", "", nil)
	})
	if req, ok := got[fakeslack.ProfileID]; !ok || req.Anchor.Dx() != 36 {
		t.Errorf("avatar trigger %v", got)
	}
}
