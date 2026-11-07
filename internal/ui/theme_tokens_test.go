package ui

import (
	"image/color"
	"strings"
	"testing"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/richview"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc/css"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// slickRule is Slick's theme rule (internal/slick/themes.go, theme.ts).
func tkSlickRule(vars ...string) string {
	var b strings.Builder
	b.WriteString(":root,html,body,.sk-client-theme--dark,.sk-client-theme--light{")
	for i := 0; i+1 < len(vars); i += 2 {
		b.WriteString(vars[i] + ":" + vars[i+1] + " !important;")
	}
	b.WriteString("}\n")
	return b.String()
}

// Values below are copied from Slick's bundled themes (themes/*.json).
var (
	nordCSS = tkSlickRule(
		"--dt_color-plt-aubergine-0", "46,52,64", "--dt_color-plt-aubergine-10", "46,52,64",
		"--p-team_sidebar__nav-bg", "#3b4252", "--p-team_sidebar__text-color", "#eceff4",
		"--dt_color-base-pry", "#2e3440", "--sk_primary_background", "46,52,64",
		"--sk_primary_foreground", "236,239,244", "--dt_color-content-sec", "#e5e9f0",
		"--dt_color-content-hgl-1", "#88c0d0", "--sk_foreground_min_solid", "59,66,82",
		"--dt_color-ctr-pry", "#3b4252", "--dt_color-theme-base-pry", "#81a1c1",
		"--dt_color-theme-content-inv-sec", "rgba(229,233,240,0.8)",
		"--TextyTheme-send-bg", "#a3be8c", "--TextyTheme-send-fg", "#2e3440",
	) + ".p-theme_background{background:#2e3440 !important;}\n" +
		".p-theme_background:after{backdrop-filter:none !important;background:#2e3440 !important;}\n" +
		".p-view_contents--sidebar{background:#3b4252 !important;}\n"

	mochaCSS = tkSlickRule(
		"--dt_color-plt-aubergine-0", "17,17,27", "--dt_color-plt-aubergine-10", "17,17,27",
		"--dt_color-base-pry", "#1e1e2e", "--sk_primary_background", "30,30,46",
		"--sk_primary_foreground", "205,214,244", "--dt_color-content-sec", "#bac2de",
		"--dt_color-content-hgl-1", "#89b4fa", "--sk_foreground_min_solid", "36,36,54",
		"--dt_color-ctr-pry", "#181825", "--dt_color-theme-base-pry", "#cba6f7",
		"--dt_color-theme-base-hgl-1", "#38324b", "--dt_color-theme-content-hgl-1", "#cba6f7",
		"--TextyTheme-send-bg", "#a6e3a1", "--TextyTheme-send-fg", "#11111b",
	) + ".p-theme_background{background:#11111b !important;}\n" +
		".p-view_contents--sidebar{background:#181825 !important;}\n"

	hotDogCSS = tkSlickRule(
		"--dt_color-plt-aubergine-0", "255,0,0", "--dt_color-plt-aubergine-10", "255,0,0",
		"--dt_color-base-pry", "#ff0000", "--sk_primary_background", "255,0,0",
		"--sk_primary_foreground", "255,255,0", "--dt_color-content-hgl-1", "#ffff00",
		"--dt_color-theme-content-inv-sec", "rgba(238,238,0,0.8)",
	) + ".p-theme_background{background:#ff0000 !important;}\n" +
		".p-view_contents--sidebar,.p-view_header,.p-outbox_page__tab-menu{--dt_color-theme-content-inv-pry:#aa0000 !important;--dt_color-theme-content-inv-sec:#cc0000 !important;background:#ffff00 !important;color:#aa0000 !important;}\n"
)

// themed builds a theme whose Restyle reads eng (the app uses slackc.Styles).
func tkThemed(eng *css.Engine, dark bool) *theme.Theme {
	th := theme.Bare(dark)
	th.Restyle = func(t *theme.Theme, p *theme.Palette) {
		t.Tokens = nil
		if !eng.Empty() {
			restylePalette(eng, t, p)
		}
	}
	th.Refresh()
	return th
}

func tkHex(v uint32) color.NRGBA {
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
}

type tkWant struct {
	name string
	got  color.NRGBA
	want color.NRGBA
}

func tkCheck(t *testing.T, ws []tkWant) {
	t.Helper()
	for _, w := range ws {
		if w.got != w.want {
			t.Errorf("%s = %v, want %v", w.name, w.got, w.want)
		}
	}
}

func TestThemeTokensNoSheetsUnchanged(t *testing.T) {
	for _, dark := range []bool{false, true} {
		def := theme.Flatten(theme.LightPalette)
		if dark {
			def = theme.Flatten(theme.DarkPalette)
		}
		eng := css.NewEngine()
		th := tkThemed(eng, dark)
		if th.C != def || th.Tokens != nil {
			t.Fatalf("dark=%v: no stylesheet changed the palette", dark)
		}
		// plugin CSS that sets no root token leaves it alone too
		eng.Set("plugin", ".c-message_kit__message{color:red}\n.p-channel_sidebar__name{font-weight:900}")
		gen := th.Gen
		th.Refresh()
		if th.C != def || th.Tokens != nil || th.Gen != gen {
			t.Fatalf("dark=%v: unrelated CSS changed the palette", dark)
		}
		if _, ok := richview.Themed(th); ok {
			t.Fatalf("dark=%v: richview themed without tokens", dark)
		}
	}
	// the real hook with the (empty) global engine
	th := theme.Bare(true)
	th.Restyle = restyleFromSheets
	th.Refresh()
	if th.C != theme.Flatten(theme.DarkPalette) {
		t.Fatal("restyleFromSheets changed the default palette")
	}
}

func TestThemeTokensNord(t *testing.T) {
	eng := css.NewEngine()
	eng.Set("slick:theme", nordCSS)
	th := tkThemed(eng, true)
	c := th.C
	tkCheck(t, []tkWant{
		{"Bg", c.Bg, tkHex(0x2e3440)},
		{"ChromeA", c.ChromeA, tkHex(0x2e3440)},
		{"ChromeB", c.ChromeB, tkHex(0x2e3440)},
		{"SidebarBg", c.SidebarBg, tkHex(0x3b4252)},
		{"SidebarText", c.SidebarText, color.NRGBA{229, 233, 240, 204}},
		{"Fg", c.Fg, color.NRGBA{236, 239, 244, 255}},
		{"FgMuted", c.FgMuted, tkHex(0xe5e9f0)},
		{"Link", c.Link, tkHex(0x88c0d0)},
		{"BgHover", c.BgHover, color.NRGBA{59, 66, 82, 255}},
		{"BgRaised", c.BgRaised, tkHex(0x3b4252)},
		{"Accent", c.Accent, tkHex(0x81a1c1)},
		{"Send", c.Send, tkHex(0xa3be8c)},
		{"SendText", c.SendText, tkHex(0x2e3440)},
		// untouched by Nord: Slack's measured value stays
		{"Presence", c.Presence, theme.DarkPalette.Presence},
	})
	if th.Tokens == nil {
		t.Fatal("theme.Tokens not set")
	}
	rv, ok := richview.Themed(th)
	if !ok {
		t.Fatal("richview not themed")
	}
	tkCheck(t, []tkWant{
		{"richview Text", rv.Text, color.NRGBA{236, 239, 244, 255}},
		{"richview Link", rv.Link, tkHex(0x88c0d0)},
		{"richview Panel", rv.Panel, tkHex(0x2e3440)},
	})

	// light/dark switch re-derives (the rule also matches
	// .sk-client-theme--light; Slick itself drops a dark-only sheet)
	th.SetDark(false)
	if th.C.Bg != tkHex(0x2e3440) {
		t.Errorf("light Bg = %v", th.C.Bg)
	}
	eng.Remove("slick:theme")
	th.Refresh()
	if th.C != theme.Flatten(theme.LightPalette) || th.Tokens != nil {
		t.Error("removing the theme didn't restore the light palette")
	}
}

func TestThemeTokensCatppuccinMocha(t *testing.T) {
	eng := css.NewEngine()
	eng.Set("slick:theme", mochaCSS)
	c := tkThemed(eng, true).C
	tkCheck(t, []tkWant{
		{"Bg", c.Bg, color.NRGBA{30, 30, 46, 255}},
		{"ChromeA", c.ChromeA, tkHex(0x11111b)},
		{"SidebarBg", c.SidebarBg, tkHex(0x181825)},
		{"SidebarActive", c.SidebarActive, tkHex(0x38324b)},
		{"SidebarActiveText", c.SidebarActiveText, tkHex(0xcba6f7)},
		{"Fg", c.Fg, color.NRGBA{205, 214, 244, 255}},
		{"Link", c.Link, tkHex(0x89b4fa)},
		{"BgHover", c.BgHover, color.NRGBA{36, 36, 54, 255}},
		{"Send", c.Send, tkHex(0xa6e3a1)},
	})
}

func TestThemeTokensHotDogStand(t *testing.T) {
	for _, dark := range []bool{false, true} {
		eng := css.NewEngine()
		eng.Set("slick:theme", hotDogCSS)
		c := tkThemed(eng, dark).C
		tkCheck(t, []tkWant{
			{"Bg", c.Bg, tkHex(0xff0000)},
			{"ChromeA", c.ChromeA, tkHex(0xff0000)},
			// sidebar: yellow with the theme's dark-red text, not red on aubergine
			{"SidebarBg", c.SidebarBg, tkHex(0xffff00)},
			{"SidebarText", c.SidebarText, tkHex(0xcc0000)},
			{"SidebarTextUnread", c.SidebarTextUnread, tkHex(0xaa0000)},
			{"Link", c.Link, tkHex(0xffff00)},
		})
	}
}

func TestThemeTokensUserCSS(t *testing.T) {
	eng := css.NewEngine()
	eng.Set("slick:theme", nordCSS)
	// user.css after the theme, same selector: it wins
	eng.Set("slick:user", tkSlickRule("--sk_primary_background", "255,0,255", "--dt_color-content-hgl-1", "#00ff00"))
	c := tkThemed(eng, true).C
	tkCheck(t, []tkWant{
		{"Bg", c.Bg, tkHex(0xff00ff)},
		{"Link", c.Link, tkHex(0x00ff00)},
		{"Fg", c.Fg, color.NRGBA{236, 239, 244, 255}}, // still Nord's
	})

	// user.css alone: only what it sets changes
	eng = css.NewEngine()
	eng.Set("slick:user", ":root{--dt_color-content-hgl-1:#00ff00}")
	c = tkThemed(eng, true).C
	def := theme.Flatten(theme.DarkPalette)
	if c.Link != tkHex(0x00ff00) {
		t.Errorf("Link = %v", c.Link)
	}
	c.Link = def.Link
	c.Mention = def.Mention // same token (.c-member_slug--link)
	if c != def {
		t.Error("user.css changed fields it doesn't touch")
	}
}
