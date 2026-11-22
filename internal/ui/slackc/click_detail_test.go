package slackc

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
)

// detailEvent is a dispatched click's or dblclick's element and detail.
type detailEvent struct {
	typ    string
	k      int64
	detail int
}

func (e detailEvent) String() string { return fmt.Sprintf("%s@%d/%d", e.typ, e.k, e.detail) }

// clickDetailEnv is two buttons side by side (Alpha k3, Beta k4, 60 px
// each) through a real router, recording their clicks and dblclicks.
type clickDetailEnv struct {
	t     *testing.T
	v     *View
	r     input.Router
	nodes []*Node
	evs   []detailEvent
}

func newClickDetailEnv(t *testing.T) *clickDetailEnv {
	t.Helper()
	e := &clickDetailEnv{t: t}
	testDispatch = func(k int64, ev []byte) {
		var d struct {
			Type   string `json:"type"`
			Detail int    `json:"detail"`
		}
		_ = json.Unmarshal(ev, &d)
		if d.Type == "click" || d.Type == "dblclick" {
			e.evs = append(e.evs, detailEvent{d.Type, k, d.Detail})
		}
	}
	t.Cleanup(func() { testDispatch = nil })
	btn := func(k, on int, label string) string {
		return fmt.Sprintf(`{"t":"button","k":%d,"p":{"style":{"width":"60px","height":"30px","margin":0,"padding":0,"border":"none"}},"on":{"click":%d,"dblclick":%d},"c":[%q]}`, k, on, on+1, label)
	}
	e.nodes = mustTree(t, `{"t":"div","k":1,"p":{"style":{"display":"flex"}},"c":[`+btn(3, 5, "Alpha")+`,`+btn(4, 7, "Beta")+`]}`)
	e.v = NewView(84)
	for range 3 {
		e.frame()
	}
	return e
}

func (e *clickDetailEnv) frame() {
	gtx := testGtx(400, 300)
	gtx.Source = e.r.Source()
	e.v.Layout(gtx, e.nodes, "PluginPanel", nil, nil)
	e.r.Frame(gtx.Ops)
}

// at is a point in button k, frac of its width from its left.
func (e *clickDetailEnv) at(k int64, frac float32) f32.Point {
	b := e.v.byK[k]
	if b == nil || b.w <= 0 || b.h <= 0 {
		e.t.Fatalf("button %d wasn't laid out: %+v", k, b)
	}
	return f32.Pt(b.ax+b.w*frac, b.ay+b.h/2)
}

func (e *clickDetailEnv) click(p f32.Point, at time.Duration) {
	e.r.Queue(
		pointer.Event{Kind: pointer.Move, Position: p, Source: pointer.Mouse, Time: at},
		pointer.Event{Kind: pointer.Press, Position: p, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Time: at + 10*time.Millisecond},
		pointer.Event{Kind: pointer.Release, Position: p, Source: pointer.Mouse, Time: at + 40*time.Millisecond})
	for range 3 {
		e.frame()
	}
}

// A plugin element's click carries the window's count as its detail, and
// the second click of a run fires dblclick, as Blink's click and dblclick
// take the platform event's click_count whatever element each lands on:
// Alpha clicked at its right edge, then Beta a pixel away, is a double
// click on Beta (detail 2 and a dblclick), not Beta's first click.
func TestPluginClickDetailIsWindowCount(t *testing.T) {
	e := newClickDetailEnv(t)
	a, b := e.at(3, 1), e.at(4, 0)
	if d := b.X - a.X; d < 0 || d > 2 {
		t.Fatalf("the buttons' edges are %.1f px apart, want them touching", d)
	}
	e.click(a.Sub(f32.Pt(.5, 0)), time.Second)
	e.click(b.Add(f32.Pt(.5, 0)), time.Second+100*time.Millisecond)
	want := []detailEvent{{"click", 3, 1}, {"click", 4, 2}, {"dblclick", 4, 2}}
	if !slices.Equal(e.evs, want) {
		t.Errorf("Alpha's right edge, then Beta's left: %v, want %v", e.evs, want)
	}
	// a double click on Beta alone
	e.evs = nil
	p := e.at(4, .5)
	e.click(p, 3*time.Second)
	e.click(p, 3*time.Second+150*time.Millisecond)
	want = []detailEvent{{"click", 4, 1}, {"click", 4, 2}, {"dblclick", 4, 2}}
	if !slices.Equal(e.evs, want) {
		t.Errorf("a double click on Beta: %v, want %v", e.evs, want)
	}
}

// Enter on a button just after a mouse click on it is a keyboard click:
// detail 0 and no dblclick, as Blink's activation behaviour dispatches a
// simulated click (UI Events: a click from the keyboard has detail 0).
// It used to count on from the mouse click, as the zero time of its event
// fell inside the double-click time: detail 2 and a dblclick.
func TestPluginKeyboardClickDetailZero(t *testing.T) {
	e := newClickDetailEnv(t)
	e.click(e.at(3, .5), time.Second)
	e.evs = nil
	e.r.Queue(key.Event{Name: key.NameReturn, State: key.Press}, key.Event{Name: key.NameReturn, State: key.Release})
	for range 3 {
		e.frame()
	}
	if want := []detailEvent{{"click", 3, 0}}; !slices.Equal(e.evs, want) {
		t.Errorf("Enter on Alpha after a click on it: %v, want %v", e.evs, want)
	}
}
