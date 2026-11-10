package ui

// Canvases (internal/ui/canvasview, docs/CANVASES.md): the full view route
// "docs/<F…>", the side panel, and the channel's canvas tab body. Named
// after mount_features.go so its init runs later and wraps mkPage.

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/ui/canvasview"
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
				ms = append(ms, MenuItem{Label: it.Label, Icon: it.Icon, Divider: it.Divider, Disabled: it.Disabled, Checked: it.Checked, Do: it.Do})
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
		act.SideView = func() { a.OpenCanvas(id) } // "Open in split view"
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

// ---- creating and sharing (stage 3) ----

func init() {
	canvasShare = (*App).shareCanvasMenu
}

// newChannelCanvas is the channel tab bar's "+" > Canvas: a channel canvas
// tab (conversations.canvases.create) that opens once the tab bar reloads.
func (a *App) newChannelCanvas(ch string) {
	c := a.Core()
	if c == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		id, err := c.CreateCanvas(ctx, ch, "", "")
		a.Post(func() {
			if err != nil {
				a.Toast("Couldn't create the canvas: " + err.Error())
				return
			}
			if a.data != nil {
				delete(a.data.fetched, ch)
				a.data.opened(ch)
			}
			a.pendingCanvasTab(ch, id)
		})
	}()
}

// pendingCanvasTab selects ch's tab for canvas id as soon as it is listed.
func (a *App) pendingCanvasTab(ch, id string) {
	tries := 0
	var check func()
	check = func() {
		for i, t := range a.channelTabs(ch) {
			if t.fileID == id {
				if a.chTab == nil {
					a.chTab = map[string]int{}
				}
				a.chTab[ch] = i
				a.Invalidate()
				return
			}
		}
		if tries++; tries < 40 {
			time.AfterFunc(250*time.Millisecond, func() { a.Post(check) })
		}
	}
	check()
}

// addTabMenu is the tab bar's "+": Slack offers a canvas, a list and more.
func (a *App) addTabMenu(ch string) {
	a.menu(a.PointerPos(), []MenuItem{
		{Label: "Canvas", Icon: "canvas", Do: func() { a.newChannelCanvas(ch) }},
		{Label: "List", Icon: "list-tab", Do: func() { a.Toast("Adding lists isn't supported yet") }},
	})
}

// CreateCanvas is Files' "+ New Canvas": a standalone canvas, opened in
// full view.
func (a *App) CreateCanvas() {
	c := a.Core()
	if c == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		id, err := c.CreateCanvas(ctx, "", "", "")
		a.Post(func() {
			if err != nil {
				a.Toast("Couldn't create the canvas: " + err.Error())
				return
			}
			a.OpenCanvasFull(id)
		})
	}()
}

// shareCanvasMenu is the canvas's Share: pick a conversation from the
// sidebar, then the access it gets; Clack sets it and posts the link.
func (a *App) shareCanvasMenu(id string) {
	m := a.model()
	var items []MenuItem
	seen := map[string]bool{}
	for _, s := range m.Sections {
		for _, cid := range s.Rows {
			c := m.conv(cid)
			if c == nil || seen[cid] || c.Archived || c.Preview {
				continue
			}
			seen[cid] = true
			cid, name := cid, c.displayName()
			icon := "hash"
			switch c.Kind {
			case "private":
				icon = "lock"
			case "im", "mpim":
				icon = "dms"
			}
			items = append(items, MenuItem{Label: name, Icon: icon, Do: func() { a.shareCanvasAccess(id, cid, name) }})
			if len(items) == 12 {
				break
			}
		}
		if len(items) == 12 {
			break
		}
	}
	if len(items) == 0 {
		a.Toast("No conversations to share to")
		return
	}
	a.menu(a.PointerPos(), append([]MenuItem{{Label: "Share canvas to…", Disabled: true}}, items...))
}

func (a *App) shareCanvasAccess(id, ch, name string) {
	share := func(level string) {
		c := a.Core()
		if c == nil {
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			err := c.ShareCanvas(ctx, id, []string{ch}, level, "")
			a.Post(func() {
				if err != nil {
					a.Toast("Couldn't share the canvas: " + err.Error())
					return
				}
				a.Toast("Canvas shared to " + name)
			})
		}()
	}
	a.menu(a.PointerPos(), []MenuItem{
		{Label: "Can edit", Icon: "edit", Do: func() { share("write") }},
		{Label: "Can view", Icon: "eye", Do: func() { share("read") }},
	})
}
