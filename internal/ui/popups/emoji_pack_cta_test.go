package popups

// The emoji picker's emoji pack call to action (Slack's EmojiPackCta,
// boot-shared +y5+; core EmojiPickerPackCTA) against internal/fakeslack:
// shown for a pack the viewer may add within 14 days, hidden otherwise,
// dismissed for good (the pref, at the server), a click opening the Add
// emoji dialog on its Emoji packs tab, the Halloween one only in October
// with slack_seasonal_delight on. Hack Club's "new" window ended
// 2026-11-20, so the fake's packs carry a recent date_create. Synthetic.

import (
	"encoding/json"
	"image"
	"image/color"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// ctaPack is a pack created at created, its emoji three made-up names.
func ctaPack(id, name string, created time.Time) fakeslack.EmojiPack {
	return fakeslack.EmojiPack{ID: id, Name: name, Author: "Slack", DateCreate: created.Unix(),
		Emoji: []string{"synthetic-a", "synthetic-b", "synthetic-c"}}
}

// ctaEnv boots core over a fake whose packs are packs (installed: their
// ids) and whose viewer may add custom emoji when canAdd; then waits for
// the pack read and the prefs it writes.
func ctaEnv(t *testing.T, packs []fakeslack.EmojiPack, installed []string, canAdd bool, prefs map[string]any) *tzHost {
	h, _ := ctaEnvFS(t, packs, installed, canAdd, prefs)
	return h
}

func ctaEnvFS(t *testing.T, packs []fakeslack.EmojiPack, installed []string, canAdd bool, prefs map[string]any) (*tzHost, *fakeslack.Server) {
	t.Helper()
	var fake *fakeslack.Server
	h := epShotEnvWith(t, false, func(fs *fakeslack.Server) {
		fake = fs
		fs.SetEmojiPacks(packs, installed...)
		if canAdd {
			fs.Permissions = map[string][]string{"": {core.PermAddCustomEmoji}}
		} else {
			fs.Permissions = map[string][]string{"": {}}
		}
		if prefs != nil {
			fs.SetPrefs(prefs)
		}
	})
	h.a.FetchEmojiPacks()
	for dl := time.Now().Add(15 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		Pump()
		ps, fetched := h.a.EmojiPacks()
		if fetched && len(ps) == len(packs) && (!canAdd || h.a.EmojiPackCanAdd()) {
			break
		}
		if time.Now().After(dl) {
			t.Fatal("the pack read never landed")
		}
	}
	// potentialNewPack's prefs write after the read, when it makes one (the
	// newest pack newer than the last available time, and the viewer may
	// add it or has it): waited for in the local prefs, which merge its
	// answer
	since := float64(ctaFakeAvailableTime)
	if v, ok := prefs[core.PrefEmojiPacksAvailableTime].(float64); ok {
		since = v
	}
	var newest fakeslack.EmojiPack
	for _, pk := range packs {
		if pk.DateCreate > newest.DateCreate {
			newest = pk
		}
	}
	if float64(newest.DateCreate) > since && (canAdd || slices.Contains(installed, newest.ID)) {
		for dl := time.Now().Add(15 * time.Second); ctaAvailableTime(h) < float64(newest.DateCreate); time.Sleep(20 * time.Millisecond) {
			Pump()
			if time.Now().After(dl) {
				t.Fatalf("potentialNewPack's write never landed (available time %v, the pack %d)", ctaAvailableTime(h), newest.DateCreate)
			}
		}
	}
	Pump()
	return h, fake
}

// ctaFakeAvailableTime is the fake's emoji_packs_most_recent_available_time
// (fakeslack.go's prefs, Hack Club's on 2026-11-22).
const ctaFakeAvailableTime = 1793987493

// ctaAvailableTime is the local emoji_packs_most_recent_available_time.
func ctaAvailableTime(h *tzHost) float64 {
	var v float64
	if raw, ok := h.a.Pref(core.PrefEmojiPacksAvailableTime); ok {
		_ = json.Unmarshal(raw, &v)
	}
	return v
}

// ctaFrame lays the picker out once at scale and returns the strings it
// drew.
func ctaFrame(p *EmojiPicker, h *tzHost, ops *op.Ops, scale float32) []string {
	win := image.Pt(int(420*scale), int(560*scale))
	ops.Reset()
	paint.FillShape(ops, colorsOf(h.th).EmojiFoot, clip.Rect{Max: win}.Op())
	gtx := layout.Context{Ops: ops, Now: time.Now(), Metric: unit.Metric{PxPerDp: scale, PxPerSp: scale}, Constraints: layout.Exact(win)}
	var mu sync.Mutex
	var drawn []string
	theme.DrawHook = func(s string) { mu.Lock(); drawn = append(drawn, s); mu.Unlock() }
	p.Layout(gtx)
	theme.DrawHook = nil
	Pump()
	return drawn
}

func ctaPicker(h *tzHost, scale float32) *EmojiPicker {
	p := NewEmojiPicker(h, nil)
	p.SetAnchorRect(image.Rect(int(20*scale), int(548*scale), int(60*scale), int(556*scale)))
	return p
}

// A pack created yesterday that the viewer may add: "{pack} emoji for
// your team" under the search, the list below it.
func TestEmojiPackCTAShown(t *testing.T) {
	h := ctaEnv(t, []fakeslack.EmojiPack{ctaPack("Ec0CTA00001", "Fall Foliage", time.Now().Add(-24*time.Hour))}, nil, true, nil)
	p := ctaPicker(h, 1)
	var ops op.Ops
	drawn := ctaFrame(p, h, &ops, 1)
	name, kind, r := p.PackCTA()
	if kind != core.PackCTAInstall || name != "Fall Foliage" {
		t.Fatalf("call to action %q %v, want Fall Foliage's install", name, kind)
	}
	if !slices.Contains(drawn, "Fall Foliage emoji for your team") {
		t.Errorf("drawn %q: no \"Fall Foliage emoji for your team\"", drawn)
	}
	// the inner box: 17.5 in, 7 under the input container, 46 tall
	b := r.Sub(p.rect.Min)
	if b.Min.Y != epListY+7 || b.Dy() != packCTAInnerH || b.Min.X < 17 || b.Min.X > 18 || epW-b.Max.X < 17 || epW-b.Max.X > 18 {
		t.Errorf("box %v in the picker, want 17.5 in at y %d, %d tall", b, epListY+7, packCTAInnerH)
	}
}

// Hidden: a viewer who may not add emoji, a window that ended (the pack
// older than the last available time, 15 days ago), while searching, in
// the huddle's embedded picker, and the Halloween pack out of season.
func TestEmojiPackCTAHidden(t *testing.T) {
	old := time.Now().Add(-20 * 24 * time.Hour)
	recent := ctaPack("Ec0CTA00001", "Fall Foliage", time.Now().Add(-24*time.Hour))
	cases := []struct {
		name    string
		packs   []fakeslack.EmojiPack
		canAdd  bool
		prefs   map[string]any
		prepare func(p *EmojiPicker, h *tzHost)
	}{
		{"not an emoji adder", []fakeslack.EmojiPack{recent}, false, nil, nil},
		{"the 14 days are over", []fakeslack.EmojiPack{ctaPack("Ec0CTA00002", "Old Pack", old)}, true,
			map[string]any{core.PrefEmojiPacksAvailableTime: float64(time.Now().Add(-15 * 24 * time.Hour).Unix())}, nil},
		{"searching", []fakeslack.EmojiPack{recent}, true, nil, func(p *EmojiPicker, h *tzHost) { p.searching, p.query = true, "tada" }},
		{"the huddle's picker", []fakeslack.EmojiPack{recent}, true, nil, func(p *EmojiPicker, h *tzHost) { p.embedded = true }},
		{"Halloween in November", []fakeslack.EmojiPack{ctaPack("Ec0CTA00003", core.HalloweenEmojiPack, time.Now().Add(-24*time.Hour))}, true, nil,
			func(p *EmojiPicker, h *tzHost) {
				h.a.SetExperimentGroup("slack_seasonal_delight", "on")
				h.a.SetEmojiPackClock(func() time.Time { return time.Date(2026, 11, 22, 12, 0, 0, 0, time.Local) })
			}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := ctaEnv(t, c.packs, nil, c.canAdd, c.prefs)
			p := ctaPicker(h, 1)
			if c.prepare != nil {
				c.prepare(p, h)
			}
			var ops op.Ops
			ctaFrame(p, h, &ops, 1)
			if name, kind, _ := p.PackCTA(); kind != core.PackCTANone {
				t.Errorf("call to action %q %v, want none", name, kind)
			}
		})
	}
}

