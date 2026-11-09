package ui

import (
	"context"
	"errors"
	"image"
	"image/color"
	"sort"
	"strconv"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/pluginlib"
	"github.com/some-du6e/yoloslackgoclient/internal/slick"
	"github.com/some-du6e/yoloslackgoclient/internal/taut"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/openurl"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Clack runs Taut's and Slick's plugins side by side and shows them as one
// plugin system. After Slack's own Preferences tabs come:
//
//   - Plugins: Installed (every plugin of both systems in one list, tagged
//     with its system, by shared category, with Slick's search and filter
//     chips; a row's switch turns it on in its own system, and the row opens
//     that system's own page for it) and Browse (the plugin library,
//     library_prefs.go), sharing one search box; "Install from file or
//     URL" works out which system a file belongs to.
//   - Themes: Slick's themes page.
//   - Plugin settings: what isn't per plugin: Slick's global switch, both
//     config folders, updates, config errors, Taut's custom CSS and
//     config.json, and the credits.
//
// The pages reuse Taut's and Slick's own drawing (taut_prefs.go,
// slick_prefs.go). Plugins' own tabs follow, unchanged.

const (
	pluginsTabID        = "plugins"
	pluginSettingsTabID = "plugin_settings"
	themesTabID         = "slick_themes" // Slick's own id for its themes tab
)

// pluginNav is Clack's plugin tabs, after a divider below Slack's.
var pluginNav = []navExtra{
	{id: pluginsTabID, label: "Plugins", icon: "apps"},
	{id: themesTabID, label: "Themes", icon: "paintbrush"},
	{id: pluginSettingsTabID, label: "Plugin settings", icon: "settings"},
}

// oldPluginTabs are the tab ids the plugin tabs had before they merged
// (Taut's, Slick's, the Library's): opening one opens its new home.
var oldPluginTabs = map[string]string{
	"taut": pluginsTabID, "slick_plugins": pluginsTabID, "slick_install": pluginsTabID, libraryTabID: pluginsTabID,
	"slick": pluginSettingsTabID,
}

// ---- the merged plugin list ----

// unifiedPlugin is one installed plugin of either system.
type unifiedPlugin struct {
	System                string // pluginlib.Taut or pluginlib.Slick
	ID, Name, Description string // Description is plain text
	Category              pluginCategory
	Enabled, User         bool
	HackClubOnly          bool
	Problem               string // why it isn't running, or what needs a look
	ProblemErr            bool   // Problem is an error (red)
	Broken                bool   // counts under the Problems chip
	hay                   string // what the search matches

	taut  taut.PluginInfo
	slick slick.PluginInfo
}

func (u unifiedPlugin) key() string { return u.System + "/" + u.ID }

// unifiedPlugins merges both systems' installed plugins (either settings
// may be nil when its system isn't running). mentions renders Slick's
// `<@U…>` author lists.
func unifiedPlugins(ts *taut.Settings, ss *slick.Settings, mentions func(string) string) []unifiedPlugin {
	var out []unifiedPlugin
	if ts != nil {
		for _, c := range ts.Categories {
			for _, pl := range c.Plugins {
				u := unifiedPlugin{System: pluginlib.Taut, ID: pl.ID, Name: pl.Name, Description: plainMrkdwn(pl.Description),
					Category: sharedCategory(pluginlib.Taut, pl.Category), Enabled: pl.Enabled, User: pl.IsUser,
					HackClubOnly: pl.HackClubOnly, taut: pl}
				switch {
				case pl.Error != "":
					u.Problem, u.ProblemErr, u.Broken = pl.Error, true, true
				case len(pl.Problems) > 0:
					u.Problem, u.ProblemErr, u.Broken = "Some of its settings aren't valid. Open it to see which.", true, true
				case pl.Enabled && !pl.Running && ts.RuntimeError == "":
					u.Problem = "Starting…"
				}
				var authors []string
				for _, a := range pl.Authors {
					authors = append(authors, a.Name)
				}
				u.hay = strings.Join(append([]string{pl.ID, pl.Name, u.Description, u.Category.Label, "taut"}, authors...), " ")
				out = append(out, u)
			}
		}
	}
	if ss != nil {
		for _, pl := range ss.Plugins {
			u := unifiedPlugin{System: pluginlib.Slick, ID: pl.ID, Name: pl.Name, Description: plainMrkdwn(pl.Description),
				Category: sharedCategory(pluginlib.Slick, pl.Category), Enabled: pl.Enabled, User: pl.User, slick: pl}
			u.Problem, u.ProblemErr = (*slickPage)(nil).problem(pl, *ss)
			u.Broken = pl.Enabled && pl.StartError != ""
			authors := pl.Authors
			if mentions != nil {
				authors = mentions(authors)
			}
			u.hay = strings.Join([]string{pl.ID, pl.Name, u.Description, u.Category.Label, authors, "slick"}, " ")
			out = append(out, u)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if ri, rj := categoryRank(out[i].Category.Key), categoryRank(out[j].Category.Key); ri != rj {
			return ri < rj
		}
		if ni, nj := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name); ni != nj {
			return ni < nj
		}
		return out[i].System > out[j].System // Taut before Slick
	})
	for i := range out {
		out[i].hay = strings.ToLower(out[i].hay)
	}
	return out
}

