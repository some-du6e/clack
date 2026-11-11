package views

// Keyboard navigation, as Slack's Activity inbox has it (gantry
// client-boot-render, webapp build a84662d2):
//
//   - The list is Slack's virtual list (U5s4 ActivityList, g.A with
//     useGlobalKeyboardShortcuts): Up and Down move the focus between the
//     rows (isFocusableItem: activity items, not day dividers or
//     headers); Enter or Right opens the focused row (onItemKeyDown →
//     handleOnClickActivityItem). A focused row shows its actions
//     (hasFocus) and, in keyboard mode, the focus ring
//     (p-activity_ia4_list_container--keyboard_mode in the bordered list:
//     4px in, square, the Detailed row's bottom 4px, the Dense row's 0).
//   - The focused row's keys (WbG1 useActivityItemShortcuts, activity
//     inbox): U marks it read or unread, C clears it (restores it in
//     Cleared items), X picks it for bulk actions, Space too in triage
//     mode, A is Save for later / Add to-do, M the custom reminder, F
//     shares (forwards) it; in triage mode Alt+U marks the picked rows
//     read and Alt+C marks them read and clears them.
//   - The page's keys (N8Zu ClientKeyboardShortcutsActivity): Alt+Up /
//     Alt+Down open the previous / next item, Shift+Alt+Up / Down the
//     previous / next unread one, ⌘. closes the detail pane, ⌘Z undoes
//     the last clear or read change. Clack takes them while the list has
//     the focus.

import (
	"context"
	"image"
	"strconv"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/op/clip"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// actKbTag is the list's key focus target (not zero-sized: its address
// must be its own).
type actKbTag struct{ _ byte }

// actUndo is the last change ⌘Z takes back (P0K2 undoableActivityItem):
// a clear, or a read state.
type actUndo struct {
	kind string // "archive", "read", "unread"
	item core.ActivityItem
	key  string
}

// listFocusArea registers the list as the key focus target over r: the
// rows draw inside it (pop the stack after them), so a press on a row
// reaches it too and focuses the list (leaving keyboard mode).
func (v *Activity) listFocusArea(gtx C, r image.Rectangle) clip.Stack {
	area := clip.Rect(r).Push(gtx.Ops)
	event.Op(gtx.Ops, &v.kbTag)
	return area
}

// listFocusEvents handles the presses listFocusArea saw.
func (v *Activity) listFocusEvents(gtx C) {
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &v.kbTag, Kinds: pointer.Press})
		if !ok {
			break
		}
		if _, ok := ev.(pointer.Event); ok {
			v.kbMode = false
			if !gtx.Focused(&v.kbTag) {
				gtx.Execute(key.FocusCmd{Tag: &v.kbTag})
			}
		}
	}
}

// keys handles the list's keys.
func (v *Activity) keys(gtx C) {
	t := &v.kbTag
	for {
		ev, ok := gtx.Event(
			key.FocusFilter{Target: t},
			key.Filter{Focus: t, Name: key.NameUpArrow, Optional: key.ModAlt | key.ModShift},
			key.Filter{Focus: t, Name: key.NameDownArrow, Optional: key.ModAlt | key.ModShift},
			key.Filter{Focus: t, Name: key.NameRightArrow},
			key.Filter{Focus: t, Name: key.NameReturn},
			key.Filter{Focus: t, Name: key.NameEnter},
			key.Filter{Focus: t, Name: key.NameSpace},
			key.Filter{Focus: t, Name: "U", Optional: key.ModAlt},
			key.Filter{Focus: t, Name: "C", Optional: key.ModAlt},
			key.Filter{Focus: t, Name: "X"},
			key.Filter{Focus: t, Name: "A"},
			key.Filter{Focus: t, Name: "M"},
			key.Filter{Focus: t, Name: "F"},
			key.Filter{Focus: t, Name: "Z", Required: key.ModShortcut},
			key.Filter{Focus: t, Name: ".", Required: key.ModShortcut},
		)
		if !ok {
			return
		}
		switch e := ev.(type) {
		case key.FocusEvent:
			v.kbFocused = e.Focus
		case key.Event:
			if e.State == key.Press {
				v.key(gtx, e)
			}
		}
	}
}

// focusable are the list's rows the focus moves through: activity items
// (v.vis without the day dividers), by key, in order.
func (v *Activity) focusable() []string { return v.listKeys() }

