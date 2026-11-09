package ui

import (
	"context"
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/pluginbench"
	"github.com/some-du6e/yoloslackgoclient/internal/pluginlib"
	"github.com/some-du6e/yoloslackgoclient/internal/slick"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// The plugin Library is Preferences > Plugins > Browse: every official
// Taut and Slick plugin from the Clack repo's `plugins` release
// (internal/pluginlib), each built unmodified by its project's own build,
// with one-click install and update. A plugin both systems have under one
// name is one row with both tags; Install installs the variant the plugin
// bench picked (internal/pluginbench, docs/PLUGIN-BENCH.md) and the chevron
// beside it offers both, the pick first with its reason (without bench
// data, Install asks which). An installed Library plugin is an ordinary
// user plugin in that system's plugins/ dir.
//
// Neither upstream has a library, so the page is drawn with Slick's
// settings parts: category cards of hairline-separated rows, badges.

// libraryTabID is the old Library tab's id: opening it opens Plugins >
// Browse (Taut's "Plugin library" row, plugins asking for it).
const libraryTabID = "library"

var (
	pluginLibMu sync.Mutex
	pluginLibs  = map[string]*pluginlib.Library{} // by index URL
)

// pluginLibURL is the index the Library reads: CLACK_PLUGIN_LIBRARY (a URL,
// or "off"), else the published one.
func pluginLibURL() string {
	if u := strings.TrimSpace(os.Getenv("CLACK_PLUGIN_LIBRARY")); u != "" {
		if u == "off" {
			return ""
		}
		return u
	}
	return pluginlib.DefaultIndexURL
}

// pluginLib is the shared Library (no I/O until the Library tab opens);
// nil when the Library is turned off.
func (a *App) pluginLib() *pluginlib.Library {
	u := pluginLibURL()
	if u == "" {
		return nil
	}
	pluginLibMu.Lock()
	defer pluginLibMu.Unlock()
	if l := pluginLibs[u]; l != nil {
		return l
	}
	dir := ""
	if a.opts.ConfigDir != "" {
		dir = filepath.Join(a.opts.ConfigDir, "cache", "plugin-library")
	}
	l := pluginlib.New(pluginlib.Options{IndexURL: u, CacheDir: dir})
	pluginLibs[u] = l
	return l
}

// libraryHost is what a plugin system gives the Library.
type libraryHost struct {
	// installed reports the hex sha256 of an installed plugin's files ("" = none)
	installed func(id string) (code, main string)
	// install imports a downloaded entry (update: replace the installed one)
	install func(ctx context.Context, e pluginlib.Entry, code, main []byte, update bool) error
	// open shows an installed plugin's settings (nil: no settings link)
	open func(id string)
}

// libraryPage is Preferences > Plugins > Browse.
type libraryPage struct {
	a       *App
	s       *slickPage // drawing parts (cards, chips, badges, inputs) and its toolbar state
	openTab func(id string)
	ix      *pluginlib.Index
	loading bool
	loadErr string
	busy    map[string]bool   // libKey → installing
	status  map[string]string // libKey → last result
	failed  map[string]bool   // status is an error

	// hosts overrides the systems' install paths (tests)
	hosts func(system string) (libraryHost, bool)
	// bench overrides the baked-in plugin bench data (tests)
	bench *pluginbench.Data
}

func newLibraryPage(a *App, openTab func(id string)) *libraryPage {
	s := newSlickPage(a)
	return &libraryPage{a: a, s: s, openTab: openTab, busy: map[string]bool{}, status: map[string]string{}, failed: map[string]bool{}}
}

func (v *libraryPage) close() { v.s.close() }

// libKey names an entry across both systems (ids may repeat: Taut and
// Slick both have a WhoReacted).
func libKey(e pluginlib.Entry) string { return e.System + "/" + e.ID }

// libraryEntries is the index's entries of both systems, grouped by the
// shared category (plugin_categories.go) in its order, by name within one.
func libraryEntries(ix *pluginlib.Index) []pluginlib.Entry {
	if ix == nil {
		return nil
	}
	out := append(ix.For(pluginlib.Taut), ix.For(pluginlib.Slick)...)
	sort.SliceStable(out, func(i, j int) bool {
		ci, cj := categoryRank(libCategory(out[i]).Key), categoryRank(libCategory(out[j]).Key)
		if ci != cj {
			return ci < cj
		}
		if ni, nj := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name); ni != nj {
			return ni < nj
		}
		return out[i].System < out[j].System // Slick before Taut ("slick" < "taut")
	})
	return out
}

