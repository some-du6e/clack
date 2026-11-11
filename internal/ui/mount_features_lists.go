package ui

// Slack Lists (internal/ui/listview, docs/LISTS.md): the full view route
// "lists/<F…>", the side panel, the item pane, the channel's list tab
// body, list links and files in messages, Files → Lists, the Create menu
// and Activity's list items. Named after mount_features.go so its init
// runs later and wraps mkPage.

import (
	"context"
	"image"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/listview"
)

func init() {
	prev := mkPage
	mkPage = func(a *App, r Route) View {
		if id := listRouteID(r); id != "" {
			return a.newListView(id, listview.Full)
		}
		if prev != nil {
			return prev(a, r)
		}
		return nil
	}
}

// listRouteID is the list of a "lists/<F…>" route.
func listRouteID(r Route) string {
	if r.Page() != "lists" {
		return ""
	}
	id := strings.TrimPrefix(string(r), "lists/")
	if i := strings.IndexAny(id, "/?"); i >= 0 {
		id = id[:i]
	}
	if len(id) < 3 || id[0] != 'F' {
		return ""
	}
	return id
}

// ListLinkID returns the list (and item) of a list link: Slack's
// https://<team>.slack.com/lists/<T…>/<F…>[?record_id=Rec…], or "".
func ListLinkID(raw string) (list, record string) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || !strings.HasSuffix(u.Host, ".slack.com") {
		return "", ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i, p := range parts {
		if p != "lists" {
			continue
		}
		for _, q := range parts[i+1:] {
			if len(q) > 3 && q[0] == 'F' && strings.ToUpper(q) == q {
				return q, u.Query().Get("record_id")
			}
		}
	}
	return "", ""
}

var listTabs sync.Map // *App -> map[string]*listview.View (UI goroutine only)

func (a *App) newListView(id string, mode listview.Mode) *listview.View {
	act := a.listActions()
	switch mode {
	case listview.Side:
		act.Close = a.CloseSecondary
		act.FullView = func() {
			a.CloseSecondary()
			a.OpenListFull(id)
		}
	case listview.Full:
		act.SideView = func() {
			if ch := a.lastChannel; ch != "" {
				a.Navigate("c/" + ch)
			} else {
				a.Back()
			}
			a.OpenList(id)
		}
	case listview.Tab:
		act.FullView = func() { a.OpenListFull(id) }
	}
	return listview.New(a, id, mode, act)
}

func (a *App) listActions() listview.Actions {
	return listview.Actions{
		OpenURL:  openURL,
		Canvas:   a.OpenCanvas,
		OpenItem: a.OpenListItem,
		Copy: func(s string) {
			a.copyText(s)
			a.Toast(i18n.T("lists", "Link copied to clipboard"))
		},
		Menu: func(items []listview.MenuItem) {
			a.menu(a.PointerPos(), listMenuItems(items))
		},
		MenuAt: func(p image.Point, items []listview.MenuItem) {
			a.menu(p, listMenuItems(items))
		},
		People: a.listPeople,
	}
}

// listMenuItems is a list menu as the shell's menu rows (separators,
// danger rows and submenus included).
func listMenuItems(items []listview.MenuItem) []MenuItem {
	var ms []MenuItem
	for _, it := range items {
		ms = append(ms, MenuItem{Label: it.Label, Icon: it.Icon, Disabled: it.Disabled, Divider: it.Divider,
			Danger: it.Danger, Sub: listMenuItems(it.Sub), Do: it.Do})
	}
	return ms
}

// listPeople are the people a person field offers besides the list's own:
// the sidebar's DM partners.
func (a *App) listPeople() []string {
	m := a.model()
	var out []string
	seen := map[string]bool{}
	for _, s := range m.Sections {
		for _, cid := range s.Rows {
			c := m.conv(cid)
			if c == nil || c.Kind != "im" || c.User == "" || seen[c.User] {
				continue
			}
			seen[c.User] = true
			out = append(out, c.User)
		}
	}
	return out
}

// OpenList shows a list in the side panel, as Slack does for lists opened
// from a message, a link or a DM.
func (a *App) OpenList(id string) {
	if a.secondary.kind != secNone && !a.secondary.hidden {
		if v, ok := a.secondary.view.(*listview.View); ok && v.ID() == id {
			return
		}
	}
	w := a.secondary.width
	if w < 520 {
		w = 520
	}
	a.secondary = secondaryState{kind: secDetails, channel: a.router.Current().Channel(), width: w}
	a.secondary.view = a.newListView(id, listview.Side)
	a.Invalidate()
}

// OpenListFull shows a list in the main view.
func (a *App) OpenListFull(id string) { a.Navigate("lists/" + id) }