// The dismiss button: emoji_packs_clicked_picker_cta at the server, and no
// call to action in this picker or the next.
func TestEmojiPackCTADismissed(t *testing.T) {
	h, fs := ctaEnvFS(t, []fakeslack.EmojiPack{ctaPack("Ec0CTA00001", "Fall Foliage", time.Now().Add(-24*time.Hour))}, nil, true, nil)
	p := ctaPicker(h, 1)
	var ops op.Ops
	ctaFrame(p, h, &ops, 1)
	p.DismissPackCTA()
	ctaFrame(p, h, &ops, 1)
	ctaFrame(p, h, &ops, 1)
	if _, kind, _ := p.PackCTA(); kind != core.PackCTANone {
		t.Errorf("still drawn after dismissing: %v", kind)
	}
	if p.Dismissed() {
		t.Error("dismissing the call to action closed the picker")
	}
	for dl := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if v, ok := fs.Pref(core.PrefEmojiPacksPickerCTA); ok && (v == true || v == "true") {
			break
		}
		if time.Now().After(dl) {
			v, _ := fs.Pref(core.PrefEmojiPacksPickerCTA)
			t.Fatalf("emoji_packs_clicked_picker_cta at the server: %v, want true", v)
		}
	}
	q := ctaPicker(h, 1)
	ctaFrame(q, h, &ops, 1)
	if _, kind, _ := q.PackCTA(); kind != core.PackCTANone {
		t.Errorf("the next picker shows it: %v", kind)
	}
}

