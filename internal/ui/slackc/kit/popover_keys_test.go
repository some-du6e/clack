package kit

// A kit Popover is Slack's react-modal (shouldCloseOnEsc,
// shouldReturnFocusAfterClose, onFocusedElementRemoved), one layer of its
// window's overlay stack (slackc layers.go): an Escape from anything in it
// closes it and goes no further, the innermost first (a confirm or a
// popover inside it closes before it), the focused element's listeners
// first; the focus goes back to what had it when it opened; and when the
// field that had the focus goes, the popover takes it back.

import (
	"image"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/composer"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// popEnv lays out a plugin tree as a window's frame does (the frame lock,
// slackc.Frame, the page, slackc.RouteEscape, then composer.KeepFocus),
// beside a focusable stand-in for the composer.
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
	t.Cleanup(func() { e.v.Close(); slackc.SetBridge(nil) })
	e.frames(3) // the window's first frames: layers an earlier test left close
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
	slackc.LockFrame()
	defer slackc.UnlockFrame()
	e.ops.Reset()
	e.now = e.now.Add(20 * time.Millisecond)
	gtx := layout.Context{Ops: &e.ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(800, 600)),
		Now: e.now, Source: e.r.Source()}
	slackc.Frame(gtx)
	for {
		if _, ok := gtx.Event(key.FocusFilter{Target: &e.comp}); !ok {
			break
		}
	}
	area := clip.Rect{Min: image.Pt(0, 560), Max: image.Pt(800, 600)}.Push(gtx.Ops)
	event.Op(gtx.Ops, &e.comp)
	area.Pop()
	if e.nodes != nil {
		e.v.Layout(gtx, e.nodes, "PluginPanel", nil, nil)
	}
	slackc.RouteEscape(gtx)
	composer.KeepFocus(gtx)
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
	return `{"t":"@","n":"Popover","k":` + strconv.Itoa(k) + `,"p":{"isOpen":` + strconv.FormatBool(open) +
		`,"targetBounds":{"x":40,"y":40,"width":120,"height":28},"onClose":{"$fn":` + strconv.FormatInt(fn, 10) + `}},"c":[` + kids + `]}`
}

// popField is a FormTextInput (k) whose onFocus is fn, taking the focus as
// it mounts with autoFocus.
func popField(k int, autoFocus bool, fn int64) string {
	return `{"t":"@","n":"FormTextInput","k":` + strconv.Itoa(k) + `,"p":{"autoFocus":` + strconv.FormatBool(autoFocus) +
		`,"value":"","onFocus":{"$fn":` + strconv.FormatInt(fn, 10) + `}}}`
}

func popText(k int, s string) string {
	return `{"t":"div","k":` + strconv.Itoa(k) + `,"c":["` + s + `"]}`
}

// Escape closes an open Popover: with the popover focused (it took the
// focus as it opened), with Shift held (react-modal checks the key code
// alone), and with the autoFocus field in it focused: the field's keydown
// goes to JS first, and no listener stopping it, on to the popover.
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
	// the plugin kept it open: past the time JS has to close it, the next
	// Escape is its own again
	e.now = e.now.Add(time.Second)
	e.key(key.NameEscape, key.ModShift)
	if !e.called(onClose) {
		t.Error("Shift+Escape didn't close the popover (no onClose)")
	}

	e2 := newPopEnv(t)
	e2.tree(popoverJS(1, true, onClose, `{"t":"div","k":2,"c":[`+popField(3, true, 41)+`]}`))
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
// and only it. Once it's closed the outer one has the focus back (it had
// it when the inner one opened) and the next Escape closes the outer one.
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
		t.Error("the inner popover closed and the focus went nowhere; want the outer popover to have it back")
	}
	e.key(key.NameEscape, 0)
	if got := e.log.take(); !slices.Contains(got, outer) || slices.Contains(got, inner) {
		t.Errorf("Escape after the inner popover closed called %v; want the outer popover's onClose (%d)", got, outer)
	}
}

