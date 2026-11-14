package views

// The Manage tabs card's rows (ManageChannelTabs, the manage-channel-tabs
// chunk Wg5J, CDN build 0e695dd18302): every tab in the channel's order
// (properties.tabs, hidden built-ins too, core.TabOrder) after Messages,
// each 46 tall with its label; "Hidden" at the right for a hidden one
// (list_item_description, hidden while the row is hovered). With tab
// editing (useCanEditTabs, core.CanEditTabs) a hovered row shows its
// actions (list_item_actions, small icon buttons with 18px icons):
//
//   - visibility, for an editable system tab (isEditableSystemTab: Files
//     & links, Workflows, Pins): eye-open "Hide tab" (removeTab:
//     conversations.removeTab) or eye-closed "Show tab" on a hidden one
//     (addTabToChannel: conversations.addTab {type}); hovering the button
//     swaps the eye; clicks are debounced 300 ms;
//   - Move tab up / Move tab down (arrow-up / arrow-down), aria-disabled
//     where the neighbour can't trade places (canMoveUp / canMoveDown:
//     both shown, editable and in the channel's tabs; Messages never
//     moves): updateTabOrder, conversations.moveTab {channel_id, id,
//     after: the last tab before its new place}.
//
//   - Rename tab (edit) for a canvas or folder tab (not the Channel
//     Expert's): RenameTabModal without isRenameFolder ("Rename tab", "Tab
//     name"), views/tab_edit.go;
//   - for an editable tab that isn't a system one (a canvas, list or
//     folder) the visibility slot is trash "Delete tab": useRemoveTab with
//     its confirmations (tab_edit.go).
//
// The actions show when useCanEditTabs (core.CanEditTabs, read each frame:
// it fills in when the grant answers) and the tab is editable or movable.
// A failed write shows "Something’s gone wrong. Mind trying again in a
// moment?" under the list.

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
)

type cdManageTabs struct {
	order   async[[]core.TabState]
	clk     map[string]*widget.Clickable
	visAt   map[string]time.Time // the visibility buttons' last runs (debounce)
	errored atomic.Bool          // a write failed (the error alert)
}

var cdManage sync.Map // *ChannelDetails -> *cdManageTabs

func (d *ChannelDetails) manageTabs() *cdManageTabs {
	if v, ok := cdManage.Load(d); ok {
		return v.(*cdManageTabs)
	}
	m := &cdManageTabs{clk: map[string]*widget.Clickable{}, visAt: map[string]time.Time{}}
	cdManage.Store(d, m)
	return m
}

// isEditableSystemTab's types among the tabs Clack draws.
var cdSystemTabs = map[string]bool{"files": true, "workflows": true, "pins": true, "bookmarks": true}

// cdTabTitle is a tab's label in the card.
func cdTabTitle(t core.TabState, titles map[string]string) string {
	if s := titles[t.ID]; s != "" {
		return s
	}
	switch t.Type {
	case "files":
		return "Files & links"
	case "workflows":
		return "Workflows"
	case "pins", "bookmarks":
		return "Pins"
	}
	return dirFirst(t.Label, t.Type)
}

func cdTabIcon(typ string) string {
	switch typ {
	case "channel":
		return "dms" // Messages (cdTabRows)
	case "canvas", "channel_canvas", "list":
		return "canvas"
	case "files":
		return "canvas-browser"
	case "bookmarks", "pins":
		return "pin"
	case "folder":
		return "bookmark"
	case "workflows":
		return "workflow"
	}
	return "link"
}

// manageOrder is the card's tabs: Messages, then the channel's.
func (m *cdManageTabs) manageOrder() []core.TabState {
	o, _, _ := m.order.get()
	return append([]core.TabState{{ID: "channel", Type: "channel", Label: "Messages"}}, o...)
}

// movable is ManageChannelTabs' A: shown, editable and in the tabs.
func cdMovable(t core.TabState) bool { return !t.Disabled && !core.StaticTab(t) }

