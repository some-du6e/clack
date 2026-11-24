package ui

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/pluginimport"
	"github.com/some-du6e/clack/internal/pluginlib"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Bringing a user's Slick or Taut plugins into Clack (Karim, 2026-11-23:
// "and import plugisn too" ... "in introduccion"). The plugins another app
// on this machine has (internal/pluginimport, which only reads, and only
// plugin lists, settings and plugin files) each go into Clack's own Taut
// or Slick through the plugin Library's install (its download and sha256
// check), then get the settings and the on or off they had there. One the
// Library says doesn't work in Clack is listed with why and left out; one
// installed there from a file or a URL comes over as its file. The
// introduction offers it when an install is found, and what's picked
// there comes over once Clack has signed in (pending); Preferences >
// Plugins > Bring over your plugins does it at any time.

// importItem is one plugin another app has, as the import offers it.
type importItem struct {
	found pluginimport.Found
	app   string          // the app it's from: Slick or Taut
	entry pluginlib.Entry // its Library entry (inLib)
	inLib bool
	name  string // the Library's name for it, else its id
	on    bool   // turned on there (or by its default)
	// reason it can't come over ("": it can): it doesn't work in Clack, or
	// the Library hasn't got it and the app no file of it
	reason string
	line   string // how it works in Clack when that's short of all it does
	have   bool   // installed in Clack already: its settings come
	pick   bool   // to bring over (at first: on there, and it can come)
}

func (it importItem) key() string { return it.found.System + "/" + it.found.ID }

// importItems lists what installs have against the Library's index ix
// (nil while it loads: a plugin is offered by its id, its compat row
// known) and Clack's installed plugins (installed, nil for none known).
func importItems(ix *pluginlib.Index, installs []pluginimport.Install, installed func(system, id string) bool) []importItem {
	lib := map[string]pluginlib.Entry{}
	if ix != nil {
		for _, e := range ix.Plugins {
			lib[e.System+"/"+e.ID] = e
		}
	}
	var out []importItem
	for _, in := range installs {
		for _, f := range in.Plugins {
			it := importItem{found: f, app: systemName(in.System), name: f.ID}
			e, ok := lib[f.System+"/"+f.ID]
			if ok {
				it.entry, it.inLib, it.name = e, true, orStr(e.Name, f.ID)
			}
			switch c := pluginlib.CompatOf(f.System, f.ID); {
			case c.Status == pluginlib.CompatBroken:
				it.reason = c.Line
			case f.Code == nil && ix != nil && !ok:
				it.reason = "Not in the plugin Library"
			case c.Status == pluginlib.CompatPartial:
				it.line = c.Line
			}
			if f.Enabled != nil {
				it.on = *f.Enabled
			} else {
				it.on = ok && e.DefaultEnabled
			}
			it.have = installed != nil && installed(f.System, f.ID)
			it.pick = it.on && it.reason == "" && !in.Off
			out = append(out, it)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].app != out[j].app {
			return out[i].app < out[j].app
		}
		return strings.ToLower(out[i].name) < strings.ToLower(out[j].name)
	})
	return out
}

// importResult is how one item went: err nil when it came over (settings
// the plugin didn't take are skipped).
type importResult struct {
	it      importItem
	err     error
	skipped []string
}

// importSystem is what the import needs of one of Clack's plugin systems.
type importSystem struct {
	installed  func(id string) bool
	importCode func(ctx context.Context, code []byte) error // a user plugin's file
	setOption  func(ctx context.Context, id, key string, value any) error
	setEnabled func(ctx context.Context, id string, on bool) error
}

// importer brings items over (off the UI goroutine): systems and hosts
// are Clack's plugin systems and the Library's installs (tests swap them).
type importer struct {
	a       *App
	systems func(system string) (importSystem, bool)
	hosts   func(system string) (libraryHost, bool)
}

func (a *App) importer() *importer {
	lp := newLibraryPage(a, nil)
	return &importer{a: a, systems: a.importSystem, hosts: lp.host}
}

// importerFor is the importer a run uses (tests swap it).
var importerFor = (*App).importer

// importSystem is Taut's or Slick's, nil-safe: false until it runs.
func (a *App) importSystem(system string) (importSystem, bool) {
	switch system {
	case pluginlib.Taut:
		t := a.Taut()
		if t == nil {
			return importSystem{}, false
		}
		return importSystem{
			installed: func(id string) bool { return t.InstalledSHA256(id) != "" },
			importCode: func(ctx context.Context, code []byte) error {
				_, err := t.Import(ctx, code, "")
				return err
			},
			setOption:  t.SetOption,
			setEnabled: t.SetEnabled,
		}, true
	case pluginlib.Slick:
		s := a.Slick()
		if s == nil {
			return importSystem{}, false
		}
		return importSystem{
			installed: func(id string) bool { r, _ := s.InstalledSHA256(id); return r != "" },
			importCode: func(ctx context.Context, code []byte) error {
				_, err := s.Import(ctx, code)
				return err
			},
			setOption:  s.SetOption,
			setEnabled: s.SetEnabled,
		}, true
	}
	return importSystem{}, false
}

