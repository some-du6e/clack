package huddleui

import (
	"context"
	"image"
	"image/color"
	"log"
	"time"

	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// HuddleLoungeMusicBanner (gantry-v2-async-client-boot-shared 4ahz, build
// 2cd9270e786c): the lounge music's player while you're alone.
//
// Where it shows: HuddlesAlertBannerContainer's temporary banner
// (HuddlesTemporaryControlBanner, 7tWB) is "lounge_music_player" when
// aAey's M holds: you're in a huddle, the alone notice is up
// (isAloneInHuddleCoachmarkOpen: INITIAL_ or IN_PROGRESS_ALONE_NOTICE_
// ACTIVE), huddles_play_music_when_last is on, you haven't turned the
// music off in this huddle (hasUserStoppedLoungeMusic), and the huddle
// window is closed or in compact mode. The compact window draws it above
// the informational banner (entryPoint baby_window); with the window
// closed the docked bar draws it above its card (bottom_bar), where it
// takes the place of the warning banners (the bar shows one type at a
// time, temporary first). Lki0 / jaWE mount it, and mounting it starts
// the song (startHuddleLoungeMusic, looped); Clack's startMusic runs at
// the same moment (avprefs.go), so the banner is up exactly while
// MusicPlaying.
//
// What it draws (client-boot-styles): a neutral c-banner (base-inv-pry,
// content-inv-pry), .p-huddles_alert_banner_base--lounge_music padding
// 12px 0, baby_window radius 8px and margin 4px 4px 6px, bottom_bar
// radius 12px and margin-bottom 4px; inside, a column:
//
//   - the first 15 s (isLoungeMusicBannerDismissed after a 15e3 timeout,
//     per huddle) and not in the bottom bar: "You’re the only one in
//     this huddle. Enjoy the tranquility." (.p-huddle_lounge_music_
//     banner__info_msg: padding 0 12px, left-aligned, two lines at most;
//     __divider--bottom: a 1px otl-sec line, 8px each side);
//   - the player (.p-huddle_lounge_music_banner__player: padding 0 12px,
//     #f8f8f8): the music note (__icon: 20px wide, margin-right 5px; the
//     15px note, zqUb), HuddleLoungeMusicMenuTrigger (fHBk: the song's
//     name and a 20px caret, tooltip "Choose a song", aria-label
//     "{trackName}, music settings") and HuddleLoungeMusicControlButton
//     (margin-left auto, margin-right 4px; ZCaB's 20px stop icon, tooltip
//     "Turn off music"), which stops the music for the rest of the huddle
//     (stopHuddleLoungeMusic isStoppedByUser).
//
// Playing, zqUb draws a looping Lottie note instead of the still one;
// Clack has no Lottie player, so it draws the still note
// (docs/HUDDLES-AUDIT.md). The info line's tooltip (Truncate withTooltip)
// only matters when two lines can't hold it, which the window's 455px
// minimum width doesn't reach.

const (
	lmPadY      = 12 // .p-huddles_alert_banner_base--lounge_music padding
	lmPadX      = 12 // __info_msg / __player padding-left/right
	lmLine      = 22 // 15px × 1.46668
	lmDivGap    = 8  // __divider--bottom padding-bottom / margin-bottom
	lmIconBox   = 20 // __icon width
	lmIconGap   = 5  // its margin-right
	lmNote      = 15 // .p-huddle_lounge_music_banner__icon .p-huddle_lounge_music_icon
	lmCaret     = 20
	lmStop      = 20 // .p-huddle_lounge_music_stop_icon
	lmStopRight = 4  // .p-huddle_lounge_music_control_button margin-right
	lmMinH      = 48 // .c-banner min-height
	lmInfoFor   = 15 * time.Second
)

// The banner's entry points.
const (
	lmWindow = "baby_window"
	lmDock   = "bottom_bar"
)

// The note (zqUb, viewBox 0 0 10 10) and the stop icon (ZCaB, 0 0 20 20).
const (
	lmNotePath = "M8.84 0.0369728L3.33333 1.58054C2.94667 1.69535 2.66667 2.10357 2.66667 2.48627V7.28281C2.45333 7.21903 2.22667 7.19351 2 7.19351C0.853334 7.19351 0 7.81859 0 8.59676C0 9.36216 0.853334 10 2 10C3.01333 10 4 9.36216 4 8.59676V2.72865L8.66667 1.45297V5.64995C8.45333 5.58616 8.22667 5.56065 8 5.56065C6.85333 5.56065 6 6.18573 6 6.96389C6 7.7293 6.85333 8.36713 8 8.36713C9.01333 8.36713 10 7.7293 10 6.96389V0.904432C10 0.355892 9.45333 -0.141622 8.84 0.0369728Z"
	lmStopRing = "M10 2.5C5.85786 2.5 2.5 5.85786 2.5 10C2.5 14.1421 5.85786 17.5 10 17.5C14.1421 17.5 17.5 14.1421 17.5 10C17.5 5.85786 14.1421 2.5 10 2.5ZM1 10C1 5.02944 5.02944 1 10 1C14.9706 1 19 5.02944 19 10C19 14.9706 14.9706 19 10 19C5.02944 19 1 14.9706 1 10Z"
)

// lmPlayerInk is .p-huddle_lounge_music_banner__player's colour
// (--_huddle-lounge-music-content-color's #f8f8f8 fallback).
var lmPlayerInk = color.NRGBA{R: 0xf8, G: 0xf8, B: 0xf8, A: 0xff}

// loungeSongMeta is getSongsMetadata (client-boot IeZG) in its order:
// the Miles Davis three first, disabled without huddles_miles_davis
// (whose display names are then untranslated).
type loungeSongMeta struct {
	key, name string
	disabled  bool
}

func (c *Controller) loungeSongs() []loungeSongMeta {
	md := c.on(ExpMilesDavis)
	t := func(s string) string {
		if md {
			return nsHuddles.T(s)
		}
		return s
	}
	return []loungeSongMeta{
		{"so_what", t("So What by Miles Davis"), !md},
		{"freddie_freeloader", t("Freddie Freeloader by Miles Davis"), !md},
		{"all_blues", t("All Blues by Miles Davis"), !md},
		{"waiting", nsHuddles.T("Waiting Room"), false},
		{"subway", nsHuddles.T("Subway Music"), false},
		{"whistle", nsHuddles.T("Whistle Tune"), false},
		{"jazz", nsHuddles.T("Smooth Jazz"), false},
		{"focus", nsHuddles.T("Focus Beats"), false},
		{"disco", nsHuddles.T("Happy Bass"), false},
		{"zen", nsHuddles.T("Sound Bath"), false},
		{"nature", nsHuddles.T("Nature Meditation"), false},
		{"piano", nsHuddles.T("Piano Concerto"), false},
	}
}

// songKey is IeZG z2: the huddles_music_player_song_key pref, "jazz"
// when unset.
func (c *Controller) songKey() string {
	if app := c.h.Core(); app != nil {
		if k := app.PrefString("huddles_music_player_song_key"); k != "" {
			return k
		}
	}
	return "jazz"
}

// trackName is HuddleLoungeMusicMenuTrigger's v: the song's display name,
// Smooth Jazz for a Miles Davis song without the experiment.
func (c *Controller) trackName() string {
	k := c.songKey()
	if _, md := milesDavisSongs[k]; md && !c.on(ExpMilesDavis) {
		k = "jazz"
	}
	for _, s := range c.loungeSongs() {
		if s.key == k {
			return s.name
		}
	}
	return ""
}

// LoungeBanner reports whether HuddleLoungeMusicBanner is up (the music
// is on: you're alone past the notice delay with the pref on and haven't
// turned it off).
func (c *Controller) LoungeBanner() bool { return c.MusicPlaying() }

// StopLoungeMusic is HuddleLoungeMusicControlButton: stopHuddleLoungeMusic
// with isStoppedByUser, which keeps it off for the rest of the huddle
// (hasUserStoppedLoungeMusic) until a song is picked.
func (c *Controller) StopLoungeMusic() {
	s := c.Session()
	if s == nil {
		return
	}
	c.av.mu.Lock()
	c.av.stopped = true
	c.av.mu.Unlock()
	c.stopMusic(s)
	c.changed()
}

// prefWriter writes an account pref (users.prefs.set); tests swap it.
var prefWriter = func(app *core.App, name string, v any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return app.SetPref(ctx, name, v)
}

func (c *Controller) writePref(name string, v any, then func()) {
	app := c.h.Core()
	if app == nil {
		return
	}
	go func() {
		if err := prefWriter(app, name, v); err != nil {
			log.Printf("huddle: %s: %v", name, err)
			return
		}
		if then != nil {
			then()
		}
	}()
}

// PlaySong is HuddleLoungeMusicMenu's song pick: startHuddleLoungeMusic
// (songKey, loopSong; hasUserStoppedLoungeMusic back to false), then the
// huddles_music_player_song_key pref.
func (c *Controller) PlaySong(key string) {
	s := c.Session()
	file, ok := loungeSongs[key]
	if f, md := milesDavisSongs[key]; md {
		file, ok = f, true
	}
	if s == nil || !ok { // isValidSongKey
		return
	}
	c.av.mu.Lock()
	c.av.stopped = false
	c.av.musicOn = true
	c.av.mu.Unlock()
	go func() {
		pcm, err := loadMusic(file)
		if err != nil {
			log.Printf("huddle: lounge music %s: %v", file, err)
			return
		}
		c.av.mu.Lock()
		still := c.av.s == s && c.av.musicOn
		c.av.mu.Unlock()
		if still {
			s.PlayLocal(pcm, true)
			log.Printf("huddle: started lounge music (%s)", file)
		}
	}()
	c.writePref("huddles_music_player_song_key", key, nil)
	c.changed()
}

// musicMenu is HuddleLoungeMusicMenu: "Choose a song", the songs (the
// disabled Miles Davis ones left out), then Settings › "Play music when
// alone in a huddle…" with the delays (useHuddleMusicTimeoutOptions,
// jfdF Rv), "Never play music" and "You can undo this later in
// preferences.". done runs after a song pick (the trigger's caret).
func (c *Controller) musicMenu() []popups.MenuItem {
	cur := c.songKey()
	items := []popups.MenuItem{{Header: nsHuddles.T("Choose a song")}}
	for _, s := range c.loungeSongs() {
		if s.disabled {
			continue
		}
		k := s.key
		items = append(items, popups.MenuItem{Key: k, Label: s.name, Checked: cur == k, Do: func() { c.PlaySong(k) }})
	}
	app := c.h.Core()
	play, timeout := false, 60
	if app != nil {
		play = app.PrefBool("huddles_play_music_when_last", false)
		timeout = app.PrefInt("huddles_play_music_timeout", 60)
	}
	settings := []popups.MenuItem{{Header: nsHuddles.T("Play music when alone in a huddle…")}}
	for _, o := range []struct {
		key, label string
		v          int
	}{
		{"when_joining", nsHuddles.T("Right when I join"), 1},
		{"after_30_secs", nsHuddles.T("After 30 seconds"), 30},
		{"after_1_min", nsHuddles.T("After 1 minute"), 60},
	} {
		v := o.v
		settings = append(settings, popups.MenuItem{Key: o.key, Label: o.label, Checked: timeout == v, Do: func() {
			c.writePref("huddles_play_music_timeout", v, nil) // setUserPrefLoungeMusicTimeout
		}})
	}
	settings = append(settings, popups.Sep(), popups.MenuItem{Key: "never_play_music", Label: nsHuddles.T("Never play music"), Checked: !play, Do: func() {
		// setUserPrefShouldPlayLoungeMusic(!B); turning it off stops the
		// music (PrefsChanged)
		c.writePref("huddles_play_music_when_last", !play, c.PrefsChanged)
	}}, popups.MenuItem{Header: nsHuddles.T("You can undo this later in preferences.")})
	return append(items, popups.Sep(), popups.MenuItem{Key: "huddle-lounge-music-submenu-settings", Label: nsHuddles.T("Settings"), Sub: settings})
}

// musicMenuWidth is the menu's width "auto": its widest row.
func musicMenuWidth(gtx C, th *theme.Theme, items []popups.MenuItem) float32 {
	w := 0
	for _, it := range items {
		l := it.Label
		if l == "" {
			l = it.Header
		}
		w = max(w, labelSize(gtx, th, 15, theme.Regular, l).X)
	}
	// a row's 24px padding each side and the check column
	return min(max(float32(w)/gtx.Metric.PxPerDp+24+24+28, 200), 420)
}

// loungeUI is one surface's banner (the window's, the dock's).
type loungeUI struct {
	song, stop       widget.Clickable
	songTip, stopTip popups.TipState
	menu             *popups.Menu
	shown            bool // drawn last frame (mounted)
}

// loungeInfo runs the banner's 15 s timer for a surface (useEffect on
// mount: isLoungeMusicBannerDismissed once it fires, per huddle; an
// unmount cancels it) and reports whether the info line shows and until
// when.
func (c *Controller) loungeInfo(surface string, now time.Time) (bool, time.Time) {
	c.av.mu.Lock()
	defer c.av.mu.Unlock()
	a := &c.av
	if a.infoGone {
		return false, time.Time{}
	}
	if a.infoOwner != surface || a.infoUntil.IsZero() {
		a.infoOwner, a.infoUntil = surface, now.Add(lmInfoFor)
	}
	if !now.Before(a.infoUntil) {
		a.infoGone = true
		return false, time.Time{}
	}
	return true, a.infoUntil
}

// loungeUnmount is the timer's cleanup (clearTimeout) when a surface
// stops drawing the banner.
func (c *Controller) loungeUnmount(surface string) {
	c.av.mu.Lock()
	if c.av.infoOwner == surface {
		c.av.infoOwner, c.av.infoUntil = "", time.Time{}
	}
	c.av.mu.Unlock()
}

// loungeInk is the banner's c-banner--neutral tokens. The window's
// banners use the light tokens (the informational banner's bannerBg);
// the dock's sits in the client chrome's inverted scope (useInvertedTheme
// view "chrome"), so it takes the dark ones in either mode.
type loungeInk struct{ bg, fg, divider color.NRGBA }

func loungeInks(entry string) loungeInk {
	dark := entry == lmDock
	v := css.SlackVars(dark)
	tok := func(n string) color.NRGBA { c, _ := css.ParseColor(v["--dt_color-"+n]); return c }
	return loungeInk{bg: tok("base-inv-pry"), fg: tok("content-inv-pry"), divider: tok("otl-sec")}
}

// loungeHeight is the banner's height at width w (px; 0: not up).
func (c *Controller) loungeHeight(gtx C, th *theme.Theme, w int, entry string, info bool) int {
	if !c.LoungeBanner() || w <= 0 {
		return 0
	}
	h := gtx.Dp(lmPadY) + gtx.Dp(lmLine) + gtx.Dp(lmPadY)
	if info && entry != lmDock {
		h += c.infoLinesH(gtx, th, w) + 2*gtx.Dp(lmDivGap) + max(1, gtx.Dp(1))
	}
	return max(h, gtx.Dp(lmMinH))
}

// infoText is the banner's first line.
func infoText() string {
	return nsHuddles.T("You’re the only one in this huddle. Enjoy the tranquility.")
}

func (c *Controller) infoLinesH(gtx C, th *theme.Theme, w int) int {
	g := gtx
	g.Constraints.Min = image.Point{}
	g.Constraints.Max = image.Pt(max(0, w-2*gtx.Dp(lmPadX)), 1<<20)
	m := op.Record(gtx.Ops)
	d := th.Layout(g, theme.TextStyle{Size: 15, Weight: theme.Regular, LineHeight: lmLine, MaxLines: 2, Color: hudWhite}, infoText())
	m.Stop()
	return max(d.Size.Y, gtx.Dp(lmLine))
}

// layoutLounge draws the banner in r (its width; the height it needs is
// returned and drawn from r.Min) for entry, with ui its surface state and
// openMenu how that surface shows the song menu.
func (c *Controller) layoutLounge(gtx C, th *theme.Theme, ui *loungeUI, r image.Rectangle, entry string, openMenu func(anchor image.Rectangle)) int {
	now := gtx.Now
	if now.IsZero() {
		now = time.Now()
	}
	if !c.LoungeBanner() || r.Dx() <= 0 {
		if ui.shown {
			ui.shown = false
			c.loungeUnmount(entry)
		}
		if ui.menu != nil {
			ui.menu.Dismiss()
			ui.menu = nil
		}
		return 0
	}
	ui.shown = true
	info, until := c.loungeInfo(entry, now)
	if info {
		gtx.Execute(op.InvalidateCmd{At: until}) // the info line goes
	}
	if ui.stop.Clicked(gtx) {
		c.StopLoungeMusic()
		return 0
	}
	if ui.menu != nil && ui.menu.Dismissed() {
		ui.menu = nil
	}
	h := c.loungeHeight(gtx, th, r.Dx(), entry, info)
	ink := loungeInks(entry)
	rad := 8
	if entry == lmDock {
		rad = 12
	}
	box := image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+h)
	theme.RRect(gtx, box, gtx.Dp(unit.Dp(rad)), ink.bg)
	y := box.Min.Y + gtx.Dp(lmPadY)
	x0, x1 := box.Min.X+gtx.Dp(lmPadX), box.Max.X-gtx.Dp(lmPadX)
	if info && entry != lmDock {
		ih := c.infoLinesH(gtx, th, box.Dx())
		at(gtx, image.Pt(x0, y), image.Pt(x1-x0, ih), func(gtx C) D {
			return th.Layout(gtx, theme.TextStyle{Size: 15, Weight: theme.Regular, LineHeight: lmLine, MaxLines: 2, Color: ink.fg}, infoText())
		})
		y += ih + gtx.Dp(lmDivGap)
		theme.Fill(gtx, image.Rect(box.Min.X, y, box.Max.X, y+max(1, gtx.Dp(1))), theme.Over(ink.divider, ink.bg))
		y += max(1, gtx.Dp(1)) + gtx.Dp(lmDivGap)
	}
	rowH := gtx.Dp(lmLine)
	// the note, 15px in a 20px box
	ns := gtx.Dp(lmNote)
	at(gtx, image.Pt(x0+(gtx.Dp(lmIconBox)-ns)/2, y+(rowH-ns)/2), image.Pt(ns, ns), func(gtx C) D {
		fillPath(gtx, lmNotePath, float32(ns)/10, lmPlayerInk)
		return D{Size: image.Pt(ns, ns)}
	})
	// the stop button at the right
	ss := gtx.Dp(lmStop)
	sx := x1 - gtx.Dp(lmStopRight) - ss
	sr := image.Rect(sx, y+(rowH-ss)/2, sx+ss, y+(rowH-ss)/2+ss)
	at(gtx, sr.Min, sr.Size(), func(gtx C) D {
		d := button(gtx, &ui.stop, sr.Size(), nil, func(gtx C) D {
			sc := float32(ss) / 20
			fillPath(gtx, lmStopRing, sc, lmPlayerInk)
			rr := image.Rect(int(7*sc+.5), int(7*sc+.5), int(13*sc+.5), int(13*sc+.5))
			theme.RRect(gtx, rr, max(1, int(sc+.5)), lmPlayerInk)
			return D{Size: sr.Size()}
		})
		popups.Tooltip(gtx, th, &ui.stopTip, ui.stop.Hovered(), sr.Size(), popups.Above, nsHuddles.T("Turn off music"), "")
		return d
	})
	// HuddleLoungeMusicMenuTrigger: the song and its caret
	tx := x0 + gtx.Dp(lmIconBox+lmIconGap)
	cs := gtx.Dp(lmCaret)
	room := max(0, sx-tx-cs)
	name := ellipsize(gtx, th, 15, theme.Regular, c.trackName(), room)
	ls := labelSize(gtx, th, 15, theme.Regular, name)
	tw := ls.X + cs
	tr := image.Rect(tx, y, tx+tw, y+rowH)
	if ui.song.Clicked(gtx) && openMenu != nil {
		if ui.menu != nil {
			ui.menu.Dismiss()
			ui.menu = nil
		} else {
			openMenu(tr)
		}
	}
	caret := "caret-down" // caret-up while the menu is open
	if ui.menu != nil {
		caret = "caret-up"
	}
	at(gtx, tr.Min, tr.Size(), func(gtx C) D {
		d := button(gtx, &ui.song, tr.Size(), nil, func(gtx C) D {
			at(gtx, image.Pt(0, (rowH-ls.Y)/2), ls, func(gtx C) D { return th.Label(gtx, 15, theme.Regular, lmPlayerInk, name) })
			at(gtx, image.Pt(ls.X, (rowH-cs)/2), image.Pt(cs, cs), func(gtx C) D {
				return icons.Get(caret)(gtx, lmCaret, lmPlayerInk)
			})
			return D{Size: tr.Size()}
		})
		popups.Tooltip(gtx, th, &ui.songTip, ui.song.Hovered(), tr.Size(), popups.Above, nsHuddles.T("Choose a song"), "")
		return d
	})
	return h
}