// manageTabRows are the card's tab rows once the order has loaded (nil
// before: the card keeps its static rows).
func (d *ChannelDetails) manageTabRows(titles map[string]string) []W {
	m := d.manageTabs()
	c, id := d.h.Core(), d.id
	if c == nil {
		return nil
	}
	if !m.order.started() {
		m.order.load(d.h, false, func(ctx context.Context) ([]core.TabState, error) { return c.TabOrder(ctx, id) })
	}
	if _, ok, err := m.order.get(); !ok || err != nil {
		return nil
	}
	can := c.CanEditTabs(id)
	order := m.manageOrder()
	var rows []W
	for i := range order {
		i := i
		rows = append(rows, func(gtx C) D { return d.manageTabRow(gtx, m, order, i, titles, can) })
	}
	if m.errored.Load() {
		rows = append(rows, func(gtx C) D {
			return layout.Inset{Left: 20, Right: 20, Bottom: 8}.Layout(gtx, func(gtx C) D {
				return d.para(gtx, 15, 22, wReg, d.p.Red, 0, nsChannelTabs.T("Something’s gone wrong. Mind trying again in a moment?"))
			})
		})
	}
	return rows
}

func (d *ChannelDetails) manageTabRow(gtx C, m *cdManageTabs, order []core.TabState, i int, titles map[string]string, can bool) D {
	t := order[i]
	row0 := dirClick(m.clk, "row:"+t.ID)
	vis, up, down := dirClick(m.clk, "vis:"+t.ID), dirClick(m.clk, "up:"+t.ID), dirClick(m.clk, "down:"+t.ID)
	ren := dirClick(m.clk, "rename:"+t.ID)
	sys := core.EditableSystemTab(t.Type)
	trash := core.EditableTab(t.Type) && !sys                              // the slot's trash "Delete tab"
	rename := (t.Type == "canvas" || t.Type == "folder") && !d.agentTab(t) // Q
	if ren.Clicked(gtx) && can && rename {
		OpenRenameTab(d.h, d.id, d.editTab(t, titles), false)
	}
	if vis.Clicked(gtx) && can && trash && gtx.Now.Sub(m.visAt[t.ID]) > 300*time.Millisecond {
		m.visAt[t.ID] = gtx.Now
		RemoveTab(d.h, d.id, d.editTab(t, titles))
	}
	canUp := i > 0 && cdMovable(t) && cdMovable(order[i-1])
	canDown := i+1 < len(order) && cdMovable(t) && cdMovable(order[i+1])
	if vis.Clicked(gtx) && sys && cdSystemTabs[t.Type] && gtx.Now.Sub(m.visAt[t.ID]) > 300*time.Millisecond {
		m.visAt[t.ID] = gtx.Now
		d.toggleTabVisible(m, t)
	}
	if up.Clicked(gtx) && canUp {
		d.moveTab(m, order, i, i-1)
	}
	if down.Clicked(gtx) && canDown {
		d.moveTab(m, order, i, i+1)
	}
	actions := can && (core.EditableTab(t.Type) || cdMovable(t)) && !core.StaticTab(t)
	return row0.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.Y = gtx.Dp(46)
		hover := row0.Hovered() || vis.Hovered() || up.Hovered() || down.Hovered() || ren.Hovered()
		return layout.Inset{Left: 28, Right: 32}.Layout(gtx, func(gtx C) D {
			return layout.Center.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				colr := d.p.Text
				if t.Disabled {
					colr = d.p.Muted
				}
				return row(gtx, layout.Middle,
					layout.Rigid(func(gtx C) D { return d.icon(gtx, cdTabIcon(t.Type), 16, colr) }),
					hgap(4),
					layout.Flexed(1, func(gtx C) D {
						return dirFill(gtx, func(gtx C) D { return d.label(gtx, 15, wReg, colr, cdTabTitle(t, titles)) })
					}),
					layout.Rigid(func(gtx C) D {
						if !(actions && hover) {
							if t.Disabled {
								return d.label(gtx, 15, wReg, d.p.Muted, nsChannelTabs.T("Hidden"))
							}
							return D{}
						}
						var ch []layout.FlexChild
						if rename {
							ch = append(ch, layout.Rigid(func(gtx C) D { return d.iconButton(gtx, ren, "edit", 28, 18, d.p.Sec) }))
						}
						if trash {
							ch = append(ch, layout.Rigid(func(gtx C) D { return d.iconButton(gtx, vis, "trash", 28, 18, d.p.Sec) }))
						}
						if sys && cdSystemTabs[t.Type] {
							eye := "eye-open" // "Hide tab"
							if t.Disabled {
								eye = "eye-closed" // "Show tab"
							}
							if vis.Hovered() { // the button previews the other state
								if eye == "eye-open" {
									eye = "eye-closed"
								} else {
									eye = "eye-open"
								}
							}
							ch = append(ch, layout.Rigid(func(gtx C) D { return d.iconButton(gtx, vis, eye, 28, 18, d.p.Sec) }))
						}
						arrow := func(c *widget.Clickable, name string, on bool) layout.FlexChild {
							return layout.Rigid(func(gtx C) D {
								colr := d.p.Sec
								if !on {
									colr = d.p.Faint
								}
								return d.iconButton(gtx, c, name, 28, 18, colr)
							})
						}
						ch = append(ch, arrow(up, "arrow-up", canUp), arrow(down, "arrow-down", canDown))
						return row(gtx, layout.Middle, ch...)
					}),
				)
			})
		})
	})
}

