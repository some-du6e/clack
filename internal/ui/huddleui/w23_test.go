package huddleui

import (
	"context"
	"image"
	"sync"
	"testing"
	"time"

	"gioui.org/op"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// gap-w23: the huddle window's leftovers against Slack's build
// 2cd9270e786c: HuddleStickerGrid (boot-shared Vw0B), HuddleLoungeMusicBanner
// (4ahz, fHBk) and the docked bar's useResponsiveHuddleToolbarButtons
// (render wjeI). Logic first; the pictures (huddles-audit-shots' w23-*)
// after, since pixeltest skips the rest of a test without a GPU.

func hasString(strs []string, want string) bool {
	for _, s := range strs {
		if s == want {
			return true
		}
	}
	return false
}

// ---- HuddleStickerGrid ----

func TestStickerGridLikeSlack(t *testing.T) {
	const W, H = 589, 351
	e, s := windowShotEnv(t, true, 2)
	e.w.tweak = nil
	var ops op.Ops
	e.w.layout(shotCtx(&ops, W, H))
	if e.w.stickBtn.Empty() {
		t.Fatal("no sticker caret in the toolbar")
	}
	e.w.click("stickers").Click()
	e.w.layout(shotCtx(&ops, W, H))
	g := e.w.stick
	if g == nil {
		t.Fatal("the caret didn't open the sticker grid")
	}
	// the grid's stickers: no On mobile, Audio-only or (without
	// huddles_music) DJ'ing
	var got []string
	for _, k := range e.c.gridStickers() {
		got = append(got, k.Emoji)
	}
	if want := "raised_hand coffee +1 -1 white_check_mark"; joinSp(got) != want {
		t.Fatalf("grid %q, want %q", joinSp(got), want)
	}
	e.h.app.SetExperimentGroup(ExpMusic, "on")
	if n := len(e.c.gridStickers()); n != 6 || e.c.gridStickers()[2].Emoji != "headphones" {
		t.Fatalf("huddles_music on: %d stickers, want DJ'ing third of 6", n)
	}
	if e.c.stickerLabel("headphones") != "DJ’ing" {
		t.Fatalf("headphones label %q", e.c.stickerLabel("headphones"))
	}
	e.h.app.SetExperimentGroup(ExpMusic, "off")
	// 360 wide; 12 + 18 + 4 of subtitle, three rows of 56 + 16, 8 below
	gtx := shotCtx(&ops, W, H)
	if sz, want := g.size(gtx, 5), image.Pt(360*shotScale, (34+3*72+8)*shotScale); sz != want {
		t.Fatalf("card %v, want %v", sz, want)
	}
	e.w.layout(shotCtx(&ops, W, H))
	if want := e.w.stickBtn.Min.Y + e.w.barTop - sgOffsetY*shotScale; g.card.Max.Y != want {
		t.Errorf("card %v ends at %d, want 5px above the caret (%d)", g.card, g.card.Max.Y, want)
	}
	strs := hudStrings(hudSurface{w: W, h: H, draw: func(gtx C) { e.w.layout(gtx) }})
	for _, want := range []string{"Stickers stay on until you remove them.", "Raise hand", "Be right back", "Yes", "No", "Done"} {
		if !hasString(strs, want) {
			t.Errorf("the grid didn't draw %q (drew %q)", want, strs)
		}
	}
	for _, not := range []string{"On mobile", "Audio-only", "DJ’ing"} {
		if hasString(strs, not) {
			t.Errorf("the grid drew %q", not)
		}
	}
	// a pick puts the sticker on and closes the grid
	g.pick("coffee").Click()
	e.w.layout(shotCtx(&ops, W, H))
	if e.w.stick != nil {
		t.Fatal("a pick left the grid open")
	}
	waitState(t, s, "the coffee sticker", func(st huddle.State) bool { return s.Sticker() == "coffee" })
	// the close button closes it
	e.w.toggleStickers()
	e.w.layout(shotCtx(&ops, W, H))
	e.w.stick.close.Click()
	e.w.layout(shotCtx(&ops, W, H))
	if e.w.stick != nil {
		t.Fatal("the close button left the grid open")
	}

	// the dock's caret opens the same grid over the main window, and
	// toggles it
	oh := &overlayHost{hcHost: *e.h}
	e.c.h = oh
	st := s.State()
	e.c.dockClick(shotCtx(&ops, 1000, 100), "stickers", s, st, image.Rect(500, 10, 528, 50))
	if len(oh.shown) != 1 {
		t.Fatalf("the dock's caret showed %d overlays", len(oh.shown))
	}
	dg, ok := oh.shown[0].(*stickerGrid)
	if !ok {
		t.Fatalf("dock overlay %T, want the sticker grid", oh.shown[0])
	}
	var caretOn bool
	for _, k := range e.c.dockCtls(st) {
		if k.id == "stickers" {
			caretOn = k.on
		}
	}
	if !caretOn {
		t.Error("the dock's caret isn't toggled on with the grid open")
	}
	e.c.dockClick(shotCtx(&ops, 1000, 100), "stickers", s, st, image.Rect(500, 10, 528, 50))
	if !dg.Dismissed() {
		t.Error("a second click on the dock's caret didn't close the grid")
	}

	// pictures: the window's grid (the window is dark in both modes), with
	// Be right back selected, and the dock's on the main window's theme
	e.c.h = e.h
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		e.h.th = theme.New(dark)
		withPictures(e)
		e.w.toggleStickers()
		settle(t, e, W, H)
		draw := func(gtx C) { e.w.layout(gtx) }
		loadPictures(t, e, W, H, draw)
		img := render(t, "w23-sticker-grid-window-"+mode, W, H, draw)
		g := e.w.stick
		// the selected tile (Be right back, second) is base-inv-hgl-2
		cell := image.Rect(g.card.Max.X-170*shotScale, g.card.Min.Y+44*shotScale, g.card.Max.X-12*shotScale, g.card.Min.Y+96*shotScale)
		if n := pixeltest.Count(img, cell, dtColor(e.w.th, "base-inv-hgl-2"), 8); n < cell.Dx()*cell.Dy()/3 {
			t.Errorf("%s: the selected sticker isn't base-inv-hgl-2 (%d px)", mode, n)
		}
		// an unselected tile (Raise hand, first) is ctr-sec
		cell0 := image.Rect(g.card.Min.X+12*shotScale, g.card.Min.Y+44*shotScale, g.card.Min.X+170*shotScale, g.card.Min.Y+96*shotScale)
		if n := pixeltest.Count(img, cell0, dtColor(e.w.th, "ctr-sec"), 8); n < cell0.Dx()*cell0.Dy()/3 {
			t.Errorf("%s: a sticker tile isn't ctr-sec (%d px)", mode, n)
		}
		e.w.closeMenus()

		const MW, MH = 1000, 520
		dock := newStickerGrid(e.c, &overlayHost{hcHost: *e.h}, image.Rect(560*shotScale, (MH-50)*shotScale, 588*shotScale, (MH-10)*shotScale))
		draw = func(gtx C) {
			theme.Fill(gtx, image.Rectangle{Max: gtx.Constraints.Max}, e.h.th.C.Bg)
			dock.Layout(gtx)
		}
		loadPictures(t, e, MW, MH, draw)
		img = render(t, "w23-sticker-grid-dock-"+mode, MW, MH, draw)
		in := image.Rect(dock.card.Min.X+4*shotScale, dock.card.Min.Y+4*shotScale, dock.card.Max.X-40*shotScale, dock.card.Min.Y+30*shotScale)
		if n := pixeltest.Count(img, in, popups.ColorsOf(e.h.th).EmojiBg, 6); n < in.Dx()*in.Dy()/2 {
			t.Errorf("%s: the dock's grid card isn't sk_primary_background (%d px)", mode, n)
		}
	}
}

