package ui

import (
	"context"
	"strings"
	"testing"

	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/pluginlib"
	"github.com/some-du6e/clack/internal/slick"
	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/icons"
)

// libraryIndexCategories are the (system, category) pairs the published
// plugin library's index.json uses (checked 2026-11-08); new ones must be
// mapped in pluginCategoryKeys.
var libraryIndexCategories = map[string][]string{
	pluginlib.Taut:  {"app", "fun", "messageBox", "messages", "people", "privacy"},
	pluginlib.Slick: {"app", "appearance", "channels", "composer", "hackclub", "messages", "other", "people", "privacy", "sounds"},
}

// One taxonomy for both systems: every key either system or the library
// uses is mapped, no two shown groups mean the same thing, and the two
// names for the message box are one group.
func TestPluginCategories(t *testing.T) {
	keys, labels := map[string]bool{}, map[string]bool{}
	for _, c := range pluginCategories {
		norm := libNameKey(c.Label)
		if keys[c.Key] || labels[norm] {
			t.Errorf("category %q / %q is listed twice", c.Key, c.Label)
		}
		keys[c.Key], labels[norm] = true, true
		if c.Icon == "" {
			t.Errorf("category %q has no icon", c.Key)
		}
	}
	if last := pluginCategories[len(pluginCategories)-1]; last.Key != "other" {
		t.Errorf("Other isn't last: %q", last.Key)
	}
	known := map[string][]string{}
	for _, c := range taut.Categories {
		known[pluginlib.Taut] = append(known[pluginlib.Taut], c.Key)
	}
	for _, c := range slick.Categories {
		known[pluginlib.Slick] = append(known[pluginlib.Slick], c.Key)
	}
	for sys, ks := range libraryIndexCategories {
		known[sys] = append(known[sys], ks...)
	}
	for sys, ks := range known {
		for _, k := range ks {
			shared, ok := pluginCategoryKeys[sys][k]
			if !ok {
				t.Errorf("%s category %q isn't mapped", sys, k)
				continue
			}
			if !keys[shared] {
				t.Errorf("%s category %q maps to unknown group %q", sys, k, shared)
			}
		}
	}
	// both systems' labels for one meaning land in one group
	for _, same := range [][2][2]string{
		{{pluginlib.Taut, "messageBox"}, {pluginlib.Slick, "composer"}},
		{{pluginlib.Taut, "app"}, {pluginlib.Slick, "app"}},
		{{pluginlib.Taut, "other"}, {pluginlib.Slick, "other"}},
	} {
		a, b := sharedCategory(same[0][0], same[0][1]), sharedCategory(same[1][0], same[1][1])
		if a != b {
			t.Errorf("%v and %v are different groups: %q, %q", same[0], same[1], a.Label, b.Label)
		}
	}
	for _, c := range []struct{ sys, key, label string }{
		{pluginlib.Taut, "messageBox", "Message box"}, {pluginlib.Slick, "hackclub", "Hack Club"},
		{pluginlib.Slick, "channels", "Channels & sidebar"}, {pluginlib.Slick, "sounds", "Huddles & sounds"},
		{pluginlib.Slick, "", "Other"}, {pluginlib.Taut, "nonsense", "Other"},
	} {
		if got := sharedCategory(c.sys, c.key).Label; got != c.label {
			t.Errorf("%s %q: %q, want %q", c.sys, c.key, got, c.label)
		}
	}
}

func testSettings() (*taut.Settings, *slick.Settings) {
	ts := &taut.Settings{Categories: []taut.SettingsCategory{
		{Key: "messageBox", Label: "Message Box", Plugins: []taut.PluginInfo{
			{ID: "SilentTyping", Name: "Silent Typing", Description: "Hide *typing*", Category: "messageBox", IsUser: true, Enabled: true, Running: true},
		}},
		{Key: "people", Label: "People", Plugins: []taut.PluginInfo{
			{ID: "Broken", Name: "Broken Thing", Category: "people", IsUser: true, Enabled: true, Error: "boom",
				Authors: []taut.Author{{Name: "Jeremy"}}},
		}},
	}}
	ss := &slick.Settings{Enabled: true, Plugins: []slick.PluginInfo{
		{ID: "restrictedChannelWarning", Name: "Restricted Channel Warning", Category: "composer", CategoryLabel: "Composer", User: true, WorkspaceAllowed: true},
		{ID: "quickJoin", Name: "Quick Join", Category: "channels", CategoryLabel: "Channels", User: true, Enabled: true, WorkspaceAllowed: true, StartError: "nope"},
		{ID: "loose", Name: "Loose", CategoryLabel: "User plugins", User: true, WorkspaceAllowed: true},
	}}
	return ts, ss
}

