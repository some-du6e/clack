package ui

import (
	"context"
	"errors"
	"image"
	"sort"
	"strconv"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/pluginlib"
	"github.com/some-du6e/clack/internal/slick"
	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Clack runs Taut's and Slick's plugins side by side and shows them as one
// plugin system. After Slack's own Preferences tabs come:
//
//   - Plugins: Taut's own Preferences tab (Taut's app/settings), over both
//     systems: rows to Appearance (custom CSS), Advanced (installing a
//     plugin from a file or URL, the config folders, Slick's switch and URL
//     updates, config.json) and About (credits), and to the plugin library
//     (library_prefs.go); then "Search plugins", Taut's filter pills and
//     every installed plugin of both systems by shared category. A row's
//     checkbox turns it on in its own system; the row opens that system's
//     own page for it.
//   - Themes: Slick's themes page.
//
// The pages reuse Taut's and Slick's own drawing (taut_prefs.go,
// slick_prefs.go). Plugins' own tabs follow, unchanged.

const (
	pluginsTabID        = "plugins"
	pluginSettingsTabID = "plugin_settings" // the old Plugin settings tab: Plugins > Advanced now
	themesTabID         = "slick_themes"    // Slick's own id for its themes tab
)

// pluginNav is Clack's plugin tabs, after a divider below Slack's.
var pluginNav = []navExtra{
	{id: pluginsTabID, label: "Plugins", icon: "apps"},
	{id: themesTabID, label: "Themes", icon: "paintbrush"},
}

// oldPluginTabs are the tab ids the plugin tabs had before they merged
// (Taut's, Slick's, the Library's, Plugin settings): opening one opens its
// new home.
var oldPluginTabs = map[string]string{
	"taut": pluginsTabID, "slick_plugins": pluginsTabID, "slick_install": pluginsTabID, libraryTabID: pluginsTabID,
	"slick": pluginsTabID, pluginSettingsTabID: pluginsTabID,
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
					Category: tautGroup(pluginlib.Taut, pl.Category, pl.IsUser), Enabled: pl.Enabled, User: pl.IsUser,
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
				Category: tautGroup(pluginlib.Slick, pl.Category, pl.User), Enabled: pl.Enabled, User: pl.User,
				HackClubOnly: hackClubOnly(pl.Workspaces), slick: pl}
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
		if ri, rj := tautGroupRank(out[i].Category.Key), tautGroupRank(out[j].Category.Key); ri != rj {
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

// hackClubOnly reports a Slick plugin limited to the Hack Club Slack.
func hackClubOnly(workspaces []string) bool {
	if len(workspaces) == 0 {
		return false
	}
	for _, w := range workspaces {
		if w != "T0266FRGM" && w != "E09V59WQY1E" {
			return false
		}
	}
	return true
}

// unifiedFilters are Taut's filter pills over both systems: All, Enabled,
// Disabled, Hack Club only (in the Hack Club Slack) and User plugins (when
// there are some; every Slick plugin in Clack is one).
func unifiedFilters(ps []unifiedPlugin, hackClub bool) []slick.Filter {
	on, user := 0, 0
	for _, u := range ps {
		if u.Enabled {
			on++
		}
		if u.User {
			user++
		}
	}
	fs := []slick.Filter{{ID: "all", Label: "All", Count: len(ps)}, {ID: "enabled", Label: "Enabled", Count: on}, {ID: "disabled", Label: "Disabled", Count: len(ps) - on}}
	if hackClub {
		n := 0
		for _, u := range ps {
			if u.HackClubOnly {
				n++
			}
		}
		fs = append(fs, slick.Filter{ID: "hack-club", Label: "Hack Club only", Count: n})
	}
	if user > 0 {
		fs = append(fs, slick.Filter{ID: "user", Label: "User plugins", Count: user})
	}
	return fs
}

// unifiedMatches applies a pill and the search words.
func unifiedMatches(u unifiedPlugin, filter string, words []string) bool {
	switch filter {
	case "enabled":
		if !u.Enabled {
			return false
		}
	case "disabled":
		if u.Enabled {
			return false
		}
	case "hack-club":
		if !u.HackClubOnly {
			return false
		}
	case "user":
		if !u.User {
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

// unifiedSection is one category group of the list.
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

// pluginsPage is the Plugins tab, laid out like Taut's own Preferences tab
// (Taut's app/settings: index.tsx, pluginList.tsx, screens.tsx): rows to
// the Appearance, Advanced and About screens (taut_prefs.go) and to the
// plugin library, then "Search plugins", Taut's filter pills and the
// plugins grouped under their category headings, one row each: its
// checkbox, name and description, a warning when it needs attention, and a
// caret to its own page. Slick's plugins are rows in the same list (their
// pages are Slick's own).
type pluginsPage struct {
	a    *App
	s    *slickPage   // the shared search, the list's filter and scroll, Slick's plugin page (s.open)
	taut *tautPage    // Taut's screens: a plugin's page, Appearance, Advanced, About
	lib  *libraryPage // the plugin library (mode browse)
	mode string       // installed | browse

	inst *pluginInstaller
}

func newPluginsPage(a *App, tp *tautPage, openTab func(string)) *pluginsPage {
	pp := &pluginsPage{a: a, s: newSlickPage(a), taut: tp, lib: newLibraryPage(a, openTab), mode: "installed"}
	pp.s.tab = "slick_plugins" // its detail draws through slickPage.layout
	pp.inst = &pluginInstaller{pp: pp}
	tp.slick, tp.install, tp.open = pp.s, pp.inst, pp.openPlugin
	return pp
}

func (pp *pluginsPage) close() {
	pp.s.close()
	pp.lib.close()
}

// setMode switches between the list and the library (the search stays).
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

// filters are Taut's pills; an unknown one falls back to All.
func (pp *pluginsPage) filters(ps []unifiedPlugin) []slick.Filter {
	fs := unifiedFilters(ps, pp.hackClub())
	ok := false
	for _, f := range fs {
		ok = ok || f.ID == pp.s.filter
	}
	if !ok {
		pp.s.filter = "all"
	}
	return fs
}

// hackClub reports the Hack Club workspace (Taut's "Hack Club only" pill
// shows only there).
func (pp *pluginsPage) hackClub() bool {
	if t := pp.a.Taut(); t != nil {
		return t.IsHackClub()
	}
	return false
}

// openPlugin shows a plugin's own page: Taut's, or Slick's detail.
func (pp *pluginsPage) openPlugin(system, id string) {
	pp.mode = "installed"
	if system == pluginlib.Taut {
		pp.s.open = ""
		pp.taut.go_("plugin", id)
		return
	}
	if pp.taut.screen != "main" {
		pp.taut.go_("main", "")
	}
	pp.s.listBack = pp.s.list.Position
	pp.s.open, pp.s.daysOpen = id, ""
	pp.s.list.Position = layout.Position{}
	pp.a.Invalidate()
}

// openScreen shows one of Taut's screens (appearance, advanced, about).
func (pp *pluginsPage) openScreen(screen string) {
	pp.s.open = ""
	pp.mode = "installed"
	pp.taut.go_(screen, "")
}

func (pp *pluginsPage) layout(gtx layout.Context) layout.Dimensions {
	a := pp.a
	if pp.taut.screen != "main" && (pp.taut.screen != "plugin" || a.Taut() != nil) {
		return pp.taut.layout(gtx)
	}
	if pp.s.open != "" && a.Slick() != nil {
		return pp.s.layout(gtx, "slick_plugins")
	}
	p := pp.s
	p.k.gtxNow = gtx
	pp.lib.s.k.gtxNow = gtx
	pp.taut.gtxNow = gtx
	sz := gtx.Constraints.Max
	return p.list.List.Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Min: image.Pt(sz.X, 0), Max: image.Pt(sz.X-gtx.Dp(8), 1<<24)}
		f := &flow{gtx: gtx, w: gtx.Constraints.Max.X}
		if pp.mode == "browse" {
			pp.libraryScreen(f)
		} else {
			pp.mainScreen(f)
		}
		f.gap(24)
		return layout.Dimensions{Size: image.Pt(sz.X, f.y)}
	})
}

// mainScreen is Taut's main screen: the rows to the other screens, then
// the plugin list.
func (pp *pluginsPage) mainScreen(f *flow) {
	a := pp.a
	if a.Taut() == nil && a.Slick() == nil {
		pp.taut.muted(f, "Plugins start once "+AppName+" has signed in to Slack.")
		return
	}
	pp.navRows(f)
	f.gap(16) // .taut-settings__nav margin-bottom
	ps := pp.installed()
	pp.pluginList(f, ps)
}

// navRows are Taut's .taut-settings__nav rows: its three screens, then the
// plugin library (Clack's: where Taut's and Slick's plugins are found).
func (pp *pluginsPage) navRows(f *flow) {
	type nav struct{ key, label, desc, icon string }
	desc := "Find and install plugins"
	if n := pp.libraryUpdates(); n == 1 {
		desc = "1 update to install"
	} else if n > 1 {
		desc = strconv.Itoa(n) + " updates to install"
	}
	for _, r := range []nav{
		{"appearance", "Appearance", "Custom CSS", "paintbrush"},
		{"advanced", "Advanced", "Installing plugins, config.json", "settings"}, // Taut's, without safe mode (Clack has none)
		{"about", "About", "Credits", "info"},
		{"library", "Plugin library", desc, "apps"},
	} {
		f.put(0, func(gtx layout.Context) layout.Dimensions {
			d, c := pp.taut.navRow(gtx, "nav."+r.key, r.icon, r.label, r.desc)
			if c {
				if r.key == "library" {
					pp.s.open = ""
					pp.setMode("browse")
				} else {
					pp.openScreen(r.key)
				}
			}
			return d
		})
	}
}

// libraryUpdates counts library rows with an update waiting.
func (pp *pluginsPage) libraryUpdates() int {
	n := 0
	if pp.lib.ix != nil {
		for _, g := range libraryGroups(libraryEntries(pp.lib.ix)) {
			if _, u := pp.lib.groupStates(g); len(u) > 0 {
				n++
			}
		}
	}
	return n
}

// pluginList is Taut's PluginList over both systems: "Search plugins", the
// filter pills, then the matching plugins under their category headings.
func (pp *pluginsPage) pluginList(f *flow, ps []unifiedPlugin) {
	p, tp := pp.s, pp.taut
	cl := tp.colors()
	fs := pp.filters(ps)
	f.put(0, func(gtx layout.Context) layout.Dimensions {
		return tp.input(gtx, &p.query, f.w, gtx.Dp(36), "Search plugins", false)
	})
	f.gap(12) // .taut-settings__filters: margin 12px 0 8px, gap 8px
	f.put(0, func(gtx layout.Context) layout.Dimensions {
		x, y, rowH := 0, 0, 0
		for _, fl := range fs {
			m := op.Record(gtx.Ops)
			d, c := tp.filterPill(gtx, "pill."+fl.ID, fl.Label, p.filter == fl.ID)
			call := m.Stop()
			if x > 0 && x+d.Size.X > f.w {
				x, y = 0, y+rowH+gtx.Dp(8)
			}
			st := op.Offset(image.Pt(x, y)).Push(gtx.Ops)
			call.Add(gtx.Ops)
			st.Pop()
			if c {
				p.filter = fl.ID
				p.list.Position = layout.Position{}
				pp.a.Invalidate()
			}
			x += d.Size.X + gtx.Dp(8)
			rowH = max(rowH, d.Size.Y)
		}
		return layout.Dimensions{Size: image.Pt(f.w, y+rowH)}
	})
	f.gap(8)
	words := strings.Fields(strings.ToLower(p.query.Text()))
	var rows []unifiedPlugin
	for _, u := range ps {
		if unifiedMatches(u, p.filter, words) {
			rows = append(rows, u)
		}
	}
	if len(rows) == 0 {
		// nothing installed matches the search: what the library has for it
		if len(words) > 0 && pp.suggestions(f, words) {
			return
		}
		msg := "No plugins match this filter."
		switch {
		case len(words) > 0:
			msg = "No plugins match “" + p.query.Text() + "”."
		case len(ps) == 0:
			msg = "No plugins installed yet. Find one in the plugin library, or install one under Advanced."
		}
		f.gap(14) // a <p>'s margin
		f.put(0, func(gtx layout.Context) layout.Dimensions { return tp.para(gtx, 15, 22, cl.fg2, msg) })
		return
	}
	for _, sec := range unifiedSections(rows) {
		f.gap(20) // .taut-settings__group margin-top
		f.put(0, func(gtx layout.Context) layout.Dimensions {
			return tp.label(gtx, 15, theme.Bold, cl.fg, sec.Category.Label) // h3: 15px 700
		})
		f.gap(4)
		for _, u := range sec.Plugins {
			f.put(0, func(gtx layout.Context) layout.Dimensions { return pp.row(gtx, f.w, u) })
		}
	}
}

// row is Taut's PluginRow, for either system's plugin: the checkbox, the
// name with its description on one line each (clipped), a warning when it
// needs attention, and a caret to its page.
func (pp *pluginsPage) row(gtx layout.Context, w int, u unifiedPlugin) layout.Dimensions {
	tp := pp.taut
	cl := tp.colors()
	// .taut-settings__row: the checkbox 4px in, gap 8; .taut-settings__row-main: padding 6px 4px
	cx := gtx.Dp(4)
	chk := gtx.Dp(theme.NativeCheckSize)
	mx := cx + chk + gtx.Dp(8)
	mw := w - mx
	h := gtx.Dp(6 + 20 + 6)
	desc := u.Description
	if u.Problem != "" && u.ProblemErr {
		desc = u.Problem // Taut: plugin.error ?? description
	}
	if desc != "" {
		h += gtx.Dp(18)
	}
	b := tp.btn("row." + u.key())
	at(gtx, image.Pt(mx, 0), image.Pt(mw, h), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, b, func(gtx layout.Context, s image.Point, hov bool) {
			if hov { // button.taut-settings__row-main:hover, radius 6
				theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(6), cl.hover)
			}
		}, func(gtx layout.Context) layout.Dimensions {
			icons16 := gtx.Dp(16)
			right := mw - gtx.Dp(4) - icons16 // the caret
			warn := u.Broken || u.Problem != "" && u.ProblemErr
			textW := right - gtx.Dp(8)
			if warn {
				textW -= icons16 + gtx.Dp(8)
			}
			at(gtx, image.Pt(gtx.Dp(4), gtx.Dp(6)), image.Pt(textW, h-gtx.Dp(12)), func(gtx layout.Context) layout.Dimensions {
				tp.a.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 20, Weight: theme.Bold, Color: cl.fg, MaxLines: 1}, u.Name)
				if desc != "" { // .taut-settings__row-description: content-sec, an error too (the icon flags it)
					at(gtx, image.Pt(0, gtx.Dp(20)), image.Pt(textW, gtx.Dp(18)), func(gtx layout.Context) layout.Dimensions {
						return tp.a.th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 18, Color: cl.fg2, MaxLines: 1}, desc)
					})
				}
				return layout.Dimensions{Size: image.Pt(textW, h-gtx.Dp(12))}
			})
			if warn {
				at(gtx, image.Pt(right-gtx.Dp(8)-icons16, (h-icons16)/2), image.Pt(icons16, icons16), func(gtx layout.Context) layout.Dimensions {
					return icons.Get("warning")(gtx, 16, cl.err)
				})
			}
			at(gtx, image.Pt(right, (h-icons16)/2), image.Pt(icons16, icons16), func(gtx layout.Context) layout.Dimensions {
				return icons.Get("caret-right")(gtx, 16, cl.fg3)
			})
			return layout.Dimensions{Size: image.Pt(mw, h)}
		})
	})
	// the checkbox, vertically centred like the row's align-items: center
	at(gtx, image.Pt(cx, (h-chk)/2), image.Pt(chk, chk), func(gtx layout.Context) layout.Dimensions {
		cb := tp.btn("en." + u.key())
		d := clickArea(gtx, cb, nil, func(gtx layout.Context) layout.Dimensions {
			return theme.NativeCheck(gtx, tp.a.th.Dark, theme.NativeCheckState{On: u.Enabled, Hover: cb.Hovered()})
		})
		if clicked(cb) {
			pp.setEnabled(u, !u.Enabled)
		}
		return d
	})
	if clicked(b) {
		pp.openPlugin(u.System, u.ID)
	}
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// maxSuggestions is how many library plugins an empty search suggests.
const maxSuggestions = 5