// unifiedFilters are Slick's chips over both systems: All, On, Off, and
// User plugins and Problems when there are some.
func unifiedFilters(ps []unifiedPlugin) []slick.Filter {
	on, user, broken := 0, 0, 0
	for _, u := range ps {
		if u.Enabled {
			on++
		}
		if u.User {
			user++
		}
		if u.Broken {
			broken++
		}
	}
	fs := []slick.Filter{{ID: "all", Label: "All", Count: len(ps)}, {ID: "on", Label: "On", Count: on}, {ID: "off", Label: "Off", Count: len(ps) - on}}
	if user > 0 {
		fs = append(fs, slick.Filter{ID: "user", Label: "User plugins", Count: user})
	}
	if broken > 0 {
		fs = append(fs, slick.Filter{ID: "problems", Label: "Problems", Count: broken})
	}
	return fs
}

// unifiedMatches applies a chip and the search words.
func unifiedMatches(u unifiedPlugin, filter string, words []string) bool {
	switch filter {
	case "on":
		if !u.Enabled {
			return false
		}
	case "off":
		if u.Enabled {
			return false
		}
	case "user":
		if !u.User {
			return false
		}
	case "problems":
		if !u.Broken {
			return false
		}
	}
	for _, w := range words {
		if !strings.Contains(u.hay, w) {
			return false
		}
	}
	return true
}

// unifiedSection is one category card of the Installed list.
type unifiedSection struct {
	Category pluginCategory
	On       int
	Plugins  []unifiedPlugin
}

// unifiedSections groups an ordered list by category.
func unifiedSections(ps []unifiedPlugin) []unifiedSection {
	var out []unifiedSection
	for _, u := range ps {
		if len(out) == 0 || out[len(out)-1].Category.Key != u.Category.Key {
			out = append(out, unifiedSection{Category: u.Category})
		}
		s := &out[len(out)-1]
		s.Plugins = append(s.Plugins, u)
		if u.Enabled {
			s.On++
		}
	}
	return out
}

// ---- Preferences > Plugins ----

type pluginsPage struct {
	a    *App
	s    *slickPage   // drawing parts, the shared search, Installed's chip, the scroll, Slick's plugin page (s.open)
	taut *tautPage    // a Taut plugin's page (screen "plugin")
	lib  *libraryPage // Browse
	mode string       // installed | browse

	toolbarH int
	selTab   image.Rectangle // the selected mode tab (its underline)
	inst     *pluginInstaller
}

func newPluginsPage(a *App, tp *tautPage, openTab func(string)) *pluginsPage {
	pp := &pluginsPage{a: a, s: newSlickPage(a), taut: tp, lib: newLibraryPage(a, openTab), mode: "installed"}
	pp.s.tab = "slick_plugins" // its detail draws through slickPage.layout
	return pp
}

func (pp *pluginsPage) close() {
	pp.s.close()
	pp.lib.close()
}

// setMode switches Installed / Browse (the search stays).
func (pp *pluginsPage) setMode(m string) {
	if m != pp.mode {
		pp.mode = m
		pp.s.list.Position = layout.Position{}
		pp.a.Invalidate()
	}
}

// installed is the merged list now.
func (pp *pluginsPage) installed() []unifiedPlugin {
	var ts *taut.Settings
	var ss *slick.Settings
	if t := pp.a.Taut(); t != nil {
		v := t.Settings()
		ts = &v
	}
	if s := pp.a.Slick(); s != nil {
		v := s.Settings()
		ss = &v
	}
	return unifiedPlugins(ts, ss, pp.s.mentions)
}

// filters are Installed's chips; an unknown chip falls back to All.
func (pp *pluginsPage) filters(ps []unifiedPlugin) []slick.Filter {
	fs := unifiedFilters(ps)
	ok := false
	for _, f := range fs {
		ok = ok || f.ID == pp.s.filter
	}
	if !ok {
		pp.s.filter = "all"
	}
	return fs
}

