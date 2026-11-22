package composer

import (
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/gesture"
	"gioui.org/io/event"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// The + button's double click (ShortcutsMenuTriggerButton onDoubleClick):
// two clicks within the double-click window close the menu the first
// opened and run Upload from your computer's file chooser, unless the
// conversation's Slack Connect prefs block uploads (rscm O) or an
// external huddle restricts it (uu7O f). Driven through a real router
// with the composer's overlays laid out over it, as the shell does.
// Synthetic data only.

const (
	tChPartner = "C0PARTNER1" // externally shared
	tChHuddle  = "C0HUDDLE01" // a huddle channel
)

// plusHost is the composer's Host with the overlays it opens kept and
// laid out over the window every frame (the shell's overlay stack).
type plusHost struct {
	*slickHost
	overlays []popups.Overlay
	menus    int // + menus opened
}

func (h *plusHost) ShowOverlay(o popups.Overlay) {
	if _, ok := o.(*popups.Menu); ok {
		h.menus++
	}
	h.overlays = append(h.overlays, o)
}

type plusEnv struct {
	t     *testing.T
	app   *core.App
	fs    *fakeSlack
	h     *plusHost
	c     *Composer
	r     input.Router
	ops   op.Ops
	now   time.Time
	ptr   time.Duration // the pointer events' clock (gesture.Click counts clicks by it)
	picks int           // file choosers opened
	scale float32       // px per dp
	size  image.Point   // the window, px
	// spy: a pass-through handler over the window, laid out last, that
	// logs the pointer events the window gets and the +'s hover per frame
	// (a failure's message)
	spy    int
	frameN int
	spyLog []string
	hov    string // the +'s state last logged
}

func newPlusEnv(t *testing.T) *plusEnv {
	t.Helper()
	return newPlusEnvTheme(t, false, 1)
}

// newPlusEnvTheme is newPlusEnv in a mode at a scale (the shots' 2x).
func newPlusEnvTheme(t *testing.T, dark bool, scale float32) *plusEnv {
	t.Helper()
	app, fs := syntheticApp(t, func(s *store.Store) {
		s.PutConv(slack.Conversation{ID: tChPartner, Name: "partners", IsChannel: true, IsMember: true, IsExtShared: true})
		s.PutConv(slack.Conversation{ID: tChHuddle, Name: "huddle", IsChannel: true, IsMember: true,
			Properties: json.RawMessage(`{"huddles":{"is_huddle_channel":true}}`)})
	})
	stubTyping(t)
	sh := theme.NewShaper()
	th := theme.New(dark)
	th.SetShapers(sh, sh)
	e := &plusEnv{t: t, app: app, fs: fs, h: &plusHost{slickHost: &slickHost{app: app, th: th}},
		now: time.Now(), ptr: 10 * time.Second, scale: scale, size: image.Pt(int(800*scale), int(600*scale))}
	old := uploadFromComputer
	uploadFromComputer = func(*attachments) { e.picks++ }
	t.Cleanup(func() { uploadFromComputer = old })
	return e
}

// open makes the composer for ch (and thread ts) the env draws.
func (e *plusEnv) open(ch, ts string) *Composer {
	clearDraft(ch, ts)
	e.t.Cleanup(func() { clearDraft(ch, ts) })
	return e.use(New(e.h, ch, ts))
}

func (e *plusEnv) use(c *Composer) *Composer {
	e.t.Cleanup(c.Close)
	e.c, e.h.overlays, e.h.menus = c, nil, 0
	e.frame()
	e.frame()
	return c
}

// frame lays the composer out at the bottom of an 800×600 dp window, then
// the open overlays over everything, and advances both clocks 16 ms.
func (e *plusEnv) frame() {
	e.ops.Reset()
	paint.FillShape(&e.ops, e.h.th.C.Bg, clip.Rect{Max: e.size}.Op())
	gtx := layout.Context{Ops: &e.ops, Metric: unit.Metric{PxPerDp: e.scale, PxPerSp: e.scale}, Source: e.r.Source(), Now: e.now,
		Constraints: layout.Exact(e.size)}
	slackc.Frame(gtx)
	e.c.Origin = image.Pt(0, int(400*e.scale))
	st := op.Offset(e.c.Origin).Push(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(e.size.X, int(200*e.scale))}
	e.c.Layout(g)
	st.Pop()
	live := e.h.overlays[:0]
	for _, o := range e.h.overlays {
		if o.Layout(gtx); !o.Dismissed() {
			live = append(live, o)
		}
	}
	e.h.overlays = live
	e.frameN++
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &e.spy, Kinds: pointer.Press | pointer.Release | pointer.Move | pointer.Drag | pointer.Enter | pointer.Leave | pointer.Cancel})
		if !ok {
			break
		}
		if pe, ok := ev.(pointer.Event); ok {
			e.spyLog = append(e.spyLog, fmt.Sprintf("f%d %v %v", e.frameN, pe.Kind, pe.Position))
		}
	}
	b := &e.c.ui.btn[crAttach]
	if st := fmt.Sprintf("+hovered=%v pressed=%v overlays=%d", b.Hovered(), b.Pressed(), e.overlaysOpen()); st != e.hov {
		e.hov = st
		e.spyLog = append(e.spyLog, fmt.Sprintf("f%d %s", e.frameN, st))
	}
	pass := pointer.PassOp{}.Push(gtx.Ops)
	sa := clip.Rect{Max: e.size}.Push(gtx.Ops)
	event.Op(gtx.Ops, &e.spy)
	sa.Pop()
	pass.Pop()
	e.r.Frame(&e.ops)
	e.now = e.now.Add(16 * time.Millisecond)
	e.ptr += 16 * time.Millisecond
}

