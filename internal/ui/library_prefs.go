package ui

import (
	"context"
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"

	"github.com/some-du6e/yoloslackgoclient/internal/pluginlib"
	"github.com/some-du6e/yoloslackgoclient/internal/slick"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// The plugin Library is one Preferences tab, "Plugin library", that lists
// every official Taut and Slick plugin from the Clack repo's `plugins`
// release (internal/pluginlib), each built unmodified by its project's own
// build, tagged with its system, with one-click install and update. An
// installed Library plugin is an ordinary user plugin in that system's
// plugins/ dir. Preferences > Taut's "Plugin library" row opens the tab.
//
// Neither upstream has a library, so the page is drawn with Slick's
// settings parts (it sits in Slick's group of tabs): the toolbar of search
// and filter chips, category cards of hairline-separated rows, badges.

// libraryTabID is the Library's Preferences tab.
const libraryTabID = "library"

var libraryTab = navExtra{id: libraryTabID, label: "Plugin library", icon: "bookmark"}

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

// libraryPage is Preferences > Plugin library.
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
}

func newLibraryPage(a *App, openTab func(id string)) *libraryPage {
	s := newSlickPage(a)
	return &libraryPage{a: a, s: s, openTab: openTab, busy: map[string]bool{}, status: map[string]string{}, failed: map[string]bool{}}
}

func (v *libraryPage) close() { v.s.close() }

// libKey names an entry across both systems (ids may repeat: Taut and
// Slick both have a WhoReacted).
func libKey(e pluginlib.Entry) string { return e.System + "/" + e.ID }

// libraryEntries is the index's entries of both systems, grouped by
// category in Taut's and Slick's category order, by name within one.
func libraryEntries(ix *pluginlib.Index) []pluginlib.Entry {
	if ix == nil {
		return nil
	}
	out := append(ix.For(pluginlib.Taut), ix.For(pluginlib.Slick)...)
	sort.SliceStable(out, func(i, j int) bool {
		ci, cj := libraryCategoryRank(out[i].CategoryLabel), libraryCategoryRank(out[j].CategoryLabel)
		if ci != cj {
			return ci < cj
		}
		if out[i].CategoryLabel != out[j].CategoryLabel {
			return out[i].CategoryLabel < out[j].CategoryLabel
		}
		if ni, nj := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name); ni != nj {
			return ni < nj
		}
		return out[i].System < out[j].System // Taut first
	})
	return out
}

// libraryCategories is Taut's PLUGIN_CATEGORIES and Slick's, merged in
// their own order (shared/Plugin.ts, src/shared/settings.ts).
var libraryCategories = []string{"Messages", "Message Box", "Composer", "People", "Channels", "Huddles & sounds",
	"Privacy", "Look & feel", "App", "Fun", "Hack Club Specialities"}

func libraryCategoryRank(label string) int {
	for i, l := range libraryCategories {
		if l == label {
			return i
		}
	}
	return len(libraryCategories) // Other and unknown labels last
}

// libraryCategoryIcons are Slick's CATEGORY_ICONS, plus Taut's own keys.
var libraryCategoryIcons = map[string]string{
	"messages": "message", "messageBox": "compose", "composer": "compose", "people": "user", "channels": "channel",
	"sounds": "headphones", "privacy": "lock", "appearance": "paintbrush", "app": "settings", "fun": "emoji", "hackclub": "hackclub",
}

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
				v.status[k] = "Installed. Turn it on in " + systemName(e.System) + "'s plugin list."
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

