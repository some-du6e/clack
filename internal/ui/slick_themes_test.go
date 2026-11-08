package ui

// Slick's bundled themes through the whole native window (ui-slick
// workflow): the real upstream themes/*.json (SLICK_PLUGINS_DIR/themes,
// cloned from github.com/3kh0/slick) selected the way Preferences > Slick
// > Themes selects them, over a Slack account theme (users.prefs
// ia_theme) that must lose to them, as in Slack + Slick. Each check reads
// the pixels the window drew: chrome, sidebar, header, message pane,
// composer, its border and the message text. Screenshots go to
// SLICK_SHOTS_DIR (light and dark). Synthetic data only.

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"gioui.org/gpu/headless"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/slick"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme/pixeltest"
)

// svThemeWant is what a theme's JSON says the main surfaces are ("" = the
// theme doesn't set it; checked loosely or not at all).
type svThemeWant struct {
	id                  string
	chrome, sidebar     string // .p-theme_background, .p-view_contents--sidebar
	pane, header        string // --sk_primary_background, .p-view_header
	composer, border    string // .p-message_input__input_container_unstyled
	text                string // --sk_primary_foreground
	sidebarDark         bool   // no sidebar colour: Slack's dark sidebar over the chrome (near black)
	modeless            bool   // no "mode": applies in light and dark
	composerIsPane      bool   // the composer's background is the pane's
	skipHeader, skipBox bool
	paneFromCSS         bool // the pane is a raw CSS background, not the vars'
}

// Values copied from Slick's themes/*.json (main, 2026-11-08); the test
// also re-reads the JSON so a changed upstream theme shows up as a
// mismatch, not a silent pass.
var svThemeWants = []svThemeWant{
	{id: "amoled", chrome: "#000000", sidebarDark: true, pane: "#000000", header: "#000000", border: "#2a2a2a", composerIsPane: true},
	{id: "catppuccin-mocha", chrome: "#11111b", sidebar: "#181825", pane: "#1e1e2e", header: "#1e1e2e", composer: "#181825", border: "#45475a", text: "#cdd6f4"},
	{id: "nord", chrome: "#2e3440", sidebar: "#3b4252", pane: "#2e3440", header: "#2e3440", composer: "#3b4252", border: "#4c566a", text: "#eceff4"},
	{id: "gruvbox-dark", chrome: "#1d2021", sidebar: "#32302f", pane: "#282828", header: "#282828", composer: "#32302f", border: "#504945", text: "#ebdbb2"},
	// Material paints the primary view and message pane with its own
	// --slick-material-background (raw CSS, not --sk_primary_background)
	// and floats the header and composer on lighter surfaces.
	{id: "material", paneFromCSS: true, chrome: "#1c1f2a", sidebar: "#1c1f2a", pane: "#1c1f2a", header: "#262b3c", composer: "#2e3449", text: "#dbe0e6"},
	{id: "hot-dog-stand", modeless: true, chrome: "#ff0000", sidebar: "#ffff00", pane: "#ff0000", header: "#ffff00", composer: "#cc0000", border: "#cccc00", text: "#ffff00"},
}

// svThemeJSON is the upstream theme file.
type svThemeJSON struct {
	Name string                    `json:"name"`
	Mode string                    `json:"mode"`
	Vars map[string]any            `json:"vars"`
	Side map[string]any            `json:"sidebar"`
	CSS  json.RawMessage           `json:"css"`
	Pal  map[string]map[string]any `json:"palette"`
}

func svReadTheme(t *testing.T, dir, id string) svThemeJSON {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "themes", id+".json"))
	if err != nil {
		t.Fatalf("upstream theme %s: %v", id, err)
	}
	var j svThemeJSON
	if err := json.Unmarshal(b, &j); err != nil {
		t.Fatalf("%s: %v", id, err)
	}
	return j
}