func joinSp(s []string) string {
	out := ""
	for i, v := range s {
		if i > 0 {
			out += " "
		}
		out += v
	}
	return out
}

// ---- HuddleLoungeMusicBanner ----

// soloEnv is a shot env alone in the fake huddle with the lounge music on
// after 1 s.
func soloEnv(t *testing.T, dark bool) (*shotEnv, *huddle.Session, *avHooks) {
	t.Helper()
	hk := installAVHooks(t)
	e := newShotEnv(t, dark)
	withPictures(e)
	setPrefs(e.h.app, map[string]any{"huddles_play_music_when_last": true, "huddles_play_music_timeout": 1})
	e.fk = huddle.NewFake(nil, t.Errorf)
	t.Cleanup(e.fk.Close)
	e.fk.SoloRoom()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// no OnChange: rooms.join lists the other person even in a solo room,
	// so the session's own ticks would read "together"; the test drives
	// musicTick with the alone state, as TestHuddleLoungeMusic does
	s, err := huddle.Join(ctx, huddle.Options{Client: e.fk.Caller(), SelfID: huddle.FakeSelfUser, Device: huddle.NewToneDevice(440),
		Logf: t.Logf, OnChange: func() {}}, hcChannel)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Leave)
	e.c.mu.Lock()
	e.c.s = s
	e.c.mu.Unlock()
	waitState(t, s, "connected", func(st huddle.State) bool { return st.Phase == huddle.Connected })
	e.c.sessionStarted(s)
	e.c.av.mu.Lock()
	e.c.av.together, e.c.av.alone = false, false // as if just joined
	e.c.av.mu.Unlock()
	e.c.musicTick(s, huddle.State{Phase: huddle.Connected, Participants: []huddle.Participant{{UserID: huddle.FakeSelfUser, Self: true}}})
	avWaitFor(t, "the lounge music", func() bool { return e.c.MusicPlaying() && s.LocalPlaying() })
	return e, s, hk
}

