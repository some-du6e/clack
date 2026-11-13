// Package views holds yoloslack's full-page views: Activity, DMs,
// Later, Files, Threads, Huddles, Drafts & sent, Directories and the
// channel details dialog. Each view is built with NewX(h Host) and drawn
// with Layout(gtx); the ui root mounts them for rail tabs and sidebar items.
//
// Scraping policy: message contents only live in memory here. Nothing in
// this package writes to disk.
package views

import (
	"image"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Host is what views need from the ui root. Method names follow the shared
// convention in docs/NATIVE.md so one *ui.App satisfies every package's Host.
type Host interface {
	Core() *core.App
	Theme() *theme.Theme
	Images() *imgcache.Cache
	Invalidate()
	// Navigate routes: "c/<id>", "c/<id>/t/<ts>", "activity", "dms",
	// "later", "files", "threads", "huddles", "drafts", "directories",
	// "search?q=".
	Navigate(route string)
	OpenThread(channel, ts string)
	CloseSecondary()
	// OpenProfile is a click on a person: the ui root opens their
	// profile in the secondary pane (Slack's showMemberProfile).
	OpenProfile(anchor image.Point, userID string)
	OpenMessageMenu(anchor image.Point, m core.Message)
	OpenLightbox(img richtext.Image, m core.Message)
	Toast(text string)
}

// openProfileCard opens the profile card as a click popover (Slack's
// ProfileCardTrigger, boot-shared 4WSP) on a host that has one (the ui
// root's App.OpenProfileCard), else the profile (OpenProfile).
func openProfileCard(h Host, anchor image.Point, userID string) {
	if c, ok := h.(interface {
		OpenProfileCard(anchor image.Point, userID string)
	}); ok {
		c.OpenProfileCard(anchor, userID)
		return
	}
	h.OpenProfile(anchor, userID)
}

// Panes is optionally implemented by the Host to embed the real
// conversation widgets (messages + composer) inside a view's right-hand
// pane, the way Slack's DMs and Activity tabs show the selected
// conversation next to the list. Views type-assert for it; without it they
// draw a read-only preview and offer "Open in channel".
type Panes interface {
	// ChannelPane returns the widget for a conversation (header, history,
	// composer). Called every frame; the Host should cache per channel.
	ChannelPane(channelID string) layout.Widget
	// ThreadPane returns the widget for a thread (root + replies + composer).
	ThreadPane(channelID, threadTS string) layout.Widget
}

// PaneChooser is optionally implemented by the Host: a view calls
// PaneChosen when the user picks something in its own list (a DM, a Files
// section), so the simplified layout can move on from the list to it.
type PaneChooser interface {
	PaneChosen()
}

// Closer is implemented by views that are modal (ChannelDetails): the Host
// removes them once Closed reports true.
type Closer interface {
	Closed() bool
}

// NestedSecondary is optionally implemented by the Host: Slack's
// isNestedSecondaryViewEnabled (client-boot kl02 W7), the
// nested_secondary_view experiment outside the simplified layout mode,
// which the view headers follow. Without it the views read the
// experiment alone.
type NestedSecondary interface {
	NestedSecondaryView() bool
}

// WideSidebar is optionally implemented by the Host: Slack's wide
// sidebar columns, the grid's sidebar at W2CY's wide sizes with their own
// saved widths and the sidebar's BaseResizer on the right edge. Activity's
// and the DMs list's column is that sidebar: zfm1 tabHasWideSidebar
// (Activity, DMs; viewLayoutSidebarWideWidthIntent / Mode), or with extra
// true tabHasExtraWideSidebar (ActivityInbox, the Activity tab under
// activity_inbox; viewLayoutSidebarExtraWideWidthIntent / Mode).
type WideSidebar interface {
	// WideSidebarWidth applies the resizer's drags and returns the
	// column's width (px) in a region total px wide whose left edge is
	// the gtx frame's x = 0 (the panel beside the tab rail). Call it
	// before laying out the column, every frame the column shows.
	WideSidebarWidth(gtx C, total int, extra bool) int
	// WideSidebarHandle draws the resizer on the column's right edge at
	// x (px, the same frame), h px tall. Call it after the column and
	// what's beside it, so it is on top.
	WideSidebarHandle(gtx C, x, h int, extra bool)
}

// wideSidebarWidth is the wide column's width in a region total px wide:
// the Host's, or W2CY's wide auto width without one (no resizer).
func wideSidebarWidth(gtx C, h Host, total int, extra bool) int {
	if w, ok := h.(WideSidebar); ok {
		return w.WideSidebarWidth(gtx, total, extra)
	}
	w := int(float32(total) * dmsPaneFrac)
	w = max(min(w, gtx.Dp(dmsPaneMax)), gtx.Dp(dmsPaneMin))
	return min(w, total)
}

// wideSidebarHandle draws the Host's resizer at x, if it has one.
func wideSidebarHandle(gtx C, h Host, x, ht int, extra bool) {
	if w, ok := h.(WideSidebar); ok {
		w.WideSidebarHandle(gtx, x, ht, extra)
	}
}

// nestedSecondary is kl02 W7 for h.
func nestedSecondary(h Host) bool {
	if n, ok := h.(NestedSecondary); ok {
		return n.NestedSecondaryView()
	}
	c := h.Core()
	return c != nil && c.NestedSecondaryView()
}
