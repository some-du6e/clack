package ui

import (
	"context"
	"image"
	"image/color"
	"strings"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/pluginlib"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/shortcut"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// prefsModal is the one Preferences dialog: Slack's left nav with its
// sections in Slack's order (Availability is ui-popups' page, the rest are
// prefs_sections.go and prefs_appearance.go). Changes apply immediately,
// like Slack.
type prefsModal struct {
	a        *App
	clicks   map[string]*widget.Clickable
	section  string
	avail    pagePositioned // popups.AvailabilityPage when linked
	org      originTracker
	taut     *tautPage              // Taut's plugin pages and its custom CSS / config.json / About screens
	slick    *slickPage             // Themes (Slick's page) and Plugin settings' Slick state
	plugins  *pluginsPage           // Preferences > Plugins: installed (both systems) and Browse (the library)
	settings *pluginSettingsPage    // Preferences > Plugin settings
	tabSubs  map[string]*slackc.Sub // plugin Preferences tabs (RenderPreferencesTab)
	tabGen   int                    // App.tautGen the tab subs belong to

	pend      map[string]any // values just written, shown until the write lands
	scroll    prefsScroll    // the page
	scrollFor string         // the section scroll belongs to
	navScroll prefsScroll

	rec       *[]prefsDrawn             // tests: every row the page draws (prefs_form.go)
	mobileOvr *bool                     // Notifications > "Set up mobile overrides" (nil: on when the levels differ)
	nav       reorderList               // Navigation's tab listbox (prefs_reorder.go)
	picker    channelPicker             // Advanced > Exclude these channels (prefs_picker.go)
	lists     prefsLists                // Connected accounts and Privacy's fetched lists (prefs_lists.go)
	themeTab  string                    // Appearance: "slack" or "custom" ("": Slack's default for the theme)
	hexEd     map[string]*widget.Editor // Appearance > Custom theme hex fields
}

// pagePositioned is a page widget that needs its window origin (dropdowns).
type pagePositioned interface {
	View
	SetOrigin(image.Point)
}

var mkAvailability func(a *App) pagePositioned

// prefsTap sees each Preferences dialog showPrefs opens (tests; nil
// otherwise).
var prefsTap func(p *prefsModal)

// prefsNav is Slack's tabs[] for the dialog, in Slack's order. The last
// id must be "advanced": Taut appends its own tabs only to the Preferences
// Tabs. Clack's first ids (navigation, appearance) stay; the rest are
// Slack's own tab ids, and Slack's ids for those two are aliases
// (prefsAliases). slackIcon is Slack's svgIcon name.
var prefsNav = []struct{ id, label, icon, slackIcon string }{
	{"availability", "Availability", "clock", "clock"},
	{"notifications", "Notifications", "bell", "notifications"},
	{"vip", "VIP", "vip", "vip"},
	{"navigation", "Navigation", "sidebar", "navigation-tabs-vertical"},
	{"sidebar", "Home", "home", "home"},
	{"appearance", "Appearance", "edit", "paintbrush"},
	{"messages_media", "Messages & media", "threads", "threads"},
	{"language_region", "Language & region", "globe", "globe"},
	{"accessibility", "Accessibility", "laptop", "laptop"},
	{"mark_as_read", "Mark as read", "check-circle", "check-circle"},
	{"video_audio", "Audio & video", "video", "video"},
	{"salesforce", "Salesforce", "cloud", "sf-cloud"},
	{"connected_accounts", "Connected accounts", "plug", "plug"},
	{"privacy_visibility", "Privacy & visibility", "lock", "lock"},
	{"advanced", "Advanced", "settings", "settings"},
}

// prefsAliases maps Slack's tab ids for sections Clack named first.
var prefsAliases = map[string]string{"tab_rail": "navigation", "themes": "appearance"}

// prefsSection is the section id to show for a requested one (Slack's
// aliases, and the plugin tabs' ids from before they merged).
func prefsSection(id string) string {
	if v, ok := prefsAliases[id]; ok {
		return v
	}
	if v, ok := oldPluginTabs[id]; ok {
		return v
	}
	return id
}

// Slack's Preferences classes (taut's settings CSS targets them).
var (
	clsPrefsTabs  = slackc.Class("c-tabs p-prefs_dialog__tabs c-tabs--vertical")
	clsPrefsMenu  = slackc.Class("p-prefs_dialog__menu c-tabs__tab_menu c-tabs__tab_menu--vertical", "role", "tablist")
	clsPrefsTab   = slackc.Class("c-button-unstyled c-tabs__tab js-tab", "role", "tab").Tag("button").Mods("c-tabs__tab--active")
	clsPrefsPanel = slackc.Class("c-tabs__tab_panel p-prefs_dialog__panel", "role", "tabpanel")
)

func (a *App) openPrefs() { a.openPrefsSection("notifications") }

// OpenPreferences opens Preferences at a section ("notifications"); views
// call it (Activity's "See all notification settings").
func (a *App) OpenPreferences(section string) { a.openPrefsSection(section) }

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

// showPrefs opens the Preferences dialog on section (a Clack section, a
// plugin tab id, or an old id like "taut" or "library"). It is
// openPrefsModal's native and UIHooks.OpenPreferences.
func (a *App) showPrefs(section string) {
	if section == "" {
		section = "notifications"
	}
	if mkModal == nil {
		a.openPrefsMenu()
		return
	}
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}}
	p.selectTab(section)
	if prefsTap != nil {
		prefsTap(p)
	}
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
	if p.plugins != nil {
		p.plugins.close()
	}
	for k, s := range p.tabSubs {
		s.Close()
		delete(p.tabSubs, k)
	}
}

