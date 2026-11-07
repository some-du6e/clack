package ui

import (
	"image"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
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

// prefsNav is Slack's tabs[] for the dialog. The last id must be
// "advanced": Taut appends its own tabs only to the Preferences Tabs.
var prefsNav = []struct{ id, label, icon, slackIcon string }{
	{"availability", "Availability", "clock", "clock"},
	{"notifications", "Notifications", "bell", "notifications"},
	{"navigation", "Navigation", "sidebar", "sidebar"},
	{"appearance", "Appearance", "edit", "paintbrush"},
	{"advanced", "Advanced", "settings", "settings"},
}

// Slack's Preferences classes (taut's settings CSS targets them).
var (
	clsPrefsTabs  = slackc.Class("c-tabs p-prefs_dialog__tabs c-tabs--vertical")
	clsPrefsMenu  = slackc.Class("c-tabs__tab_menu p-prefs_dialog__menu", "role", "tablist")
	clsPrefsTab   = slackc.Class("c-button-unstyled c-tabs__tab js-tab", "role", "tab").Tag("button").Mods("c-tabs__tab--active")
	clsPrefsPanel = slackc.Class("c-tabs__tab_panel p-prefs_dialog__panel", "role", "tabpanel")
)

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
	return slackc.Component(gtx, "Tabs", p, (*prefsModal).tabsProps, (*prefsModal).tabs)
}

// tabsProps is Slack's Tabs shape: tabs [{id, label, content, svgIcon}],
// currentTabId, onTabChange(id, event), collapsible.
func (p *prefsModal) tabsProps() slackc.Props {
	tabs := make([]any, len(prefsNav))
	for i, it := range prefsNav {
		id := it.id
		tabs[i] = slackc.Props{
			"id":         id,
			"label":      it.label,
			"aria-label": it.label,
			"svgIcon":    slackc.Props{"name": it.slackIcon},
			"content": slackc.Child("PrefsSection", slackc.Props{"id": id}, func(gtx layout.Context, _ slackc.Props) layout.Dimensions {
				return p.section1(gtx, id, image.Point{})
			}),
		}
	}
	return slackc.Props{
		"$key":         "prefs",
		"tabs":         tabs,
		"currentTabId": p.section,
		"collapsible":  true,
		"onTabChange": func(id string) {
			p.section = id
			p.a.Invalidate()
		},
	}
}

// tabs is the native Tabs: Clack's nav + page. Under a patch it draws the
// tabs[] it was given (Taut's own tabs lay out their content element),
// selects currentTabId and reports clicks through onTabChange.
func (p *prefsModal) tabs(gtx layout.Context, tp slackc.Props) layout.Dimensions {
	a := p.a
	w := gtx.Constraints.Max.X
	navW := gtx.Dp(200)
	h := gtx.Dp(560)
	org := p.org.get(image.Point{})
	g := gtx
	g.Constraints = layout.Exact(image.Pt(w, h))
	p.org.track(g, a)
	ts := clsPrefsTabs.Enter(gtx, 0, 0)
	defer ts.Leave()

	cur := p.section
	var list []any
	if tp != nil {
		cur = tp.StrOr("currentTabId", cur)
		list = tp.List("tabs")
	}
	var content slackc.Element
	ownContent := true
	ms := clsPrefsMenu.Enter(gtx, 0, 0)
	y := 0
	if list == nil {
		for _, it := range prefsNav {
			if p.navItem(gtx, y, navW, it.id, it.label, nil, it.icon, cur == it.id) {
				p.section = it.id
				a.Invalidate()
			}
			y += gtx.Dp(34)
		}
	} else {
		for i, x := range list {
			t, _ := x.(slackc.Props)
			if t == nil {
				continue
			}
			id := t.StrOr("id", "tab-"+itoa(i))
			label := t.StrOr("label", "")
			var lel *slackc.Element
			if e, ok := t.Elem("label"); ok {
				lel = &e
			}
			if id == cur {
				if e, ok := t.Elem("content"); ok && !p.ownSection(id) {
					content, ownContent = e, false
				}
			}
			icon := t.Map("svgIcon").StrOr("name", "")
			if icon != "" {
				icon = icons.Slack(icon)
			}
			if p.navItem(gtx, y, navW, id, label, lel, icon, cur == id) {
				if tp.Has("onTabChange") {
					tp.Invoke("onTabChange", id, slackc.Props{"type": "click"})
				} else {
					p.section = id
				}
				a.Invalidate()
			}
			y += gtx.Dp(34)
		}
	}
	ms.Leave()

	pageW := w - navW
	at(gtx, image.Pt(navW, 0), image.Pt(pageW, h), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min = image.Point{}
		ps := clsPrefsPanel.Enter(gtx, 0, 0)
		defer ps.Leave()
		if !ownContent {
			return slackc.LayoutElement(gtx, content)
		}
		return p.section1(gtx, cur, org.Add(image.Pt(navW, 0)))
	})
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// ownSection reports whether id is one of Clack's own sections.
func (p *prefsModal) ownSection(id string) bool {
	for _, it := range prefsNav {
		if it.id == id {
			return true
		}
	}
	return false
}

