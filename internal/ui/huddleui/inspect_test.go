package huddleui

// Karim, 2026-11-20 (Windows, α.112, in a huddle): "no dev tools" / "they
// dont appear": the inspect tool ran only in the main window. These drive
// each of huddleui's windows frame by frame as its run loop does
// (frameEvent), against the fake meeting and fakeslack (never a real
// huddle), with the tool on: the shortcut starts a session; F holds the
// window on the frame on screen (its frames draw the frozen frame, the
// animated images held) and lets it go; a dragged region is shot and
// reviewed, and Enter, once armed, copies and saves a report naming the
// window, which a toast in the window says; turned off in the main window,
// the window's tool goes.

import (
	"image"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/app"
	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/ui/inspect"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// inspHost is the ui root's inspect tool for these tests: on, its reports
// saved in dir, the windows' holds on the images counted.
type inspHost struct {
	on    atomic.Bool
	dir   string
	holds atomic.Int32
}

func (h *inspHost) InspectOn() bool          { return h.on.Load() }
func (h *inspHost) InspectKey() key.Filter   { return inspKey }
func (h *inspHost) InspectConfigDir() string { return h.dir }
func (h *inspHost) InspectVersion() string   { return "test" }
func (h *inspHost) InspectHold(on bool) {
	if on {
		h.holds.Add(1)
	} else {
		h.holds.Add(-1)
	}
}

// inspKey is the shortcut here (the ui root's is Hyper+D); inspMods its
// modifiers.
const inspMods = key.ModShortcut | key.ModAlt | key.ModShift

var inspKey = key.Filter{Name: "D", Required: inspMods}

// inspWin is one of huddleui's windows driven through a router, frame by
// frame as its run loop drives it, its inspect tool on.
type inspWin struct {
	t     *testing.T
	kind  string
	r     input.Router
	size  image.Point
	wi    *winInspect
	frame func(e app.FrameEvent)
	h     *inspHost

	mu     sync.Mutex
	shots  int
	copied image.Image
}

func newInspWin(t *testing.T, kind string, size image.Point, wi *winInspect, frame func(app.FrameEvent)) *inspWin {
	t.Helper()
	if wi.kind != kind {
		t.Fatalf("the window names itself %q, want %q", wi.kind, kind)
	}
	h := &inspHost{dir: t.TempDir()}
	h.on.Store(true)
	wi.h = h
	e := &inspWin{t: t, kind: kind, size: size, wi: wi, frame: frame, h: h}
	prevShoot, prevWrite := inspect.Shoot, inspect.WriteImage
	inspect.Shoot = func(_ op.CallOp, region image.Rectangle, k float32) (*image.RGBA, error) {
		e.mu.Lock()
		e.shots++
		e.mu.Unlock()
		return image.NewRGBA(image.Rectangle{Max: inspect.ShotSize(region, k)}), nil
	}
	inspect.WriteImage = func(img image.Image) error {
		e.mu.Lock()
		e.copied = img
		e.mu.Unlock()
		return nil
	}
	t.Cleanup(func() {
		wi.close()
		inspect.Shoot, inspect.WriteImage = prevShoot, prevWrite
	})
	return e
}

// step is one frame of the window.
func (e *inspWin) step() {
	e.frame(app.FrameEvent{Now: time.Now(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Size: e.size,
		Source: e.r.Source(), Frame: func(o *op.Ops) { e.r.Frame(o) }})
}

func (e *inspWin) key(name key.Name, mods key.Modifiers) {
	e.r.Queue(key.Event{Name: name, Modifiers: mods, State: key.Press}, key.Event{Name: name, Modifiers: mods, State: key.Release})
}

func (e *inspWin) ptr(evs ...pointer.Event) {
	for _, ev := range evs {
		ev.Source = pointer.Mouse
		e.r.Queue(ev)
	}
}

func (e *inspWin) counts() (int, image.Image) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.shots, e.copied
}

// start starts a session with the shortcut.
func (e *inspWin) start() {
	e.t.Helper()
	e.key("D", inspMods)
	e.step()
	if !e.wi.active() {
		e.t.Fatalf("%s: the shortcut started no session", e.kind)
	}
	e.step()
}

// run is the scenario every window kind goes through.
func (e *inspWin) run() {
	t := e.t
	t.Helper()
	e.step()
	e.step()
	if e.wi.tool == nil {
		t.Fatalf("%s: the tool is on, and the window has none", e.kind)
	}
	e.start()

	// F holds the window on the frame on screen: its frames draw that
	// frame again, laying nothing out (a SubWindow's patch frame is begun
	// only by a layout), the animated images held
	e.key(inspect.FreezeKey, 0)
	e.step()
	subs := slackc.SubFrames()
	e.step()
	e.step()
	if !e.wi.tool.Frozen() || e.h.holds.Load() != 1 || slackc.SubFrames() != subs {
		t.Fatalf("%s: after F: frozen %v, image holds %d, frames laid out %d; want frozen, one hold, none laid out",
			e.kind, e.wi.tool.Frozen(), e.h.holds.Load(), slackc.SubFrames()-subs)
	}
	e.key(inspect.FreezeKey, 0) // and again lets it go
	e.step()
	e.step()
	if e.wi.tool.Frozen() || e.h.holds.Load() != 0 || !e.wi.active() {
		t.Fatalf("%s: after F again: frozen %v, image holds %d, session %v; want unfrozen, none, the session on",
			e.kind, e.wi.tool.Frozen(), e.h.holds.Load(), e.wi.active())
	}

	// a region dragged out is shot and reviewed
	from := f32.Pt(float32(e.size.X)/4, float32(e.size.Y)/4)
	to := f32.Pt(float32(e.size.X)*3/4, float32(e.size.Y)*3/4)
	e.ptr(pointer.Event{Kind: pointer.Move, Position: from}, pointer.Event{Kind: pointer.Press, Buttons: pointer.ButtonPrimary, Position: from})
	e.step()
	e.ptr(pointer.Event{Kind: pointer.Move, Buttons: pointer.ButtonPrimary, Position: from.Add(f32.Pt(3, 2))})
	e.step()
	e.ptr(pointer.Event{Kind: pointer.Move, Buttons: pointer.ButtonPrimary, Position: to})
	e.step()
	e.ptr(pointer.Event{Kind: pointer.Release, Position: to})
	e.step()
	e.step()
	if shots, _ := e.counts(); shots != 1 || !e.wi.tool.Reviewing() {
		t.Fatalf("%s: a dragged region: %d shots, reviewing %v; want one, the review up", e.kind, shots, e.wi.tool.Reviewing())
	}
	// Enter, once the review has been on screen a while, copies it and
	// saves the report: the frame after the one that drew the review
	// finds it shown (it arms 300 ms on)
	e.step()
	time.Sleep(450 * time.Millisecond)
	e.step()
	e.key(key.NameReturn, 0)
	e.step()
	for dl := time.Now().Add(10 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		if _, img := e.counts(); img != nil {
			break
		}
		if time.Now().After(dl) {
			t.Fatalf("%s: Enter in the review copied nothing", e.kind)
		}
		e.step()
	}
	e.wi.tool.Wait()
	e.step() // the toast the delivery posted
	e.step()
	if e.wi.active() {
		t.Errorf("%s: the session still runs after the copy", e.kind)
	}
	rows := e.report()
	if rows["page"] != e.kind || !strings.HasPrefix(rows["region"], e.kind) || rows["capture"] != "dragged region" {
		t.Errorf("%s: the report's page %q, region %q, capture %q; want the window, the window first, a dragged region",
			e.kind, rows["page"], rows["region"], rows["capture"])
	}
	e.wi.mu.Lock()
	note := e.wi.note
	e.wi.mu.Unlock()
	if !strings.HasPrefix(note, "Copied the inspect report") {
		t.Errorf("%s: the window's toast says %q, want that the report was copied", e.kind, note)
	}

	// the tool turned off in the main window goes from this one at its
	// next frame
	e.h.on.Store(false)
	e.step()
	if e.wi.tool != nil {
		t.Errorf("%s: the tool turned off, and the window still has one", e.kind)
	}
}

// report reads the report saved last (its rows).
func (e *inspWin) report() map[string]string {
	e.t.Helper()
	d := filepath.Join(e.h.dir, "inspect-reports")
	names, err := filepath.Glob(filepath.Join(d, "*.txt"))
	if err != nil || len(names) == 0 {
		e.t.Fatalf("%s: no report saved in %s (%v)", e.kind, d, err)
	}
	sort.Strings(names)
	b, err := os.ReadFile(names[len(names)-1])
	if err != nil {
		e.t.Fatal(err)
	}
	out := map[string]string{}
	for _, l := range strings.Split(string(b), "\n") {
		if k, v, ok := strings.Cut(l, "  "); ok && k != "" && !strings.Contains(k, " ") {
			out[k] = strings.TrimLeft(v, " ")
		}
	}
	return out
}

// bareTheme is a window's own theme, as its run loop makes it.
func bareTheme(dark bool) *theme.Theme {
	th := theme.Bare(dark)
	sh := theme.NewShaperFor(dark)
	th.SetShapers(sh, sh)
	return th
}

// TestInspectHuddleWindow: the huddle window, in the fake meeting. Its ⋯
// menu is a layer of the record, as the main window's menus are: a row
// of it is picked as itself, not the window under it.
func TestInspectHuddleWindow(t *testing.T) {
	e := newShotEnv(t, false)
	s := e.join(nil, nil)
	waitState(t, s, "connected", func(st huddle.State) bool { return st.Phase == huddle.Connected && len(st.Participants) > 1 })
	w := e.w
	w.insp = w.newInspect()
	var ops op.Ops
	var patch slackc.SubWindow
	t.Cleanup(patch.Close)
	iw := newInspWin(t, "huddle window", image.Pt(620, 400), w.insp, func(ev app.FrameEvent) { w.frameEvent(ev, &ops, &patch) })
	iw.run()

	iw.h.on.Store(true)
	iw.step()
	iw.start()
	w.toggleMore(s, s.State())
	iw.step()
	iw.step()
	if w.more == nil {
		t.Fatal("the ⋯ menu didn't open")
	}
	mr := w.more.Rect()
	at := f32.Pt(float32(mr.Min.X+mr.Dx()/2), float32(mr.Min.Y+mr.Dy()/4))
	iw.ptr(pointer.Event{Kind: pointer.Move, Position: at})
	iw.step()
	iw.step()
	label, r, ok := w.insp.tool.Hovered()
	pt := image.Pt(int(at.X), int(at.Y))
	if !ok || !pt.In(r) || !r.In(mr) || r == mr || strings.HasPrefix(label, "menu (") || strings.HasPrefix(label, "huddle window") {
		t.Errorf("over the ⋯ menu's row at %v: outline %q %v (%v), the menu %v; want the row's own, inside the menu", pt, label, r, ok, mr)
	}
	found := false
	for _, rg := range w.insp.Regions() {
		found = found || rg.Name == "menu (popups.Menu)" && rg.Rect == mr
	}
	if !found {
		t.Errorf("the window's regions %v don't name the ⋯ menu", w.insp.Regions())
	}
}

// TestInspectScreensharePopout: a screen share popped out of the huddle
// window (the remote's, in the fake meeting).
func TestInspectScreensharePopout(t *testing.T) {
	e := newShotEnv(t, false)
	s := e.join(nil, nil)
	waitState(t, s, "the remote's share", func(st huddle.State) bool {
		for _, v := range st.Videos {
			if v.Content {
				return true
			}
		}
		return false
	})
	const share = "att-remote#content"
	w := &window{c: e.c, clicks: map[string]*widget.Clickable{}, popout: share, popoutUser: huddle.FakeRemoteUser, th: e.w.th}
	e.c.popout.wins = map[string]*window{share: w}
	w.insp = w.newInspect()
	var ops op.Ops
	var patch slackc.SubWindow
	t.Cleanup(patch.Close)
	newInspWin(t, "screen share popout", image.Pt(960, 600), w.insp, func(ev app.FrameEvent) { w.frameEvent(ev, &ops, &patch) }).run()
}

// TestInspectPresenterPanel: the presenter's panel while you share.
func TestInspectPresenterPanel(t *testing.T) {
	e := newShotEnv(t, false)
	s := e.join(nil, nil)
	waitState(t, s, "connected", func(st huddle.State) bool { return st.Phase == huddle.Connected })
	th := bareTheme(true)
	p := &miniPanel{c: e.c, src: huddle.ShareSource{ID: "screen:1:0", Name: "Entire Screen", Bounds: image.Rect(0, 0, 1280, 720)}}
	p.insp = p.newInspect(th)
	var ops op.Ops
	var patch slackc.SubWindow
	t.Cleanup(patch.Close)
	newInspWin(t, "presenter panel", image.Pt(panelW, panelH), p.insp, func(ev app.FrameEvent) {
		p.frameEvent(ev, &ops, &patch, th, ev.Now.Add(500*time.Millisecond))
	}).run()
}

// TestInspectInviteWindow: an invite's window (fakeslack's invite to a DM
// huddle).
func TestInspectInviteWindow(t *testing.T) {
	e := newInviteEnv(t, nil)
	e.invite(dmChannel)
	e.shown("the invite")
	e.c.mu.Lock()
	inv := e.c.inv
	e.c.mu.Unlock()
	w := &inviteWindow{c: e.c, inv: inv, th: bareTheme(false)}
	w.insp = w.newInspect()
	var ops op.Ops
	newInspWin(t, "huddle invite", image.Pt(380, 280), w.insp, func(ev app.FrameEvent) { w.frameEvent(ev, &ops) }).run()
}

// TestInspectJoinPreview: the join preview (its microphone a tone).
func TestInspectJoinPreview(t *testing.T) {
	e := newShotEnv(t, false)
	w := &previewWindow{c: e.c, channel: hcChannel, entry: EntryHeaderButton, fx: huddle.VideoEffectNone, th: bareTheme(false)}
	w.p = huddle.StartPreview(huddle.Options{Device: huddle.NewToneDevice(440)}, false)
	t.Cleanup(w.p.Close)
	w.insp = w.newInspect()
	var ops op.Ops
	newInspWin(t, "huddle preview", image.Pt(previewW, previewH), w.insp, func(ev app.FrameEvent) { w.frameEvent(ev, &ops) }).run()
}
