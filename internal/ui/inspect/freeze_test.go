package inspect

// The freeze (freeze.go) on the tool alone, through a real Gio router
// (dragEnv, whose frame runs Frame first as the window does): F holds the
// frame on screen, which the window then draws again with nothing laid
// out, and the picker goes on over it. Its captures are shot from that
// frame: a Shift+click copies at once, a plain click and a drag are
// reviewed over it. A drag too small to grab keeps the freeze and says so
// in the banner; Esc mid-drag drops the drag only. F again or Esc
// unfreezes, as does a new window size, the frozen ops go, and the next
// frame starts the pointer state over before it lays out.

import (
	"image"
	"image/color"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
	"weak"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
)

// freezeEnv is a dragEnv whose window draws a widget's area and counts
// its layouts, with the session started and the pointer in the primary
// area. Each layout draws a different number of ops, so two frames' ops
// (End's call) never compare equal.
func freezeEnv(t *testing.T) (e *dragEnv, laid *int, at f32.Point) {
	e = newDragEnv(t)
	laid = new(int)
	widget := new(int)
	e.content = func(gtx layout.Context) {
		*laid++
		for range *laid % 4 {
			paint.ColorOp{Color: color.NRGBA{A: 0xff}}.Add(gtx.Ops)
		}
		defer clip.Rect(image.Rect(0, 0, 100, 100)).Push(gtx.Ops).Pop()
		event.Op(gtx.Ops, widget)
	}
	at = f32.Pt(300, 200)
	e.startAt(at, "primary")
	return e, laid, at
}

func (e *dragEnv) freeze() {
	e.t.Helper()
	e.key(FreezeKey, 0)
	e.frame()
	if !e.tool.Frozen() {
		e.t.Fatal("F didn't freeze")
	}
}

func pointerMove(p f32.Point) pointer.Event { return pointer.Event{Kind: pointer.Move, Position: p} }

// hint is the session's banner now.
func (e *dragEnv) hint() string {
	gtx := layout.Context{Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Now: time.Now(), Source: e.r.Source()}
	return e.tool.sessionHint(gtx)
}

func TestFreezeHoldsTheFrameOnScreen(t *testing.T) {
	e, laid, at := freezeEnv(t)
	if got := e.hint(); got != HintInspect || !strings.Contains(got, "F freezes") {
		t.Errorf("the banner %q, want %q", got, HintInspect)
	}
	onScreen := e.tool.content
	n := *laid
	e.freeze()
	if e.tool.fz.content != onScreen {
		t.Error("the freeze holds another frame than the one on screen")
	}
	if !e.frozen || *laid != n {
		t.Errorf("the F frame: drawn frozen %v, %d layouts; want it drawn from the frozen frame, none laid out", e.frozen, *laid-n)
	}
	if !slices.Equal(e.host.frozen, []bool{true}) {
		t.Errorf("the host heard %v, want a freeze", e.host.frozen)
	}
	if got := e.hint(); got != HintFrozen || !strings.Contains(got, "F or Esc unfreezes") {
		t.Errorf("frozen, the banner %q, want %q", got, HintFrozen)
	}
	for range 3 {
		e.frame()
	}
	if *laid != n || !e.frozen {
		t.Errorf("%d layouts while frozen (the last frame drawn frozen %v)", *laid-n, e.frozen)
	}
	// the picker goes on, over the frozen frame
	if label, _, ok := e.tool.Hovered(); !ok || label != "primary" {
		t.Errorf("frozen: outline %q (%v), want the primary area", label, ok)
	}
	e.queue(pointerMove(f32.Pt(100, 200)))
	e.frame()
	if label, _, ok := e.tool.Hovered(); !ok || label != "sidebar" {
		t.Errorf("frozen, the pointer in the sidebar: outline %q (%v), want the sidebar", label, ok)
	}
	e.queue(pointerMove(at))
	e.frame()

	// F again unfreezes: that frame lays out, the pointer state started
	// over first; the session goes on
	e.key(FreezeKey, 0)
	e.frame()
	if e.tool.Frozen() || !e.tool.Active() || *laid == n {
		t.Fatalf("after F again: frozen %v, session %v, laid out %v", e.tool.Frozen(), e.tool.Active(), *laid != n)
	}
	if !slices.Equal(e.host.frozen, []bool{true, false}) || e.host.reset {
		t.Errorf("the host heard %v (reset pending %v), want a freeze and its end, the reset done", e.host.frozen, e.host.reset)
	}
	// Esc unfreezes too, the session on; Esc again ends it
	e.frame()
	e.freeze()
	e.key(key.NameEscape, 0)
	e.frame()
	if e.tool.Frozen() || !e.tool.Active() {
		t.Fatalf("after Esc: frozen %v, session %v", e.tool.Frozen(), e.tool.Active())
	}
	e.key(key.NameEscape, 0)
	e.frame()
	if e.tool.Active() {
		t.Error("Esc again didn't end the session")
	}
}

