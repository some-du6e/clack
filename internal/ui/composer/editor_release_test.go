package composer

import (
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// KeepFocus bridges a few frames without the editor (a plugin's tree
// skipping it once), not two seconds of it: an editor that stays off the
// screen for longer than keepFor and keepFrames frames lets go of the
// focus. Release (its conversation left the screen) lets go at the end of
// the next frame, unless that frame drew it anyway, and what is typed
// afterwards isn't kept for it.
func TestKeepFocusLetsGoOfUndrawnEditors(t *testing.T) {
	var r input.Router
	var ops op.Ops
	st := DefaultEditorStyle(theme.NewShaper())
	now := time.Now()
	ed := NewEditor()
	gtx := func() layout.Context {
		return layout.Context{Ops: &ops, Source: r.Source(), Now: now, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(600, 120))}
	}
	frame := func(draw bool, step time.Duration) {
		now = now.Add(step)
		ops.Reset()
		g := gtx()
		if draw {
			ed.Layout(g, st)
		}
		KeepFocus(g)
		r.Frame(&ops)
	}
	focus := func() {
		t.Helper()
		frame(true, 16*time.Millisecond)
		ed.Focus(gtx())
		frame(true, 16*time.Millisecond)
		frame(true, 16*time.Millisecond)
		if !r.Source().Focused(ed) {
			t.Fatal("the editor didn't take the focus")
		}
	}
	text := func() string { return ed.Doc().Text() }

	focus()
	for range keepFrames {
		frame(false, 16*time.Millisecond) // a glitch: a few frames without it
	}
	if !r.Source().Focused(ed) {
		t.Fatal("the focus went over a few missed frames")
	}
	frame(true, 16*time.Millisecond)

	// off the screen for good, with no Release: gone well before 2 s
	for range keepFrames + 2 {
		frame(false, 100*time.Millisecond)
	}
	if r.Source().Focused(ed) {
		t.Fatal("an editor off the screen for 600 ms and 6 frames still has the focus (KeepFocus held it)")
	}

	// released: the next frame lets go, and typing then goes nowhere
	focus()
	Release(ed)
	frame(false, 16*time.Millisecond)
	if r.Source().Focused(ed) {
		t.Fatal("a released editor kept the focus")
	}
	r.Queue(key.EditEvent{Text: "typed-elsewhere"})
	frame(false, 16*time.Millisecond)
	frame(true, 16*time.Millisecond)
	frame(true, 16*time.Millisecond)
	if strings.Contains(text(), "typed-elsewhere") {
		t.Errorf("text typed after the release landed in the released editor: %q", text())
	}

	// released while it is still drawn (another pane shows it): it keeps
	// the focus
	focus()
	Release(ed)
	frame(true, 16*time.Millisecond)
	if !r.Source().Focused(ed) {
		t.Fatal("a released editor drawn in that frame lost the focus")
	}

	// slow frames: 4 missed frames 100 ms apart are past keepFor, but the
	// hold lasts keepFrames frames whatever the clock says
	frame(true, 16*time.Millisecond)
	for range keepFrames {
		frame(false, 100*time.Millisecond)
	}
	if !r.Source().Focused(ed) {
		t.Fatal("the focus went over 4 slow missed frames (keepFrames)")
	}
	frame(true, 16*time.Millisecond)

	// a glitch: what was typed while the editor missed a frame lands in it
	// when it's back
	r.Queue(key.EditEvent{Text: "glitch-typing"})
	frame(false, 16*time.Millisecond)
	frame(true, 16*time.Millisecond)
	frame(true, 16*time.Millisecond)
	if !strings.Contains(text(), "glitch-typing") {
		t.Errorf("text typed over a glitch frame didn't reach the editor: %q", text())
	}

	// a hold that ends without the editor back drops what it read: drawn
	// again later, it doesn't replay text typed while nobody saw it
	r.Queue(key.EditEvent{Text: "hidden-typing"})
	for range keepFrames + 2 {
		frame(false, 100*time.Millisecond)
	}
	if r.Source().Focused(ed) {
		t.Fatal("the hold didn't end")
	}
	frame(true, 16*time.Millisecond)
	frame(true, 16*time.Millisecond)
	if strings.Contains(text(), "hidden-typing") {
		t.Errorf("text typed during a hold that ended came back in the editor: %q", text())
	}
}
