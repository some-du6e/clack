package slackc

// A plugin's patched roots stay mounted while their window still draws
// them. slackc used to sweep every root not drawn for 5 s against
// whichever frame ran, before that frame drew anything: a window that sat
// without a frame (unfocused, no caret blinking) came back to every root
// unmounted and mounted again (plugin state lost, the native drawn until
// JS answered: Show Real User's names flashed back to the relay bot), and
// the huddle window's frames swept the main window's roots while it sat
// still. Each window's own frames age the roots it drew now (frame.go).

import (
	"encoding/json"
	"image"
	"image/color"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
)

// keyBridge records the root each Card (by its card prop: JS never gets
// "$key") last rendered into, and the roots unmounted.
type keyBridge struct {
	recBridge
	root map[string]uint64
}

func (b *keyBridge) Render(root uint64, name string, props []byte) {
	b.recBridge.Render(root, name, props)
	var p struct {
		Card string `json:"card"`
	}
	_ = json.Unmarshal(props, &p)
	b.root[p.Card] = root
}

func (b *keyBridge) gone(root uint64) bool {
	for _, r := range b.unmounted {
		if r == root {
			return true
		}
	}
	return false
}

// sweepEnv patches Card with a keyBridge. The clock starts an hour from
// now, past every frame other tests drew, and the main window's clock
// starts over (as in a fresh process), so no earlier frame's time holds a
// sweep back: a window sweeps at most every 2 s.
func sweepEnv(t *testing.T) (*keyBridge, time.Time) {
	t.Helper()
	Reset()
	fresh := func() { mainClock.now, mainClock.swept = time.Time{}, time.Time{} }
	fresh()
	b := &keyBridge{root: map[string]uint64{}}
	SetBridge(b)
	SetPatched([]string{"Card"})
	t.Cleanup(func() { Reset(); SetBridge(nil); SetPatched(nil); fresh() })
	return b, time.Now().Add(time.Hour)
}

// cardTree is a plugin's tree for a Card: a div around its <Original>.
const cardTree = `[{"t":"div","k":1,"c":[{"t":"@","n":"Card","k":2}]}]`

func cardCtx(at time.Time) layout.Context {
	return layout.Context{Ops: new(op.Ops), Now: at, Constraints: layout.Exact(image.Pt(400, 300))}
}

// cards draws the patched Card of each key, as their owners do every frame.
func cards(gtx layout.Context, keys []string) {
	for _, k := range keys {
		Render(gtx, "Card", Props{"$key": k, "card": k}, func(gtx layout.Context, p Props) layout.Dimensions {
			return layout.Dimensions{Size: image.Pt(100, 40)}
		})
	}
}

// mainFrame is one frame of the main window at at (the ui root's Frame,
// then its layout, under the frame lock), drawing the Cards keys.
func mainFrame(at time.Time, keys ...string) {
	LockFrame()
	defer UnlockFrame()
	gtx := cardCtx(at)
	Frame(gtx)
	cards(gtx, keys)
}

// subWinFrame is one frame of the secondary window w (the huddle window's
// loop) at at, drawing the Cards keys.
func subWinFrame(w *SubWindow, at time.Time, keys ...string) {
	LockFrame()
	defer UnlockFrame()
	gtx := cardCtx(at)
	restore := w.Frame(gtx, "p-multi_view_window p-huddle_window", color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})
	cards(gtx, keys)
	restore()
}

// draws is how many windows' draws the Card key keeps (frame lock).
func draws(key string) int {
	LockFrame()
	defer UnlockFrame()
	for k, in := range insts {
		if k.key == key {
			return len(in.draws)
		}
	}
	return -1
}

