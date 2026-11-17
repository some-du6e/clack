package views

import (
	"fmt"
	"image/color"
	"testing"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// The Direct messages list sits in the sidebar's theme scope: Slack's
// .p-dms_list rows are p-activity_ia4_page__items, theme-content-inv-pry
// names and messages, theme-content-inv-sec meta and timestamps,
// theme-surf-inv-ter :hover and --selected, theme-otl-inv-pry rules, and
// its search box a themed c-filter_input (theme-surf-inv-sec). With a
// light sidebar (Karim's #FED4BE as Slack's picker stores it: brightness
// 2, so nothing inverts) that is the custom colour's dark ink, not the
// default theme's inverted white. Values: Slack's tokens for the sidebar's
// scope (theme.TestThemeInkMatchesSlack's table, from Slack's code).
func TestDMsListFollowsSidebarScope(t *testing.T) {
	hx := func(v uint32) color.NRGBA { return rgb(v) }
	hxa := func(v uint32, a uint8) color.NRGBA { c := rgb(v); c.A = a; return c }
	for _, c := range []struct {
		name, ia                         string
		dark                             bool
		white, soft, tint, rule, inputBg color.NRGBA
	}{
		{"karim picker light", `{"primary":{"palette":"aubergine","custom":"#FED4BE"},"highlight1":{"palette":"aubergine"},"highlight2":{"palette":"jade"},"important":{"palette":"aubergine"},"brightness":2,"sidebarInverted":false,"useCustomHex":true}`, false,
			hx(0x1c0901), hxa(0x1c0901, 0xcc), hxa(0x41281a, 0x21), hxa(0x41281a, 0x21), hxa(0xffffff, 0xe6)},
		{"ochin light", `{"primary":{"palette":"ocean"},"highlight1":{"palette":"ocean"},"highlight2":{"palette":"cilantro"},"important":{"palette":"mojito"},"brightness":5,"sidebarInverted":false,"useCustomHex":false}`, false,
			hx(0x122a59), hxa(0x122a59, 0xcc), hxa(0x0b4c8c, 0x21), hxa(0x0b4c8c, 0x21), hxa(0xffffff, 0xe6)},
		{"karim picker dark", `{"primary":{"palette":"aubergine","custom":"#FED4BE"},"highlight1":{"palette":"aubergine"},"highlight2":{"palette":"jade"},"important":{"palette":"aubergine"},"brightness":2,"sidebarInverted":false,"useCustomHex":true}`, true,
			hx(0xf8f8f8), hxa(0xffdfcf, 0xcc), hxa(0xfff4f0, 0x14), hxa(0xfff4f0, 0x38), hxa(0x101214, 0x99)},
		{"default light", ``, false,
			hx(0xffffff), hxa(0xf6e4ff, 0xe6), hxa(0xf9edff, 0x14), hxa(0xf9edff, 0x38), hxa(0xf9edff, 0x1c)},
		{"default dark", ``, true,
			hx(0xf8f8f8), hxa(0xe3ceeb, 0xcc), hxa(0xefe1f5, 0x14), hxa(0xefe1f5, 0x38), hxa(0x101214, 0x99)},
	} {
		t.Run(c.name, func(t *testing.T) {
			ia, _ := theme.ParseIATheme(c.ia)
			th := theme.Bare(c.dark)
			// the account theme under the palette, as the ui's restyle lays it
			th.Restyle = func(t *theme.Theme, p *theme.Palette) { theme.ApplyIATheme(p, ia, t.Dark, true) }
			th.Refresh()
			d := &DMs{}
			d.th = th
			d.dmsColors()
			// the column: the gradient's middle under the sidebar's tint,
			// every translucent colour flattened over it (dmsColors)
			base := flatten(th.C.SidebarBg, theme.Mix(th.C.ChromeA, th.C.ChromeB, .5))
			hex := func(c color.NRGBA) string { return fmt.Sprintf("#%02x%02x%02x%02x", c.R, c.G, c.B, c.A) }
			for _, f := range []struct {
				what      string
				got, want color.NRGBA
			}{
				{"names (theme-content-inv-pry)", d.c.white, c.white},
				{"meta (theme-content-inv-sec)", d.c.soft, flatten(c.soft, base)},
				{"hover and selection (theme-surf-inv-ter)", d.c.tint, flatten(c.tint, base)},
				{"rules (theme-otl-inv-pry)", d.c.rule, flatten(c.rule, base)},
				{"search box (theme-surf-inv-sec)", d.c.inputBg, flatten(c.inputBg, base)},
			} {
				if f.got != f.want {
					t.Errorf("%s = %s, Slack %s", f.what, hex(f.got), hex(f.want))
				}
			}
		})
	}
}