// suggestions is what an installed-plugins search that found nothing shows
// instead of the empty state: the plugin library's plugins that match it
// and aren't installed, each with its Install action, and a link to every
// result in the library (the search kept). False when the library has
// none (or isn't loaded yet): then the plain empty state shows.
func (pp *pluginsPage) suggestions(f *flow, words []string) bool {
	v := pp.lib
	if v.ix == nil && !v.loading && v.loadErr == "" {
		v.load(false) // the cached index at once, then a fresh one
	}
	hits := v.suggest(words)
	if len(hits) == 0 {
		return false
	}
	tp := pp.taut
	cl := tp.colors()
	f.gap(14) // a <p>'s margin
	f.put(0, func(gtx layout.Context) layout.Dimensions {
		return tp.para(gtx, 15, 22, cl.fg2, "You don't have anything like that installed. Maybe you're looking for:")
	})
	f.gap(4)
	for i, g := range hits {
		if i == maxSuggestions {
			break
		}
		f.put(0, func(gtx layout.Context) layout.Dimensions { return pp.suggestionRow(gtx, f.w, g) })
	}
	f.gap(8)
	label := "See all results in the plugin library"
	if len(hits) > maxSuggestions {
		label = "See all " + strconv.Itoa(len(hits)) + " results in the plugin library"
	}
	f.put(0, func(gtx layout.Context) layout.Dimensions {
		d, c := tp.linkButton(gtx, "suggest.all", label, "")
		if c {
			pp.s.open = ""
			pp.setMode("browse") // the shared search keeps the query
		}
		return d
	})
	return true
}

