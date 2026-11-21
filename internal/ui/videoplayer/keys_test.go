package videoplayer

// Round 4, step 3: the controls' keyboard and focus (keys.go), the
// corner's Dock to corner (PipButton) and the controls' tooltips (Kit's
// Tooltip on ToggleMute, FullscreenButton, PipButton and
// VideoActionsButton). Before, nothing on the player took the focus or a
// key, and the corner had Expand and More actions only.

import (
	"context"
	"encoding/json"
	"image"
	"io"
	"slices"
	"sync"
	"testing"
	"time"

	gkey "gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/media"
	"github.com/some-du6e/clack/internal/media/mediatest"
	"github.com/some-du6e/clack/internal/richtext"
)

// cornerHost is a row host with the corner's every action.
type cornerHost struct {
	menuHost
	mu       sync.Mutex
	expanded int
}

func (h *cornerHost) ExpandVideo(f *richtext.File, at time.Duration, autoplay bool) bool {
	h.mu.Lock()
	h.expanded++
	h.mu.Unlock()
	return true
}

func (g *tileRig) tileOf() *tile {
	g.c.mu.Lock()
	defer g.c.mu.Unlock()
	return g.c.tiles[g.r.key(g.f)]
}

// focusOn gives control i the focus, as Tab reaching it does.
func (g *tileRig) focusOn(i int) {
	t := g.tileOf()
	g.focus = &t.ftags[i]
	g.frame()
	g.frame()
}

func (g *tileRig) press(k gkey.Name, mods gkey.Modifiers) {
	g.router.Queue(gkey.Event{Name: k, Modifiers: mods, State: gkey.Press}, gkey.Event{Name: k, Modifiers: mods, State: gkey.Release})
	g.frame()
	g.frame()
}

func near(a, b time.Duration) bool { return a-b < 60*time.Millisecond && b-a < 60*time.Millisecond }

func TestControlsKeys(t *testing.T) {
	c, p := fakeEnv(t)
	h := &cornerHost{}
	g := &tileRig{c: c, r: c.Row("channel", "C1", "1.0", h), f: videoFile("F1"), size: image.Pt(480, 270), at: time.Second}
	g.frame()
	g.frame()

	// the scrubber (X7vT): → a second, Shift+→ a tenth, Home the start
	g.focusOn(fScrub)
	if st := state(t, c, g.r, g.f); !st.Active {
		t.Error("the scrubber has the focus and the controls are hidden (fIJO's onFocus keeps them)")
	}
	g.press(gkey.NameRightArrow, 0)
	if st := state(t, c, g.r, g.f); !near(st.Elapsed, time.Second) || !st.Mounted || st.Playing {
		t.Errorf("→ on the scrubber: %+v, want the time at 1s, the player mounted, not playing", st)
	}
	waitFor(t, "the player at 1s", 5*time.Second, func() bool { g.frame(); return near(state(t, c, g.r, g.f).Elapsed, time.Second) })
	g.press(gkey.NameRightArrow, gkey.ModShift)
	waitFor(t, "Shift+→: 1.1s", 5*time.Second, func() bool { g.frame(); return near(state(t, c, g.r, g.f).Elapsed, 1100*time.Millisecond) })
	g.press(gkey.NameHome, 0)
	waitFor(t, "Home: the start", 5*time.Second, func() bool { g.frame(); return state(t, c, g.r, g.f).Elapsed < 60*time.Millisecond })

	// the volume: the mute's focus opens the slider; ↓ a hundredth,
	// PageDown a tenth, Home mutes
	g.focusOn(fMute)
	if st := state(t, c, g.r, g.f); !st.VolumeOpen {
		t.Error("the mute has the focus and the slider is closed (--keyboard_focus:focus-within opens it)")
	}
	g.press(gkey.NameSpace, 0)
	if muted, _ := c.Volume(); !muted {
		t.Error("Space on the mute: not muted")
	}
	g.press(gkey.NameReturn, 0)
	if muted, _ := c.Volume(); muted {
		t.Error("Enter on the mute: still muted")
	}
	g.focusOn(fSlider)
	g.press(gkey.NameDownArrow, 0)
	if _, v := c.Volume(); v != 0.99 {
		t.Errorf("↓ on the volume: %v, want 0.99", v)
	}
	g.press(gkey.NamePageDown, 0)
	if _, v := c.Volume(); v != 0.89 {
		t.Errorf("PageDown: %v, want 0.89", v)
	}
	g.press(gkey.NameHome, 0)
	if muted, _ := c.Volume(); !muted {
		t.Error("Home on the volume: 0 mutes")
	}
	if w := p.writes(); !slices.Contains(w, "media_volume=0.99") || !slices.Contains(w, "media_volume=0.89") {
		t.Errorf("the volume keys wrote %v, want each change's media_volume", w)
	}

	// the pill: Space plays, Enter pauses
	g.focusOn(fPill)
	g.press(gkey.NameSpace, 0)
	waitFor(t, "playing from the pill's Space", 10*time.Second, func() bool { g.frame(); return state(t, c, g.r, g.f).Playing })
	g.press(gkey.NameReturn, 0)
	waitFor(t, "paused from the pill's Enter", 5*time.Second, func() bool { g.frame(); return !state(t, c, g.r, g.f).Playing })

	// the corner: More actions and Expand by Enter
	g.focusOn(fMore)
	g.press(gkey.NameReturn, 0)
	h.menuHost.mu.Lock()
	opened := h.menuHost.opened
	h.menuHost.mu.Unlock()
	if opened != 1 {
		t.Errorf("Enter on More actions opened %d menus", opened)
	}
	g.focusOn(fExpand)
	g.press(gkey.NameSpace, 0)
	h.mu.Lock()
	ex := h.expanded
	h.mu.Unlock()
	if ex != 1 {
		t.Errorf("Space on Expand expanded %d times", ex)
	}
	// Dock to corner by Enter
	g.focusOn(fDock)
	g.press(gkey.NameReturn, 0)
	if _, _, ok := c.Pip(); !ok || c.PipTrigger() != "manual" {
		t.Errorf("Enter on Dock to corner: docked %v by %q, want manual", ok, c.PipTrigger())
	}
}