// focusedItem is the focused row's item.
func (v *Activity) focusedItem() (*core.ActivityItem, string) {
	if v.focusKey == "" {
		return nil, ""
	}
	page, _, _ := v.data.get()
	for i := range page.Items {
		if k := actKey(&page.Items[i]); k == v.focusKey {
			for _, j := range v.vis {
				if j == i {
					return &page.Items[i], k
				}
			}
		}
	}
	return nil, ""
}

func (v *Activity) key(gtx C, e key.Event) {
	alt, shift := e.Modifiers.Contain(key.ModAlt), e.Modifiers.Contain(key.ModShift)
	switch e.Name {
	case key.NameUpArrow, key.NameDownArrow:
		d := 1
		if e.Name == key.NameUpArrow {
			d = -1
		}
		switch {
		case alt && shift:
			v.openStep(d, true)
		case alt:
			v.openStep(d, false)
		case !shift:
			v.moveFocus(d)
		}
		v.kbMode = true
	case key.NameRightArrow, key.NameReturn, key.NameEnter:
		if it, k := v.focusedItem(); it != nil {
			v.choose(it, k, v.rowFor(k))
		}
	case "U":
		if alt {
			v.bulkKeyRead(false)
			break
		}
		if it, k := v.focusedItem(); it != nil {
			if v.isUnread(it, k) {
				v.undoable("read", it, k)
				v.markRead(it, k)
			} else {
				v.undoable("unread", it, k)
				v.markUnread(it, k)
			}
		}
	case "C":
		if alt {
			v.bulkKeyRead(true)
			break
		}
		if it, k := v.focusedItem(); it != nil {
			if v.showCleared {
				v.unclear(it, k)
			} else {
				v.undoable("archive", it, k)
				v.clear(it, k)
			}
		}
	case "X":
		if _, k := v.focusedItem(); k != "" {
			v.pickRow(k, false)
		}
	case key.NameSpace:
		// ignoreIf !triage
		if _, k := v.focusedItem(); k != "" && v.triage() {
			v.pickRow(k, false)
		}
	case "A":
		v.runItemKey("save_for_later")
	case "M":
		if it, _ := v.focusedItem(); it != nil {
			if ph, ok := v.h.(popups.Host); ok && it.Channel != "" && it.TS != "" {
				ch, ts := it.Channel, it.TS
				if it.Msg != nil {
					ch, ts = firstNonEmpty(it.Msg.Channel, ch), it.Msg.TS
				}
				ph.ShowOverlay(popups.NewReminderModal(ph, ch, ts))
			}
		}
	case "F":
		if it, _ := v.focusedItem(); it != nil && it.Msg != nil {
			if ph, ok := v.h.(popups.Host); ok {
				ph.ShowOverlay(popups.NewForwardModal(ph, *it.Msg))
			}
		}
	case "Z":
		v.undoLast()
	case ".":
		v.selOK = false
	}
	v.dirty = true
	v.h.Invalidate()
}

// moveFocus moves the focused row by d through the rows and scrolls it
// into view; the first press lands on the opened row, else the first.
func (v *Activity) moveFocus(d int) {
	keys := v.focusable()
	if len(keys) == 0 {
		return
	}
	at := -1
	for i, k := range keys {
		if k == v.focusKey {
			at = i
		}
	}
	if at < 0 && v.selOK {
		for i, k := range keys {
			if k == v.selKey {
				at = i
			}
		}
	}
	switch {
	case at < 0:
		at = 0
	case at+d >= 0 && at+d < len(keys):
		at += d
	}
	v.focusKey = keys[at]
	v.scrollToKey(v.focusKey)
}

// scrollToKey scrolls the list so key's row shows.
func (v *Activity) scrollToKey(k string) {
	page, _, _ := v.data.get()
	for pos, i := range v.vis {
		if i >= 0 && i < len(page.Items) && actKey(&page.Items[i]) == k {
			p := v.list.Position
			if pos < p.First {
				v.list.ScrollTo(pos)
			} else if p.Count > 0 && pos >= p.First+p.Count-1 {
				v.list.ScrollTo(max(0, pos-max(1, p.Count-2)))
			}
			return
		}
	}
}

