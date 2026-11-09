package ui

import (
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// A frame that leaves the conversation out (a plugin's tree re-rendering
// around it: Chat Tabs wraps ClientPrimaryView) must not cost the
// composer its focus, nor a click that spans it its click. Gio forgets a
// handler absent for one frame: the focus goes silently, and a pressed
// button gets a reset Cancel when it's back (keepBody, KeepFocus).

// mfEnv drives the whole window for the missed-frame checks.
type mfEnv struct {
	t     *testing.T
	a     *App
	rt    *input.Router
	frame func()
	clock time.Duration
	ch    string
	size  image.Point
}

func (e *mfEnv) frames(n int) {
	for i := 0; i < n; i++ {
		e.frame()
	}
}

func (e *mfEnv) ptr(kind pointer.Kind, p image.Point) {
	e.clock += 20 * time.Millisecond
	ev := pointer.Event{Kind: kind, Source: pointer.Mouse, Position: f32.Pt(float32(p.X), float32(p.Y)), Time: e.clock}
	if kind == pointer.Press {
		ev.Buttons = pointer.ButtonPrimary
	}
	e.rt.Queue(ev)
}

// hoverFind moves the pointer over rect's points until c reports hovered.
func (e *mfEnv) hoverFind(c *widget.Clickable, r image.Rectangle) (image.Point, bool) {
	for y := r.Min.Y + 4; y < r.Max.Y; y += 6 {
		for x := r.Max.X - 4; x > r.Min.X; x -= 6 {
			e.ptr(pointer.Move, image.Pt(x, y))
			e.frame()
			if c.Hovered() {
				return image.Pt(x, y), true
			}
		}
	}
	return image.Point{}, false
}

func (e *mfEnv) composer() composerBox {
	e.t.Helper()
	cv := e.a.convos[e.ch+"/"]
	if cv == nil || cv.comp == nil || cv.compRect.Empty() {
		e.t.Fatal("no composer laid out")
	}
	cb, ok := cv.comp.(composerBox)
	if !ok {
		e.t.Fatalf("the composer is a %T", cv.comp)
	}
	return cb
}

func (e *mfEnv) focusComposer() composerBox {
	e.t.Helper()
	cb := e.composer()
	r := e.a.convos[e.ch+"/"].compRect
	p := r.Min.Add(image.Pt(60, r.Dy()/2))
	e.ptr(pointer.Move, p)
	e.ptr(pointer.Press, p)
	e.ptr(pointer.Release, p)
	e.frames(3)
	if !e.rt.Source().Focused(cb.FocusTag()) {
		e.t.Fatal("one click on the composer didn't focus it")
	}
	return cb
}

func (e *mfEnv) typeIn(cb composerBox, s string) {
	e.t.Helper()
	st := e.rt.EditorState()
	e.rt.Queue(key.EditEvent{Range: st.Selection.Range, Text: s})
	e.frames(3)
	if got := cb.DocText(); !strings.Contains(got, s) {
		e.t.Errorf("typed %q after the missed frame, the composer holds %q", s, got)
	}
}

func (e *mfEnv) clearComposer(cb composerBox) {
	e.rt.Queue(key.Event{Name: "A", Modifiers: key.ModShortcut, State: key.Press}, key.Event{Name: "A", Modifiers: key.ModShortcut, State: key.Release},
		key.Event{Name: key.NameDeleteBackward, State: key.Press}, key.Event{Name: key.NameDeleteBackward, State: key.Release})
	e.frames(3)
}

func runMissedFrame(t *testing.T, e *mfEnv) {
	t.Run("focus survives a frame without the body", func(t *testing.T) {
		e.t = t
		e.a.noKeepBody = false
		cb := e.focusComposer()
		e.a.skipBody = 1
		e.frames(3)
		if !e.rt.Source().Focused(cb.FocusTag()) {
			t.Fatal("the composer lost focus over one frame without the body")
		}
		e.typeIn(cb, "kept")
		e.clearComposer(cb)
	})
	t.Run("the editor alone keeps its focus", func(t *testing.T) {
		e.t = t
		e.a.noKeepBody = true // only composer.KeepFocus
		defer func() { e.a.noKeepBody = false }()
		cb := e.focusComposer()
		e.a.skipBody = 2
		e.frames(4)
		if !e.rt.Source().Focused(cb.FocusTag()) {
			t.Fatal("the composer lost focus with only the editor's keep-alive")
		}
		e.typeIn(cb, "editor")
		e.clearComposer(cb)
	})
	t.Run("a click spanning the missed frame clicks", func(t *testing.T) {
		e.t = t
		e.dismiss()
		kebab := e.a.clicks.get("hdr.kebab")
		o := e.a.primaryOrigin
		hdr := image.Rect(o.X, o.Y, e.size.X, o.Y+theme.HeaderH)
		p, ok := e.hoverFind(kebab, hdr)
		if !ok {
			t.Fatal("no channel header kebab under the pointer")
		}
		n := len(e.a.overlays)
		e.ptr(pointer.Press, p)
		e.ptr(pointer.Release, p)
		e.frames(3)
		if len(e.a.overlays) <= n {
			t.Fatalf("a plain click on the kebab at %v didn't open its menu", p)
		}
		e.dismiss()
		span := func(skip int) bool {
			e.clock += time.Second // not a double click
			e.ptr(pointer.Move, p)
			e.frame()
			n := len(e.a.overlays)
			e.ptr(pointer.Press, p)
			e.frame()
			e.a.skipBody = skip
			e.frame()
			e.ptr(pointer.Release, p)
			e.frames(3)
			opened := len(e.a.overlays) > n
			e.dismiss()
			return opened
		}
		if !span(0) {
			t.Fatal("press, a frame, release (no missed frame): the kebab's menu didn't open")
		}
		if !span(1) {
			t.Error("press, a frame without the body, release: the kebab's menu didn't open")
		}
		// control: without the stand-in Gio drops the press (the bug), so
		// this test does test something
		e.a.noKeepBody = true
		lost := !span(1)
		e.a.noKeepBody = false
		if !lost {
			t.Error("control: with no stand-in body the press survived the missed frame; the test no longer reproduces the bug")
		}
	})
	t.Run("a click on the search box moves the focus there", func(t *testing.T) {
		e.t = t
		cb := e.focusComposer()
		search := e.a.clicks.get("nav.search")
		p, ok := e.hoverFind(search, image.Rect(0, 0, e.size.X, theme.TopNavH))
		if !ok {
			t.Fatal("no search box under the pointer")
		}
		n := len(e.a.overlays)
		e.ptr(pointer.Press, p)
		e.ptr(pointer.Release, p)
		e.frames(3)
		if len(e.a.overlays) <= n {
			t.Fatal("the search box didn't open")
		}
		e.a.skipBody = 1 // a missed frame now must not pull the focus back
		e.frames(10)
		if e.rt.Source().Focused(cb.FocusTag()) {
			t.Fatal("the composer took the focus back from search")
		}
		st := e.rt.EditorState()
		e.rt.Queue(key.EditEvent{Range: st.Selection.Range, Text: "query"})
		e.frames(3)
		if strings.Contains(cb.DocText(), "query") {
			t.Error("text typed into search landed in the composer")
		}
		e.dismiss()
	})
}

func (e *mfEnv) dismiss() {
	for i := 0; i < 5 && len(e.a.overlays) > 0; i++ {
		e.rt.Queue(key.Event{Name: key.NameEscape, State: key.Press}, key.Event{Name: key.NameEscape, State: key.Release})
		e.frames(2)
	}
	for e.a.dismissTop() {
	}
	e.frames(2)
}

// The whole client on the fake Slack, no plugins.
func TestMissedFrameKeepsFocusAndClicks(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	pe := startEnv(t, fs, t.TempDir())
	pe.waitReady()
	ch := fs.ChannelIDs()[1]
	pe.openChannel(ch)
	pe.until("history", 10*time.Second, func() bool { return pe.loadedMessages(ch) > 0 })
	pe.settle(300 * time.Millisecond)
	runMissedFrame(t, &mfEnv{t: t, a: pe.a, rt: &pe.r, frame: pe.frame, ch: ch, size: pe.size})
}

// The same with Slick's Chat Tabs wrapping the primary view.
func TestSlickViewMissedFrameChatTabs(t *testing.T) {
	e := newSV(t)
	e.install("ChatTabs", nil)
	e.until(8*time.Second, "the tab bar", func() bool { return e.tree(`slick-chat-tabs`, `data-tab-count`) != "" })
	e.wait(400*time.Millisecond, func() bool { return false })
	t.Logf("frames Chat Tabs' tree left the body out on its own so far: %d", e.a.standIns)
	m := &mfEnv{t: t, a: e.a, rt: e.rt, frame: e.frame, ch: ttGen, size: e.sz, clock: e.clock}
	runMissedFrame(t, m)
}