// The main window draws two patched Cards, then sits 10 s without a frame
// (unfocused: no caret blinking, nothing animating). Its first frames back
// draw the roots it had: none unmounted or rendered again, and their trees
// still there.
func TestIdleWindowKeepsPatchedRoots(t *testing.T) {
	b, at := sweepEnv(t)
	for i := 0; i < 10; i++ {
		mainFrame(at, "c1", "c2")
		at = at.Add(20 * time.Millisecond)
	}
	r1, r2 := b.root["c1"], b.root["c2"]
	if r1 == 0 || r2 == 0 {
		t.Fatalf("no render reached the bridge: %v", b.root)
	}
	for _, r := range []uint64{r1, r2} {
		if err := Deliver(r, []byte(cardTree)); err != nil {
			t.Fatal(err)
		}
	}
	mainFrame(at, "c1", "c2")
	renders := len(b.renders)
	at = at.Add(10 * time.Second) // no frame
	for i := 0; i < 5; i++ {
		mainFrame(at, "c1", "c2")
		at = at.Add(20 * time.Millisecond)
	}
	if len(b.unmounted) != 0 {
		t.Errorf("after 10 s without a frame, the next frames unmounted roots %v (c1 %d, c2 %d); the window still draws them", b.unmounted, r1, r2)
	}
	if b.root["c1"] != r1 || b.root["c2"] != r2 || len(b.renders) != renders {
		t.Errorf("after 10 s without a frame the Cards rendered again (%d renders, roots %v; were c1 %d, c2 %d): mounted afresh", len(b.renders)-renders, b.root, r1, r2)
	}
	for _, k := range []string{"c1", "c2"} {
		if s := KeyedTrees(k); s != "Card: 2 nodes" {
			t.Errorf("%s after the idle window's next frames: %s; want its plugin tree (2 nodes)", k, s)
		}
	}
}

// A root its window stopped drawing (scrolled away, closed) still goes
// once the window has drawn frames without it for 5 s; one scrolled back
// within seconds keeps its root.
func TestUndrawnPatchedRootsUnmount(t *testing.T) {
	b, at := sweepEnv(t)
	frames := func(n int, keys ...string) {
		for i := 0; i < n; i++ {
			mainFrame(at, keys...)
			at = at.Add(20 * time.Millisecond)
		}
	}
	frames(5, "kept", "away")
	kept, away := b.root["kept"], b.root["away"]
	frames(150, "kept") // 3 s scrolled away
	frames(5, "kept", "away")
	if b.root["away"] != away || b.gone(away) {
		t.Fatalf("a Card scrolled away for 3 s came back in root %d (was %d, unmounted %v); want the same root", b.root["away"], away, b.unmounted)
	}
	frames(400, "kept") // 8 s
	if !b.gone(away) {
		t.Errorf("a Card the window hasn't drawn for 8 s of frames is still mounted (unmounted %v, want %d)", b.unmounted, away)
	}
	if b.gone(kept) {
		t.Errorf("the Card drawn every frame was unmounted (%v)", b.unmounted)
	}
}

// During a huddle the call window draws frames while the main window sits
// still: the main window's roots stay, and its next frame finds them as
// it left them. Each window's frames sweep its own undrawn roots, never
// the other window's.
func TestOtherWindowsFramesKeepPatchedRoots(t *testing.T) {
	b, at := sweepEnv(t)
	var huddle SubWindow
	for i := 0; i < 5; i++ {
		mainFrame(at, "main")
		subWinFrame(&huddle, at, "call", "caption")
		at = at.Add(20 * time.Millisecond)
	}
	main, call, caption := b.root["main"], b.root["call"], b.root["caption"]
	if main == 0 || call == 0 || caption == 0 {
		t.Fatalf("no render reached the bridge: %v", b.root)
	}
	// 10 s of call window frames, the caption gone; the main window draws none
	for i := 0; i < 100; i++ {
		subWinFrame(&huddle, at, "call")
		at = at.Add(100 * time.Millisecond)
	}
	if b.gone(main) {
		t.Errorf("the call window's frames unmounted the still main window's root (unmounted %v)", b.unmounted)
	}
	if b.gone(call) {
		t.Errorf("the call window unmounted the root it draws every frame (unmounted %v)", b.unmounted)
	}
	if !b.gone(caption) {
		t.Errorf("the call window's caption, undrawn for 10 s of its frames, is still mounted (unmounted %v)", b.unmounted)
	}
	renders := len(b.renders)
	mainFrame(at, "main")
	if b.root["main"] != main || len(b.renders) != renders {
		t.Errorf("the main window's next frame mounted its Card afresh (root %d, was %d)", b.root["main"], main)
	}
	// and the other way round: 10 s of main window frames, the call window still
	for i := 0; i < 100; i++ {
		mainFrame(at, "main")
		at = at.Add(100 * time.Millisecond)
	}
	if b.gone(call) || b.gone(main) {
		t.Errorf("the main window's frames unmounted a root (unmounted %v; call %d, main %d)", b.unmounted, call, main)
	}
	subWinFrame(&huddle, at, "call")
	if b.root["call"] != call {
		t.Errorf("the call window's next frame mounted its Card afresh (root %d, was %d)", b.root["call"], call)
	}
}

