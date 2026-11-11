package listview

// The table's keyboard model, ported from Slack's web client (build
// a84662d2d135, modern.vendor; docs/LISTS.md "Keyboard" has the evidence
// per binding):
//
//   - 8dY2 useKeyCommands: the grid's Mousetrap bindings (arrows, Tab,
//     Home/End, PageUp/PageDown, Backspace/Del, Shift+Enter, Alt+Enter,
//     Mod+Shift+↑/↓) and the cut/copy/paste listeners.
//   - 8dY2 GridField onKeyDown: Enter/Space/F2 start editing, typing a
//     printable character starts editing, Escape leaves the cell.
//   - 3VkJ GridView: skipEditMode and onCustomClickCell (what a key does
//     per column type), enableTabbingNavigation (experiment
//     lists_tabbing_navigation and the pref), the column configs'
//     copyValue / pasteValue / deleteValue.
//   - 3VkJ BaseFieldInputRenderer: the editors and nextRowOnSave.
//   - b2eO (the rating editor), mhiL LinkColumn (Enter opens "Link
//     options", ⌘/Super+Enter opens the link), x8/8 and 3VkJ nE (the
//     cells' screen-reader label and description).

import (
	"image"
	"io"
	"strconv"
	"strings"

	"gioui.org/io/clipboard"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/semantic"
	"gioui.org/io/transfer"
	"gioui.org/layout"
	"gioui.org/op/clip"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/lists"
	"github.com/some-du6e/clack/internal/ui/shortcut"
)

// cellRef is a cell by record id and column key (rows move when the list
// changes; Slack's rowIndex is the virtual list's).
type cellRef struct{ rec, col string }

func (c cellRef) ok() bool { return c.rec != "" && c.col != "" }

// PrefListsTabbing is the user pref behind Preferences > Accessibility >
// Lists keyboard interactions ("tab_nav" or "arrow_nav"; unset reads as
// data entry mode).
const PrefListsTabbing = "lists_tabbing_navigation_option_type"

// ExpListsTabbing is the experiment that shows that preference and turns
// on Tab navigation (jPF5 isListsTabbingNavigationEnabled).
const ExpListsTabbing = "lists_tabbing_navigation"

// kbMode is how the grid reads the keyboard for the viewer.
type kbMode struct {
	// tabbing is Slack's enableTabbingNavigation: the experiment is on and
	// the pref is unset or "tab_nav" (3VkJ GridView tg). ↑/↓ then cross
	// group boundaries and Tab can move between cells.
	tabbing bool
	// tabKeys: Tab and Shift+Tab move between cells. useKeyCommands also
	// returns early when the pref was never written (`if (!eP) return`),
	// so only an explicit "tab_nav" moves; otherwise Tab is the browser's.
	tabKeys bool
	// arrowNav is Full keyboard mode ("arrow_nav"): commits stay on the
	// cell (nextRowOnSave: !arrowNav) and Add item is not a tab stop.
	arrowNav bool
}

func (v *View) kbMode() kbMode {
	a := v.h.Core()
	if a == nil {
		return kbMode{}
	}
	pref := a.PrefString(PrefListsTabbing)
	exp := a.Experiment(ExpListsTabbing) == "on"
	m := kbMode{arrowNav: pref == "arrow_nav"}
	m.tabbing = exp && (pref == "" || pref == "tab_nav")
	m.tabKeys = m.tabbing && pref != ""
	return m
}

// metaMod is the key Slack reads as event.metaKey: ⌘ on macOS, the
// Super/Windows key elsewhere.
func metaMod() key.Modifiers {
	if shortcut.Mac() {
		return key.ModCommand
	}
	return key.ModSuper
}

// ---- positions ----

// navRow is the index in g.nav of record rec (-1 none).
func (g *gridState) navRow(rec string) int {
	for i, li := range g.nav {
		if g.lines[li].row.Rec != nil && g.lines[li].row.Rec.ID == rec {
			return i
		}
	}
	return -1
}

func (g *gridState) colIdx(key string) int {
	for i, c := range g.cols {
		if c.c.Key == key {
			return i
		}
	}
	return -1
}

// pos is c's (row, column) this frame; ok is false when it isn't shown.
func (g *gridState) pos(c cellRef) (ni, ci int, ok bool) {
	if !c.ok() {
		return -1, -1, false
	}
	ni, ci = g.navRow(c.rec), g.colIdx(c.col)
	return ni, ci, ni >= 0 && ci >= 0
}

func (g *gridState) cell(ni, ci int) cellRef {
	if ni < 0 || ni >= len(g.nav) || ci < 0 || ci >= len(g.cols) {
		return cellRef{}
	}
	return cellRef{rec: g.lines[g.nav[ni]].row.Rec.ID, col: g.cols[ci].c.Key}
}

// contiguous: rows ni and nj (nj = ni±1) are adjacent lines, with no
// group header or Add item between them (Slack: rowIndex differs by 1).
func (g *gridState) contiguous(ni, nj int) bool {
	if nj < 0 || nj >= len(g.nav) {
		return false
	}
	d := g.nav[nj] - g.nav[ni]
	return d == 1 || d == -1
}

// groupOf is the group of row ni.
func (g *gridState) groupOf(ni int) string { return g.lines[g.nav[ni]].group }

// selection is the selected range (rows and columns, inclusive); multi
// is false for a single cell (Slack paints p-grid_multi_cell only with a
// max).
func (g *gridState) selection() (r0, r1, c0, c1 int, multi, ok bool) {
	a0, b0, okA := g.pos(g.selA)
	if !okA {
		return 0, 0, 0, 0, false, false
	}
	a1, b1, okB := g.pos(g.selB)
	if !okB {
		return a0, a0, b0, b0, false, true
	}
	return min(a0, a1), max(a0, a1), min(b0, b1), max(b0, b1), true, true
}

func (g *gridState) inSelection(ni, ci int) bool {
	r0, r1, c0, c1, multi, ok := g.selection()
	return ok && multi && ni >= r0 && ni <= r1 && ci >= c0 && ci <= c1
}

