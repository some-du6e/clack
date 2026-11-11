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
	if subscribed { // the message I18n's (ojBT)
		return nsMessage.T("Turn off notifications for replies")
	}
	return nsMessage.T("Get notified about new replies")
}

// NewThreadHeaderMenu is the thread pane header's ⋮ (Slack's
// SecondaryHeaderMore for a Thread view, module IO7k): the subscribe
// toggle (left out while the header shows the bell, as Slack drops it when
// thread_header_mute_button is on and the root is loaded), then Copy link
// (L) to the root, then the surface actions: Open in split view, except in
// the split view itself (useShouldShowSplitViewButton). Slack's other rows
// have nothing behind them in Clack (Summarize thread needs Slack AI,
// Start huddle in thread, Open in new window needs a second window) and
// are not shown (docs/THREADS-AUDIT.md).
func NewThreadHeaderMenu(h Host, anchor image.Rectangle, root core.Message, subscribed, bell bool, toggle func()) *Menu {
	var items []MenuItem
	if !bell && toggle != nil {
		items = append(items, MenuItem{Key: "notifications", Label: ThreadFollowLabel(subscribed), Do: toggle})
	}
	items = append(items, MenuItem{Key: "copy_link", Label: nsMenu.T("Copy link"), Hint: "L", DoCtx: func(gtx layout.Context) { // SecondaryHeaderMore (IO7k)
		link := root.Permalink
		if link == "" {
			link = permalink(h, root.Channel, root.TS, "")
		}
		copyText(gtx, link) // Slack's copyLink thunk: the clipboard only, no toast
	}})
	if sv, ok := h.(SplitViewer); ok && !sv.InSplitView() && root.Channel != "" && root.TS != "" {
		thread := root.TS
		if root.ThreadTS != "" {
			thread = root.ThreadTS
		}
		if it, ok := SplitViewItem(h, "c/"+root.Channel+"/t/"+thread, false); ok {
			items = append(items, Sep(), it)
		}
	}
	return NewMenuOpts(h, items, MenuOpts{Anchor: anchor, AlignRight: true})
}