// libCategory is an entry's shared category.
func libCategory(e pluginlib.Entry) pluginCategory { return sharedCategory(e.System, e.Category) }

// host is an entry's system: where Install puts it and how it's found.
func (v *libraryPage) host(system string) (libraryHost, bool) {
	if v.hosts != nil {
		return v.hosts(system)
	}
	switch system {
	case pluginlib.Taut:
		t := v.a.Taut()
		if t == nil {
			return libraryHost{}, false
		}
		return libraryHost{
			installed: func(id string) (string, string) { return t.InstalledSHA256(id), "" },
			install: func(ctx context.Context, e pluginlib.Entry, code, _ []byte, update bool) error {
				replacing := ""
				if update {
					replacing = e.ID
				}
				_, err := t.Import(ctx, code, replacing)
				return err
			},
			open: func(id string) {
				if v.openTab != nil {
					v.openTab("taut:" + id)
				}
			},
		}, true
	case pluginlib.Slick:
		s := v.a.Slick()
		if s == nil {
			return libraryHost{}, false
		}
		return libraryHost{
			installed: s.InstalledSHA256,
			// the renderer first, then its privileged half
			install: func(ctx context.Context, e pluginlib.Entry, code, main []byte, _ bool) error {
				if _, err := s.Import(ctx, code); err != nil {
					return err
				}
				if main != nil {
					if _, err := s.Import(ctx, main); err != nil {
						return err
					}
				}
				return nil
			},
			open: func(id string) {
				if v.openTab != nil {
					v.openTab("slick_plugins:" + id)
				}
			},
		}, true
	}
	return libraryHost{}, false
}

// state is an entry's install state in its system (NotInstalled when the
// system isn't running).
func (v *libraryPage) state(e pluginlib.Entry) pluginlib.State {
	h, ok := v.host(e.System)
	if !ok {
		return pluginlib.NotInstalled
	}
	c, m := h.installed(e.ID)
	return pluginlib.StateOf(e, c, m)
}

func (v *libraryPage) load(refresh bool) {
	if v.loading {
		return
	}
	a := v.a
	lib := a.pluginLib()
	if lib == nil {
		v.loadErr = "The plugin library is turned off (CLACK_PLUGIN_LIBRARY=off)."
		return
	}
	if v.ix == nil {
		v.ix = lib.Cached()
	}
	v.loading, v.loadErr = true, ""
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		ix, err := lib.Index(ctx, refresh)
		a.Post(func() {
			v.loading = false
			if err != nil {
				v.loadErr = err.Error()
			} else {
				v.ix = ix
			}
			a.Invalidate()
		})
	}()
}

// installEntry downloads e (sha256-checked) and installs it in its system.
func (v *libraryPage) installEntry(e pluginlib.Entry, update bool) {
	k := libKey(e)
	if v.busy[k] {
		return
	}
	h, ok := v.host(e.System)
	a := v.a
	v.busy[k], v.status[k], v.failed[k] = true, "", false
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		var code, main []byte
		err := errors.New("the plugin library is turned off")
		if !ok {
			err = fmt.Errorf("%s plugins start once %s has signed in to Slack", systemName(e.System), AppName)
		} else if lib := a.pluginLib(); lib != nil {
			code, main, err = lib.Download(ctx, e)
		}
		if err == nil {
			err = h.install(ctx, e, code, main, update)
		}
		a.Post(func() {
			delete(v.busy, k)
			switch {
			case err != nil:
				v.status[k], v.failed[k] = err.Error(), true
			case update:
				v.status[k] = "Updated."
			default:
				v.status[k] = "Installed. Turn it on under Installed."
			}
			a.Invalidate()
		})
	}()
}

