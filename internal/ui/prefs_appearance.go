package ui

import (
	"image"
	"image/color"
	"strings"

	"gioui.org/layout"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// appearancePage is Preferences > Appearance, laid out like Slack's
// "themes" tab: Color Mode (Light / Dark / System), Themes (Slack's
// presets and the custom theme's four roles), Darker sidebars, Window
// gradient, then Emoji. Theme choices are written to the account like
// Slack does (theme_account.go); the colour mode stays on this device.
func (p *prefsModal) appearancePage(gtx layout.Context) layout.Dimensions {
	a := p.a
	f := p.form(gtx)
	sync := a.themeSyncOn()
	f.heading("Color Mode")
	f.note("Choose if Slack’s appearance should be light or dark, or follow your computer’s settings.", false, 1)
	f.option("mode.light", "Light", "", !sync && !a.th.Dark, true, func() { a.pickMode(false) })
	f.option("mode.dark", "Dark", "", !sync && a.th.Dark, true, func() { a.pickMode(true) })
	_, osOK := osPrefersDark()
	hint := ""
	if !osOK {
		hint = "Your desktop doesn't report a colour scheme, so this stays light"
	}
	f.option("mode.os", "System", hint, sync, true, func() { a.setThemeSync(true) })
	f.gap()

	cur := a.accountTheme()
	curID := cur.PresetID()
	f.heading("Themes")
	f.note("Your Slack theme, saved to your account: it shows in Slack too.", false, 1)
	group := ""
	col := 0
	for _, pr := range theme.ThemePresets {
		if pr.Group != group {
			if col != 0 {
				f.y += gtx.Dp(48)
				col = 0
			}
			group = pr.Group
			at(gtx, image.Pt(0, f.y), image.Pt(f.w, gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
				return a.th.Label(gtx, 13, theme.Bold, a.th.C.FgMuted, group)
			})
			f.y += gtx.Dp(24)
		}
		pr := pr
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
	f.gap()

	f.heading("Custom theme")
	if curID == "" {
		f.note("You're using a custom theme.", false, 1)
	} else {
		f.note("Pick a palette for each part to make your own theme.", false, 1)
	}
	roles := []struct {
		key, label string
		get        func(*theme.IATheme) *theme.IARole
	}{
		{"primary", "System navigation", func(t *theme.IATheme) *theme.IARole { return &t.Primary }},
		{"highlight1", "Selected items", func(t *theme.IATheme) *theme.IARole { return &t.Highlight1 }},
		{"highlight2", "Presence indication", func(t *theme.IATheme) *theme.IARole { return &t.Highlight2 }},
		{"important", "Notifications", func(t *theme.IATheme) *theme.IARole { return &t.Important }},
	}
	for _, r := range roles {
		r := r
		role := *r.get(&cur)
		label := r.label
		if cur.UseCustomHex && role.Custom != "" {
			label += "  " + strings.ToUpper(role.Custom)
		}
		at(gtx, image.Pt(0, f.y), image.Pt(f.w, gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
			return a.th.Label(gtx, 13, theme.Bold, a.th.C.Fg, label)
		})
		f.y += gtx.Dp(22)
		p.paletteRow(gtx, f, "role."+r.key, role, cur.UseCustomHex && role.Custom != "", func(name string) {
			t := a.accountTheme()
			rr := r.get(&t)
			rr.Palette, rr.Custom = name, ""
			a.setAccountTheme(t)
		})
	}
	if cur.UseCustomHex {
		f.note("Typing in your own hex colours isn't available in Clack yet; picking a palette replaces that part's colour.", false, 2)
	}
	// brightness 1..6 (Slack's theme brightness: how deep the window colour is)
	at(gtx, image.Pt(0, f.y), image.Pt(f.w, gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
		return a.th.Label(gtx, 13, theme.Bold, a.th.C.Fg, "Brightness")
	})
	f.y += gtx.Dp(22)
	for b := 1; b <= 6; b++ {
		b := b
		x := (b - 1) * gtx.Dp(40)
		btn := p.btn("bright." + itoa(b))
		on := cur.Brightness == b
		at(gtx, image.Pt(x, f.y), image.Pt(gtx.Dp(34), gtx.Dp(28)), func(gtx layout.Context) layout.Dimensions {
			return clickArea(gtx, btn, func(gtx layout.Context, s image.Point, hov bool) {
				r := image.Rectangle{Max: s}
				bg := a.th.C.Bg
				if on {
					bg = a.th.C.Link
				} else if hov {
					bg = a.th.C.BgHover
				}
				theme.RRect(gtx, r, gtx.Dp(6), bg)
				if !on {
					theme.Border(gtx, r, gtx.Dp(6), gtx.Dp(1), a.th.C.BorderStrong)
				}
			}, func(gtx layout.Context) layout.Dimensions {
				fg := a.th.C.Fg
				if on {
					fg = a.th.C.SendText
				}
				return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return a.th.Label(gtx, 13, theme.Bold, fg, itoa(b))
				})
			})
		})
		if clicked(btn) && !on {
			t := a.accountTheme()
			t.Brightness = b
			a.setAccountTheme(t)
		}
	}
	f.y += gtx.Dp(40)

	// Slack hides "Darker sidebars" in dark mode and at low brightness.
	if !a.th.Dark && cur.Brightness >= 3 {
		f.option("inverted", "Darker sidebars", "Use this setting to choose whether you want light or dark sidebars.", cur.SidebarInverted, false, func() {
			t := a.accountTheme()
			t.SidebarInverted = !t.SidebarInverted
			a.setAccountTheme(t)
		})
	}
	f.option("gradients", "Window gradient", "Blend Window background and Selected items colors together in window backgrounds.", a.accountGradients(), false, func() {
		a.setWindowGradients(!a.accountGradients())
	})
	f.gap()

	f.heading("Emoji")
	style := a.th.EmojiStyle
	f.option("apple", "Apple", "Slack's default emoji set", style == "" || style == "apple", true, func() { a.setEmojiStyle("apple") })
	f.option("google", "Google", "", style == "google", true, func() { a.setEmojiStyle("google") })
	return f.dims()
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
	fields := pr.Theme.Fields(a.th.Dark, true)
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

// paletteRow is a role's palette picker: Slack's 20 palettes as swatches
// (custom: the role uses its own hex, so none is selected).
func (p *prefsModal) paletteRow(gtx layout.Context, f *prefsForm, key string, role theme.IARole, custom bool, pick func(string)) {
	a := p.a
	d := gtx.Dp(20)
	step := gtx.Dp(24)
	per := max(1, (f.w+gtx.Dp(4))/step)
	for i, name := range theme.PaletteNames {
		name := name
		x := (i % per) * step
		y := f.y + (i/per)*step
		b := p.btn(key + "." + name)
		sel := !custom && role.Palette == name
		sw := theme.PaletteSwatch(name, a.th.Dark)
		at(gtx, image.Pt(x, y), image.Pt(d, d), func(gtx layout.Context) layout.Dimensions {
			return clickArea(gtx, b, func(gtx layout.Context, s image.Point, hov bool) {
				ctr := image.Pt(s.X/2, s.Y/2)
				if sel {
					theme.Circle(gtx, ctr, s.X, a.th.C.Link)
					theme.Circle(gtx, ctr, s.X-gtx.Dp(4), a.th.C.Bg)
					theme.Circle(gtx, ctr, s.X-gtx.Dp(8), sw)
				} else {
					theme.Circle(gtx, ctr, s.X-gtx.Dp(2), sw)
					if hov {
						theme.Circle(gtx, ctr, gtx.Dp(6), a.th.C.Bg)
					}
				}
			}, func(gtx layout.Context) layout.Dimensions {
				return layout.Dimensions{Size: image.Pt(d, d)}
			})
		})
		if clicked(b) && !sel {
			pick(name)
		}
	}
	rows := (len(theme.PaletteNames) + per - 1) / per
	f.y += rows*step + gtx.Dp(8)
}

func opaque(c color.NRGBA) color.NRGBA { c.A = 0xff; return c }