// A click before the install: the pref, the picker closes, the Add emoji
// dialog opens on Emoji packs (initialTab PACKS). After it: "{pack} emoji
// added!", and a click jumps to the pack's group.
func TestEmojiPackCTAOpensPack(t *testing.T) {
	h := ctaEnv(t, []fakeslack.EmojiPack{ctaPack("Ec0CTA00001", "Fall Foliage", time.Now().Add(-24*time.Hour))}, nil, true, nil)
	p := ctaPicker(h, 1)
	h.ShowOverlay(p)
	var ops op.Ops
	ctaFrame(p, h, &ops, 1)
	p.ClickPackCTA()
	ctaFrame(p, h, &ops, 1)
	if !p.Dismissed() {
		t.Error("the picker stayed open")
	}
	h.mu.Lock()
	shown := append([]Overlay(nil), h.shown...)
	h.mu.Unlock()
	var dlg *Modal
	for _, o := range shown {
		if m, ok := o.(*Modal); ok && !m.Dismissed() {
			dlg = m
		}
	}
	if dlg == nil {
		t.Fatal("no Add emoji dialog")
	}
	// on Emoji packs: the pack's card is drawn (the Custom tab has none)
	var dops op.Ops
	gtx := layout.Context{Ops: &dops, Now: time.Now(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(1000, 900))}
	var mu sync.Mutex
	var dd []string
	theme.DrawHook = func(s string) { mu.Lock(); dd = append(dd, s); mu.Unlock() }
	dlg.Layout(gtx)
	dlg.Layout(gtx)
	theme.DrawHook = nil
	if !slices.Contains(dd, "Fall Foliage") {
		t.Errorf("the dialog drew %q: not the Emoji packs tab with Fall Foliage's card", dd)
	}
	if !h.a.PrefBool(core.PrefEmojiPacksPickerCTA, false) {
		t.Error("the click didn't record emoji_packs_clicked_picker_cta")
	}

	// installed (with its emoji in the picker): emoji added, a jump to it
	h2 := ctaEnv(t, []fakeslack.EmojiPack{ctaPack("Ec0CTA00004", "Fall Foliage", time.Now().Add(-24*time.Hour))}, []string{"Ec0CTA00004"}, true, nil)
	for dl := time.Now().Add(15 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		Pump()
		if _, full := epIndexFor(h2); full {
			break
		}
		if time.Now().After(dl) {
			t.Fatal("the custom emoji index never landed")
		}
	}
	q := ctaPicker(h2, 1)
	drawn := ctaFrame(q, h2, &ops, 1)
	name, kind, _ := q.PackCTA()
	if kind != core.PackCTAPostInstall || name != "Fall Foliage" {
		t.Fatalf("after the install: %q %v, want Fall Foliage's post-install (pack groups %v)", name, kind, q.packNames)
	}
	if !slices.Contains(drawn, "Fall Foliage emoji added!") {
		t.Errorf("drawn %q: no \"Fall Foliage emoji added!\"", drawn)
	}
	q.ClickPackCTA()
	ctaFrame(q, h2, &ops, 1)
	if q.Dismissed() {
		t.Error("the post-install click closed the picker")
	}
	first := -1
	for i, r := range q.rows {
		if int(r.list) == epGroupSearch+1 {
			first = i
			break
		}
	}
	if first < 0 || q.list.Position.First != first {
		t.Errorf("the list at row %d, want the pack's group at %d", q.list.Position.First, first)
	}
}

