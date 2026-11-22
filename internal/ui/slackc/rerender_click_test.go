package slackc

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
)

// A click on an element a native lays out (a plugin's row inside the
// patched Menu, Panel here) clicks it when JS delivers a new tree between
// the press and the release: a re-render keeps the element (its node id),
// and the browser's click goes to the element pressed and released on.
// setTree used to drop every sub scope, so the elements' new scope had a
// new Gio pointer tag: Gio sent it a Cancel, the release went to the outer
// scope, and the click went to the Panel around the button (node 2), not
// the button (TestMessageMenuOldMenuRows lost its click whenever the
// loaded engine's re-render landed between the two, alpha.135's release).
func TestClickSurvivesReRender(t *testing.T) {
	for _, rerender := range []bool{false, true} {
		t.Run(map[bool]string{false: "same tree", true: "re-rendered between press and release"}[rerender], func(t *testing.T) {
			var clicks []int64
			testDispatch = func(k int64, ev []byte) {
				var e struct {
					Type string `json:"type"`
				}
				_ = json.Unmarshal(ev, &e)
				if e.Type == "click" {
					clicks = append(clicks, k)
				}
			}
			t.Cleanup(func() { testDispatch = nil })
			slots := map[string]int64{}
			t.Cleanup(func() { releaseSlots(slots) })
			js := encodeProps(Props{"Panel": Child("Panel", nil, func(gtx layout.Context, p Props) layout.Dimensions {
				if e, ok := PropChildren(p); ok {
					return LayoutElement(gtx, e)
				}
				return layout.Dimensions{}
			})}, slots)
			var enc map[string]struct {
				ID int64 `json:"$slot"`
			}
			if err := json.Unmarshal(js, &enc); err != nil {
				t.Fatal(err)
			}
			// a fresh parse each call: new nodes and boxes, the same ids
			tree := func() []*Node {
				return mustTree(t, `{"t":"div","k":1,"c":[{"t":"@","n":"Panel","k":2,"p":{"$slot":`+
					strconv.FormatInt(enc["Panel"].ID, 10)+`},"c":[{"t":"button","k":3,"on":{"click":5},"c":["Accept deletion"]}]}]}`)
			}
			v := NewView(83)
			var r input.Router
			frame := func(nodes []*Node) {
				gtx := testGtx(400, 300)
				gtx.Source = r.Source()
				v.Layout(gtx, nodes, "PluginPanel", nil, nil)
				r.Frame(gtx.Ops)
			}
			n := tree()
			for i := 0; i < 3; i++ {
				frame(n)
			}
			b := v.byK[3]
			if b == nil || b.w <= 0 || b.h <= 0 {
				t.Fatalf("the button wasn't laid out: %+v", b)
			}
			p := f32.Pt(b.ax+b.w/2, b.ay+b.h/2)
			r.Queue(
				pointer.Event{Kind: pointer.Move, Position: p, Source: pointer.Mouse, Time: 10 * time.Millisecond},
				pointer.Event{Kind: pointer.Press, Position: p, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Time: 20 * time.Millisecond},
			)
			frame(n)
			frame(n)
			if rerender {
				n = tree() // JS answered the mousedown with a new tree
			}
			frame(n)
			r.Queue(pointer.Event{Kind: pointer.Release, Position: p, Source: pointer.Mouse, Time: 120 * time.Millisecond})
			frame(n)
			frame(n)
			if len(clicks) != 1 || clicks[0] != 3 {
				t.Errorf("clicks went to %v, want the button (3) once", clicks)
			}
		})
	}
}
