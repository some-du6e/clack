package shortcut

import "strings"

// ClickHint is how Slack labels a modifier click on a menu row (the
// "Open in split view" and "Open in new window" rows' hints in
// getTilesTemplate, get-activity-item-menu-template and the tab rail's
// menu): the modifiers of s ("Mod+Alt", in this package's syntax) then
// "Click", space-separated, "⌘ Opt Click" and "⌘ Click" on macOS,
// "Ctrl Alt Click" and "Ctrl Click" elsewhere. Strings that aren't a
// modifier list come back as they are.
func ClickHint(s string) string { return ClickHintFor(GOOS, s) }

// ClickHintFor is ClickHint on goos ("darwin", "windows", "linux").
func ClickHintFor(goos, s string) string {
	c, ok := parse(s + "+Click")
	if !ok {
		return s
	}
	var parts []string
	if goos == "darwin" {
		// Slack's order: "⌘ Opt Click"
		if c.ctrl {
			parts = append(parts, "Ctrl")
		}
		if c.mod {
			parts = append(parts, "⌘")
		}
		if c.alt {
			parts = append(parts, "Opt")
		}
	} else {
		if c.mod || c.ctrl {
			parts = append(parts, "Ctrl")
		}
		if c.alt {
			parts = append(parts, "Alt")
		}
	}
	if c.shift {
		parts = append(parts, "Shift")
	}
	return strings.Join(append(parts, "Click"), " ")
}
