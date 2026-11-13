package ui

import (
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/pluginlib"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Plugins > About is about Clack (Karim: Taut's own blurb there was
// "so awkward"): its name and version, the README's line and its
// repository; then the credits: what makes plugins work (Clack's plugin
// compatibility layer, linking Taut and Slick, no mention or channel
// chips) and everyone who made a plugin, Jeremy and Rowan first. Each
// plugin's own "X by Y" line is on its page, not here (Karim 2026-11-13:
// "half of this stuff should go to their respecting plugins sections").
func TestAboutIsAboutClack(t *testing.T) {
	if !strings.HasPrefix(aboutClack, "A fast Slack client for the desktop") || !strings.Contains(aboutClack, "<https://github.com/some-du6e/clack|") {
		t.Errorf("About's line: %q", aboutClack)
	}
	if !strings.Contains(aboutLayer, "compatibility layer") || !strings.Contains(aboutLayer, "<https://github.com/jeremy46231/taut|Taut>") ||
		!strings.Contains(aboutLayer, "<https://github.com/3kh0/slick|Slick>") {
		t.Errorf("About's credit for plugins: %q", aboutLayer)
	}
	lines := []string{aboutClack, aboutLayer}
	for _, s := range lines {
		for _, bad := range []string{"<@", "<#", "made by", "say hi", "client mod", "no Taut code"} {
			if strings.Contains(s, bad) {
				t.Errorf("%q has %q", s, bad)
			}
		}
	}
	// everyone, once, Jeremy and Rowan first, then by name
	es := []pluginlib.Entry{
		{Authors: []pluginlib.Author{{Name: "Sahil", SlackID: "U08PUHSMW4V"}, {Name: "Rowan", SlackID: "U080A3QP42C"}}},
		{Authors: []pluginlib.Author{{Name: "Jeremy", SlackID: "U06UYA5GMB5"}}, Contributors: []string{"zed"}},
		{Contributors: []string{"Ani", "rowan", "Zed"}},
	}
	more := []creditPerson{{Name: "Izie", SlackID: "U09KKMHLS15"}, {Name: "Sahil"}}
	if got := (creditResolver{}).contributors(es, more); got != "Jeremy, Rowan, Ani, Izie, Sahil, zed" {
		t.Errorf("contributors elsewhere: %q", got)
	}
	if got := (creditResolver{hc: true}).contributors(es, more); got != "<@U06UYA5GMB5|Jeremy>, <@U080A3QP42C|Rowan>, <@U01D9DWGEB0|Ani>, <@U09KKMHLS15|Izie>, <@U08PUHSMW4V|Sahil>, zed" {
		t.Errorf("contributors in the Hack Club Slack: %q", got)
	}

	a, _ := ttApp(t, t.TempDir())
	a.data.startTaut(a)
	t.Cleanup(func() { a.data.stopTaut() })
	if a.Taut() == nil {
		t.Fatal("taut didn't start")
	}
	tp := newTautPage(a)
	tp.go_("about", "")
	var b strings.Builder
	theme.DrawHook = func(s string) { b.WriteString(s + "\n") }
	defer func() { theme.DrawHook = nil }()
	gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(523, 600)), Now: time.Now()}
	tp.layout(gtx)
	got := b.String()
	for _, want := range []string{AppName + " " + appVersion(), "Credits"} {
		if !strings.Contains(got, want) {
			t.Errorf("About doesn't draw %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "No plugins installed") {
		t.Errorf("About still says no plugins are installed:\n%s", got)
	}
	if strings.Contains(got, "Compatible with") {
		t.Errorf("About still lists per-plugin credits:\n%s", got)
	}
	if i, j := strings.Index(got, "Jeremy"), strings.Index(got, "Rowan"); i < 0 || j < i {
		t.Errorf("About's contributors don't start with Jeremy then Rowan:\n%s", got)
	}
}
