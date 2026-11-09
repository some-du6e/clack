package ui

import (
	"encoding/json"
	"image"
	"slices"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Navigation's tab list is slack-kit's ReorderableOptionsList (OkAw,
// shared-js_lib_slack-kit_src_components_reorderable-options-list): a
// multiselect listbox whose rows have a drag handle, a checkbox, the tab's
// icon and label. The active option follows the pointer (onMouseEnter) and
// the arrow keys; a click or Space/Enter toggles it unless it's disabled;
// an action bar with Move up / Move down (arrow-up / arrow-down) floats at
// the active row while the list is hovered or focused, and Alt+↑/↓ do the
// same. Dragging a row onto another moves it to that row's index
// (onOptionReordered: splice out, splice in), with a drop line on the
// edge it lands on. TabRail's onChange writes the new order with
// setTabRailCustomizationOrder (the listbox's ids) or the toggled tab with
// setTabRailCustomizationByTabName.

// reorderList is the listbox's state on the dialog.
type reorderList struct {
	active  string // the active option's id ("": the first)
	hovered bool   // pointer inside the list
	focused bool

	press    string // option under the press
	pressY   float32
	dragging bool
	dragY    float32 // pointer y (list px) while dragging
}

// reorderOption is one row.
type reorderOption struct {
	id, label, icon string
	selected        bool
	disabled        bool
}

// reorderMove is onOptionReordered: from moves to to's index.
func reorderMove(ids []string, from, to string) []string {
	i, j := slices.Index(ids, from), slices.Index(ids, to)
	if i < 0 || j < 0 || i == j {
		return ids
	}
	out := slices.Clone(ids)
	out = slices.Delete(out, i, i+1)
	return slices.Insert(out, j, from)
}

// navTabs is the listbox's options: the available tabs, the order just
// written shown until it lands.
func (p *prefsModal) navTabs() []reorderOption {
	c := p.a.Core()
	if c == nil {
		return nil
	}
	r := c.Rail()
	cust := c.RailCustomization()
	ids := r.Available
	if v, ok := p.pending("tab_rail_customization_order"); ok {
		if o, ok := v.([]string); ok && len(o) == len(ids) {
			ids = o
		}
	}
	out := make([]reorderOption, 0, len(ids))
	for _, t := range ids {
		forced := core.RailForced(t)
		on := forced || cust[t]
		if v, ok := p.pending("rail:" + t); ok {
			on, _ = v.(bool)
		}
		rt := p.a.railTabFor(t)
		out = append(out, reorderOption{id: t, label: r.Tabs[t].Label, icon: rt.icon, selected: on, disabled: forced})
	}
	return out
}

// setRailOrder is setTabRailCustomizationOrder with the listbox's order.
func (p *prefsModal) setRailOrder(order []string) {
	j, _ := json.Marshal(order)
	done := p.hold("tab_rail_customization_order", order)
	p.a.setSlackPref("tab_rail_customization_order", string(j), done)
}

func reorderIDs(opts []reorderOption) []string {
	ids := make([]string, len(opts))
	for i, o := range opts {
		ids[i] = o.id
	}
	return ids
}

// moveActive is the action bar's Move up (d=-1) / Move down (d=1).
func (p *prefsModal) moveActive(opts []reorderOption, d int) {
	i := p.activeIndex(opts)
	if i < 0 || i+d < 0 || i+d >= len(opts) {
		return
	}
	ids := reorderIDs(opts)
	p.setRailOrder(reorderMove(ids, ids[i], ids[i+d]))
}

func (p *prefsModal) activeIndex(opts []reorderOption) int {
	if len(opts) == 0 {
		return -1
	}
	for i, o := range opts {
		if o.id == p.nav.active {
			return i
		}
	}
	return 0 // useState(options[0].id)
}

func (p *prefsModal) toggleOption(o reorderOption) {
	if o.disabled {
		return
	}
	p.setRailTab(o.id, !o.selected)
}

// reorderList draws the listbox and handles its pointer and keys.
func (f *prefsForm) reorderList(opts []reorderOption) {
	gtx, p, a := f.gtx, f.p, f.p.a
	c := &a.th.C
	l := &p.nav
	rowH, pad := gtx.Dp(36), gtx.Dp(4)
	n := len(opts)
	listH := n*rowH + 2*pad
	act := p.activeIndex(opts)
	for _, o := range opts {
		f.record("check", "rail."+o.id, o.label, o.selected, !o.disabled)
	}
	rowAt := func(y float32) int {
		i := int(y-float32(pad)) / rowH
		return min(max(i, 0), n-1)
	}

	// pointer and keys (the list's tag; events from last frame's areas)
	for {
		ev, ok := gtx.Event(
			pointer.Filter{Target: l, Kinds: pointer.Press | pointer.Release | pointer.Drag | pointer.Move | pointer.Enter | pointer.Leave | pointer.Cancel},
			key.FocusFilter{Target: l},
			key.Filter{Focus: l, Name: key.NameUpArrow, Optional: key.ModAlt},
			key.Filter{Focus: l, Name: key.NameDownArrow, Optional: key.ModAlt},
			key.Filter{Focus: l, Name: key.NameHome},
			key.Filter{Focus: l, Name: key.NameEnd},
			key.Filter{Focus: l, Name: key.NameSpace},
			key.Filter{Focus: l, Name: key.NameReturn},
			key.Filter{Focus: l, Name: key.NameEnter},
		)
		if !ok || n == 0 {
			break
		}
		switch e := ev.(type) {
		case key.FocusEvent:
			l.focused = e.Focus
		case key.Event:
			if e.State != key.Press {
				continue
			}
			switch e.Name {
			case key.NameUpArrow, key.NameDownArrow:
				d := 1
				if e.Name == key.NameUpArrow {
					d = -1
				}
				if e.Modifiers.Contain(key.ModAlt) {
					p.moveActive(opts, d)
				} else if j := act + d; j >= 0 && j < n {
					l.active = opts[j].id
				}
			case key.NameHome:
				l.active = opts[0].id
			case key.NameEnd:
				l.active = opts[n-1].id
			default: // Space, Enter
				p.toggleOption(opts[act])
			}
			a.Invalidate()
		case pointer.Event:
			switch e.Kind {
			case pointer.Enter, pointer.Move:
				l.hovered = true
				if !l.dragging {
					l.active = opts[rowAt(e.Position.Y)].id
				}
			case pointer.Leave:
				l.hovered = false
			case pointer.Press:
				l.press, l.pressY, l.dragging = opts[rowAt(e.Position.Y)].id, e.Position.Y, false
				gtx.Execute(key.FocusCmd{Tag: l})
			case pointer.Drag:
				if l.press != "" && (l.dragging || abs32(e.Position.Y-l.pressY) > float32(gtx.Dp(4))) {
					l.dragging, l.dragY = true, e.Position.Y
				}
			case pointer.Release:
				if l.press != "" {
					to := opts[rowAt(e.Position.Y)].id
					if l.dragging {
						if to != l.press { // drop: onOptionReordered(dragged, target)
							p.setRailOrder(reorderMove(reorderIDs(opts), l.press, to))
						}
					} else if to == l.press {
						for _, o := range opts {
							if o.id == to {
								p.toggleOption(o)
							}
						}
					}
				}
				l.press, l.dragging = "", false
			case pointer.Cancel:
				l.press, l.dragging = "", false
			}
			a.Invalidate()
		}
	}
	act = p.activeIndex(opts)

	at(gtx, image.Pt(0, f.y), image.Pt(f.w, listH), func(gtx layout.Context) layout.Dimensions {
		r := image.Rectangle{Max: image.Pt(f.w, listH)}
		theme.RRect(gtx, r, gtx.Dp(8), c.Bg)
		bc := c.BorderStrong
		if l.focused {
			bc = c.Link
		}
		theme.Border(gtx, r, gtx.Dp(8), gtx.Dp(1), bc)
		dropAt := -1
		if l.dragging {
			dropAt = rowAt(l.dragY)
		}
		for i, o := range opts {
			y := pad + i*rowH
			if i == act && (l.hovered || l.focused) {
				theme.RRect(gtx, image.Rect(gtx.Dp(4), y, f.w-gtx.Dp(4), y+rowH), gtx.Dp(6), c.BgHover)
			}
			fg := c.Fg
			if o.disabled {
				fg = c.FgSubtle
			}
			if i == act && (l.hovered || l.focused) || (l.dragging && o.id == l.press) {
				at(gtx, image.Pt(gtx.Dp(8), y+(rowH-gtx.Dp(20))/2), image.Pt(gtx.Dp(20), gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
					return icons.Get("reorder")(gtx, 20, c.FgMuted)
				})
			}
			// checkbox
			s := gtx.Dp(18)
			cb := image.Rect(gtx.Dp(32), y+(rowH-s)/2, gtx.Dp(32)+s, y+(rowH-s)/2+s)
			fill := c.Link
			if o.disabled {
				fill = c.FgSubtle
			}
			if o.selected {
				theme.RRect(gtx, cb, gtx.Dp(4), fill)
				at(gtx, cb.Min, image.Pt(s, s), func(gtx layout.Context) layout.Dimensions {
					return icons.Get("check")(gtx, 18, c.SendText)
				})
			} else {
				theme.RRect(gtx, cb, gtx.Dp(4), c.Bg)
				theme.Border(gtx, cb, gtx.Dp(4), gtx.Dp(1), c.BorderStrong)
			}
			x := gtx.Dp(60)
			if o.icon != "" {
				at(gtx, image.Pt(x, y+(rowH-gtx.Dp(20))/2), image.Pt(gtx.Dp(20), gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
					return icons.Get(o.icon)(gtx, 20, fg)
				})
				x += gtx.Dp(28)
			}
			at(gtx, image.Pt(x, y), image.Pt(f.w-x-gtx.Dp(80), rowH), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, rowH, func(gtx layout.Context) layout.Dimensions {
					return a.th.Label(gtx, 15, theme.Regular, fg, o.label)
				})
			})
			if dropAt == i && o.id != l.press { // DropIndicator: top when moving up, bottom when down
				from := slices.Index(reorderIDs(opts), l.press)
				ly := y
				if from < i {
					ly = y + rowH
				}
				theme.Fill(gtx, image.Rect(gtx.Dp(4), ly-gtx.Dp(1), f.w-gtx.Dp(4), ly+gtx.Dp(1)), c.Link)
			}
		}
		area := clip.Rect(r).Push(gtx.Ops)
		event.Op(gtx.Ops, l)
		cur := pointer.CursorPointer
		if l.dragging {
			cur = pointer.CursorGrabbing
		}
		cur.Add(gtx.Ops)
		area.Pop()
		return layout.Dimensions{Size: r.Max}
	})

	// ContextualActions: Move up / Move down at the active row
	up, down := p.btn("button.rail.up"), p.btn("button.rail.down")
	show := (l.hovered || l.focused || up.Hovered() || down.Hovered()) && act >= 0
	f.record("button", "button.rail.up", "Move up", false, show && act > 0)
	f.record("button", "button.rail.down", "Move down", false, show && act < n-1)
	if show {
		bs := gtx.Dp(28)
		y := f.y + pad + act*rowH + (rowH-bs)/2
		for k, b := range []struct {
			key, icon string
			ok        bool
			d         int
		}{{"rail.up", "arrow-up", act > 0, -1}, {"rail.down", "arrow-down", act < n-1, 1}} {
			btn := up
			if b.d > 0 {
				btn = down
			}
			col := c.Fg
			if !b.ok {
				col = c.FgSubtle
			}
			at(gtx, image.Pt(f.w-gtx.Dp(8)-(2-k)*(bs+gtx.Dp(2)), y), image.Pt(bs, bs), func(gtx layout.Context) layout.Dimensions {
				return iconButton(gtx, btn, b.icon, 28, 16, col, c.BgHover, 6)
			})
			if clicked(btn) && b.ok {
				p.moveActive(opts, b.d)
				a.Invalidate()
			}
		}
	}
	f.y += listH + gtx.Dp(12)
}