// svTriple reads Slack's "r,g,b" token form.
func svTriple(s string) (color.NRGBA, bool) {
	var r, g, b int
	if _, err := fmt.Sscanf(strings.ReplaceAll(s, " ", ""), "%d,%d,%d", &r, &g, &b); err != nil {
		return color.NRGBA{}, false
	}
	return color.NRGBA{uint8(r), uint8(g), uint8(b), 0xff}, true
}

// svPaneOf is the pane colour a theme's vars give (Slack paints the
// primary view from --sk_primary_background).
func svPaneOf(j svThemeJSON) (color.NRGBA, bool) {
	if s, ok := j.Vars["--sk_primary_background"].(string); ok {
		if c, ok := svTriple(s); ok {
			return c, true
		}
	}
	if s, ok := j.Vars["--dt_color-base-pry"].(string); ok && len(s) == 7 {
		return parseHexColor(s), true
	}
	return color.NRGBA{}, false
}

// svCopyThemes puts the upstream themes where Slick reads bundled ones.
func svCopyThemes(t *testing.T, src, home string) int {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(src, "themes", "*.json"))
	if len(files) == 0 {
		t.Fatalf("no upstream themes in %s/themes (the workflow copies Slick's themes/ there)", src)
	}
	dst := filepath.Join(home, "slick", "themes")
	if err := os.MkdirAll(dst, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, filepath.Base(f)), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return len(files)
}

// svShooter renders the env's frames into a headless GPU window.
type svShooter struct {
	e   *svEnv
	win *headless.Window
	dir string
}

func newSVShooter(t *testing.T, e *svEnv) *svShooter {
	w, err := headless.NewWindow(e.sz.X, e.sz.Y)
	if err != nil {
		if os.Getenv("CLACK_PIXEL_TESTS") != "" {
			t.Fatalf("headless GPU: %v", err)
		}
		t.Skipf("no headless GPU (%v)", err)
	}
	t.Cleanup(w.Release)
	return &svShooter{e: e, win: w, dir: os.Getenv("SLICK_SHOTS_DIR")}
}

// shot draws a frame and reads it back.
func (s *svShooter) shot() *image.RGBA {
	s.e.frame()
	if err := s.win.Frame(s.e.ops); err != nil {
		s.e.t.Fatalf("frame: %v", err)
	}
	img := image.NewRGBA(image.Rectangle{Max: s.e.sz})
	if err := s.win.Screenshot(img); err != nil {
		s.e.t.Fatalf("screenshot: %v", err)
	}
	return img
}

func (s *svShooter) save(name string, img *image.RGBA) {
	if s.dir == "" {
		return
	}
	f, err := os.Create(filepath.Join(s.dir, name+".png"))
	if err != nil {
		s.e.t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		s.e.t.Fatal(err)
	}
}

// svRegions are the window's main surfaces (1 px per dp in tests).
type svRegions struct{ chrome, sidebar, header, pane, composer image.Rectangle }

func (e *svEnv) regions() svRegions {
	navH, railW, inset := theme.TopNavH, theme.RailW, theme.PanelInset
	sbW := e.a.sidebar.width
	sz := e.sz
	px := railW + sbW // the primary view's left edge
	return svRegions{
		chrome:   image.Rect(0, navH+260, railW, sz.Y),
		sidebar:  image.Rect(railW+12, navH+90, px-12, sz.Y-inset-12),
		header:   image.Rect(px+260, navH+6, sz.X-inset-260, navH+40),
		pane:     image.Rect(px+20, navH+140, sz.X-inset-20, sz.Y-230),
		composer: image.Rect(px+8, sz.Y-inset-170, sz.X-inset-8, sz.Y-inset-4),
	}
}

// svDominant is the most common colour in r and its share.
func svDominant(img *image.RGBA, r image.Rectangle) (color.NRGBA, float64) {
	r = r.Intersect(img.Bounds())
	counts := map[[3]uint8]int{}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			p := img.RGBAAt(x, y)
			counts[[3]uint8{p.R, p.G, p.B}]++
		}
	}
	var best [3]uint8
	n := -1
	for k, v := range counts {
		if v > n || (v == n && fmt.Sprint(k) < fmt.Sprint(best)) {
			best, n = k, v
		}
	}
	if r.Empty() {
		return color.NRGBA{}, 0
	}
	return color.NRGBA{best[0], best[1], best[2], 0xff}, float64(n) / float64(r.Dx()*r.Dy())
}