type prefLog struct {
	mu  sync.Mutex
	got map[string]any
}

func stubPrefs(t *testing.T) *prefLog {
	l := &prefLog{got: map[string]any{}}
	old := prefWriter
	t.Cleanup(func() { prefWriter = old })
	prefWriter = func(app *core.App, name string, v any) error {
		l.mu.Lock()
		l.got[name] = v
		l.mu.Unlock()
		setPrefs(app, map[string]any{name: v})
		return nil
	}
	return l
}

func (l *prefLog) get(name string) (any, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	v, ok := l.got[name]
	return v, ok
}

func TestLoungeBannerLikeSlack(t *testing.T) {
	const W, H = 589, 351
	pl := stubPrefs(t)
	e, s, hk := soloEnv(t, true)
	if !e.c.LoungeBanner() {
		t.Fatal("no banner with the music on")
	}
	var ops op.Ops
	now := time.Now()
	gtx := shotCtx(&ops, W, H)
	gtx.Now = now
	// the first 15 s: the info line, its divider, the player: 12 + 22 +
	// 8 + 1 + 8 + 22 + 12
	r := image.Rect(gtx.Dp(4), gtx.Dp(winTitleH+4), gtx.Dp(W-4), gtx.Dp(winTitleH+4))
	if h := e.w.layoutLounge(gtx, s, r); h != (12+22+8+1+8+22+12)*shotScale {
		t.Fatalf("banner %d px, want 85 dp", h)
	}
	strs := hudStrings(hudSurface{w: W, h: H, draw: func(gtx C) { e.w.layout(gtx) }})
	for _, want := range []string{"You’re the only one in this huddle. Enjoy the tranquility.", "Smooth Jazz"} {
		if !hasString(strs, want) {
			t.Errorf("the window didn't draw %q (drew %q)", want, strs)
		}
	}
	// after 15 s the info line is gone for this huddle: the player alone,
	// at c-banner's 48px
	ops.Reset()
	gtx = shotCtx(&ops, W, H)
	gtx.Now = now.Add(15*time.Second + time.Millisecond)
	if h := e.w.layoutLounge(gtx, s, r); h != 48*shotScale {
		t.Fatalf("after 15 s the banner is %d px, want 48 dp", h)
	}
	e.c.av.mu.Lock()
	gone := e.c.av.infoGone
	e.c.av.mu.Unlock()
	if !gone {
		t.Fatal("isLoungeMusicBannerDismissed not set after 15 s")
	}
	// an unmount cancels the timer (it never fired here): a new mount
	// starts it again
	e.c.av.mu.Lock()
	e.c.av.infoGone = false
	e.c.av.mu.Unlock()
	e.c.loungeUnmount(lmWindow)
	if ok, until := e.c.loungeInfo(lmDock, now); !ok || !until.Equal(now.Add(lmInfoFor)) {
		t.Fatalf("a new mount: info %v until %v", ok, until)
	}

	// the song menu: Choose a song, the nine songs (no Miles Davis without
	// the experiment, which core's defaults turn on as the account has
	// it), Settings ›
	e.h.app.SetExperimentGroup(ExpMilesDavis, "off")
	items := e.c.musicMenu()
	if items[0].Header != "Choose a song" || len(items) != 1+9+2 {
		t.Fatalf("menu %d rows, first %+v", len(items), items[0])
	}
	if !items[4].Checked || items[4].Label != "Smooth Jazz" {
		t.Errorf("Smooth Jazz (jazz, the default) isn't checked: %+v", items[4])
	}
	set := items[len(items)-1]
	if set.Label != "Settings" || len(set.Sub) != 7 || set.Sub[0].Header != "Play music when alone in a huddle…" ||
		!set.Sub[1].Checked || set.Sub[5].Label != "Never play music" || set.Sub[5].Checked || set.Sub[6].Header != "You can undo this later in preferences." {
		t.Fatalf("Settings › %+v", set.Sub)
	}
	e.h.app.SetExperimentGroup(ExpMilesDavis, "on")
	if items := e.c.musicMenu(); len(items) != 1+12+2 || items[1].Label != "So What by Miles Davis" {
		t.Fatalf("huddles_miles_davis: %d rows, then %q", len(items), items[1].Label)
	}
	e.h.app.SetExperimentGroup(ExpMilesDavis, "off")

	// Turn off music: the music and the banner go for the rest of the
	// huddle; the alone notice coming back doesn't restart it
	e.c.StopLoungeMusic()
	if e.c.MusicPlaying() || s.LocalPlaying() || e.c.LoungeBanner() {
		t.Fatal("music on after Turn off music")
	}
	e.c.startMusic(s)
	if e.c.MusicPlaying() {
		t.Fatal("the music came back after you turned it off")
	}
	// a song pick plays it, clears the stop and saves the pref
	e.c.PlaySong("subway")
	avWaitFor(t, "Subway Music", func() bool { return e.c.MusicPlaying() && s.LocalPlaying() })
	if got := hk.songs(); got[len(got)-1] != "subway_music-ff03dfb.mp3" {
		t.Fatalf("loaded %v", got)
	}
	avWaitFor(t, "the song pref", func() bool { v, ok := pl.get("huddles_music_player_song_key"); return ok && v == "subway" })
	if e.c.trackName() != "Subway Music" {
		t.Errorf("trigger reads %q", e.c.trackName())
	}
	// Never play music turns the pref off and stops it
	set = e.c.musicMenu()[len(e.c.musicMenu())-1]
	set.Sub[5].Do()
	avWaitFor(t, "the music stopping", func() bool { return !e.c.MusicPlaying() })
	if v, _ := pl.get("huddles_play_music_when_last"); v != false {
		t.Fatalf("huddles_play_music_when_last %v", v)
	}
	setPrefs(e.h.app, map[string]any{"huddles_play_music_when_last": true, "huddles_play_music_timeout": 1})
	e.c.PlaySong("jazz")
	avWaitFor(t, "jazz", func() bool { return e.c.MusicPlaying() })

	// the dock (window closed): the player alone above the card, and the
	// stop button there works too
	if e.c.WindowOpen() {
		t.Fatal("window open in the shot env")
	}
	gd := shotCtx(&ops, 1000, 200)
	if got, want := e.c.dockLoungeHeight(gd, e.h.th, gd.Dp(1000-2*dockPad)), (48+4)*shotScale; got != want {
		t.Fatalf("dock banner %d px, want 48 + 4 dp", got)
	}
	if got, want := e.c.DockHeight(gd), (dockBar+48+4+dockPad)*shotScale; got != want {
		t.Fatalf("dock %d px, want %d", got, want)
	}
	dstr := hudStrings(hudSurface{w: 1000, h: dockBar + 48 + 4 + dockPad, draw: func(gtx C) { e.c.LayoutDock(gtx, e.h.th) }})
	if hasString(dstr, "You’re the only one in this huddle. Enjoy the tranquility.") || !hasString(dstr, "Smooth Jazz") {
		t.Errorf("dock drew %q, want the player without the info line", dstr)
	}

	// pictures: the window with the info line and without, the dock, in
	// light and dark
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		e.h.th = theme.New(dark)
		withPictures(e)
		e.c.av.mu.Lock()
		e.c.av.infoGone, e.c.av.infoOwner, e.c.av.infoUntil = false, "", time.Time{}
		e.c.av.mu.Unlock()
		e.w.lounge.shown = false
		draw := func(gtx C) { e.w.layout(gtx) }
		loadPictures(t, e, W, H, draw)
		img := render(t, "w23-lounge-window-"+mode, W, H, draw)
		top := image.Rect(8*shotScale, (winTitleH+8)*shotScale, (W-8)*shotScale, (winTitleH+14)*shotScale)
		if n := pixeltest.Count(img, top, bannerBg, 6); n < top.Dx()*top.Dy()/2 {
			t.Errorf("%s: no base-inv-pry banner under the title (%d px)", mode, n)
		}
		render(t, "w23-lounge-window-later-"+mode, W, H, func(gtx C) {
			gtx.Now = time.Now().Add(16 * time.Second)
			e.w.layout(gtx)
		})
		dh := dockBar + 48 + 4 + dockPad
		dock := func(gtx C) {
			theme.Fill(gtx, image.Rectangle{Max: gtx.Constraints.Max}, hudFrame)
			e.c.LayoutDock(gtx, e.h.th)
		}
		loadPictures(t, e, 1000, dh, dock)
		img = render(t, "w23-lounge-dock-"+mode, 1000, dh, dock)
		bb := image.Rect(20*shotScale, 4*shotScale, 300*shotScale, 8*shotScale)
		if n := pixeltest.Count(img, bb, loungeInks(lmDock).bg, 6); n < bb.Dx()*bb.Dy()/2 {
			t.Errorf("%s: no banner above the dock's card (%d px)", mode, n)
		}
	}
}

