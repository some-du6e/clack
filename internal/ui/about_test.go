package ui

import (
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// Plugins > About is about Clack (Karim: Taut's own blurb there was
// "so awkward"): its name and version, the README's line and its
// repository; then the credits, a short line each for Taut and Slick
// (links, no mention or channel chips, nothing copied from their own
// pages) before the installed plugins' authors, and nothing more while
// none is installed.
func TestAboutIsAboutClack(t *testing.T) {
	if !strings.HasPrefix(aboutClack, "A fast Slack client for the desktop") || !strings.Contains(aboutClack, "<https://github.com/some-du6e/clack|") {
		t.Errorf("About's line: %q", aboutClack)
	}
	lines := []string{aboutClack}
	for _, c := range aboutSystems {
		lines = append(lines, c[1])
		if !strings.Contains(c[1], "<https://github.com/") || len(c[1]) > 80 {
			t.Errorf("credit %q: not a short line with its link", c[1])
		}
	}
	for _, s := range lines {
		for _, bad := range []string{"<@", "<#", "made by", "say hi", "client mod", "no Taut code"} {
			if strings.Contains(s, bad) {
				t.Errorf("%q has %q", s, bad)
			}
		}
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
}