// wholeRows: the selection spans every column (useKeyCommands eK).
func (g *gridState) wholeRows() bool {
	_, _, c0, c1, multi, ok := g.selection()
	return ok && multi && c0 == 0 && c1+1 == len(g.cols)
}

// ---- state changes ----

// setActive is useKeyCommands' e$: the active cell moves, and the
// selection follows (eY: Shift extends it, except with Tab).
func (v *View) setActive(ni, ci int, extend bool) {
	g := &v.grid
	c := g.cell(ni, ci)
	if !c.ok() {
		return
	}
	selCol := ci
	if g.wholeRows() {
		if _, ac, ok := g.pos(g.active); ok {
			if _, mc, ok2 := g.pos(g.selB); ok2 && ac != mc {
				selCol = mc
			}
		}
	}
	g.active = c
	if extend {
		if !g.selA.ok() {
			g.selA = g.cell(ni, ci)
		}
		g.selB = g.cell(ni, selCol)
	} else {
		g.selA, g.selB = g.cell(ni, selCol), cellRef{}
	}
	g.reveal = true
}

// leave is onFocusLeave (8dY2 aD): the cell stops being active and
// remembers itself for when focus comes back; the selection goes.
func (v *View) leave() {
	g := &v.grid
	if v.edit.active || g.menuOpen || g.rate.active {
		return
	}
	if g.active.ok() {
		g.prev = g.active
	}
	g.active, g.selA, g.selB = cellRef{}, cellRef{}, cellRef{}
}

// enter is the record row's onFocusEnter (8dY2 aO) with no active cell:
// the last active cell, else the first row's first cell.
func (v *View) enter() {
	g := &v.grid
	if g.active.ok() {
		return
	}
	if _, _, ok := g.pos(g.prev); ok {
		g.active, g.selA, g.selB = g.prev, g.prev, cellRef{}
		g.reveal = true
		return
	}
	if len(g.nav) > 0 && len(g.cols) > 0 {
		v.setActive(0, 0, false)
	}
}

// ---- the key handler ----

func kf(tag event.Tag, name key.Name, req, opt key.Modifiers) key.Filter {
	return key.Filter{Focus: tag, Name: name, Required: req, Optional: opt}
}

// tabLeaves reports whether Tab (or Shift+Tab when back) goes to the next
// interactive element instead of a cell: the browser's Tab in Full
// keyboard mode or without the pref, while editing, or at the edge of
// the list or group (useKeyCommands e3/e4 call onFocusLeave there). The
// grid then doesn't ask for the key and Gio moves the focus.
func (v *View) tabLeaves(m kbMode, back bool) bool {
	g := &v.grid
	ni, ci, ok := g.pos(g.active)
	if !ok || v.edit.active || g.menuOpen || g.rate.active || !m.tabKeys {
		return true
	}
	if back {
		return ci == 0 && (ni == 0 || !g.contiguous(ni, ni-1))
	}
	return ci == len(g.cols)-1 && (ni == len(g.nav)-1 || !g.contiguous(ni, ni+1))
}

// gridKeys reads the grid's keyboard events (after the frame's lines are
// known). viewH is the grid's height.
func (v *View) gridKeys(gtx layout.Context, d *core.ListDoc, viewH int) {
	g := &v.grid
	m := v.kbMode()
	if g.refocus {
		g.refocus = false
		gtx.Execute(key.FocusCmd{Tag: &g.tag})
	}
	shortcut.OwnEscape(&g.tag)
	if g.active.ok() {
		if _, _, ok := g.pos(g.active); !ok && !v.edit.active && !g.menuOpen { // its row left the view
			g.active, g.selA, g.selB = cellRef{}, cellRef{}, cellRef{}
		}
	}
	t := &g.tag
	sc := key.ModShortcut
	meta := metaMod()
	fs := []event.Filter{
		key.FocusFilter{Target: t},
		transfer.TargetFilter{Target: t, Type: "application/text"},
		kf(t, key.NameLeftArrow, 0, key.ModShift), kf(t, key.NameRightArrow, 0, key.ModShift),
		kf(t, key.NameUpArrow, 0, key.ModShift), kf(t, key.NameDownArrow, 0, key.ModShift),
		kf(t, key.NameUpArrow, key.ModShift|key.ModCtrl, 0), kf(t, key.NameDownArrow, key.ModShift|key.ModCtrl, 0),
		kf(t, key.NameUpArrow, key.ModShift|meta, 0), kf(t, key.NameDownArrow, key.ModShift|meta, 0),
		kf(t, key.NameHome, 0, 0), kf(t, key.NameEnd, 0, 0),
		kf(t, key.NamePageUp, 0, 0), kf(t, key.NamePageDown, 0, 0),
		kf(t, key.NameReturn, 0, key.ModShift|key.ModAlt|meta), kf(t, key.NameEnter, 0, key.ModShift|key.ModAlt|meta),
		kf(t, key.NameF2, 0, 0), kf(t, key.NameSpace, 0, 0),
		kf(t, key.NameEscape, 0, 0),
		kf(t, key.NameDeleteBackward, 0, 0), kf(t, key.NameDeleteForward, 0, 0),
		kf(t, "C", sc, 0), kf(t, "X", sc, 0), kf(t, "V", sc, 0),
	}
	if g.rate.active { // the range input takes digits
		for c := '0'; c <= '9'; c++ {
			fs = append(fs, kf(t, key.Name(string(c)), 0, 0))
		}
	}
	if !v.tabLeaves(m, false) {
		fs = append(fs, kf(t, key.NameTab, 0, 0))
	}
	if !v.tabLeaves(m, true) {
		fs = append(fs, kf(t, key.NameTab, key.ModShift, 0))
	}
	for {
		ev, ok := gtx.Event(fs...)
		if !ok {
			break
		}
		switch e := ev.(type) {
		case key.FocusEvent:
			g.focused = e.Focus
			if e.Focus {
				g.menuOpen = false
				v.enter()
			} else if g.rate.active {
				v.rateDone(d, true) // the range input's blur saves
				v.leave()
			} else {
				v.leave()
			}
		case key.EditEvent:
			v.typed(gtx, d, m, e.Text)
		case transfer.DataEvent:
			if e.Type == "application/text" {
				r := e.Open()
				b, _ := io.ReadAll(io.LimitReader(r, 1<<20))
				r.Close()
				v.paste(d, string(b))
			}
		case key.Event:
			if e.State != key.Press {
				continue
			}
			g.say = ""
			if g.rate.active {
				v.rateKey(d, e)
				continue
			}
			v.gridKey(gtx, d, m, e, viewH)
		}
	}
}

