package slackc

import (
	"encoding/json"
	"strconv"
	"testing"

	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/layout"
)

// A native that lays out its children (a plugin's panel around an input)
// paints them in its measure pass too, with gtx.Disabled(): Gio reports
// nothing focused there and drops FocusCmds. That pass must neither take
// an <input autoFocus>'s focus request (the input never got the caret)
// nor report a focused input's blur (it sent JS blur and focus again on
// every relayout).
func TestMeasurePassLeavesFocusAlone(t *testing.T) {
	for _, auto := range []bool{true, false} {
		t.Run(map[bool]string{true: "autoFocus", false: "focused"}[auto], func(t *testing.T) {
			var evs []string
			testDispatch = func(_ int64, ev []byte) {
				var e struct {
					Type string `json:"type"`
				}
				_ = json.Unmarshal(ev, &e)
				evs = append(evs, e.Type)
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
			in := `{"t":"input","k":3}`
			if auto {
				in = `{"t":"input","k":3,"p":{"autoFocus":true}}`
			}
			tree := func(text string) []*Node {
				return mustTree(t, `{"t":"div","k":1,"c":[{"t":"@","n":"Panel","k":2,"p":{"$slot":`+
					strconv.FormatInt(enc["Panel"].ID, 10)+`},"c":[`+in+`]},{"t":"span","k":9,"c":["`+text+`"]}]}`)
			}
			v := NewView(79)
			var r input.Router
			frame := func(nodes []*Node) {
				gtx := testGtx(400, 300)
				gtx.Source = r.Source()
				v.Layout(gtx, nodes, "PluginPanel", nil, nil)
				r.Frame(gtx.Ops)
			}
			n := tree("a")
			for i := 0; i < 3; i++ {
				frame(n)
			}
			ns := v.ns[3]
			if ns == nil || ns.ed == nil {
				t.Fatal("the input has no editor")
			}
			if !auto {
				r.Source().Execute(key.FocusCmd{Tag: ns.ed})
				frame(n)
				frame(n)
			}
			if !r.Source().Focused(ns.ed) {
				t.Fatalf("the input doesn't have the focus (events %v)", evs)
			}
			evs = evs[:0]
			for i, s := range []string{"b", "c", "d"} {
				frame(tree(s)) // a re-render: the panel measured again
				frame(tree(s))
				if !r.Source().Focused(ns.ed) {
					t.Fatalf("focus lost after re-render %d", i+1)
				}
			}
			for _, e := range evs {
				if e == "blur" || e == "focusout" || e == "focus" || e == "focusin" {
					t.Errorf("relayouts of a focused input dispatched %v; want no focus events", evs)
					break
				}
			}
		})
	}
}