// frames lays out frames for about d.
func (e *plusEnv) frames(d time.Duration) {
	for range int(d/(16*time.Millisecond)) + 1 {
		e.frame()
	}
}

// plus is the + button's centre in the window.
func (e *plusEnv) plus() image.Point {
	r := e.c.ui.rect[crAttach]
	if r.Empty() {
		e.t.Fatal("no + laid out")
	}
	r = r.Add(e.c.Origin).Add(image.Pt(0, e.c.top))
	return image.Pt((r.Min.X+r.Max.X)/2, (r.Min.Y+r.Max.Y)/2)
}

// queueClick queues a press and its release 40 ms later at p, on the
// pointer clock.
func (e *plusEnv) queueClick(p image.Point) {
	pos := f32.Pt(float32(p.X), float32(p.Y))
	e.r.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pos, Time: e.ptr},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pos, Time: e.ptr},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pos, Time: e.ptr + 40*time.Millisecond})
	e.ptr += 40 * time.Millisecond
}

// click clicks at p and lays out a frame.
func (e *plusEnv) click(p image.Point) {
	e.queueClick(p)
	e.frame()
}

// dblclick clicks p twice, the second press 120 ms after the first (inside
// gesture.Click's double-click window), a frame after each.
func (e *plusEnv) dblclick(p image.Point) {
	e.click(p)
	e.ptr += 80 * time.Millisecond
	e.click(p)
}

// rest leaves the pointer alone past the double-click window (the
// platform's, Gio patch 48) and the menu's scrim delay.
func (e *plusEnv) rest() { e.frames(gesture.DoubleClickTime() + 200*time.Millisecond) }

// diag is the +'s state, for a failure's message: where it is, its
// hover and press, the presses it saw (widget.Clickable's history), the
// open state and the turn.
func (e *plusEnv) diag() string {
	b := &e.c.ui.btn[crAttach]
	var hs []string
	for _, h := range b.History() {
		hs = append(hs, fmt.Sprintf("{at %v, %v ago, ended %v, cancelled %v}", h.Position, e.now.Sub(h.Start), !h.End.IsZero(), h.Cancelled))
	}
	log := e.spyLog
	if len(log) > 80 {
		log = log[len(log)-80:]
	}
	return fmt.Sprintf("+ %v (local %v), hovered %v, pressed %v, open %v, turn %+v, presses %v; the window's pointer events and the +'s hover, last frames: %v",
		e.plus(), e.c.ui.rect[crAttach], b.Hovered(), b.Pressed(), e.c.att.plusOpen, e.c.ui.turn, hs, log)
}

// menuOpen reports whether the + menu shows.
func (e *plusEnv) menuOpen() bool { return e.c.att.plus != nil && !e.c.att.plus.Dismissed() }