// section1 draws one of Clack's sections; org is its window origin.
func (p *prefsModal) section1(gtx layout.Context, id string, org image.Point) layout.Dimensions {
	if id == "availability" && p.avail != nil {
		p.avail.SetOrigin(org)
		return p.avail.Layout(gtx)
	}
	return p.page(gtx, id)
}

// navItem draws one tab button at y; label is a string, or lel a plugin's
// label element. Reports a click.
func (p *prefsModal) navItem(gtx layout.Context, y, navW int, id, label string, lel *slackc.Element, icon string, cur bool) bool {
	a := p.a
	c := &a.th.C
	b := p.btn("nav." + id)
	var mods uint64
	if cur {
		mods = 1
	}
	st := clsPrefsTab.Enter(gtx, hoverState(b.Hovered()), mods)
	defer st.Leave()
	if st.Hidden() {
		return false
	}
	at(gtx, image.Pt(0, y), image.Pt(navW-gtx.Dp(16), gtx.Dp(32)), func(gtx layout.Context) layout.Dimensions {
		return st.Wrap(gtx, func(gtx layout.Context) layout.Dimensions {
			return clickArea(gtx, b, func(gtx layout.Context, s image.Point, hov bool) {
				if cur {
					theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(6), c.Link)
				} else if hov {
					theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(6), c.BgHover)
				}
			}, func(gtx layout.Context) layout.Dimensions {
				fg := c.Fg
				if cur {
					fg = c.SendText
				}
				fg = st.Color(fg)
				if icon != "" {
					at(gtx, image.Pt(gtx.Dp(10), gtx.Dp(8)), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
						return icons.Get(icon)(gtx, 16, fg)
					})
				}
				at(gtx, image.Pt(gtx.Dp(34), 0), image.Pt(navW, gtx.Dp(32)), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, gtx.Dp(32), func(gtx layout.Context) layout.Dimensions {
						if lel != nil {
							return slackc.LayoutElement(gtx, *lel)
						}
						return a.th.Label(gtx, st.FontSize(15), theme.Regular, fg, label)
					})
				})
				return layout.Dimensions{Size: image.Pt(navW-gtx.Dp(16), gtx.Dp(32))}
			})
		})
	})
	return clicked(b)
}

func (p *prefsModal) btn(k string) *widget.Clickable {
	if c, ok := p.clicks[k]; ok {
		return c
	}
	c := new(widget.Clickable)
	p.clicks[k] = c
	return c
}

func (p *prefsModal) page(gtx layout.Context, sec string) layout.Dimensions {
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
	if sec == "advanced" {
		heading("Advanced")
		at(gtx, image.Pt(0, y), image.Pt(w, gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
			return a.th.Label(gtx, 13, theme.Regular, c.FgMuted, "Plugin settings appear as their own tabs.")
		})
		y += gtx.Dp(28)
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