// gridKey is one key press with the grid focused and no editor open.
func (v *View) gridKey(gtx layout.Context, d *core.ListDoc, m kbMode, e key.Event, viewH int) {
	g := &v.grid
	ni, ci, ok := g.pos(g.active)
	if !ok || v.edit.active || g.menuOpen {
		return // useKeyCommands: `if (q && !Z ...)`
	}
	shift := e.Modifiers.Contain(key.ModShift)
	last := len(g.cols) - 1
	switch e.Name {
	case key.NameLeftArrow: // eQ
		if ci > 0 {
			v.setActive(ni, ci-1, shift)
		}
	case key.NameRightArrow: // eZ
		if ci < last {
			v.setActive(ni, ci+1, shift)
		}
	case key.NameUpArrow:
		if e.Modifiers.Contain(key.ModCtrl) || e.Modifiers.Contain(metaMod()) { // t_: extend to the first row (of the group)
			v.extendTo(ni, -1)
			return
		}
		// eJ: across a group boundary only with tabbing navigation
		if ni > 0 && (g.contiguous(ni, ni-1) || m.tabbing) {
			v.setActive(ni-1, ci, shift)
		}
	case key.NameDownArrow:
		if e.Modifiers.Contain(key.ModCtrl) || e.Modifiers.Contain(metaMod()) { // tp
			v.extendTo(ni, 1)
			return
		}
		if ni < len(g.nav)-1 && (g.contiguous(ni, ni+1) || m.tabbing) { // eX
			v.setActive(ni+1, ci, shift)
		}
	case key.NameHome: // e7
		if ci != 0 {
			v.setActive(ni, 0, false)
		}
	case key.NameEnd: // e6
		if ci != last {
			v.setActive(ni, last, false)
		}
	case key.NamePageUp, key.NamePageDown: // e9, tm
		v.page(ni, ci, e.Name == key.NamePageDown, viewH)
	case key.NameTab:
		if shift { // e4
			if ci == 0 {
				v.setActive(ni-1, last, false)
			} else {
				v.setActive(ni, ci-1, false)
			}
		} else { // e3
			if ci >= last {
				v.setActive(ni+1, 0, false)
			} else {
				v.setActive(ni, ci+1, false)
			}
		}
	case key.NameEscape: // GridField: clearSelection({clearAwareness})
		g.prev = g.active
		g.active, g.selA, g.selB = cellRef{}, cellRef{}, cellRef{}
	case key.NameDeleteBackward, key.NameDeleteForward: // th
		v.deleteSelection(d)
	case "C":
		v.copySelection(gtx, d, false)
	case "X":
		v.copySelection(gtx, d, true)
	case "V":
		gtx.Execute(clipboard.ReadCmd{Tag: &g.tag})
	case key.NameReturn, key.NameEnter:
		switch {
		case e.Modifiers == key.ModShift: // e2: an item below
			v.addBelow(d, ni)
		case e.Modifiers == key.ModAlt: // e1: open the item
			if v.act.OpenItem != nil {
				v.act.OpenItem(v.id, g.active.rec)
			}
		case e.Modifiers == metaMod(): // LinkColumn: ⌘/Super+Enter opens the link
			v.cellKey(gtx, d, m, keyMetaEnter, "")
		case e.Modifiers == 0:
			v.cellKey(gtx, d, m, keyEnter, "")
		}
	case key.NameF2:
		v.cellKey(gtx, d, m, keyF2, "")
	case key.NameSpace:
		v.cellKey(gtx, d, m, keySpace, "")
		g.eatSpace = true
	}
}

// typed is text typed into the focused grid (Gio's EditEvent): a
// printable ASCII character starts editing (GridField: /^[ -~]$/ without
// Ctrl or ⌘), and lands in the editor at the end of the value.
func (v *View) typed(gtx layout.Context, d *core.ListDoc, m kbMode, s string) {
	g := &v.grid
	if g.eatSpace && s == " " { // Space was the key itself (preventDefault in Slack)
		g.eatSpace = false
		return
	}
	g.eatSpace = false
	if v.edit.active || g.menuOpen || len(s) != 1 || s[0] < ' ' || s[0] > '~' {
		return
	}
	if g.rate.active {
		return
	}
	if _, _, ok := g.pos(g.active); !ok {
		return
	}
	v.cellKey(gtx, d, m, keyChar, s)
}

type cellKeyKind uint8

const (
	keyEnter cellKeyKind = iota
	keyF2
	keySpace
	keyChar
	keyMetaEnter
)

// skipEditMode is 3VkJ GridView t2: whether a click (ctx 0) or key
// (ctx 1) in a cell does the column's custom action instead of opening
// its editor; notEditable announces "Not editable".
func skipEditMode(c *lists.Column, keyboard, populated bool) (skip, notEditable bool) {
	switch c.Type {
	case lists.CreatedTime, lists.LastEditedTime, lists.CreatedBy, lists.LastEditedBy:
		return true, true
	case lists.Vote, lists.TodoCompleted, lists.Attachment:
		return true, false
	case lists.Link, lists.Message:
		return keyboard && populated, false
	case lists.Rating:
		return !keyboard, false
	case lists.Checkbox:
		return !c.Options.MarkAsDoneWhenChecked, false
	}
	return false, false
}

