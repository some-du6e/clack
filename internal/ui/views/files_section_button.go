package views

// FileSectionDropdownButton's menu outside the Files page (boot-shared
// ofyN, CDN build fa938a086569): with custom_file_sections on, the list
// header (YALQ ListHeader, before the title), the canvas header (ioWZ
// CanvasViewHeaderDefault, before the title) and a list tab's More
// actions (57SI MoreActionsMenu's "Move to section" ▸) offer the file's
// section menu, FileSectionMoveToMenu (QLxC, moveToItems), which asks
// for the collections when it opens (maybeFetchCollections). A removed
// file goes back to Recently viewed (addToRecentsOnRemove).

import (
	"context"
	"image"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// sectionHelpers holds one detached Files per host (UI goroutine only).
var sectionHelpers sync.Map // Host -> *Files

func sectionHelper(h Host) *Files {
	if f, ok := sectionHelpers.Load(h); ok {
		f := f.(*Files)
		f.frame()
		return f
	}
	f := NewFiles(h)
	f.detached = true
	f.frame()
	sectionHelpers.Store(h, f)
	return f
}

// SectionMenuItems is FileSectionMoveToMenu for file (nil without
// custom_file_sections): Move to…, the collections, Create section and
// Remove from {name}. It starts files.collections.list when it is due.
func SectionMenuItems(h Host, file core.File, source string) []popups.MenuItem {
	a := h.Core()
	if a == nil || !a.FileSectionsOn() {
		return nil
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if a.FetchFileCollections(ctx) == nil {
			h.Invalidate()
		}
	}()
	return sectionHelper(h).moveToItems(file, true, source)
}

// OpenSectionMenu opens FileSectionDropdownButton's menu under btn
// (position bottom-right, 320 wide as the Files page's).
func OpenSectionMenu(h Host, file core.File, btn image.Rectangle, source string) {
	ph, ok := h.(popups.Host)
	if !ok {
		return
	}
	items := SectionMenuItems(h, file, source)
	if len(items) == 0 {
		return
	}
	ph.ShowOverlay(popups.NewMenuOpts(ph, items, popups.MenuOpts{Anchor: btn, Width: 320, Popover: "bottom-right"}))
}

// SectionButtonLook is which of FileSectionDropdownButton's three
// buttons the file gets.
type SectionButtonLook int

const (
	// SectionStar: in no collection, the IconButton's "star".
	SectionStar SectionButtonLook = iota
	// SectionStarred: in the starred collection, the filled star on
	// sunflower (p-view_header_star_action_button_filled).
	SectionStarred
	// SectionEmoji: a collection with an emoji, the emoji in the button.
	SectionEmoji
	// SectionIcon: any other collection, the IconButton's "section".
	SectionIcon
)

// SectionButtonFor is FileSectionDropdownButton's choice for fileID and,
// for SectionEmoji, the emoji's image URL ("" while unknown).
func SectionButtonFor(a *core.App, fileID string) (SectionButtonLook, string) {
	c, in := a.FileCollectionOf(fileID)
	switch {
	case !in:
		return SectionStar, ""
	case c.Starred():
		return SectionStarred, ""
	case c.Emoji != "":
		u, _, _ := a.Emoji(trimColons(c.Emoji))
		return SectionEmoji, u
	}
	return SectionIcon, ""
}

func trimColons(s string) string {
	for len(s) > 0 && s[0] == ':' {
		s = s[1:]
	}
	for len(s) > 0 && s[len(s)-1] == ':' {
		s = s[:len(s)-1]
	}
	return s
}