// overlaysOpen is how many overlays show.
func (e *plusEnv) overlaysOpen() int {
	n := 0
	for _, o := range e.h.overlays {
		if !o.Dismissed() {
			n++
		}
	}
	return n
}

// The double click in a channel composer, a thread's and an edit's (the
// edit composer's + is the same ShortcutsMenuTriggerButton, with
// isEditMessageShortcutsButton); a single click only opens the menu.
func TestPlusDoubleClickOpensFileChooser(t *testing.T) {
	e := newPlusEnv(t)
	for _, tc := range []struct {
		name string
		make func() *Composer
	}{
		{"channel", func() *Composer { return e.open(tChGen, "") }},
		{"thread", func() *Composer { return e.open(tChGen, "1700000000.000100") }},
		{"edit", func() *Composer {
			return e.use(NewEdit(e.h, core.Message{Channel: tChGen, TS: "1700000000.000200", Text: "edit me", User: tSelf}, func() {}))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e.t = t
			tc.make()
			e.picks = 0
			p := e.plus()

			// one click: the menu, no chooser; it stays past the
			// double-click window
			e.click(p)
			if !e.menuOpen() || e.picks != 0 {
				t.Fatalf("one click: menu %v, %d choosers; want the menu only", e.menuOpen(), e.picks)
			}
			e.rest()
			if !e.menuOpen() || e.picks != 0 || e.h.menus != 1 {
				t.Fatalf("after one click: menu %v, %d choosers, %d menus", e.menuOpen(), e.picks, e.h.menus)
			}
			// with its scrim up (250 ms on), a click on the + is outside
			// the menu: it closes it and doesn't reopen it
			e.click(p)
			if e.menuOpen() || e.overlaysOpen() != 0 || e.h.menus != 1 || e.picks != 0 {
				t.Fatalf("a click on the + with the menu up: menu %v, %d overlays, %d menus, %d choosers",
					e.menuOpen(), e.overlaysOpen(), e.h.menus, e.picks)
			}
			e.rest()

			// a double click: the first click's menu shows, the second
			// closes it and opens the chooser; no second menu
			e.click(p)
			if !e.menuOpen() {
				t.Fatal("the double click's first click opened no menu")
			}
			e.ptr += 80 * time.Millisecond
			e.click(p)
			if e.picks != 1 {
				t.Fatalf("double click: %d choosers, want 1", e.picks)
			}
			if e.menuOpen() || e.overlaysOpen() != 0 || e.h.menus != 2 {
				t.Fatalf("double click: menu %v, %d overlays, %d menus opened in all; want the first click's closed and no other",
					e.menuOpen(), e.overlaysOpen(), e.h.menus)
			}
			e.rest()
			if e.menuOpen() || e.picks != 1 {
				t.Fatalf("after the double click: menu %v, %d choosers", e.menuOpen(), e.picks)
			}

			// two clicks further apart than the double-click window: the
			// second is a click on the scrim (the menu closes), no chooser
			e.click(p)
			e.frames(300 * time.Millisecond)
			e.ptr += 300 * time.Millisecond
			e.click(p)
			if e.menuOpen() || e.picks != 1 || e.overlaysOpen() != 0 {
				t.Fatalf("slow second click: menu %v, %d choosers, %d overlays", e.menuOpen(), e.picks, e.overlaysOpen())
			}
			e.rest()

			// both clicks in one frame (a slow frame): the chooser opens,
			// and the menu the first opened is closed (Gio's router may
			// hold the rest of a frame's events for the next, after the
			// menu takes the focus: a second frame)
			menus := e.h.menus
			e.queueClick(p)
			e.ptr += 80 * time.Millisecond
			e.queueClick(p)
			e.frame()
			first := fmt.Sprintf("after one frame: %d choosers, menu %v, %s", e.picks, e.menuOpen(), e.diag())
			e.frame()
			if e.picks != 2 || e.menuOpen() || e.overlaysOpen() != 0 || e.h.menus != menus+1 {
				t.Fatalf("both clicks in a frame: %d choosers, menu %v, %d overlays, %d menus; %s; now %s",
					e.picks, e.menuOpen(), e.overlaysOpen(), e.h.menus-menus, first, e.diag())
			}
			t.Logf("both clicks in a frame, %s", first)
			e.rest()

			// a triple click: the third click toggles the menu open again,
			// and only the second is a double click (Chromium's dblclick
			// follows the click whose detail is 2)
			e.click(p)
			e.ptr += 80 * time.Millisecond
			e.click(p)
			e.ptr += 80 * time.Millisecond
			e.click(p)
			if e.picks != 3 || !e.menuOpen() {
				t.Fatalf("triple click: %d choosers, menu %v; want one more chooser and the menu open", e.picks, e.menuOpen())
			}
			e.c.att.closePlusMenu()
			e.rest()
		})
	}
}

