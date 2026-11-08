package ui

// Slick themes on overlays (ui-slick workflow): a Slick theme's CSS
// applies document-wide in Slack, so Preferences, menus and every other
// dialog take it. Every bundled theme repaints them with one raw rule,
// `[role=menu],[role=dialog],[role=listbox],.c-menu,.p-ia4_floating_window,
// .c-popover__content{background-color:…;border:1px solid …}`, its active
// Preferences tab with `.c-tabs__tab_menu--vertical .c-tabs__tab
// .c-tabs__tab--active{background-color:…}`, and the rest with Slack's
// tokens. This draws Preferences and a menu over the real upstream themes
// and reads the pixels. Screenshots go to SLICK_SHOTS_DIR. Synthetic data
// only.

import (
	"fmt"
	"image"
	"image/color"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme/pixeltest"
)

// svOverlayWant is what a theme's CSS gives the overlays ("" = the theme
// has no rule for it).
type svOverlayWant struct {
	id     string
	dialog string // [role=dialog] / .c-menu background-color
	tab    string // the active vertical tab's background-color
	text   string // --dt_color-content-pry / --sk_primary_foreground
	dark   bool   // the mode to draw in (dark-only themes)
	both   bool   // modeless: draw in light and dark
}

// Values copied from Slick's themes/*.json (main, 2026-11-08); the test
// re-reads the CSS so an upstream change shows up as a mismatch.
var svOverlayWants = []svOverlayWant{
	{id: "hot-dog-stand", dialog: "#cc0000", tab: "#cccc00", text: "#ffff00", both: true},
	{id: "nord", dialog: "#3b4252", tab: "#81a1c1", text: "#eceff4", dark: true},
	{id: "catppuccin-mocha", dialog: "#181825", tab: "#cba6f7", text: "#cdd6f4", dark: true},
	{id: "gruvbox-dark", dialog: "#32302f", tab: "#fabd2f", text: "#ebdbb2", dark: true},
	{id: "amoled", dialog: "#000000", dark: true},
}

// svPrefsRegions are Preferences' surfaces in the 1280×860 test window:
// the 800×700 dialog centred at (240, 80), its 76px header, the nav at
// (16, 17) under it with 29px tabs every 33px (Notifications, the second,
// is current), the page from x 249.
type svPrefsRegions struct{ panel, nav, tab image.Rectangle }

func svPrefsRegionsOf() svPrefsRegions {
	x, y := 240, 80
	body := y + 76
	return svPrefsRegions{
		panel: image.Rect(x+260, body+20, x+780, y+690),
		nav:   image.Rect(x+16, body+17+33*4, x+221, body+17+33*14),
		tab:   image.Rect(x+16+40, body+17+33+3, x+16+200, body+17+33+26),
	}
}

func (e *svEnv) closeOverlays() {
	for e.a.dismissTop() {
	}
	for i := 0; i < 3; i++ {
		e.frame()
	}
}

// checkPrefs opens Preferences and asserts its dialog and active tab.
func (e *svEnv) checkPrefs(sh *svShooter, w svOverlayWant, tag, shot string) {
	t := e.t
	t.Helper()
	e.closeOverlays()
	e.a.showPrefs("notifications")
	for i := 0; i < 6; i++ {
		e.frame()
	}
	img := sh.shot()
	sh.save(shot, img)
	r := svPrefsRegionsOf()
	bg, share := svDominant(img, r.panel)
	t.Logf("%s prefs panel: %s (%.0f%%)", tag, svHex(bg), share*100)
	if !svNear(bg, parseHexColor(w.dialog), 6) {
		t.Errorf("%s: Preferences is %s, the theme's [role=dialog] rule says %s", tag, svHex(bg), w.dialog)
	}
	if c, _ := svDominant(img, r.nav); !svNear(c, parseHexColor(w.dialog), 6) {
		t.Errorf("%s: the Preferences nav is %s, want the dialog's %s", tag, svHex(c), w.dialog)
	}
	if w.tab != "" {
		c, share := svDominant(img, r.tab)
		t.Logf("%s active tab: %s (%.0f%%)", tag, svHex(c), share*100)
		if !svNear(c, parseHexColor(w.tab), 6) {
			t.Errorf("%s: the active tab is %s, the theme says %s", tag, svHex(c), w.tab)
		}
	}
	if w.text != "" {
		if n := pixeltest.Ink(img, r.panel, parseHexColor(w.text), bg); n < 40 {
			t.Errorf("%s: no %s text on the Preferences page (%d px)", tag, w.text, n)
		}
	}
	// a theme: Slack's default dialog surfaces must not show through
	for _, gray := range []color.NRGBA{rgbT(0xffffff), rgbT(0x222529), rgbT(0x1a1d21), rgbT(0xf8f8f8)} {
		if w.id == "slack" || w.dialog == svHex(gray) {
			continue
		}
		if n := pixeltest.Count(img, r.panel, gray, 2); n > r.panel.Dx()*r.panel.Dy()/20 {
			t.Errorf("%s: %d px of Slack's default %s in Preferences", tag, n, svHex(gray))
		}
	}
}