// fillPath fills SVG path data d scaled by sc (px per unit) at the origin.
func fillPath(gtx C, d string, sc float32, col color.NRGBA) {
	defer clip.Outline{Path: icons.ClipPath(gtx, d, sc)}.Op().Push(gtx.Ops).Pop()
	paint.ColorOp{Color: col}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

// ---- the surfaces ----

// layoutLounge is the window's banner (baby_window) in r; it returns
// the height it took.
func (w *window) layoutLounge(gtx C, s *huddle.Session, r image.Rectangle) int {
	return w.c.layoutLounge(gtx, w.th, &w.lounge, r, lmWindow, func(anchor image.Rectangle) {
		w.closeMenus()
		items := w.c.musicMenu()
		w.lounge.menu = popups.NewMenuOpts(winHost{w}, items, popups.MenuOpts{Anchor: anchor, Side: popups.Above,
			Width: musicMenuWidth(gtx, w.th, items), MenuClass: "p-huddle_more_actions_menu"})
	})
}

// dockLoungeHeight is the dock's banner and its 4px margin (0: none).
func (c *Controller) dockLoungeHeight(gtx C, th *theme.Theme, w int) int {
	if c.WindowOpen() {
		return 0
	}
	if h := c.loungeHeight(gtx, th, w, lmDock, false); h > 0 {
		return h + gtx.Dp(4)
	}
	return 0
}

// layoutDockLounge is the dock's banner (bottom_bar) at the top of r.
func (c *Controller) layoutDockLounge(gtx C, th *theme.Theme, r image.Rectangle) int {
	if c.WindowOpen() {
		r = image.Rectangle{}
	}
	ph, _ := c.h.(popups.Host)
	return c.layoutLounge(gtx, th, &c.dock.lounge, r, lmDock, func(anchor image.Rectangle) {
		if ph == nil {
			return
		}
		items := c.musicMenu()
		c.dock.lounge.menu = popups.NewMenuOpts(ph, items, popups.MenuOpts{Anchor: anchor.Add(c.dockOrigin), Side: popups.Above,
			Width: musicMenuWidth(gtx, th, items), MenuClass: "p-huddle_more_actions_menu"})
		ph.ShowOverlay(c.dock.lounge.menu)
	})
}