// The Halloween pack in October with slack_seasonal_delight on: its own
// banner, "Get the Halloween emoji pack".
func TestEmojiPackCTAHalloween(t *testing.T) {
	h := ctaEnv(t, []fakeslack.EmojiPack{ctaPack("Ec0CTA00003", core.HalloweenEmojiPack, time.Now().Add(-24*time.Hour))}, nil, true, nil)
	h.a.SetExperimentGroup("slack_seasonal_delight", "on")
	h.a.SetEmojiPackClock(func() time.Time { return time.Date(2026, 10, 20, 12, 0, 0, 0, time.Local) })
	p := ctaPicker(h, 1)
	var ops op.Ops
	drawn := ctaFrame(p, h, &ops, 1)
	if _, kind, _ := p.PackCTA(); kind != core.PackCTAInstall {
		t.Fatalf("call to action %v, want the install", kind)
	}
	if !slices.Contains(drawn, "Get the Halloween emoji pack") {
		t.Errorf("drawn %q: no \"Get the Halloween emoji pack\"", drawn)
	}
}

// TestEmojiPackCTAPixels draws the picker with the call to action at 2x
// (EMOJI_SHOTS_DIR keeps it): the inner box in foreground_min_solid on
// the white wrapper, the label's ink 40 px in, the dismiss icon at the
// right, the list starting under it.
func TestEmojiPackCTAPixels(t *testing.T) {
	const scale = 2
	h := ctaEnv(t, []fakeslack.EmojiPack{ctaPack("Ec0CTA00001", "Fall Foliage", time.Now().Add(-24*time.Hour))}, nil, true, nil)
	p := ctaPicker(h, scale)
	var ops op.Ops
	ctaFrame(p, h, &ops, scale)
	_, kind, r := p.PackCTA()
	if kind != core.PackCTAInstall {
		t.Fatalf("no call to action (%v)", kind)
	}
	ctaFrame(p, h, &ops, scale)
	img := pixeltest.Render(t, 420*scale, 560*scale, &ops)
	epSaveShot(t, os.Getenv("EMOJI_SHOTS_DIR"), "emoji-picker-pack-cta-light", img.SubImage(p.rect).(*image.RGBA))
	near := func(a, b color.RGBA, d int) bool {
		ab := func(x, y uint8) int {
			if x > y {
				return int(x - y)
			}
			return int(y - x)
		}
		return ab(a.R, b.R) <= d && ab(a.G, b.G) <= d && ab(a.B, b.B) <= d
	}
	box := color.RGBA{0xf8, 0xf8, 0xf8, 0xff}
	white := color.RGBA{0xff, 0xff, 0xff, 0xff}
	if c := img.RGBAAt(r.Min.X+6*scale, r.Min.Y+r.Dy()/2); !near(c, box, 3) {
		t.Errorf("the box's left at %v is %v, want #f8f8f8", image.Pt(r.Min.X+6*scale, r.Min.Y+r.Dy()/2), c)
	}
	if c := img.RGBAAt(r.Min.X+r.Dx()/2, r.Min.Y-3*scale); !near(c, white, 3) {
		t.Errorf("the wrapper above the box is %v, want white", c)
	}
	ink := func(rr image.Rectangle) int {
		n := 0
		for y := rr.Min.Y; y < rr.Max.Y; y++ {
			for x := rr.Min.X; x < rr.Max.X; x++ {
				if c := img.RGBAAt(x, y); c.R < 0x90 && c.G < 0x90 && c.B < 0x90 {
					n++
				}
			}
		}
		return n
	}
	text := image.Rect(r.Min.X+52*scale, r.Min.Y+12*scale, r.Min.X+250*scale, r.Max.Y-12*scale)
	if n := ink(text); n < 200 {
		t.Errorf("%d dark pixels in the label's line %v, want the bold label", n, text)
	}
	dismiss := image.Rect(r.Max.X-30*scale, r.Min.Y+12*scale, r.Max.X-8*scale, r.Max.Y-12*scale)
	if n := ink(dismiss); n < 10 {
		t.Errorf("%d dark pixels where the dismiss button is (%v)", n, dismiss)
	}
}