// Preferences > Plugins is Taut's list over both systems' plugins: under
// Taut's groups (a Slick plugin in a category Taut doesn't have is one of
// the User plugins, as Taut lists any), by name, with Taut's pills.
func TestUnifiedPlugins(t *testing.T) {
	ts, ss := testSettings()
	ps := unifiedPlugins(ts, ss, nil)
	var got []string
	for _, u := range ps {
		got = append(got, u.System+":"+u.ID+"@"+u.Category.Label)
	}
	want := "slick:restrictedChannelWarning@Message Box taut:SilentTyping@Message Box taut:Broken@People slick:loose@User plugins slick:quickJoin@User plugins"
	if strings.Join(got, " ") != want {
		t.Errorf("merged\n got %v\nwant %s", got, want)
	}
	var secs []string
	for _, s := range unifiedSections(ps) {
		secs = append(secs, s.Category.Label+"="+string(rune('0'+len(s.Plugins)))+"/"+string(rune('0'+s.On)))
	}
	if strings.Join(secs, " ") != "Message Box=2/1 People=1/1 User plugins=2/1" {
		t.Errorf("sections %v", secs)
	}
	// Taut's pills: All, Enabled, Disabled, Hack Club only (in the Hack
	// Club Slack), User plugins
	counts := map[string]int{}
	var order []string
	for _, f := range unifiedFilters(ps, false) {
		counts[f.ID] = f.Count
		order = append(order, f.Label)
	}
	if counts["all"] != 5 || counts["enabled"] != 3 || counts["disabled"] != 2 || counts["user"] != 5 || len(counts) != 4 {
		t.Errorf("filters %v", counts)
	}
	if got := strings.Join(order, ", "); got != "All, Enabled, Disabled, User plugins" {
		t.Errorf("pills %q", got)
	}
	if fs := unifiedFilters(ps, true); len(fs) != 5 || fs[3].ID != "hack-club" || fs[3].Label != "Hack Club only" {
		t.Errorf("Hack Club pills %v", fs)
	}
	ids := func(filter string, words ...string) string {
		var out []string
		for _, u := range ps {
			if unifiedMatches(u, filter, words) {
				out = append(out, u.ID)
			}
		}
		return strings.Join(out, " ")
	}
	for _, c := range []struct{ filter, words, want string }{
		{"disabled", "", "restrictedChannelWarning loose"},
		{"all", "taut", "SilentTyping Broken"},
		{"all", "slick message", "restrictedChannelWarning"},
		{"all", "jeremy", "Broken"},
		{"enabled", "typing", "SilentTyping"},
		{"hack-club", "", ""},
	} {
		if got := ids(c.filter, strings.Fields(c.words)...); got != c.want {
			t.Errorf("%s %q: %q, want %q", c.filter, c.words, got, c.want)
		}
	}
	if ps[1].Description != "Hide typing" {
		t.Errorf("description isn't plain text: %q", ps[1].Description)
	}
	for _, u := range ps {
		if u.ID == "Broken" && (u.Problem != "boom" || !u.ProblemErr) || u.ID == "quickJoin" && !strings.Contains(u.Problem, "nope") {
			t.Errorf("%s problem %q", u.ID, u.Problem)
		}
	}
	// a system that isn't running has no rows
	if n := len(unifiedPlugins(nil, ss, nil)); n != 3 {
		t.Errorf("Slick only: %d rows", n)
	}
	if n := len(unifiedPlugins(ts, nil, nil)); n != 2 {
		t.Errorf("Taut only: %d rows", n)
	}
}

