package ui

import (
	"context"
	"image"
	"image/color"

	"gioui.org/layout"
	"gioui.org/op"
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
	taut    *tautPage              // Preferences > Taut, made on first view
	slick   *slickPage             // Preferences > Slick's tabs
	tabSubs map[string]*slackc.Sub // plugin Preferences tabs (RenderPreferencesTab)
	tabGen  int                    // App.tautGen the tab subs belong to
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

// openPrefsSection opens Preferences on a section through Slack's
// openPrefsModal thunk (plugins may redirect it; Taut opens its own tab
// with activeSection "advanced"). popups' OpenPreferencesSection hook and
// "Edit your working hours" land here.
func (a *App) openPrefsSection(section string) {
	a.dispatch("openPrefsModal", map[string]any{"activeSection": section},
		func(ctx context.Context, args map[string]any) (any, error) {
			sec := argStr(args, "activeSection")
			a.Post(func() { a.showPrefs(sec) })
			return nil, nil
		})
}

// showPrefs opens the Preferences dialog on section (a Clack section,
// "taut", or a plugin tab id). It is openPrefsModal's native and
// UIHooks.OpenPreferences.
func (a *App) showPrefs(section string) {
	if section == "" {
		section = "notifications"
	}
	if mkModal == nil {
		a.openPrefsMenu()
		return
	}
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}, section: section}
	if mkAvailability != nil {
		p.avail = mkAvailability(a)
	}
	mk := mkModal
	if mkPrefsModal != nil {
		mk = mkPrefsModal
	}
	a.ShowOverlay(mk(a, "Preferences", p.layout, p.close))
}

// close unmounts the JS roots the dialog mounted (Taut editors, data
// panels, plugin tabs).
func (p *prefsModal) close() {
	if p.taut != nil {
		p.taut.close()
	}
	if p.slick != nil {
		p.slick.close()
	}
	for k, s := range p.tabSubs {
		s.Close()
		delete(p.tabSubs, k)
	}
}

// extraNav is Clack's Taut tab and the plugins' own Preferences tabs,
// drawn after Slack's tabs[] (kept out of tabsProps: plugins patching Tabs
// expect "advanced" last).
func (p *prefsModal) extraNav() []navExtra {
	out := []navExtra{{id: "taut", label: "Taut", icon: "code"}}
	if p.a.Slick() != nil { // Slick's tabs, a divider above the first
		for i, t := range slickTabs {
			out = append(out, navExtra{id: t.id, label: t.label, icon: t.icon, divider: i == 0})
		}
	}
	for _, t := range p.a.tautTabs {
		icon := "settings" // none: the host (Advanced) tab's icon, like Slick
		if t.Icon != "" {
			icon = icons.Slack(t.Icon)
		}
		out = append(out, navExtra{id: t.ID, label: t.Label, icon: icon})
	}
	return out
}

type navExtra struct {
	id, label, icon string
	divider         bool // Slick's: 17px margin and a 1px rule 8px above the tab
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
	// Slack's p-prefs_dialog__layout (800×624 under the 76px header): a 1px
	// top line, the nav at (16, 17) with 205×29 items every 33px, the page
	// at (249, 21), 523 wide.
	w := gtx.Constraints.Max.X
	navW := gtx.Dp(205 + 16)
	h := gtx.Dp(624)
	theme.Fill(gtx, image.Rect(0, 0, w, max(1, gtx.Dp(1))), a.th.C.Border)
	navT := op.Offset(image.Pt(gtx.Dp(16), gtx.Dp(17))).Push(gtx.Ops)
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
			y += gtx.Dp(33)
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
			y += gtx.Dp(33)
		}
	}
	for _, it := range p.extraNav() {
		if listHas(list, it.id) {
			continue
		}
		if it.divider {
			y += gtx.Dp(17)
			theme.Fill(gtx, image.Rect(0, y-gtx.Dp(9), navW-gtx.Dp(16), y-gtx.Dp(8)), p.a.th.OnBg(otlTer(p.a.th.Dark)))
		}
		if p.navItem(gtx, y, navW, it.id, it.label, nil, it.icon, cur == it.id) {
			if tp != nil && tp.Has("onTabChange") && list != nil {
				tp.Invoke("onTabChange", it.id, slackc.Props{"type": "click"})
			}
			p.section = it.id
			a.Invalidate()
		}
		y += gtx.Dp(33)
	}
	ms.Leave()
	navT.Pop()

	pageX, pageY := gtx.Dp(249), gtx.Dp(21)
	pageW := w - pageX - gtx.Dp(28)
	at(gtx, image.Pt(pageX, pageY), image.Pt(pageW, h-pageY), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min = image.Point{}
		ps := clsPrefsPanel.Enter(gtx, 0, 0)
		defer ps.Leave()
		if !ownContent {
			return slackc.LayoutElement(gtx, content)
		}
		return p.section1(gtx, cur, org.Add(image.Pt(pageX, pageY)))
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
	for _, it := range p.extraNav() {
		if it.id == id {
			return true
		}
	}
	return false
}

func listHas(list []any, id string) bool {
	for _, x := range list {
		if t, _ := x.(slackc.Props); t != nil && t.StrOr("id", "") == id {
			return true
		}
	}
	return false
}

