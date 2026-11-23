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
	"unicode"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/pluginbench"
	"github.com/some-du6e/clack/internal/pluginlib"
	"github.com/some-du6e/clack/internal/slick"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The plugin Library is Preferences > Plugins > Plugin library: every official
// Taut and Slick plugin from the Clack repo's `plugins` release
// (internal/pluginlib), each built unmodified by its project's own build,
// with one-click install and update. A plugin both systems have is one row
// with both tags, under one name or names of their own (pluginlib.Pairs,
// docs/PLUGINS.md's pair table); Install installs the variant the plugin
// bench picked (internal/pluginbench, docs/PLUGIN-BENCH.md), the row says
// which, and the chevron beside Install offers both, the pick first with
// its reason (without bench data, Install asks which). Once either is
// installed the row shows that one, whichever it is. An installed Library
// plugin is an ordinary user plugin in that system's plugins/ dir.
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
	// enable turns an installed plugin on (nil: installs stay as they are)
	enable func(ctx context.Context, id string) error
}

// libraryPage is Preferences > Plugins > Plugin library (mode "browse").
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

	// states are the entries' install states by libKey, read through their
	// hosts (each read opens the plugin's files and hashes them) once, and
	// again only when statesFor moves: a plugin system starts or stops, or
	// its installed plugins change (an install, an update, a removal:
	// PluginsGen). A frame reads every row's state several times;
	// scrolling read the files of all 71 every frame.
	states    map[string]pluginlib.State
	statesFor libStatesKey
	// groups is libraryGroups of groupsOf's entries, built once an index
	groups   []libGroup
	groupsOf *pluginlib.Index
}

// libStatesKey is what the install states were read under.
type libStatesKey struct {
	up        [2]bool // Taut, Slick running
	taut, sli uint64  // their PluginsGens
	ix        *pluginlib.Index
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
			enable: func(ctx context.Context, id string) error { return t.SetEnabled(ctx, id, true) },
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
			enable: func(ctx context.Context, id string) error { return s.SetEnabled(ctx, id, true) },
		}, true
	}
	return libraryHost{}, false
}

// state is an entry's install state in its system (NotInstalled when the
// system isn't running), read once until the systems change (states).
func (v *libraryPage) state(e pluginlib.Entry) pluginlib.State {
	if k := v.statesKey(); k != v.statesFor || v.states == nil {
		v.states, v.statesFor = map[string]pluginlib.State{}, k
	}
	key := libKey(e)
	if st, ok := v.states[key]; ok {
		return st
	}
	st := pluginlib.NotInstalled
	if h, ok := v.host(e.System); ok {
		c, m := h.installed(e.ID)
		st = pluginlib.StateOf(e, c, m)
	}
	v.states[key] = st
	return st
}

// statesKey is libStatesKey now.
func (v *libraryPage) statesKey() libStatesKey {
	k := libStatesKey{ix: v.ix}
	if v.hosts != nil {
		_, k.up[0] = v.hosts(pluginlib.Taut)
		_, k.up[1] = v.hosts(pluginlib.Slick)
	} else {
		k.up = [2]bool{v.a.Taut() != nil, v.a.Slick() != nil}
	}
	if t := v.a.Taut(); t != nil {
		k.taut = t.PluginsGen()
	}
	if s := v.a.Slick(); s != nil {
		k.sli = s.PluginsGen()
	}
	return k
}

// forgetStates reads the install states again at their next use.
func (v *libraryPage) forgetStates() { v.states = nil }

