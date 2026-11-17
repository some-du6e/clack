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
// {background-color:#0000}.
func TestDMsListHoverByMode(t *testing.T) {
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