// OpenListItem shows an item's pane in the side panel (Slack's
// ListRecordDetails secondary view).
func (a *App) OpenListItem(listID, recID string) {
	if a.secondary.kind != secNone && !a.secondary.hidden {
		if v, ok := a.secondary.view.(*listview.Item); ok && v.ListID() == listID && v.RecordID() == recID {
			return
		}
	}
	w := a.secondary.width
	if w < 440 {
		w = 440
	}
	act := a.listActions()
	act.Close = a.CloseSecondary
	a.secondary = secondaryState{kind: secDetails, channel: a.router.Current().Channel(), width: w}
	a.secondary.view = listview.NewItem(a, listID, recID, act)
	a.Invalidate()
}

// OpenListLink opens a list link natively; false if url isn't one.
func (a *App) OpenListLink(raw string) bool {
	id, rec := ListLinkID(raw)
	if id == "" {
		return false
	}
	if rec != "" {
		a.OpenListItem(id, rec)
		return true
	}
	a.OpenList(id)
	return true
}

// listTabView is the body of ch's selected tab when it is a list.
func (a *App) listTabView(ch string) View {
	i := a.chTab[ch]
	if i <= 0 {
		return nil
	}
	tabs := a.channelTabs(ch)
	if i >= len(tabs) || tabs[i].icon != "list-tab" || tabs[i].fileID == "" {
		return nil
	}
	m, _ := listTabs.Load(a)
	views, _ := m.(map[string]*listview.View)
	if views == nil {
		views = map[string]*listview.View{}
		listTabs.Store(a, views)
	}
	key := ch + "/" + tabs[i].fileID
	v := views[key]
	if v == nil {
		v = a.newListView(tabs[i].fileID, listview.Tab)
		// lists_channel_tabs_default_view (mwX5 Wy): a list opened from its
		// channel tab starts on the tab's default view (K7k9's initial view,
		// listsChannelTabDefaultRefineViewId)
		if c := a.Core(); c != nil && tabs[i].defaultView != "" && c.Experiment("lists_channel_tabs_default_view") == "on" {
			v.SelectView(tabs[i].defaultView)
		}
		views[key] = v
	}
	return v
}

// CreateList is the Create menu's List and Files' "+ New List": a blank
// list (lists.create, Slack's "Untitled list"), opened in full view.
func (a *App) CreateList() { a.createList("") }

func (a *App) createList(template string) {
	c := a.Core()
	if c == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		id, err := c.CreateList(ctx, "", template)
		a.Post(func() {
			if err != nil {
				a.Toast("Couldn't create the list: " + err.Error())
				return
			}
			a.OpenListFull(id)
		})
	}()
}

// NewListMenu is Files → Lists' "+ New List": a blank list or one of
// Slack's templates (lists.templates).
func (a *App) NewListMenu() {
	c := a.Core()
	if c == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		tpls, err := c.ListTemplates(ctx)
		a.Post(func() {
			items := []MenuItem{{Label: "Blank list", Icon: "lists", Do: a.CreateList}}
			if err == nil && len(tpls) > 0 {
				items = append(items, MenuItem{Divider: true}, MenuItem{Label: i18n.T("tabbed-channel", "Templates"), Header: true})
				for _, t := range tpls {
					t := t
					items = append(items, MenuItem{Label: t.Name, Subtitle: t.Description, Icon: "list-view", Do: func() { a.createList(t.ID) }})
				}
			}
			a.menu(a.PointerPos(), items)
		})
	}()
}

// AddMessageToList is a message's "Add to list…": Slack's
// createActionAddToList opens AddToListDialog (listview.NewAddToList).
func (a *App) AddMessageToList(m core.Message) {
	if a.Core() == nil || m.Channel == "" || m.TS == "" {
		return
	}
	a.ShowOverlay(listview.NewAddToList(a, m.Channel, m.TS, a.listAddOpts("")))
}

// addListTab is the channel tab bar's "+" > List: Slack's
// useCreateListTab opens the ListPicker (source "channel-tab").
func (a *App) addListTab(ch string) {
	if a.Core() == nil {
		return
	}
	a.ShowOverlay(listview.NewListPicker(a, ch, a.listAddOpts(ch)))
}

func (a *App) listAddOpts(ch string) listview.AddOpts {
	o := listview.AddOpts{Post: a.Post, OpenItem: a.OpenListItem, OpenList: a.OpenListFull, Menu: a.listActions().Menu}
	if ch == "" {
		return o
	}
	tabOf := func(id string) int {
		for i, t := range a.channelTabs(ch) {
			if t.fileID == id {
				return i
			}
		}
		return -1
	}
	o.HasTab = func(id string) bool { return tabOf(id) >= 0 }
	o.ShowTab = func(id string) {
		if i := tabOf(id); i >= 0 {
			if a.chTab == nil {
				a.chTab = map[string]int{}
			}
			a.chTab[ch] = i
			a.Invalidate()
			return
		}
		if a.data != nil { // the new tab: reload the tab bar, then select it
			delete(a.data.fetched, ch)
			a.data.opened(ch)
		}
		a.pendingCanvasTab(ch, id)
	}
	return o
}
