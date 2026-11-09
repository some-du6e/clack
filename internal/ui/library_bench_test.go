package ui

import (
	"context"
	"strings"
	"testing"

	"github.com/some-du6e/yoloslackgoclient/internal/pluginbench"
	"github.com/some-du6e/yoloslackgoclient/internal/pluginlib"
)

// With plugin bench data for a plugin both systems offer, Install installs
// the bench's pick directly and the chevron's "Install for" menu lists the
// pick first, marked Recommended, with the bench's reason; without data
// (or for another version of the ids) Install asks.
func TestLibraryBenchPick(t *testing.T) {
	a, _ := ttApp(t, t.TempDir())
	ix := &pluginlib.Index{Schema: 1, Plugins: []pluginlib.Entry{
		{System: pluginlib.Taut, ID: "WhoReacted", Name: "Who Reacted", Category: "messages", File: pluginlib.File{SHA256: "aa"}},
		{System: pluginlib.Slick, ID: "WhoReacted", Name: "Who Reacted", Category: "messages", File: pluginlib.File{SHA256: "bb"}},
		{System: pluginlib.Taut, ID: "UserPronouns", Name: "User Pronouns", Category: "messages", File: pluginlib.File{SHA256: "cc"}},
		{System: pluginlib.Slick, ID: "UserPronouns", Name: "User Pronouns", Category: "people", File: pluginlib.File{SHA256: "dd"}},
	}}
	var installs []string
	v := newLibraryPage(a, nil)
	defer v.close()
	v.ix = ix
	v.hosts = func(sys string) (libraryHost, bool) {
		return libraryHost{
			installed: func(string) (string, string) { return "", "" },
			install: func(ctx context.Context, e pluginlib.Entry, code, main []byte, update bool) error {
				installs = append(installs, e.System+"/"+e.ID)
				return nil
			},
		}, true
	}
	v.bench = &pluginbench.Data{Schema: 1, Plugins: []pluginbench.Plugin{{
		Name:   "Who Reacted",
		Winner: pluginlib.Slick,
		Reason: "works fully, 3.0x faster to load",
		Variants: []pluginbench.Variant{
			{System: pluginlib.Taut, ID: "WhoReacted", SHA256: "aa"},
			{System: pluginlib.Slick, ID: "WhoReacted", SHA256: "bb"},
		},
	}}}
	var who, pronouns libGroup
	for _, g := range libraryGroups(libraryEntries(ix)) {
		switch g.first().ID {
		case "WhoReacted":
			who = g
		case "UserPronouns":
			pronouns = g
		}
	}
	act := v.action(who)
	if act.label != "Install" || act.menu == nil {
		t.Fatalf("with bench data: %q, menu %v", act.label, act.menu != nil)
	}
	if w, _, ok := v.benchPick(v.installChoices(who)); !ok || w.System != pluginlib.Slick {
		t.Fatalf("pick %v %v", w.System, ok)
	}
	items := v.installMenu(v.installChoices(who))
	if len(items) != 3 || items[0].Label != "Install for" || !items[0].Disabled {
		t.Fatalf("menu %+v", items)
	}
	if items[1].Label != "Slick (recommended)" || items[1].Subtitle != "Works fully, 3.0x faster to load" {
		t.Errorf("first row %+v", items[1])
	}
	if items[2].Label != "Taut" || items[2].Subtitle != systemBlurb(pluginlib.Taut) {
		t.Errorf("second row %+v", items[2])
	}
	if act := v.action(pronouns); act.label != "Install…" || act.menu != nil {
		t.Errorf("no bench data: %q", act.label)
	}
	// another version of the same ids: still the pick, flagged
	ix.Plugins[1].SHA256 = "b2"
	who = libraryGroups(libraryEntries(ix))[0]
	if who.first().ID != "WhoReacted" {
		who = libraryGroups(libraryEntries(ix))[1]
	}
	if _, p, ok := v.benchPick(v.installChoices(who)); !ok || p.Exact || !strings.Contains(benchReason(p), "earlier build") {
		t.Errorf("other version: %+v %v", p, ok)
	}
}