// The corner holds Expand, Dock to corner and More actions, right to
// left 2px apart, with clips_pip on and the player wider than 154px;
// Dock to corner's click docks it, playing on if it played. Without
// clips_pip there's none.
func TestCornerDockButton(t *testing.T) {
	c, _ := fakeEnv(t)
	g := &tileRig{c: c, r: c.Row("channel", "C1", "1.0", &cornerHost{}), f: videoFile("F1"), size: image.Pt(480, 270), at: time.Second}
	g.frame()
	g.ev(pointer.Move, image.Pt(240, 100), 0)
	z := g.zones()
	if z.dock.Empty() || z.expand.Max.X+2 != z.dock.Min.X || z.dock.Max.X+2 != z.more.Min.X {
		t.Fatalf("the corner: Expand %v, Dock %v, More %v, want them in a row 2px apart", z.expand, z.dock, z.more)
	}
	click(c, g.r, g.f)
	waitFor(t, "playing", 10*time.Second, func() bool { g.frame(); return state(t, c, g.r, g.f).Playing })
	d := z.dock.Min.Add(z.dock.Max).Div(2)
	g.ev(pointer.Move, d, 0)
	g.ev(pointer.Press, d, pointer.ButtonPrimary)
	g.ev(pointer.Release, d, 0)
	if _, _, ok := c.Pip(); !ok || c.PipTrigger() != "manual" {
		t.Fatalf("the Dock to corner click: docked %v by %q", ok, c.PipTrigger())
	}
	waitFor(t, "the mini player playing", 5*time.Second, func() bool { pipFrame(c, [2]any{g.r, g.f}); return pipPlaying(c) })

	off, _ := fakeEnvExp(t, func(name string) string {
		if name == "clips_pip" {
			return "off"
		}
		return "on"
	})
	o := &tileRig{c: off, r: off.Row("channel", "C1", "1.0", &cornerHost{}), f: videoFile("F1"), size: image.Pt(480, 270), at: time.Second}
	o.frame()
	o.ev(pointer.Move, image.Pt(240, 100), 0)
	if z := o.zones(); !z.dock.Empty() {
		t.Errorf("Dock to corner %v without clips_pip", z.dock)
	}
}

// The controls' tooltips: Mute or Unmute (60ms, below with the vertical
// volume), Expand, Dock to corner and More actions (above).
func TestControlTooltips(t *testing.T) {
	_, o := mediatest.Source("FVT", mediatest.GreyGreyRed, false)
	pr := &prefs{vals: map[string]json.RawMessage{}}
	type tip struct {
		label string
		below bool
		delay time.Duration
	}
	var mu sync.Mutex
	shown := map[string]tip{}
	c := New(Env{
		Manager: media.NewManager(media.NewFake(media.FakeOptions{Log: func(string, ...any) {}})),
		Open: func(ctx context.Context, _ string, off int64) (io.ReadCloser, media.Info, error) {
			return o.Open(ctx, off)
		},
		Pref: pr.get, SetPref: pr.set, Experiment: func(string) string { return "on" },
		Tooltip: func(gtx layout.Context, id any, hovered bool, r image.Rectangle, below bool, delay time.Duration, label string) {
			if hovered {
				mu.Lock()
				shown[label] = tip{label, below, delay}
				mu.Unlock()
			}
		},
	})
	t.Cleanup(c.CloseAll)
	g := &tileRig{c: c, r: c.Row("channel", "C1", "1.0", &cornerHost{}), f: videoFile("F1"), size: image.Pt(480, 270), at: time.Second}
	g.frame()
	g.ev(pointer.Move, image.Pt(240, 100), 0)
	z := g.zones()
	for _, w := range []struct {
		r     image.Rectangle
		label string
		delay time.Duration
	}{{z.mute, "Mute", 60 * time.Millisecond}, {z.expand, "Expand", 0}, {z.dock, "Dock to corner", 0}, {z.more, "More actions", 0}} {
		g.ev(pointer.Move, w.r.Min.Add(w.r.Max).Div(2), 0)
		g.frame()
		mu.Lock()
		got, ok := shown[w.label]
		mu.Unlock()
		if !ok || got.below || got.delay != w.delay {
			t.Errorf("hovering %v: tooltip %+v (%v), want %q above after %v", w.r, got, ok, w.label, w.delay)
		}
	}
}
