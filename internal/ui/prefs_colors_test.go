package ui

// The Preferences dialog's colours against Slack's: the token values in
// slack-kit-gantry-v2-styles (.sk-client-theme--light / --dark), the
// browser's native checkbox, account themes leaving them alone (none of
// them is a --dt_color-theme-* token) and a stylesheet's token overrides
// recolouring them.

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/op"

	"gioui.org/gpu/headless"
	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// pcCustomHex is a custom-hex ia_theme (Slack's red/yellow "hot dog"
// colours typed into Custom theme) with a dark, uninverted sidebar.
const pcCustomHex = `{"primary":{"palette":"aubergine","custom":"#D70000"},"highlight1":{"palette":"aubergine","custom":"#FFFF00"},"highlight2":{"palette":"jade","custom":"#00A000"},"important":{"palette":"aubergine","custom":"#FF8800"},"brightness":5,"sidebarInverted":false,"useCustomHex":true}`

func TestPrefsColorsMatchSlack(t *testing.T) {
	for _, dark := range []bool{false, true} {
		pc := prefsColorsOf(tkThemed(css.NewEngine(), dark))
		over := func(fg, bg uint32) color.NRGBA {
			return theme.Over(hex8(fg), hex8(bg))
		}
		want := []tkWant{
			// light: .sk-client-theme--light
			{"nav text (content-pry)", pc.tabText, tkHex(0x1d1c1d)},
			{"nav selected (base-inv-hgl-1)", pc.tabActive, tkHex(0x1264a3)},
			{"nav selected text (content-inv-pry)", pc.tabActiveText, tkHex(0xffffff)},
			{"nav hover (base-pry-hover over base-pry)", pc.tabHover, over(0x4544470f, 0xffffffff)},
			{"nav icon (sk_foreground_max .7)", pc.on(theme.Bare(false), pc.tabIcon), over(0x1d1c1db3, 0xffffffff)},
			{"link (content-hgl-1)", pc.link, tkHex(0x1264a3)},
			{"subtext (content-ter)", pc.subtext, tkHex(0x5e5d60)},
			{"select border (otl-pry)", pc.fieldBorder, tkHex(0x7c7a7f)},
			{"select fill (base-pry)", pc.field, tkHex(0xffffff)},
			{"primary (base-inv-hgl-2)", pc.primary, tkHex(0x007a5a)},
			{"hr (sk_foreground_low_solid)", pc.hr, tkHex(0xdddddd)},
			{"native checkbox", theme.NativeAccent(false), tkHex(0x0075ff)},
			{"surface (base-pry)", popups.BasePryOf(theme.Bare(false)), tkHex(0xffffff)},
		}
		if dark {
			want = []tkWant{
				{"nav text (content-pry)", pc.tabText, tkHex(0xf8f8f8)},
				{"nav selected (base-inv-hgl-1)", pc.tabActive, tkHex(0x0a77a7)},
				{"nav selected text (content-inv-pry)", pc.tabActiveText, tkHex(0xf8f8f8)},
				{"nav hover (base-pry-hover over base-pry)", pc.tabHover, over(0xf8f8f814, 0x1a1d21ff)},
				{"nav icon (sk_foreground_max .7)", pc.on(theme.Bare(true), pc.tabIcon), over(0xe8e8e8b3, 0x1a1d21ff)},
				{"link (content-hgl-1)", pc.link, tkHex(0x2ba5ce)},
				{"subtext (content-ter)", pc.subtext, tkHex(0x9a9b9e)},
				{"select border (otl-pry)", pc.fieldBorder, tkHex(0x797c81)},
				{"select fill (base-pry)", pc.field, tkHex(0x1a1d21)},
				{"primary (base-inv-hgl-2)", pc.primary, tkHex(0x177e56)},
				{"hr (sk_foreground_low_solid)", pc.hr, tkHex(0x35373b)},
				{"native checkbox", theme.NativeAccent(true), tkHex(0x99c8ff)},
				{"surface (base-pry)", popups.BasePryOf(theme.Bare(true)), tkHex(0x1a1d21)},
			}
		}
		t.Run(map[bool]string{false: "light", true: "dark"}[dark], func(t *testing.T) { tkCheck(t, want) })
	}
}

// Slack's account themes write only --dt_color-theme-* (getThemeVariables),
// so Aubergine, a dark uninverted sidebar and custom hexes give the same
// dialog; only Appearance's tab underline (--dt_color-theme-base-pry, Accent)
// follows them.
func TestPrefsColorsIgnoreAccountTheme(t *testing.T) {
	for _, dark := range []bool{false, true} {
		def := prefsColorsOf(tkThemed(css.NewEngine(), dark))
		for _, js := range []string{"", ochinJSON, pcCustomHex} {
			a, ca, _ := fakeApp(t, map[string]any{"ia_theme": js})
			a.th.SetDark(dark)
			a.applyAccountPrefs(ca.Prefs())
			if got := prefsColorsOf(a.th); got != def {
				t.Errorf("dark=%v ia_theme %.40q: prefs colours %+v, want Slack's untouched %+v", dark, js, got, def)
			}
		}
		a, ca, _ := fakeApp(t, map[string]any{"ia_theme": pcCustomHex})
		a.th.SetDark(dark)
		before := a.th.C.Accent
		a.applyAccountPrefs(ca.Prefs())
		if a.th.C.Accent == before {
			t.Errorf("dark=%v: the custom theme left Accent (Appearance's tab underline) at %v", dark, before)
		}
	}
}