// N (rscm O): a Slack Connect conversation whose list prefs turn
// can_upload_files off has no double click: no tip under the tooltip, the
// menu's scrim is up at once (no overlay trick), so the second click
// closes the menu, and Upload from your computer is disabled.
func TestPlusDoubleClickSlackConnectBlocked(t *testing.T) {
	e := newPlusEnv(t)
	e.fs.mu.Lock()
	e.fs.reply = map[string]any{"conversations.listPrefs": map[string]any{"ok": true, "prefs": map[string]any{"can_upload_files": false}}}
	e.fs.mu.Unlock()
	if got := plusTip(false); got != "Attach\nTip: double click to upload" {
		t.Errorf("tip %q", got)
	}
	if got := plusTip(true); got != "Attach" {
		t.Errorf("blocked tip %q", got)
	}

	e.open(tChPartner, "")
	dl := time.Now().Add(10 * time.Second)
	for !e.c.cs.UploadBlocked {
		if time.Now().After(dl) {
			t.Fatal("the composer never read can_upload_files off")
		}
		time.Sleep(10 * time.Millisecond)
		e.frame()
	}
	if n := len(e.fs.called("conversations.listPrefs")); n != 1 {
		t.Errorf("%d conversations.listPrefs, want 1 (the + read it when the composer drew)", n)
	}
	if f := e.fs.called("conversations.listPrefs"); len(f) > 0 && (f[0].Get("channel") != tChPartner || f[0].Get("reason") != "client_redux_store") {
		t.Errorf("conversations.listPrefs %v", f[0])
	}
	p := e.plus()
	e.dblclick(p)
	if e.picks != 0 {
		t.Fatalf("%d choosers in a Slack Connect conversation without uploads", e.picks)
	}
	if e.menuOpen() || e.h.menus != 1 {
		t.Errorf("menu %v, %d menus: want the first click's, closed by the second (on the scrim)", e.menuOpen(), e.h.menus)
	}
	items := e.c.att.plusMenuItems(e.c.plusMenu())
	if up := items[len(items)-1]; up.Icon != "laptop" || !up.Disabled {
		t.Errorf("Upload from your computer %+v: want it disabled", up)
	}

	// allowed: the tip and the double click are back
	e.fs.mu.Lock()
	e.fs.reply = nil
	e.fs.mu.Unlock()
	e.app.S.PutConv(slack.Conversation{ID: tChPartner, Name: "partners", IsChannel: true, IsMember: true})
	e.rest()
	if e.c.cs.UploadBlocked {
		t.Fatal("still blocked once the conversation isn't shared")
	}
	before := e.diag()
	p2 := e.plus()
	e.dblclick(p2)
	e.frame()
	if e.picks != 1 {
		t.Errorf("%d choosers once uploads are allowed, want 1 (the + was at %v before the conversation changed); before %s; after %s", e.picks, p, before, e.diag())
	}
}

