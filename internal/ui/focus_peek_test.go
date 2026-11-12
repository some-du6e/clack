package ui

import (
	"image"
	"math"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// focusEnv is the whole client against fakeslack with focus mode
// (silent_sidebar) on, a GPU window when STATUS_SHOTS_DIR is set, and the
// users.prefs.set calls it makes.
type focusEnv struct {
	*perfEnv
	mu   sync.Mutex
	sets []url.Values
	ptrT time.Duration // pointer event times (two clicks at one time are a double click)
}

func newFocusEnv(t *testing.T, prep func(fs *fakeslack.Server)) *focusEnv {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	if prep != nil {
		prep(fs)
	}
	e := &focusEnv{perfEnv: startEnv(t, fs, t.TempDir())}
	if os.Getenv("STATUS_SHOTS_DIR") != "" && e.gpu == nil {
		if w, err := pixeltest.NewWindow(e.size.X, e.size.Y); err == nil {
			e.gpu = w
		} else {
			t.Logf("no GPU renderer (%v): no shots", err)
		}
	}
	fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method == "users.prefs.set" {
			e.mu.Lock()
			e.sets = append(e.sets, f)
			e.mu.Unlock()
		}
		return nil, false
	})
	e.waitReady()
	e.a.Core().SetExperimentGroup("silent_sidebar", "on")
	e.settle(150 * time.Millisecond)
	if e.a.fm().trig.Empty() {
		t.Fatal("no focus mode button in the control strip")
	}
	return e
}

func (e *focusEnv) prefSets() []url.Values {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]url.Values(nil), e.sets...)
}

func (e *focusEnv) move(p image.Point) {
	e.ptrT += 10 * time.Millisecond
	e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(p.X), float32(p.Y)), Time: e.ptrT})
	e.frame()
}

func (e *focusEnv) clickOn(p image.Point) {
	e.ptrT += time.Second // never a double click
	e.clickAt(p, e.ptrT)
	e.frame()
	e.frame()
}

// rest runs frames for d without input.
func (e *focusEnv) rest(d time.Duration) {
	for dl := time.Now().Add(d); time.Now().Before(dl); {
		e.frame()
		time.Sleep(3 * time.Millisecond)
	}
}

func (e *focusEnv) trigMid() image.Point {
	r := e.a.fm().trig
	return r.Min.Add(r.Size().Div(2))
}

// Karim, Windows, 2026-11-12: "this is kinda finicky". Slack's focus mode
// popover is a PeekTrigger's hover peek (SilentSidebarTrigger, 20Sq):
// 300 ms after the pointer comes onto the button, no scrim, no keyboard
// focus; leaving the safe area closes it; the button stays clickable
// under it, so one click turns focus mode off. Clack's was a click popover
// with a window-wide scrim: it took the focus, stayed open after the
// pointer left, and ate the click meant for the button.
func TestFocusPeekHover(t *testing.T) {
	e := newFocusEnv(t, nil)
	a := e.a
	f := a.fm()
	mid := e.trigMid()
	away := image.Pt(f.trig.Max.X+700, max(f.trig.Min.Y-400, 10))

	// off: the tooltip's button, no peek
	e.move(away)
	e.move(mid)
	e.rest(500 * time.Millisecond)
	if a.focusPeekOpen() {
		t.Fatal("a peek opened while focus mode is off")
	}

	// a click turns it on: one write, the view transition, and the peek
	// only once the transition is over and the trigger delay has passed
	t0 := time.Now()
	e.clickOn(mid)
	if !a.focusModeActive() {
		t.Fatal("the click didn't turn focus mode on")
	}
	if !a.focusFading(time.Now()) {
		t.Error("no view transition on the toggle")
	}
	e.until("the write", 5*time.Second, func() bool { return len(e.prefSets()) == 1 })
	if s := e.prefSets()[0]; s.Get("name") != "focus_mode_expiration_ts" || s.Get("value") != "9999999999" {
		t.Errorf("toggle wrote %v", s)
	}
	e.until("the peek", 3*time.Second, a.focusPeekOpen)
	if d := time.Since(t0); d < 600*time.Millisecond {
		t.Errorf("the peek opened %v after the click: before the transition (350 ms) and the delay (300 ms)", d)
	}
	pop := f.pop
	if e.r.Source().Focused(pop) {
		t.Error("the hover peek took the keyboard focus")
	}
	e.rest(250 * time.Millisecond) // p-peek_card--animate-in
	card := pop.Rect()
	// right-top, offsetX 12, offsetY 15: the card's bottom 15 below the
	// button's (keepInBounds keeps it 10 inside the window)
	if card.Min.X != f.trig.Max.X+12 {
		t.Errorf("card at x %d, want the button's right + 12 = %d", card.Min.X, f.trig.Max.X+12)
	}
	if want := min(f.trig.Max.Y+15, e.size.Y-10); card.Max.Y != want {
		t.Errorf("card bottom %d, want %d", card.Max.Y, want)
	}
	e.shotBoth("focus-peek-open")

	// through the card it stays; out of the safe area it closes
	e.move(image.Pt(card.Min.X+30, card.Min.Y+card.Dy()/2))
	e.rest(60 * time.Millisecond)
	if !a.focusPeekOpen() {
		t.Fatal("moving onto the card closed the peek")
	}
	e.move(away)
	e.rest(60 * time.Millisecond) // the throttle's trailing call
	if a.focusPeekOpen() {
		t.Fatal("the peek stayed open after the pointer left the button and the card")
	}

	// back on the button: the delay, then the peek; one click on the
	// button turns focus mode off (no scrim in the way) and the peek goes
	e.move(mid)
	t1 := time.Now()
	e.until("the peek again", 3*time.Second, a.focusPeekOpen)
	if d := time.Since(t1); d < focusPeekDelay-20*time.Millisecond {
		t.Errorf("reopened after %v, before the 300 ms trigger delay", d)
	}
	e.clickOn(mid)
	if a.focusModeActive() {
		t.Fatal("clicking the button under its peek didn't turn focus mode off (the click went elsewhere)")
	}
	e.until("the off write", 5*time.Second, func() bool { return len(e.prefSets()) == 2 })
	if v := e.prefSets()[1].Get("value"); v != "0" {
		t.Errorf("off wrote %q", v)
	}
	e.frame()
	if a.focusPeekOpen() {
		t.Error("the peek outlived focus mode")
	}
}