func systemName(sys string) string {
	if sys == pluginlib.Slick {
		return "Slick"
	}
	return "Taut"
}

// libGroup is one Library listing: a plugin by name, in one system or in
// both (many plugins exist in Taut and in Slick under the same name).
type libGroup struct {
	Entries []pluginlib.Entry // in libraryEntries order: Taut before Slick
}

func (g libGroup) first() pluginlib.Entry { return g.Entries[0] }

// libNameKey is the name a plugin is listed under ("Clear URLs" and
// "ClearURLs" are one).
func libNameKey(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r > 0x7f {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// libraryGroups lists each plugin once: entries with the same name share a
// row, filed under its Taut entry's category when the two differ.
func libraryGroups(entries []pluginlib.Entry) []libGroup {
	var out []libGroup
	at := map[string]int{}
	for _, e := range entries {
		k := libNameKey(orStr(e.Name, e.ID))
		if i, ok := at[k]; ok {
			out[i].Entries = append(out[i].Entries, e)
			continue
		}
		at[k] = len(out)
		out = append(out, libGroup{Entries: []pluginlib.Entry{e}})
	}
	for i := range out { // Taut first within a row
		sort.SliceStable(out[i].Entries, func(a, b int) bool {
			return out[i].Entries[a].System == pluginlib.Taut && out[i].Entries[b].System != pluginlib.Taut
		})
	}
	// a row sits under its first (Taut) entry's category
	sort.SliceStable(out, func(i, j int) bool {
		if ri, rj := categoryRank(libCategory(out[i].first()).Key), categoryRank(libCategory(out[j].first()).Key); ri != rj {
			return ri < rj
		}
		return strings.ToLower(out[i].first().Name) < strings.ToLower(out[j].first().Name)
	})
	return out
}

// groupStates are the states of a row's entries.
func (v *libraryPage) groupStates(g libGroup) (installed, updates []pluginlib.Entry) {
	for _, e := range g.Entries {
		switch v.state(e) {
		case pluginlib.Installed:
			installed = append(installed, e)
		case pluginlib.UpdateAvailable:
			installed = append(installed, e)
			updates = append(updates, e)
		}
	}
	return installed, updates
}

// installChoices are a row's entries Install can put in a running system.
func (v *libraryPage) installChoices(g libGroup) []pluginlib.Entry {
	var out []pluginlib.Entry
	for _, e := range g.Entries {
		if _, ok := v.host(e.System); ok {
			out = append(out, e)
		}
	}
	return out
}

// chips are the Browse filter chips: All, Taut, Slick, Installed, and
// Updates while there are some, counting rows.
func (v *libraryPage) chips() []slick.Filter {
	gs := libraryGroups(libraryEntries(v.ix))
	n := map[string]int{"all": len(gs)}
	for _, g := range gs {
		seen := map[string]bool{}
		for _, e := range g.Entries {
			if !seen[e.System] {
				n[e.System]++
				seen[e.System] = true
			}
		}
		installed, updates := v.groupStates(g)
		if len(installed) > 0 {
			n["installed"]++
		}
		if len(updates) > 0 {
			n["updates"]++
		}
	}
	fs := []slick.Filter{{ID: "all", Label: "All", Count: n["all"]}, {ID: pluginlib.Taut, Label: "Taut", Count: n[pluginlib.Taut]},
		{ID: pluginlib.Slick, Label: "Slick", Count: n[pluginlib.Slick]}, {ID: "installed", Label: "Installed", Count: n["installed"]}}
	if n["updates"] > 0 {
		fs = append(fs, slick.Filter{ID: "updates", Label: "Updates", Count: n["updates"]})
	}
	ok := false
	for _, f := range fs {
		ok = ok || f.ID == v.s.filter
	}
	if !ok {
		v.s.filter = "all"
	}
	return fs
}

// matches applies the chip and the search words to a row.
func (v *libraryPage) matches(g libGroup, words []string) bool {
	switch v.s.filter {
	case pluginlib.Taut, pluginlib.Slick:
		has := false
		for _, e := range g.Entries {
			has = has || e.System == v.s.filter
		}
		if !has {
			return false
		}
	case "installed", "updates":
		installed, updates := v.groupStates(g)
		if v.s.filter == "installed" && len(installed) == 0 || v.s.filter == "updates" && len(updates) == 0 {
			return false
		}
	}
	for _, e := range g.Entries {
		if libraryMatches(e, words) {
			return true
		}
	}
	return false
}

func libraryMatches(e pluginlib.Entry, words []string) bool {
	hay := strings.ToLower(strings.Join(append([]string{e.ID, e.Name, e.Description, e.CategoryLabel, libCategory(e).Label, systemName(e.System)}, e.Credit()...), " "))
	for _, w := range words {
		if !strings.Contains(hay, w) {
			return false
		}
	}
	return true
}

// sourceLine is "Taut 6fe5123 · Slick f52d55d, GPLv3" as mrkdwn links.
func (v *libraryPage) sourceLine() string {
	var parts []string
	for _, sys := range []string{pluginlib.Taut, pluginlib.Slick} {
		src, ok := v.ix.Sources[sys]
		if !ok {
			continue
		}
		name := orStr(src.Name, systemName(sys))
		c := shortSHA(src.Commit)
		if strings.HasPrefix(src.Repo, "https://") && src.Commit != "" {
			parts = append(parts, "<"+src.Repo+"/tree/"+src.Commit+"|"+name+" "+c+">")
		} else {
			parts = append(parts, strings.TrimSpace(name+" "+c))
		}
	}
	return strings.Join(parts, " and ")
}

// body draws the Library under Plugins > Browse (the search box and chips
// are the Plugins toolbar's): its head (Refresh, Update all), the intro,
// and the rows by category. words is the shared search.
func (v *libraryPage) body(f *flow, words []string) {
	p := v.s
	k := p.k
	k.gtxNow = f.gtx
	cl := p.colors()
	if v.ix == nil && !v.loading && v.loadErr == "" {
		v.load(false)
	}
	groups := libraryGroups(libraryEntries(v.ix))
	var updates []pluginlib.Entry
	for _, g := range groups {
		_, u := v.groupStates(g)
		updates = append(updates, u...)
	}
	// Slick's section head: title, actions at the right
	f.put(0, func(gtx layout.Context) layout.Dimensions {
		h := gtx.Dp(28)
		x := f.w - gtx.Dp(2)
		add := func(key, label string, kind popups.BtnKind, do func()) {
			b := k.btn(key)
			hit := b.Clicked(gtx)
			m := op.Record(gtx.Ops)
			d := popups.Button(gtx, p.a.th, b, kind, popups.BtnSmall, label, "", 0)
			call := m.Stop()
			x -= d.Size.X
			st := op.Offset(image.Pt(x, (h-d.Size.Y)/2)).Push(gtx.Ops)
			call.Add(gtx.Ops)
			st.Pop()
			x -= gtx.Dp(8)
			if hit && kind != popups.BtnDisabled {
				do()
			}
		}
		refresh, rkind := "Refresh", popups.BtnOutline
		if v.loading {
			refresh, rkind = "Loading...", popups.BtnDisabled
		}
		add("lib.refresh", refresh, rkind, func() { v.load(true) })
		if len(updates) > 0 {
			add("lib.updateall", fmt.Sprintf("Update all (%d)", len(updates)), popups.BtnPrimary, func() {
				for _, e := range updates {
					v.installEntry(e, true)
				}
			})
		}
		at(gtx, image.Pt(gtx.Dp(2), 0), image.Pt(max(0, x-gtx.Dp(14)), h), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
				return k.label(gtx, 15, theme.Bold, cl.pry, "Official plugins")
			})
		})
		return layout.Dimensions{Size: image.Pt(f.w, h)}
	})
	f.gap(8)
	intro := "Every plugin in Taut's and Slick's own repositories, built unmodified by that project's build. They're third-party code under GPLv3 by their authors. Installing one adds it to your installed plugins."
	if v.ix != nil {
		if src := v.sourceLine(); src != "" {
			intro += " Built from " + src + "."
		}
	}
	f.put(0, func(gtx layout.Context) layout.Dimensions { return k.mrkdwn(gtx, "lib.intro", intro, true, cl.sec) })
	f.gap(16)
	if v.loadErr != "" {
		p.notice(f, v.loadErr, true)
	}
	if v.ix == nil {
		if v.loading {
			p.hint(f, "Loading the plugin library...")
		}
		return
	}
	var group []libGroup
	label, shown := "", 0
	flush := func() {
		if len(group) == 0 {
			return
		}
		rows := make([]func(gtx layout.Context, w int) layout.Dimensions, len(group))
		for i, g := range group {
			rows[i] = func(gtx layout.Context, w int) layout.Dimensions { return v.row(gtx, w, g) }
		}
		c := libCategory(group[0].first())
		p.card(f, c.Label, c.Icon, nil, rows)
		group = nil
	}
	for _, g := range groups {
		if !v.matches(g, words) {
			continue
		}
		if l := libCategory(g.first()).Key; l != label {
			flush()
			label = l
		}
		group = append(group, g)
		shown++
	}
	flush()
	if shown == 0 {
		if len(words) > 0 {
			p.empty(f, "No plugins match “"+strings.Join(words, " ")+"”.")
		} else {
			p.empty(f, "No plugins in this view.")
		}
	}
}