// run brings the picked items over, one after another, and returns how
// each went.
func (im *importer) run(ctx context.Context, items []importItem) []importResult {
	var out []importResult
	for _, it := range items {
		if !it.pick || it.reason != "" {
			continue
		}
		r := importResult{it: it}
		r.skipped, r.err = im.one(ctx, it)
		if r.err != nil {
			log.Printf("ui: bringing over %s's %s: %v", it.app, it.found.ID, r.err)
		}
		out = append(out, r)
	}
	return out
}

func (im *importer) one(ctx context.Context, it importItem) (skipped []string, err error) {
	return nil, errors.New("mutation (plugin-import-mut1): nothing brought over")
}

// importSummary is one line on how results went.
func importSummary(rs []importResult) string {
	var ok, failed []string
	for _, r := range rs {
		if r.err == nil {
			ok = append(ok, r.it.name)
		} else {
			failed = append(failed, r.it.name)
		}
	}
	var parts []string
	switch len(ok) {
	case 0:
	case 1:
		parts = append(parts, "Brought over "+ok[0]+".")
	default:
		parts = append(parts, "Brought over "+strconv.Itoa(len(ok))+" plugins.")
	}
	if len(failed) > 0 {
		parts = append(parts, "Couldn't bring over "+strings.Join(failed, ", ")+".")
	}
	if len(parts) == 0 {
		return "Nothing was brought over."
	}
	return strings.Join(parts, " ")
}

// ---- the introduction's picks, kept until the plugins start ----

// pendingImport is what the introduction picked, until Clack has signed
// in and the plugin systems run: <config>/plugin-import.json.
type pendingImport struct {
	Picks []string `json:"picks"` // importItem.key
}

func (a *App) pendingImportPath() string {
	if a.opts.ConfigDir == "" {
		return ""
	}
	return filepath.Join(a.opts.ConfigDir, "plugin-import.json")
}

// savePendingImport keeps the picked items for after the sign-in.
func (a *App) savePendingImport(items []importItem) {
	// mutation (plugin-import-mut1): the picks aren't kept
}

// resumePendingImport brings over what the introduction picked, once the
// plugin systems run (installTaut), from the apps as they are now, and
// says how it went. The picks go either way: one run only.
func (a *App) resumePendingImport() {
	p := a.pendingImportPath()
	if p == "" {
		return
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return
	}
	_ = os.Remove(p)
	var pi pendingImport
	if json.Unmarshal(b, &pi) != nil || len(pi.Picks) == 0 {
		return
	}
	picked := map[string]bool{}
	for _, k := range pi.Picks {
		picked[k] = true
	}
	im := importerFor(a)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		var ix *pluginlib.Index
		if lib := a.pluginLib(); lib != nil {
			ix, _ = lib.Index(ctx, false)
		}
		items := importItems(ix, pluginimport.Find(), nil)
		for i := range items {
			items[i].pick = picked[items[i].key()] && items[i].reason == ""
		}
		rs := im.run(ctx, items)
		if len(rs) == 0 {
			return
		}
		a.Post(func() { a.ToastIcon(importSummary(rs), "check") })
	}()
}

// ---- Preferences > Plugins > Bring over your plugins ----

// importScreen is the screen's state: the apps found, their plugins
// against the Library, and how a run went.
type importScreen struct {
	loading, loaded bool
	installs        []pluginimport.Install
	items           []importItem
	ix              *pluginlib.Index // the index items were made with
	running         bool
	results         map[string]string // item key → how it went
	failed          map[string]bool
	summary         string
}

// findImports reads the other apps' plugins off the UI goroutine (files up
// to 16 MiB each) for the screen and the Plugins row.
func (p *tautPage) findImports() {
	s := &p.imp
	if s.loading || s.loaded {
		return
	}
	s.loading = true
	a := p.a
	go func() {
		found := pluginimport.Find()
		a.Post(func() {
			s.loading, s.loaded, s.installs = false, true, found
			s.items, s.ix = nil, nil
			a.Invalidate()
		})
	}()
}

// importsFound reports whether another app's plugins were found (the
// Plugins row shows then).
func (p *tautPage) importsFound() bool {
	p.findImports()
	for _, in := range p.imp.installs {
		if len(in.Plugins) > 0 {
			return true
		}
	}
	return false
}