// A plugin both systems have under one name is one Browse row with both
// tags; Install asks which system while both run and neither has it.
func TestLibraryGroups(t *testing.T) {
	a, _ := ttApp(t, t.TempDir())
	ix := &pluginlib.Index{Schema: 1, Plugins: []pluginlib.Entry{
		{System: pluginlib.Taut, ID: "ClearURLs", Name: "Clear URLs", Category: "messageBox", File: pluginlib.File{SHA256: "x"}},
		{System: pluginlib.Slick, ID: "clearUrls", Name: "ClearURLs", Category: "privacy", File: pluginlib.File{SHA256: "x"}},
		{System: pluginlib.Slick, ID: "oneko", Name: "Oneko", Category: "appearance", File: pluginlib.File{SHA256: "x"}},
		{System: pluginlib.Taut, ID: "Oneko", Name: "oneko", Category: "fun", File: pluginlib.File{SHA256: "x"}},
		{System: pluginlib.Taut, ID: "Solo", Name: "Solo", Category: "people", File: pluginlib.File{SHA256: "x"}},
	}}
	installed := map[string]bool{}
	running := map[string]bool{pluginlib.Taut: true, pluginlib.Slick: true}
	v := newLibraryPage(a, nil)
	defer v.close()
	v.ix = ix
	v.hosts = func(sys string) (libraryHost, bool) {
		if !running[sys] {
			return libraryHost{}, false
		}
		return libraryHost{
			installed: func(id string) (string, string) {
				if installed[sys+"/"+id] {
					return "x", ""
				}
				return "", ""
			},
			install: func(ctx context.Context, e pluginlib.Entry, code, main []byte, update bool) error { return nil },
			open:    func(string) {},
		}, true
	}
	gs := libraryGroups(libraryEntries(ix))
	var got []string
	for _, g := range gs {
		var sys []string
		for _, e := range g.Entries {
			sys = append(sys, e.System)
		}
		got = append(got, g.first().Name+"="+strings.Join(sys, "+")+"@"+libCategory(g.first()).Label)
	}
	// rows sit where their Taut entry sorts (Message box before People before Fun)
	if want := "Clear URLs=taut+slick@Message box Solo=taut@People oneko=taut+slick@Fun"; strings.Join(got, " ") != want {
		t.Fatalf("groups\n got %v\nwant %s", got, want)
	}
	counts := map[string]int{}
	for _, f := range v.chips() {
		counts[f.ID] = f.Count
	}
	if counts["all"] != 3 || counts[pluginlib.Taut] != 3 || counts[pluginlib.Slick] != 2 || counts["installed"] != 0 {
		t.Errorf("chips %v", counts)
	}
	clear := gs[0]
	if act := v.action(clear); act.label != "Install…" {
		t.Errorf("both running, neither installed: %q", act.label)
	}
	if n := len(v.installChoices(clear)); n != 2 {
		t.Errorf("%d install choices", n)
	}
	if act := v.action(gs[1]); act.label != "Install" {
		t.Errorf("one system: %q", act.label)
	}
	running[pluginlib.Slick] = false
	if act := v.action(clear); act.label != "Install" || len(v.installChoices(clear)) != 1 || v.installChoices(clear)[0].System != pluginlib.Taut {
		t.Errorf("Slick not running: %q", act.label)
	}
	running[pluginlib.Slick] = true
	installed["slick/clearUrls"] = true
	if act := v.action(clear); act.label != "Settings" {
		t.Errorf("installed in Slick: %q", act.label)
	}
	v.s.filter = "installed"
	if !v.matches(clear, nil) || v.matches(gs[1], nil) {
		t.Error("Installed chip")
	}
	v.s.filter = pluginlib.Slick
	if v.matches(gs[1], nil) || !v.matches(gs[2], nil) {
		t.Error("Slick chip")
	}
	v.s.filter = "all"
	// a row matches when one of its entries does, by name ("clearurls" is
	// the Slick entry's) or description, never by a system's or a
	// category's name alone (libraryRank)
	if !v.matches(clear, []string{"clearurls"}) || v.matches(clear, []string{"slick", "privacy"}) || v.matches(gs[1], []string{"slick"}) {
		t.Error("search across a row's entries")
	}
}

// After Slack's tabs: Clack's own tab (Preferences > Clack, where a Taut
// tab lands), a divider, Plugins and Themes (while Slick runs), then the
// plugins' own tabs. The old ids land on their new homes: the plugin
// library, or Plugins > Advanced for Plugin settings.
func TestPluginNav(t *testing.T) {
	a, _ := ttApp(t, t.TempDir())
	a.data.startTaut(a)
	t.Cleanup(func() { a.data.stopTaut() })
	pm := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}}
	defer pm.close()
	nav := func() string {
		var ids []string
		for i, it := range pm.extraNav() {
			if it.divider != (i == 1) { // Plugins starts the plugin group
				t.Errorf("tab %s divider %v", it.id, it.divider)
			}
			if !icons.Has(it.icon) {
				t.Errorf("tab %s: icon %q isn't registered", it.id, it.icon)
			}
			ids = append(ids, it.id)
		}
		return strings.Join(ids, " ")
	}
	want := "clack plugins slick_themes"
	if a.Slick() == nil {
		want = "clack plugins"
	}
	if got := nav(); got != want {
		t.Errorf("tabs %q, want %q", got, want)
	}
	a.tautTabs = []taut.PreferencesTab{{ID: "myplugin", Label: "My plugin"}}
	if got := nav(); got != want+" myplugin" {
		t.Errorf("with a plugin tab: %q", got)
	}
	a.tautTabs = nil
	for _, c := range []struct{ target, section, mode, screen string }{
		{"library", pluginsTabID, "browse", "main"},
		{"slick_install", pluginsTabID, "browse", "main"},
		{"taut", pluginsTabID, "installed", "main"},
		{"slick_plugins", pluginsTabID, "installed", "main"},
		{pluginSettingsTabID, pluginsTabID, "installed", "advanced"},
		{"slick", pluginsTabID, "installed", "advanced"},
		{"slick_themes", themesTabID, "", ""},
		{"themes", "appearance", "", ""},
		{"notifications", "notifications", "", ""},
	} {
		if pm.plugins != nil {
			pm.plugins.taut.go_("main", "")
		}
		pm.selectTab(c.target)
		if pm.section != c.section || c.mode != "" && pm.plugins.mode != c.mode || c.screen != "" && pm.plugins.taut.screen != c.screen {
			t.Errorf("%s: section %q mode %q screen %q, want %q %q %q", c.target, pm.section, pm.plugins.mode, pm.plugins.taut.screen, c.section, c.mode, c.screen)
		}
	}
	if !pm.ownSection(pluginsTabID) || pm.ownSection(pluginSettingsTabID) || pm.ownSection("library") {
		t.Error("ownSection")
	}
}