// The first picker of a session while the pack read is still out: the
// call to action is there from its first frame, because boot learned
// ADD_CUSTOM_EMOJI (AcSl maybeFetchClientPermissions; Slack's eyHx sets
// hasFetched before the read), its pack's name filling in when the read
// lands, and the box (with the list under it) doesn't move. Learned only
// after the read, the list jumped 67dp under a pointer about to click.
func TestEmojiPackCTAFirstFrame(t *testing.T) {
	h := epShotEnvWith(t, false, func(fs *fakeslack.Server) {
		fs.SetEmojiPacks([]fakeslack.EmojiPack{ctaPack("Ec0CTA00001", "Fall Foliage", time.Now().Add(-24*time.Hour))})
		fs.Permissions = map[string][]string{"": {core.PermAddCustomEmoji}}
		// the window open since yesterday (an earlier session's read
		// reset it)
		fs.SetPrefs(map[string]any{core.PrefEmojiPacksAvailableTime: float64(time.Now().Add(-23 * time.Hour).Unix())})
		fs.SetFault("emoji.collections.list", fakeslack.Fault{Delay: 1500 * time.Millisecond, Times: 1})
	})
	for dl := time.Now().Add(10 * time.Second); !h.a.EmojiPackCanAdd(); time.Sleep(20 * time.Millisecond) {
		Pump()
		if time.Now().After(dl) {
			t.Fatal("boot never learned ADD_CUSTOM_EMOJI: the first picker would draw without the call to action")
		}
	}
	if _, fetched := h.a.EmojiPacks(); fetched {
		t.Fatal("the packs were read before the picker opened")
	}
	p := ctaPicker(h, 1) // its mount starts the pack read, held
	var ops op.Ops
	ctaFrame(p, h, &ops, 1)
	_, kind, first := p.PackCTA()
	if kind != core.PackCTAInstall {
		t.Fatalf("the first frame: call to action %v, want the install's box", kind)
	}
	if ps, _ := h.a.EmojiPacks(); len(ps) != 0 {
		t.Fatal("the pack read landed before the first frame: nothing tested")
	}
	for dl := time.Now().Add(15 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		Pump()
		if ps, _ := h.a.EmojiPacks(); len(ps) == 1 {
			break
		}
		if time.Now().After(dl) {
			t.Fatal("the pack read never landed")
		}
	}
	drawn := ctaFrame(p, h, &ops, 1)
	name, kind, after := p.PackCTA()
	if kind != core.PackCTAInstall || name != "Fall Foliage" || !slices.Contains(drawn, "Fall Foliage emoji for your team") {
		t.Errorf("after the read: %q %v, drawn %q", name, kind, drawn)
	}
	if after != first {
		t.Errorf("the box moved when the read landed: %v → %v", first, after)
	}
}

