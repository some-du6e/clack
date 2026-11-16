package kit

import (
	"image"
	"sync"
	"testing"
	"time"

	"gioui.org/io/input"
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