func svNear(a, b color.NRGBA, tol int) bool {
	d := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	return d(a.R, b.R) <= tol && d(a.G, b.G) <= tol && d(a.B, b.B) <= tol
}

func svHex(c color.NRGBA) string { return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B) }

func svLuma(c color.NRGBA) int { return (299*int(c.R) + 587*int(c.G) + 114*int(c.B)) / 1000 }

// card is Preferences > Slick > Themes' card for id.
func (e *svEnv) card(id string) slick.ThemeCard {
	e.t.Helper()
	for _, c := range e.s.Settings().Themes {
		if c.ID == id {
			return c
		}
	}
	e.t.Fatalf("no theme card %q", id)
	return slick.ThemeCard{}
}

// mode switches light/dark the way Appearance does and waits for it.
func (e *svEnv) mode(dark bool) {
	e.t.Helper()
	e.a.pickMode(dark)
	e.until(10*time.Second, fmt.Sprintf("dark=%v", dark), func() bool { return e.a.th.Dark == dark })
}

// svThemesEnv is the shell with the upstream themes installed, an account
// theme (Ocean) set, and synthetic messages in #general.
func svThemesEnv(t *testing.T) *svEnv {
	n := 0
	e := newSV(t, func(home string, a *App, ca *core.App) {
		n = svCopyThemes(t, os.Getenv("SLICK_PLUGINS_DIR"), home)
		ca.S.SetLatest(ttGen, []slack.Message{
			{Type: "message", TS: "1700000000.000100", User: svAlice, Text: "synthetic message one: the theme should paint this pane"},
			{Type: "message", TS: "1700000060.000200", User: svBob, Text: "synthetic message two, with *bold* and a <https://example.com|link>"},
			{Type: "message", TS: "1700000120.000300", User: ttSelf, Text: "synthetic message three"},
		}, false)
		// Slack's account theme (Ocean): Slick themes must still win over it
		pr := ca.S.Prefs
		pr.IATheme = ochinJSON
		ca.S.SetPrefs(pr, nil)
		a.applyAccountPrefs(ca.Prefs())
		if !a.acct.set {
			t.Fatal("the account theme didn't apply")
		}
	})
	if got := len(e.s.Settings().Themes); got < n+2 { // + Slack's and "Custom CSS only"
		t.Fatalf("%d theme cards for %d upstream themes", got, n)
	}
	e.a.Navigate("c/" + ttGen)
	for i := 0; i < 10; i++ {
		e.frame()
	}
	return e
}