// pluginTab lays out a plugin's own Preferences tab (Taut
// registerPreferencesTab), mounted through Bridge.RenderPreferencesTab.
func (p *prefsModal) pluginTab(gtx layout.Context, id string) (layout.Dimensions, bool) {
	t := p.a.Taut()
	known := false
	for _, x := range p.a.tautTabs {
		known = known || x.ID == id
	}
	if t == nil || !known {
		return layout.Dimensions{}, false
	}
	if p.tabGen != p.a.tautGen { // fresh runtime: the old roots are gone
		for k, s := range p.tabSubs {
			s.Close()
			delete(p.tabSubs, k)
		}
		p.tabGen = p.a.tautGen
	}
	if p.tabSubs == nil {
		p.tabSubs = map[string]*slackc.Sub{}
	}
	s := p.tabSubs[id]
	if s == nil {
		s = slackc.NewSub()
		p.tabSubs[id] = s
		t.Bridge().RenderPreferencesTab(s.Root(), id)
	}
	if !s.Ready() {
		return p.a.th.Label(gtx, 13, theme.Regular, p.a.th.C.FgSubtle, "Loading…"), true
	}
	return s.Layout(gtx), true
}

// section1 draws one of Clack's sections; org is its window origin.
func (p *prefsModal) section1(gtx layout.Context, id string, org image.Point) layout.Dimensions {
	if id == "availability" && p.avail != nil {
		p.avail.SetOrigin(org)
		return p.avail.Layout(gtx)
	}
	if isSlickTab(id) {
		if p.slick == nil {
			p.slick = newSlickPage(p.a)
		}
		return p.slick.layout(gtx, id)
	}
	if id == "taut" {
		if p.taut == nil {
			p.taut = newTautPage(p.a)
		}
		return p.taut.layout(gtx)
	}
	if d, ok := p.pluginTab(gtx, id); ok {
		return d
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
	ih := gtx.Dp(29) // Slack: 205×29, radius 4, 15px icon at (9, 7), label at x 32
	at(gtx, image.Pt(0, y), image.Pt(navW-gtx.Dp(16), ih), func(gtx layout.Context) layout.Dimensions {
		return st.Wrap(gtx, func(gtx layout.Context) layout.Dimensions {
			return clickArea(gtx, b, func(gtx layout.Context, s image.Point, hov bool) {
				if cur {
					theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(4), c.Link)
				} else if hov {
					theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(4), c.BgHover)
				}
			}, func(gtx layout.Context) layout.Dimensions {
				fg := c.Fg
				if cur {
					fg = c.SendText
				}
				fg = st.Color(fg)
				if icon != "" {
					at(gtx, image.Pt(gtx.Dp(9), gtx.Dp(7)), image.Pt(gtx.Dp(15), gtx.Dp(15)), func(gtx layout.Context) layout.Dimensions {
						return icons.Get(icon)(gtx, 15, fg)
					})
				}
				at(gtx, image.Pt(gtx.Dp(32), 0), image.Pt(navW, ih), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, ih, func(gtx layout.Context) layout.Dimensions {
						if lel != nil {
							return slackc.LayoutElement(gtx, *lel)
						}
						return a.th.Label(gtx, st.FontSize(15), theme.Regular, fg, label)
					})
				})
				return layout.Dimensions{Size: image.Pt(navW-gtx.Dp(16), ih)}
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
			return a.th.Label(gtx, 18, theme.Bold, c.Fg, t) // Slack's section h2: 700 18px/24
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
		gap()
		heading("Input options")
		option("plaintext", "Format messages with markup", "The text formatting toolbar won’t show in the composer.", a.plainTextOn(), false, a.togglePlainText)
		gap()
		heading("Live updates")
		rtHint := "Opens Slack's realtime connection. Off: messages refresh every 20 s"
		if a.opts.NoRealtime {
			rtHint = "Turned off with -no-realtime or CLACK_NO_REALTIME"
		}
		option("realtime", "Live updates (typing, presence, instant messages)", rtHint, a.realtimeOn(), false, a.toggleRealtime)
		at(gtx, image.Pt(gtx.Dp(28), y), image.Pt(w-gtx.Dp(28), gtx.Dp(40)), func(gtx layout.Context) layout.Dimensions {
			return a.th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 18, Color: c.FgMuted, MaxLines: 2},
				"Clack shares your browser's Slack session. On some Enterprise Grid workspaces Slack can sign that session out everywhere if it notices.")
		})
		y += gtx.Dp(44)
		gap()
		heading("Updates")
		option("updates", "Check for updates", "Shows a banner when a new Clack release is on GitHub. Nothing downloads on its own.", a.updateChecksOn(), false, a.toggleUpdateChecks)
		if buildVersion != "" {
			at(gtx, image.Pt(gtx.Dp(28), y), image.Pt(w-gtx.Dp(28), gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
				return a.th.Label(gtx, 13, theme.Regular, c.FgMuted, "You have Clack "+buildVersion)
			})
			y += gtx.Dp(28)
		}
	}
	if sec == "notifications" || sec == "availability" {
		heading("Notifications")
		option("notify", "Desktop notifications for DMs and mentions", "Muted conversations never notify", a.notifyOn(), false, a.toggleNotify)
	}
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// setEmojiStyle goes through setUserPrefByApi (Slack's emoji_style pref),
// applied locally.
func (a *App) setEmojiStyle(s string) {
	a.dispatch("setUserPrefByApi", map[string]any{"pref": "emoji_style", "value": s},
		func(ctx context.Context, args map[string]any) (any, error) {
			v := argStr(args, "value")
			a.Post(func() {
				a.th.EmojiStyle = v
				a.prefs.EmojiStyle = v
				a.savePrefs()
				a.Invalidate()
			})
			return nil, nil
		})
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

// otlTer is Slack's --dt_color-otl-ter (hairlines).
func otlTer(dark bool) color.NRGBA {
	if dark {
		return hex8(0x797c814d)
	}
	return hex8(0x5e5d6021)
}
