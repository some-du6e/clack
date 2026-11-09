package pluginbench

import "testing"

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