// M (uu7O f): under external_huddles, in a huddle channel or as an
// external guest the double click does nothing (the clicks still open
// and close the menu), and the menu has no Upload from your computer
// and no GIF.
func TestPlusDoubleClickRestrictedExternalHuddle(t *testing.T) {
	e := newPlusEnv(t)
	e.app.SetExperimentGroup(core.ExpExternalHuddles, "on")
	t.Cleanup(func() { e.app.ClearExperimentGroup(core.ExpExternalHuddles) })
	check := func(name string) {
		t.Helper()
		e.picks, e.h.menus = 0, 0
		e.dblclick(e.plus())
		if e.picks != 0 {
			t.Errorf("%s: %d choosers", name, e.picks)
		}
		if e.menuOpen() || e.h.menus != 1 {
			t.Errorf("%s: menu %v, %d menus; want the first click's, closed by the second", name, e.menuOpen(), e.h.menus)
		}
		pm := e.c.plusMenu()
		pm.gif = func(image.Rectangle) {}
		for _, it := range e.c.att.plusMenuItems(pm) {
			if it.Icon == "laptop" || it.Icon == "gif" {
				t.Errorf("%s: the menu has %q", name, it.Label)
			}
		}
		e.rest()
	}

	e.open(tChHuddle, "")
	check("huddle channel")
	e.open(tChGen, "")
	e.dblclick(e.plus())
	if e.picks != 1 {
		t.Fatalf("a regular channel under external_huddles: %d choosers, want 1", e.picks)
	}
	e.rest()
	e.app.S.PutUsers([]slack.User{{ID: tSelf, Name: "me", RealName: "Test Self", IsRestricted: true, RestrictedUserType: "external_guest"}})
	check("external guest")

	e.app.SetExperimentGroup(core.ExpExternalHuddles, "off")
	e.picks = 0
	e.dblclick(e.plus())
	if e.picks != 1 {
		t.Errorf("external_huddles off: %d choosers, want 1", e.picks)
	}
}

// queueAt queues a press at time at and its release held for hold, at p.
func (e *plusEnv) queueAt(p image.Point, at, hold time.Duration) {
	pos := f32.Pt(float32(p.X), float32(p.Y))
	e.r.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pos, Time: at},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pos, Time: at},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pos, Time: at + hold})
}

// pressAt and releaseAt queue the press and the release at p on the
// pointer clock, which frame advances with the frame clock: a press held
// across frames, as hands make one.
func (e *plusEnv) pressAt(p image.Point) {
	pos := f32.Pt(float32(p.X), float32(p.Y))
	e.r.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pos, Time: e.ptr},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pos, Time: e.ptr})
}

func (e *plusEnv) releaseAt(p image.Point) {
	e.r.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: f32.Pt(float32(p.X), float32(p.Y)), Time: e.ptr})
}

// A double click as people make one: each press held 100 ms, the second
// 300 ms after the first, 16 ms frames running all along, also while each
// button is down. That was two clicks for gesture.Click's old fixed
// 200 ms; by the platform's rule (Gio patch 48: Chromium's 500 ms here,
// Windows' GetDoubleClickTime) it's a double click. The second press
// lands 200 ms after the menu opened, while its scrim is still down
// (plusScrimDelay), and comes up 300 ms after: the scrim stays down while
// a press it let through is held, so the release reaches the + and isn't
// a cancel under a scrim that came up meanwhile.
func TestPlusDoubleClickAt300ms(t *testing.T) {
	e := newPlusEnv(t)
	e.open(tChGen, "")
	p := e.plus()
	t0 := e.ptr
	hold := func() { // the button down for 100 ms of frames
		e.frame()
		e.frames(100*time.Millisecond - 32*time.Millisecond)
	}
	e.pressAt(p)
	hold()
	e.releaseAt(p)
	e.frame() // the release: the menu opens
	if !e.menuOpen() {
		t.Fatalf("the first click opened no menu: %s", e.diag())
	}
	for e.ptr < t0+300*time.Millisecond {
		e.frame()
	}
	down2 := e.ptr - t0
	e.pressAt(p)
	hold()
	up2 := e.ptr - t0
	e.releaseAt(p)
	e.frame()
	e.frame()
	if e.picks != 1 || e.menuOpen() || e.h.menus != 1 {
		t.Fatalf("a double click, each press held 100 ms (the second down %v and up %v after the first press): %d choosers, menu %v, %d menus; want the chooser and the menu closed (double-click time %v); %s",
			down2, up2, e.picks, e.menuOpen(), e.h.menus, gesture.DoubleClickTime(), e.diag())
	}
}

