package ui

import (
	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/messages"
)

// Red evidence (thread-load-speed-red1, on 6db17b41): the code before the
// fix has neither thread history slices nor ThreadView.SliceRange; these
// map the test's two reads onto what it has. The store held a thread as
// its replies cache (the whole thread), and the pane drew the model's
// messages whole, so its first message is the root.

func threadHistoryHeld(app *core.App, ch, ts string) bool {
	th, ok := app.S.Thread(ch, ts)
	return ok && len(th) > 0
}

func paneSliceFirst(tv *messages.ThreadView) string {
	if rs := tv.RowRects(); len(rs) > 0 {
		return rs[0].TS
	}
	return ""
}