// libraryFilters: All, Taut, Slick, Installed, and Updates while there are some.
func (v *libraryPage) filters(entries []pluginlib.Entry) []slick.Filter {
	n := map[string]int{"all": len(entries)}
	for _, e := range entries {
		n[e.System]++
		switch v.state(e) {
		case pluginlib.Installed:
			n["installed"]++
		case pluginlib.UpdateAvailable:
			n["installed"]++
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

func (v *libraryPage) matches(e pluginlib.Entry, words []string) bool {
	switch v.s.filter {
	case pluginlib.Taut, pluginlib.Slick:
		if e.System != v.s.filter {
			return false
		}
	case "installed":
		if v.state(e) == pluginlib.NotInstalled {
			return false
		}
	case "updates":
		if v.state(e) != pluginlib.UpdateAvailable {
			return false
		}
	}
	return libraryMatches(e, words)
}

func libraryMatches(e pluginlib.Entry, words []string) bool {
	hay := strings.ToLower(strings.Join(append([]string{e.ID, e.Name, e.Description, e.CategoryLabel, systemName(e.System)}, e.Credit()...), " "))
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

// layout draws the Library tab.
func (v *libraryPage) layout(gtx layout.Context) layout.Dimensions {
	p := v.s
	k := p.k
	k.gtxNow = gtx
	cl := p.colors()
	if v.ix == nil && !v.loading && v.loadErr == "" {
		v.load(false)
	}
	entries := libraryEntries(v.ix)
	fs := v.filters(entries)
	updates := 0
	for _, e := range entries {
		if v.state(e) == pluginlib.UpdateAvailable {
			updates++
		}
	}
	sz := gtx.Constraints.Max
	p.list.List.Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Min: image.Pt(sz.X, 0), Max: image.Pt(sz.X-gtx.Dp(8), 1<<24)}
		f := &flow{gtx: gtx, w: gtx.Constraints.Max.X}
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
			if updates > 0 {
				add("lib.updateall", fmt.Sprintf("Update all (%d)", updates), popups.BtnPrimary, func() {
					for _, e := range entries {
						if v.state(e) == pluginlib.UpdateAvailable {
							v.installEntry(e, true)
						}
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
		intro := "Every plugin in Taut's and Slick's own repositories, built unmodified by that project's build. They're third-party code under GPLv3 by their authors. Installing one adds it as a user plugin of its system; turn it on in that system's plugin list."
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
			return layout.Dimensions{Size: image.Pt(sz.X, f.y)}
		}
		// toolbar: Slick's search and filter chips
		f.put(0, func(gtx layout.Context) layout.Dimensions { return v.toolbar(gtx, fs) })
		f.gap(20)
		words := strings.Fields(strings.ToLower(p.query.Text()))
		var group []pluginlib.Entry
		label, shown := "", 0
		flush := func() {
			if len(group) == 0 {
				return
			}
			rows := make([]func(gtx layout.Context, w int) layout.Dimensions, len(group))
			for i, e := range group {
				rows[i] = func(gtx layout.Context, w int) layout.Dimensions { return v.row(gtx, w, e) }
			}
			icon := libraryCategoryIcons[group[0].Category]
			if icon == "" {
				icon = "code"
			}
			p.card(f, orStr(label, "Other"), icon, nil, rows)
			group = nil
		}
		for _, e := range entries {
			if !v.matches(e, words) {
				continue
			}
			if e.CategoryLabel != label {
				flush()
				label = e.CategoryLabel
			}
			group = append(group, e)
			shown++
		}
		flush()
		if shown == 0 {
			if q := strings.TrimSpace(p.query.Text()); q != "" {
				p.empty(f, "No plugins match “"+q+"”.")
			} else {
				p.empty(f, "No plugins in this view.")
			}
		}
		f.gap(24)
		return layout.Dimensions{Size: image.Pt(sz.X, f.y)}
	})
	return layout.Dimensions{Size: sz}
}

// toolbar is Slick's: the search box (search icon, 34px inset) with the
// filter chips beside it, or below when they don't fit.
func (v *libraryPage) toolbar(gtx layout.Context, fs []slick.Filter) layout.Dimensions {
	p := v.s
	cl := p.colors()
	w := gtx.Constraints.Max.X
	chipW := func(f slick.Filter) int {
		return gtx.Dp(24) + p.a.th.Measure(gtx, 13, theme.Bold, f.Label) + gtx.Dp(6) + p.a.th.Measure(gtx, 13, theme.Regular, strconv.Itoa(f.Count))
	}
	chipsW := 0
	for i, f := range fs {
		if i > 0 {
			chipsW += gtx.Dp(6)
		}
		chipsW += chipW(f)
	}
	inputH := gtx.Dp(36)
	searchW, chipsX, chipsY := w-chipsW-gtx.Dp(12), 0, 0
	if searchW < gtx.Dp(220) {
		searchW, chipsY = w, inputH+gtx.Dp(8)
	} else {
		chipsX, chipsY = searchW+gtx.Dp(12), (inputH-gtx.Dp(28))/2
	}
	at(gtx, image.Point{}, image.Pt(searchW, inputH), func(gtx layout.Context) layout.Dimensions {
		d := p.sinput(gtx, &p.query, searchW, inputH, gtx.Dp(34), "Search the library", false)
		at(gtx, image.Pt(gtx.Dp(10), (inputH-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
			return icons.Get("search")(gtx, 16, cl.ter)
		})
		return d
	})
	x, y := chipsX, chipsY
	for _, f := range fs {
		cw := chipW(f)
		if x+cw > w && x > chipsX {
			x, y = chipsX, y+gtx.Dp(28+8)
		}
		at(gtx, image.Pt(x, y), image.Pt(cw, gtx.Dp(28)), func(gtx layout.Context) layout.Dimensions { return p.chip(gtx, f, cw) })
		x += cw + gtx.Dp(6)
	}
	return layout.Dimensions{Size: image.Pt(w, max(inputH, y+gtx.Dp(28)))}
}

// row is a Slick plugin row with the switch swapped for the action:
// name (15/700) with its system's badge and any flags, the description
// (13/18, two lines), "By …" (13, tertiary), the last result, and
// Install / Update / Settings at the right.
func (v *libraryPage) row(gtx layout.Context, w int, e pluginlib.Entry) layout.Dimensions {
	p := v.s
	k := p.k
	cl := p.colors()
	key := libKey(e)
	state := v.state(e)
	label, kind := "Install", popups.BtnPrimary
	switch {
	case v.busy[key]:
		label, kind = "Installing...", popups.BtnDisabled
		if state != pluginlib.NotInstalled {
			label = "Updating..."
		}
	case state == pluginlib.UpdateAvailable:
		label = "Update"
	case state == pluginlib.Installed:
		label, kind = "Settings", popups.BtnOutline
	}
	bw := gtx.Dp(96)
	tx, gap := gtx.Dp(16), gtx.Dp(14)
	tw := w - tx - gtx.Dp(12) - bw - gap
	textCol := func(gtx layout.Context) layout.Dimensions {
		nd := p.a.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Weight: theme.Bold, Color: cl.pry, MaxLines: 1}, e.Name)
		bx := min(nd.Size.X, tw) + gtx.Dp(8)
		badge := func(label string, warn bool) {
			d := at(gtx, image.Pt(bx, gtx.Dp(1)), image.Pt(gtx.Dp(200), gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
				return p.badge(gtx, label, "", warn)
			})
			bx += d.Size.X + gtx.Dp(6)
		}
		badge(systemName(e.System), false)
		if e.HackClubOnly {
			badge("Hack Club only", false)
		}
		if e.UpstreamDisabled {
			badge("Off upstream", true)
		}
		y := gtx.Dp(22)
		if e.Description != "" {
			y += at(gtx, image.Pt(0, y), image.Pt(tw, 1<<20), func(gtx layout.Context) layout.Dimensions {
				return p.a.th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 18, Color: cl.sec, MaxLines: 2, Wrap: text.WrapWords}, plainMrkdwn(e.Description))
			}).Size.Y
		}
		if by := libraryCredit(e); by != "" {
			y += gtx.Dp(4)
			y += at(gtx, image.Pt(0, y), image.Pt(tw, 1<<20), func(gtx layout.Context) layout.Dimensions {
				return k.mrkdwn(gtx, "lib.by."+key, "By "+by, true, cl.ter)
			}).Size.Y
		}
		if st := v.status[key]; st != "" {
			col := cl.ter
			if v.failed[key] {
				col = cl.imp
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
	at(gtx, image.Pt(w-gtx.Dp(12)-bw, (h-gtx.Dp(28))/2), image.Pt(bw, gtx.Dp(28)), func(gtx layout.Context) layout.Dimensions {
		return popups.Button(gtx, p.a.th, b, kind, popups.BtnSmall, label, "", bw)
	})
	if hit && kind != popups.BtnDisabled {
		switch state {
		case pluginlib.NotInstalled:
			v.installEntry(e, false)
		case pluginlib.UpdateAvailable:
			v.installEntry(e, true)
		default:
			if h, ok := v.host(e.System); ok && h.open != nil {
				h.open(e.ID)
			}
		}
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
