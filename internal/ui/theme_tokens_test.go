package ui

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"gioui.org/op"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/richview"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
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

// A sheet that paints the primary view or message pane with raw CSS
// (Material: `.p-view_contents--primary,.p-message_pane{background:
// var(--slick-material-background)}`) recolours the pane everywhere the
// pane token is read, over its --sk_primary_background.
func TestThemeTokensPrimaryViewBackground(t *testing.T) {
	eng := css.NewEngine()
	eng.Set("slick:theme", tkSlickRule("--sk_primary_background", "38,43,60", "--slick-material-background", "#1c1f2a", "--dt_color-content-hgl-1", "#899de7")+
		".p-message_pane,.p-threads_view{background:var(--slick-material-background) !important;}\n"+
		".p-view_contents--primary,.p-workspace__primary_view{background:var(--slick-material-background) !important;}\n")
	th := tkThemed(eng, true)
	rv, ok := richview.Themed(th)
	if !ok {
		t.Fatal("richview not themed")
	}
	tkCheck(t, []tkWant{
		{"Bg", th.C.Bg, tkHex(0x1c1f2a)},
		{"Link", th.C.Link, tkHex(0x899de7)},
		{"richview Panel", rv.Panel, tkHex(0x1c1f2a)},
	})

	// raw CSS alone, no root token: only the pane changes
	eng = css.NewEngine()
	eng.Set("slick:user", ".p-message_pane{background:#123456}")
	th = tkThemed(eng, false)
	if th.C.Bg != tkHex(0x123456) {
		t.Fatalf("Bg = %v", th.C.Bg)
	}
	// Flatten pre-mixes translucent fields over Bg, so those follow it
	want := theme.LightPalette
	want.Bg = tkHex(0x123456)
	if th.C != theme.Flatten(want) {
		t.Error("a pane background changed other fields")
	}
	// a lone gradient (i hate myself) has no colour: the pane keeps its token
	eng = css.NewEngine()
	eng.Set("slick:theme", tkSlickRule("--sk_primary_background", "255,241,168")+
		".p-message_pane{background:repeating-linear-gradient(0deg,#fff1a8 0 27px,#ffffc7 27px 29px) !important;}\n")
	if bg := tkThemed(eng, false).C.Bg; bg != tkHex(0xfff1a8) {
		t.Errorf("gradient pane Bg = %v", bg)
	}
}

// hotDogOverlayCSS is Hot Dog Stand's overlay part (themes/hot-dog-stand.json):
// the tokens Slack's dialogs, menus, tooltips and tabs are painted from,
// and the raw rules every bundled theme repaints cards and the active
// Preferences tab with.
var hotDogOverlayCSS = hotDogCSS + tkSlickRule(
	"--dt_color-ctr-pry", "#cc0000", "--dt_color-content-pry", "#ffff00",
	"--dt_color-content-sec", "#eeee00", "--dt_color-content-inv-pry", "#ffff00",
	"--dt_color-base-inv-pry", "#cc0000", "--dt_color-base-inv-hgl-1", "#aa0000",
	"--dt_color-base-pry-hover", "#ffff0014", "--dt_color-ctr-pry-hover", "#ffff0014",
	"--dt_color-base-modal", "#ff0000b3",
	"--sk_menu-item_highlight-background", "#aa0000", "--sk_menu-item_highlight-color", "#ffff00",
) + ".c-tabs__tab_menu--vertical .c-tabs__tab.c-tabs__tab--active,.c-tabs__tab_menu--vertical .c-tabs__tab:active{background-color:#cccc00 !important;color:#ff0000 !important;}\n" +
	"[role=menu],[role=dialog],[role=listbox],.c-menu,.p-ia4_floating_window,.c-popover__content{background-color:#cc0000 !important;border:1px solid #aa0000 !important;}\n"