func populated(c *lists.Column, f *lists.Field) bool {
	if f == nil {
		return false
	}
	switch c.Type {
	case lists.Link:
		return len(f.Link) > 0
	case lists.Message:
		return len(f.Message) > 0
	}
	return CellText(nil, c, f) != "" || len(f.Attachment) > 0
}

// cellKey is GridField's onKeyDown for the active cell.
func (v *View) cellKey(gtx layout.Context, d *core.ListDoc, m kbMode, k cellKeyKind, ch string) {
	g := &v.grid
	_, ci, ok := g.pos(g.active)
	if !ok || d == nil {
		return
	}
	rec := d.Record(g.active.rec)
	col := g.cols[ci].c
	if rec == nil {
		return
	}
	f := rec.Field(col.Key)
	start := k == keyEnter || k == keySpace || k == keyF2 // J
	enterF2 := k == keyEnter || k == keyF2 || k == keyMetaEnter
	if !d.Info.Editable { // viewOnly
		if start {
			v.announce("Not editable")
		}
		return
	}
	if k == keyMetaEnter {
		if col.Type == lists.Link && f != nil && len(f.Link) > 0 && v.act.OpenURL != nil {
			v.act.OpenURL(f.Link[0].OriginalURL)
		}
		return
	}
	if start || (k == keyChar && ci != 0) {
		if skip, ne := skipEditMode(col, true, populated(col, f)); skip {
			switch {
			case ne:
				v.announce("Not editable")
			case col.Type == lists.Link && enterF2: // LinkColumn: Enter clicks "Link options"
				v.linkMenu(d, rec, col, f)
			case col.Type == lists.Attachment || col.Type == lists.Message:
				if enterF2 {
					if err := canEdit(d, col, f); err != nil {
						v.refuse(err)
					}
				}
			default:
				v.customClick(d, rec, col, f)
			}
			return
		}
	}
	// start editing
	if err := canEdit(d, col, f); err != nil {
		typed := ""
		if k == keyChar {
			typed = ch
		}
		if v.startRich(d, rec, col, f, err, typed) { // richedit.go: a formatted text cell
			v.edit.done = v.editDone(col)
			return
		}
		v.refuse(err)
		return
	}
	switch col.Type {
	case lists.Rating:
		val := 0
		if f != nil && len(f.Rating) > 0 {
			val = int(f.Rating[0])
		}
		g.rate = rateEdit{active: true, at: g.active, val: val, orig: val}
	case lists.Checkbox, lists.Completed, lists.TodoCompleted:
		v.customClick(d, rec, col, f) // the checkbox input is the editor; Clack toggles it
	case lists.Select, lists.MultiSelect, lists.User, lists.Assignee, lists.TodoAssignee, lists.Channel:
		v.cellClickWith(d, rec, col, 0, gtx, v.gridMenu)
	default:
		orig := editText(v.h, col, f)
		v.edit.start(rec.ID, col, orig)
		if k == keyChar {
			v.edit.ed.SetText(orig + ch)
			n := len([]rune(orig + ch))
			v.edit.ed.SetCaret(n, n)
		}
		v.edit.done = v.editDone(col)
	}
}

// customClick is 3VkJ onCustomClickCell: checkboxes and to-dos toggle,
// votes add or take back the viewer's.
func (v *View) customClick(d *core.ListDoc, rec *lists.Record, c *lists.Column, f *lists.Field) {
	switch c.Type {
	case lists.Checkbox, lists.Completed, lists.TodoCompleted, lists.Vote:
		if c.Type == lists.TodoCompleted {
			if f != nil && f.Checkbox != nil && *f.Checkbox {
				v.announce("Not completed")
			} else {
				v.announce("Completed")
			}
		}
		v.cellClickWith(d, rec, c, 0, layout.Context{}, v.gridMenu)
	}
}

// announce is Slack's announcer API (a live region). Clack has no
// screen-reader bridge on the desktop yet: tests read it, and the cell's
// semantic description repeats it until the next key.
func (v *View) announce(s string) {
	v.grid.announced = append(v.grid.announced, s)
	v.grid.say = s
}

// ---- moves ----

// extendTo is useKeyCommands tp / t_: Mod+Shift+↓/↑ stretch the
// selection to the last/first row, of the row's group when grouped; the
// active cell stays.
func (v *View) extendTo(ni, dir int) {
	g := &v.grid
	target := ni
	for j := ni + dir; j >= 0 && j < len(g.nav) && g.contiguous(j-dir, j); j += dir {
		target = j
	}
	if !g.grouped {
		if dir > 0 {
			target = len(g.nav) - 1
		} else {
			target = 0
		}
	}
	_, ci, _ := g.pos(g.active)
	selCol := ci
	if g.wholeRows() {
		_, _, _, c1, _, _ := g.selection()
		if _, mc, ok := g.pos(g.selB); ok {
			selCol = mc
		} else {
			selCol = c1
		}
	}
	if !g.selA.ok() {
		g.selA = g.active
	}
	g.selB = g.cell(target, selCol)
}

// page is useKeyCommands e9 / tm: the first (last) row in view, the next
// one up (down) when the active row is already it, within the row's
// group; Slack leaves Chrome's PageUp/PageDown on, so the list also
// scrolls by a page (7/8 of the view, Chromium's paging step).
func (v *View) page(ni, ci int, down bool, viewH int) {
	g := &v.grid
	top, bottom := g.scrollY+g.headH, g.scrollY+viewH
	first, lastIn := -1, -1
	for j, li := range g.nav {
		ln := g.lines[li]
		if ln.y >= top && ln.y+ln.h <= bottom {
			if first < 0 {
				first = j
			}
			lastIn = j
		}
	}
	target := first
	if down {
		target = lastIn
	}
	if target == ni || target < 0 {
		if down {
			target = ni + 1
		} else {
			target = ni - 1
		}
	}
	if g.grouped && target >= 0 && target < len(g.nav) && g.groupOf(target) != g.groupOf(ni) {
		// the group's first (last) row
		target = ni
		for j := ni; j >= 0 && j < len(g.nav) && g.groupOf(j) == g.groupOf(ni); {
			target = j
			if down {
				j++
			} else {
				j--
			}
		}
	}
	if target < 0 || target >= len(g.nav) {
		return
	}
	v.setActive(target, ci, false)
	g.reveal = false
	step := viewH - g.headH
	step = step * 7 / 8
	if down {
		g.scrollY += step
	} else {
		g.scrollY -= step
	}
	// keep the row on screen
	ln := g.lines[g.nav[target]]
	if ln.y < g.scrollY+g.headH {
		g.scrollY = ln.y - g.headH
	}
	if ln.y+ln.h > g.scrollY+viewH {
		g.scrollY = ln.y + ln.h - viewH
	}
}