// openPlugin shows a plugin's own page: Taut's, or Slick's detail.
func (pp *pluginsPage) openPlugin(system, id string) {
	if system == pluginlib.Taut {
		pp.s.open = ""
		pp.taut.go_("plugin", id)
		return
	}
	if pp.taut.screen == "plugin" {
		pp.taut.go_("main", "")
	}
	pp.s.listBack = pp.s.list.Position
	pp.s.open, pp.s.daysOpen = id, ""
	pp.s.list.Position = layout.Position{}
	pp.a.Invalidate()
}

func (pp *pluginsPage) layout(gtx layout.Context) layout.Dimensions {
	a := pp.a
	if pp.taut.screen == "plugin" && a.Taut() != nil {
		return pp.taut.layout(gtx)
	}
	if pp.s.open != "" && a.Slick() != nil {
		return pp.s.layout(gtx, "slick_plugins")
	}
	p := pp.s
	p.k.gtxNow = gtx
	pp.lib.s.k.gtxNow = gtx
	sz := gtx.Constraints.Max
	ps := pp.installed() // Installed's rows, and its count on the tab
	fs := pp.filters(ps)
	if pp.mode == "browse" {
		fs = pp.lib.chips()
	}
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(sz.X-gtx.Dp(8), 1<<20)}
	pp.toolbarH = pp.toolbar(g, fs, len(ps)).Size.Y
	bar := m.Stop()
	p.list.List.Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Min: image.Pt(sz.X, 0), Max: image.Pt(sz.X-gtx.Dp(8), 1<<24)}
		f := &flow{gtx: gtx, w: gtx.Constraints.Max.X}
		f.y += pp.toolbarH + gtx.Dp(20)
		words := strings.Fields(strings.ToLower(p.query.Text()))
		if pp.mode == "browse" {
			pp.lib.body(f, words)
		} else {
			pp.installedList(f, ps, words)
		}
		f.gap(24)
		return layout.Dimensions{Size: image.Pt(sz.X, f.y)}
	})
	// pinned above the scrolling list, opaque
	theme.Fill(gtx, image.Rect(0, 0, sz.X-gtx.Dp(8), pp.toolbarH), p.colors().base)
	bar.Add(gtx.Ops)
	return layout.Dimensions{Size: sz}
}

// toolbar is Slack's underline tab strip (Installed, Browse) on a
// hairline with "Install from file or URL" at its right, then the shared
// search with the mode's filter chips (Slick's toolbar).
func (pp *pluginsPage) toolbar(gtx layout.Context, fs []slick.Filter, installed int) layout.Dimensions {
	p := pp.s
	cl := p.colors()
	w := gtx.Constraints.Max.X
	td := pp.modeTabs(gtx, installed)
	h := td.Size.Y
	b := p.k.btn("install.open")
	hit := b.Clicked(gtx)
	m := op.Record(gtx.Ops)
	bd := popups.Button(gtx, p.a.th, b, popups.BtnOutline, popups.BtnSmall, "Install from file or URL", "", 0)
	call := m.Stop()
	st := op.Offset(image.Pt(w-bd.Size.X, (h-gtx.Dp(1)-bd.Size.Y)/2)).Push(gtx.Ops)
	call.Add(gtx.Ops)
	st.Pop()
	if hit {
		pp.openInstaller()
	}
	// the strip's hairline, under the selected tab's underline
	theme.Fill(gtx, image.Rect(0, h-gtx.Dp(1), w, h), cl.otlTer)
	pp.modeUnderline(gtx, h)
	y := h + gtx.Dp(16)
	chips, placeholder := p, "Search installed plugins"
	if pp.mode == "browse" {
		chips, placeholder = pp.lib.s, "Search the plugin library"
	}
	d := at(gtx, image.Pt(0, y), image.Pt(w, 1<<20), func(gtx layout.Context) layout.Dimensions {
		return chips.searchBar(gtx, &p.query, placeholder, fs)
	})
	return layout.Dimensions{Size: image.Pt(w, y+d.Size.Y+gtx.Dp(10))}
}

// pluginModes are the Plugins tab's two views.
var pluginModes = []struct{ id, label string }{{"installed", "Installed"}, {"browse", "Browse"}}

// modeTabColors are the strip's colours: Slack's horizontal c-tabs
// (Activity, channel details; views.tabStrip) with the underline from
// --dt_color-theme-base-pry, so a theme recolours it.
func (pp *pluginsPage) modeTabColors() (line, hover color.NRGBA) {
	th := pp.a.th
	line, hover = rgbT(0x83388a), th.OnBg(hex8(0x611f6912))
	if th.Dark {
		line, hover = rgbT(0xe3ceeb), th.OnBg(hex8(0xefe1f514))
	}
	if t := th.Tokens; t != nil && t.Overridden() {
		theme.SetColor(t, &line, "var(--dt_color-theme-base-pry)")
	}
	return line, hover
}

