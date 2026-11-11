package popups

import (
	"image"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
)

// ThreadFollowLabel is the subscribe action's name for a thread (Slack's
// message action module ojBT, the thread header bell's tooltip and the
// header ⋮ menu row).
func ThreadFollowLabel(subscribed bool) string {
	if subscribed {
		return "Turn off notifications for replies"
	}
	return "Get notified about new replies"
}

// NewThreadHeaderMenu is the thread pane header's ⋮ (Slack's
// SecondaryHeaderMore for a Thread view, module IO7k): the subscribe
// toggle (left out while the header shows the bell, as Slack drops it when
// thread_header_mute_button is on and the root is loaded), then Copy link
// (L) to the root. Slack's other rows have nothing behind them in Clack
// (Summarize thread needs Slack AI, Start huddle in thread, Open in split
// view / new window) and are not shown (docs/THREADS-AUDIT.md).
func NewThreadHeaderMenu(h Host, anchor image.Rectangle, root core.Message, subscribed, bell bool, toggle func()) *Menu {
	var items []MenuItem
	if !bell && toggle != nil {
		items = append(items, MenuItem{Key: "notifications", Label: ThreadFollowLabel(subscribed), Do: toggle})
	}
	items = append(items, MenuItem{Key: "copy_link", Label: "Copy link", Hint: "L", DoCtx: func(gtx layout.Context) {
		link := root.Permalink
		if link == "" {
			link = permalink(h, root.Channel, root.TS, "")
		}
		copyText(gtx, link) // Slack's copyLink thunk: the clipboard only, no toast
	}})
	return NewMenuOpts(h, items, MenuOpts{Anchor: anchor, AlignRight: true})
}
