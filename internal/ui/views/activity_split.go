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
	sc, ok := v.h.(interface{ SplitClick() bool })
	if !ok || !sc.SplitClick() || it.Channel == "" {
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