// modeTabs draws Installed (with its count) and Browse (an accent dot
// while library updates wait): padding 9px 8px, 13px bold, selected in the
// primary colour, hover tint with rounded top corners. It records where
// the selected tab is for modeUnderline.
func (pp *pluginsPage) modeTabs(gtx layout.Context, installed int) layout.Dimensions {
	p := pp.s
	cl := p.colors()
	_, hover := pp.modeTabColors()
	updates := 0
	if pp.lib.ix != nil {
		for _, g := range libraryGroups(libraryEntries(pp.lib.ix)) {
			if _, u := pp.lib.groupStates(g); len(u) > 0 {
				updates++
			}
		}
	}
	x, h := 0, gtx.Dp(9+16+8)+gtx.Dp(1)
	for _, md := range pluginModes {
		b := p.k.btn("mode." + md.id)
		sel := pp.mode == md.id
		count := ""
		if md.id == "installed" && installed > 0 {
			count = strconv.Itoa(installed)
		}
		lw := p.a.th.Measure(gtx, 13, theme.Bold, md.label)
		tw := gtx.Dp(8) + lw + gtx.Dp(8)
		if count != "" {
			tw += gtx.Dp(6) + p.a.th.Measure(gtx, 13, theme.Regular, count)
		}
		dot := md.id == "browse" && updates > 0
		if dot {
			tw += gtx.Dp(8 + 8)
		}
		at(gtx, image.Pt(x, 0), image.Pt(tw, h-gtx.Dp(1)), func(gtx layout.Context) layout.Dimensions {
			th := h - gtx.Dp(1)
			return clickArea(gtx, b, func(gtx layout.Context, sz image.Point, hov bool) {
				if hov && !sel {
					r := gtx.Dp(8)
					paintRRectTop(gtx, image.Rectangle{Max: sz}, r, hover)
				}
			}, func(gtx layout.Context) layout.Dimensions {
				fg := cl.sec
				if sel {
					fg = cl.pry
				}
				cx := gtx.Dp(8)
				at(gtx, image.Pt(cx, gtx.Dp(9)), image.Pt(lw+2, gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
					return p.k.label(gtx, 13, theme.Bold, fg, md.label)
				})
				cx += lw
				if count != "" {
					cx += gtx.Dp(6)
					at(gtx, image.Pt(cx, gtx.Dp(9)), image.Pt(tw, gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
						return p.k.label(gtx, 13, theme.Regular, cl.ter, count)
					})
				}
				if dot {
					d := gtx.Dp(8)
					theme.Circle(gtx, image.Pt(tw-gtx.Dp(8)-d/2, gtx.Dp(9)+gtx.Dp(8)), d, p.a.th.C.Mention)
				}
				return layout.Dimensions{Size: image.Pt(tw, th)}
			})
		})
		if sel {
			pp.selTab = image.Rect(x, 0, x+tw, h)
		}
		if clicked(b) {
			pp.setMode(md.id)
		}
		x += tw
	}
	return layout.Dimensions{Size: image.Pt(x, h)}
}

// modeUnderline is the selected tab's 2px underline, over the hairline.
func (pp *pluginsPage) modeUnderline(gtx layout.Context, h int) {
	line, _ := pp.modeTabColors()
	r := pp.selTab
	theme.Fill(gtx, image.Rect(r.Min.X, h-gtx.Dp(2), r.Max.X, h), line)
}

// paintRRectTop fills r with its top corners rounded.
func paintRRectTop(gtx layout.Context, r image.Rectangle, rad int, c color.NRGBA) {
	rr := clip.RRect{Rect: r, NE: rad, NW: rad}
	paint.FillShape(gtx.Ops, c, rr.Op(gtx.Ops))
}

// installedList draws the merged list by category, or why it's empty.
func (pp *pluginsPage) installedList(f *flow, ps []unifiedPlugin, words []string) {
	p := pp.s
	a := pp.a
	if a.Taut() == nil && a.Slick() == nil {
		p.hint(f, "Plugins start once "+AppName+" has signed in to Slack.")
		return
	}
	var rows []unifiedPlugin
	for _, u := range ps {
		if unifiedMatches(u, p.filter, words) {
			rows = append(rows, u)
		}
	}
	for _, sec := range unifiedSections(rows) {
		fns := make([]func(gtx layout.Context, w int) layout.Dimensions, len(sec.Plugins))
		for i, u := range sec.Plugins {
			fns[i] = func(gtx layout.Context, w int) layout.Dimensions { return pp.row(gtx, w, u) }
		}
		on := sec.On
		p.card(f, sec.Category.Label, sec.Category.Icon, func(gtx layout.Context) layout.Dimensions {
			if on == 0 {
				return layout.Dimensions{}
			}
			return p.k.label(gtx, 13, theme.Regular, p.colors().ter, strconv.Itoa(on)+" on")
		}, fns)
	}
	switch {
	case len(rows) > 0:
	case len(ps) == 0:
		p.empty(f, "No plugins installed yet. Find one under Browse, or install one from a file or URL.")
	case len(words) > 0:
		p.empty(f, "No plugins match “"+strings.Join(words, " ")+"”.")
	default:
		p.empty(f, "No plugins in this view.")
	}
}

