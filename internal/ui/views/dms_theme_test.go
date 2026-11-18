package views

import (
	"fmt"
	"image/color"
	"testing"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// themed is the mode's theme with ia_theme ia under its palette, as the
// ui's restyle lays it.
func themed(ia string, dark bool) *theme.Theme {
	t, _ := theme.ParseIATheme(ia)
	th := theme.Bare(dark)
	th.Restyle = func(th *theme.Theme, p *theme.Palette) { theme.ApplyIATheme(p, t, th.Dark, true) }
	th.Refresh()
	return th
}

func hexA(c color.NRGBA) string { return fmt.Sprintf("#%02x%02x%02x%02x", c.R, c.G, c.B, c.A) }

const (
	pickerIA = `{"primary":{"palette":"aubergine","custom":"#FED4BE"},"highlight1":{"palette":"aubergine"},"highlight2":{"palette":"jade"},"important":{"palette":"aubergine"},"brightness":2,"sidebarInverted":false,"useCustomHex":true}`
	importIA = `{"primary":{"palette":"aubergine","custom":"#FED4BE"},"highlight1":{"palette":"aubergine","custom":"#611F69"},"highlight2":{"palette":"jade","custom":"#2EB67D"},"important":{"palette":"aubergine","custom":"#E01E5A"},"brightness":6,"sidebarInverted":false,"useCustomHex":true}`
	ochinIA  = `{"primary":{"palette":"ocean"},"highlight1":{"palette":"ocean"},"highlight2":{"palette":"cilantro"},"important":{"palette":"mojito"},"brightness":5,"sidebarInverted":false,"useCustomHex":false}`
)

// The Direct messages list sits in the sidebar's theme scope: Slack's
// .p-dms_list rows are p-activity_ia4_page__items, theme-content-inv-pry
// names and read and unread messages, theme-content-inv-sec timestamps
// (.p-dms_channel__timestamp) and meta, theme-content-inv-ter a muted
// conversation's timestamp, theme-surf-inv-ter --selected, theme-otl-inv-pry
// rules, and its search box a themed c-filter_input (theme-surf-inv-sec).
// With a light sidebar (Karim's #FED4BE as Slack's picker stores it:
// brightness 2, so nothing inverts) that is the custom colour's dark ink,
// not the default theme's inverted white. Values: Slack's tokens for the
// sidebar's scope (theme.TestThemeInkMatchesSlack's table, from Slack's
// code).
func TestDMsListFollowsSidebarScope(t *testing.T) {
	hx := func(v uint32) color.NRGBA { return rgb(v) }
	hxa := func(v uint32, a uint8) color.NRGBA { c := rgb(v); c.A = a; return c }
	for _, c := range []struct {
		name, ia                              string
		dark                                  bool
		white, soft, tint, rule, inputBg, ter color.NRGBA
		preview, time                         color.NRGBA // a read message, a timestamp
	}{
		{"karim picker light", pickerIA, false,
			hx(0x1c0901), hxa(0x1c0901, 0xcc), hxa(0x41281a, 0x21), hxa(0x41281a, 0x21), hxa(0xffffff, 0xe6), hxa(0x1c0901, 0x80),
			hx(0x1c0901), hxa(0x1c0901, 0xcc)},
		{"ochin light", ochinIA, false,
			hx(0x122a59), hxa(0x122a59, 0xcc), hxa(0x0b4c8c, 0x21), hxa(0x0b4c8c, 0x21), hxa(0xffffff, 0xe6), hxa(0x122a59, 0x80),
			hx(0x122a59), hxa(0x122a59, 0xcc)},
		{"karim picker dark", pickerIA, true,
			hx(0xf8f8f8), hxa(0xffdfcf, 0xcc), hxa(0xfff4f0, 0x14), hxa(0xfff4f0, 0x38), hxa(0x101214, 0x99), hxa(0xffdfcf, 0x99),
			hx(0xf8f8f8), hxa(0xffdfcf, 0xcc)},
		{"default light", ``, false,
			hx(0xffffff), hxa(0xf6e4ff, 0xe6), hxa(0xf9edff, 0x14), hxa(0xf9edff, 0x38), hxa(0xf9edff, 0x1c), hxa(0xf9edff, 0x99),
			hx(0xffffff), hxa(0xf6e4ff, 0xe6)},
		{"default dark", ``, true,
			hx(0xf8f8f8), hxa(0xe3ceeb, 0xcc), hxa(0xefe1f5, 0x14), hxa(0xefe1f5, 0x38), hxa(0x101214, 0x99), hxa(0xe3ceeb, 0x99),
			hx(0xf8f8f8), hxa(0xe3ceeb, 0xcc)},
	} {
		t.Run(c.name, func(t *testing.T) {
			th := themed(c.ia, c.dark)
			d := &DMs{}
			d.th = th
			d.dmsColors()
			// the column: the gradient's middle under the sidebar's tint,
			// every translucent colour flattened over it (dmsColors)
			base := flatten(th.C.SidebarBg, theme.Mix(th.C.ChromeA, th.C.ChromeB, .5))
			for _, f := range []struct {
				what      string
				got, want color.NRGBA
			}{
				{"names, unread messages (theme-content-inv-pry)", d.c.white, c.white},
				{"read messages (theme-content-inv-pry)", d.c.preview, flatten(c.preview, base)},
				{"meta (theme-content-inv-sec)", d.c.soft, flatten(c.soft, base)},
				{"timestamps (theme-content-inv-sec)", d.c.time, flatten(c.time, base)},
				{"muted timestamps (theme-content-inv-ter)", d.c.ter, flatten(c.ter, base)},
				{"selection (theme-surf-inv-ter)", d.c.tint, flatten(c.tint, base)},
				{"rules (theme-otl-inv-pry)", d.c.rule, flatten(c.rule, base)},
				{"search box (theme-surf-inv-sec)", d.c.inputBg, flatten(c.inputBg, base)},
			} {
				if f.got != f.want {
					t.Errorf("%s = %s, Slack %s", f.what, hexA(f.got), hexA(f.want))
				}
			}
		})
	}
}

// A row's fills: --selected is theme-surf-inv-ter in both modes; :hover is
// too in light mode, and transparent in dark mode, where DmsList's
// p-dms_list--dark (clientTheme dark) has
// .p-dms_list--dark .p-activity_ia4_page__item:hover:not(--selected,…)
// {background-color:#0000}. On the peek card that rule meets
// .p-peek_card .p-activity_ia4_page__item:hover (base-pry under the
// base-pry-hover image), whose image it leaves: the card keeps a hover in
// dark mode too, #f8f8f8 at .08 over its #212428 (#323538); the presence
// ring's cutout over it is TestDMsRowsDrawn's.
func TestDMsListHoverByMode(t *testing.T) {
	for _, dark := range []bool{false, true} {
		d := &DMs{}
		d.th = themed("", dark)
		d.peeking = true
		d.dmsColors()
		bg, hov := d.rowTints(false)
		want := flatten(argb(0x0f454447), rgb(0xffffff)) // base-pry #fff under #4544470f
		if dark {
			want = rgb(0x323538)
		}
		if bg.A != 0 || hov != want {
			t.Errorf("peek card dark=%v: row fill %s, hover %s; Slack transparent, hover %s", dark, hexA(bg), hexA(hov), hexA(want))
		}
	}
	for _, dark := range []bool{false, true} {
		for _, ia := range []string{"", pickerIA} {
			d := &DMs{}
			d.th = themed(ia, dark)
			d.dmsColors()
			bg, hov := d.rowTints(false)
			sbg, shov := d.rowTints(true)
			wantHov := d.c.tint
			if dark {
				wantHov = color.NRGBA{}
			}
			if bg.A != 0 || hov != wantHov {
				t.Errorf("dark=%v %q: unselected row fill %s, hover %s; Slack transparent, hover %s", dark, ia[:min(len(ia), 30)], hexA(bg), hexA(hov), hexA(wantHov))
			}
			if sbg != d.c.tint || shov != d.c.tint {
				t.Errorf("dark=%v %q: selected row fill %s, hover %s; Slack theme-surf-inv-ter %s", dark, ia[:min(len(ia), 30)], hexA(sbg), hexA(shov), hexA(d.c.tint))
			}
		}
	}
}

// Activity's unread dots and count pills (and the tab strips' dots) are
// .c-badge--themed / theme-base-imp with theme-content-imp in the main
// pane: Slack's important role, a custom hex as is (Karim's import draws
// #e01e5a with #fff4f4, Ochin mojito), never the primary's ramp (the brown
// #573c2c Clack drew for Karim). Values: Slack's root-scope tokens.
func TestActivityBadgesFollowImportant(t *testing.T) {
	hx := func(v uint32) color.NRGBA { return rgb(v) }
	for _, c := range []struct {
		name, ia  string
		dark      bool
		fill, ink color.NRGBA
	}{
		{"karim import light", importIA, false, hx(0xe01e5a), hx(0xfff4f4)},
		{"karim import dark", importIA, true, hx(0xe01e5a), hx(0xfff4f4)},
		{"karim picker light", pickerIA, false, hx(0x83388a), hx(0xf9edff)},
		{"karim picker dark", pickerIA, true, hx(0xcfa1de), hx(0x200221)},
		{"ochin light", ochinIA, false, hx(0x1a8343), hx(0xe9ffec)},
		{"default light", ``, false, hx(0x83388a), hx(0xf9edff)},
		{"default dark", ``, true, hx(0xcfa1de), hx(0x200221)},
	} {
		p := palOf(themed(c.ia, c.dark))
		if p.Badge != c.fill || p.Accent != c.fill || p.AccentText != c.ink {
			t.Errorf("%s: Activity's badge %s / dot %s with %s; Slack theme-base-imp %s with theme-content-imp %s",
				c.name, hexA(p.Badge), hexA(p.Accent), hexA(p.AccentText), hexA(c.fill), hexA(c.ink))
		}
	}
}

// The DMs peek card (.p-peek_card--dms: DmsListAll in PeekView's portal
// under document.body, outside .p-ia4_client) paints in the root scope:
// timestamps .p-dms_channel__timestamp theme-content-inv-sec, a muted
// conversation's --muted theme-content-inv-ter, a row's border-bottom
// theme-otl-inv-pry, all from the account theme's primary (Karim's peach
// ramp, Ochin's navy), over the card's ctr-pry; and the rest as outside
// .p-ia4_client: the previews content-pry (--read, --unread), content-sec
// (--muted, and every row's with activity_inbox: the item's colour), the
// Unreads toggle content-sec, content-pry on. Values: Slack's tokens
// (testdata/theme-oracle.json's root scope, kit.css).
func TestDMsPeekCardTokens(t *testing.T) {
	hx := func(v uint32) color.NRGBA { return rgb(v) }
	hxa := func(v uint32, a uint8) color.NRGBA { c := rgb(v); c.A = a; return c }
	for _, c := range []struct {
		name, ia                          string
		dark                              bool
		bg, fg, sec, invSec, invTer, rule color.NRGBA
		baseImp, contImp                  color.NRGBA
	}{
		{"default light", ``, false, hx(0xffffff), hx(0x1d1c1d), hx(0x454447), hxa(0x39063a, 0xcc), hxa(0x39063a, 0x80), hxa(0x611f69, 0x21), hx(0x83388a), hx(0xf9edff)},
		{"default dark", ``, true, hx(0x212428), hx(0xf8f8f8), hx(0xb9babd), hxa(0xe3ceeb, 0xcc), hxa(0xe3ceeb, 0x99), hxa(0xefe1f5, 0x38), hx(0xcfa1de), hx(0x200221)},
		{"karim import light", importIA, false, hx(0xffffff), hx(0x1d1c1d), hx(0x454447), hxa(0x1c0901, 0xcc), hxa(0x1c0901, 0x80), hxa(0x41281a, 0x21), hx(0xe01e5a), hx(0xfff4f4)},
		{"karim picker dark", pickerIA, true, hx(0x212428), hx(0xf8f8f8), hx(0xb9babd), hxa(0xffdfcf, 0xcc), hxa(0xffdfcf, 0x99), hxa(0xfff4f0, 0x38), hx(0xcfa1de), hx(0x200221)},
		{"ochin light", ochinIA, false, hx(0xffffff), hx(0x1d1c1d), hx(0x454447), hxa(0x122a59, 0xcc), hxa(0x122a59, 0x80), hxa(0x0b4c8c, 0x21), hx(0x1a8343), hx(0xe9ffec)},
	} {
		th := themed(c.ia, c.dark)
		pp := peekPalOf(th)
		lOff, eOff, kOff, fOff := peekToggleInk(pp, false)
		lOn, eOn, kOn, fOn := peekToggleInk(pp, true)
		d := &DMs{}
		d.th = th
		d.peeking = true
		d.dmsColors()
		for _, f := range []struct {
			what      string
			got, want color.NRGBA
		}{
			{"card (ctr-pry)", pp.bg, c.bg},
			{"timestamps (theme-content-inv-sec)", pp.invSec, c.invSec},
			{"muted timestamps (theme-content-inv-ter)", pp.invTer, c.invTer},
			{"row border (theme-otl-inv-pry)", pp.rule, c.rule},
			{"the card's timestamp", d.c.time, flatten(c.invSec, c.bg)},
			{"the card's muted timestamp", d.c.ter, flatten(c.invTer, c.bg)},
			{"the card's row rule", d.c.rule, flatten(c.rule, c.bg)},
			{"read and unread previews (--read, --unread: content-pry)", d.c.preview, c.fg},
			{"unread preview", d.c.white, c.fg},
			{"muted preview (--muted: content-sec)", d.c.soft, c.sec},
			{"activity_inbox preview (the item's content-sec)", d.c.item, c.sec},
			{"Unreads toggle label, off (content-sec)", lOff, c.sec},
			{"its border, off", eOff, c.sec},
			{"its handle, off", kOff, c.sec},
			{"Unreads toggle label, on (content-pry)", lOn, c.fg},
			{"its fill, on (theme-base-imp)", fOn, c.baseImp},
			{"its border, on (theme-content-imp)", eOn, c.contImp},
			{"its handle, on", kOn, c.contImp},
		} {
			if f.got != f.want {
				t.Errorf("%s: %s = %s, Slack %s", c.name, f.what, hexA(f.got), hexA(f.want))
			}
		}
		if fOff.A != 0 {
			t.Errorf("%s: the toggle has a fill while off (%s)", c.name, hexA(fOff))
		}
		if d.c.ter.A == 0 {
			t.Errorf("%s: the card's muted timestamp is invisible", c.name)
		}
	}
}

// cascadeTokens are a stylesheet's tokens: what each expression resolves
// to through its var() chain, and the custom properties the sheet
// declares itself (theme.Declarer).
type cascadeTokens struct {
	vals     map[string]color.NRGBA // expression -> colour (own through the chain)
	declared map[string]bool
}

func (c cascadeTokens) Color(expr string) (color.NRGBA, bool, bool) {
	v, ok := c.vals[expr]
	return v, ok, ok
}
func (c cascadeTokens) Overridden() bool          { return true }
func (c cascadeTokens) Key() uint64               { return uint64(len(c.vals))*31 + uint64(len(c.declared)) }
func (c cascadeTokens) Declares(name string) bool { return c.declared[name] }

// The cascade is per variable, in the views kit and on the peek card: a
// stylesheet keeps a token where it declares it itself, and only there.
// One declaring only theme-base-imp keeps the account's theme-content-imp
// (Karim's import in dark mode: #fff4f4 numerals); one overriding only a
// ramp (Slick's AMOLED: the aubergine stops, which Clack's defaults spell
// the theme tokens on) reaches none of an account theme's tokens, whose
// Theme stylesheet spells them on its own ramp, but does reach Slack's
// default theme's. A sheet declaring theme-content-inv-ter beats the
// account's on the card.
func TestThemeTokensCascadePerVariable(t *testing.T) {
	hx := func(v uint32) color.NRGBA { return rgb(v) }
	baseImp, contImp := "var(--dt_color-theme-base-imp)", "var(--dt_color-theme-content-imp)"
	only := cascadeTokens{vals: map[string]color.NRGBA{baseImp: hx(0x3b4252)}, declared: map[string]bool{"--dt_color-theme-base-imp": true}}
	both := cascadeTokens{vals: map[string]color.NRGBA{baseImp: hx(0x3b4252), contImp: hx(0xeceff4)},
		declared: map[string]bool{"--dt_color-theme-base-imp": true, "--dt_color-theme-content-imp": true}}
	amoled := cascadeTokens{vals: map[string]color.NRGBA{baseImp: hx(0x000000), contImp: hx(0x000000)},
		declared: map[string]bool{"--dt_color-plt-aubergine-0": true, "--dt_color-plt-aubergine-10": true}}
	ter := cascadeTokens{vals: map[string]color.NRGBA{"var(--dt_color-theme-content-inv-ter)": hx(0x123456)},
		declared: map[string]bool{"--dt_color-theme-content-inv-ter": true}}
	for _, c := range []struct {
		name, ia             string
		tk                   cascadeTokens
		badge, badgeText     color.NRGBA // the views kit's (palOf)
		peekImp, peekImpText color.NRGBA // the card's
	}{
		{"import, sheet declares base-imp only", importIA, only, hx(0x3b4252), hx(0xfff4f4), hx(0x3b4252), hx(0xfff4f4)},
		{"import, sheet declares both", importIA, both, hx(0x3b4252), hx(0xeceff4), hx(0x3b4252), hx(0xeceff4)},
		{"import, AMOLED's ramp only", importIA, amoled, hx(0xe01e5a), hx(0xfff4f4), hx(0xe01e5a), hx(0xfff4f4)},
		{"default theme, AMOLED's ramp only", ``, amoled, hx(0x000000), hx(0x000000), hx(0x000000), hx(0x000000)},
	} {
		th := themed(c.ia, true)
		th.Tokens = c.tk
		p := palOf(th)
		if p.Badge != c.badge || p.Accent != c.badge || p.AccentText != c.badgeText {
			t.Errorf("%s: the kit's badge %s / dot %s with %s; Slack's cascade %s with %s", c.name, hexA(p.Badge), hexA(p.Accent), hexA(p.AccentText), hexA(c.badge), hexA(c.badgeText))
		}
		pp := peekPalOf(th)
		if pp.baseImp != c.peekImp || pp.contImp != c.peekImpText {
			t.Errorf("%s: the card's badge %s with %s; Slack's cascade %s with %s", c.name, hexA(pp.baseImp), hexA(pp.contImp), hexA(c.peekImp), hexA(c.peekImpText))
		}
	}
	th := themed(importIA, true)
	th.Tokens = ter
	if pp := peekPalOf(th); pp.invTer != hx(0x123456) || pp.invSec != (color.NRGBA{R: 0xff, G: 0xdf, B: 0xcf, A: 0xcc}) {
		t.Errorf("a sheet declaring theme-content-inv-ter over Karim's import: the card's %s / %s; Slack the sheet's #123456 with the account's #ffdfcfcc", hexA(pp.invTer), hexA(pp.invSec))
	}
}
