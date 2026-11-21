package videoplayer

// Round 3's review, a MINOR: the overlay stays active while its More
// actions menu is open (frPc's onPopoverOpen keeps fIJO's --active), so
// its controls don't vanish under the menu when the pointer moves onto
// it. tileActive is menu_hooks_test.go's.

import (
	"image"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/media"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/richview"
)

// menuHost records the menus the corner opens.
type menuHost struct {
	mu      sync.Mutex
	opened  int
	onClose func()
}

func (h *menuHost) OpenFile(*richtext.File) {}
func (h *menuHost) FileAction(a richview.FileAction, f *richtext.File, btn image.Rectangle) {
	h.mu.Lock()
	h.opened++
	h.mu.Unlock()
}
func (h *menuHost) OpenFileMenu(f *richtext.File, win, view image.Rectangle, onClose func()) bool {
	h.mu.Lock()
	h.opened++
	h.onClose = onClose
	h.mu.Unlock()
	return true
}

func TestOverlayActiveWhileMenuOpen(t *testing.T) {
	c, _ := slowEnv(t, 0, media.FakeOptions{})
	h := &menuHost{}
	r := c.Row("channel", "C1", "1.0", h)
	f := videoFile("F1")
	var router input.Router
	var ops op.Ops
	now := time.Now()
	frame := func() {
		ops.Reset()
		now = now.Add(20 * time.Millisecond)
		gtx := layout.Context{Ops: &ops, Now: now, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(480, 270)), Source: router.Source()}
		c.BeginFrame()
		r.LayoutMedia(gtx, richview.MediaSlot{File: f, Poster: image.Rect(0, 0, 480, 270)})
		c.EndFrame()
		router.Frame(&ops)
	}
	frame()
	at := time.Second
	ev := func(k pointer.Kind, p image.Point, btn pointer.Buttons) {
		at += 30 * time.Millisecond
		router.Queue(pointer.Event{Kind: k, Source: pointer.Mouse, Buttons: btn, Position: f32.Pt(float32(p.X), float32(p.Y)), Time: at})
		frame()
	}
	ev(pointer.Move, image.Pt(240, 100), 0)
	frame()
	more := state(t, c, r, f).More
	if more.Empty() {
		t.Fatal("no More actions in the corner")
	}
	m := more.Min.Add(more.Max).Div(2)
	ev(pointer.Move, m, 0)
	ev(pointer.Press, m, pointer.ButtonPrimary)
	ev(pointer.Release, m, 0)
	h.mu.Lock()
	opened := h.opened
	h.mu.Unlock()
	if opened != 1 {
		t.Fatalf("%d menus opened, want More actions'", opened)
	}
	ev(pointer.Move, image.Pt(600, 100), 0) // onto the menu, off the tile
	frame()
	if !tileActive(state(t, c, r, f)) {
		t.Error("the pointer went onto the open menu and the overlay's controls hid")
	}
	h.mu.Lock()
	closeMenu := h.onClose
	h.mu.Unlock()
	if closeMenu != nil {
		closeMenu()
		frame()
		if tileActive(state(t, c, r, f)) {
			t.Error("the menu closed with the pointer away, and the overlay stays active")
		}
	}
}