// checkMenu opens a menu over the pane and asserts its card.
func (e *svEnv) checkMenu(sh *svShooter, w svOverlayWant, tag, shot string) {
	t := e.t
	t.Helper()
	e.closeOverlays()
	e.a.menu(image.Pt(520, 300), []MenuItem{{Label: "Copy link"}, {Label: "Mark unread"}, {Divider: true}, {Label: "Leave channel", Danger: true}})
	for i := 0; i < 4; i++ {
		e.frame()
	}
	img := sh.shot()
	sh.save(shot, img)
	area := image.Rect(480, 260, 900, 560)
	n := pixeltest.Count(img, area, parseHexColor(w.dialog), 6)
	t.Logf("%s menu %s: %d px", tag, w.dialog, n)
	if n < 15000 {
		t.Errorf("%s: the menu card isn't the theme's %s (%d px)", tag, w.dialog, n)
	}
	e.closeOverlays()
}

// TestSlickViewThemeOverlays picks upstream themes and checks Preferences
// and menus take their overlay rules and tokens, and that with Slack's own
// look they keep Slack's.
func TestSlickViewThemeOverlays(t *testing.T) {
	e := svThemesEnv(t)
	sh := newSVShooter(t, e)
	p := newSlickPage(e.a)
	defer p.close()
	defer e.closeOverlays()

	// no theme: Slack's measured dialog
	for _, dark := range []bool{false, true} {
		e.mode(dark)
		if err := e.s.SetTheme(""); err != nil {
			t.Fatal(err)
		}
		e.until(10*time.Second, "no theme", func() bool { return !e.s.ThemeApplies() })
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		want := svOverlayWant{id: "slack", dialog: map[bool]string{false: "#ffffff", true: "#222529"}[dark]}
		e.checkPrefs(sh, want, "Slack ("+mode+")", "overlay-slack-prefs-"+mode)
		e.checkMenu(sh, want, "Slack ("+mode+")", "overlay-slack-menu-"+mode)
	}

	for _, w := range svOverlayWants {
		j := svReadTheme(t, e.dir, w.id)
		css := string(j.CSS)
		short := w.dialog // #000000 is written #000
		if len(short) == 7 && short[1] == short[2] && short[3] == short[4] && short[5] == short[6] {
			short = "#" + short[1:2] + short[3:4] + short[5:6]
		}
		if !strings.Contains(css, "[role=dialog]") || !strings.Contains(css, "background-color:"+w.dialog) && !strings.Contains(css, "background-color:"+short+" ") {
			t.Errorf("%s: upstream has no [role=dialog] rule with %s now", w.id, w.dialog)
		}
		if w.tab != "" && !strings.Contains(css, "c-tabs__tab--active{background-color:"+w.tab) {
			t.Errorf("%s: upstream's active tab isn't %s now", w.id, w.tab)
		}
		modes := []bool{w.dark}
		if w.both {
			modes = []bool{false, true}
		}
		for _, dark := range modes {
			mode := map[bool]string{false: "light", true: "dark"}[dark]
			tag := fmt.Sprintf("%s (%s)", w.id, mode)
			e.closeOverlays()
			e.mode(dark)
			if err := e.s.SetTheme(""); err != nil {
				t.Fatal(err)
			}
			e.until(10*time.Second, "no theme", func() bool { return !e.s.ThemeApplies() })
			p.pickTheme(e.card(w.id))
			e.until(15*time.Second, tag+" applied", func() bool { return e.s.ThemeApplies() && e.a.th.Dark == dark })
			e.checkPrefs(sh, w, tag, "overlay-"+w.id+"-prefs-"+mode)
			e.checkMenu(sh, w, tag, "overlay-"+w.id+"-menu-"+mode)
		}
	}
}
