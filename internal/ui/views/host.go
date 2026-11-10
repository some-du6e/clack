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
	OpenProfile(anchor image.Point, userID string)
	OpenMessageMenu(anchor image.Point, m core.Message)
	OpenLightbox(img richtext.Image, m core.Message)
	Toast(text string)
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
