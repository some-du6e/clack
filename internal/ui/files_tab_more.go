package ui

// The Files & links tab's other ways out (files-links-2,
// docs/FILES-AUDIT.md "Files & links tab, round 2"): the media viewer's
// gallery, the link details pane and the tab bar's right-click menu.

import (
	"context"
	"image"
	"strings"
	"sync"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/ctxclick"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/views"
)

// secLinkDetails is the secondary pane's kind for LinkDetailsView.
const secLinkDetails secKind = 67

var tabCtxTags sync.Map // *App -> map[string]*int: the tab bar's right-click tags (UI goroutine only)

// OpenMediaGallery opens the media viewer on items at idx (the Files &
// links tab's fileChannelMediaInfo; views.ChannelFiles.openMedia).
func (a *App) OpenMediaGallery(items []popups.GalleryItem, idx int) {
	if lb := popups.NewFileGallery(a, items, idx); lb != nil {
		a.ShowOverlay(lb)
	}
}

// OpenLinkDetails shows LinkDetailsView for url in the secondary pane
// (toLinkDetails: the view's id is link_details, so a second link
// replaces the first).
func (a *App) OpenLinkDetails(url, title, icon string) {
	v := views.NewLinkDetails(a, url, title, icon)
	v.Close = a.CloseSecondary
	a.secondary = secondaryState{kind: secLinkDetails, width: a.secondary.width}
	a.secondary.view = v
	a.Invalidate()
}

// LinkDetailsShown is the link the secondary pane's link details show
// ("" for none).
func (a *App) LinkDetailsShown() string {
	if v, ok := a.secondary.view.(*views.LinkDetails); ok && a.secondary.kind == secLinkDetails && a.secondary.open() {
		return v.URL()
	}
	return ""
}

// builtinTabIDs are isKnownChannelTabId's ids among the tabs Clack draws:
// their context menu hides them ("Hide tab") instead of removing them.
var builtinTabIDs = map[string]bool{"files": true, "workflows": true, "pins": true, "bookmarks": true}

// tabCtx registers the right-click (or macOS Ctrl+click) on a tab of the
// bar at r: the tab's context menu (useTabSchemaToTabItems'
// getTabContextMenu).
func (a *App) tabCtx(gtx layout.Context, ch string, t chTab, r image.Rectangle) {
	m, _ := tabCtxTags.Load(a)
	tags, _ := m.(map[string]*int)
	if tags == nil {
		tags = map[string]*int{}
		tabCtxTags.Store(a, tags)
	}
	k := ch + "." + t.key
	tag := tags[k]
	if tag == nil {
		tag = new(int)
		tags[k] = tag
	}
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: tag, Kinds: pointer.Press})
		if !ok {
			break
		}
		if e, ok := ev.(pointer.Event); ok && ctxclick.Press(e) {
			a.openTabMenu(ch, t, a.ptrWin)
		}
	}
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	defer pointer.PassOp{}.Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, tag)
}

// openTabMenu is getTabContextMenu for a built-in tab: with tab editing
// (useCanEditTabs) the "remove" group's "Hide tab" (danger; removeTab:
// conversations.removeTab, then Messages if it was the open tab). Slack
// shows no menu when the template is empty.
func (a *App) openTabMenu(ch string, t chTab, at image.Point) {
	id := strings.TrimPrefix(t.key, "tab:")
	c := a.Core()
	if c == nil || !builtinTabIDs[id] {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if !c.CanEditTabsChecked(ctx, ch) {
			return
		}
		a.Post(func() {
			items := []popups.MenuItem{{Label: i18n.T("tabbed-channel", "Hide tab"), Danger: true, Do: func() { a.hideTab(ch, id) }}}
			a.ShowOverlay(popups.NewMenuOpts(a, items, popups.MenuOpts{Anchor: image.Rectangle{Min: at, Max: at.Add(image.Pt(1, 1))},
				Width: 240}))
		})
	}()
}

// hideTab is removeTab: the open tab goes back to Messages first
// (navigateToTab Channel), then conversations.removeTab; the "tabs" event
// re-reads the bar.
func (a *App) hideTab(ch, id string) {
	tabs := a.channelTabs(ch)
	if i := a.chTab[ch]; i > 0 && i < len(tabs) && tabs[i].key == "tab:"+id {
		a.chTab[ch] = 0
	}
	c := a.Core()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := c.HideTab(ctx, ch, id); err != nil {
			a.Post(func() { a.Toast("Couldn't do that: " + err.Error()) })
		}
	}()
}