func TestFreezeCapturesTheFrozenFrame(t *testing.T) {
	e, _, at := freezeEnv(t)
	var shotFrom []op.CallOp
	fake := Shoot
	Shoot = func(f op.CallOp, region image.Rectangle, k float32) (*image.RGBA, error) {
		shotFrom = append(shotFrom, f)
		return fake(f, region, k)
	}
	t.Cleanup(func() { Shoot = fake })

	// Shift+click: the frozen frame shot and copied at once, the session
	// and the freeze over
	e.freeze()
	frozen := e.tool.fz.content
	e.shiftClick(at, true, true)
	e.frame()
	e.tool.Wait()
	if e.tool.Active() || e.tool.Frozen() || e.tool.Reviewing() {
		t.Fatalf("after a Shift+click: session %v, frozen %v, reviewing %v", e.tool.Active(), e.tool.Frozen(), e.tool.Reviewing())
	}
	if len(shotFrom) != 1 || shotFrom[0] != frozen || e.copied == nil {
		t.Fatalf("after a Shift+click: %d shots (of the frozen frame %v), copied %v", len(shotFrom), len(shotFrom) > 0 && shotFrom[0] == frozen, e.copied != nil)
	}

	// a plain click: the review, over the frozen frame, of its shot;
	// Enter copies, the session and the freeze over
	e.copied = nil
	e.startAt(at, "primary")
	e.freeze()
	frozen = e.tool.fz.content
	e.press(at)
	e.releaseAt(at)
	e.frame()
	if !e.tool.Reviewing() || !e.tool.Frozen() || len(shotFrom) != 2 || shotFrom[1] != frozen {
		t.Fatalf("after a plain click: reviewing %v, frozen %v, the frozen frame shot %v", e.tool.Reviewing(), e.tool.Frozen(), len(shotFrom) == 2 && shotFrom[1] == frozen)
	}
	e.untilReady()
	e.key(key.NameReturn, 0)
	e.frame()
	e.tool.Wait()
	if e.tool.Active() || e.tool.Frozen() || e.copied == nil {
		t.Fatalf("after Enter: session %v, frozen %v, copied %v", e.tool.Active(), e.tool.Frozen(), e.copied != nil)
	}

	// a drag: its region of the frozen frame, reviewed
	e.startAt(at, "primary")
	e.freeze()
	frozen = e.tool.fz.content
	from, to := f32.Pt(150, 120), f32.Pt(450, 320)
	e.press(from)
	e.frame()
	e.dragTo(to)
	e.frame()
	e.releaseAt(to)
	e.frame()
	if !e.tool.Reviewing() || !e.tool.Frozen() || len(shotFrom) != 3 || shotFrom[2] != frozen || e.shots[2] != image.Rect(150, 120, 450, 320) {
		t.Fatalf("after a drag: reviewing %v, frozen %v, shots %v (of the frozen frame %v)", e.tool.Reviewing(), e.tool.Frozen(), e.shots, len(shotFrom) == 3 && shotFrom[2] == frozen)
	}
	e.key(key.NameEscape, 0)
	e.frame()
	if e.tool.Active() || e.tool.Frozen() {
		t.Errorf("Esc in the review: session %v, frozen %v", e.tool.Active(), e.tool.Frozen())
	}
}

