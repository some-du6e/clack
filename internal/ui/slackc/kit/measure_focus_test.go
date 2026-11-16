package kit

import (
	"image"
	"sync"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/slackc"
)

// callLog is a bridge that records which JS functions Go called.
type callLog struct {
	mu    sync.Mutex
	calls []int64
}

func (c *callLog) Render(uint64, string, []byte)                  {}
func (c *callLog) RenderFn(uint64, int64, []byte)                 {}
func (c *callLog) Unmount(uint64)                                 {}
func (c *callLog) Dispatch(int64, []byte)                         {}
func (c *callLog) SetRects(map[int64][4]float32)                  {}
func (c *callLog) SetViewport(w, h, dpr float32, dark bool)       {}
func (c *callLog) Call(fn int64, _ []byte, _ func([]byte, error)) { c.add(fn) }

func (c *callLog) add(fn int64) {
	c.mu.Lock()
	c.calls = append(c.calls, fn)
	c.mu.Unlock()
}

// take returns the calls so far and forgets them.
func (c *callLog) take() []int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.calls
	c.calls = nil
	return out
}

// A FormTextInput in a plugin's tree is a native slackc also lays out in
// measure passes (gtx.Disabled()), where Gio reports nothing focused and
// drops FocusCmds. Its autoFocus waits for a live pass (it was spent in
// the measure pass, and the field never got the caret), and a focused
// field sends JS no onBlur, onFocus pair on each relayout. Buttons
// (ctlPress) and TimePicker read focus the same way (liveFocus).
func TestFormTextInputFocusThroughMeasurePasses(t *testing.T) {
	Register()
	const onFocus, onBlur = 11, 12
	log := &callLog{}
	slackc.SetBridge(log)
	t.Cleanup(func() { slackc.SetBridge(nil) })
	tree := func(text string) []*slackc.Node {
		nodes, err := slackc.ParseTree([]byte(`{"t":"div","k":1,"c":[{"t":"@","n":"FormTextInput","k":2,"p":{"autoFocus":true,"value":"",` +
			`"onFocus":{"$fn":11},"onBlur":{"$fn":12}}},{"t":"span","k":3,"c":["` + text + `"]}]}`))
		if err != nil {
			t.Fatal(err)
		}
		return nodes
	}
	v := slackc.NewView(902)
	t.Cleanup(v.Close)
	var r input.Router
	ops := new(op.Ops)
	now := time.Unix(1000, 0)
	frame := func(nodes []*slackc.Node) {
		ops.Reset()
		now = now.Add(20 * time.Millisecond)
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Constraints{Max: image.Pt(600, 400)},
			Now: now, Source: r.Source()}
		v.Layout(gtx, nodes, "PluginPanel", nil, nil)
		r.Frame(ops)
	}
	n := tree("a")
	for i := 0; i < 4; i++ {
		frame(n)
	}
	got := log.take()
	focused := false
	for _, c := range got {
		focused = focused || c == onFocus
		if c == onBlur {
			t.Errorf("onBlur before the field was ever left (calls %v)", got)
		}
	}
	if !focused {
		t.Fatalf("the autoFocus field never got the focus: no onFocus (calls %v)", got)
	}
	for _, s := range []string{"b", "c", "d"} {
		frame(tree(s)) // a re-render: the tree laid out (and measured) again
		frame(tree(s))
	}
	for _, c := range log.take() {
		if c == onFocus || c == onBlur {
			t.Fatalf("re-renders of a focused field called onFocus/onBlur (fn %d); want neither", c)
		}
	}
}

// A Popover's modal layer takes the focus as it opens (for its Escape),
// but a field in it with autoFocus (a menu's search) gets the caret, as a
// browser's autofocus inside a popover does. The layer took the focus
// after its content had laid out, and Gio's last FocusCmd wins: the field
// got the focus and lost it in the same frame (onFocus, then onBlur), drew
// no caret, and typing went nowhere until it was clicked.
func TestPopoverAutoFocusFieldKeepsCaret(t *testing.T) {
	Register()
	const onFocus, onBlur, onChange = 21, 22, 23
	log := &callLog{}
	slackc.SetBridge(log)
	t.Cleanup(func() { slackc.SetBridge(nil) })
	nodes, err := slackc.ParseTree([]byte(`{"t":"@","n":"Popover","k":1,"p":{"isOpen":true,"targetBounds":{"x":40,"y":40,"width":120,"height":28},` +
		`"onClose":{"$fn":24}},"c":[{"t":"div","k":2,"c":[{"t":"@","n":"FormTextInput","k":3,"p":{"autoFocus":true,"value":"",` +
		`"onFocus":{"$fn":21},"onBlur":{"$fn":22},"onChange":{"$fn":23}}}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	v := slackc.NewView(903)
	t.Cleanup(v.Close)
	var r input.Router
	ops := new(op.Ops)
	now := time.Unix(1000, 0)
	frame := func() {
		ops.Reset()
		now = now.Add(20 * time.Millisecond)
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Constraints{Max: image.Pt(600, 400)},
			Now: now, Source: r.Source()}
		v.Layout(gtx, nodes, "PluginPanel", nil, nil)
		r.Frame(ops)
	}
	for i := 0; i < 4; i++ {
		frame()
	}
	got := log.take()
	focused := false
	for _, c := range got {
		focused = focused || c == onFocus
		if c == onBlur {
			t.Errorf("the autoFocus field in the Popover was blurred (calls %v): the layer took the focus from it", got)
		}
	}
	if !focused {
		t.Fatalf("the autoFocus field in the Popover never got the focus: no onFocus (calls %v)", got)
	}
	// typing goes into the field
	r.Queue(key.EditEvent{Text: "x"}, key.SelectionEvent{Start: 1, End: 1})
	for i := 0; i < 3; i++ {
		frame()
	}
	changed := false
	for _, c := range log.take() {
		changed = changed || c == onChange
	}
	if !changed {
		t.Error("typing with the Popover open changed nothing (no onChange): the field doesn't have the caret")
	}
}
