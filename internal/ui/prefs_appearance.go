package ui

import (
	"image"
	"image/color"
	"strconv"
	"strings"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// appearancePage is Preferences > Appearance, laid out like Slack's
// "themes" tab (PrefsSectionThemes): Font, Color Mode (Light / Dark /
// System), then the Slack themes | Custom theme tabs. Theme choices are
// written to the account like Slack does (theme_account.go); the colour
// mode stays on this device.
func (p *prefsModal) appearancePage(gtx layout.Context) layout.Dimensions {
	a := p.a
	f := p.form(gtx)
	f.subheading("Font")
	f.selectBox("font", "", "Lato (Default)", false, nil)
	f.na()
	f.hr()

	sync := a.themeSyncOn()
	f.subheading("Color Mode")
	f.para("Choose if Slack’s appearance should be light or dark, or follow your computer’s settings.")
	f.option("mode.light", "Light", "", !sync && !a.th.Dark, true, func() { a.pickMode(false) })
	f.option("mode.dark", "Dark", "", !sync && a.th.Dark, true, func() { a.pickMode(true) })
	_, osOK := osSchemeDark() // the watcher's atomic: never an OS call in layout
	hint := ""
	if !osOK {
		hint = "Your desktop doesn't report a colour scheme, so this stays light"
	}
	f.option("mode.os", "System", hint, sync, true, func() { a.setThemeSync(true) })
	if !a.th.Dark { // focus_mode_force_dark_mode_enabled (Clack has no focus mode)
		f.off("focus_mode_force_dark_mode_enabled", "Always use dark color mode when in focus mode.", "", p.prefBool("focus_mode_force_dark_mode_enabled", true), false)
	}
	f.gap()

	cur := a.accountTheme()
	curID := cur.PresetID()
	tab := p.themeTab
	if tab == "" { // Slack's Container: Custom theme for a custom-hex theme that is no preset
		tab = "slack"
		if cur.UseCustomHex && curID == "" {
			tab = "custom"
		}
	}
	p.themeTabs(f, tab)
	if tab == "custom" {
		p.customTheme(f, cur)
	} else {
		p.slackThemes(f, curID)
	}
	return f.dims()
}

// themeTabs is the Slack themes | Custom theme strip (Slack's horizontal
// Tabs: 13px 700, a 2px underline under the current one).
func (p *prefsModal) themeTabs(f *prefsForm, cur string) {
	gtx, a := f.gtx, f.p.a
	c := &a.th.C
	x := gtx.Dp(8)
	h := gtx.Dp(32)
	for _, t := range []struct{ id, label string }{{"slack", "Slack themes"}, {"custom", "Custom theme"}} {
		t := t
		f.record("tab", "themetab."+t.id, t.label, t.id == cur, true)
		b := p.btn("themetab." + t.id)
		w := a.th.Measure(gtx, 13, theme.Bold, t.label) + gtx.Dp(16)
		at(gtx, image.Pt(x, f.y), image.Pt(w, h), func(gtx layout.Context) layout.Dimensions {
			return clickArea(gtx, b, nil, func(gtx layout.Context) layout.Dimensions {
				// .c-tabs__tab: content-sec, content-pry when hovered or
				// active; the active underline is inset 0 -2px 0 0
				// var(--dt_color-theme-base-pry): the dialog's one
				// account-theme colour (th.C.Accent)
				pc := p.colors()
				fg := pc.on(a.th, pc.tabSec)
				if t.id == cur || b.Hovered() {
					fg = pc.fieldFg
				}
				if t.id == cur {
					theme.Fill(gtx, image.Rect(0, h-gtx.Dp(2), w, h), c.Accent)
				}
				at(gtx, image.Pt(gtx.Dp(8), 0), image.Pt(w, h), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
						return a.th.Label(gtx, 13, theme.Bold, fg, t.label)
					})
				})
				return layout.Dimensions{Size: image.Pt(w, h)}
			})
		})
		if clicked(b) {
			p.themeTab = t.id
			a.Invalidate()
		}
		x += w + gtx.Dp(12)
	}
	pc := p.colors() // .c-tabs__tab_menu: inset 0 -1px 0 0 var(--dt_color-otl-ter)
	theme.Fill(gtx, image.Rect(0, f.y+h, f.w, f.y+h+max(1, gtx.Dp(1))), pc.on(a.th, pc.divider))
	f.y += h + gtx.Dp(16)
}

