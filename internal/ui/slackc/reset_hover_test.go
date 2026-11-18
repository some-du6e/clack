package slackc

import (
	"encoding/json"
	"fmt"
	"image"
	"slices"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/ui/slackc/css"
)

// A plugin tree's button is :hover when the window freezes (Clack's
// inspect tool: the window draws the last frame's ops again and lays
// nothing out). The pointer leaves the tree while frozen, and the freeze
// ends (Gio patch 49, Source.ResetPointers): the button loses :hover and
// JS gets its mouseleave, for Windows' mouse (PointerID 1) as for macOS'
// and X11's (0). A scope's Cancel clears only the press and the active
// element; the reset's Leave is what clears the hover.
func TestResetPointersLeavesTree(t *testing.T) {
	for _, id := range []pointer.ID{1, 0} {
		t.Run(fmt.Sprintf("mouse %d", id), func(t *testing.T) {
			withStyles(t, func(n *Node, s *css.Style) {
				switch n.ID {
				case "outer":
					s.Width, s.Height = px(200), px(100)
				case "btn":
					s.Display = css.DisplayBlock
					s.Width, s.Height = px(50), px(30)
					s.Margin[0], s.Margin[3] = px(10), px(20)
				}
			})
			var left []int64
			testDispatch = func(k int64, b []byte) {
				var m map[string]any
				if err := json.Unmarshal(b, &m); err != nil {
					t.Errorf("bad event json %s: %v", b, err)
				}
				if m["type"] == "mouseleave" {
					left = append(left, k)
				}
			}
			defer func() { testDispatch = nil }()
			windowSize = image.Pt(1000, 800)
			nodes := mustTree(t, `{"t":"div","k":1,"p":{"id":"outer"},"c":[{"t":"button","k":2,"p":{"id":"btn"},"c":["Go"]}]}`)
			v := NewView(1)
			var r input.Router
			var ops, show op.Ops
			laidOut := func() {
				ops.Reset()
				gtx := testGtx(400, 400)
				gtx.Ops = &ops
				gtx.Source = r.Source()
				v.Layout(gtx, nodes, "", nil, nil)
				r.Frame(&ops)
			}
			frozen := func() { // the last laid-out frame's ops again
				show.Reset()
				ops.Whole().Add(&show)
				r.Frame(&show)
			}
			clock := time.Duration(0)
			move := func(p f32.Point) {
				clock += 10 * time.Millisecond
				r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, PointerID: id, Position: p, Time: clock})
			}
			laidOut()
			laidOut()
			move(f32.Pt(30, 20)) // on the button (20..70, 10..40)
			laidOut()
			if byID(v, "btn").state&css.StateHover == 0 {
				t.Fatal("the button isn't :hover under the pointer")
			}
			frozen()
			move(f32.Pt(300, 300)) // off the tree (200x100)
			frozen()
			frozen()
			left = nil
			r.Source().ResetPointers()
			laidOut()
			if byID(v, "btn").state&css.StateHover != 0 {
				t.Error("after the freeze the button is still :hover, the pointer gone from it")
			}
			if !slices.Contains(left, 2) {
				t.Errorf("after the freeze no mouseleave on the button (mouseleaves %v)", left)
			}
		})
	}
}