func TestFreezeDragsAndEnds(t *testing.T) {
	e, _, at := freezeEnv(t)
	e.freeze()
	toasts := len(e.host.toasts)

	// a drag too small to grab: frozen still, the banner says why (the
	// window draws no toast while frozen), and the picker goes on
	e.press(at)
	e.frame()
	e.dragTo(at.Add(f32.Pt(30, 3)))
	e.frame()
	e.releaseAt(at.Add(f32.Pt(30, 3)))
	e.frame()
	if !e.tool.Frozen() || !e.tool.Active() {
		t.Fatalf("after a thin drag: frozen %v, session %v", e.tool.Frozen(), e.tool.Active())
	}
	if hint := e.hint(); !strings.Contains(hint, "drag a bigger region") || len(e.host.toasts) != toasts {
		t.Errorf("after a thin drag: the banner %q, toasts %v; want the banner to say it", hint, e.host.toasts[toasts:])
	}
	e.frame()
	if label, _, ok := e.tool.Hovered(); !ok || label != "primary" {
		t.Errorf("after a thin drag: outline %q (%v), want the primary area", label, ok)
	}
	// Esc mid-drag drops the drag, frozen still
	e.press(at)
	e.frame()
	e.dragTo(at.Add(f32.Pt(80, 60)))
	e.frame()
	e.key(key.NameEscape, 0)
	e.frame()
	if _, ok := e.tool.Dragging(); ok || !e.tool.Frozen() {
		t.Fatalf("Esc mid-drag: dragging %v, frozen %v", ok, e.tool.Frozen())
	}
	e.releaseAt(at.Add(f32.Pt(80, 60)))
	e.frame()
	if len(e.shots) != 0 {
		t.Fatalf("%d shots without a capture", len(e.shots))
	}

	// the frozen ops go when the freeze ends
	frozenOps := weak.Make(e.tool.FrozenOps())
	e.key(FreezeKey, 0)
	e.frame()
	e.frame()
	if e.tool.Frozen() || e.tool.FrozenOps() != nil {
		t.Fatal("F didn't unfreeze")
	}
	runtime.GC()
	runtime.GC()
	if frozenOps.Value() != nil {
		t.Error("the frozen ops outlived the freeze")
	}

	// a new window size unfreezes
	e.freeze()
	e.size, e.host.size = image.Pt(800, 600), image.Pt(800, 600)
	e.frame()
	if e.tool.Frozen() || !e.tool.Active() {
		t.Errorf("a new window size: frozen %v, session %v; want unfrozen, the session on", e.tool.Frozen(), e.tool.Active())
	}
	if n := len(e.host.toasts); n == 0 || !strings.Contains(e.host.toasts[n-1], "unfrozen") {
		t.Errorf("a new window size: toasts %v, want one saying it unfroze", e.host.toasts)
	}
	// and a session's end does
	e.frame()
	e.freeze()
	e.key("D", key.ModShortcut|key.ModAlt|key.ModShift) // the tool's shortcut
	e.frame()
	if e.tool.Active() || e.tool.Frozen() {
		t.Errorf("the shortcut while frozen: session %v, frozen %v", e.tool.Active(), e.tool.Frozen())
	}
}

// hold is a key's press alone, as a held key repeats it.
func (e *dragEnv) hold(name key.Name) {
	e.r.Queue(key.Event{Name: name, State: key.Press})
}

// A held F or Esc repeats its press: only the first counts, until the key
// comes up. F held freezes once; Esc held while frozen unfreezes and
// doesn't then end the session.
func TestFreezeKeysIgnoreRepeats(t *testing.T) {
	e, _, _ := freezeEnv(t)
	e.hold(FreezeKey)
	e.frame()
	if !e.tool.Frozen() {
		t.Fatal("F didn't freeze")
	}
	for range 3 { // the key held
		e.hold(FreezeKey)
		e.frame()
	}
	if !e.tool.Frozen() {
		t.Fatal("F held: its repeats unfroze the window")
	}
	e.r.Queue(key.Event{Name: FreezeKey, State: key.Release})
	e.key(FreezeKey, 0)
	e.frame()
	if e.tool.Frozen() {
		t.Fatal("F pressed again after its release didn't unfreeze")
	}
	e.frame()
	e.freeze()
	e.hold(key.NameEscape)
	e.frame()
	if e.tool.Frozen() || !e.tool.Active() {
		t.Fatalf("Esc: frozen %v, session %v", e.tool.Frozen(), e.tool.Active())
	}
	for range 3 {
		e.hold(key.NameEscape)
		e.frame()
	}
	if !e.tool.Active() {
		t.Fatal("Esc held: its repeats ended the session after the unfreeze")
	}
	e.r.Queue(key.Event{Name: key.NameEscape, State: key.Release})
	e.key(key.NameEscape, 0)
	e.frame()
	if e.tool.Active() {
		t.Error("Esc pressed again after its release didn't end the session")
	}
}

