package ui

import (
	"context"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/pluginbench"
	"github.com/some-du6e/clack/internal/pluginlib"
)

// TestLibraryInstallPick: Install never asks which version of a plugin
// both running systems have (Karim, 2026-11-23: "is this not the same
// shit? just download the best thing"); it installs installPick's, and
// the row says which and why in one line:
//
//   - the one that works in Clack, by compat.go: Bring Back Slackbot's
//     Taut version doesn't work and Slick's Custom Slackbot does, so
//     Slick's, whatever the bench's costs say (library-dupes' review: a
//     version that does nothing won on frame cost);
//   - of two that work fully, the plugin bench's pick on its costs: IDV
//     Status (Taut) and HCA Status (Slick), where the bench measured
//     Taut's smaller bundle;
//   - with no bench data, Slick's, the recommended system.
//
// The chevron's "Install for" menu still offers each version, the pick
// first, "(recommended)", with why; a search suggestion's Install is the
// pick too.
func TestLibraryInstallPick(t *testing.T) {
	a, _ := ttApp(t, t.TempDir())
	ix := &pluginlib.Index{Schema: 1, Plugins: []pluginlib.Entry{
		{System: pluginlib.Taut, ID: "IdvStatus", Name: "IDV Status", Category: "hackclub", File: pluginlib.File{SHA256: "a1"}},
		{System: pluginlib.Slick, ID: "HcaStatus", Name: "HCA Status", Category: "hackclub", File: pluginlib.File{SHA256: "a2"}},
		{System: pluginlib.Taut, ID: "BringBackSlackbot", Name: "Bring Back Slackbot", Category: "fun", File: pluginlib.File{SHA256: "b1"}},
		{System: pluginlib.Slick, ID: "CustomSlackbot", Name: "Custom Slackbot", Category: "fun", File: pluginlib.File{SHA256: "b2"}},
		{System: pluginlib.Taut, ID: "WhoReacted", Name: "Who Reacted", Category: "messages", File: pluginlib.File{SHA256: "c1"}},
		{System: pluginlib.Slick, ID: "WhoReacted", Name: "Who Reacted", Category: "messages", File: pluginlib.File{SHA256: "c2"}},
	}}
	v := newLibraryPage(a, nil)
	defer v.close()
	v.ix = ix
	v.hosts = func(sys string) (libraryHost, bool) {
		return libraryHost{
			installed: func(string) (string, string) { return "", "" },
			install:   func(ctx context.Context, e pluginlib.Entry, code, main []byte, update bool) error { return nil },
		}, true
	}
	// the bench's own data for the first two pairs; none for Who Reacted
	shipped := pluginbench.Load()
	v.bench = &pluginbench.Data{Schema: 1}
	for _, p := range shipped.Plugins {
		if p.Name == "IDV Status" || p.Name == "Bring Back Slackbot" {
			v.bench.Plugins = append(v.bench.Plugins, p)
		}
	}
	if len(v.bench.Plugins) != 2 {
		t.Fatalf("the shipped bench data has %d of IDV Status and Bring Back Slackbot", len(v.bench.Plugins))
	}
	groups := map[string]libGroup{}
	for _, g := range libraryGroups(libraryEntries(ix)) {
		if len(g.Entries) != 2 {
			t.Fatalf("%s isn't one row of both versions: %+v", g.first().ID, g.Entries)
		}
		groups[g.first().Name] = g
	}
	// IDV Status's why is what the bench measured, from bench.json (a bench
	// run rewrites it), past what says both work; this test's bundles
	// aren't the ones it measured
	idv, ok := v.bench.For([]pluginbench.Entry{{System: pluginlib.Taut, ID: "IdvStatus"}, {System: pluginlib.Slick, ID: "HcaStatus"}})
	if !ok || idv.Winner != pluginlib.Taut {
		t.Fatalf("bench.json's IDV Status / HCA Status: %+v (data %v), want Taut's", idv, ok)
	}
	idvWhy := benchCost(idv) + " on the plugin bench (measured on an earlier build)"
	for _, c := range []struct {
		row, system, why string
	}{
		{"IDV Status", pluginlib.Taut, idvWhy},
		{"Bring Back Slackbot", pluginlib.Slick, "it works fully in Clack, Taut's doesn't work"},
		{"Who Reacted", pluginlib.Slick, "the recommended system (no plugin bench data)"},
	} {
		g, ok := groups[c.row]
		if !ok {
			t.Fatalf("no %s row", c.row)
		}
		w, why := v.installPick(v.installChoices(g))
		if w.System != c.system || !strings.Contains(why, c.why) {
			t.Errorf("%s: Install adds %s's, %q; want %s's, %q", c.row, w.System, why, c.system, c.why)
		}
		// the row's line: which and why
		_, line := libFace(v, g)
		if want := "Install adds " + systemName(c.system) + "'s version: "; !strings.HasPrefix(line, want) || !strings.Contains(line, c.why) {
			t.Errorf("%s's row says %q, want %q… %q", c.row, line, want, c.why)
		}
		// Install installs it, never asking; the chevron offers both; a
		// search suggestion's Install is the same, with no chevron
		act := v.action(g)
		if act.label != "Install" || act.do == nil || act.menu == nil {
			t.Fatalf("%s: %q (do %v, menu %v), want Install with its chevron", c.row, act.label, act.do != nil, act.menu != nil)
		}
		if s := libSuggestAction(v, g); s.label != "Install" || s.menu != nil {
			t.Errorf("%s's search suggestion: %q (menu %v), want Install", c.row, s.label, s.menu != nil)
		}
		// it starts installing the pick, and only it (installEntry marks
		// what it installs busy before its download)
		act.do()
		for _, e := range g.Entries {
			if busy := v.busy[libKey(e)]; busy != (e.System == w.System) {
				t.Errorf("%s: Install left %s's version busy %v", c.row, e.System, busy)
			}
		}
		items := v.installMenu(v.installChoices(g))
		if len(items) != 3 || items[0].Label != "Install for" || !items[0].Disabled {
			t.Fatalf("%s's menu %+v", c.row, items)
		}
		if items[1].Label != systemName(c.system)+" (recommended)" || !strings.Contains(strings.ToLower(items[1].Subtitle), strings.ToLower(c.why)) {
			t.Errorf("%s's menu, its first row %+v", c.row, items[1])
		}
		if strings.Contains(items[2].Label, "recommended") {
			t.Errorf("%s's menu recommends both: %+v", c.row, items[2])
		}
	}
}

