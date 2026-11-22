package ui

import (
	"context"
	"encoding/json"
	"image"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/pluginbench"
	"github.com/some-du6e/clack/internal/pluginlib"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// publishedLibrary serves the plugins release's index.json as published
// (testdata/plugin-library-index.json: generated 2026-11-16, taut 6fe5123,
// slick b582605), every entry's metadata as the Library has it, with
// synthetic bundles in place of the ones a test installs (bundles:
// system/id → renderer and, for Slick, its privileged half). Nothing
// third-party runs and nothing is fetched from outside.
func publishedLibrary(t *testing.T, l *libServer, bundles map[string][2][]byte) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "plugin-library-index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ix pluginlib.Index
	if err := json.Unmarshal(b, &ix); err != nil {
		t.Fatal(err)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := range ix.Plugins {
		e := &ix.Plugins[i]
		code, ok := bundles[libKey(*e)]
		if !ok {
			continue
		}
		e.File = pluginlib.File{File: "synth-" + e.System + "-" + e.ID + ".js", SHA256: pluginlib.Hash(code[0]), Size: int64(len(code[0]))}
		l.files[e.File.File] = code[0]
		e.Main = nil
		if code[1] != nil {
			e.Main = &pluginlib.File{File: "synth-" + e.System + "-" + e.ID + ".main.js", SHA256: pluginlib.Hash(code[1]), Size: int64(len(code[1]))}
			l.files[e.Main.File] = code[1]
		}
	}
	out, err := json.Marshal(ix)
	if err != nil {
		t.Fatal(err)
	}
	l.files["index.json"] = out
}

// TestLibraryOneRowPerPlugin: Karim (2026-11-22, #clack, Windows α.140)
// found "No Track" (Slick) and "No Tracking" (Taut) as two rows of the
// plugin library, "two of the same thing". Over the published Library,
// through Preferences > Plugins > Browse's real Layout: each plugin both
// systems ship is one row, whatever its names (docs/PLUGINS.md's pair
// table), and says which version Install installs: the plugin bench's
// pick, with the other one in Install's "Install for" menu. Whichever
// version is installed, the row shows that one, and nothing swaps it.
// Search and the chips find the row under either version's name, it sits
// in one category, and a plugin in one system keeps its row as it was.
func TestLibraryOneRowPerPlugin(t *testing.T) {
	tautCode := synthTaut("NoTrack", "No Tracking", "Blocks Slack's built-in tracking and analytics requests", "privacy", false)
	slickCode := synthSlick("NoTrack", "No Track", "Disable Slack's built-in tracking and telemetry", "privacy")
	slickMain := []byte(`// slick-main {"id":"NoTrack"}
(globalThis.__slickMainPlugins ??= []).push((require) => {
return { id: "NoTrack", capabilities: [] };
});
`)
	ls := newLibServer(t)
	publishedLibrary(t, ls, map[string][2][]byte{
		pluginlib.Taut + "/NoTrack":  {tautCode, nil},
		pluginlib.Slick + "/NoTrack": {slickCode, slickMain},
	})
	a, _ := ttApp(t, t.TempDir())
	a.data.startTaut(a)
	t.Cleanup(func() { a.data.stopTaut() })
	tt, sl := a.Taut(), a.Slick()
	if tt == nil || sl == nil {
		t.Fatal("taut or slick didn't start")
	}
	pm := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}}
	pm.selectTab(libraryTabID) // Plugins > Browse
	defer pm.close()
	r := &ttRenderer{t: t, a: a, sz: image.Pt(800, 1400)}
	dialog := func(gtx layout.Context) { pm.layout(gtx) }
	// the whole list at once: every row laid out, as scrolling would
	tall := func(gtx layout.Context) {
		gtx.Constraints = layout.Exact(image.Pt(600, 40000))
		pm.plugins.layout(gtx)
	}
	r.frame(dialog)
	lp := pm.plugins.lib
	wait := func(what string, cond func() bool) {
		t.Helper()
		for deadline := time.Now().Add(30 * time.Second); !cond(); time.Sleep(10 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s (status %v, load error %q)", what, lp.status, lp.loadErr)
			}
			r.frame(dialog)
		}
	}
	wait("the index", func() bool { return lp.ix != nil && !lp.loading })
	// drawn is what a frame of the whole list draws: each string Text drew
	drawn := func() map[string]bool {
		var mu sync.Mutex
		got := map[string]bool{}
		prev := theme.DrawHook
		theme.DrawHook = func(s string) { mu.Lock(); got[s] = true; mu.Unlock() }
		r.frame(tall)
		theme.DrawHook = prev
		return got
	}
	has := func(g libGroup, sys, id string) bool {
		for _, e := range g.Entries {
			if e.System == sys && e.ID == id {
				return true
			}
		}
		return false
	}
	desc := func(g libGroup) string {
		var s []string
		for _, e := range g.Entries {
			s = append(s, e.System+"/"+e.ID+" "+e.Name)
		}
		return "[" + strings.Join(s, ", ") + "]"
	}
	rowOf := func(sys, id string) libGroup {
		t.Helper()
		for _, g := range lp.allGroups() {
			if has(g, sys, id) {
				return g
			}
		}
		t.Fatalf("no library row has %s/%s", sys, id)
		return libGroup{}
	}

	// one row per plugin: the pairs read from the sources, and the bench's
	// (Click to Load's ids differ); related plugins that aren't one keep
	// their rows
	for _, p := range []struct{ taut, slick string }{
		{"NoTrack", "NoTrack"}, {"AnonymizeFilenames", "AnonymiseFileNames"}, {"PrivateChannel", "PrivateChannelMapper"},
		{"RealMarkdown", "NotShitMarkdown"}, {"IdvStatus", "HcaStatus"}, {"BringBackSlackbot", "CustomSlackbot"},
		{"ClearURLs", "ClearURLs"}, {"ClickToLoad", "Click2Load"},
	} {
		if g := rowOf(pluginlib.Taut, p.taut); len(g.Entries) != 2 || !has(g, pluginlib.Slick, p.slick) {
			t.Errorf("Taut's %s and Slick's %s: row %s and row %s, want one row with both", p.taut, p.slick, desc(g), desc(rowOf(pluginlib.Slick, p.slick)))
		}
	}
	for _, k := range []string{"taut/NativeAtChannel", "slick/bChannel", "taut/FasterSlack", "slick/Snappy", "taut/HuddleActivity", "slick/HuddleMuteDetector"} {
		sys, id, _ := strings.Cut(k, "/")
		if g := rowOf(sys, id); len(g.Entries) != 1 {
			t.Errorf("%s shares a row: %s", k, desc(g))
		}
	}
	if n := len(lp.allGroups()); n != 65 {
		t.Errorf("%d library rows, want 65: the published 96 entries, 31 plugins of them in both systems", n)
	}
	nt := rowOf(pluginlib.Taut, "NoTrack")
	if len(nt.Entries) != 2 {
		t.Fatalf("No Tracking (Taut) and No Track (Slick) aren't one row: %s and %s", desc(nt), desc(rowOf(pluginlib.Slick, "NoTrack")))
	}

	// No Tracking / No Track: the bench checked nothing either version does
	// (costs only), so the Library recommends neither: Install asks which,
	// and the row says so and names the other version
	entry := func(g libGroup, sys string) pluginlib.Entry {
		for _, e := range g.Entries {
			if e.System == sys {
				return e
			}
		}
		t.Fatalf("row %s has no %s entry", desc(g), sys)
		return pluginlib.Entry{}
	}
	tautNT, slickNT := entry(nt, pluginlib.Taut), entry(nt, pluginlib.Slick)
	if act := lp.action(nt); act.label != "Install…" || act.menu != nil {
		t.Errorf("No Tracking / No Track, costs only: %q (menu %v), want Install… asking which", act.label, act.menu != nil)
	}
	ask := "Install asks which: Taut's version or Slick's. In Slick it's “No Track”."
	if face, line := libFace(lp, nt); face.System != pluginlib.Taut || line != ask {
		t.Errorf("the row shows %s's %q with %q, want Taut's %q with %q", face.System, face.Name, line, tautNT.Name, ask)
	}
	seen := drawn()
	if !seen[tautNT.Name] || !seen[ask] {
		t.Errorf("the list didn't draw the row's name %q and %q", tautNT.Name, ask)
	}
	if seen[slickNT.Name] || seen[plainMrkdwn(slickNT.Description)] {
		t.Errorf("the list drew %q or %q on its own: a second row for the other version", slickNT.Name, slickNT.Description)
	}
	menu := lp.installMenu(lp.installChoices(nt))
	if len(menu) != 3 || menu[1].Label != "Taut" || menu[2].Label != "Slick" || menu[2].Subtitle != "Slick's version, “No Track”" {
		t.Fatalf("Install for: %+v, want Taut's version, then Slick's named, neither recommended", menu)
	}
	ntIDs := []pluginbench.Entry{{System: pluginlib.Taut, ID: "NoTrack"}, {System: pluginlib.Slick, ID: "NoTrack"}}
	if pick, ok := pluginbench.Load().For(ntIDs); !ok || benchChecked(pick) {
		t.Errorf("bench.json's No Tracking / No Track: %+v (data %v), want costs only, as this test's asked-about pair", pick, ok)
	}

	installedIn := func(sys string) bool {
		if sys == pluginlib.Taut {
			return tt.InstalledSHA256("NoTrack") != ""
		}
		c, _ := sl.InstalledSHA256("NoTrack")
		return c != ""
	}
	remove := func(sys string) {
		t.Helper()
		var err error
		if sys == pluginlib.Taut {
			err = tt.Remove(context.Background(), "NoTrack")
		} else {
			err = sl.Remove(context.Background(), "NoTrack")
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	install := func(what string, do func(), e pluginlib.Entry) {
		t.Helper()
		delete(lp.status, libKey(e))
		do()
		wait(what, func() bool { return lp.status[libKey(e)] != "" })
		if lp.failed[libKey(e)] {
			t.Fatalf("%s: %q", what, lp.status[libKey(e)])
		}
	}
	// installed, whichever version: the row shows that one and its Settings
	installedRow := func(e, o pluginlib.Entry) {
		t.Helper()
		if !installedIn(e.System) || installedIn(o.System) {
			t.Fatalf("installed in Taut %v, in Slick %v, want %s's only", installedIn(pluginlib.Taut), installedIn(pluginlib.Slick), e.System)
		}
		if act := lp.action(nt); act.label != "Settings" {
			t.Errorf("with %s's version installed the row says %q, want Settings", e.System, act.label)
		}
		want := systemName(e.System) + "'s version is installed. In " + systemName(o.System) + " it's “" + o.Name + "”."
		if face, line := libFace(lp, nt); face.System != e.System || line != want {
			t.Errorf("installed: the row shows %s's %q with %q, want %s's %q with %q", face.System, face.Name, line, e.System, e.Name, want)
		}
		for _, f := range lp.chips() {
			if f.ID == "installed" && f.Count != 1 {
				t.Errorf("the Installed chip counts %d rows, want this one", f.Count)
			}
		}
		seen := drawn()
		if !seen[e.Name] || !seen[want] || seen[o.Name] {
			t.Errorf("installed %s's: drawn %q %v, %q %v, %q %v; want the first two only", e.System, e.Name, seen[e.Name], want, seen[want], o.Name, seen[o.Name])
		}
	}
	install("Install for > Taut", menu[1].Do, tautNT)
	installedRow(tautNT, slickNT)

	// the other version, picked from Install's menu once Taut's is gone, is
	// the row's: nothing swaps it back
	remove(pluginlib.Taut)
	wait("the removal", func() bool { return lp.action(nt).label == "Install…" })
	menu = lp.installMenu(lp.installChoices(nt))
	if len(menu) != 3 || menu[2].Label != "Slick" || menu[2].Do == nil {
		t.Fatalf("Install for after the removal: %+v", menu)
	}
	install("Install for > Slick", menu[2].Do, slickNT)
	for end := time.Now().Add(300 * time.Millisecond); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		r.frame(dialog)
	}
	installedRow(slickNT, tautNT)

	// both versions installed (someone who had both before the rows
	// merged): Settings opens the first, and the chevron reaches either
	// one's settings
	install("Taut's version as well", func() { lp.installEntry(tautNT, false) }, tautNT)
	if act := lp.action(nt); act.label != "Settings" || act.menu == nil {
		t.Errorf("both versions installed: %q (menu %v), want Settings and a chevron", act.label, act.menu != nil)
	}
	both := "Both versions are installed. In Slick it's “No Track”."
	if _, line := libFace(lp, nt); line != both {
		t.Errorf("both installed: %q, want %q", line, both)
	}
	installed, _ := lp.groupStates(nt)
	sets := libSettingsMenu(lp, installed)
	if len(sets) != 3 || sets[0].Label != "Settings for" || sets[1].Label != "Taut" || sets[2].Label != "Slick" {
		t.Fatalf("both installed, the chevron: %+v, want Settings for Taut's and Slick's", sets)
	}
	sets[1].Do()
	if pm.section != pluginsTabID || pm.taut.screen != "plugin" || pm.taut.plugin != "NoTrack" {
		t.Errorf("Settings for > Taut: section %q screen %q plugin %q", pm.section, pm.taut.screen, pm.taut.plugin)
	}
	r.frame(dialog)
	sets[2].Do()
	r.frame(dialog)
	if pm.section != pluginsTabID || pm.plugins.s.open != "NoTrack" {
		t.Errorf("Settings for > Slick: section %q open %q", pm.section, pm.plugins.s.open)
	}
	pm.plugins.s.open = ""
	pm.selectTab(libraryTabID)
	r.frame(dialog)

	// Bring Back Slackbot / Custom Slackbot: the bench checked what each does
	// (a custom response drawn with the plugin's avatar). Taut's does nothing
	// in Clack, so Install installs Slick's, recommended; Taut's stays in the
	// menu with how it works in Clack, and the row is filed and sorted as
	// the version it shows, Custom Slackbot
	bb := rowOf(pluginlib.Taut, "BringBackSlackbot")
	bpick, ok := pluginbench.Load().For([]pluginbench.Entry{{System: pluginlib.Taut, ID: "BringBackSlackbot"}, {System: pluginlib.Slick, ID: "CustomSlackbot"}})
	if !ok || !benchChecked(bpick) || bpick.Winner != pluginlib.Slick {
		t.Errorf("bench.json's Bring Back Slackbot / Custom Slackbot: %+v (data %v), want Slick's version, picked on a behaviour check", bpick, ok)
	}
	if act := lp.action(bb); act.label != "Install" || act.menu == nil {
		t.Errorf("Bring Back Slackbot / Custom Slackbot: %q (menu %v), want Install and its chevron", act.label, act.menu != nil)
	}
	recommend := "Install adds Slick's version (recommended). In Taut it's “Bring Back Slackbot”."
	if face, line := libFace(lp, bb); face.ID != "CustomSlackbot" || line != recommend {
		t.Errorf("the row shows %s/%s with %q, want Slick's Custom Slackbot with %q", face.System, face.ID, line, recommend)
	}
	dead := pluginlib.CompatOf(pluginlib.Taut, "BringBackSlackbot")
	if bm := lp.installMenu(lp.installChoices(bb)); len(bm) != 3 || bm[1].Label != "Slick (recommended)" || bm[2].Label != "Taut" ||
		dead.Status != pluginlib.CompatBroken || bm[2].Subtitle != dead.Line {
		t.Errorf("Install for: %+v, want Slick's recommended, then Taut's saying %q", bm, dead.Line)
	}
	cards, _ := lp.cards(nil)
	filed := false
	for _, c := range cards {
		for i, g := range c.rows {
			if !has(g, pluginlib.Taut, "BringBackSlackbot") {
				continue
			}
			filed = true
			face, _ := libFace(lp, g)
			if c.label != libCategory(face).Label {
				t.Errorf("Custom Slackbot's row is under %q, want its own category %q", c.label, libCategory(face).Label)
			}
			for _, j := range []int{i - 1, i + 1} {
				if j < 0 || j >= len(c.rows) {
					continue
				}
				nb, _ := libFace(lp, c.rows[j])
				if a, b := strings.ToLower(face.Name), strings.ToLower(nb.Name); (j < i) != (b < a) {
					t.Errorf("%q is filed next to %q out of name order", face.Name, nb.Name)
				}
			}
		}
	}
	if !filed {
		t.Error("the cards have no Bring Back Slackbot / Custom Slackbot row")
	}
	seen = drawn()
	if !seen["Custom Slackbot"] || !seen[recommend] || seen["Bring Back Slackbot"] {
		t.Errorf("drawn Custom Slackbot %v, %q %v, Bring Back Slackbot %v: want the row as Slick's version", seen["Custom Slackbot"], recommend, seen[recommend], seen["Bring Back Slackbot"])
	}

	// the plugins page's search suggests that row too, and the suggestion
	// asks which version (it has no chevron): Install…, the pick first
	pm.plugins.setMode("installed")
	pm.plugins.s.query.SetText("slackbot")
	wait("the suggestions", func() bool {
		for _, g := range lp.suggest([]string{"slackbot"}) {
			if has(g, pluginlib.Slick, "CustomSlackbot") {
				return true
			}
		}
		return false
	})
	if act := libSuggestAction(lp, bb); act.label != "Install…" || act.do == nil {
		t.Errorf("the suggestion's button: %q, want Install… opening Install for", act.label)
	}
	{
		var mu sync.Mutex
		got := map[string]bool{}
		prev := theme.DrawHook
		theme.DrawHook = func(s string) { mu.Lock(); got[s] = true; mu.Unlock() }
		r.frame(dialog)
		theme.DrawHook = prev
		if !got["Custom Slackbot"] || !got["Install…"] {
			t.Errorf("the suggestions drew Custom Slackbot %v, Install… %v", got["Custom Slackbot"], got["Install…"])
		}
	}
	pm.plugins.s.query.SetText("")
	pm.plugins.setMode("browse")
	r.frame(dialog)

	// search finds the one row by either version's name or description,
	// and the Taut, Slick and Installed chips list it, under one category
	for _, q := range []string{"No Track", "No Tracking", "notrack", "telemetry", "analytics"} {
		cards, _ := lp.cards(strings.Fields(strings.ToLower(q)))
		n := 0
		for _, c := range cards {
			for _, g := range c.rows {
				if has(g, pluginlib.Taut, "NoTrack") || has(g, pluginlib.Slick, "NoTrack") {
					n++
					if len(g.Entries) != 2 {
						t.Errorf("search %q found %s", q, desc(g))
					}
				}
			}
		}
		if n != 1 {
			t.Errorf("search %q: %d rows of No Tracking / No Track, want 1", q, n)
		}
	}
	for _, c := range []struct{ q, sys, id string }{
		{"Anonymise", pluginlib.Taut, "AnonymizeFilenames"}, {"Anonymize", pluginlib.Slick, "AnonymiseFileNames"},
		{"Not Shit Markdown", pluginlib.Taut, "RealMarkdown"}, {"Real Markdown", pluginlib.Slick, "NotShitMarkdown"},
		{"HCA", pluginlib.Taut, "IdvStatus"}, {"Private Channel Mapper", pluginlib.Taut, "PrivateChannel"},
		{"Bring Back", pluginlib.Slick, "CustomSlackbot"},
	} {
		cards, _ := lp.cards(strings.Fields(strings.ToLower(c.q)))
		found := false
		for _, cd := range cards {
			for _, g := range cd.rows {
				found = found || has(g, c.sys, c.id)
			}
		}
		if !found {
			t.Errorf("search %q doesn't find the row with %s/%s", c.q, c.sys, c.id)
		}
	}
	pm.plugins.s.query.SetText("No Track") // the page's own search, drawn
	seen = drawn()
	if !seen[tautNT.Name] || seen[slickNT.Name] {
		t.Errorf("searching \"No Track\": drawn %q %v, %q %v, want the row's name only", tautNT.Name, seen[tautNT.Name], slickNT.Name, seen[slickNT.Name])
	}
	pm.plugins.s.query.SetText("")
	for _, f := range []string{pluginlib.Taut, pluginlib.Slick, "installed", "all"} {
		lp.s.filter = f
		cards, _ := lp.cards(nil)
		n := 0
		for _, c := range cards {
			for _, g := range c.rows {
				if has(g, pluginlib.Taut, "NoTrack") || has(g, pluginlib.Slick, "NoTrack") {
					n++
				}
			}
		}
		if n != 1 {
			t.Errorf("the %s chip lists No Tracking / No Track %d times, want once", f, n)
		}
	}
	lp.s.filter = "all"

	// a plugin in one system keeps its row: one tag, its own name and
	// description, no line about versions, Install without a menu
	seen = drawn()
	for _, k := range []string{"taut/ThreadJump", "slick/MessageLogger", "slick/bChannel", "taut/NativeAtChannel"} {
		sys, id, _ := strings.Cut(k, "/")
		g := rowOf(sys, id)
		face, line := libFace(lp, g)
		if len(g.Entries) != 1 || face.System != sys || face.ID != id || line != "" {
			t.Errorf("%s: row %s shows %s/%s with %q, want its own row as it was", k, desc(g), face.System, face.ID, line)
		}
		if act := lp.action(g); act.label != "Install" || act.menu != nil {
			t.Errorf("%s: %q (menu %v), want Install alone", k, act.label, act.menu != nil)
		}
		if !seen[face.Name] || face.Description != "" && !seen[plainMrkdwn(face.Description)] {
			t.Errorf("%s: its name %q or description isn't drawn", k, face.Name)
		}
	}
}