// row is Slick's plugin row with a tag for the plugin's system: name,
// tags, description, what's wrong, its switch and a caret.
func (pp *pluginsPage) row(gtx layout.Context, w int, u unifiedPlugin) layout.Dimensions {
	p := pp.s
	cl := p.colors()
	b := p.k.btn("row." + u.key())
	swW, chevW, gap := gtx.Dp(28), gtx.Dp(16), gtx.Dp(14)
	tx := gtx.Dp(16)
	tw := w - tx - gtx.Dp(12) - swW - chevW - 2*gap
	textCol := func(gtx layout.Context) layout.Dimensions {
		nd := p.a.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Weight: theme.Bold, Color: cl.pry, MaxLines: 1}, u.Name)
		bx := min(nd.Size.X, tw) + gtx.Dp(8)
		badge := func(label string) {
			d := at(gtx, image.Pt(bx, gtx.Dp(1)), image.Pt(gtx.Dp(200), gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
				return p.badge(gtx, label, "", false)
			})
			bx += d.Size.X + gtx.Dp(6)
		}
		badge(systemName(u.System))
		if u.HackClubOnly {
			badge("Hack Club only")
		}
		y := gtx.Dp(22)
		if u.Description != "" {
			y += at(gtx, image.Pt(0, y), image.Pt(tw, 1<<20), func(gtx layout.Context) layout.Dimensions {
				return p.a.th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 18, Color: cl.sec, MaxLines: 2, Wrap: text.WrapWords}, u.Description)
			}).Size.Y
		}
		if u.Problem != "" {
			col := cl.ter
			if u.ProblemErr {
				col = cl.imp
			}
			y += gtx.Dp(4)
			y += at(gtx, image.Pt(0, y), image.Pt(tw, 1<<20), func(gtx layout.Context) layout.Dimensions {
				return p.a.th.Layout(gtx, theme.TextStyle{Size: 12, Weight: theme.Bold, Color: col, Wrap: text.WrapWords}, u.Problem)
			}).Size.Y
		}
		return layout.Dimensions{Size: image.Pt(tw, y)}
	}
	m := op.Record(gtx.Ops)
	td := at(gtx, image.Point{}, image.Pt(tw, 1<<20), textCol)
	tcall := m.Stop()
	h := td.Size.Y + gtx.Dp(24)
	d := clickArea(gtx, b, func(gtx layout.Context, s image.Point, hov bool) {
		if hov {
			theme.Fill(gtx, image.Rectangle{Max: s}, cl.hover)
		}
	}, func(gtx layout.Context) layout.Dimensions {
		st := op.Offset(image.Pt(tx, gtx.Dp(12))).Push(gtx.Ops)
		tcall.Add(gtx.Ops)
		st.Pop()
		at(gtx, image.Pt(w-gtx.Dp(12)-chevW, (h-gtx.Dp(16))/2), image.Pt(chevW, chevW), func(gtx layout.Context) layout.Dimensions {
			return icons.Get("caret-right")(gtx, 16, cl.ter)
		})
		return layout.Dimensions{Size: image.Pt(w, h)}
	})
	// the switch sits above the row's click area (its clicks don't open the page)
	at(gtx, image.Pt(w-gtx.Dp(12)-chevW-gap-swW, (h-gtx.Dp(16))/2), image.Pt(swW, gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
		d, c := p.sw(gtx, "en."+u.key(), u.Enabled, false)
		if c {
			pp.setEnabled(u, !u.Enabled)
		}
		return d
	})
	if clicked(b) {
		pp.openPlugin(u.System, u.ID)
	}
	return d
}

// setEnabled turns a plugin on or off in its own system.
func (pp *pluginsPage) setEnabled(u unifiedPlugin, on bool) {
	if u.System == pluginlib.Slick {
		pp.s.setEnabled(u.slick, on) // asks first for a plugin that keeps messages
		return
	}
	t := pp.a.Taut()
	if t == nil {
		return
	}
	a, id := pp.a, u.ID
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if err := t.SetEnabled(ctx, id, on); err != nil {
			a.Post(func() { a.Toast(err.Error()) })
		}
		a.Invalidate()
	}()
}

