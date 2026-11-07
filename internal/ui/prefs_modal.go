package ui

import (
	"image"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// prefsModal is the one Preferences dialog: Slack's left nav with
// Availability (ui-popups' page), Notifications, Navigation and Appearance.
// Changes apply immediately, like Slack.
type prefsModal struct {
	a       *App
	clicks  map[string]*widget.Clickable
	section string
	avail   pagePositioned // popups.AvailabilityPage when linked
	org     originTracker
}

// pagePositioned is a page widget that needs its window origin (dropdowns).
type pagePositioned interface {
	View
	SetOrigin(image.Point)
}

var mkAvailability func(a *App) pagePositioned

var prefsNav = []struct{ id, label, icon string }{
	{"availability", "Availability", "clock"},
	{"notifications", "Notifications", "bell"},
	{"navigation", "Navigation", "sidebar"},
	{"appearance", "Appearance", "edit"},
}

func (a *App) openPrefs() { a.openPrefsSection("notifications") }

// openPrefsSection opens Preferences on a section (popups'
// OpenPreferencesSection hook, e.g. "availability" from "Edit your working
// hours").
func (a *App) openPrefsSection(section string) {
	if mkModal == nil {
		a.openPrefsMenu()
		return
	}
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}, section: section}
	if mkAvailability != nil {
		p.avail = mkAvailability(a)
	}
	a.ShowOverlay(mkModal(a, "Preferences", p.layout, nil))
}

func (p *prefsModal) layout(gtx layout.Context) layout.Dimensions {
	a := p.a
	c := &a.th.C
	w := gtx.Constraints.Max.X
	navW := gtx.Dp(200)
	h := gtx.Dp(560)
	org := p.org.get(image.Point{})
	g := gtx
	g.Constraints = layout.Exact(image.Pt(w, h))
	p.org.track(g, a)
	y := 0
	for _, it := range prefsNav {
		it := it
		b := p.btn("nav." + it.id)
		at(gtx, image.Pt(0, y), image.Pt(navW-gtx.Dp(16), gtx.Dp(32)), func(gtx layout.Context) layout.Dimensions {
			return clickArea(gtx, b, func(gtx layout.Context, s image.Point, hov bool) {
				if p.section == it.id {
					theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(6), c.Link)
				} else if hov {
					theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(6), c.BgHover)
				}
			}, func(gtx layout.Context) layout.Dimensions {
				fg := c.Fg
				if p.section == it.id {
					fg = c.SendText
				}
				at(gtx, image.Pt(gtx.Dp(10), gtx.Dp(8)), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
					return icons.Get(it.icon)(gtx, 16, fg)
				})
				at(gtx, image.Pt(gtx.Dp(34), 0), image.Pt(navW, gtx.Dp(32)), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, gtx.Dp(32), func(gtx layout.Context) layout.Dimensions {
						return a.th.Label(gtx, 15, theme.Regular, fg, it.label)
					})
				})
				return layout.Dimensions{Size: image.Pt(navW-gtx.Dp(16), gtx.Dp(32))}
			})
		})
		if clicked(b) {
			p.section = it.id
			a.Invalidate()
		}
		y += gtx.Dp(34)
	}
	pageW := w - navW
	at(gtx, image.Pt(navW, 0), image.Pt(pageW, h), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min = image.Point{}
		if p.section == "availability" && p.avail != nil {
			p.avail.SetOrigin(org.Add(image.Pt(navW, 0)))
			return p.avail.Layout(gtx)
		}
		return p.page(gtx)
	})
	return layout.Dimensions{Size: image.Pt(w, h)}
}

func (p *prefsModal) btn(k string) *widget.Clickable {
	if c, ok := p.clicks[k]; ok {
		return c
	}
	c := new(widget.Clickable)
	p.clicks[k] = c
	return c
}

func (p *prefsModal) page(gtx layout.Context) layout.Dimensions {
	a := p.a
	c := &a.th.C
	w := gtx.Constraints.Max.X
	y := 0
	sec := p.section
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

	if sec == "appearance" {
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
	}
	if sec == "navigation" {
		heading("Sidebar")
		option("sidebar", "Show the sidebar", "Ctrl+Shift+D toggles it", !a.sidebar.hidden, false, func() {
			a.sidebar.hidden = !a.sidebar.hidden
			a.savePrefs()
		})
		option("sbreset", "Reset sidebar width", "Back to the default 232px", a.sidebar.width == theme.SidebarW, false, func() {
			a.sidebar.width = theme.SidebarW
			a.savePrefs()
		})
	}
	if sec == "notifications" || sec == "availability" {
		heading("Notifications")
		option("notify", "Desktop notifications for DMs and mentions", "Muted conversations never notify", a.notifyOn(), false, a.toggleNotify)
	}
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
