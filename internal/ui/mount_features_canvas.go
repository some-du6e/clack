package ui

// Canvases (internal/ui/canvasview, docs/CANVASES.md): the full view route
// "docs/<F…>", the side panel, and the channel's canvas tab body. Named
// after mount_features.go so its init runs later and wraps mkPage.

import (
	"strings"
	"sync"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/canvasview"
)

func init() {
	prev := mkPage
	mkPage = func(a *App, r Route) View {
		if id := canvasRouteID(r); id != "" {
			return a.newCanvasView(id, canvasview.Full)
		}
		if prev != nil {
			return prev(a, r)
		}
		return nil
	}
}

// canvasRouteID is the canvas of a "docs/<F…>" route.
func canvasRouteID(r Route) string {
	if r.Page() != "docs" {
		return ""
	}
	id := strings.TrimPrefix(string(r), "docs/")
	if i := strings.IndexAny(id, "/?"); i >= 0 {
		id = id[:i]
	}
	if len(id) < 3 || id[0] != 'F' {
		return ""
	}
	return id
}

// canvasTabs caches each channel's canvas tab views (per App).
var canvasTabs sync.Map // *App -> map[string]*canvasview.View (UI goroutine only)

func (a *App) newCanvasView(id string, mode canvasview.Mode) *canvasview.View {
	canvasview.SetCopy(func(h canvasview.Host, s string) {
		a.copyText(s)
		a.Toast("Link copied to clipboard")
	})
	act := canvasview.Actions{
		OpenURL: openURL,
		Canvas:  a.OpenCanvas,
		Menu: func(items []canvasview.MenuItem) {
			var ms []MenuItem
			for _, it := range items {
				ms = append(ms, MenuItem{Label: it.Label, Icon: it.Icon, Do: it.Do})
			}
			a.menu(a.PointerPos(), ms)
		},
	}
	switch mode {
	case canvasview.Side:
		act.Close = a.CloseSecondary
		act.FullView = func() {
			a.CloseSecondary()
			a.OpenCanvasFull(id)
		}
	case canvasview.Full:
		act.SideView = func() {
			ch := a.lastChannel
			if ch != "" {
				a.Navigate("c/" + ch)
			} else {
				a.Back()
			}
			a.OpenCanvas(id)
		}
	case canvasview.Tab:
		act.FullView = func() { a.OpenCanvasFull(id) }
	}
	if canvasShare != nil {
		act.Share = func(id string) { canvasShare(a, id) }
	}
	return canvasview.New(a, id, mode, act)
}

// canvasShare opens the share dialog (canvas_share.go); nil hides Share.
var canvasShare func(a *App, id string)

// OpenCanvas shows a canvas in the side panel, as Slack does for canvases
// opened from a message, a link or a DM.
func (a *App) OpenCanvas(id string) {
	if a.secondary.kind != secNone && !a.secondary.hidden {
		if v, ok := a.secondary.view.(*canvasview.View); ok && v.ID() == id {
			return
		}
	}
	w := a.secondary.width
	if w < 520 {
		w = 520
	}
	a.secondary = secondaryState{kind: secDetails, channel: a.router.Current().Channel(), width: w}
	a.secondary.view = a.newCanvasView(id, canvasview.Side)
	a.Invalidate()
}

// OpenCanvasFull shows a canvas in the main view ("Open in full view").
func (a *App) OpenCanvasFull(id string) { a.Navigate("docs/" + id) }

// OpenCanvasLink opens a canvas link natively; false if url isn't one.
func (a *App) OpenCanvasLink(url string) bool {
	id := canvasview.CanvasLinkID(url)
	if id == "" {
		return false
	}
	a.OpenCanvas(id)
	return true
}

// canvasTabView is the body of ch's selected tab when it is a canvas.
func (a *App) canvasTabView(ch string) View {
	i := a.chTab[ch]
	if i <= 0 {
		return nil
	}
	tabs := a.channelTabs(ch)
	if i >= len(tabs) || tabs[i].icon != "canvas" || tabs[i].fileID == "" {
		return nil
	}
	m, _ := canvasTabs.Load(a)
	views, _ := m.(map[string]*canvasview.View)
	if views == nil {
		views = map[string]*canvasview.View{}
		canvasTabs.Store(a, views)
	}
	key := ch + "/" + tabs[i].fileID
	v := views[key]
	if v == nil {
		v = a.newCanvasView(tabs[i].fileID, canvasview.Tab)
		views[key] = v
	}
	return v
}