// Esc with the button down over the frozen frame (a press not yet a
// drag) drops only the press, as it does a drag: frozen still, the
// session on. The release that follows picks nothing.
func TestFreezeEscWhilePressing(t *testing.T) {
	e, _, at := freezeEnv(t)
	e.freeze()
	e.press(at)
	e.frame()
	e.key(key.NameEscape, 0)
	e.frame()
	if !e.tool.Frozen() || !e.tool.Active() {
		t.Fatalf("Esc with the button down: frozen %v, session %v; want the press dropped only", e.tool.Frozen(), e.tool.Active())
	}
	e.releaseAt(at)
	e.frame()
	e.frame()
	if len(e.shots) != 0 || e.tool.Reviewing() {
		t.Errorf("the release after Esc picked: %d shots, reviewing %v", len(e.shots), e.tool.Reviewing())
	}
	if label, _, ok := e.tool.Hovered(); !ok || label != "primary" {
		t.Errorf("after the dropped press: outline %q (%v), want the primary area again", label, ok)
	}
}

// The window losing the focus while frozen (Gio sends a pointer Cancel at
// 0,0) doesn't move the outline to the window's corner: the pointer is
// where it was, the tool just doesn't see it until it moves.
func TestFreezeWindowBlur(t *testing.T) {
	e, _, at := freezeEnv(t)
	e.freeze()
	e.queue(pointer.Event{Kind: pointer.Cancel})
	e.frame()
	e.frame()
	if label, _, ok := e.tool.Hovered(); ok && label != "primary" {
		t.Errorf("after a blur: outline %q, want none or the primary area under the pointer", label)
	}
	e.queue(pointerMove(at))
	e.frame()
	if label, _, ok := e.tool.Hovered(); !ok || label != "primary" {
		t.Errorf("the pointer back: outline %q (%v), want the primary area", label, ok)
	}
}

// While frozen every key press is the tool's, so none goes unhandled
// (macOS beeps for an unhandled key: ← → Home End and the like, which the
// frozen frame's composer would have taken).
func TestFreezeTakesEveryKey(t *testing.T) {
	e, _, _ := freezeEnv(t)
	e.freeze()
	e.frame()
	for _, n := range []key.Name{key.NameLeftArrow, key.NameRightArrow, key.NameHome, "X"} {
		e.r.Queue(key.Event{Name: n, State: key.Press})
		if _, ok := e.r.Source().Event(key.Filter{Name: n}); !ok {
			t.Errorf("frozen, %s went unhandled", n)
		}
	}
	if !e.tool.Frozen() || !e.tool.Active() {
		t.Errorf("after the keys: frozen %v, session %v", e.tool.Frozen(), e.tool.Active())
	}
}

// A frozen session that ends lets its frozen record's held state go with
// it, not only its nodes.
func TestFreezeEndReleasesHold(t *testing.T) {
	e, _, _ := freezeEnv(t)
	e.freeze()
	e.frame()
	held := false
	for i := range e.tool.frames {
		held = held || e.tool.frames[i].Held()
	}
	if !held {
		t.Fatal("frozen, no record is held")
	}
	e.key(key.NameEscape, 0)
	e.frame()
	e.key(key.NameEscape, 0)
	e.frame()
	if e.tool.Active() {
		t.Fatal("Esc twice didn't end the session")
	}
	for i := range e.tool.frames {
		if e.tool.frames[i].Held() {
			t.Errorf("after the session record %d is still held", i)
		}
	}
}