// TestBenchPairsAreLibraryRows: the Library's pairs have one source,
// pluginlib's (pairs.go, docs/PLUGINS.md) plus one name in both systems;
// bench.json follows them and never pairs anything itself. Every plugin
// the bench has results for is one row of the published Library.
func TestBenchPairsAreLibraryRows(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "plugin-library-index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ix pluginlib.Index
	if err := json.Unmarshal(b, &ix); err != nil {
		t.Fatal(err)
	}
	groups := libraryGroups(libraryEntries(&ix))
	row := func(sys, id string) int {
		for i, g := range groups {
			for _, e := range g.Entries {
				if e.System == sys && e.ID == id {
					return i
				}
			}
		}
		return -1
	}
	d := pluginbench.Load()
	if len(d.Plugins) < 31 {
		t.Fatalf("bench.json has %d plugins", len(d.Plugins))
	}
	for _, p := range d.Plugins {
		tv, sv := p.Variant(pluginlib.Taut), p.Variant(pluginlib.Slick)
		if tv == nil || sv == nil {
			continue
		}
		if i, j := row(pluginlib.Taut, tv.ID), row(pluginlib.Slick, sv.ID); i < 0 || i != j {
			t.Errorf("bench.json's %s (taut %s, slick %s) isn't one Library row (rows %d, %d): list it in pluginlib's pairs or drop it", p.Name, tv.ID, sv.ID, i, j)
		}
	}
}