// libAction is what a row's button does; menu, when set, is the chevron
// beside it (Install's other variants).
type libAction struct {
	label string
	kind  popups.BtnKind
	do    func()
	menu  func()
}

// action: Installing/Updating while busy; Update when an installed entry
// has an update; Settings when one is installed; else Install. When the
// plugin exists in both systems and both are running, Install installs the
// bench's pick and its chevron offers both; with no bench data it asks.
func (v *libraryPage) action(g libGroup) libAction {
	installed, updates := v.groupStates(g)
	for _, e := range g.Entries {
		if v.busy[libKey(e)] {
			if len(installed) > 0 {
				return libAction{label: "Updating...", kind: popups.BtnDisabled}
			}
			return libAction{label: "Installing...", kind: popups.BtnDisabled}
		}
	}
	switch {
	case len(updates) > 0:
		return libAction{label: "Update", kind: popups.BtnPrimary, do: func() {
			for _, e := range updates {
				v.installEntry(e, true)
			}
		}}
	case len(installed) > 0:
		e := installed[0]
		return libAction{label: "Settings", kind: popups.BtnOutline, do: func() {
			if h, ok := v.host(e.System); ok && h.open != nil {
				h.open(e.ID)
			}
		}}
	}
	choices := v.installChoices(g)
	if len(choices) < 2 {
		e := g.first()
		if len(choices) == 1 {
			e = choices[0]
		}
		return libAction{label: "Install", kind: popups.BtnPrimary, do: func() { v.installEntry(e, false) }}
	}
	menu := func() { v.a.menu(v.a.PointerPos(), v.installMenu(choices)) }
	if w, _, ok := v.benchPick(choices); ok {
		return libAction{label: "Install", kind: popups.BtnPrimary, do: func() { v.installEntry(w, false) }, menu: menu}
	}
	return libAction{label: "Install…", kind: popups.BtnPrimary, do: menu}
}