// suggest is the library's plugins that match words and aren't
// installed, in the library's order (nil until its index is there).
func (v *libraryPage) suggest(words []string) []libGroup {
	if v.ix == nil {
		return nil
	}
	var hits []libGroup
	for _, g := range libraryGroups(libraryEntries(v.ix)) {
		if installed, _ := v.groupStates(g); len(installed) > 0 {
			continue
		}
		for _, e := range g.Entries {
			if libraryMatches(e, words) {
				hits = append(hits, g)
				break
			}
		}
	}
	return hits
}

// suggestionRow is a library plugin in Taut's row: the name and its
// description (in line with the installed rows' names, past the checkbox
// column), and the library's own action for it (Install, or Install… when
// both systems have it) at the right, with how it went under the name.
func (pp *pluginsPage) suggestionRow(gtx layout.Context, w int, g libGroup) layout.Dimensions {
	v, tp := pp.lib, pp.taut
	cl := tp.colors()
	e := g.first()
	x := gtx.Dp(4+theme.NativeCheckSize+8) + gtx.Dp(4) // .taut-settings__row-spacer, then row-main's padding
	act := v.action(g)
	kind := act.kind
	if kind != popups.BtnDisabled {
		kind = popups.BtnOutline // Taut's small outline buttons, quieter than the library's
	}
	b := v.s.k.btn("suggest." + libKey(e))
	hit := b.Clicked(gtx)
	m := op.Record(gtx.Ops)
	bd := popups.Button(gtx, pp.a.th, b, kind, popups.BtnSmall, act.label, "", 0)
	bcall := m.Stop()
	tw := w - x - bd.Size.X - gtx.Dp(12)
	status, failed := "", false
	for _, en := range g.Entries {
		if st := v.status[libKey(en)]; st != "" {
			status, failed = st, v.failed[libKey(en)]
		}
	}
	y := gtx.Dp(6)
	y += at(gtx, image.Pt(x, y), image.Pt(tw, gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
		return tp.a.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 20, Weight: theme.Bold, Color: cl.fg, MaxLines: 1}, e.Name)
	}).Size.Y
	if d := plainMrkdwn(e.Description); d != "" {
		y += at(gtx, image.Pt(x, y), image.Pt(tw, gtx.Dp(18)), func(gtx layout.Context) layout.Dimensions {
			return tp.a.th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 18, Color: cl.fg2, MaxLines: 1}, d)
		}).Size.Y
	}
	if status != "" {
		col := cl.fg3
		if failed {
			col = cl.err
		}
		y += at(gtx, image.Pt(x, y), image.Pt(tw, gtx.Dp(18)), func(gtx layout.Context) layout.Dimensions {
			return tp.a.th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 18, Color: col, MaxLines: 2}, status)
		}).Size.Y
	}
	h := y + gtx.Dp(6)
	st := op.Offset(image.Pt(w-gtx.Dp(4)-bd.Size.X, (h-bd.Size.Y)/2)).Push(gtx.Ops)
	bcall.Add(gtx.Ops)
	st.Pop()
	if hit && act.do != nil && act.kind != popups.BtnDisabled {
		act.do()
	}
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// libraryScreen is the plugin library as one of Taut's sub-screens: the
// back link, its title, then its search, chips and rows.
func (pp *pluginsPage) libraryScreen(f *flow) {
	p, tp := pp.s, pp.taut
	f.put(0, func(gtx layout.Context) layout.Dimensions {
		d, c := tp.linkButton(gtx, "lib.back", "Plugins", "arrow-left")
		if c {
			pp.setMode("installed")
		}
		return d
	})
	f.gap(12) // .taut-settings__back margin-bottom
	f.put(0, func(gtx layout.Context) layout.Dimensions {
		return tp.label(gtx, 18, theme.Bold, tp.colors().fg, "Plugin library") // .taut-settings__title
	})
	f.gap(16)
	fs := pp.lib.chips()
	f.put(0, func(gtx layout.Context) layout.Dimensions {
		return pp.lib.s.searchBar(gtx, &p.query, "Search the plugin library", fs)
	})
	f.gap(10)
	pp.lib.body(f, strings.Fields(strings.ToLower(p.query.Text())))
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

// ---- installing (Advanced > Install a plugin) ----

// pluginInstaller installs a file or a URL in the right system, for
// Taut's ImportControls on the Advanced screen: a file with Slick's plugin
// header goes to Slick (after Slick's confirmation), anything else to
// Taut. Done (UI goroutine) gets the system and id of what was installed,
// or the error.
type pluginInstaller struct {
	pp   *pluginsPage
	busy bool
}

// pluginKind is which system a plugin file is for.
func pluginKind(code []byte) string {
	if slick.IsPluginFile(code) {
		return pluginlib.Slick
	}
	return pluginlib.Taut
}

// installURL downloads u and installs it.
func (in *pluginInstaller) installURL(u string, done func(system, id string, err error)) {
	u = strings.TrimSpace(u)
	if u == "" || in.busy {
		return
	}
	in.busy = true
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
				in.busy = false
				done("", "", err)
				return
			}
			in.install(code, source, done)
		})
	}()
}