// A closed window's roots go with it, at once: not at another window's
// next sweep, which a hidden or minimized main window never runs (it
// draws no frames), while the closed window's plugin roots ran on in JS.
// The roots of windows still open stay.
func TestClosedSubWindowRootsUnmount(t *testing.T) {
	b, at := sweepEnv(t)
	var huddle, panel SubWindow
	mainFrame(at, "main")
	subWinFrame(&huddle, at, "call")
	subWinFrame(&panel, at, "presenter")
	call, presenter, main := b.root["call"], b.root["presenter"], b.root["main"]
	huddle.Close()
	huddle.Close() // once is enough
	closing.Wait() // and no window draws a frame meanwhile
	if !b.gone(call) {
		t.Errorf("the closed call window's root is still mounted with no frame since (unmounted %v, want %d)", b.unmounted, call)
	}
	if b.gone(presenter) || b.gone(main) {
		t.Errorf("closing the call window unmounted another window's root (unmounted %v; presenter %d, main %d)", b.unmounted, presenter, main)
	}
	if n := len(b.unmounted); n != 1 {
		t.Errorf("closing the call window unmounted %d roots (%v), want 1", n, b.unmounted)
	}
}

// HuddleMiniPanel is one root that two windows draw: the main window's
// dock and the presenter panel (same key). While the main window draws
// rarely (unfocused) the panel draws it every 500 ms, then closes (Stop
// sharing). The main window's next frame sweeps first, and the root is
// still the dock's: it isn't unmounted and mounted again. Once neither
// window draws it, it goes.
func TestRootSharedByTwoWindowsOutlivesOneClosing(t *testing.T) {
	b, at := sweepEnv(t)
	var panel SubWindow
	for i := 0; i < 2; i++ {
		mainFrame(at, "mini")
		subWinFrame(&panel, at, "mini")
		at = at.Add(20 * time.Millisecond)
	}
	mini := b.root["mini"]
	if mini == 0 {
		t.Fatalf("no render reached the bridge: %v", b.root)
	}
	for i := 0; i < 20; i++ {
		if i == 5 {
			mainFrame(at, "mini") // the main window's one frame meanwhile
		}
		subWinFrame(&panel, at, "mini")
		at = at.Add(500 * time.Millisecond)
	}
	panel.Close()
	closing.Wait()
	if b.gone(mini) {
		t.Fatalf("the presenter panel closed and the main window's dock root went with it (unmounted %v)", b.unmounted)
	}
	renders := len(b.renders)
	for i := 0; i < 3; i++ {
		mainFrame(at, "mini")
		at = at.Add(20 * time.Millisecond)
	}
	if b.gone(mini) || b.root["mini"] != mini || len(b.renders) != renders {
		t.Errorf("the presenter panel closed and the main window's dock root was unmounted and mounted again (unmounted %v, root %d, was %d)", b.unmounted, b.root["mini"], mini)
	}
	// screen share after screen share: each panel's draw goes when it closes
	for share := 0; share < 3; share++ {
		var p SubWindow
		subWinFrame(&p, at, "mini")
		mainFrame(at, "mini")
		at = at.Add(20 * time.Millisecond)
		if n := draws("mini"); n != 2 {
			t.Fatalf("share %d: the root keeps %d windows' draws while the dock and the panel draw it, want 2", share, n)
		}
		p.Close()
		closing.Wait()
		if n := draws("mini"); n != 1 {
			t.Errorf("share %d: the root keeps %d windows' draws after the panel closed, want 1 (the dock's): closed windows pile up", share, n)
		}
	}
	for i := 0; i < 80; i++ {
		mainFrame(at)
		at = at.Add(100 * time.Millisecond)
	}
	if !b.gone(mini) {
		t.Errorf("neither window draws the root and it's still mounted (unmounted %v, want %d)", b.unmounted, mini)
	}
}