// checkSurfaces asserts what the window shows for w in the current mode.
func (e *svEnv) checkSurfaces(img *image.RGBA, w svThemeWant, tag string) {
	t := e.t
	t.Helper()
	r := e.regions()
	type m struct {
		name string
		rect image.Rectangle
		want string
	}
	got := map[string]color.NRGBA{}
	for _, x := range []m{
		{"chrome", r.chrome, w.chrome},
		{"sidebar", r.sidebar, w.sidebar},
		{"header", r.header, w.header},
		{"pane", r.pane, w.pane},
	} {
		c, share := svDominant(img, x.rect)
		got[x.name] = c
		t.Logf("%s %s: %s (%.0f%% of %v)", tag, x.name, svHex(c), share*100, x.rect)
		if x.want == "" || (x.name == "header" && w.skipHeader) {
			continue
		}
		if !svNear(c, parseHexColor(x.want), 6) {
			t.Errorf("%s: the %s is %s, the theme says %s", tag, x.name, svHex(c), x.want)
		}
	}
	if w.sidebarDark && svLuma(got["sidebar"]) > 24 {
		t.Errorf("%s: the sidebar is %s, not near black over the theme's black chrome", tag, svHex(got["sidebar"]))
	}
	// the account theme (Ocean, #1264a3 chrome) must not show through
	if svNear(got["chrome"], rgbT(0x1264a3), 12) || svNear(got["chrome"], rgbT(0x093a73), 12) {
		t.Errorf("%s: the chrome is the account theme's %s, not the Slick theme's", tag, svHex(got["chrome"]))
	}
	if !w.skipBox {
		box := w.composer
		if w.composerIsPane {
			box = w.pane
		}
		if box != "" {
			n := pixeltest.Count(img, r.composer, parseHexColor(box), 6)
			share := float64(n) / float64(r.composer.Dx()*r.composer.Dy())
			t.Logf("%s composer %s: %.0f%%", tag, box, share*100)
			if share < 0.2 {
				t.Errorf("%s: the composer isn't %s (%.0f%% of its area)", tag, box, share*100)
			}
		}
		if w.border != "" {
			n := pixeltest.Count(img, r.composer, parseHexColor(w.border), 10)
			t.Logf("%s composer border %s: %d px", tag, w.border, n)
			if n < 500 {
				t.Errorf("%s: the composer border isn't %s (%d px)", tag, w.border, n)
			}
		}
	}
	fg := e.a.th.C.Fg
	if w.text != "" {
		fg = parseHexColor(w.text)
		if !svNear(e.a.th.C.Fg, fg, 2) {
			t.Errorf("%s: text colour %s, the theme says %s", tag, svHex(e.a.th.C.Fg), w.text)
		}
	}
	if n := pixeltest.Ink(img, r.pane, fg, got["pane"]); n < 40 {
		t.Errorf("%s: no %s message text on the pane (%d px)", tag, svHex(fg), n)
	}
}

// TestSlickViewThemes selects Slick's built-in themes the way Preferences
// does and checks the whole window takes their colours, in light and dark.
// A dark-only theme (Slick's "Only in dark mode") picked in light mode
// switches Clack to dark, and stays selected across a restart.
func TestSlickViewThemes(t *testing.T) {
	e := svThemesEnv(t)
	sh := newSVShooter(t, e)
	p := newSlickPage(e.a)
	defer p.close()

	for _, w := range svThemeWants {
		j := svReadTheme(t, e.dir, w.id)
		if pane, ok := svPaneOf(j); ok && !w.paneFromCSS && !svNear(pane, parseHexColor(w.pane), 1) {
			t.Errorf("%s: upstream pane is %s now, the test expects %s", w.id, svHex(pane), w.pane)
		}
		if (j.Mode == "") != w.modeless {
			t.Errorf("%s: upstream mode is %q", w.id, j.Mode)
		}
		for _, startDark := range []bool{false, true} {
			tag := fmt.Sprintf("%s (picked in %s)", w.id, map[bool]string{false: "light", true: "dark"}[startDark])
			e.mode(startDark)
			// Slack's own first, so every pick is a change
			if err := e.s.SetTheme(""); err != nil {
				t.Fatal(err)
			}
			e.until(10*time.Second, "no theme", func() bool { return !e.s.ThemeApplies() })
			p.pickTheme(e.card(w.id))
			wantDark := startDark
			if !w.modeless {
				wantDark = j.Mode == "dark"
			}
			pane := parseHexColor(w.pane)
			e.until(15*time.Second, tag+" applied", func() bool {
				return e.s.ThemeApplies() && e.a.th.Dark == wantDark && e.a.th.C.Bg == pane
			})
			if !w.modeless && startDark != wantDark && !strings.Contains(p.themeStatus, "switched") {
				t.Errorf("%s: no status for the mode switch (%q)", tag, p.themeStatus)
			}
			img := sh.shot()
			sh.save(fmt.Sprintf("theme-%s-%s", w.id, map[bool]string{false: "light", true: "dark"}[e.a.th.Dark]), img)
			e.checkSurfaces(img, w, tag)
		}
		if !w.modeless {
			// back to the other mode: the theme is off, as in Slick
			other := j.Mode != "dark"
			e.mode(other)
			e.until(10*time.Second, w.id+" off", func() bool { return !e.s.ThemeApplies() })
			img := sh.shot()
			sh.save(fmt.Sprintf("theme-%s-%s-off", w.id, map[bool]string{false: "light", true: "dark"}[other]), img)
			if c, _ := svDominant(img, e.regions().pane); svNear(c, parseHexColor(w.pane), 4) {
				t.Errorf("%s: still themed in %v mode (%s)", w.id, other, svHex(c))
			}
		}
	}

	// The selection is saved and applies at the next start.
	e.mode(true)
	p.pickTheme(e.card("amoled"))
	e.until(15*time.Second, "amoled", func() bool { return e.s.ThemeApplies() && e.a.th.C.Bg == rgbT(0) })
	e.a.data.stopTaut()
	e.frame()
	if e.a.th.C.Bg == rgbT(0) {
		t.Error("the theme stayed after Slick closed")
	}
	e.a.data.startTaut(e.a)
	if e.s = e.a.Slick(); e.s == nil {
		t.Fatal("slick didn't restart")
	}
	e.until(15*time.Second, "amoled after a restart", func() bool { return e.s.ThemeApplies() && e.a.th.C.Bg == rgbT(0) })
	img := sh.shot()
	sh.save("theme-amoled-dark-restarted", img)
	e.checkSurfaces(img, svThemeWants[0], "amoled (restarted)")
}