// revealActive scrolls the active cell into view: a row out of view goes
// to the top under the header (the virtual list's scrollToKey), a column
// to the nearest edge (scrollIntoView nearest).
func (v *View) revealActive(viewW, viewH int) {
	g := &v.grid
	ni, ci, ok := g.pos(g.active)
	if !ok {
		return
	}
	ln := g.lines[g.nav[ni]]
	if ln.y < g.scrollY+g.headH || ln.y+ln.h > g.scrollY+viewH {
		g.scrollY = ln.y - g.headH
	}
	if ci > 0 && len(g.cols) > 0 {
		firstW := g.cols[0].w
		gc := g.cols[ci]
		left := gc.x - firstW // visible left edge in scrolled space
		if left < g.scrollX {
			g.scrollX = left
		}
		if right := g.x0 + gc.x + gc.w; right-g.scrollX > viewW {
			g.scrollX = right - viewW
		}
	}
}

// ---- editing outcomes ----

const (
	doneEnter = iota
	doneEscape
	doneBlur
)

// editDone is what happens to the active cell when its inline editor
// closes: Enter commits and, in data entry mode, moves down a row in the
// group (nextRowOnSave; dates never do); on the group's last row the
// primary column adds an item (GridField tS → onAddRecordKeyboardEvent).
// Escape cancels and stays. Clicking away commits and leaves the grid.
func (v *View) editDone(c *lists.Column) func(how int) {
	return func(how int) {
		g := &v.grid
		d := v.Doc()
		m := v.kbMode()
		switch how {
		case doneEnter:
			g.refocus = true
			next := !m.arrowNav
			switch c.Type {
			case lists.Date, lists.DueDate, lists.TodoDueDate:
				next = false
			}
			if !next {
				return
			}
			ni, ci, ok := g.pos(g.active)
			if !ok {
				return
			}
			if g.contiguous(ni, ni+1) {
				v.setActive(ni+1, ci, false)
				return
			}
			if c.IsPrimary && d != nil && d.Info.Editable {
				v.startNew(d, v.groupCol(d), g.groupOf(ni))
				v.edit.done = v.newDone()
			}
		case doneEscape:
			g.refocus = true
		case doneBlur:
			if !g.focused {
				v.leave()
			}
		}
	}
}

// newDone follows a new item's name editor: in data entry mode Enter
// names it and opens the next new item, as Slack's primary-column Enter
// on the last row does.
func (v *View) newDone() func(how int) {
	return func(how int) {
		g := &v.grid
		switch how {
		case doneEnter:
			g.refocus = true
			if !v.kbMode().arrowNav && v.lastNewText != "" {
				if d := v.Doc(); d != nil {
					v.startNew(d, v.edit.groupCol, v.edit.group)
					v.edit.done = v.newDone()
				}
			}
		case doneEscape:
			g.refocus = true
		case doneBlur:
			if !g.focused {
				v.leave()
			}
		}
	}
}

func (v *View) groupCol(d *core.ListDoc) *lists.Column {
	if lv := v.current(d); lv != nil {
		return d.Meta.Column(lv.GroupBy())
	}
	return nil
}

// addBelow is Shift+Enter (useKeyCommands e2): a new item under the
// row, its name being edited. Clack's new-item editor sits at the end of
// the row's group (it creates the item without a position).
func (v *View) addBelow(d *core.ListDoc, ni int) {
	if d == nil || !d.Info.Editable {
		return
	}
	v.startNew(d, v.groupCol(d), v.grid.groupOf(ni))
	v.edit.done = v.newDone()
}

// gridMenu shows a cell editor's menu (select, people, channel) at the
// cell; the cell stays active while it is open, and a pick moves down a
// row in data entry mode when nextRow (single select: nextRowOnSave).
func (v *View) gridMenu(items []MenuItem, nextRow bool) {
	g := &v.grid
	g.menuOpen = true
	at := g.active
	for i := range items {
		do := items[i].Do
		if do == nil {
			continue
		}
		items[i].Do = func() {
			do()
			g.menuOpen = false
			g.refocus = true
			if nextRow && !v.kbMode().arrowNav && g.active == at {
				if ni, ci, ok := g.pos(at); ok && g.contiguous(ni, ni+1) {
					v.setActive(ni+1, ci, false)
				}
			}
			v.h.Invalidate()
		}
	}
	if p, ok := v.cellAnchor(at); ok && v.act.MenuAt != nil {
		v.act.MenuAt(p, items)
		return
	}
	if v.act.Menu != nil {
		v.act.Menu(items)
	}
}

// cellAnchor is the bottom-left of a cell in window coordinates, once the
// pointer has shown where the grid is.
func (v *View) cellAnchor(c cellRef) (image.Point, bool) {
	g := &v.grid
	ni, ci, ok := g.pos(c)
	if !ok || !g.originOK {
		return image.Point{}, false
	}
	ln := g.lines[g.nav[ni]]
	gc := g.cols[ci]
	x := g.x0 + gc.x
	if ci > 0 {
		x -= g.scrollX
	}
	return g.origin.Add(image.Pt(x, ln.y-g.scrollY+ln.h)), true
}