// A ConfirmationModal in a Popover's content is a layer over it: Escape
// closes the confirm (its onClose), the popover stays; the next one, with
// the confirm gone and the focus back on the popover, closes the popover.
func TestConfirmInsidePopoverClosesFirst(t *testing.T) {
	const onPopClose, onConfirmClose = 31, 51
	confirm := `{"t":"@","n":"ConfirmationModal","k":5,"p":{"title":"Delete it?","onClose":{"$fn":51},"onSubmit":{"$fn":52},"onCancel":{"$fn":53}},"c":["Sure?"]}`
	e := newPopEnv(t)
	e.tree(popoverJS(1, true, onPopClose, popText(2, "menu")+","+confirm))
	e.frames(5)
	e.key(key.NameEscape, 0)
	got := e.log.take()
	if !slices.Contains(got, onConfirmClose) || slices.Contains(got, onPopClose) {
		t.Fatalf("Escape with a confirm in the popover called %v; want the confirm's onClose (%d) alone", got, onConfirmClose)
	}
	e.tree(popoverJS(1, true, onPopClose, popText(2, "menu")))
	e.frames(4)
	e.key(key.NameEscape, 0)
	if got := e.log.take(); !slices.Contains(got, onPopClose) {
		t.Errorf("Escape after the confirm went called %v; want the popover's onClose (%d)", got, onPopClose)
	}
}

// The focus goes back to what had it when the popover opened, once it
// closes or leaves the tree: here the composer, then a plugin's own field.
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
			t.Errorf("popover %s: the focus is on %v; want the composer, which had it when it opened", c.what, e.r.Source().FocusTag())
		}
	}

	// a plugin field opens the popover: it gets the focus back (its onFocus)
	const onFieldFocus = 61
	field := popField(9, true, onFieldFocus)
	e.tree(`[` + field + `]`)
	e.frames(4)
	if !e.called(onFieldFocus) {
		t.Fatal("the plugin field didn't take the focus")
	}
	e.tree(`[` + field + `,` + open + `]`)
	e.frames(4)
	e.log.take()
	e.tree(`[` + field + `,` + popoverJS(1, false, 31, popText(2, "menu")) + `]`)
	e.frames(4)
	if !e.called(onFieldFocus) {
		t.Errorf("the popover the plugin field opened closed: the focus is on %v; want the field (its onFocus)", e.r.Source().FocusTag())
	}
}