// A stylesheet that overrides the tokens (Hot Dog Stand) recolours them,
// as in Slack.
func TestPrefsColorsFollowStylesheetTokens(t *testing.T) {
	for _, dark := range []bool{false, true} {
		eng := css.NewEngine()
		eng.Set("slick:theme", hotDogOverlayCSS+tkSlickRule("--dt_color-otl-pry", "#00ff00", "--dt_color-content-ter", "#ff00ff"))
		th := tkThemed(eng, dark)
		pc := prefsColorsOf(th)
		tkCheck(t, []tkWant{
			{"nav selected", pc.tabActive, tkHex(0xaa0000)},
			{"nav text", pc.tabText, tkHex(0xffff00)},
			{"link", pc.link, tkHex(0xffff00)},
			{"select border", pc.fieldBorder, tkHex(0x00ff00)},
			{"select fill", pc.field, tkHex(0xff0000)},
			{"subtext", pc.subtext, tkHex(0xff00ff)},
			{"surface", popups.BasePryOf(th), tkHex(0xff0000)},
		})
	}
}

// NativeCheck paints Chromium's colours: checked fill and check, the
// unchecked border and fill, sampled off the drawn box.
func TestNativeCheckPixels(t *testing.T) {
	w, err := headless.NewWindow(40, 20)
	if err != nil {
		t.Skipf("no GPU renderer: %v", err)
	}
	defer w.Release()
	for _, dark := range []bool{false, true} {
		ops := new(op.Ops)
		gtx := ttContext(ops, image.Pt(40, 20))
		theme.NativeCheck(gtx, dark, theme.NativeCheckState{On: true})
		off := op.Offset(image.Pt(20, 0)).Push(ops)
		theme.NativeCheck(gtx, dark, theme.NativeCheckState{})
		off.Pop()
		if err := w.Frame(ops); err != nil {
			t.Fatal(err)
		}
		img := image.NewRGBA(image.Rect(0, 0, 40, 20))
		if err := w.Screenshot(img); err != nil {
			t.Fatal(err)
		}
		at := func(x, y int) color.NRGBA {
			c := img.RGBAAt(x, y)
			return color.NRGBA{c.R, c.G, c.B, 0xff}
		}
		want := []tkWant{
			{"checked fill", at(6, 1), tkHex(0x0075ff)},
			{"unchecked border", at(20, 6), tkHex(0x767676)},
			{"unchecked fill", at(26, 6), tkHex(0xffffff)},
		}
		if dark {
			want = []tkWant{
				{"checked fill", at(6, 1), tkHex(0x99c8ff)},
				{"unchecked border", at(20, 6), tkHex(0x858585)},
				{"unchecked fill", at(26, 6), tkHex(0x3b3b3b)},
			}
		}
		tkCheck(t, want)
	}
}

// TestPrefsColorShots draws Preferences in light, dark, a custom-hex
// account theme (dark and light) and Hot Dog Stand, on the Availability,
// Notifications and Plugins pages, into PREFS_COLOR_SHOTS_DIR (the
// prefs-colors workflow). Synthetic data only.
func TestPrefsColorShots(t *testing.T) {
	dir := os.Getenv("PREFS_COLOR_SHOTS_DIR")
	if dir == "" {
		t.Skip("PREFS_COLOR_SHOTS_DIR unset")
	}
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	home := t.TempDir()
	t.Setenv("CLACK_HOME", home)
	e := startEnv(t, fs, home)
	if e.gpu == nil {
		w, err := headless.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	shot := func(name string) {
		e.settle(200 * time.Millisecond)
		img := image.NewRGBA(image.Rectangle{Max: e.size})
		if err := e.gpu.Screenshot(img); err != nil {
			t.Fatalf("screenshot: %v", err)
		}
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
	}
	var pm *prefsModal
	prefsTap = func(p *prefsModal) { pm = p }
	t.Cleanup(func() { prefsTap = nil })
	pages := func(variant string) {
		for _, sec := range []string{"availability", "notifications", "plugins"} {
			for e.a.dismissTop() {
			}
			pm = nil
			e.a.showPrefs(sec)
			e.until("Preferences "+sec, 5*time.Second, func() bool { return pm != nil })
			shot("prefs-colors-" + variant + "-" + sec)
		}
		for e.a.dismissTop() {
		}
	}
	for _, v := range []struct {
		name, ia string
		dark     bool
		sheet    string
	}{
		{"light", "", false, ""},
		{"dark", "", true, ""},
		{"custom-hex-light", pcCustomHex, false, ""},
		{"custom-hex-dark", pcCustomHex, true, ""},
		{"hotdog-light", "", false, hotDogOverlayCSS},
	} {
		e.a.applyAccountPrefs(core.Prefs{IATheme: v.ia})
		e.a.setDark(v.dark)
		if v.sheet != "" {
			slackc.SetStyle("slick:theme", v.sheet)
		} else {
			slackc.RemoveStyle("slick:theme")
		}
		e.settle(300 * time.Millisecond)
		pages(v.name)
	}
	slackc.RemoveStyle("slick:theme")
}
