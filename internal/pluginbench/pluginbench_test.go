package pluginbench

import (
	"strings"
	"testing"
)

// Decide's rules on synthetic measurements.
func TestDecide(t *testing.T) {
	ok := func(sys string, init float64) Variant {
		return Variant{System: sys, Loaded: true, InitMS: init, IdleSeconds: 3,
			Checks: []Check{{Name: "a", Pass: true}, {Name: "b", Pass: true}}}
	}
	cases := []struct {
		name   string
		t, s   Variant
		winner string
		reason string
	}{
		{"faster", ok("taut", 30), ok("slick", 90), "taut", "works fully, 3.0x faster to load"},
		{"broken", ok("taut", 30), func() Variant { v := ok("slick", 9); v.Loaded = false; return v }(), "taut", "the Slick version doesn't load"},
		{"rerenders", func() Variant { v := ok("taut", 5); v.IdleDeliveries = 40; return v }(), ok("slick", 90), "slick", "the Taut version keeps re-rendering while idle"},
		{"checks", func() Variant { v := ok("taut", 5); v.Checks[1].Pass = false; return v }(), ok("slick", 90), "slick", "works fully (Taut passes 1/2 checks)"},
		{"noise", func() Variant { v := ok("taut", 30); v.Size = 20480; return v }(), func() Variant { v := ok("slick", 31); v.Size = 10240; return v }(), "slick", "works fully in both; costs within noise, smaller bundle (10.0 KB vs 20.0 KB)"},
	}
	for _, c := range cases {
		p := Plugin{Name: c.name, Variants: []Variant{c.t, c.s}}
		Decide(&p)
		if p.Winner != c.winner || p.Reason != c.reason {
			t.Errorf("%s: %s (%s), want %s (%s)", c.name, p.Winner, p.Reason, c.winner, c.reason)
		}
	}
	// a winner that lost a check is a regression
	old := &Data{Plugins: []Plugin{{Name: "x", Winner: "taut", Variants: []Variant{ok("taut", 5), ok("slick", 9)}}}}
	cur := &Data{Plugins: []Plugin{{Name: "x", Variants: []Variant{func() Variant { v := ok("taut", 5); v.Checks[0].Pass = false; return v }(), ok("slick", 9)}}}}
	Decide(&cur.Plugins[0])
	if regs := Regressions(old, cur); len(regs) != 1 {
		t.Errorf("regressions %v", regs)
	}
}

// Without a behaviour check on either variant the bench can only weigh
// costs, which can't tell a version that does nothing from one that works
// (Bring Back Slackbot did nothing in Clack and won on frame cost): the
// reason says so, never "works fully", and the pick isn't Checked, so the
// Library doesn't recommend it. One check on either side makes it Checked.
func TestDecideWithoutChecks(t *testing.T) {
	bare := func(sys, id string, init float64, size int64) Variant {
		return Variant{System: sys, ID: id, Loaded: true, InitMS: init, IdleSeconds: 3, Size: size}
	}
	for _, c := range []struct {
		name   string
		t, s   Variant
		winner string
		reason string
	}{
		{"faster", bare("taut", "A", 30, 1000), bare("slick", "A", 90, 1000), "taut", "no behaviour checks, both load, 3.0x faster to load"},
		{"noise", bare("taut", "B", 30, 20480), bare("slick", "B", 31, 10240), "slick", "no behaviour checks, both load; costs within noise, smaller bundle (10.0 KB vs 20.0 KB)"},
	} {
		p := Plugin{Name: c.name, Variants: []Variant{c.t, c.s}}
		Decide(&p)
		if p.Winner != c.winner || p.Reason != c.reason {
			t.Errorf("%s: %s (%s), want %s (%s)", c.name, p.Winner, p.Reason, c.winner, c.reason)
		}
		d := &Data{Plugins: []Plugin{p}}
		pk, ok := d.For([]Entry{{System: "taut", ID: c.t.ID}, {System: "slick", ID: c.s.ID}})
		if !ok || pickChecked(pk) {
			t.Errorf("%s: For = %+v %v, want a pick that isn't Checked", c.name, pk, ok)
		}
		d.Plugins[0].Variants[1].Checks = []Check{{Name: "does its thing", Pass: true}}
		if pk, _ := d.For([]Entry{{System: "taut", ID: c.t.ID}, {System: "slick", ID: c.s.ID}}); !pickChecked(pk) {
			t.Errorf("%s: a check on one variant, and the pick isn't Checked", c.name)
		}
	}
}