// ---- Install from file or URL ----

// pluginInstaller is the "Install from file or URL" dialog: one URL box
// and one file picker for both systems. A file with Slick's plugin header
// goes to Slick (after Slick's confirmation), anything else to Taut.
type pluginInstaller struct {
	pp     *pluginsPage
	urlEd  widget.Editor
	busy   bool
	status string
	failed bool
}

func (pp *pluginsPage) openInstaller() {
	if pp.inst == nil {
		pp.inst = &pluginInstaller{pp: pp}
		pp.inst.urlEd.SingleLine, pp.inst.urlEd.Submit = true, true
	}
	in := pp.inst
	in.status, in.failed = "", false
	if mkModal == nil {
		return
	}
	pp.a.ShowOverlay(mkModal(pp.a, "Install from file or URL", in.layout, nil))
}

// layout is the dialog body: Slick's install card for both systems.
func (in *pluginInstaller) layout(gtx layout.Context) layout.Dimensions {
	p := in.pp.s
	for {
		ev, ok := in.urlEd.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.SubmitEvent); ok {
			in.installURL()
		}
	}
	f := &flow{gtx: gtx, w: gtx.Constraints.Max.X}
	p.hint(f, "A Taut or Slick plugin, built by its project's own build. "+AppName+" works out which it is.")
	f.gap(16)
	p.card(f, "", "", nil, []func(gtx layout.Context, w int) layout.Dimensions{
		func(gtx layout.Context, w int) layout.Dimensions {
			return p.field(gtx, w, "From a URL", "Slick plugins installed from a URL are re-downloaded in the background and updated when the file changes.", "", true,
				func(gtx layout.Context, iw int) layout.Dimensions {
					h := gtx.Dp(36)
					bm := op.Record(gtx.Ops)
					bd, c := p.btn(gtx, "inst.url", "Install", popups.BtnPrimary, popups.BtnMedium, in.busy || strings.TrimSpace(in.urlEd.Text()) == "")
					bcall := bm.Stop()
					if c {
						in.installURL()
					}
					inW := iw - bd.Size.X - gtx.Dp(8)
					p.sinput(gtx, &in.urlEd, inW, h, gtx.Dp(12), "https://example.com/MyPlugin.js", in.busy)
					st := op.Offset(image.Pt(inW+gtx.Dp(8), (h-bd.Size.Y)/2)).Push(gtx.Ops)
					bcall.Add(gtx.Ops)
					st.Pop()
					return layout.Dimensions{Size: image.Pt(iw, h)}
				})
		},
		func(gtx layout.Context, w int) layout.Dimensions {
			return p.field(gtx, w, "From a file", "A .js file from Taut's build:user-plugin or Slick's build:plugin.", "", false, func(gtx layout.Context, _ int) layout.Dimensions {
				d, c := p.btn(gtx, "inst.file", "Choose file…", popups.BtnOutline, popups.BtnMedium, in.busy)
				if c {
					in.installFile()
				}
				return d
			})
		},
	})
	if in.status != "" {
		f.put(0, func(gtx layout.Context) layout.Dimensions {
			col := p.colors().sec
			if in.failed {
				col = p.colors().imp
			}
			return p.k.para(gtx, 13, 18, col, in.status)
		})
		f.gap(12)
	}
	p.notice(f, "Plugins you install will have FULL ACCESS to your Slack data. Only install code you trust!", false)
	return layout.Dimensions{Size: image.Pt(f.w, f.y)}
}

func (in *pluginInstaller) done(status string, err error) {
	in.busy = false
	if err != nil {
		in.status, in.failed = installErr(err), true
	} else {
		in.status, in.failed = status, false
	}
	in.pp.a.Invalidate()
}

// pluginKind is which system a plugin file is for.
func pluginKind(code []byte) string {
	if slick.IsPluginFile(code) {
		return pluginlib.Slick
	}
	return pluginlib.Taut
}

func (in *pluginInstaller) installURL() {
	u := strings.TrimSpace(in.urlEd.Text())
	if u == "" || in.busy {
		return
	}
	in.busy, in.status = true, ""
	a := in.pp.a
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		var code []byte
		source := ""
		var err error
		if s := a.Slick(); s != nil {
			code, source, err = s.Download(ctx, u)
		} else if t := a.Taut(); t != nil {
			code, err = t.FetchPlugin(ctx, u)
		} else {
			err = errors.New("Plugins start once " + AppName + " has signed in to Slack.")
		}
		a.Post(func() {
			if err != nil {
				in.done("", err)
				return
			}
			in.install(code, source, func() { in.urlEd.SetText("") })
		})
	}()
}

