package slackc

import (
	"encoding/json"
	"image"
	"strconv"
	"testing"

	"gioui.org/io/event"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/layout"
)

// A focused widget inside a native slot keeps key focus when its patched
// tree is re-delivered (React re-renders the root: BetterTypingIndicator's
// TypingNames re-renders on store changes, and live the composer under its
// MessagePaneInput patch lost focus within seconds).
func TestNativeKeepsFocusAcrossTrees(t *testing.T) {
	for _, nested := range []bool{false, true} {
		t.Run(map[bool]string{false: "slot", true: "nested slot"}[nested], func(t *testing.T) {
			tag := new(int)
			asked := false
			editor := func(gtx layout.Context, _ Props) layout.Dimensions {
				event.Op(gtx.Ops, tag)
				for {
					if _, ok := gtx.Event(key.FocusFilter{Target: tag}); !ok {
						break
					}
				}
				if !asked && gtx.Source.Enabled() {
					asked = true
					gtx.Execute(key.FocusCmd{Tag: tag})
				}
				return layout.Dimensions{Size: image.Pt(100, 20)}
			}
			slots := map[string]int64{}
			defer releaseSlots(slots)
			slotID := func(name string, p Props, n Native) string {
				js := encodeProps(Props{"x": Child(name, p, n)}, slots)
				var enc struct {
					X struct {
						ID int64 `json:"$slot"`
					} `json:"x"`
				}
				if err := json.Unmarshal(js, &enc); err != nil {
					t.Fatal(err)
				}
				return strconv.FormatInt(enc.X.ID, 10)
			}
			texty := slotID("Texty", nil, editor)
			inner := `{"t":"@","n":"Texty","k":3,"p":{"$slot":` + texty + `}}`
			if nested {
				// a slot whose native lays out its children (WysiwygContainer around the editor)
				outer := slotID("Wysiwyg", nil, func(gtx layout.Context, p Props) layout.Dimensions {
					if e, ok := PropChildren(p); ok {
						return LayoutElement(gtx, e)
					}
					return layout.Dimensions{}
				})
				inner = `{"t":"@","n":"Wysiwyg","k":2,"p":{"$slot":` + outer + `},"c":[` + inner + `]}`
			}
			tree := func(text string) []*Node {
				return mustTree(t, `{"t":"div","k":1,"c":[`+inner+`,{"t":"span","k":9,"c":["`+text+`"]}]}`)
			}
			v := NewView(77)
			var r input.Router
			frame := func(nodes []*Node) {
				gtx := testGtx(400, 300)
				gtx.Source = r.Source()
				v.Layout(gtx, nodes, "MessagePaneInput", nil, nil)
				r.Frame(gtx.Ops)
			}
			n1 := tree("a")
			for i := 0; i < 3; i++ {
				frame(n1)
			}
			if !r.Source().Focused(tag) {
				t.Fatal("the widget didn't take focus")
			}
			for i, s := range []string{"b", "c", "d"} {
				frame(tree(s)) // a re-render: same host instances, new tree
				frame(tree(s))
				if !r.Source().Focused(tag) {
					t.Fatalf("focus lost after re-render %d", i+1)
				}
			}
		})
	}
}