// ctaRT lays the picker out through a router, for real clicks.
type ctaRT struct {
	t   *testing.T
	p   *EmojiPicker
	h   *tzHost
	r   input.Router
	ops op.Ops
	at  time.Duration
}

func (e *ctaRT) frame() {
	win := image.Pt(420, 560)
	e.ops.Reset()
	gtx := layout.Context{Ops: &e.ops, Now: time.Now(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(win), Source: e.r.Source()}
	paint.FillShape(&e.ops, colorsOf(e.h.th).EmojiFoot, clip.Rect{Max: win}.Op())
	e.p.Layout(gtx)
	e.r.Frame(&e.ops)
	Pump()
}

func (e *ctaRT) click(pt image.Point) {
	p := f32.Pt(float32(pt.X)+.5, float32(pt.Y)+.5)
	e.at += time.Second
	e.r.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p, Time: e.at},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: p, Time: e.at + time.Millisecond},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: p, Time: e.at + 2*time.Millisecond},
	)
	e.frame()
	e.frame()
}

// The call to action's click target is Slack's content-sized unstyled
// button: the 22px label line from the inner box's 12px padding to the
// arrow's end (and its art). A click in the box's padding above it, or in
// the box right of it, does nothing; on the label it opens the Add emoji
// dialog.
func TestEmojiPackCTAClickArea(t *testing.T) {
	h := ctaEnv(t, []fakeslack.EmojiPack{ctaPack("Ec0CTA00001", "Fall Foliage", time.Now().Add(-24*time.Hour))}, nil, true, nil)
	e := &ctaRT{t: t, h: h, p: ctaPicker(h, 1)}
	h.ShowOverlay(e.p)
	e.frame()
	e.frame()
	_, kind, box := e.p.PackCTA()
	btn := e.p.PackCTAButton()
	if kind != core.PackCTAInstall || btn.Empty() {
		t.Fatalf("call to action %v, button %v", kind, btn)
	}
	// the label's line: 12 in, 12 down, 22 tall
	if btn.Min.X-box.Min.X != 12 || btn.Min.Y-box.Min.Y != 12 || btn.Dy() != 22 {
		t.Errorf("button %v in the box %v, want 12px in and down, 22 tall", btn.Sub(box.Min), box.Size())
	}
	dismissX := box.Max.X - 8 - 22
	if btn.Max.X+8 >= dismissX {
		t.Fatalf("button %v runs into the dismiss button at x %d: no space right of it to click", btn, dismissX)
	}
	misses := map[string]image.Point{
		"the box's top padding":    image.Pt((btn.Min.X+btn.Max.X)/2, box.Min.Y+4),
		"the box's bottom padding": image.Pt((btn.Min.X+btn.Max.X)/2, box.Max.Y-4),
		"right of the button":      image.Pt((btn.Max.X+dismissX)/2, (btn.Min.Y+btn.Max.Y)/2),
		"the box's left padding":   image.Pt(box.Min.X+5, (btn.Min.Y+btn.Max.Y)/2),
	}
	for what, pt := range misses {
		e.click(pt)
		if e.p.Dismissed() || h.a.PrefBool(core.PrefEmojiPacksPickerCTA, false) {
			t.Fatalf("a click on %s (%v) took the call to action", what, pt)
		}
	}
	e.click(image.Pt((btn.Min.X+btn.Max.X)/2, (btn.Min.Y+btn.Max.Y)/2))
	if !e.p.Dismissed() || !h.a.PrefBool(core.PrefEmojiPacksPickerCTA, false) {
		t.Errorf("a click on the label: picker dismissed %v, pref %v", e.p.Dismissed(), h.a.PrefBool(core.PrefEmojiPacksPickerCTA, false))
	}
}
