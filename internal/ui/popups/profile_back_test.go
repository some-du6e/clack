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

	"github.com/some-du6e/clack/internal/fakeslack"
)

// backHost is the profile pane's host with a secondary-pane back stack:
// the view the pane opened over (the ui root's App.SecondaryBack).
type backHost struct {
	*tzHost
	back bool
	went int
}

func (h *backHost) SecondaryBack() bool { return h.back }
func (h *backHost) GoSecondaryBack()    { h.went++; h.back = false }

// Opened over another view (a name clicked in the thread pane: Slack's
// view.prev), the profile pane's header starts with FlexpaneBackButton
// (DG1E: 28px caret-left-full, 16px in) before "Profile", and a click on
// it goes back; with no back stack there is no button. With
// PROFILE_SHOTS_DIR set (the own-profile workflow) the header is saved.
func TestProfilePaneBackButton(t *testing.T) {
	h := &backHost{tzHost: hcEnv(t), back: true}
	h.th.Images = &pvImages{}
	v := NewProfileView(h, fakeslack.ProfileID)
	pvFrames(t, v, pvGPU(t), "profile-back-button-light")

	var r input.Router
	ops := new(op.Ops)
	frame := func() {
		ops.Reset()
		gtx := layout.Context{Ops: ops, Now: time.Now(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(pvW, pvH)), Source: r.Source()}
		v.Layout(gtx)
		r.Frame(ops)
	}
	var at time.Duration
	click := func(p f32.Point) {
		at += time.Second
		r.Queue(
			pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p, Time: at},
			pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: p, Time: at + time.Millisecond},
			pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: p, Time: at + 2*time.Millisecond},
		)
		frame()
		frame()
	}
	frame()
	btn := f32.Pt(16+14, 49/2)
	click(btn)
	if h.went != 1 {
		t.Fatalf("clicking the back button called GoSecondaryBack %d times", h.went)
	}
	// no back stack now: no button there
	click(btn)
	if h.went != 1 {
		t.Errorf("a click where the back button was called GoSecondaryBack with no back stack (%d)", h.went)
	}
}
