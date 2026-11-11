package popups

import (
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/ui/shortcut"
)

// Slack's looksLikeChannelId (jh4t): /^(C|G|D|Z)[A-Z0-9]{8,}$/.
func TestLooksLikeChannelID(t *testing.T) {
	for id, want := range map[string]bool{
		"C0FAKE00001": true, "D0123456789": true, "G01234567": true, "Z9ABCDEF0": true,
		"C0123": false, "U0123456789": false, "c0123456789": false, "C01234567-": false, "": false,
		"threads": false, "C012345678a": false,
	} {
		if got := looksLikeChannelID(id); got != want {
			t.Errorf("looksLikeChannelID(%q) = %v, want %v", id, got, want)
		}
	}
}

// The split rows' hint is Slack's literal per-platform string.
func TestSplitHint(t *testing.T) {
	prev := shortcut.GOOS
	t.Cleanup(func() { shortcut.GOOS = prev })
	shortcut.GOOS = "darwin"
	if got := SplitHint(); got != "⌘ Opt Click" {
		t.Errorf("macOS: %q", got)
	}
	shortcut.GOOS = "linux"
	if got := SplitHint(); got != "Ctrl Alt Click" {
		t.Errorf("Linux: %q", got)
	}
}

// StripSplitView drops hidden split rows always and every split row when
// asked, collapsing the separators they leave.
func TestStripSplitView(t *testing.T) {
	labels := func(m *Menu) string { return strings.Join(m.Labels(), "|") }
	items := func() []MenuItem {
		return []MenuItem{
			{Label: "Copy"}, Sep(),
			{Key: SplitKey, Label: "Open in split view"}, Sep(),
			{Label: "Leave"},
		}
	}
	m := &Menu{items: items()}
	StripSplitView(m, false)
	if got := labels(m); got != "Copy|---|Open in split view|---|Leave" {
		t.Errorf("kept: %s", got)
	}
	StripSplitView(m, true)
	if got := labels(m); got != "Copy|---|Leave" || len(m.clicks) != 3 {
		t.Errorf("stripped: %s (%d clicks)", got, len(m.clicks))
	}
	hidden := items()
	hidden[2].Key = SplitKey + ":hidden"
	m = &Menu{items: hidden}
	StripSplitView(m, false)
	if got := labels(m); got != "Copy|---|Leave" {
		t.Errorf("hidden row: %s", got)
	}
	tail := []MenuItem{{Label: "Copy"}, Sep(), {Key: SplitKey, Label: "Open in split view"}}
	m = &Menu{items: tail}
	StripSplitView(m, true)
	if got := labels(m); got != "Copy" {
		t.Errorf("trailing separator: %s", got)
	}
}

// Without a split view (no SplitViewer) the row is still Slack's, with
// nothing to do; ok says so.
func TestSplitViewItemNoHost(t *testing.T) {
	it, ok := SplitViewItem(nil, "c/C0FAKE00001", true)
	if ok || it.Do != nil || it.Key != SplitKey || it.Icon != "move-to-split-view" || it.Hint == "" {
		t.Errorf("%+v ok=%v", it, ok)
	}
}