// slackThemes is the Slack themes tab: Slack's preset groups as 3-column
// tiles.
func (p *prefsModal) slackThemes(f *prefsForm, curID string) {
	gtx, a := f.gtx, f.p.a
	group := ""
	col := 0
	presets := theme.ThemePresets
	if c := a.Core(); c != nil && c.Experiment("miles_davis_themes") == "on" { // TU6L: Miles Davis first
		presets = append(append([]theme.ThemePreset(nil), theme.MilesDavisPresets...), presets...)
	}
	for _, pr := range presets {
		if pr.Group != group {
			if col != 0 {
				f.y += gtx.Dp(48)
				col = 0
			}
			group = pr.Group
			f.record("legend", "", group, false, true)
			at(gtx, image.Pt(0, f.y), image.Pt(f.w, gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
				return a.th.Label(gtx, 13, theme.Bold, a.th.C.Fg, group)
			})
			f.y += gtx.Dp(24)
		}
		pr := pr
		f.record("radio", "preset."+pr.ID, pr.Name, pr.ID == curID, true)
		p.presetTile(gtx, f, col, pr, pr.ID == curID)
		col++
		if col == 3 {
			col = 0
			f.y += gtx.Dp(48)
		}
	}
	if col != 0 {
		f.y += gtx.Dp(48)
	}
}

// customRoles are the Custom theme tab's four colours, in Slack's order.
var customRoles = []struct {
	key, label string
	get        func(*theme.IATheme) *theme.IARole
}{
	{"primary", "System navigation", func(t *theme.IATheme) *theme.IARole { return &t.Primary }},
	{"highlight1", "Selected items", func(t *theme.IATheme) *theme.IARole { return &t.Highlight1 }},
	{"highlight2", "Presence indication", func(t *theme.IATheme) *theme.IARole { return &t.Highlight2 }},
	{"important", "Notifications", func(t *theme.IATheme) *theme.IARole { return &t.Important }},
}

// customTheme is Slack's CustomTheme: Theme colors (Share, Import,
// Surprise me), a hex field per role, Window gradient, Darker sidebars.
func (p *prefsModal) customTheme(f *prefsForm, cur theme.IATheme) {
	a := p.a
	f.subheading("Theme colors")
	hexes := make([]string, len(customRoles))
	for i, r := range customRoles {
		hexes[i] = cur.RoleHex(r.key, a.th.Dark)
	}
	f.buttons(prefsButton{"theme_share", "Share", true, func() {
		a.copyText("Check out my Slack theme: " + strings.Join(hexes, ","))
		a.Toast("Copied")
	}}, prefsButton{"theme_import", "Import", true, func() { p.importTheme() }}, // prefs_a.go
		prefsButton{"theme_surprise", "Surprise me", true, func() { a.setAccountTheme(theme.SurpriseTheme(surpriseRand)) }})
	gtx := f.gtx
	colW := (f.w - gtx.Dp(16)) / 2
	for i, r := range customRoles {
		x := (i % 2) * (colW + gtx.Dp(16))
		i := i
		p.hexField(f, x, colW, r.key, r.label, hexes[i], func(hex string) { p.setRoleHex(i, hex) })
		if i%2 == 1 {
			f.y += gtx.Dp(76)
		}
	}
	f.gap()
	f.option("gradients", "Window gradient", "Blend Window background and Selected items colors together in window backgrounds.", a.accountGradients(), false, func() {
		a.setWindowGradients(!a.accountGradients())
	})
	// ThemeSidebarInvertedCheckBox: hidden in dark mode and at low brightness
	if !a.th.Dark && cur.Brightness >= 3 {
		f.option("inverted", "Darker sidebars", "Use this setting to choose whether you want light or dark sidebars.", cur.SidebarInverted, false, func() {
			t := a.accountTheme()
			t.SidebarInverted = !t.SidebarInverted
			a.setAccountTheme(t)
		})
	}
}

// setRoleHex gives customRoles[i] its own colour: the theme becomes a
// custom-hex one (useCustomHex) and every other role keeps the colour it
// showed, like Slack's CustomHEXPickerSection.
func (p *prefsModal) setRoleHex(i int, hex string) {
	a := p.a
	t := a.accountTheme()
	for j, rr := range customRoles {
		role := rr.get(&t)
		if role.Custom == "" {
			role.Custom = t.RoleHex(rr.key, a.th.Dark)
		}
		if j == i {
			role.Custom = hex
		}
	}
	t.UseCustomHex = true
	if i == 0 { // ColorPicker: a custom primary sets the brightness (S6)
		if b := theme.BrightnessFromCustomHex(hex); b > 0 {
			t.Brightness = b
		}
	}
	a.setAccountTheme(t)
}