// benchPick is the variant of choices the plugin bench picked, with its
// reason (ok false without bench data for this plugin).
func (v *libraryPage) benchPick(choices []pluginlib.Entry) (pluginlib.Entry, pluginbench.Pick, bool) {
	d := v.bench
	if d == nil {
		d = pluginbench.Load()
	}
	es := make([]pluginbench.Entry, len(choices))
	for i, e := range choices {
		es[i] = pluginbench.Entry{System: e.System, ID: e.ID, SHA256: e.SHA256}
	}
	p, ok := d.For(es)
	if !ok {
		return pluginlib.Entry{}, p, false
	}
	for _, e := range choices {
		if e.System == p.Winner {
			return e, p, true
		}
	}
	return pluginlib.Entry{}, p, false
}

// installMenu is "Install for": the bench's pick first, marked
// "(recommended)" with its reason, then the other system.
func (v *libraryPage) installMenu(choices []pluginlib.Entry) []MenuItem {
	items := []MenuItem{{Label: "Install for", Disabled: true}}
	order := append([]pluginlib.Entry(nil), choices...)
	w, pick, ok := v.benchPick(choices)
	if ok {
		sort.SliceStable(order, func(i, j int) bool { return order[i].System == w.System && order[j].System != w.System })
	}
	for _, e := range order {
		it := MenuItem{Label: systemName(e.System), Subtitle: systemBlurb(e.System), Do: func() { v.installEntry(e, false) }}
		if ok && e.System == w.System {
			it.Label += " (recommended)"
			it.Subtitle = benchReason(pick)
		}
		items = append(items, it)
	}
	return items
}

