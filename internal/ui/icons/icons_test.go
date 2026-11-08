package icons

import (
	"strings"
	"testing"
)

func TestAllIconsParse(t *testing.T) {
	for _, n := range Names() {
		d := registry[n]
		for i, p := range d.parts {
			segs := parsePath(p.d)
			if len(segs) == 0 || segs[0].op != 'M' {
				t.Errorf("%s part %d: bad path %q -> %v", n, i, p.d, segs)
			}
			for _, s := range segs {
				for _, pt := range s.p {
					if pt.X < -1 || pt.X > 21 || pt.Y < -1 || pt.Y > 21 || pt.X != pt.X {
						t.Errorf("%s part %d: point out of viewbox %v", n, i, pt)
					}
				}
			}
		}
	}
}

func TestParseRelativeAndArc(t *testing.T) {
	segs := parsePath("M2 2 h4 v4 l-4 0 z M10 10 a2 2 0 1 1 4 0")
	if len(segs) < 6 {
		t.Fatalf("segs = %v", segs)
	}
	last := segs[len(segs)-1]
	if last.op != 'C' || last.p[2].X != 14 || last.p[2].Y != 10 {
		t.Fatalf("arc end = %v", last)
	}
}

func TestSlackNames(t *testing.T) {
	for slack, clack := range slackIconNames {
		if !Has(clack) {
			t.Errorf("slackIconNames[%q] = %q: not registered", slack, clack)
		}
	}
	// Every SvgIcon name taut's plugins pass (docs/PLUGINS-components.md §1.12).
	for _, n := range []string{
		"arrow-left", "bug", "caret-up-down", "check-filled", "eye-closed", "eye-open", "file-upload",
		"glasses", "glasses-filled", "magic-wand", "megaphone", "megaphone-filled", "notifications",
		"notifications-off", "paintbrush", "plug", "reorder", "sparkles", "trash-filled", "undo",
		"calendar", "caret-down", "caret-right", "close", "code", "copy", "edit", "emoji", "info",
		"lock", "minus", "plus", "settings", "warning",
	} {
		if !HasSlack(n) {
			t.Errorf("Slack icon %q has no counterpart", n)
		}
	}
	if got := Slack("calendar-filled"); got != "calendar" {
		t.Errorf("suffix fallback: %q", got)
	}
	if got := Slack("no-such-icon"); got != SlackFallback {
		t.Errorf("fallback: %q", got)
	}
	Get("no-such-clack-icon")
	if m := strings.Join(Missing(), " "); !strings.Contains(m, "no-such-icon") || !strings.Contains(m, "no-such-clack-icon") {
		t.Errorf("Missing() = %q: placeholder draws aren't recorded", m)
	}
	if n := testing.AllocsPerRun(100, func() { _ = Slack("notifications-off"); _ = Slack("trash-small") }); n != 0 {
		t.Errorf("Slack allocates: %v", n)
	}
}