// The install box tells Slick's files from Taut's.
func TestPluginKind(t *testing.T) {
	if pluginKind([]byte(libSlickPlugin)) != pluginlib.Slick || pluginKind([]byte(libSlickMain)) != pluginlib.Slick {
		t.Error("a Slick file isn't Slick's")
	}
	if pluginKind(libTestPlugin("x")) != pluginlib.Taut {
		t.Error("a Taut file isn't Taut's")
	}
}

// Taut's groups, as its pluginGroup names them, for both systems.
func TestTautGroups(t *testing.T) {
	for _, c := range []struct {
		sys, key string
		user     bool
		want     string
	}{
		{pluginlib.Taut, "messageBox", true, "Message Box"}, {pluginlib.Taut, "fun", false, "Fun"},
		{pluginlib.Taut, "", true, "User plugins"}, {pluginlib.Taut, "", false, "Other"},
		{pluginlib.Slick, "composer", true, "Message Box"}, {pluginlib.Slick, "channels", true, "User plugins"},
		{pluginlib.Slick, "hackclub", true, "User plugins"}, {pluginlib.Slick, "privacy", true, "Privacy"},
	} {
		if got := tautGroup(c.sys, c.key, c.user).Label; got != c.want {
			t.Errorf("%s %q user=%v: %q, want %q", c.sys, c.key, c.user, got, c.want)
		}
	}
	var labels []string
	for _, g := range tautGroups {
		labels = append(labels, g.Label)
	}
	if got := strings.Join(labels, ", "); got != "Messages, Message Box, People, Privacy, App, Fun, User plugins, Other" {
		t.Errorf("groups %q", got)
	}
}

// A search that finds nothing installed suggests the library's plugins
// that match it, leaving out what's installed already.
func TestLibrarySuggestions(t *testing.T) {
	a, _ := ttApp(t, t.TempDir())
	ix := &pluginlib.Index{Schema: 1, Plugins: []pluginlib.Entry{
		{System: pluginlib.Taut, ID: "OwlMode", Name: "Owl Mode", Description: "Lets you reply and react anonymously as the author of your owl confessions", Category: "messages", File: pluginlib.File{SHA256: "x"}},
		{System: pluginlib.Taut, ID: "SilentTyping", Name: "Silent Typing", Description: "Hide your typing indicator", Category: "messageBox", File: pluginlib.File{SHA256: "x"}},
		{System: pluginlib.Slick, ID: "silentTyping", Name: "SilentTyping", Description: "Hide typing", Category: "composer", File: pluginlib.File{SHA256: "x"}},
		{System: pluginlib.Slick, ID: "oneko", Name: "Oneko", Description: "A cat that follows the cursor", Category: "appearance", File: pluginlib.File{SHA256: "x"}},
	}}
	installed := map[string]bool{pluginlib.Slick + "/silentTyping": true}
	v := newLibraryPage(a, nil)
	defer v.close()
	if v.suggest([]string{"owl"}) != nil {
		t.Error("suggestions before the library's index is there")
	}
	v.ix = ix
	v.hosts = func(sys string) (libraryHost, bool) {
		return libraryHost{installed: func(id string) (string, string) {
			if installed[sys+"/"+id] {
				return "x", ""
			}
			return "", ""
		}}, true
	}
	names := func(words ...string) string {
		var out []string
		for _, g := range v.suggest(words) {
			out = append(out, g.first().Name)
		}
		return strings.Join(out, ", ")
	}
	if got := names("owl"); got != "Owl Mode" {
		t.Errorf("owl: %q", got)
	}
	if got := names("anonymously", "confessions"); got != "Owl Mode" {
		t.Errorf("by description: %q", got)
	}
	if got := names("typing"); got != "" { // installed in Slick: the list has it
		t.Errorf("typing suggests an installed plugin: %q", got)
	}
	if got := names("zzz"); got != "" {
		t.Errorf("zzz: %q", got)
	}
}