func (in *pluginInstaller) installFile() {
	if in.busy {
		return
	}
	in.busy, in.status = true, ""
	a := in.pp.a
	go func() {
		files, err := a.pickFiles(".js,.mjs", false)
		a.Post(func() {
			if err != nil || len(files) == 0 {
				in.done("", err)
				if err == nil {
					in.status = ""
				}
				return
			}
			in.install(files[0].Data, "", nil)
		})
	}()
}

// install confirms and installs a downloaded or picked file in its system
// (UI goroutine). ok runs once it's installed.
func (in *pluginInstaller) install(code []byte, source string, ok func()) {
	a := in.pp.a
	if s := a.Slick(); s != nil {
		if pk, err := s.Peek(code); err == nil {
			in.busy = false
			title, body, action := slickInstallText(pk, source)
			a.ShowOverlay(popups.Confirm(a, title, body, action, pk.Main, nil, func() {
				in.busy = true
				in.run(func(ctx context.Context) (string, error) {
					if source != "" {
						return s.InstallDownloaded(ctx, code, source)
					}
					return s.Import(ctx, code)
				}, ok)
			}))
			return
		}
	}
	if pluginKind(code) == pluginlib.Slick {
		in.done("", errors.New("That is a Slick plugin, and Slick plugins start once "+AppName+" has signed in to Slack."))
		return
	}
	t := a.Taut()
	if t == nil {
		in.done("", errors.New("That is not a Slick plugin, and Taut plugins start once "+AppName+" has signed in to Slack."))
		return
	}
	in.busy = false
	a.ShowOverlay(popups.Confirm(a, "Install this Taut plugin?",
		"Installing runs the file's code. Plugins you install will have FULL ACCESS to your Slack data: your messages, your session token, and posting as you. Only install code you trust!",
		"Install", false, nil, func() {
			in.busy = true
			in.run(func(ctx context.Context) (string, error) {
				id, err := t.Import(ctx, code, "")
				if err != nil {
					return "", err
				}
				for _, c := range t.Settings().Categories {
					for _, pl := range c.Plugins {
						if pl.ID == id && pl.Name != "" {
							return pl.Name, nil
						}
					}
				}
				return id, nil
			}, ok)
		}))
}

// run installs off the UI goroutine and reports it.
func (in *pluginInstaller) run(install func(ctx context.Context) (string, error), ok func()) {
	a := in.pp.a
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		name, err := install(ctx)
		a.Post(func() {
			if err == nil && ok != nil {
				ok()
			}
			in.done("Installed "+name+". Turn it on under Installed.", err)
		})
	}()
}

// ---- Preferences > Plugin settings ----

type pluginSettingsPage struct {
	a    *App
	s    *slickPage // drawing parts; Slick's update state
	taut *tautPage  // Taut's custom CSS, config.json and About screens
}

// tautSettingsScreens are the Taut screens Plugin settings opens.
var tautSettingsScreens = map[string]bool{"appearance": true, "advanced": true, "about": true}

func (sp *pluginSettingsPage) layout(gtx layout.Context) layout.Dimensions {
	a := sp.a
	t, s := a.Taut(), a.Slick()
	if t != nil && tautSettingsScreens[sp.taut.screen] {
		return sp.taut.layout(gtx)
	}
	p := sp.s
	p.k.gtxNow = gtx
	sz := gtx.Constraints.Max
	p.list.List.Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Min: image.Pt(sz.X, 0), Max: image.Pt(sz.X-gtx.Dp(8), 1<<24)}
		f := &flow{gtx: gtx, w: gtx.Constraints.Max.X}
		if t == nil && s == nil {
			p.hint(f, "Plugins start once "+AppName+" has signed in to Slack.")
			return layout.Dimensions{Size: image.Pt(sz.X, f.y)}
		}
		sp.body(f, t, s)
		f.gap(24)
		return layout.Dimensions{Size: image.Pt(sz.X, f.y)}
	})
	return layout.Dimensions{Size: sz}
}

type settingsRow = func(gtx layout.Context, w int) layout.Dimensions