// toggleTabVisible is the visibility button: Show tab (addTabToChannel)
// on a hidden built-in, else Hide tab (removeTab).
func (d *ChannelDetails) toggleTabVisible(m *cdManageTabs, t core.TabState) {
	c, id := d.h.Core(), d.id
	m.errored.Store(false)
	m.order.mutate(func(o *[]core.TabState) { // the row's state at once (j)
		for i := range *o {
			if (*o)[i].ID == t.ID {
				(*o)[i].Disabled = !t.Disabled
			}
		}
	})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		var err error
		if t.Disabled {
			err = c.ShowTab(ctx, id, t.Type)
		} else {
			err = c.HideTab(ctx, id, t.ID)
		}
		if err != nil {
			m.errored.Store(true)
		}
		m.order.load(d.h, true, func(ctx context.Context) ([]core.TabState, error) { return c.TabOrder(ctx, id) })
	}()
}

// moveTab is updateTabOrder from the card: the row goes to its new place
// at once, conversations.moveTab goes out, and the answer's order (or a
// re-read after a failure) replaces it.
func (d *ChannelDetails) moveTab(m *cdManageTabs, order []core.TabState, from, to int) {
	tid, after, ok := core.MoveTabAfter(order, from, to)
	if !ok {
		return
	}
	c, id := d.h.Core(), d.id
	m.errored.Store(false)
	m.order.mutate(func(o *[]core.TabState) {
		f, t := from-1, to-1 // without Messages
		if f < 0 || t < 0 || f >= len(*o) || t >= len(*o) {
			return
		}
		x := (*o)[f]
		*o = append((*o)[:f], (*o)[f+1:]...)
		*o = append((*o)[:t], append([]core.TabState{x}, (*o)[t:]...)...)
	})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, err := c.MoveTab(ctx, id, tid, after)
		if err != nil {
			m.errored.Store(true)
		}
		m.order.load(d.h, true, func(ctx context.Context) ([]core.TabState, error) { return c.TabOrder(ctx, id) })
	}()
}

// editTab is the card's row as the rename and remove flows take it.
func (d *ChannelDetails) editTab(t core.TabState, titles map[string]string) EditTab {
	e := EditTab{ID: t.ID, Type: t.Type, Label: t.Label, FileID: t.FileID, FolderID: t.FolderID}
	if t.FileID != "" {
		e.FileTitle = titles[t.ID]
	}
	return e
}

// agentTab is K: the channel's Channel Expert folder tab (no rename).
func (d *ChannelDetails) agentTab(t core.TabState) bool {
	c := d.h.Core()
	return c != nil && t.Type == "folder" && c.IsKnowledgeFolder(d.id, t.AppID, t.FolderID)
}