// ---- useResponsiveHuddleToolbarButtons ----

func TestDockResponsiveButtonsLikeSlack(t *testing.T) {
	var ops op.Ops
	gtx := shotCtx(&ops, 1000, 100)
	dp := func(v int) int { return v * shotScale }
	for _, c := range []struct {
		w, max, want int
		cam          bool
	}{
		{500, 7, 7, true}, {486, 7, 7, true}, {485, 7, 6, true}, {438, 7, 6, true}, {400, 7, 5, true},
		{300, 7, 4, true}, {250, 7, 3, true}, {200, 7, 2, true}, {100, 7, 1, true}, {50, 7, 1, true},
		{366, 6, 6, false}, {320, 6, 5, false}, {200, 6, 3, false}, {77, 6, 1, false},
	} {
		if got := respCount(gtx, dp(c.w), c.max, dockBreakpoints(c.cam)); got != c.want {
			t.Errorf("width %d (camera %v): %d buttons, want %d", c.w, c.cam, got, c.want)
		}
	}
	comps := [][]ctl{{{id: "mic", seg: segMain}, {id: "micmenu", seg: segCaret}}, {{id: "camera", seg: segMain}, {id: "videomenu", seg: segCaret}},
		{{id: "share"}}, {{id: "hand", seg: segMain}, {id: "stickers", seg: segCaret}}, {{id: "react"}}, {{id: "thread"}}, {{id: "more"}}}
	ids := func(cs [][]ctl) string {
		var out []string
		for _, k := range cs {
			out = append(out, k[0].id)
		}
		return joinSp(out)
	}
	if got := ids(sliceComps(comps, 4)); got != "mic camera share more" {
		t.Fatalf("slice 4: %q", got)
	}
	if got := ids(sliceComps(comps, 1)); got != "more" {
		t.Fatalf("slice 1: %q", got)
	}
	// all seven are 408dp: 3 segmented of 64 + 8, 4 of 40 + 8
	if w := compsWidth(gtx, comps); w != dp(3*72+4*48) {
		t.Fatalf("row %d px, want 408 dp", w)
	}
	// the hook: every button until the debounced observer has measured
	t0 := time.Now()
	var r dockResp
	frame := func(at time.Duration, w int) string {
		ops.Reset()
		g := shotCtx(&ops, 1000, 100)
		return ids(r.layout(g, t0.Add(at), dp(w), comps, dockBpsCamera))
	}
	all := "mic camera share hand react thread more"
	if got := frame(0, 400); got != all {
		t.Fatalf("first frame %q, want every button", got)
	}
	if got := frame(100*time.Millisecond, 400); got != all {
		t.Fatalf("inside the debounce %q", got)
	}
	if got := frame(260*time.Millisecond, 400); got != "mic camera share hand more" {
		t.Fatalf("400 dp after the debounce: %q, want 5", got)
	}
	// growing within the step shows them all again while they fit
	frame(time.Second, 420)
	if got := frame(time.Second+260*time.Millisecond, 420); got != all {
		t.Fatalf("grown to 420 dp: %q, want every button (408 dp fit)", got)
	}
	// shrinking below the row re-picks
	frame(2*time.Second, 404)
	if got := frame(2*time.Second+260*time.Millisecond, 404); got != "mic camera share hand more" {
		t.Fatalf("shrunk to 404 dp: %q", got)
	}
	frame(3*time.Second, 200)
	if got := frame(3*time.Second+260*time.Millisecond, 200); got != "mic more" {
		t.Fatalf("200 dp: %q", got)
	}
	// the grid: minmax(0,1fr) minmax(0,2fr) minmax(max-content,1fr), gap 16
	if c1, c2 := dockColumns(980, 168, 16); c1 != 237 || c2 != 474 {
		t.Fatalf("980 wide: %d / %d, want 1fr = 237", c1, c2)
	}
	if c1, c2 := dockColumns(600, 168, 16); c1 != (568-168)/3 || c2 != 2*((568-168)/3) {
		t.Fatalf("600 wide (right column at its content): %d / %d", c1, c2)
	}

	// the bar at three widths, light and dark: Leave stays; ⋯ stays last
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		e, s := windowShotEnv(t, dark, 2)
		e.c.mu.Lock()
		e.c.win = nil
		e.c.mu.Unlock()
		if !e.c.videoAllowed() {
			t.Fatal("the shot env has no camera button")
		}
		_ = s
		// the middle column at 1200, 1000, 760 and 560 dp: 570, 470, 350
		// and 242 dp (the right column's 136 dp: the window button and
		// Leave with their margins), so 7, 6, 5 and 3 buttons
		for _, w := range []int{1200, 1000, 760, 560} {
			dh := dockBar + dockPad
			at := time.Now()
			draw := func(gtx C) {
				gtx.Now = at
				theme.Fill(gtx, image.Rectangle{Max: gtx.Constraints.Max}, hudFrame)
				e.c.LayoutDock(gtx, e.h.th)
			}
			loadPictures(t, e, w, dh, draw)
			at = at.Add(time.Second) // past the debounce
			var ops op.Ops
			g := shotCtx(&ops, w, dh)
			g.Now = at
			e.c.LayoutDock(g, e.h.th)
			r := e.c.dock.resp
			n := r.max
			if !r.showAll {
				n = r.visible
			}
			want := map[int]int{1200: 7, 1000: 6, 760: 5, 560: 3}[w]
			if n != want {
				t.Errorf("%s %d dp: %d buttons, want %d", mode, w, n, want)
			}
			render(t, "w23-dock-"+mode+"-"+itoa(w), w, dh, draw)
		}
	}
}
