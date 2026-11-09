// Package canvasview draws a Slack canvas natively (docs/CANVASES.md): the
// title and the document, block by block, with richview laying out the inline text. It
// keeps the view live (files.open plus the realtime document_update echo,
// or polling when realtime is off) and edits through core's canvases.edit.
//
// Three modes, as in Slack: the channel's canvas tab (in the channel's
// body), the side panel (canvases opened from a message, a link or a DM)
// and the full view (Files → Canvases, "Open in full view").
//
// Scraping policy: canvas contents live in memory only.
package canvasview

import (
	"image"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/imgcache"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Host is what the view needs from the ui root (the shared names of
// docs/NATIVE.md).
type Host interface {
	Core() *core.App
	Theme() *theme.Theme
	Images() *imgcache.Cache
	Invalidate()
	Navigate(route string)
	OpenProfile(anchor image.Point, userID string)
	OpenLightbox(img richtext.Image, m core.Message)
	Toast(text string)
}

// Mode is where the canvas is shown.
type Mode uint8

const (
	Tab  Mode = iota // a channel's canvas tab: no bar, a floating More options
	Side             // the side panel: a 49px bar with expand / more / close
	Full             // the main view: a header like a channel's
)

// Actions are the shell's handlers for the header buttons (nil hides one).
type Actions struct {
	Close    func()             // side panel ×
	FullView func()             // "Open in full view"
	SideView func()             // "Open in side panel"
	OpenURL  func(url string)   // links that aren't Slack's own
	Canvas   func(id string)    // a link to another canvas
	Share    func(id string)    // "Share": the ui root's conversation picker
	Menu     func(m []MenuItem) // "More actions": the shell's menu at the pointer
}

// MenuItem is one row of a canvas menu (More options, the bottom
// toolbar's insert and formatting menus).
type MenuItem struct {
	Label, Icon string
	Divider     bool // a separator instead of an item
	Disabled    bool
	Checked     bool // a radio item's check (the formatting menu's current style)
	Do          func()
}