// The baked-in data parses, names a winner and a reason for every plugin,
// and For finds a plugin by its variants' ids.
func TestEmbedded(t *testing.T) {
	d := Load()
	if len(d.Plugins) == 0 {
		t.Skip("no bench data baked in yet")
	}
	for _, p := range d.Plugins {
		if p.Winner == "" || p.Reason == "" || p.Variant(p.Winner) == nil {
			t.Errorf("%s: winner %q reason %q", p.Name, p.Winner, p.Reason)
		}
		var es []Entry
		for _, v := range p.Variants {
			es = append(es, Entry{System: v.System, ID: v.ID, SHA256: v.SHA256})
		}
		if pk, ok := d.For(es); !ok || pk.Winner != p.Winner || !pk.Exact {
			t.Errorf("%s: For = %+v %v", p.Name, pk, ok)
		}
	}
}

// A run limited to some plugins (PLUGIN_BENCH_ONLY, report -keep) keeps
// the data before it for the rest: each with the run that measured it,
// its sources beside the run's, the run's own results where both have
// one, and the plugins in name order.
func TestKeep(t *testing.T) {
	old := &Data{Run: "https://example.com/runs/1", Sources: []string{"slick 34f2c25", "taut 6fe5123"}, Plugins: []Plugin{
		{Name: "Who Reacted", Winner: "taut"},
		{Name: "Clear URLs", Winner: "slick"},
		{Name: "Oneko", Winner: "taut", Run: "https://example.com/runs/0"},
	}}
	cur := &Data{Run: "https://example.com/runs/2", Sources: []string{"slick b582605", "taut 6fe5123"}, Plugins: []Plugin{
		{Name: "No Tracking", Winner: "taut"},
		{Name: "Clear URLs", Winner: "taut"},
	}}
	cur.Keep(old)
	var got []string
	for _, p := range cur.Plugins {
		got = append(got, p.Name+"="+p.Winner+"@"+p.Run)
	}
	want := []string{"Clear URLs=taut@", "No Tracking=taut@", "Oneko=taut@https://example.com/runs/0", "Who Reacted=taut@https://example.com/runs/1"}
	if len(got) != len(want) {
		t.Fatalf("plugins %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("plugin %d: %s, want %s", i, got[i], want[i])
		}
	}
	if s := cur.Sources; len(s) != 3 || s[0] != "slick 34f2c25" || s[1] != "slick b582605" || s[2] != "taut 6fe5123" {
		t.Errorf("sources %v", s)
	}
	if cur.Run != "https://example.com/runs/2" {
		t.Errorf("the run is %q", cur.Run)
	}
	// the kept plugins are the committed ones: nothing regressed
	if regs := Regressions(old, cur); len(regs) != 0 {
		t.Errorf("regressions %v", regs)
	}

	// a plugin is the same one by its variants' ids, whatever it's called:
	// the run's Custom Slackbot is the committed Bring Back Slackbot (the
	// Library named the row after its Taut entry then), not a second plugin
	ids := func(t, s string) []Variant { return []Variant{{System: "taut", ID: t}, {System: "slick", ID: s}} }
	old = &Data{Run: "https://example.com/runs/1", Plugins: []Plugin{
		{Name: "Bring Back Slackbot", Winner: "taut", Variants: ids("BringBackSlackbot", "CustomSlackbot")},
		{Name: "Who Reacted", Winner: "taut", Variants: ids("WhoReacted", "WhoReacted")},
	}}
	cur = &Data{Run: "https://example.com/runs/3", Plugins: []Plugin{
		{Name: "Custom Slackbot", Winner: "slick", Variants: ids("BringBackSlackbot", "CustomSlackbot")},
	}}
	cur.Keep(old)
	got = nil
	for _, p := range cur.Plugins {
		got = append(got, p.Name+"="+p.Winner+"@"+p.Run)
	}
	if want := "Custom Slackbot=slick@ Who Reacted=taut@https://example.com/runs/1"; strings.Join(got, " ") != want {
		t.Errorf("kept %q, want %q", strings.Join(got, " "), want)
	}
}
