package composer

import (
	"image"
	"io"
	"strings"

	"gioui.org/io/clipboard"
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/openurl"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// Texty's link menus (boot-shared, build 83aff18a1847): a right-click on
// a link in the composer opens LinkContextMenu (HysJ), on a slug
// SlugContextMenu (tA8u), each a ContextMenu (l3cY) at the pointer
// (position "right-bottom", tryOppositePosition, width 320) over a
// Menu template, msg_input's strings. TextyInput.onContextMenu calls
// preventDefault for them, so the desktop app's own menu (Cut, Copy,
// Paste, the spelling rows) doesn't come.
//
// LinkContextMenu: Copy link, Edit link, (Add / Remove link preview), a
// separator, "Display as" URL and a separator when the link's text isn't
// its URL, and Remove link in red. SlugContextMenu: Copy link, Edit link
// for a link slug that shows less than its URL (isTruncatedLinkSlug),
// (the link preview rows), a separator; Open in browser and a separator
// for a link slug; Open in split view (and Open in new window) and a
// separator for a message slug; then "Display as" URL. The link preview
// rows need unfurl_skeletons (LXOb wb) and a draft's unfurl state Clack
// doesn't keep, so they never show; Karim's account isn't in
// unfurl_skeletons either. Open in new window opens a message in a window
// of its own (jumpToMessage's shouldOpenInTile), which Clack has none of.

// linkOpenURL opens a link in the browser (tests replace it).
var linkOpenURL = func(u string) { _ = openurl.Open(u) }

// bindLinkEditing wires the editor's link right-clicks and clicks.
func (c *Composer) bindLinkEditing() {
	c.ed.OnElementContext = c.linkContext
	c.ed.OnLinkClick = c.linkClicked
}

// linkMenuTap sees each link or slug menu's rows as they open (tests).
var linkMenuTap func(items []popups.MenuItem)

// linkContext is TextyInput.onContextMenu over a link or a slug.
func (c *Composer) linkContext(el edElem, at image.Point) bool {
	var items []popups.MenuItem
	if el.slug {
		items = c.slugMenuItems(el)
	} else {
		items = c.linkMenuItems(el)
	}
	if linkMenuTap != nil {
		linkMenuTap(items)
	}
	if len(items) == 0 {
		return true // SlugContextMenu renders null: no menu at all
	}
	c.h.ShowOverlay(popups.NewMenuOpts(c.h, items, popups.MenuOpts{
		Anchor: image.Rectangle{Min: at, Max: at}, Width: 320, Popover: "right-bottom", TryOpposite: true}))
	return true
}

// onRange runs f over the element's range as it is now (the menu's
// range, kept live: an edit that came meanwhile moves it, one that
// touched it leaves the row nothing to act on).
func (c *Composer) onRange(r edRange, f func(lo, hi int)) func() {
	return func() {
		if lo, hi, ok := c.ed.resolve(r); ok {
			f(lo, hi)
		}
	}
}

// linkMenuItems is LinkContextMenu's template for the <a> el.
func (c *Composer) linkMenuItems(el edElem) []popups.MenuItem {
	r, url := c.ed.liveRange(el.lo, el.hi), el.url
	items := []popups.MenuItem{
		{Key: "copy", Label: msgInputNS.T("Copy link"), Icon: "link", DoCtx: func(gtx layout.Context) { c.copyLink(gtx, url) }},
		{Key: "edit-link", Label: msgInputNS.T("Edit link"), Icon: "edit", Do: c.onRange(r, func(lo, hi int) { c.editLink(lo, hi, url) })},
		popups.Sep(),
	}
	if el.text != url { // W = r !== a: the text isn't its URL
		items = append(items,
			popups.MenuItem{Key: "header-display-as", Header: msgInputNS.T("Display as")},
			popups.MenuItem{Key: "url", Label: msgInputNS.T("URL"), Do: c.onRange(r, func(lo, hi int) { c.displayAsURL(lo, hi, url) })},
			popups.Sep())
	}
	return append(items, popups.MenuItem{Key: "remove-link", Label: msgInputNS.T("Remove link"), Danger: true,
		Do: c.onRange(r, func(lo, hi int) { c.ed.unlinkRange(lo, hi); c.Focus() })})
}

// slugMenuItems is SlugContextMenu's template for the slug el.
func (c *Composer) slugMenuItems(el edElem) []popups.MenuItem {
	at := el.atom
	r, url := c.ed.liveRange(el.lo, el.hi), el.url
	var items []popups.MenuItem
	if url != "" {
		items = append(items, popups.MenuItem{Key: "copy", Label: msgInputNS.T("Copy link"), Icon: "link",
			DoCtx: func(gtx layout.Context) { c.copyLink(gtx, url) }})
		if truncatedLinkSlug(at) {
			items = append(items, popups.MenuItem{Key: "edit-link", Label: msgInputNS.T("Edit link"), Icon: "edit",
				Do: c.onRange(r, func(lo, hi int) { c.editLink(lo, hi, url) })})
		}
		items = append(items, popups.Sep())
		if at.Kind == AtomLink { // "attachment" or "link"
			items = append(items,
				popups.MenuItem{Key: "open-in-browser", Label: msgInputNS.T("Open in browser"), Icon: "new-window",
					Do: func() { linkOpenURL(url) }},
				popups.Sep())
		}
	}
	if at.Kind == AtomMessage {
		// useShouldShowSplitViewButton: not in the split view itself
		if sv, ok := c.h.(popups.SplitViewer); ok && !sv.InSplitView() {
			if it, ok := popups.SplitViewItem(c.h, messageSlugRoute(at), false); ok {
				it.Key = "open-in-split-view"
				items = append(items, it, popups.Sep())
			}
		}
	}
	if url != "" {
		items = append(items,
			popups.MenuItem{Key: "header-display-as", Header: msgInputNS.T("Display as")},
			popups.MenuItem{Key: "url", Label: msgInputNS.T("URL"), Do: c.onRange(r, func(lo, hi int) { c.displayAsURL(lo, hi, url) })})
	}
	return items
}

// messageSlugRoute is getSplitViewNavProps for a message slug: its thread
// (tertiaryObjectId) or the message in its channel.
func messageSlugRoute(a *Atom) string {
	if a.Thread != "" && a.Thread != a.ID {
		return "c/" + a.Channel + "/t/" + a.Thread
	}
	return "c/" + a.Channel + "/m/" + a.ID
}

// truncatedLinkSlug is bZFu isTruncatedLinkSlug: a link slug whose label
// isn't its URL.
func truncatedLinkSlug(a *Atom) bool {
	return a != nil && a.Kind == AtomLink && a.URL != a.Text
}

// copyLink is onCopyToClipboard: the URL as text.
func (c *Composer) copyLink(gtx layout.Context, url string) {
	gtx.Execute(clipboard.WriteCmd{Type: "application/text", Data: io.NopCloser(strings.NewReader(url))})
	c.Focus()
}

// displayAsURL is onDisplayAsUrl: the element becomes its URL, linked
// (one undo step: Slack swaps the history entry for one that puts the
// text and its formats back).
func (c *Composer) displayAsURL(lo, hi int, url string) {
	c.ed.replaceRange(lo, hi, url, Attr{Link: url}, -1)
	c.Focus()
}

// editLink is TextyInput.onEditLink for [lo, hi): the link dialog over
// the element, its text editable unless it holds an embed; a link slug
// that shows less than its URL is edited as its URL (isEditingSlug).
func (c *Composer) editLink(lo, hi int, url string) {
	if c.ed.plainText { // isPlainTextModeEnabledPref
		return
	}
	d := c.ed.Doc()
	if lo < 0 || hi > d.Len() || hi <= lo {
		return
	}
	f := linkForm{lo: lo, hi: hi}
	if a := d.Attrs[lo].Atom; hi-lo == 1 && truncatedLinkSlug(a) {
		f.text, f.url, f.editable, f.editingSlug = a.URL, a.URL, true, true
	} else {
		s := d.String(lo, hi)
		f.text, f.editable = s, !strings.ContainsRune(s, ObjRune)
		f.url = d.Attrs[lo].Link // getLinkOpLink(ops[0]) …
		if f.url == "" {
			f.url = url // … || contextMenu.url
		}
	}
	c.selectForLink(lo, hi) // preventToolbarOnNextSelection
	c.openLinkForm(f)
}

// selectForLink selects [lo, hi) for a link form without the floating
// formatting bar (preventToolbarOnNextSelection).
func (c *Composer) selectForLink(lo, hi int) {
	c.ed.SetSelection(lo, hi)
	c.fbar.hidLo, c.fbar.hidHi = lo, hi
}
