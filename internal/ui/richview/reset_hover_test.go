//go:build !stockgio

// Gio patches 31 (Ops.Whole) and 49 (Source.ResetPointers): ci's stock-Gio
// build of this package (gio-layer-fix.sh, -tags stockgio) leaves this out.

package richview

import (
	"fmt"
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	rt "github.com/some-du6e/clack/internal/richtext"
)

// A message body is hovered on an emoji, its :name: tooltip up, when the
// window freezes (Clack's inspect tool: the window draws the last frame's
// ops again and lays nothing out). The pointer leaves the body while
// frozen, and the freeze ends (Gio patch 49, Source.ResetPointers): the
// body no longer counts the pointer as over it and has no tooltip to
// draw, for Windows' mouse (PointerID 1) as for macOS' and X11's (0). Its
// Cancel case clears only its presses; the reset's Leave is what clears
// the hover.
func TestResetPointersClearsBodyHover(t *testing.T) {
	for _, id := range []pointer.ID{1, 0} {
		t.Run(fmt.Sprintf("mouse %d", id), func(t *testing.T) {
			em := rt.Span{Kind: rt.Emoji, ID: "robot_face", URL: "https://e.test/robot_face.png"}
			v := New(doc(para(txt("hello "), em, txt(" there"))), Options{})
			st := NewStyle(sharedShaper(), noImages{}, LightPalette())
			var r input.Router
			var ops, show op.Ops
			t0 := time.Unix(1000, 0)
			laidOut := func(now time.Time) {
				ops.Reset()
				gtx := layout.Context{Ops: &ops, Source: r.Source(), Now: now, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
					Constraints: layout.Exact(image.Pt(400, 60))}
				v.Layout(gtx, st, &recHandler{})
				r.Frame(&ops)
			}
			frozen := func() { // the last laid-out frame's ops again
				show.Reset()
				ops.Whole().Add(&show)
				r.Frame(&show)
			}
			move := func(p f32.Point) {
				r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, PointerID: id, Position: p})
			}
			laidOut(t0)
			var emoji *frag
			for i := range v.d.frags {
				if v.d.frags[i].kind == fImage {
					emoji = &v.d.frags[i]
				}
			}
			if emoji == nil {
				t.Fatal("no emoji in the body")
			}
			move(f32.Pt(emoji.x+float32(emoji.size)/2, float32(emoji.y+emoji.size/2)))
			laidOut(t0)
			laidOut(t0.Add(EmojiTipDelay + 10*time.Millisecond))
			if name, ok := v.HoveredEmoji(); !ok || name != ":robot_face:" || !v.Pointer() {
				t.Fatalf("on the emoji: tooltip %q %v, pointer over the body %v", name, ok, v.Pointer())
			}
			frozen()
			move(f32.Pt(200, 150)) // under the body, which is 60 px tall
			frozen()
			frozen()
			r.Source().ResetPointers()
			laidOut(t0.Add(time.Second))
			if v.Pointer() {
				t.Error("after the freeze the body still counts the pointer, which left it while frozen, as over it")
			}
			if name, ok := v.HoveredEmoji(); ok {
				t.Errorf("after the freeze the body still has %s's tooltip to draw", name)
			}
		})
	}
}
