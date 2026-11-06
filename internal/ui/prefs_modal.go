package ui

import (
	"image"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// prefsModal is the Preferences dialog body: Appearance (theme, emoji
// style), Sidebar, Notifications. Changes apply immediately, like Slack.
type prefsModal struct {
	a      *App
	clicks map[string]*widget.Clickable
}

func (a *App) openPrefs() {
	if mkModal == nil {
		a.openPrefsMenu()
		return
	}
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}}
	a.ShowOverlay(mkModal(a, "Preferences", p.layout, nil))
}

func (p *prefsModal) btn(k string) *widget.Clickable {
	if c, ok := p.clicks[k]; ok {
		return c
	}
	c := new(widget.Clickable)
	p.clicks[k] = c
	return c
}

func (p *prefsModal) layout(gtx layout.Context) layout.Dimensions {
	a := p.a
	c := &a.th.C
	w := gtx.Constraints.Max.X
	y := 0
	heading := func(t string) {
		at(gtx, image.Pt(0, y), image.Pt(w, gtx.Dp(24)), func(gtx layout.Context) layout.Dimensions {
			return a.th.Label(gtx, 15, theme.Black, c.Fg, t)
		})
		y += gtx.Dp(30)
	}
	// option draws a radio (round) or checkbox (square) row.
	option := func(key, label, hint string, on, radio bool, do func()) {
		b := p.btn(key)
		rowH := gtx.Dp(28)
		if hint != "" {
			rowH = gtx.Dp(44)
		}
		at(gtx, image.Pt(0, y), image.Pt(w, rowH), func(gtx layout.Context) layout.Dimensions {
			return clickArea(gtx, b, nil, func(gtx layout.Context) layout.Dimensions {
				s := gtx.Dp(18)
				r := image.Rect(0, gtx.Dp(3), s, gtx.Dp(3)+s)
				rad := gtx.Dp(4)
				if radio {
					rad = s / 2
				}
				if on {
					theme.RRect(gtx, r, rad, c.Link)
					if radio {
						theme.Circle(gtx, r.Min.Add(image.Pt(s/2, s/2)), s*4/9, c.Bg)
					} else {
						at(gtx, r.Min, image.Pt(s, s), func(gtx layout.Context) layout.Dimensions {
							return icons.Get("check")(gtx, 18, c.SendText)
						})
					}
				} else {
					theme.RRect(gtx, r, rad, c.Bg)
					theme.Border(gtx, r, rad, gtx.Dp(1), c.BorderStrong)
				}
				at(gtx, image.Pt(gtx.Dp(28), 0), image.Pt(w-gtx.Dp(28), gtx.Dp(24)), func(gtx layout.Context) layout.Dimensions {
					return a.th.Label(gtx, 15, theme.Regular, c.Fg, label)
				})
				if hint != "" {
					at(gtx, image.Pt(gtx.Dp(28), gtx.Dp(22)), image.Pt(w-gtx.Dp(28), gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
						return a.th.Label(gtx, 13, theme.Regular, c.FgMuted, hint)
					})
				}
				return layout.Dimensions{Size: image.Pt(w, rowH)}
			})
		})
		if clicked(b) {
			do()
			a.Invalidate()
		}
		y += rowH + gtx.Dp(4)
	}
	gap := func() { y += gtx.Dp(14) }

	heading("Theme")
	option("light", "Light", "", !a.th.Dark, true, func() {
		if a.th.Dark {
			a.ToggleDark()
		}
	})
	option("dark", "Dark", "", a.th.Dark, true, func() {
		if !a.th.Dark {
			a.ToggleDark()
		}
	})
	gap()
	heading("Emoji")
	style := a.th.EmojiStyle
	option("apple", "Apple", "Slack's default emoji set", style == "" || style == "apple", true, func() { a.setEmojiStyle("apple") })
	option("google", "Google", "", style == "google", true, func() { a.setEmojiStyle("google") })
	gap()
	heading("Sidebar")
	option("sidebar", "Show the sidebar", "Ctrl+Shift+D toggles it", !a.sidebar.hidden, false, func() {
		a.sidebar.hidden = !a.sidebar.hidden
		a.savePrefs()
	})
	option("sbreset", "Reset sidebar width", "Back to the default 232px", a.sidebar.width == theme.SidebarW, false, func() {
		a.sidebar.width = theme.SidebarW
		a.savePrefs()
	})
	gap()
	heading("Notifications")
	option("notify", "Desktop notifications for DMs and mentions", "Muted conversations never notify", a.notifyOn(), false, a.toggleNotify)
	return layout.Dimensions{Size: image.Pt(w, y)}
}

func (a *App) setEmojiStyle(s string) {
	a.th.EmojiStyle = s
	a.prefs.EmojiStyle = s
	a.savePrefs()
}

// openPrefsMenu is the fallback when popups isn't linked.
func (a *App) openPrefsMenu() {
	a.menu(image.Pt(a.px(300), a.px(90)), []MenuItem{
		{Label: "Preferences", Disabled: true},
		{Divider: true},
		{Label: map[bool]string{true: "✓ Dark mode", false: "Dark mode"}[a.th.Dark], Icon: "moon", Do: a.ToggleDark},
		{Label: map[bool]string{true: "✓ Desktop notifications", false: "Desktop notifications"}[a.notifyOn()], Icon: "bell", Do: a.toggleNotify},
		{Label: map[bool]string{true: "Show sidebar", false: "Hide sidebar"}[a.sidebar.hidden], Icon: "sidebar", Hint: "Ctrl+Shift+D", Do: func() { a.sidebar.hidden = !a.sidebar.hidden }},
	})
}