// openStep is Alt+Up / Alt+Down (and with Shift the unread rows only):
// open the row before or after the opened one (or the focused one),
// the first one when neither is (N8Zu ec / eu / em / ep).
func (v *Activity) openStep(d int, unreadOnly bool) {
	page, _, _ := v.data.get()
	type cand struct {
		it  *core.ActivityItem
		key string
	}
	cur := v.focusKey
	if v.selOK {
		cur = v.selKey
	}
	var list []cand
	for _, i := range v.vis {
		if i < 0 || i >= len(page.Items) {
			continue
		}
		it := &page.Items[i]
		k := actKey(it)
		if !unreadOnly || k == cur || v.isUnread(it, k) {
			list = append(list, cand{it, k})
		}
	}
	if len(list) == 0 {
		return
	}
	at := -1
	for i, c := range list {
		if c.key == cur {
			at = i
		}
	}
	var next cand
	switch {
	case at < 0 && unreadOnly && d < 0:
		next = list[len(list)-1]
	case at < 0:
		next = list[0]
	case at+d >= 0 && at+d < len(list):
		next = list[at+d]
	case unreadOnly && d > 0:
		next = list[0]
	case unreadOnly:
		next = list[len(list)-1]
	default:
		return
	}
	if next.key == cur && v.selOK {
		return
	}
	v.focusKey = next.key
	v.choose(next.it, next.key, v.rowFor(next.key))
	v.scrollToKey(next.key)
}

// bulkKeyRead is Alt+U (mark the picked rows read; the selection stays)
// and Alt+C (read and cleared, the selection ends), in triage mode with
// something picked.
func (v *Activity) bulkKeyRead(clearToo bool) {
	if !v.triage() || len(v.picked) == 0 {
		return
	}
	items, keys := v.pickedItems()
	n := len(items)
	for _, k := range keys {
		v.override[k] = true
		if clearToo {
			v.cleared[k] = true
			if v.selOK && v.selKey == k {
				v.selOK = false
			}
		}
	}
	if clearToo {
		v.endSelect()
	} else {
		noun := "items"
		if n == 1 {
			noun = "item"
		}
		v.h.Toast(strconv.Itoa(n) + " " + noun + " marked as read")
	}
	v.write("Couldn’t update the items: ", func(ctx context.Context, c *core.App) error {
		if err := c.MarkActivityReadBulk(ctx, items...); err != nil || !clearToo {
			return err
		}
		return c.ClearActivityBulk(ctx, items...)
	}, func() {
		for _, k := range keys {
			delete(v.override, k)
			if clearToo {
				delete(v.cleared, k)
			}
		}
		v.dirty = true
	})
}

// runItemKey runs the focused row's item menu row with that key (A:
// save_for_later, the template's shortcut).
func (v *Activity) runItemKey(menuKey string) {
	it, k := v.focusedItem()
	ph, ok := v.h.(popups.Host)
	if it == nil || !ok {
		return
	}
	for _, m := range v.itemMenu(ph, *it, k, v.isUnread(it, k)) {
		if m.Key == menuKey && m.Do != nil {
			m.Do()
			return
		}
	}
}

// undoable records the change ⌘Z takes back.
func (v *Activity) undoable(kind string, it *core.ActivityItem, k string) {
	v.lastUndo = &actUndo{kind: kind, item: *it, key: k}
}

// undoLast is ⌘Z (N8Zu eM): a clear is restored, a read change reverted.
func (v *Activity) undoLast() {
	u := v.lastUndo
	if u == nil {
		return
	}
	v.lastUndo = nil
	switch u.kind {
	case "archive":
		v.undo.until = time.Time{}
		v.unclear(&u.item, u.key)
	case "read":
		v.markUnread(&u.item, u.key)
	case "unread":
		v.markRead(&u.item, u.key)
	}
}

// focusRing draws the keyboard focus ring on a focused row of size sz:
// 4px in on every side but the Dense row's bottom, square corners (the
// bordered list's --activity-focus-ring-*), 1px otl-hgl-1 and its 5px
// halo.
func (v *Activity) focusRing(gtx C, sz image.Point) {
	in := gtx.Dp(4)
	bottom := in
	if v.dense {
		bottom = 0
	}
	r := image.Rect(in, in, sz.X-in, sz.Y-bottom)
	popups.FocusRing(gtx, v.th, r, 0)
}
