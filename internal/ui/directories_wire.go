package ui

import "github.com/some-du6e/clack/internal/ui/popups"

// MaybeLeaveChannel lets the channel browser's Leave button ask the way
// Slack's maybeLeaveChannel does (views.Directories; popups has the
// dialogs): leave runs at once for a public channel and after the
// LeaveChannel confirmation for a private one.
func (a *App) MaybeLeaveChannel(name, kind string, general bool, leave func()) {
	popups.MaybeLeaveChannel(a, name, kind, general, leave)
}
