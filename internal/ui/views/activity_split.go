package views

import (
	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// splitOpen is Slack's maybeOpenInSurface for an activity row's click
// (boot-shared GMkG): with the split modifier (⌘⌥ on macOS, Ctrl+Alt
// elsewhere; shouldSplit) the item, marked read first, opens in the split
// view, its thread or its channel, instead of in the preview.
func (v *Activity) splitOpen(it *core.ActivityItem, key string) bool {
	return v.splitOpenIf(v.splitClick(), it, key)
}

// splitClick reports whether this frame's click is a split one (the
// host's modifiers).
func (v *Activity) splitClick() bool {
	sc, ok := v.h.(interface{ SplitClick() bool })
	return ok && sc.SplitClick()
}

// splitOpenIf is splitOpen for a click whose split state was split (a
// click decided a frame after it came: the row's clickLater).
func (v *Activity) splitOpenIf(split bool, it *core.ActivityItem, key string) bool {
	if !split || it.Channel == "" {
		return false
	}
	sv, ok := v.h.(popups.SplitViewer)
	if !ok {
		return false
	}
	if v.isUnread(it, key) {
		v.markRead(it, key)
	}
	route := "c/" + it.Channel
	if t := actThread(it); t != "" {
		route += "/t/" + t
	}
	sv.OpenSplit(route)
	return true
}
