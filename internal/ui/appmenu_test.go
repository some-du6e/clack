package ui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/ui/shortcut"
)

func renderAppMenu(items []MenuItem, depth int, b *strings.Builder) {
	for _, it := range items {
		b.WriteString(strings.Repeat("  ", depth))
		if it.Divider {
			b.WriteString("---\n")
			continue
		}
		b.WriteString(it.Label)
		if it.Disabled {
			b.WriteString(" (off)")
		}
		b.WriteString("\n")
		renderAppMenu(it.Sub, depth+1, b)
	}
}

// The Linux and Windows app menu: macMenus without AppKit's items, the
// app menu's Preferences, Sign Out and Quit in File, and Help >
// Troubleshooting with the logs, the freeze reports and Copy Diagnostics.
func TestAppMenuWinLinux(t *testing.T) {
	a := New(Options{ConfigDir: t.TempDir(), NoRealtime: true,
		CheckUpdate: func(context.Context) (UpdateInfo, bool, error) { return UpdateInfo{}, false, nil }}, nil)
	var b strings.Builder
	renderAppMenu(a.appMenuItems(), 0, &b)
	got := b.String()
	var tops []string
	for _, l := range strings.Split(got, "\n") {
		if l != "" && !strings.HasPrefix(l, " ") {
			tops = append(tops, l)
		}
	}
	if w := "File|Edit|View|Go|History|Help"; strings.Join(tops, "|") != w {
		t.Errorf("menus %q, want %q\n%s", tops, w, got)
	}
	for _, want := range []string{
		"  Preferences…", "  Sign Out", "  Quit Clack",
		"  Troubleshooting\n    Show Logs in File Manager\n    Show Freeze Reports\n    Copy Diagnostics\n",
		"  Release Notes",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in\n%s", want, got)
		}
	}
	for _, bad := range []string{"Services", "Hide Clack", "Cut", "Paste", "Minimize", "Toggle Full Screen", "About Clack"} {
		if strings.Contains(got, "  "+bad+"\n") {
			t.Errorf("%q is AppKit's, not in the app menu:\n%s", bad, got)
		}
	}
	// signed out: the web client's items are greyed
	if !strings.Contains(got, "New Message (off)") {
		t.Errorf("signed out, New Message isn't greyed:\n%s", got)
	}
}

// File on Linux and Windows: getFileMenuItems' New Message, New Channel,
// New Canvas (Ctrl+Shift+N) and Invite People (order 11, after the
// Workspace submenu's separator), then the app menu's Preferences; with
// Invite People left out its separator goes too.
func TestAppMenuFileItems(t *testing.T) {
	a, ca, _ := fakeApp(t, nil)
	file := func() ([]MenuItem, string) {
		for _, m := range a.appMenuItems() {
			if m.Label == "File" {
				var b strings.Builder
				renderAppMenu(m.Sub, 0, &b)
				return m.Sub, b.String()
			}
		}
		t.Fatal("no File menu")
		return nil, ""
	}
	items, got := file()
	if want := "New Message\nNew Channel\nNew Canvas\n---\nInvite People\n---\nPreferences…\n"; !strings.HasPrefix(got, want) {
		t.Errorf("File:\n%s\nwant it to start:\n%s", got, want)
	}
	for _, it := range items {
		if it.Key == menuNewCanvas && it.Hint != shortcut.Format("Mod+Shift+N") {
			t.Errorf("New Canvas hint %q", it.Hint)
		}
		if (it.Key == menuNewChannel || it.Key == menuInvitePeople) && it.Hint != "" {
			t.Errorf("%s hint %q: Slack gives it no accelerator", it.Label, it.Hint)
		}
	}
	ca.SetTeamPrefs(map[string]json.RawMessage{"invites_only_admins": json.RawMessage("true")})
	if _, got := file(); !strings.HasPrefix(got, "New Message\nNew Channel\nNew Canvas\n---\nPreferences…\n") {
		t.Errorf("without Invite People:\n%s", got)
	}
}