// When the field that had the focus goes (a search swapped for results),
// the popover takes the focus back instead of leaving it nowhere.
func TestPopoverTakesFocusBackFromRemovedField(t *testing.T) {
	e := newPopEnv(t)
	e.tree(popoverJS(1, true, 31, `{"t":"div","k":2,"c":[`+popField(3, true, 41)+`]}`))
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

// resultLog is a bridge that answers DispatchResult with stop and
// records the Escape keydowns dispatched.
type resultLog struct {
	callLog
	mu       sync.Mutex
	stop     bool
	keydowns []int64
}

func (b *resultLog) DispatchResult(target int64, ev []byte, done func(bool)) {
	b.mu.Lock()
	if strings.Contains(string(ev), `"key":"Escape"`) && strings.Contains(string(ev), `"type":"keydown"`) {
		b.keydowns = append(b.keydowns, target)
	}
	stop := b.stop
	b.mu.Unlock()
	done(stop)
}

// A plugin's <input> in a Popover gets the Escape keydown first: a
// listener that stops it keeps the popover open (a filter cleared
// instead); with none stopping it, the popover closes.
func TestPopoverInputCanStopEscape(t *testing.T) {
	const onClose = 31
	e := newPopEnv(t)
	rl := &resultLog{stop: true}
	slackc.SetBridge(rl)
	e.log = &rl.callLog
	e.tree(popoverJS(1, true, onClose, `{"t":"input","k":7,"p":{"autoFocus":true}}`))
	e.frames(4)
	e.key(key.NameEscape, 0)
	rl.mu.Lock()
	kd := slices.Clone(rl.keydowns)
	rl.mu.Unlock()
	if len(kd) != 1 || kd[0] != 7 {
		t.Fatalf("Escape keydowns dispatched at %v; want one, at the focused input (7)", kd)
	}
	if e.called(onClose) {
		t.Error("the input's listener stopped the Escape and the popover closed anyway")
	}
	rl.mu.Lock()
	rl.stop = false
	rl.mu.Unlock()
	e.key(key.NameEscape, 0)
	if !e.called(onClose) {
		t.Error("no listener stopped the input's Escape and the popover didn't close")
	}
}

// eventLog is a bridge whose function calls with a DOM event answer
// stop (slackc.EventBridge) and record what the event was.
type eventLog struct {
	callLog
	mu     sync.Mutex
	stop   bool
	events []string // "<fn>:<args>"
}

func (b *eventLog) CallEvent(fn int64, args []byte, done func(bool)) {
	b.add(fn)
	b.mu.Lock()
	b.events = append(b.events, strconv.FormatInt(fn, 10)+":"+string(args))
	stop := b.stop
	b.mu.Unlock()
	done(stop)
}

// A FormTextInput's onKeyDown gets React's synthetic event (it can call
// stopPropagation and preventDefault): one that stops an Escape (a search
// field clearing its query) keeps the Popover around it open; one that
// doesn't, and no DOM listener stopping it either, closes it.
func TestFormTextInputOnKeyDownCanStopEscape(t *testing.T) {
	const onClose, onKeyDown = 31, 71
	e := newPopEnv(t)
	el := &eventLog{stop: true}
	slackc.SetBridge(el)
	e.log = &el.callLog
	field := `{"t":"@","n":"FormTextInput","k":3,"p":{"autoFocus":true,"value":"","onKeyDown":{"$fn":71}}}`
	e.tree(popoverJS(1, true, onClose, `{"t":"div","k":2,"c":[`+field+`]}`))
	e.frames(4)
	e.key(key.NameEscape, 0)
	got := e.log.take()
	if !slices.Contains(got, onKeyDown) {
		t.Fatalf("Escape in the field called %v; want its onKeyDown (%d)", got, onKeyDown)
	}
	if slices.Contains(got, onClose) {
		t.Error("onKeyDown stopped the Escape and the popover closed anyway")
	}
	el.mu.Lock()
	ev := el.events[len(el.events)-1]
	el.mu.Unlock()
	if !strings.Contains(ev, `"$event"`) || !strings.Contains(ev, `"key":"Escape"`) {
		t.Errorf("onKeyDown's argument %s; want an Escape keydown marked as a DOM event ($event)", ev)
	}
	el.mu.Lock()
	el.stop = false
	el.mu.Unlock()
	e.key(key.NameEscape, 0)
	if got := e.log.take(); !slices.Contains(got, onClose) {
		t.Errorf("onKeyDown let the Escape go: calls %v; want the popover's onClose (%d)", got, onClose)
	}
}

// focusedRich is a RichTextInput whose editor takes the focus as it
// mounts (as a click into it does): a composer.Editor, which
// composer.KeepFocus holds when it misses a frame.
func init() {
	slackc.RegisterHost(func(gtx layout.Context, h *slackc.Host) layout.Dimensions {
		d := richTextInput(gtx, h)
		if st := stateOf[richInputState](h); st.ed != nil && !st.focused && gtx.Source.Enabled() && !gtx.Focused(st.ed) {
			gtx.Execute(key.FocusCmd{Tag: st.ed})
		}
		return d
	}, "TestFocusedRichTextInput")
}

// Escape in a Popover's RichTextInput with nothing to cancel closes the
// Popover (the editor passes it on), and when the plugin closes it the
// focus goes back to the composer, which had it when the popover opened:
// the hidden editor doesn't keep it (KeepFocus held it, typing went into
// the hidden box, then the focus went nowhere for good).
func TestRichTextInputPopoverGivesFocusBack(t *testing.T) {
	const onClose = 31
	e := newPopEnv(t)
	e.frames(1)
	e.r.Source().Execute(key.FocusCmd{Tag: &e.comp})
	e.frames(2)
	rich := `{"t":"@","n":"TestFocusedRichTextInput","k":3,"p":{"value":null}}`
	e.tree(popoverJS(1, true, onClose, `{"t":"div","k":2,"c":[`+rich+`]}`))
	e.frames(4)
	if e.r.Source().Focused(&e.comp) || e.r.Source().Focused(nil) {
		t.Fatalf("the popover's editor didn't take the focus (on %v)", e.r.Source().FocusTag())
	}
	e.key(key.NameEscape, 0)
	if !e.called(onClose) {
		t.Fatal("Escape in the popover's RichTextInput didn't close the popover (no onClose)")
	}
	e.tree(popoverJS(1, false, onClose, `{"t":"div","k":2,"c":[`+rich+`]}`))
	e.frames(1)
	if !e.r.Source().Focused(&e.comp) {
		t.Errorf("the popover closed: the focus is on %v; want the composer back, in that frame", e.r.Source().FocusTag())
	}
	e.frames(8)
	if !e.r.Source().Focused(&e.comp) {
		t.Errorf("frames after the popover closed the focus is on %v; want the composer", e.r.Source().FocusTag())
	}
}