// benchReason is the pick's reason as one menu line: its first two
// clauses ("Works fully, 3.9x faster to load"), capitalized.
func benchReason(p pluginbench.Pick) string {
	r := p.Reason
	if r == "" {
		return "Recommended by the plugin bench"
	}
	if parts := strings.Split(r, ", "); len(parts) > 2 {
		r = strings.Join(parts[:2], ", ")
	}
	r = strings.ToUpper(r[:1]) + r[1:]
	if !p.Exact {
		r += " (measured on an earlier build)"
	}
	return r
}

// systemBlurb tells the two systems apart in Install's choice.
func systemBlurb(sys string) string {
	if sys == pluginlib.Slick {
		return "Slick's version of the plugin"
	}
	return "Taut's version of the plugin"
}

// row is a Slick plugin row with the switch swapped for the action:
// name (15/700) with a tag per system and any flags, the description
// (13/18, two lines), "By …" (13, tertiary), the last result, and
// Install / Update / Settings at the right.
func (v *libraryPage) row(gtx layout.Context, w int, g libGroup) layout.Dimensions {
	p := v.s
	k := p.k
	cl := p.colors()
	e0 := g.first()
	key := libKey(e0)
	act := v.action(g)
	bw := gtx.Dp(96)
	tx, gap := gtx.Dp(16), gtx.Dp(14)
	tw := w - tx - gtx.Dp(12) - bw - gap
	var desc, hc, off bool
	var credits []string
	seen := map[string]bool{}
	for _, e := range g.Entries {
		desc = desc || e.Description != ""
		hc = hc || e.HackClubOnly
		off = off || e.UpstreamDisabled
		if c := libraryCredit(e); c != "" && !seen[c] {
			seen[c] = true
			credits = append(credits, c)
		}
	}
	textCol := func(gtx layout.Context) layout.Dimensions {
		nd := p.a.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Weight: theme.Bold, Color: cl.pry, MaxLines: 1}, e0.Name)
		bx := min(nd.Size.X, tw) + gtx.Dp(8)
		badge := func(label string, warn bool) {
			d := at(gtx, image.Pt(bx, gtx.Dp(1)), image.Pt(gtx.Dp(200), gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
				return p.badge(gtx, label, "", warn)
			})
			bx += d.Size.X + gtx.Dp(6)
		}
		for _, e := range g.Entries {
			badge(systemName(e.System), false)
		}
		if hc {
			badge("Hack Club only", false)
		}
		if off {
			badge("Off upstream", true)
		}
		y := gtx.Dp(22)
		if desc {
			d := ""
			for _, e := range g.Entries {
				if d == "" {
					d = e.Description
				}
			}
			y += at(gtx, image.Pt(0, y), image.Pt(tw, 1<<20), func(gtx layout.Context) layout.Dimensions {
				return p.a.th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 18, Color: cl.sec, MaxLines: 2, Wrap: text.WrapWords}, plainMrkdwn(d))
			}).Size.Y
		}
		if len(credits) > 0 {
			y += gtx.Dp(4)
			y += at(gtx, image.Pt(0, y), image.Pt(tw, 1<<20), func(gtx layout.Context) layout.Dimensions {
				return k.mrkdwn(gtx, "lib.by."+key, "By "+strings.Join(credits, ", "), true, cl.ter)
			}).Size.Y
		}
		for _, e := range g.Entries {
			st := v.status[libKey(e)]
			if st == "" {
				continue
			}
			col := cl.ter
			if v.failed[libKey(e)] {
				col = cl.imp
			}
			if len(g.Entries) > 1 {
				st = systemName(e.System) + ": " + st
			}
			y += gtx.Dp(4)
			y += at(gtx, image.Pt(0, y), image.Pt(tw, 1<<20), func(gtx layout.Context) layout.Dimensions {
				return p.a.th.Layout(gtx, theme.TextStyle{Size: 12, Weight: theme.Bold, Color: col, Wrap: text.WrapWords}, st)
			}).Size.Y
		}
		return layout.Dimensions{Size: image.Pt(tw, y)}
	}
	m := op.Record(gtx.Ops)
	td := at(gtx, image.Point{}, image.Pt(tw, 1<<20), textCol)
	tcall := m.Stop()
	h := td.Size.Y + gtx.Dp(24)
	st := op.Offset(image.Pt(tx, gtx.Dp(12))).Push(gtx.Ops)
	tcall.Add(gtx.Ops)
	st.Pop()
	b := k.btn("lib.act." + key)
	hit := b.Clicked(gtx)
	bx, by := w-gtx.Dp(12)-bw, (h-gtx.Dp(28))/2
	mainW := float32(96)
	if act.menu != nil { // [Install][▾]
		mainW = 64
		cb := k.btn("lib.more." + key)
		if cb.Clicked(gtx) {
			act.menu()
		}
		at(gtx, image.Pt(bx+gtx.Dp(68), by), image.Pt(gtx.Dp(28), gtx.Dp(28)), func(gtx layout.Context) layout.Dimensions {
			return popups.Button(gtx, p.a.th, cb, act.kind, popups.BtnSmall, "", "caret-down", 28)
		})
	}
	at(gtx, image.Pt(bx, by), image.Pt(gtx.Dp(unit.Dp(mainW)), gtx.Dp(28)), func(gtx layout.Context) layout.Dimensions {
		return popups.Button(gtx, p.a.th, b, act.kind, popups.BtnSmall, act.label, "", mainW)
	})
	if hit && act.kind != popups.BtnDisabled && act.do != nil {
		act.do()
	}
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// libraryCredit is an entry's authors as mrkdwn names (linked when they
// have a page), else its upstream contributors.
func libraryCredit(e pluginlib.Entry) string {
	var out []string
	for _, a := range e.Authors {
		switch {
		case a.Name == "":
		case strings.HasPrefix(a.URL, "https://"):
			out = append(out, "<"+a.URL+"|"+mrkdwnEscape(a.Name)+">")
		default:
			out = append(out, mrkdwnEscape(a.Name))
		}
	}
	if len(out) == 0 {
		for _, c := range e.Contributors {
			out = append(out, mrkdwnEscape(c))
		}
	}
	return strings.Join(out, ", ")
}

func shortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

func orStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
