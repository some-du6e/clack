package composer

import (
	"encoding/json"
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
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
}

func newPlusEnv(t *testing.T) *plusEnv {
	t.Helper()
	app, fs := syntheticApp(t, func(s *store.Store) {
		s.PutConv(slack.Conversation{ID: tChPartner, Name: "partners", IsChannel: true, IsMember: true, IsExtShared: true})
		s.PutConv(slack.Conversation{ID: tChHuddle, Name: "huddle", IsChannel: true, IsMember: true,
			Properties: json.RawMessage(`{"huddles":{"is_huddle_channel":true}}`)})
	})
	stubTyping(t)
	sh := theme.NewShaper()
	th := theme.New(false)
	th.SetShapers(sh, sh)
	e := &plusEnv{t: t, app: app, fs: fs, h: &plusHost{slickHost: &slickHost{app: app, th: th}},
		now: time.Now(), ptr: 10 * time.Second}
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

// frame lays the composer out at the bottom of an 800×600 window, then
// the open overlays over everything, and advances both clocks 16 ms.
func (e *plusEnv) frame() {
	e.ops.Reset()
	gtx := layout.Context{Ops: &e.ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Source: e.r.Source(), Now: e.now,
		Constraints: layout.Exact(image.Pt(800, 600))}
	slackc.Frame(gtx)
	e.c.Origin = image.Pt(0, 400)
	st := op.Offset(e.c.Origin).Push(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(800, 200)}
	e.c.Layout(g)
	st.Pop()
	live := e.h.overlays[:0]
	for _, o := range e.h.overlays {
		if o.Layout(gtx); !o.Dismissed() {
			live = append(live, o)
		}
	}
	e.h.overlays = live
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

// rest leaves the pointer alone past the double-click window and the
// menu's scrim delay.
func (e *plusEnv) rest() { e.frames(400 * time.Millisecond) }

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

			// both clicks in one frame (a slow frame): the menu opens and
			// closes before it draws, and the chooser opens
			menus := e.h.menus
			e.queueClick(p)
			e.ptr += 80 * time.Millisecond
			e.queueClick(p)
			e.frame()
			if e.picks != 2 || e.menuOpen() || e.overlaysOpen() != 0 || e.h.menus != menus+1 {
				t.Fatalf("both clicks in a frame: %d choosers, menu %v, %d overlays, %d menus", e.picks, e.menuOpen(), e.overlaysOpen(), e.h.menus-menus)
			}
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
	e.dblclick(p)
	if e.picks != 1 {
		t.Errorf("%d choosers once uploads are allowed, want 1", e.picks)
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