// importItemsNow is the screen's items, made again when the Library's
// index changes (a plugin's reason, its name).
func (p *tautPage) importItemsNow() []importItem {
	s := &p.imp
	var ix *pluginlib.Index
	if lib := p.a.pluginLib(); lib != nil {
		ix = lib.Cached()
		if ix == nil {
			lib.Refresh(libraryTTL, false, func(*pluginlib.Index, error) { p.a.Invalidate() })
		}
	}
	if s.items == nil || ix != s.ix {
		picks := map[string]bool{}
		for _, it := range s.items {
			picks[it.key()] = it.pick
		}
		a := p.a
		installed := func(system, id string) bool {
			sys, ok := a.importSystem(system)
			return ok && sys.installed(id)
		}
		s.items, s.ix = importItems(ix, s.installs, installed), ix
		for i := range s.items {
			if v, ok := picks[s.items[i].key()]; ok && s.items[i].reason == "" {
				s.items[i].pick = v
			}
		}
	}
	return s.items
}

func (p *tautPage) importScreenLayout(f *flow) {
	cl := p.colors()
	s := &p.imp
	p.subScreen(f, "Bring over your plugins", "Plugins")
	p.findImports()
	if !s.loaded {
		p.muted(f, "Looking for Slick and Taut on this computer...")
		return
	}
	items := p.importItemsNow()
	if len(items) == 0 {
		p.muted(f, "Clack didn't find Slick's or Taut's plugins on this computer.")
		return
	}
	p.muted(f, "Plugins from Slick and Taut on this computer, with their settings. Each one comes over as the plugin Library has it, and the other app is left as it is.")
	f.gap(16)
	app := ""
	for i := range items {
		it := &items[i]
		if it.app != app {
			if app != "" {
				f.gap(12)
			}
			app = it.app
			p.legend(f, "From "+app)
		}
		k := "import." + it.key()
		f.put(0, func(gtx layout.Context) layout.Dimensions {
			w := gtx.Constraints.Max.X
			x := gtx.Dp(26)
			if it.reason == "" && !s.running {
				if _, c := p.checkbox(gtx, k, it.pick); c {
					it.pick = !it.pick
				}
			}
			title := it.name
			if !it.on {
				title += " (off there)"
			}
			if it.have {
				title += " (in Clack already: its settings)"
			}
			if it.found.Code != nil {
				title += " (its own file)"
			}
			col := cl.fg
			if it.reason != "" {
				col = cl.fg2
			}
			nd := at(gtx, image.Pt(x, 0), image.Pt(w-x, 1<<20), func(gtx layout.Context) layout.Dimensions {
				return p.label(gtx, 15, theme.Regular, col, title)
			})
			h := nd.Size.Y
			sub := it.reason
			if sub == "" {
				sub = it.line
			}
			if r, ok := s.results[it.key()]; ok {
				sub = r
			}
			if sub != "" {
				sc := cl.fg2
				if s.failed[it.key()] {
					sc = cl.err
				}
				sd := at(gtx, image.Pt(x, h+gtx.Dp(2)), image.Pt(w-x, 1<<20), func(gtx layout.Context) layout.Dimensions {
					return p.para(gtx, 13, 18, sc, sub)
				})
				h += gtx.Dp(2) + sd.Size.Y
			}
			return layout.Dimensions{Size: image.Pt(w, max(h, gtx.Dp(20)))}
		})
		f.gap(10)
	}
	f.gap(14)
	n := 0
	for _, it := range items {
		if it.pick && it.reason == "" {
			n++
		}
	}
	f.put(0, func(gtx layout.Context) layout.Dimensions {
		label := "Bring them over"
		kind := popups.BtnPrimary
		switch {
		case s.running:
			label, kind = "Bringing them over...", popups.BtnDisabled
		case n == 0:
			kind = popups.BtnDisabled
		}
		d, c := p.button(gtx, "import.run", label, kind)
		if c && n > 0 && !s.running {
			p.runImportScreen(items)
		}
		return d
	})
	if s.summary != "" {
		f.gap(10)
		p.muted(f, s.summary)
	}
}

// runImportScreen brings the screen's picks over and shows how each went.
func (p *tautPage) runImportScreen(items []importItem) {
	s := &p.imp
	s.running, s.summary = true, ""
	s.results, s.failed = map[string]string{}, map[string]bool{}
	a := p.a
	im := importerFor(a)
	todo := append([]importItem(nil), items...)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		rs := im.run(ctx, todo)
		a.Post(func() {
			s.running = false
			for _, r := range rs {
				switch {
				case r.err != nil:
					s.results[r.it.key()], s.failed[r.it.key()] = "Couldn't bring it over: "+r.err.Error(), true
				case len(r.skipped) > 0:
					s.results[r.it.key()] = "Brought over; it didn't take " + strings.Join(r.skipped, ", ") + "."
				default:
					s.results[r.it.key()] = "Brought over."
				}
			}
			s.summary = importSummary(rs)
			s.items = nil // installed now: read again
			a.Invalidate()
		})
	}()
}