// Escape closes the peek; with the pointer still on the button it opens
// again only after two moves on it (PeekTrigger's ep / eh). A right press
// on the button closes it too.
func TestFocusPeekEscape(t *testing.T) {
	e := newFocusEnv(t, func(fs *fakeslack.Server) {
		fs.SetPrefs(map[string]any{"focus_mode_expiration_ts": focusForever})
	})
	a := e.a
	if !a.focusModeActive() {
		t.Fatal("focus mode isn't on from the pref")
	}
	mid := e.trigMid()
	e.move(image.Pt(mid.X+400, mid.Y-300))
	e.move(mid)
	e.until("the peek", 3*time.Second, a.focusPeekOpen)
	e.key(key.NameEscape, 0)
	e.frame()
	e.frame()
	if a.focusPeekOpen() {
		t.Fatal("Escape left the peek open")
	}
	if !a.focusModeActive() {
		t.Fatal("Escape turned focus mode off")
	}
	e.rest(500 * time.Millisecond)
	if a.focusPeekOpen() {
		t.Fatal("the peek came back with the pointer resting on the button")
	}
	e.move(mid.Add(image.Pt(1, 0)))
	e.rest(450 * time.Millisecond)
	if a.focusPeekOpen() {
		t.Fatal("one move after Escape reopened it (eh: the first move only rearms)")
	}
	e.move(mid)
	e.until("the peek after the second move", 3*time.Second, a.focusPeekOpen)

	// a secondary press on the trigger closes it (forceOpenOnClick keeps
	// only a plain click)
	pt := f32.Pt(float32(mid.X), float32(mid.Y))
	e.ptrT += time.Second
	e.r.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonSecondary, Position: pt, Time: e.ptrT},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pt, Time: e.ptrT + time.Millisecond})
	e.frame()
	e.frame()
	if a.focusPeekOpen() {
		t.Error("a right press on the button left the peek open")
	}
}

