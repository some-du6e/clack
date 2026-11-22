package ui

import (
	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/messages"
)

// The huge-thread test's reads of what the fix added (thread_load_speed_test.go
// calls only these, so an evidence run on the code before can swap them).

// threadHistoryHeld reports whether the store holds any of the thread's
// history (its replies reads' slices).
func threadHistoryHeld(app *core.App, ch, ts string) bool { return app.S.ThreadHeld(ch, ts) }

// paneSliceFirst is the first message of the slice the pane drew last.
func paneSliceFirst(tv *messages.ThreadView) string {
	first, _, _, _ := tv.SliceRange()
	return first
}