// TestBenchCostShapes: the why line names what the bench's pick won on,
// past the clauses that say both versions work ("works fully in both",
// "same results (…)", "costs within noise", "no behaviour checks", "both
// load"): nine Library rows read "works fully in both on the plugin bench"
// before. No reason in bench.json leaves such a clause as the why.
func TestBenchCostShapes(t *testing.T) {
	for reason, want := range map[string]string{
		"works fully in both; costs within noise, smaller bundle (7.1 KB vs 9.1 KB)":            "smaller bundle (7.1 KB vs 9.1 KB)",
		"no behaviour checks, both load; costs within noise, smaller bundle (2.4 KB vs 6.2 KB)": "smaller bundle (2.4 KB vs 6.2 KB)",
		"works fully, 3.0x faster to load, 3.4x less JS memory":                                 "3.0x faster to load",
		"same results (3/3 checks each), 1.4x faster to load":                                   "1.4x faster to load",
		"works fully (Taut passes 0/1 checks)":                                                  "works fully (Taut passes 0/1 checks)",
	} {
		if got := benchCost(pluginbench.Pick{Reason: reason}); got != want {
			t.Errorf("benchCost(%q) = %q, want %q", reason, got, want)
		}
	}
	for _, p := range pluginbench.Load().Plugins {
		if p.Reason == "" {
			continue
		}
		got := benchCost(pluginbench.Pick{Reason: p.Reason})
		for _, filler := range []string{"works fully in both", "costs within noise", "no behaviour checks", "both load", "works fully"} {
			if got == filler {
				t.Errorf("%s: its why is %q (from %q)", p.Name, got, p.Reason)
			}
		}
		if strings.HasPrefix(got, "same results (") {
			t.Errorf("%s: its why is %q", p.Name, got)
		}
	}
}
