package videoplayer

// Round 3's review, two NITs on the volume control (lGYe in the inline
// player, css-volume-pillow.txt). Its hidden slider takes no hover and no
// press (visibility:hidden), so the slider opens from the mute button
// alone: .16 s later in the popover (players over 330px), at once in the
// vertical box. And the slider's press writes media_volume, because X7vT
// calls onChange before onMouseDown; its release writes it again where a
// drag left it. On the round-3 head (0cbe7e63) the hidden slider's area
// opened the slider at once, and a press wrote nothing until the release.

import (
	"fmt"
	"image"
	"slices"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	gkey "gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/richview"
)

// tileRig lays one tile out frame after frame through one router, as a
// list does, and sends it pointer events.
type tileRig struct {
	c      *Controller
	r      *Row
	f      *richtext.File
	size   image.Point
	router input.Router
	ops    op.Ops
	at     time.Duration
	focus  any // a tag the next frame focuses (keys_test.go)
}

func (g *tileRig) frame() {
	g.ops.Reset()
	gtx := layout.Context{Ops: &g.ops, Now: time.Now(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(g.size), Source: g.router.Source()}
	if g.focus != nil {
		gtx.Execute(gkey.FocusCmd{Tag: g.focus})
		g.focus = nil
	}
	g.c.BeginFrame()
	g.r.LayoutMedia(gtx, richview.MediaSlot{File: g.f, Poster: image.Rectangle{Max: g.size}})
	g.c.EndFrame()
	g.router.Frame(&g.ops)
}

func (g *tileRig) ev(k pointer.Kind, p image.Point, btn pointer.Buttons) {
	g.at += 30 * time.Millisecond
	g.router.Queue(pointer.Event{Kind: k, Source: pointer.Mouse, Buttons: btn, Position: f32.Pt(float32(p.X), float32(p.Y)), Time: g.at})
	g.frame()
}

// zones are the controls the last layout drew.
func (g *tileRig) zones() zones {
	g.c.mu.Lock()
	defer g.c.mu.Unlock()
	return g.c.tiles[g.r.key(g.f)].z
}

// volOpen is whether the slider shows.
func (g *tileRig) volOpen() bool {
	g.c.mu.Lock()
	defer g.c.mu.Unlock()
	t := g.c.tiles[g.r.key(g.f)]
	return t.volOver || t.vol
}

func TestVolumePopoverOpensFromMute(t *testing.T) {
	c, p := fakeEnv(t)
	g := &tileRig{c: c, r: c.Row("channel", "C1", "1.0", nil), f: videoFile("F1"), size: image.Pt(480, 270), at: time.Second}
	g.frame()
	g.ev(pointer.Move, image.Pt(240, 100), 0) // hovered: the controls show
	z := g.zones()
	if z.mute.Empty() || z.slider.Empty() || z.vertical {
		t.Fatalf("a 480px player's controls: %+v, want the popover's mute and slider", z)
	}
	hidden := z.slider.Min.Add(z.slider.Max).Div(2)
	g.ev(pointer.Move, hidden, 0)
	if g.volOpen() {
		t.Error("the pointer on the hidden slider's area opened it: it takes no hover")
	}
	g.ev(pointer.Press, hidden, pointer.ButtonPrimary)
	g.ev(pointer.Release, hidden, 0)
	if w := p.writes(); len(w) != 0 {
		t.Errorf("a press on the hidden slider wrote %v", w)
	}
	mute := z.mute.Min.Add(z.mute.Max).Div(2)
	g.ev(pointer.Move, mute, 0)
	if g.volOpen() {
		t.Error("the slider opened the moment the pointer reached the mute: the popover waits .16 s")
	}
	opened := false
	for dl := time.Now().Add(2 * time.Second); time.Now().Before(dl) && !opened; time.Sleep(10 * time.Millisecond) {
		g.frame()
		opened = g.volOpen()
	}
	if !opened {
		t.Fatal("the pointer rested on the mute and the slider never opened")
	}
	g.ev(pointer.Move, hidden, 0) // along the open slider: it stays open
	if !g.volOpen() {
		t.Fatal("the open slider closed as the pointer moved onto it")
	}
	g.ev(pointer.Press, hidden, pointer.ButtonPrimary)
	v1 := z.volumeAt(hidden)
	if w := p.writes(); !slices.Equal(w, []string{fmt.Sprintf("media_volume=%v", v1)}) {
		t.Errorf("after the slider's press: %v, want media_volume=%v written at the press", w, v1)
	}
	drag := image.Pt(z.slider.Min.X+z.slider.Dx()*3/4, hidden.Y)
	v2 := z.volumeAt(drag)
	g.ev(pointer.Move, drag, pointer.ButtonPrimary) // held: the router makes it a drag
	if n := len(p.writes()); n != 1 {
		t.Errorf("%d writes during the drag, want none past the press's", n-1)
	}
	g.ev(pointer.Release, drag, 0)
	want := []string{fmt.Sprintf("media_volume=%v", v1), fmt.Sprintf("media_volume=%v", v2)}
	if w := p.writes(); !slices.Equal(w, want) {
		t.Errorf("the press and the drag's release wrote %v, want %v", w, want)
	}
	if muted, vol := c.Volume(); muted || vol != v2 {
		t.Errorf("after the drag: muted %v at %v, want %v", muted, vol, v2)
	}

	// the vertical box (330px or less) opens at once from the mute
	n := len(p.writes())
	v := &tileRig{c: c, r: c.Row("channel", "C1", "2.0", nil), f: videoFile("F2"), size: image.Pt(300, 170), at: 10 * time.Second}
	v.frame()
	v.ev(pointer.Move, image.Pt(150, 40), 0)
	vz := v.zones()
	if !vz.vertical || vz.mute.Empty() {
		t.Fatalf("a 300px player's controls: %+v, want the vertical volume", vz)
	}
	v.ev(pointer.Move, vz.mute.Min.Add(vz.mute.Max).Div(2), 0)
	if !v.volOpen() {
		t.Error("the vertical volume didn't open at once from the mute")
	}
	if w := p.writes()[n:]; len(w) != 0 {
		t.Errorf("hovering the vertical volume wrote %s", strings.Join(w, ", "))
	}
}