// hexField is CustomHEXPickerSection: the role's name over a pill with a
// swatch and its hex, which can be typed over (Enter applies it).
func (p *prefsModal) hexField(f *prefsForm, x, w int, role, label, hex string, set func(string)) {
	gtx, a := f.gtx, f.p.a
	c := &a.th.C
	if p.hexEd == nil {
		p.hexEd = map[string]*widget.Editor{}
	}
	ed := p.hexEd[role]
	if ed == nil {
		ed = &widget.Editor{SingleLine: true, Submit: true, MaxLen: 7}
		p.hexEd[role] = ed
	}
	focused := gtx.Focused(ed)
	if !focused && ed.Text() != hex {
		ed.SetText(hex)
	}
	for {
		e, ok := ed.Update(gtx)
		if !ok {
			break
		}
		if _, ok := e.(widget.SubmitEvent); ok {
			if h, ok := theme.ParseHexColor(ed.Text()); ok && !strings.EqualFold(h, hex) {
				set(h)
			} else if !ok {
				a.Toast("That isn’t a hex colour (like #611F69)")
			}
			gtx.Execute(key.FocusCmd{Tag: nil})
		}
	}
	f.record("hex", "hex."+role, label+" "+hex, false, true)
	y := f.y
	at(gtx, image.Pt(x, y), image.Pt(w, gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
		return a.th.Label(gtx, 13, theme.Bold, c.Fg, label)
	})
	ph := gtx.Dp(34)
	col, _ := hexColor(hex)
	at(gtx, image.Pt(x, y+gtx.Dp(26)), image.Pt(w, ph), func(gtx layout.Context) layout.Dimensions {
		r := image.Rectangle{Max: image.Pt(w, ph)}
		pc := p.colors() // an input: base-pry, otl-pry, otl-hgl-1 when focused
		theme.RRect(gtx, r, ph/2, pc.field)
		bc := pc.on(a.th, pc.fieldBorder)
		if focused {
			bc = pc.focus
		}
		theme.Border(gtx, r, ph/2, gtx.Dp(1), bc)
		d := gtx.Dp(22)
		theme.Circle(gtx, image.Pt(gtx.Dp(6)+d/2, ph/2), d, col)
		at(gtx, image.Pt(gtx.Dp(38), 0), image.Pt(w-gtx.Dp(70), ph), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, ph, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return a.th.Editor(gtx, ed, 15, c.Fg, c.Link)
			})
		})
		eb := p.btn("hexedit." + role) // "Edit … color": the colour popover (colorpicker.go)
		at(gtx, image.Pt(w-gtx.Dp(32), (ph-gtx.Dp(24))/2), image.Pt(gtx.Dp(24), gtx.Dp(24)), func(gtx layout.Context) layout.Dimensions {
			return clickArea(gtx, eb, nil, func(gtx layout.Context) layout.Dimensions {
				at(gtx, image.Pt(gtx.Dp(4), gtx.Dp(4)), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
					return icons.Get("edit")(gtx, 16, c.Fg)
				})
				return layout.Dimensions{Size: image.Pt(gtx.Dp(24), gtx.Dp(24))}
			})
		})
		if clicked(eb) {
			a.ShowOverlay(newColorPopover(a, a.PointerPos(), hex, set))
		}
		return layout.Dimensions{Size: r.Max}
	})
	f.record("button", "button.hexedit."+role, "Edit "+label+" color", false, true)
}

// hexColor is "#RRGGBB" as a colour.
func hexColor(s string) (color.NRGBA, bool) {
	h, ok := theme.ParseHexColor(s)
	if !ok {
		return color.NRGBA{}, false
	}
	v, err := strconv.ParseUint(h[1:], 16, 32)
	if err != nil {
		return color.NRGBA{}, false
	}
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}, true
}

// presetTile is one Slack theme in the 3-column grid: a preview circle in
// the theme's window colour with its notification colour, and the name.
func (p *prefsModal) presetTile(gtx layout.Context, f *prefsForm, col int, pr theme.ThemePreset, sel bool) {
	a := p.a
	c := &a.th.C
	gapX := gtx.Dp(8)
	tw := (f.w - 2*gapX) / 3
	th := gtx.Dp(40)
	b := p.btn("preset." + pr.ID)
	// cached and computed off the UI goroutine; a tile draws once it's in
	fields, _ := theme.FieldsAsync(pr.Theme, a.th.Dark, true, time.Millisecond, a.Invalidate)
	at(gtx, image.Pt(col*(tw+gapX), f.y), image.Pt(tw, th), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, b, func(gtx layout.Context, s image.Point, hov bool) {
			r := image.Rectangle{Max: s}
			if hov && !sel {
				theme.RRect(gtx, r, gtx.Dp(8), c.BgHover)
			}
			if sel {
				theme.Border(gtx, r, gtx.Dp(8), gtx.Dp(2), c.Link)
			} else {
				theme.Border(gtx, r, gtx.Dp(8), gtx.Dp(1), c.BorderStrong)
			}
		}, func(gtx layout.Context) layout.Dimensions {
			cx := gtx.Dp(22)
			cy := th / 2
			theme.Circle(gtx, image.Pt(cx, cy), gtx.Dp(22), opaque(fields["ChromeA"]))
			theme.Circle(gtx, image.Pt(cx+gtx.Dp(5), cy+gtx.Dp(5)), gtx.Dp(10), opaque(fields["Badge"]))
			at(gtx, image.Pt(gtx.Dp(42), 0), image.Pt(tw-gtx.Dp(46), th), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, th, func(gtx layout.Context) layout.Dimensions {
					return a.th.Label(gtx, 13, theme.Regular, c.Fg, pr.Name)
				})
			})
			return layout.Dimensions{Size: image.Pt(tw, th)}
		})
	})
	if clicked(b) && !sel {
		a.setAccountTheme(pr.Theme)
	}
}

func opaque(c color.NRGBA) color.NRGBA { c.A = 0xff; return c }