// The Duration select opens its menu over the window; the peek stays open
// while it shows (the menu is the card's child), a pick writes the pref at
// once, and the peek is still there afterwards.
func TestFocusPeekDuration(t *testing.T) {
	e := newFocusEnv(t, func(fs *fakeslack.Server) {
		fs.SetPrefs(map[string]any{"focus_mode_expiration_ts": focusForever})
	})
	a := e.a
	f := a.fm()
	mid := e.trigMid()
	e.move(image.Pt(mid.X+400, mid.Y-300))
	e.move(mid)
	e.until("the peek", 3*time.Second, a.focusPeekOpen)
	e.rest(250 * time.Millisecond)
	card := f.pop.Rect()
	sel := f.durRect.Add(card.Min)
	if sel.Empty() || !sel.In(card) {
		t.Fatalf("the Duration select %v isn't on the card %v", sel, card)
	}
	selMid := sel.Min.Add(sel.Size().Div(2))
	e.move(image.Pt(card.Min.X+20, card.Min.Y+20))
	e.move(selMid)
	e.clickOn(selMid)
	if !a.focusChildOpen() {
		t.Fatal("the Duration select didn't open its options")
	}
	e.shotBoth("focus-peek-duration")
	// anywhere while the menu shows: still open
	e.move(image.Pt(card.Max.X+300, card.Min.Y-100))
	e.rest(60 * time.Millisecond)
	if !a.focusPeekOpen() || !a.focusChildOpen() {
		t.Fatalf("moving off the card with the select's menu open: peek %v, menu %v", a.focusPeekOpen(), a.focusChildOpen())
	}
	// four Ups reach the first row from anywhere ("30 minutes"), Return
	// picks it
	before := time.Now().Unix()
	for i := 0; i < 4; i++ {
		e.key(key.NameUpArrow, 0)
		e.frame()
	}
	e.key(key.NameReturn, 0)
	e.frame()
	e.frame()
	if a.focusChildOpen() {
		t.Fatal("picking an option left the menu open")
	}
	e.until("the duration write", 5*time.Second, func() bool { return len(e.prefSets()) == 1 })
	s := e.prefSets()[0]
	if s.Get("name") != "focus_mode_expiration_ts" || f.option != "half_hour" {
		t.Fatalf("pick wrote %v, option %q", s, f.option)
	}
	if exp := a.focusExp(); exp < before+1800 || exp > time.Now().Unix()+1800 {
		t.Errorf("30 minutes: %d", exp)
	}
	e.frame()
	if !a.focusPeekOpen() {
		t.Error("the pick closed the peek")
	}
}

// The button's transitions (client-boot-styles): hovering scales it to
// 1.1 over .125s cubic-bezier(.17,.67,.55,1.09) and tilts it −15° with
// its two stars fading in over .2s ease-in-out; leaving reverses them,
// shortened to the share already run.
func TestFocusButtonTransitions(t *testing.T) {
	var tr cssTransition
	t0 := time.Unix(1900000000, 0)
	if v := tr.value(t0, 0, 200*time.Millisecond, cssEaseInOut, false); v != 0 {
		t.Fatalf("first value %v", v)
	}
	if v := tr.value(t0, 1, 200*time.Millisecond, cssEaseInOut, false); v != 0 {
		t.Errorf("at the start %v", v)
	}
	if v := tr.value(t0.Add(100*time.Millisecond), 1, 200*time.Millisecond, cssEaseInOut, false); math.Abs(v-.5) > .01 {
		t.Errorf("halfway (ease-in-out is symmetric) %v", v)
	}
	if !tr.running(t0.Add(150 * time.Millisecond)) {
		t.Error("not running at 150 ms")
	}
	// reversed at 100 ms: back to 0 in 100 ms (half of it ran)
	v := tr.value(t0.Add(100*time.Millisecond), 0, 200*time.Millisecond, cssEaseInOut, false)
	if math.Abs(v-.5) > .01 || tr.dur != 100*time.Millisecond {
		t.Errorf("reversal from %v over %v, want .5 over 100ms", v, tr.dur)
	}
	if v := tr.value(t0.Add(201*time.Millisecond), 0, 200*time.Millisecond, cssEaseInOut, false); v != 0 || tr.running(t0.Add(201*time.Millisecond)) {
		t.Errorf("after the reversal %v", v)
	}
	// the strip's curve overshoots 1.1 a little before settling
	var sc cssTransition
	sc.value(t0, 1, 125*time.Millisecond, stripEase, false)
	peak := 0.0
	for ms := 0; ms <= 125; ms += 5 {
		peak = math.Max(peak, sc.value(t0.Add(time.Duration(ms)*time.Millisecond), 1.1, 125*time.Millisecond, stripEase, false))
	}
	if peak <= 1.1 || peak > 1.11 {
		t.Errorf("scale peak %v, want just over 1.1 (cubic-bezier(.17,.67,.55,1.09))", peak)
	}
	if v := sc.value(t0.Add(time.Second), 1.1, 125*time.Millisecond, stripEase, false); v != 1.1 {
		t.Errorf("settled scale %v", v)
	}
	// reduced motion snaps
	if v := sc.value(t0.Add(time.Second), 1, 125*time.Millisecond, stripEase, true); v != 1 || sc.running(t0.Add(time.Second)) {
		t.Errorf("snap %v", v)
	}

	// the button itself: hovered, it tilts and its stars come in; the
	// frames stop once the transitions are done
	e := newFocusEnv(t, nil)
	p := &e.a.fm().peek
	mid := e.trigMid()
	e.move(image.Pt(mid.X+400, mid.Y-300))
	e.rest(300 * time.Millisecond)
	if p.tilt.to != 0 || p.stars.to != 0 || p.scale.to != 1 {
		t.Fatalf("idle: tilt %v stars %v scale %v", p.tilt.to, p.stars.to, p.scale.to)
	}
	e.move(mid)
	e.frame()
	if p.tilt.to != 1 || p.stars.to != 1 || p.scale.to != 1.1 {
		t.Fatalf("hovered: tilt %v stars %v scale %v", p.tilt.to, p.stars.to, p.scale.to)
	}
	e.rest(300 * time.Millisecond)
	now := time.Now()
	if p.tilt.running(now) || p.stars.running(now) || p.scale.running(now) {
		t.Error("still moving 300 ms after the hover")
	}
	e.shotBoth("focus-button-hover")
}

