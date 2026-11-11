package shortcut

import "testing"

// Slack's literal modifier-click hints (boot-render getTilesTemplate,
// wrqs renderMenuPlaceholderItem, Zxh6's tab rail menu).
func TestClickHint(t *testing.T) {
	for _, c := range []struct{ goos, in, want string }{
		{"darwin", "Mod+Alt", "⌘ Opt Click"},
		{"darwin", "Mod", "⌘ Click"},
		{"linux", "Mod+Alt", "Ctrl Alt Click"},
		{"windows", "Mod+Alt", "Ctrl Alt Click"},
		{"linux", "Mod", "Ctrl Click"},
		{"linux", "not a combo", "not a combo"},
	} {
		if got := ClickHintFor(c.goos, c.in); got != c.want {
			t.Errorf("ClickHintFor(%q, %q) = %q, want %q", c.goos, c.in, got, c.want)
		}
	}
}