// linkMenu is LinkColumnMenu ("Link options").
func (v *View) linkMenu(d *core.ListDoc, rec *lists.Record, c *lists.Column, f *lists.Field) {
	if f == nil || len(f.Link) == 0 {
		return
	}
	l := f.Link[0]
	viewOnly := canEdit(d, c, f) != nil
	items := []MenuItem{{Label: "Open link", Icon: "external", Do: func() {
		if v.act.OpenURL != nil {
			v.act.OpenURL(l.OriginalURL)
		}
	}}}
	if !viewOnly {
		items = append(items, MenuItem{Label: "Edit link", Icon: "edit", Do: func() {
			v.edit.start(rec.ID, c, l.OriginalURL)
			v.edit.done = v.editDone(c)
		}})
	}
	items = append(items, MenuItem{Label: "Copy link", Icon: "copy", Do: func() {
		if v.act.Copy != nil {
			v.act.Copy(l.OriginalURL)
		}
	}})
	if !viewOnly && l.DisplayName != "" {
		lbl := "Display as URL"
		if l.DisplayAsURL {
			lbl = "Display as title"
		}
		items = append(items, MenuItem{Label: lbl, Icon: "link", Do: func() {
			nl := l
			nl.DisplayAsURL = !l.DisplayAsURL
			v.save(lists.Edit{RowID: rec.ID, Column: c, Link: &nl})
		}})
	}
	if !viewOnly {
		items = append(items, MenuItem{Label: "Remove link", Icon: "trash", Do: func() {
			v.save(lists.Edit{RowID: rec.ID, Column: c, Clear: true})
		}})
	}
	v.gridMenu(items, false)
}

// ---- the rating editor (b2eO: a range input) ----

type rateEdit struct {
	active    bool
	at        cellRef
	val, orig int
}

// rateKey: ←/↓ one less, →/↑ one more, Home none, End the most, a digit
// that many; Enter keeps the value and blurs (saves), Escape restores it.
func (v *View) rateKey(d *core.ListDoc, e key.Event) {
	g := &v.grid
	r := &g.rate
	mx := 5
	if c := d.Meta.Column(r.at.col); c != nil && c.Options.Max > 0 {
		mx = c.Options.Max
	}
	switch e.Name {
	case key.NameReturn, key.NameEnter:
		v.rateDone(d, true)
	case key.NameEscape:
		r.val = r.orig
		v.rateDone(d, false)
	case key.NameEnd:
		r.val = mx
	case key.NameHome:
		r.val = 0
	case key.NameLeftArrow, key.NameDownArrow:
		if r.val > 0 {
			r.val--
		}
	case key.NameRightArrow, key.NameUpArrow:
		if r.val < mx {
			r.val++
		}
	default:
		if n, err := strconv.Atoi(string(e.Name)); err == nil && len(e.Name) == 1 && n >= 0 && n <= mx {
			r.val = n
		}
	}
}

func (v *View) rateDone(d *core.ListDoc, save bool) {
	g := &v.grid
	r := g.rate
	g.rate = rateEdit{}
	if !save || r.val == r.orig || d == nil {
		return
	}
	c := d.Meta.Column(r.at.col)
	if c == nil {
		return
	}
	e := lists.Edit{RowID: r.at.rec, Column: c, Rating: r.val, Clear: r.val == 0}
	v.save(e)
}

// ---- delete, copy, paste ----

// deleteSelection is 8dY2 eD: the selected cells take their column's
// deleteValue (empty; checkboxes unchecked); computed cells keep theirs.
// A selection across every column deletes those items instead
// (deleteRecords: "Item deleted" / "Items deleted").
func (v *View) deleteSelection(d *core.ListDoc) {
	g := &v.grid
	if d == nil || !d.Info.Editable {
		return
	}
	r0, r1, c0, c1, _, ok := g.selection()
	if !ok {
		return
	}
	if g.wholeRows() {
		var ids []string
		for ni := r0; ni <= r1; ni++ {
			ids = append(ids, g.lines[g.nav[ni]].row.Rec.ID)
		}
		v.deleteRecords(ids)
		return
	}
	var edits []lists.Edit
	refused := error(nil)
	for ni := r0; ni <= r1; ni++ {
		rec := g.lines[g.nav[ni]].row.Rec
		for ci := c0; ci <= c1; ci++ {
			c := g.cols[ci].c
			f := rec.Field(c.Key)
			if err := canEdit(d, c, f); err != nil {
				if refused == nil {
					refused = err
				}
				continue
			}
			if f == nil || CellText(nil, c, f) == "" && !(c.Type == lists.Checkbox || c.Type == lists.Completed || c.Type == lists.TodoCompleted) {
				continue
			}
			e := lists.Edit{RowID: rec.ID, Column: c, Clear: true}
			if c.Type == lists.Checkbox || c.Type == lists.Completed || c.Type == lists.TodoCompleted {
				e = lists.Edit{RowID: rec.ID, Column: c, Check: false}
			}
			edits = append(edits, e)
		}
	}
	if len(edits) == 0 && refused != nil {
		v.refuse(refused)
	}
	v.saveAll(edits)
}

func (v *View) deleteRecords(ids []string) {
	a := v.h.Core()
	if a == nil || len(ids) == 0 {
		return
	}
	msg := "Item deleted"
	if len(ids) > 1 {
		msg = "Items deleted"
	}
	g := &v.grid
	g.active, g.selA, g.selB = cellRef{}, cellRef{}, cellRef{}
	go func() {
		ctx, cancel := ctxTimeout(30e9)
		defer cancel()
		for _, id := range ids {
			if err := a.DeleteListRecord(ctx, v.id, id); err != nil {
				v.h.Toast("Couldn't delete the item: " + err.Error())
				return
			}
		}
		v.h.Toast(msg)
		v.refresh()
	}()
}

// saveAll writes several cells in one lists.cells.update.
func (v *View) saveAll(edits []lists.Edit) {
	a := v.h.Core()
	if a == nil || len(edits) == 0 {
		return
	}
	go func() {
		ctx, cancel := ctxTimeout(30e9)
		defer cancel()
		if err := a.UpdateListCells(ctx, v.id, edits...); err != nil {
			v.h.Toast("Couldn't save the change: " + err.Error())
		}
		v.h.Invalidate()
	}()
}

