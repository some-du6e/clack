package kit

// A kit Popover is Slack's react-modal (shouldCloseOnEsc,
// shouldReturnFocusAfterClose, onFocusedElementRemoved): an Escape from
// anything inside it closes it and goes no further, the topmost of several
// first; the focus goes back to what had it once it closes; and when the
// field that had the focus goes, the popover takes it back.

import (
	"image"
	"slices"
	"strconv"
	"testing"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/refocus"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// popEnv lays out a plugin tree as a window's frame does (slackc.Frame,
// then the tree), under a composer stand-in that notes the focus and takes
// it back when asked, as composer.Editor does (refocus).
type popEnv struct {
	t     *testing.T
	log   *callLog
	v     *slackc.View
	r     input.Router
	ops   op.Ops
	now   time.Time
	nodes []*slackc.Node
	comp  int // the stand-in's tag
}

func newPopEnv(t *testing.T) *popEnv {
	t.Helper()
	Register()
	e := &popEnv{t: t, log: &callLog{}, v: slackc.NewView(904), now: time.Unix(2000, 0)}
	slackc.SetBridge(e.log)
	t.Cleanup(func() { e.v.Close(); slackc.SetBridge(nil); refocus.Reset() })
	e.frames(2) // layers an earlier test left in the window close
	refocus.Reset()
	return e
}

// tree sets the plugin's tree.
func (e *popEnv) tree(js string) {
	e.t.Helper()
	nodes, err := slackc.ParseTree([]byte(js))
	if err != nil {
		e.t.Fatal(err)
	}
	e.nodes = nodes
}

func (e *popEnv) frame() {
	e.ops.Reset()
	e.now = e.now.Add(20 * time.Millisecond)
	gtx := layout.Context{Ops: &e.ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(800, 600)),
		Now: e.now, Source: e.r.Source()}
	slackc.Frame(gtx)
	if refocus.Take(gtx, &e.comp) {
		gtx.Execute(key.FocusCmd{Tag: &e.comp})
	}
	for {
		if _, ok := gtx.Event(key.FocusFilter{Target: &e.comp}); !ok {
			break
		}
	}
	area := clip.Rect{Min: image.Pt(0, 560), Max: image.Pt(800, 600)}.Push(gtx.Ops)
	event.Op(gtx.Ops, &e.comp)
	area.Pop()
	refocus.Note(gtx, &e.comp)
	if e.nodes != nil {
		e.v.Layout(gtx, e.nodes, "PluginPanel", nil, nil)
	}
	e.r.Frame(&e.ops)
}

func (e *popEnv) frames(n int) {
	for i := 0; i < n; i++ {
		e.frame()
	}
}

// key presses and releases name (with mods), then lays out frames.
func (e *popEnv) key(name key.Name, mods key.Modifiers) {
	e.r.Queue(key.Event{Name: name, Modifiers: mods, State: key.Press}, key.Event{Name: name, Modifiers: mods, State: key.Release})
	e.frames(3)
}

// called reports whether JS function fn was called since the last take.
func (e *popEnv) called(fn int64) bool { return slices.Contains(e.log.take(), fn) }

// popoverJS is an open (or closed) Popover whose onClose is fn, around kids.
func popoverJS(k int, open bool, fn int64, kids string) string {
	o := "false"
	if open {
		o = "true"
	}
	return `{"t":"@","n":"Popover","k":` + strconv.Itoa(k) + `,"p":{"isOpen":` + o + `,"targetBounds":{"x":40,"y":40,"width":120,"height":28},` +
		`"onClose":{"$fn":` + strconv.FormatInt(fn, 10) + `}},"c":[` + kids + `]}`
}

// popField is a FormTextInput (k) that takes the focus as it mounts.
func popField(k int) string {
	return `{"t":"@","n":"FormTextInput","k":` + strconv.Itoa(k) + `,"p":{"autoFocus":true,"value":"","onFocus":{"$fn":41},"onBlur":{"$fn":42}}}`
}

func popText(k int, s string) string {
	return `{"t":"div","k":` + strconv.Itoa(k) + `,"c":["` + s + `"]}`
}