// TestSlickViewEveryTheme applies every upstream theme in each mode it is
// made for and checks the palette and the drawn pane, sidebar and chrome
// follow its JSON: any Slick theme, not a list of special cases.
func TestSlickViewEveryTheme(t *testing.T) {
	e := svThemesEnv(t)
	sh := newSVShooter(t, e)
	files, _ := filepath.Glob(filepath.Join(e.dir, "themes", "*.json"))
	var ids []string
	for _, f := range files {
		ids = append(ids, strings.TrimSuffix(filepath.Base(f), ".json"))
	}
	sort.Strings(ids)
	for _, id := range ids {
		j := svReadTheme(t, e.dir, id)
		modes := []bool{true}
		switch j.Mode {
		case "":
			modes = []bool{false, true}
		case "light":
			modes = []bool{false}
		}
		for _, dark := range modes {
			tag := fmt.Sprintf("%s/%s", id, map[bool]string{false: "light", true: "dark"}[dark])
			e.mode(dark)
			if err := e.s.SetTheme(id); err != nil {
				t.Fatal(err)
			}
			pane, hasPane := svPaneOf(j)
			e.until(15*time.Second, tag, func() bool { return e.s.ThemeApplies() && e.a.th.Tokens != nil })
			for i := 0; i < 3; i++ {
				e.frame()
			}
			img := sh.shot()
			sh.save("every-"+strings.ReplaceAll(tag, "/", "-"), img)
			r := e.regions()
			for _, x := range []struct {
				name string
				rect image.Rectangle
			}{{"chrome", r.chrome}, {"sidebar", r.sidebar}, {"header", r.header}, {"pane", r.pane}, {"composer", r.composer}} {
				c, share := svDominant(img, x.rect)
				t.Logf("%s %s: %s (%.0f%%)", tag, x.name, svHex(c), share*100)
			}
			// nothing on screen is Slack's unthemed pane any more
			def := theme.Flatten(theme.LightPalette).Bg
			if dark {
				def = theme.Flatten(theme.DarkPalette).Bg
			}
			if c, _ := svDominant(img, r.pane); hasPane && svNear(c, def, 2) && !svNear(pane, def, 2) {
				t.Errorf("%s: the message pane is Slack's own %s, not the theme's %s", tag, svHex(c), svHex(pane))
			}
			if c, _ := svDominant(img, r.chrome); svNear(c, rgbT(0x1264a3), 12) || svNear(c, rgbT(0x093a73), 12) {
				t.Errorf("%s: the window chrome is the account theme's %s", tag, svHex(c))
			}
		}
	}
}