// The + pressed (its menu opens), the menu's scrim pressed on the same
// spot once it's up (the menu closes), then the + again inside the
// double-click time of the first press: Chromium counts a window's clicks
// whatever element each lands on (Blink takes the platform event's
// click_count and resets it for no element), so that's a triple click.
// The + only opens its menu again; no file chooser (Gio patch 48, the
// router's window count).
func TestPlusAfterScrimClickIsNoDoubleClick(t *testing.T) {
	e := newPlusEnv(t)
	e.open(tChGen, "")
	p := e.plus()
	t0 := e.ptr
	e.click(p)
	if !e.menuOpen() {
		t.Fatalf("the first click opened no menu: %s", e.diag())
	}
	for e.ptr < t0+330*time.Millisecond { // past plusScrimDelay: the scrim is up
		e.frame()
	}
	e.click(p)
	if e.menuOpen() {
		t.Fatalf("a click on the scrim left the menu open: %s", e.diag())
	}
	e.frame()
	third := e.ptr - t0
	e.click(p)
	e.frame()
	if e.picks != 0 || !e.menuOpen() || e.h.menus != 2 {
		t.Fatalf("+, scrim, + (the third press %v after the first, double-click time %v): %d choosers, menu %v, %d menus; want no chooser and the menu open again (a triple click); %s",
			third, gesture.DoubleClickTime(), e.picks, e.menuOpen(), e.h.menus, e.diag())
	}
}

// A double click 400 ms apart selects the word in the composer's editor
// (Gio patch 48), where gesture.Click's old 200 ms made it two clicks.
func TestEditorDoubleClickAt400ms(t *testing.T) {
	e := newPlusEnv(t)
	c := e.open(tChGen, "")
	d := NewDoc()
	d.Insert(0, "hello brave world", Attr{})
	c.ed.SetDoc(d)
	e.frame()
	e.frame()
	a, b := c.ed.PosRect(7), c.ed.PosRect(8) // inside "brave"
	org := c.Origin.Add(c.ui.InputOffset()).Add(image.Pt(0, c.top))
	p := image.Pt((a.Min.X+b.Min.X)/2, (a.Min.Y+a.Max.Y)/2).Add(org)
	t0 := e.ptr
	e.queueAt(p, t0, 40*time.Millisecond)
	e.frame()
	e.queueAt(p, t0+400*time.Millisecond, 40*time.Millisecond)
	e.frame()
	e.frame()
	e.ptr = t0 + time.Second
	if got := c.ed.SelectedText(); got != "brave" {
		lo, hi := c.ed.Selection()
		t.Fatalf("a double click 400 ms apart selected %q (%d..%d), want \"brave\" (double-click time %v)", got, lo, hi, gesture.DoubleClickTime())
	}
	// 600 ms apart: two clicks, a caret
	e.rest()
	t0 = e.ptr
	e.queueAt(p, t0, 40*time.Millisecond)
	e.frame()
	e.queueAt(p, t0+600*time.Millisecond, 40*time.Millisecond)
	e.frame()
	e.frame()
	e.ptr = t0 + time.Second
	if got := c.ed.SelectedText(); got != "" {
		t.Errorf("two clicks 600 ms apart selected %q, want a caret", got)
	}
}

