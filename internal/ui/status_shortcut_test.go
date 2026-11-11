package ui

import (
	"testing"

	"github.com/some-du6e/clack/internal/ui/popups"
)

// Slack's mod+shift+y (boot-render PG1Q) is openCustomStatusDialog with
// fromKeyboardShortcut, the "Set a status" dialog; Slack has no dark mode
// shortcut. Pressing it opens the status modal.
func TestStatusShortcut(t *testing.T) {
	a := navTestApp(t)
	n, m := parseCombo("Mod+Shift+Y")
	var bound *keyBinding
	for i, b := range a.keys.list {
		if b.name == n && b.mods == m {
			bound = &a.keys.list[i]
		}
		if b.desc == "Toggle dark mode" {
			t.Errorf("a dark mode shortcut: %s", b.desc)
		}
	}
	if bound == nil || bound.desc != "Set a status" {
		t.Fatalf("Mod+Shift+Y is %+v", bound)
	}
	dark := a.th.Dark
	bound.do(a)
	if a.th.Dark != dark {
		t.Error("Mod+Shift+Y toggled dark mode")
	}
	found := false
	for _, o := range a.overlays {
		if md, ok := o.(*popups.Modal); ok && !md.Dismissed() {
			found = true
		}
	}
	if !found {
		t.Errorf("no status modal: overlays %d", len(a.overlays))
	}
}