// extraNav is Clack's plugin tabs (Plugins, Themes, Plugin settings: Taut
// and Slick as one system, plugins_prefs.go) below a divider, then the
// plugins' own Preferences tabs, drawn after Slack's tabs[] (kept out of
// tabsProps: plugins patching Tabs expect "advanced" last). Themes is
// Slick's and only shows while Slick runs.
func (p *prefsModal) extraNav() []navExtra {
	var out []navExtra
	for _, t := range pluginNav {
		if t.id == themesTabID && p.a.Slick() == nil {
			continue
		}
		t.divider = len(out) == 0
		out = append(out, t)
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
			p.section = prefsSection(id)
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
		cur = prefsSection(tp.StrOr("currentTabId", cur))
		list = tp.List("tabs")
	}
	var content slackc.Element
	ownContent := true
	ms := clsPrefsMenu.Enter(gtx, 0, 0)
	if ms.Has("background-color") { // a theme's .p-prefs_dialog__menu (Material)
		theme.Fill(gtx, image.Rect(-gtx.Dp(16), 0, navW, h-gtx.Dp(17)), ms.Background(color.NRGBA{}))
	}
	// 15 sections plus Taut's and Slick's tabs outgrow the dialog: the
	// nav scrolls.
	ng := gtx
	ng.Constraints = layout.Constraints{Max: image.Pt(navW, h-gtx.Dp(17))}
	p.navScroll.layout(ng, func(gtx layout.Context) layout.Dimensions {
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
		return layout.Dimensions{Size: image.Pt(navW, y)}
	})
	ms.Leave()
	navT.Pop()

	pageX, pageY := gtx.Dp(249), gtx.Dp(21)
	pageW := w - pageX - gtx.Dp(28)
	at(gtx, image.Pt(pageX, pageY), image.Pt(pageW, h-pageY), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min = image.Point{}
		ps := clsPrefsPanel.Enter(gtx, 0, 0)
		defer ps.Leave()
		if ps.Has("background-color") { // a theme's .p-prefs_dialog__panel (Material)
			theme.Fill(gtx, image.Rectangle{Max: gtx.Constraints.Max}, ps.Background(color.NRGBA{}))
		}
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
	switch prefsSection(id) {
	case pluginsTabID:
		return p.pluginsPage().layout(gtx)
	case pluginSettingsTabID:
		return p.settingsPage().layout(gtx)
	case themesTabID:
		return p.slickPage().layout(gtx, themesTabID)
	}
	if d, ok := p.pluginTab(gtx, id); ok {
		return d
	}
	return p.page(gtx, id)
}

func (p *prefsModal) tautPage() *tautPage {
	if p.taut == nil {
		p.taut = newTautPage(p.a)
	}
	return p.taut
}

func (p *prefsModal) slickPage() *slickPage {
	if p.slick == nil {
		p.slick = newSlickPage(p.a)
	}
	return p.slick
}

func (p *prefsModal) pluginsPage() *pluginsPage {
	if p.plugins == nil {
		p.plugins = newPluginsPage(p.a, p.tautPage(), p.selectTab)
	}
	return p.plugins
}

func (p *prefsModal) settingsPage() *pluginSettingsPage {
	if p.settings == nil {
		p.settings = &pluginSettingsPage{a: p.a, s: p.slickPage(), taut: p.tautPage()}
	}
	return p.settings
}

// selectTab switches the dialog to a tab, as a click on it would. Old
// plugin tab ids land on their new home: "library" and "slick_install" on
// Plugins > Browse; "taut:ID" and "slick_plugins:ID" open a plugin's own
// page under Plugins (the Library's Settings button).
func (p *prefsModal) selectTab(target string) {
	id, plugin, _ := strings.Cut(target, ":")
	switch {
	case id == "taut" && plugin != "":
		p.pluginsPage().openPlugin(pluginlib.Taut, plugin)
	case id == "slick_plugins" && plugin != "":
		p.pluginsPage().openPlugin(pluginlib.Slick, plugin)
	case id == libraryTabID || id == "slick_install":
		p.pluginsPage().setMode("browse")
	case id == "taut" || id == "slick_plugins":
		p.pluginsPage().setMode("installed")
	}
	p.section = prefsSection(id)
	p.a.Invalidate()
}

// navItem draws one tab button at y; label is a string, or lel a plugin's
// label element. Reports a click.
func (p *prefsModal) navItem(gtx layout.Context, y, navW int, id, label string, lel *slackc.Element, icon string, cur bool) bool {
	a := p.a
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
	tc := prefsTabColorsOf(a.th)
	ih := gtx.Dp(29) // Slack: 205×29, radius 4, 15px icon at (9, 7), label at x 32
	at(gtx, image.Pt(0, y), image.Pt(navW-gtx.Dp(16), ih), func(gtx layout.Context) layout.Dimensions {
		// Slack's .c-tabs__tab has border-radius 4px: a theme's background
		// on the tab (.c-tabs__tab--active) rounds with it
		return st.WrapRounded(gtx, 4, func(gtx layout.Context) layout.Dimensions {
			return clickArea(gtx, b, func(gtx layout.Context, s image.Point, hov bool) {
				if cur {
					theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(4), st.Background(tc.active))
				} else if hov {
					theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(4), st.Background(tc.hover))
				}
			}, func(gtx layout.Context) layout.Dimensions {
				fg := tc.text
				if cur {
					fg = tc.activeText
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
	if p.scrollFor != sec {
		p.scroll, p.scrollFor = prefsScroll{}, sec
	}
	if sec == "appearance" { // prefs_appearance.go, owned with the account theme
		return p.scroll.layout(gtx, func(gtx layout.Context) layout.Dimensions {
			d := p.appearancePage(gtx)
			d.Size.Y += gtx.Dp(24)
			return d
		})
	}
	fn := pageFns[sec]
	if sec == "availability" { // popups' page isn't linked: notifications
		fn = (*prefsModal).notificationsPage
	}
	if fn == nil {
		return layout.Dimensions{}
	}
	return p.scroll.layout(gtx, func(gtx layout.Context) layout.Dimensions {
		f := p.form(gtx)
		fn(p, f)
		f.y += gtx.Dp(24) // room under the last row
		return f.dims()
	})
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
		{Label: map[bool]string{true: "Show sidebar", false: "Hide sidebar"}[a.sidebar.hidden], Icon: "sidebar", Hint: shortcut.Format("Mod+Shift+D"), Do: func() { a.sidebar.hidden = !a.sidebar.hidden }},
	})
}

// prefsTabColors are the vertical tab's colours. Slack paints them from
// tokens (.c-tabs__tab_menu--vertical .c-tabs__tab: color
// var(--dt_color-content-pry); --active: background-color
// var(--dt_color-base-inv-hgl-1), color var(--dt_color-content-inv-pry);
// :hover: var(--dt_color-base-pry-hover) over var(--dt_color-base-pry)),
// so a theme's tokens recolour them; with none overridden they are the
// measured ones. A theme's raw rule on the tab still wins (Styled).
type prefsTabColors struct{ text, active, activeText, hover color.NRGBA }

func prefsTabColorsOf(th *theme.Theme) prefsTabColors {
	c := &th.C
	tc := prefsTabColors{text: c.Fg, active: c.Link, activeText: c.SendText, hover: c.BgHover}
	t := th.Tokens
	if t == nil || !t.Overridden() {
		return tc
	}
	theme.SetColor(t, &tc.text, "var(--dt_color-content-pry)")
	theme.SetColor(t, &tc.active, "var(--dt_color-base-inv-hgl-1)")
	theme.SetColor(t, &tc.activeText, "var(--dt_color-content-inv-pry)")
	// the hover tint over the tab's base, either of them the theme's
	h, hOwn, hOK := t.Color("var(--dt_color-base-pry-hover)")
	base, bOwn, bOK := t.Color("var(--dt_color-base-pry)")
	if hOK && bOK && (hOwn || bOwn) {
		tc.hover = theme.Over(h, base)
	}
	return tc
}

// otlTer is Slack's --dt_color-otl-ter (hairlines).
func otlTer(dark bool) color.NRGBA {
	if dark {
		return hex8(0x797c814d)
	}
	return hex8(0x5e5d6021)
}
