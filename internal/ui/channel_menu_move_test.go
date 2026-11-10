package ui

import (
	"image"
	"slices"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

func findItem(items []popups.MenuItem, label string) (popups.MenuItem, bool) {
	for _, it := range items {
		if it.Label == label {
			return it, true
		}
		if s, ok := findItem(it.Sub, label); ok {
			return s, true
		}
	}
	return popups.MenuItem{}, false
}

// The sidebar row menu's section rows are Slack's
// getChannelSectionsTemplate (sidebar): with a custom section, "Move
// channel" ▸ the header's Move to… rows instead of Star, and picking a
// section moves the channel there (users.channelSections.channels.
// bulkUpdate, out of the section it was in).
func TestSidebarMenuMoveChannel(t *testing.T) {
	e, fs := previewEnv(t)
	ch := customSectionChannel(t, e)
	var got []popups.MenuItem
	sbMenuTap = func(id string, items []popups.MenuItem) {
		if id == ch {
			got = items
		}
	}
	t.Cleanup(func() { sbMenuTap = nil })
	e.a.openSidebarMenu(ch, image.Pt(120, 200), slackc.JSFunc{})
	if got == nil {
		t.Fatal("the row menu didn't open")
	}
	if _, ok := findItem(got, "Star channel"); ok {
		t.Error("a channel with custom sections still offers Star (Slack moves it to Starred instead)")
	}
	move, ok := findItem(got, "Move channel")
	if !ok {
		t.Fatalf("no Move channel row: %s", menuLabelsP(got))
	}
	want := menuLabels(e.a.moveToItems(ch, e.a.channelSections(ch)))
	if l := menuLabelsP(move.Sub); l != want {
		t.Errorf("Move channel ▸\n got %s\nwant %s", l, want)
	}
	proj, ok := findItem(move.Sub, "Projects")
	if !ok || proj.Do == nil {
		t.Fatalf("no Projects row in %s", menuLabelsP(move.Sub))
	}
	projID := ""
	for _, s := range fs.Sections() {
		if s.Name == "Projects" {
			projID = s.ID
		}
	}
	proj.Do()
	e.until("moved into Projects", 10*time.Second, func() bool {
		in, out := false, false
		for _, s := range fs.Sections() {
			switch s.ID {
			case projID:
				in = slices.Contains(s.Channels, ch)
			case "L0FAKE01":
				out = !slices.Contains(s.Channels, ch)
			}
		}
		return in && out
	})
}

// Without a custom section the row menu keeps Star and adds "Move to new
// section", which opens Create a section with the channel chosen.
func TestSidebarMenuMoveToNewSection(t *testing.T) {
	e := ctxShell(t)
	var got []popups.MenuItem
	sbMenuTap = func(id string, items []popups.MenuItem) {
		if id == svRandom {
			got = items
		}
	}
	e.a.openSidebarMenu(svRandom, image.Pt(120, 200), slackc.JSFunc{})
	if _, ok := findItem(got, "Star channel"); !ok {
		t.Errorf("no Star channel row: %s", menuLabelsP(got))
	}
	it, ok := findItem(got, "Move to new section")
	if !ok || it.Do == nil {
		t.Fatalf("no Move to new section row: %s", menuLabelsP(got))
	}
	it.Do()
	if _, ok := topOverlay[*popups.CreateSectionModal](e.a); !ok {
		t.Error("Move to new section didn't open Create a section")
	}
}

// The channel and account menus' hooks are always installed with the
// window (Run calls installHooks; mount_features sets it), so their
// items reach the real Channel details, Move to… and Preferences: the
// old "aren't available yet" / "isn't supported yet" toasts, for nil
// hooks, could never show.
func TestChannelMenuHooksInstalled(t *testing.T) {
	if installHooks == nil {
		t.Fatal("installHooks is nil: Run would leave popups' hooks unset")
	}
	e, _ := previewEnv(t)
	for name, set := range map[string]bool{
		"OpenChannelDetails": popups.OpenChannelDetails != nil, "OpenChannelSettings": popups.OpenChannelSettings != nil,
		"MoveChannel": popups.MoveChannel != nil, "OpenPreferences": popups.OpenPreferences != nil,
	} {
		if !set {
			t.Errorf("installHooks left popups.%s nil", name)
		}
	}
	ch := customSectionChannel(t, e)
	e.openChannel(ch)
	e.settle(200 * time.Millisecond)
	e.a.toast = toastState{}

	det, ok := findItem(popups.NewChannelMenu(e.a, image.Pt(900, 60), ch).Items(), "Open channel details")
	if !ok {
		t.Fatal("no Open channel details row")
	}
	det.Do()
	e.until("channel details", 5*time.Second, func() bool { _, ok := topOverlay[detailsOverlay](e.a); return ok })
	e.a.overlays = nil

	mv, ok := findItem(popups.NewChannelMenu(e.a, image.Pt(900, 60), ch).Items(), "Move channel")
	if !ok {
		t.Fatal("no Move channel row")
	}
	mv.Do()
	e.until("the Move to… menu", 5*time.Second, func() bool { _, ok := topOverlay[*popups.Menu](e.a); return ok })
	if m, _ := topOverlay[*popups.Menu](e.a); len(m.Labels()) == 0 || m.Labels()[0] != "[Move to…]" {
		t.Errorf("Move channel opened %v, want the Move to… menu", m.Labels())
	}
	e.a.overlays = nil

	opened := false
	prefsTap = func(*prefsModal) { opened = true }
	t.Cleanup(func() { prefsTap = nil })
	pr, ok := findItem(popups.NewUserMenu(e.a, image.Pt(10, 10)).Items(), "Preferences")
	if !ok {
		t.Fatal("no Preferences row")
	}
	pr.Do()
	e.until("Preferences", 5*time.Second, func() bool { return opened })
	if e.a.toast.text != "" {
		t.Errorf("a menu item toasted %q", e.a.toast.text)
	}
}

func menuLabelsP(items []popups.MenuItem) string {
	var out []MenuItem
	for _, it := range items {
		l := it.Label
		if it.Header != "" {
			l = it.Header
		}
		out = append(out, MenuItem{Label: l})
	}
	return menuLabels(out)
}