// Escape closes an open Popover: with the popover focused (it took the
// focus as it opened), with Shift held (react-modal checks the key code
// alone), and with the autoFocus field inside it focused, the Escape
// bubbling up from the field as from any input in it.
func TestPopoverEscapeCloses(t *testing.T) {
	const onClose = 31
	e := newPopEnv(t)
	e.tree(popoverJS(1, true, onClose, popText(2, "menu")))
	e.frames(4)
	if e.called(onClose) {
		t.Fatal("the popover closed before any key")
	}
	e.key(key.NameEscape, 0)
	if !e.called(onClose) {
		t.Error("Escape with the popover focused didn't close it (no onClose)")
	}
	e.key(key.NameEscape, key.ModShift)
	if !e.called(onClose) {
		t.Error("Shift+Escape didn't close the popover (no onClose)")
	}

	e2 := newPopEnv(t)
	e2.tree(popoverJS(1, true, onClose, `{"t":"div","k":2,"c":[`+popField(3)+`]}`))
	e2.frames(4)
	if !e2.called(41) {
		t.Fatal("the autoFocus field never got the focus (no onFocus)")
	}
	e2.key(key.NameEscape, 0)
	if !e2.called(onClose) {
		t.Error("Escape with the autoFocus field focused didn't close the popover (no onClose)")
	}
}

// With a popover open inside another, Escape closes the inner one first,
// and only it. Once it's closed the outer one has the focus back (the
// inner one's went with it) and the next Escape closes the outer one.
func TestNestedPopoversCloseTopFirst(t *testing.T) {
	const outer, inner = 31, 32
	e := newPopEnv(t)
	e.tree(popoverJS(1, true, outer, popText(2, "outer")+","+popoverJS(3, true, inner, popText(4, "inner"))))
	e.frames(4)
	e.key(key.NameEscape, 0)
	got := e.log.take()
	if !slices.Contains(got, inner) || slices.Contains(got, outer) {
		t.Fatalf("Escape with both open called %v; want the inner popover's onClose (%d) alone", got, inner)
	}
	e.tree(popoverJS(1, true, outer, popText(2, "outer")+","+popoverJS(3, false, inner, popText(4, "inner"))))
	e.frames(4)
	if e.r.Source().Focused(nil) {
		t.Error("the inner popover closed and the focus went nowhere; want the outer popover to take it back")
	}
	e.key(key.NameEscape, 0)
	if got := e.log.take(); !slices.Contains(got, outer) || slices.Contains(got, inner) {
		t.Errorf("Escape after the inner popover closed called %v; want the outer popover's onClose (%d)", got, outer)
	}
}

// The focus goes back to what had it when the popover opened (here the
// composer), once the popover closes or leaves the tree.
func TestPopoverGivesFocusBack(t *testing.T) {
	e := newPopEnv(t)
	e.frames(1)
	e.r.Source().Execute(key.FocusCmd{Tag: &e.comp})
	e.frames(3)
	if !e.r.Source().Focused(&e.comp) {
		t.Fatal("the composer stand-in didn't take the focus")
	}
	open := popoverJS(1, true, 31, popText(2, "menu"))
	for _, c := range []struct{ what, closed string }{
		{"closed (isOpen false)", popoverJS(1, false, 31, popText(2, "menu"))},
		{"gone from the tree", popText(5, "no popover")},
	} {
		e.tree(open)
		e.frames(3)
		if e.r.Source().Focused(&e.comp) {
			t.Fatalf("%s: the open popover didn't take the focus from the composer", c.what)
		}
		e.tree(c.closed)
		e.frames(4)
		if !e.r.Source().Focused(&e.comp) {
			t.Errorf("popover %s: the focus didn't go back to the composer, which had it when it opened", c.what)
		}
	}
}

// When the field that had the focus goes (a search swapped for results),
// the popover takes the focus back instead of leaving it nowhere.
func TestPopoverTakesFocusBackFromRemovedField(t *testing.T) {
	e := newPopEnv(t)
	e.tree(popoverJS(1, true, 31, `{"t":"div","k":2,"c":[`+popField(3)+`]}`))
	e.frames(4)
	if !e.called(41) || e.r.Source().Focused(nil) {
		t.Fatal("the autoFocus field never got the focus")
	}
	e.tree(popoverJS(1, true, 31, popText(2, "results")))
	e.frames(4)
	if e.r.Source().Focused(nil) {
		t.Error("the focused field went and the focus is nowhere; want the popover to take it back")
	}
}