// installFile asks for a .js file and installs it.
func (in *pluginInstaller) installFile(done func(system, id string, err error)) {
	if in.busy {
		return
	}
	in.busy = true
	a := in.pp.a
	go func() {
		files, err := a.pickFiles(".js,.mjs", false)
		a.Post(func() {
			if err != nil || len(files) == 0 {
				in.busy = false
				if err != nil {
					done("", "", err)
				}
				return
			}
			in.install(files[0].Data, "", done)
		})
	}()
}

// install confirms and installs a downloaded or picked file in its system
// (UI goroutine).
func (in *pluginInstaller) install(code []byte, source string, done func(system, id string, err error)) {
	a := in.pp.a
	if s := a.Slick(); s != nil {
		if pk, err := s.Peek(code); err == nil {
			in.busy = false
			title, body, action := slickInstallText(pk, source)
			a.ShowOverlay(popups.Confirm(a, title, body, action, pk.Main, nil, func() {
				in.busy = true
				in.run(pluginlib.Slick, func(ctx context.Context) (string, error) {
					if _, err := func() (string, error) {
						if source != "" {
							return s.InstallDownloaded(ctx, code, source)
						}
						return s.Import(ctx, code)
					}(); err != nil {
						return "", err
					}
					return pk.ID, nil
				}, done)
			}))
			return
		}
	}
	if pluginKind(code) == pluginlib.Slick {
		in.busy = false
		done("", "", errors.New("That is a Slick plugin, and Slick plugins start once "+AppName+" has signed in to Slack."))
		return
	}
	t := a.Taut()
	if t == nil {
		in.busy = false
		done("", "", errors.New("That is not a Slick plugin, and Taut plugins start once "+AppName+" has signed in to Slack."))
		return
	}
	in.busy = false
	a.ShowOverlay(popups.Confirm(a, "Install this Taut plugin?",
		"Installing runs the file's code. Plugins you install will have FULL ACCESS to your Slack data: your messages, your session token, and posting as you. Only install code you trust!",
		"Install", false, nil, func() {
			in.busy = true
			in.run(pluginlib.Taut, func(ctx context.Context) (string, error) { return t.Import(ctx, code, "") }, done)
		}))
}

// run installs off the UI goroutine and reports it.
func (in *pluginInstaller) run(system string, install func(ctx context.Context) (string, error), done func(system, id string, err error)) {
	a := in.pp.a
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		id, err := install(ctx)
		a.Post(func() {
			in.busy = false
			done(system, id, err)
			a.Invalidate()
		})
	}()
}