// allGroups is libraryGroups(libraryEntries(v.ix)), built once an index.
func (v *libraryPage) allGroups() []libGroup {
	if v.groupsOf != v.ix || v.groups == nil && v.ix != nil {
		v.groups, v.groupsOf = libraryGroups(libraryEntries(v.ix)), v.ix
	}
	return v.groups
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
		v.groups, v.states = nil, nil
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
				v.groups, v.states = nil, nil // read again, even from the same index
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
		// installing turns the plugin on (an update keeps its state), but
		// one that doesn't work in Clack stays off: its row says why
		works := pluginlib.CompatOf(e.System, e.ID).Status != pluginlib.CompatBroken
		if err == nil && !update && works && h.enable != nil {
			err = h.enable(ctx, e.ID)
		}
		a.Post(func() {
			delete(v.busy, k)
			v.forgetStates() // the system's PluginsGen moves too; a failed install may have left files
			switch {
			case err != nil:
				v.status[k], v.failed[k] = err.Error(), true
			case update:
				v.status[k] = "Updated."
			case !works:
				v.status[k] = "Installed and left off: it doesn't work in " + AppName + "."
			default:
				v.status[k] = "Installed and turned on."
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

// libGroup is one Library listing: a plugin, in one system or in both
// (many plugins exist in Taut and in Slick, most under one name, some
// under names of their own: Taut's No Tracking is Slick's No Track).
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
// row, and so do the ids of a plugin with a name of its own in each system
// (pluginlib.Pairs, docs/PLUGINS.md's pair table: the one source of
// pairs; bench.json follows it, TestBenchPairsAreLibraryRows).
func libraryGroups(entries []pluginlib.Entry) []libGroup {
	// entries join by name and by pair, each row at its earliest entry
	root := make([]int, len(entries))
	for i := range root {
		root[i] = i
	}
	find := func(i int) int {
		for root[i] != i {
			root[i] = root[root[i]]
			i = root[i]
		}
		return i
	}
	join := func(i, j int) {
		if a, b := find(i), find(j); a != b {
			root[max(a, b)] = min(a, b)
		}
	}
	byName, byID := map[string]int{}, map[string]int{}
	for i, e := range entries {
		k := libNameKey(orStr(e.Name, e.ID))
		if j, ok := byName[k]; ok {
			join(j, i)
		} else {
			byName[k] = i
		}
		byID[libKey(e)] = i
	}
	for _, p := range pluginlib.Pairs() {
		i, ok := byID[pluginlib.Taut+"/"+p.Taut]
		j, ok2 := byID[pluginlib.Slick+"/"+p.Slick]
		if ok && ok2 {
			join(i, j)
		}
	}
	var out []libGroup
	at := map[int]int{}
	for i, e := range entries {
		r := find(i)
		if k, ok := at[r]; ok {
			out[k].Entries = append(out[k].Entries, e)
			continue
		}
		at[r] = len(out)
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
	gs := v.allGroups()
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

// libraryMatches reports whether e is a result for words: libraryRank.
func libraryMatches(e pluginlib.Entry, words []string) bool { return libraryRank(e, words) >= 0 }

// libraryRank is how well words match a library plugin, for the search
// suggestions under Preferences > Plugins (and their "See all N
// results") and the library's own search: the worst of each word's
// match, -1 when one of them doesn't match. A word matches the name it
// starts (0), a word of the name it starts (1), somewhere in the name
// (2; the name without spaces counts, so "clearurls" is Clear URLs), or,
// from 3 letters, a word of the description it starts (3). The ID, the
// category, the system and the authors don't count: a category's name
// would match everything filed under it.
func libraryRank(e pluginlib.Entry, words []string) int {
	if len(words) == 0 {
		return 0
	}
	name := strings.ToLower(orStr(e.Name, e.ID))
	key := libNameKey(name)
	nameWords := searchWords(name)
	var descWords []string
	worst := 0
	for _, w := range words {
		wk := libNameKey(w)
		t := -1
		switch {
		case strings.HasPrefix(name, w) || wk != "" && strings.HasPrefix(key, wk):
			t = 0
		case wordStarts(nameWords, w):
			t = 1
		case strings.Contains(name, w) || wk != "" && strings.Contains(key, wk):
			t = 2
		case len([]rune(w)) >= 3:
			if descWords == nil {
				descWords = searchWords(plainMrkdwn(e.Description))
			}
			if wordStarts(descWords, w) {
				t = 3
			}
		}
		if t < 0 {
			return -1
		}
		worst = max(worst, t)
	}
	return worst
}

// groupCompat is how a library row works in Clack (pluginlib.CompatOf):
// its best entry's, since Install takes the bench's pick when both
// systems have it.
func groupCompat(g libGroup) pluginlib.Compat {
	best := pluginlib.Compat{Status: pluginlib.CompatBroken + 1}
	for _, e := range g.Entries {
		if c := pluginlib.CompatOf(e.System, e.ID); c.Status < best.Status {
			best = c
		}
	}
	return best
}

// rowPick is how a row's version came to be the one it shows.
type rowPick int

const (
	pickAsks      rowPick = iota // both systems run, no bench data: Install asks which
	pickOnly                     // one system has it, or runs: Install installs that one
	pickBench                    // the plugin bench's pick: Install installs it
	pickInstalled                // installed (the first, when both are)
)

// face is the entry a row shows (its name, description and how it works
// in Clack): the installed one, else the one Install installs (the plugin
// bench's pick, or the only system's), else, while Install asks which,
// its Taut entry. Installing the other version (Install's chevron) makes
// that one the face; nothing swaps an installed one.
func (v *libraryPage) face(g libGroup) (pluginlib.Entry, rowPick) {
	if installed, _ := v.groupStates(g); len(installed) > 0 {
		return installed[0], pickInstalled
	}
	cs := v.installChoices(g)
	switch len(cs) {
	case 0:
		return g.first(), pickOnly
	case 1:
		return cs[0], pickOnly
	}
	if w, _, ok := v.benchPick(cs); ok {
		return w, pickBench
	}
	return g.first(), pickAsks
}

// home is the version a row shows before either is installed: the one
// Install installs (the bench's pick, or the only running system's), else
// its Taut entry. The row is filed and sorted as it (cards).
func (v *libraryPage) home(g libGroup) pluginlib.Entry {
	cs := v.installChoices(g)
	if len(cs) == 1 {
		return cs[0]
	}
	if w, _, ok := v.benchPick(cs); ok {
		return w
	}
	return g.first()
}

// rowCompat is how a row's plugin works in Clack: its face's, or while
// Install asks which version, its best (groupCompat).
func rowCompat(g libGroup, face pluginlib.Entry, pick rowPick) pluginlib.Compat {
	if pick == pickAsks {
		return groupCompat(g)
	}
	return pluginlib.CompatOf(face.System, face.ID)
}

// rowDescription is the face's description, or another version's when it
// has none.
func rowDescription(g libGroup, face pluginlib.Entry) string {
	if face.Description != "" {
		return face.Description
	}
	for _, e := range g.Entries {
		if e.Description != "" {
			return e.Description
		}
	}
	return ""
}

// versions is a row's line about a plugin both systems have ("", "" for
// one in one system): which, the version Install installs or the one
// installed; alias, the other's name where it has its own ("In Slick it's
// “No Track”."), so a search by that name makes sense of the row it finds.
func (v *libraryPage) versions(g libGroup, face pluginlib.Entry, pick rowPick) (which, alias string) {
	both := false
	for _, e := range g.Entries {
		both = both || e.System != face.System
	}
	if !both {
		return "", ""
	}
	switch pick {
	case pickInstalled:
		if installed, _ := v.groupStates(g); len(installed) > 1 {
			which = "Both versions are installed."
		} else {
			which = systemName(face.System) + "'s version is installed."
		}
	case pickBench:
		which = "Install adds " + systemName(face.System) + "'s version (recommended)."
	case pickOnly:
		which = "Install adds " + systemName(face.System) + "'s version."
	default:
		which = "Install asks which: Taut's version or Slick's."
	}
	var as []string
	for _, e := range g.Entries {
		if e.System != face.System && libNameKey(orStr(e.Name, e.ID)) != libNameKey(orStr(face.Name, face.ID)) {
			as = append(as, "In "+systemName(e.System)+" it's \u201c"+orStr(e.Name, e.ID)+"\u201d.")
		}
	}
	return which, strings.Join(as, " ")
}

// groupRank is a row's best libraryRank over its systems' entries.
func groupRank(g libGroup, words []string) int {
	best := -1
	for _, e := range g.Entries {
		if r := libraryRank(e, words); r >= 0 && (best < 0 || r < best) {
			best = r
		}
	}
	return best
}

// searchWords splits s into lower-case words: at anything but letters and
// digits, and where a capital follows a small letter ("DeArrow": de,
// arrow).
func searchWords(s string) []string {
	var out []string
	var b strings.Builder
	var prev rune
	flush := func() {
		if b.Len() > 0 {
			out = append(out, b.String())
			b.Reset()
		}
	}
	for _, r := range s {
		switch {
		case !unicode.IsLetter(r) && !unicode.IsDigit(r):
			flush()
		case unicode.IsUpper(r) && unicode.IsLower(prev):
			flush()
			b.WriteRune(unicode.ToLower(r))
		default:
			b.WriteRune(unicode.ToLower(r))
		}
		prev = r
	}
	flush()
	return out
}

// wordStarts reports whether one of words starts with w.
func wordStarts(words []string, w string) bool {
	for _, x := range words {
		if strings.HasPrefix(x, w) {
			return true
		}
	}
	return false
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

// body draws the Library under Plugins > Plugin library (libraryScreen
// draws its search box and chips above): its head (Refresh, Update all),
// the intro, and the rows by category. words is the shared search.
// head is the library's section head (its title, Update all and
// Refresh), its intro and its notices, drawn into f; the cards follow
// (cards).
func (v *libraryPage) head(f *flow) {
	p := v.s
	k := p.k
	k.gtxNow = f.gtx
	cl := p.colors()
	if v.ix == nil && !v.loading && v.loadErr == "" {
		v.load(false)
	}
	groups := v.allGroups()
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
	if v.ix == nil && v.loading {
		p.hint(f, "Loading the plugin library...")
	}
}

// libCard is one category's card: its title and its rows.
type libCard struct {
	label, icon string
	rows        []libGroup
}

// cards are the rows the chip and the search words leave, by category
// (a search's best matches first within one), and the empty state's
// text when none are left ("" while the index loads). A row is filed and
// sorted as the version it shows before either is installed (home: the
// bench's pick, else its Taut entry), under that version's category by its
// name, as a plugin in one system is. Installing the other version renames
// the row where it is rather than moving it away from the click. No
// upstream lists both systems' plugins to follow: Taut's list and Slick's
// each hold their own.
func (v *libraryPage) cards(words []string) (cards []libCard, empty string) {
	if v.ix == nil {
		return nil, ""
	}
	type shownRow struct {
		g    libGroup
		face pluginlib.Entry // its home
	}
	var rows []shownRow
	for _, g := range v.allGroups() {
		if v.matches(g, words) {
			rows = append(rows, shownRow{g, v.home(g)})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if ci, cj := categoryRank(libCategory(rows[i].face).Key), categoryRank(libCategory(rows[j].face).Key); ci != cj {
			return ci < cj
		}
		return strings.ToLower(orStr(rows[i].face.Name, rows[i].face.ID)) < strings.ToLower(orStr(rows[j].face.Name, rows[j].face.ID))
	})
	for i := 0; i < len(rows); {
		c := libCategory(rows[i].face)
		var group []libGroup
		for ; i < len(rows) && libCategory(rows[i].face).Key == c.Key; i++ {
			group = append(group, rows[i].g)
		}
		if len(words) > 0 { // a search: the best matches of a category first
			group = v.rank(group, words, nil)
		}
		cards = append(cards, libCard{label: c.Label, icon: c.Icon, rows: group})
	}
	if len(rows) == 0 {
		if len(words) > 0 {
			return nil, "No plugins match \u201c" + strings.Join(words, " ") + "\u201d."
		}
		return nil, "No plugins in this view."
	}
	return cards, ""
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
	case len(installed) > 1: // both versions: the chevron reaches either one's settings
		e := installed[0]
		return libAction{label: "Settings", kind: popups.BtnOutline, do: func() { v.openSettings(e) },
			menu: func() { v.a.menu(v.a.PointerPos(), v.settingsMenu(installed)) }}
	case len(installed) > 0:
		e := installed[0]
		act := libAction{label: "Settings", kind: popups.BtnOutline, do: func() { v.openSettings(e) }}
		// a version that does nothing in Clack keeps the other one offered
		// (nothing swaps it: installing the other is the user's pick)
		if pluginlib.CompatOf(e.System, e.ID).Status == pluginlib.CompatBroken {
			var others []pluginlib.Entry
			for _, c := range v.installChoices(g) {
				if c.System != e.System {
					others = append(others, c)
				}
			}
			if len(others) > 0 {
				act.menu = func() { v.a.menu(v.a.PointerPos(), v.installMenuFor(others, e)) }
			}
		}
		return act
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

// openSettings shows an installed entry's settings, in its system's page.
func (v *libraryPage) openSettings(e pluginlib.Entry) {
	if h, ok := v.host(e.System); ok && h.open != nil {
		h.open(e.ID)
	}
}

// settingsMenu is "Settings for" each of a row's installed versions.
func (v *libraryPage) settingsMenu(installed []pluginlib.Entry) []MenuItem {
	items := []MenuItem{{Label: "Settings for", Disabled: true}}
	for _, e := range installed {
		items = append(items, MenuItem{Label: systemName(e.System),
			Subtitle: systemName(e.System) + "'s version, \u201c" + orStr(e.Name, e.ID) + "\u201d", Do: func() { v.openSettings(e) }})
	}
	return items
}

// benchPick is the variant of choices the plugin bench picked, with its
// reason. ok is false without bench data for this plugin, and with data
// that checked nothing it does (Pick.Checked): costs alone can't tell a
// version that does nothing in Clack from one that works, so Install asks.
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
	if !ok || !p.Checked {
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
// "(recommended)" with its reason, then the other system: how it works in
// Clack when that's short of all it does, else named when its version's
// name isn't the row's ("Slick's version, “No Track”").
func (v *libraryPage) installMenu(choices []pluginlib.Entry) []MenuItem {
	if len(choices) == 0 {
		return []MenuItem{{Label: "Install for", Disabled: true}}
	}
	title := choices[0] // the row's face (face): the pick, else the Taut entry
	if w, _, ok := v.benchPick(choices); ok {
		title = w
	}
	return v.installMenuFor(choices, title)
}

// installMenuFor is installMenu for a row whose face is title.
func (v *libraryPage) installMenuFor(choices []pluginlib.Entry, title pluginlib.Entry) []MenuItem {
	items := []MenuItem{{Label: "Install for", Disabled: true}}
	order := append([]pluginlib.Entry(nil), choices...)
	w, pick, ok := v.benchPick(choices)
	if ok {
		sort.SliceStable(order, func(i, j int) bool { return order[i].System == w.System && order[j].System != w.System })
	}
	for _, e := range order {
		it := MenuItem{Label: systemName(e.System), Subtitle: systemBlurb(e.System), Do: func() { v.installEntry(e, false) }}
		if n := orStr(e.Name, e.ID); libNameKey(n) != libNameKey(orStr(title.Name, title.ID)) {
			it.Subtitle = systemName(e.System) + "'s version, \u201c" + n + "\u201d"
		}
		if c := pluginlib.CompatOf(e.System, e.ID); c.Line != "" {
			it.Subtitle = c.Line
		}
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
// (13/18, two lines), for a plugin both systems have which version
// Install installs or is installed (13/18, tertiary), "By …" (13,
// tertiary), the last result, and Install / Update / Settings at the
// right. The name, description and how it works in Clack are its face's.
func (v *libraryPage) row(gtx layout.Context, w int, g libGroup) layout.Dimensions {
	p := v.s
	k := p.k
	cl := p.colors()
	e0, pick := v.face(g)
	key := libKey(g.first()) // the row's widgets, whichever version it shows
	act := v.action(g)
	bw := gtx.Dp(96)
	tx, gap := gtx.Dp(16), gtx.Dp(14)
	tw := w - tx - gtx.Dp(12) - bw - gap
	var hc, off bool
	for _, e := range g.Entries {
		hc = hc || e.HackClubOnly
		off = off || e.UpstreamDisabled
	}
	desc := rowDescription(g, e0)
	which, alias := v.versions(g, e0, pick)
	versions := strings.TrimSpace(which + " " + alias)
	credit := groupCredit(p.a.credits(), g) // mentions (plugin_credit.go)
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
		if c := rowCompat(g, e0, pick); c.Line != "" { // how it works in Clack, before Install
			col := cl.sec
			if c.Status == pluginlib.CompatBroken {
				col = cl.imp
			}
			y += at(gtx, image.Pt(0, y), image.Pt(tw, gtx.Dp(18)), func(gtx layout.Context) layout.Dimensions {
				return p.a.th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 18, Color: col, MaxLines: 1}, c.Line)
			}).Size.Y
		}
		if desc != "" {
			y += at(gtx, image.Pt(0, y), image.Pt(tw, 1<<20), func(gtx layout.Context) layout.Dimensions {
				return p.a.th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 18, Color: cl.sec, MaxLines: 2, Wrap: text.WrapWords}, plainMrkdwn(desc))
			}).Size.Y
		}
		if versions != "" {
			y += at(gtx, image.Pt(0, y), image.Pt(tw, 1<<20), func(gtx layout.Context) layout.Dimensions {
				return p.a.th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 18, Color: cl.ter, MaxLines: 2, Wrap: text.WrapWords}, versions)
			}).Size.Y
		}
		if credit != "" {
			y += gtx.Dp(4)
			y += at(gtx, image.Pt(0, y), image.Pt(tw, 1<<20), func(gtx layout.Context) layout.Dimensions {
				return k.mrkdwn(gtx, "lib.by."+key, "By "+credit, true, cl.ter)
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