func (sp *pluginSettingsPage) body(f *flow, t *taut.Taut, s *slick.Slick) {
	p := sp.s
	var ts taut.Settings
	var ss slick.Settings
	if t != nil {
		ts = t.Settings()
	}
	if s != nil {
		ss = s.Settings()
	}
	// what's broken first
	if ts.RuntimeError != "" {
		p.notice(f, "Taut plugins can't run: "+ts.RuntimeError, true)
	}
	if ts.ConfigError != "" {
		p.notice(f, "Can't read Taut's config.json: "+ts.ConfigError+". Fix it under Taut config.json below.", true)
	}
	if ss.ConfigError != "" {
		p.notice(f, "Can't read Slick's settings.json: "+ss.ConfigError+". Nothing is saved until it's fixed.", true)
	}
	var failing []string
	for _, pl := range ss.Plugins {
		if pl.Enabled && pl.StartError != "" {
			failing = append(failing, pl.Name)
		}
	}
	if len(failing) > 0 {
		p.notice(f, strings.Join(failing, ", ")+" could not start. Open them under Plugins to see why.", true)
	}

	// running
	var rows []settingsRow
	if s != nil {
		rows = append(rows, func(gtx layout.Context, w int) layout.Dimensions {
			desc := "Off: no Slick plugins or theme run until you turn this back on."
			if !ss.Enabled {
				desc = "Paused: no Slick plugins or theme are running."
			}
			return p.field(gtx, w, "Run Slick plugins", desc, "", false, func(gtx layout.Context, _ int) layout.Dimensions {
				d, c := p.sw(gtx, "global.slick", ss.Enabled, false)
				if c {
					on := !ss.Enabled
					p.write(func(ctx context.Context, s *slick.Slick) error { return s.SetGlobalEnabled(ctx, on) })
				}
				return d
			})
		})
	}
	if t != nil {
		on, total := 0, 0
		for _, c := range ts.Categories {
			for _, pl := range c.Plugins {
				total++
				if pl.Enabled {
					on++
				}
			}
		}
		rows = append(rows, func(gtx layout.Context, w int) layout.Dimensions {
			return p.field(gtx, w, "Taut plugins", strconv.Itoa(on)+" of "+strconv.Itoa(total)+" on. Taut has no global switch: turn plugins off one by one under Plugins.", "", false, nil)
		})
	}
	p.card(f, "Running", "", nil, rows)

	// folders
	rows = nil
	folder := func(k, label, dir string) settingsRow {
		return func(gtx layout.Context, w int) layout.Dimensions {
			return p.field(gtx, w, label, dir, "", false, func(gtx layout.Context, _ int) layout.Dimensions {
				d, c := p.btn(gtx, k, "Open folder", popups.BtnOutline, popups.BtnSmall, false)
				if c {
					_ = openurl.OpenFolder(dir)
				}
				return d
			})
		}
	}
	if t != nil {
		rows = append(rows, folder("dir.taut", "Taut", t.Dir()))
	}
	if s != nil {
		rows = append(rows, folder("dir.slick", "Slick", s.Dir()))
	}
	p.card(f, "Config folders", "", nil, rows)

	// updates
	if s != nil {
		p.card(f, "Updates", "", nil, []settingsRow{
			func(gtx layout.Context, w int) layout.Dimensions {
				return p.field(gtx, w, "Update URL-installed plugins automatically", "Slick plugins installed from a URL. Library plugins update from Plugins > Browse.", "", false, func(gtx layout.Context, _ int) layout.Dimensions {
					d, c := p.sw(gtx, "autoupd", ss.AutoUpdates, false)
					if c {
						on := !ss.AutoUpdates
						p.write(func(ctx context.Context, s *slick.Slick) error { return s.SetAutoUpdates(on) })
					}
					return d
				})
			},
			func(gtx layout.Context, w int) layout.Dimensions {
				return p.field(gtx, w, "Check now", "Fetch the latest version of every plugin installed from a URL.", "", false, func(gtx layout.Context, _ int) layout.Dimensions {
					d, c := p.btn(gtx, "checkupd", "Check for updates", popups.BtnOutline, popups.BtnSmall, p.installing)
					if c {
						p.checkUpdates()
					}
					return d
				})
			},
		})
		if p.installStatus != "" {
			p.hint(f, p.installStatus)
			f.gap(12)
		}
	}

	// Taut's own screens
	if t != nil {
		open := func(k, label, desc, screen string) settingsRow {
			return func(gtx layout.Context, w int) layout.Dimensions {
				return p.field(gtx, w, label, desc, "", false, func(gtx layout.Context, _ int) layout.Dimensions {
					d, c := p.btn(gtx, k, "Open", popups.BtnOutline, popups.BtnSmall, false)
					if c {
						sp.taut.go_(screen, "")
					}
					return d
				})
			}
		}
		p.card(f, "Taut", "", nil, []settingsRow{
			open("taut.css", "Custom CSS", "Taut's user.css, styling "+AppName+"'s Slack class names", "appearance"),
			open("taut.cfg", "config.json", "Every Taut plugin's settings as JSON", "advanced"),
			open("taut.about", "About Taut", "Credits", "about"),
		})
	}
	if s != nil {
		p.aboutCards(f)
	}
}