// The + is drawn open while its menu shows (ShortcutsMenuTriggerButton's
// S, --composer_ia-open): turned 135° over .32s
// cubic-bezier(.34,1.56,.64,1), overshooting on the way, and back when the
// menu closes; a double click drops S (x(false)), so the turn reverses
// part-way, shortened as CSS Transitions shortens a reversed one. A click
// on the + that only toggles the menu shut leaves S set, as Slack's
// onTrigger never calls onPopoverClose.
func TestPlusOpenState(t *testing.T) {
	e := newPlusEnv(t)
	e.open(tChGen, "")
	p := e.plus()
	if e.c.cs.PlusOpen || e.c.ui.turn.to != 0 {
		t.Fatal("drawn open before any click")
	}

	// one click: open, the turn runs and overshoots
	e.click(p)
	if !e.c.att.plusOpen {
		t.Fatal("the menu opened, S not set")
	}
	e.frame() // the chrome draws the new state from this frame
	if !e.c.cs.PlusOpen {
		t.Fatal("not drawn open")
	}
	start := e.c.ui.turn.start
	peak := 0.0
	for range 30 {
		e.frame()
		if v, _ := e.c.ui.turn.value(e.now); v > peak {
			peak = v
		}
	}
	if v, running := e.c.ui.turn.value(e.now); running || v != crTurnOpen {
		t.Fatalf("after %v: %v° (turning %v), want settled at 135°", e.now.Sub(start), v, running)
	}
	if peak <= crTurnOpen {
		t.Errorf("the turn peaked at %v°: cubic-bezier(.34,1.56,.64,1) overshoots 135°", peak)
	}
	// closing through the scrim (onPopoverClose): back to 0
	e.click(image.Pt(790, 10))
	if e.menuOpen() || e.c.att.plusOpen {
		t.Fatalf("the click outside: menu %v, S %v", e.menuOpen(), e.c.att.plusOpen)
	}
	e.frames(400 * time.Millisecond)
	if v, running := e.c.ui.turn.value(e.now); running || v != 0 {
		t.Fatalf("closed: %v° (turning %v), want 0°", v, running)
	}
	e.rest()

	// a double click: the turn starts, then reverses shortened
	e.click(p)
	e.frame()
	opened := e.c.ui.turn.start
	e.ptr += 80 * time.Millisecond
	e.click(p)
	if e.picks != 1 || e.c.att.plusOpen {
		t.Fatalf("double click: %d choosers, S %v", e.picks, e.c.att.plusOpen)
	}
	e.frame()
	tr := e.c.ui.turn
	ran := tr.start.Sub(opened)
	want := time.Duration(float64(crTurnDur) * min(max(crTurnEase(float64(ran)/float64(crTurnDur)), 0), 1))
	if tr.to != 0 || tr.dur != want || tr.revStart != crTurnOpen {
		t.Errorf("the reversed turn: to %v° over %v (revStart %v), want 0° over %v (the shortening factor after %v)", tr.to, tr.dur, tr.revStart, want, ran)
	}
	e.rest()
	if v, _ := e.c.ui.turn.value(e.now); v != 0 {
		t.Errorf("after the double click: %v°, want 0°", v)
	}

	// a click on the + that only toggles the menu shut keeps S
	e.click(p)
	if !e.menuOpen() {
		t.Fatal("no menu")
	}
	e.c.plusClick(chromeAction{Kind: caAttach, Anchor: e.c.ui.rect[crAttach], Clicks: 1})
	if e.menuOpen() || !e.c.att.plusOpen {
		t.Errorf("toggled shut: menu %v, S %v; want closed with S still set", e.menuOpen(), e.c.att.plusOpen)
	}
	// the next open and close (onPopoverClose) clear it
	e.rest()
	e.click(p)
	e.rest()
	e.click(image.Pt(790, 10))
	if e.menuOpen() || e.c.att.plusOpen {
		t.Errorf("closed again: menu %v, S %v", e.menuOpen(), e.c.att.plusOpen)
	}
}

// TestPlusOpenShots draws the + with its menu open, light and dark at 2x,
// into COMPOSER_AUDIT_SHOTS_DIR (composer-audit-shots; skipped without
// it): composer-plus-turn-* 128 ms into the turn, composer-plus-open-*
// settled at 135°, an ×, on sk_foreground_mid at .3. The pointer leaves
// the + after the click, as the menu's overlay takes it in Slack.
func TestPlusOpenShots(t *testing.T) {
	dir := os.Getenv("COMPOSER_AUDIT_SHOTS_DIR")
	if dir == "" {
		t.Skip("COMPOSER_AUDIT_SHOTS_DIR not set")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	save := func(t *testing.T, e *plusEnv, name string) {
		img := pixeltest.Render(t, e.size.X, e.size.Y, &e.ops)
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		deg, _ := e.c.ui.turn.value(e.now.Add(-16 * time.Millisecond)) // the last frame's
		t.Logf("wrote %s.png (the + at %.1f°)", name, deg)
	}
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		t.Run(mode, func(t *testing.T) {
			e := newPlusEnvTheme(t, dark, 2)
			e.open(tChGen, "")
			e.click(e.plus())
			away := f32.Pt(float32(e.size.X)-20, float32(e.size.Y)/4)
			e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: away, Time: e.ptr})
			e.frame() // the + drawn open from here
			e.frames(120 * time.Millisecond)
			save(t, e, "composer-plus-turn-"+mode)
			e.frames(500 * time.Millisecond)
			if v, running := e.c.ui.turn.value(e.now); running || v != crTurnOpen || !e.menuOpen() {
				t.Fatalf("the + at %v° (turning %v), menu %v; want 135° with the menu open", v, running, e.menuOpen())
			}
			save(t, e, "composer-plus-open-"+mode)
		})
	}
}