// Turning focus mode on fades the palette into the silent sidebar's
// (eLK8's view transition: 0.35s ease-in-out; 0.25s, Chromium's own,
// under reduced motion), and ends on it exactly.
func TestFocusToggleFade(t *testing.T) {
	for _, reduce := range []bool{false, true} {
		e := newFocusEnv(t, nil)
		a := e.a
		if reduce {
			a.motionOverride = 1
		} else {
			a.motionOverride = -1
		}
		before := a.th.C
		gen := a.th.Gen
		a.toggleFocusMode()
		f := a.fm()
		if !f.fade.active {
			t.Fatalf("reduce %v: no transition", reduce)
		}
		want := 350 * time.Millisecond
		if reduce {
			want = 250 * time.Millisecond
		}
		if f.fade.dur != want {
			t.Errorf("reduce %v: %v, want %v", reduce, f.fade.dur, want)
		}
		if a.th.C != before {
			t.Errorf("reduce %v: the toggle's frame already shows the new palette", reduce)
		}
		after := f.fade.to
		if after.SidebarBg == before.SidebarBg {
			t.Fatalf("reduce %v: the silent sidebar theme changes nothing", reduce)
		}
		start := time.Now()
		e.frameAt(start)
		e.frameAt(start.Add(want / 2))
		mid := a.th.C.SidebarBg
		if mid == before.SidebarBg || mid == after.SidebarBg {
			t.Errorf("reduce %v: halfway the sidebar is %v (from %v to %v)", reduce, mid, before.SidebarBg, after.SidebarBg)
		}
		if !a.focusFading(start.Add(want / 2)) {
			t.Errorf("reduce %v: not fading halfway", reduce)
		}
		e.frameAt(start.Add(want + time.Millisecond))
		if a.th.C != after || f.fade.active || a.th.Gen == gen {
			t.Errorf("reduce %v: ended on %v (want %v), active %v, gen %d→%d", reduce, a.th.C.SidebarBg, after.SidebarBg, f.fade.active, gen, a.th.Gen)
		}
	}
}

// frameAt lays out one frame at now (and renders it with a GPU).
func (e *perfEnv) frameAt(now time.Time) {
	a := e.a
	a.drain()
	e.now = now
	gtx := e.context()
	a.frame(gtx)
	e.r.Frame(&e.ops)
	if e.gpu != nil {
		if err := e.gpu.Frame(&e.ops); err != nil {
			e.tb.Fatal(err)
		}
	}
}

// shotBoth for STATUS_SHOTS_DIR.
func (e *focusEnv) shotBoth(name string) {
	dir := os.Getenv("STATUS_SHOTS_DIR")
	if dir == "" || e.gpu == nil {
		return
	}
	for _, dark := range []bool{false, true} {
		e.setDark(dark)
		e.rest(150 * time.Millisecond)
		e.shotTo(e.tb.(*testing.T), dir, name+map[bool]string{false: "-light", true: "-dark"}[dark])
	}
	e.setDark(false)
	e.rest(100 * time.Millisecond)
}