// Overlays read the theme's tokens like the window does: Slack paints
// menus and popovers from --dt_color-ctr-pry (.c-menu), modals from
// --dt_color-base-pry (.c-sk-modal), the menu highlight from
// --sk_menu-item_highlight-*, tooltips from --dt_color-base-inv-pry and
// Preferences' tabs from --dt_color-base-inv-hgl-1. With no sheet they
// keep the measured colours.
func TestThemeTokensOverlaysHotDogStand(t *testing.T) {
	for _, dark := range []bool{false, true} {
		eng := css.NewEngine()
		eng.Set("slick:theme", hotDogOverlayCSS)
		th := tkThemed(eng, dark)
		cl := popups.ColorsOf(th)
		tc := prefsTabColorsOf(th)
		tkCheck(t, []tkWant{
			{"popups Bg (.c-menu)", cl.Bg, tkHex(0xcc0000)},
			{"popups ModalBg (.c-sk-modal)", cl.ModalBg, tkHex(0xff0000)},
			{"popups Fg", cl.Fg, tkHex(0xffff00)},
			{"popups Fg2", cl.Fg2, tkHex(0xeeee00)},
			{"popups Highlight", cl.Highlight, tkHex(0xaa0000)},
			{"popups HighText", cl.HighText, tkHex(0xffff00)},
			{"popups BgHover", cl.BgHover, theme.Over(color.NRGBA{255, 255, 0, 0x14}, tkHex(0xcc0000))},
			{"popups Tooltip", cl.Tooltip, tkHex(0xcc0000)},
			{"popups TooltipText", cl.TooltipText, tkHex(0xffff00)},
			{"palette Tooltip", th.C.Tooltip, tkHex(0xcc0000)},
			{"prefs tab text", tc.text, tkHex(0xffff00)},
			{"prefs active tab", tc.active, tkHex(0xaa0000)},
			{"prefs active tab text", tc.activeText, tkHex(0xffff00)},
			{"prefs tab hover", tc.hover, theme.Over(color.NRGBA{255, 255, 0, 0x14}, tkHex(0xff0000))},
		})
		if cl.Backdrop.A == 0 || cl.Backdrop.R != 0xff || cl.Backdrop.G != 0 {
			t.Errorf("dark=%v: backdrop %v, want the theme's red scrim", dark, cl.Backdrop)
		}
	}

	// no stylesheet: the measured palettes, the tabs' measured colours
	for _, dark := range []bool{false, true} {
		th := tkThemed(css.NewEngine(), dark)
		if popups.ColorsOf(th) != popups.ColorsOf(theme.Bare(dark)) {
			t.Errorf("dark=%v: popups palette themed without a sheet", dark)
		}
		bg := map[bool]color.NRGBA{false: tkHex(0xffffff), true: tkHex(0x222529)}[dark]
		if c := popups.ColorsOf(th); c.Bg != bg || c.ModalBg != bg {
			t.Errorf("dark=%v: default card %v / modal %v, want %v", dark, c.Bg, c.ModalBg, bg)
		}
		want := prefsTabColors{text: th.C.Fg, active: th.C.Link, activeText: th.C.SendText, hover: th.C.BgHover}
		if got := prefsTabColorsOf(th); got != want {
			t.Errorf("dark=%v: prefs tabs %+v, want the measured %+v", dark, got, want)
		}
	}
}

// The theme's raw rules reach the native elements Slack's DOM has them
// on: the modal (role dialog) takes the card background and border, the
// Preferences tab under .c-tabs__tab_menu--vertical its active colours.
func TestThemeOverlayRulesReachNatives(t *testing.T) {
	slackc.SetStyle("slick:theme", hotDogOverlayCSS)
	defer slackc.RemoveStyle("slick:theme")
	gtx := ttContext(new(op.Ops), image.Pt(800, 600))
	none := color.NRGBA{1, 2, 3, 255}

	st := popups.ModalClass.EnterClass(gtx, 0, 0, "p-prefs_dialog__modal")
	bg, bw := st.Background(none), st.BorderWidth()
	st.Leave()
	if bg != tkHex(0xcc0000) || bw != 1 {
		t.Errorf("modal: background %v border %v, want #cc0000 and 1px (the [role=dialog] rule)", bg, bw)
	}

	ts := clsPrefsTabs.Enter(gtx, 0, 0)
	ms := clsPrefsMenu.Enter(gtx, 0, 0)
	tab := clsPrefsTab.Enter(gtx, 0, 1) // c-tabs__tab--active
	tbg, tfg := tab.Background(none), tab.Color(none)
	tab.Leave()
	idle := clsPrefsTab.Enter(gtx, 0, 0)
	ibg := idle.Background(none)
	idle.Leave()
	ms.Leave()
	ts.Leave()
	if tbg != tkHex(0xcccc00) || tfg != tkHex(0xff0000) {
		t.Errorf("active prefs tab: background %v color %v, want #cccc00 / #ff0000", tbg, tfg)
	}
	if ibg != none {
		t.Errorf("an inactive tab took a background (%v)", ibg)
	}
}