// copyValue is a cell's column copyValue as the plain text Slack puts on
// the clipboard (8dY2 t2: text/plain keeps raw ids, timestamps and
// option labels; rich text becomes its plain text).
func copyValue(c *lists.Column, f *lists.Field) string {
	if f == nil {
		switch c.Type {
		case lists.Checkbox, lists.Completed, lists.TodoCompleted:
			return "NO"
		case lists.Vote, lists.Rating:
			return "0"
		}
		return ""
	}
	switch c.Type {
	case lists.Text, lists.RichText:
		return f.PlainText()
	case lists.Select, lists.MultiSelect: // wXyt / H1Mx: labels, "|" between
		var ls []string
		for _, ch := range c.Options.Choices {
			for _, s := range f.Select {
				if s == ch.Value {
					ls = append(ls, ch.Label)
				}
			}
		}
		return strings.Join(ls, "|")
	case lists.Checkbox, lists.Completed, lists.TodoCompleted: // DjJG
		if f.Checkbox != nil && *f.Checkbox {
			return "YES"
		}
		return "NO"
	case lists.Vote: // /2GL: the number of voters
		seen := map[string]bool{}
		for _, u := range f.User {
			seen[u] = true
		}
		return strconv.Itoa(len(seen))
	case lists.Rating:
		if len(f.Rating) > 0 {
			return strconv.Itoa(int(f.Rating[0]))
		}
		return "0"
	case lists.Number, lists.Currency:
		if len(f.Number) > 0 {
			return strconv.FormatFloat(f.Number[0], 'f', -1, 64)
		}
	case lists.Date, lists.DueDate, lists.TodoDueDate:
		return strings.Join(f.Date, "|")
	case lists.User, lists.Assignee, lists.TodoAssignee, lists.CreatedBy, lists.LastEditedBy:
		return strings.Join(f.User, "|")
	case lists.Channel:
		return strings.Join(f.Channel, "|")
	case lists.Attachment, lists.Canvas:
		return strings.Join(f.Attachment, "|")
	case lists.CreatedTime, lists.LastEditedTime:
		if len(f.Timestamp) > 0 {
			return strconv.FormatInt(int64(f.Timestamp[0]), 10)
		}
	case lists.Link:
		if len(f.Link) > 0 {
			return f.Link[0].OriginalURL
		}
	case lists.Email:
		return strings.Join(f.Email, "|")
	case lists.Phone:
		return strings.Join(f.Phone, "|")
	case lists.Message:
		if len(f.Message) > 0 {
			return f.Message[0].Value
		}
	}
	return ""
}

// copySelection is 8dY2 eL (and eB for cut): the selection as tab- and
// newline-separated text, a "Cell copied" / "Cells copied" toast; cut
// then deletes it.
func (v *View) copySelection(gtx layout.Context, d *core.ListDoc, cut bool) {
	g := &v.grid
	r0, r1, c0, c1, multi, ok := g.selection()
	if !ok || d == nil {
		return
	}
	var rows []string
	for ni := r0; ni <= r1; ni++ {
		rec := g.lines[g.nav[ni]].row.Rec
		var cells []string
		for ci := c0; ci <= c1; ci++ {
			c := g.cols[ci].c
			cells = append(cells, copyValue(c, rec.Field(c.Key)))
		}
		rows = append(rows, strings.Join(cells, "\t"))
	}
	s := strings.Join(rows, "\n")
	g.lastCopy = s
	g.copied = s
	gtx.Execute(clipboard.WriteCmd{Type: "application/text", Data: io.NopCloser(strings.NewReader(s))})
	if multi {
		v.h.Toast("Cells copied")
	} else {
		v.h.Toast("Cell copied")
	}
	if cut {
		v.deleteSelection(d)
	}
}

// parseClipboard is qtAi `to`: tab-separated cells with quoted values;
// several rows of plain text from elsewhere become one cell (only
// Slack's own slack/texty copy keeps rows: Clack knows its last copy).
func parseClipboard(s string, own bool) [][]string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.TrimSuffix(s, "\n")
	rows := [][]string{{}}
	start, inQ := 0, false
	save := func(end int) {
		cell := s[start:end]
		if inQ && strings.HasSuffix(cell, `"`) && strings.Contains(cell, "\n") && len(cell) >= 2 {
			cell = strings.ReplaceAll(cell[1:len(cell)-1], `""`, `"`)
		}
		if inQ && !strings.HasSuffix(cell, `"`) {
			parts := strings.Split(cell, "\n")
			for i, p := range parts {
				rows[len(rows)-1] = append(rows[len(rows)-1], p)
				if i < len(parts)-1 {
					rows = append(rows, []string{})
				}
			}
			return
		}
		rows[len(rows)-1] = append(rows[len(rows)-1], cell)
	}
	for i := 0; i < len(s); i++ {
		if inQ && s[i] == '"' && i+1 < len(s) && s[i+1] != '\t' && s[i+1] != '"' {
			inQ = false
		}
		if inQ && s[i] == '"' && i+1 < len(s) && s[i+1] == '"' {
			i++
			continue
		}
		if i == start && s[i] == '"' {
			inQ = true
		}
		if s[i] == '\t' {
			save(i)
			start, inQ = i+1, false
		} else if s[i] == '\n' && !inQ {
			save(i)
			rows = append(rows, []string{})
			start = i + 1
		}
	}
	save(len(s))
	if len(rows) > 1 && !own {
		var all []string
		for _, r := range rows {
			for _, c := range r {
				if c != "" {
					all = append(all, c)
				}
			}
		}
		return [][]string{{strings.Join(all, "")}}
	}
	return rows
}

var falsey = map[string]bool{"": true, "false": true, "no": true, "off": true, "disabled": true, "0": true, "n": true, "f": true,
	"unchecked": true, "undefined": true, "null": true, "wrong": true, "negative": true}

// pasteEdit is a column's pasteValue as a Clack edit (ok false: Clack
// can't write it).
func pasteEdit(rec string, c *lists.Column, s string) (lists.Edit, bool) {
	e := lists.Edit{RowID: rec, Column: c}
	switch c.Type {
	case lists.Text, lists.RichText, lists.Email, lists.Phone: // createTextColumn: newlines to spaces, trimmed, empty clears
		t := strings.TrimSpace(strings.Join(strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == '\r' }), " "))
		e.Text, e.Clear = t, t == ""
	case lists.Select, lists.MultiSelect: // a label to its option, else empty
		e.Clear = true
		for _, ch := range c.Options.Choices {
			if ch.Label == s {
				e.Values, e.Clear = []string{ch.Value}, false
				break
			}
		}
	case lists.Checkbox, lists.Completed, lists.TodoCompleted:
		e.Check = !falsey[strings.ToLower(s)]
	case lists.Rating:
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			return e, false
		}
		e.Rating, e.Clear = n, n <= 0
	case lists.Number, lists.Currency:
		t := strings.TrimSpace(s)
		if t == "" {
			e.Clear = true
			break
		}
		n, err := strconv.ParseFloat(strings.ReplaceAll(t, ",", ""), 64)
		if err != nil {
			return e, false
		}
		e.Number = &n
	case lists.Date, lists.DueDate, lists.TodoDueDate:
		t := strings.TrimSpace(s)
		if t == "" {
			e.Clear = true
			break
		}
		if _, err := parseDay(t, c); err != nil {
			return e, false
		}
		e.Values = []string{t}
	case lists.User, lists.Assignee, lists.TodoAssignee, lists.Channel:
		t := strings.TrimSpace(s)
		if t == "" {
			e.Clear = true
			break
		}
		for _, id := range strings.Split(t, "|") {
			id = strings.TrimSpace(id)
			if id == "" || strings.ToUpper(id) != id {
				return e, false
			}
			e.Values = append(e.Values, id)
		}
	case lists.Link:
		t := strings.TrimSpace(s)
		if t == "" {
			e.Clear = true
			break
		}
		e.Link = &lists.LinkValue{OriginalURL: t} // I6yI d: display_as_url false
	default:
		return e, false
	}
	return e, true
}

// paste is 8dY2 handlePasteData: the block lands from the active cell
// (or the selection's corner), clipped to the columns; Clack doesn't add
// items for rows past the end.
func (v *View) paste(d *core.ListDoc, s string) {
	g := &v.grid
	if d == nil || !d.Info.Editable || v.edit.active {
		return
	}
	r0, _, c0, _, _, ok := g.selection()
	if !ok {
		return
	}
	block := parseClipboard(s, s == g.lastCopy)
	var edits []lists.Edit
	var refused error
	for i, row := range block {
		ni := r0 + i
		if ni >= len(g.nav) {
			break
		}
		rec := g.lines[g.nav[ni]].row.Rec
		for j, val := range row {
			ci := c0 + j
			if ci >= len(g.cols) {
				break
			}
			c := g.cols[ci].c
			if err := canEdit(d, c, rec.Field(c.Key)); err != nil {
				if refused == nil {
					refused = err
				}
				continue
			}
			if e, ok := pasteEdit(rec.ID, c, val); ok {
				edits = append(edits, e)
			}
		}
	}
	if len(edits) == 0 && refused != nil {
		v.refuse(refused)
	}
	v.saveAll(edits)
}

// ---- screen-reader labels ----

func emojiName(s, def string) string {
	if s == "" {
		s = def
	}
	return strings.Trim(s, ":")
}

// cellLabel is the cell's accessible name: Slack's aria-label (x8/8:
// votes, ratings, several select options) or else its text.
func cellLabel(a *core.App, c *lists.Column, f *lists.Field) string {
	switch c.Type {
	case lists.Vote:
		n := 0
		if f != nil {
			n = len(f.User)
		}
		w := "votes"
		if n == 1 {
			w = "vote"
		}
		return emojiName(c.Options.Emoji, ":+1:") + ", " + strconv.Itoa(n) + " " + w
	case lists.Rating:
		val := 0
		if f != nil && len(f.Rating) > 0 {
			val = int(f.Rating[0])
		}
		s := strconv.Itoa(val)
		if c.Options.Max > 0 {
			s += " of " + strconv.Itoa(c.Options.Max)
		}
		return s + ", " + emojiName(c.Options.Emoji, ":star:")
	case lists.Checkbox, lists.Completed, lists.TodoCompleted: // CheckboxFieldInput aria-label
		if f != nil && f.Checkbox != nil && *f.Checkbox {
			return "Yes"
		}
		return "No"
	}
	return CellText(a, c, f)
}

// cellDescription is 3VkJ nE's aria-describedby: computed columns are
// "Read only", votes say whether the viewer's is in.
func cellDescription(self string, c *lists.Column, f *lists.Field) string {
	switch c.Type {
	case lists.CreatedBy, lists.CreatedTime, lists.LastEditedBy, lists.LastEditedTime:
		return "Read only"
	case lists.Vote:
		if f != nil {
			for _, u := range f.User {
				if u == self {
					return "Including you"
				}
			}
		}
		return "No vote from you"
	}
	return ""
}

// cellSemantics describes one drawn cell to Gio's semantic tree: its
// label, description and whether it is the active cell (Gio has no grid
// roles; Slack's are rowheader for the primary column, gridcell else).
func (v *View) cellSemantics(gtx layout.Context, r image.Rectangle, c *lists.Column, f *lists.Field, active bool) {
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	a := v.h.Core()
	self := ""
	if a != nil {
		self = a.Self().ID
	}
	semantic.LabelOp(cellLabel(a, c, f)).Add(gtx.Ops)
	desc := cellDescription(self, c, f)
	if active && v.grid.say != "" {
		desc = strings.TrimPrefix(desc+", "+v.grid.say, ", ")
	}
	if desc != "" {
		semantic.DescriptionOp(desc).Add(gtx.Ops)
	}
	semantic.SelectedOp(active).Add(gtx.Ops)
}
