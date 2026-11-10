package listview

// Router-driven tests of the Lists keyboard model (keys.go), one per
// binding per mode, against the fake server's "Launch tracker" list.
// Synthetic fixtures only.

import (
	"context"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/gpu/headless"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/io/transfer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/lists"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// the two modes of Preferences > Accessibility > Lists keyboard interactions
var kbModes = []string{"tab_nav", "arrow_nav"}

type kbEnv struct {
	t    *testing.T
	h    *testHost
	fs   *fakeslack.Server
	v    *View
	r    *input.Router
	mr   *menuRec
	sz   image.Point
	win  *headless.Window
	last *op.Ops

	mu     sync.Mutex
	opened []string
	urls   []string
}

func newKB(t *testing.T, pref, view string, dark bool) *kbEnv {
	t.Helper()
	h, fs := newEnv(t, dark)
	if pref != "" {
		if err := h.a.SetPref(context.Background(), PrefListsTabbing, pref); err != nil {
			t.Fatal(err)
		}
	}
	e := &kbEnv{t: t, h: h, fs: fs, r: new(input.Router), mr: &menuRec{}, sz: image.Pt(1440, 820)}
	act := e.mr.act()
	act.OpenItem = func(_, rec string) { e.mu.Lock(); e.opened = append(e.opened, rec); e.mu.Unlock() }
	act.OpenURL = func(u string) { e.mu.Lock(); e.urls = append(e.urls, u); e.mu.Unlock() }
	act.Copy = func(string) {}
	e.v = New(h, fakeslack.ListKitchen, Full, act)
	e.until("the list", func() bool { return e.v.Doc() != nil })
	if view != "" {
		e.v.SelectView(view)
	}
	e.frames(2)
	return e
}

func (e *kbEnv) frame() {
	ops := new(op.Ops)
	gtx := layout.Context{Ops: ops, Now: time.Now(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(e.sz), Source: e.r.Source()}
	e.v.Layout(gtx)
	e.r.Frame(ops)
	e.last = ops
}

func (e *kbEnv) frames(n int) {
	for i := 0; i < n; i++ {
		e.frame()
	}
}

func (e *kbEnv) until(what string, cond func() bool) {
	e.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			e.t.Fatalf("timed out waiting for %s", what)
		}
		e.frame()
		time.Sleep(10 * time.Millisecond)
	}
}

// focus is Tab arriving at the grid.
func (e *kbEnv) focus() {
	e.r.Source().Execute(key.FocusCmd{Tag: &e.v.grid.tag})
	e.frames(3)
}

func (e *kbEnv) press(name key.Name, mods key.Modifiers) {
	e.r.Queue(key.Event{Name: name, Modifiers: mods, State: key.Press}, key.Event{Name: name, Modifiers: mods, State: key.Release})
	e.frames(3)
}

// tab presses Tab as the window does (a system event: unclaimed, Gio
// moves the focus) and reports whether the grid took it.
func (e *kbEnv) tab(mods key.Modifiers) bool {
	e.frames(1)
	e.r.WakeupTime()
	e.r.Queue(input.SystemEvent{Event: key.Event{Name: key.NameTab, Modifiers: mods, State: key.Press}})
	_, handled := e.r.WakeupTime()
	e.frames(3)
	return handled
}

func (e *kbEnv) typeText(s string) {
	e.r.Queue(key.EditEvent{Text: s})
	e.frames(3)
}

// at is the active cell as (row, column) indexes.
func (e *kbEnv) at() (int, int) {
	ni, ci, ok := e.v.grid.pos(e.v.grid.active)
	if !ok {
		return -1, -1
	}
	return ni, ci
}

func (e *kbEnv) col(key string) int { return e.v.grid.colIdx(key) }

func (e *kbEnv) row(rec string) int { return e.v.grid.navRow(rec) }

// goTo makes a cell the active one, as a click on it would.
func (e *kbEnv) goTo(rec, col string) {
	e.t.Helper()
	g := &e.v.grid
	c := cellRef{rec: rec, col: col}
	if _, _, ok := g.pos(c); !ok {
		e.t.Fatalf("no cell %s/%s", rec, col)
	}
	g.active, g.selA, g.selB = c, c, cellRef{}
	g.reveal = true
	e.frames(2)
}

func (e *kbEnv) wantAt(what string, ni, ci int) {
	e.t.Helper()
	if gn, gc := e.at(); gn != ni || gc != ci {
		e.t.Fatalf("%s: active cell (%d,%d), want (%d,%d)", what, gn, gc, ni, ci)
	}
}

func (e *kbEnv) stored(id, key string) *lists.Field {
	l, _ := e.fs.List(fakeslack.ListKitchen)
	for i := range l.Records {
		if l.Records[i].ID == id {
			return l.Records[i].Field(key)
		}
	}
	return nil
}

func (e *kbEnv) exists(id string) bool {
	l, _ := e.fs.List(fakeslack.ListKitchen)
	for i := range l.Records {
		if l.Records[i].ID == id {
			return true
		}
	}
	return false
}

func (e *kbEnv) menu(label string) *MenuItem {
	e.t.Helper()
	it := e.mr.find(label)
	if it == nil {
		e.mr.mu.Lock()
		var ls []string
		for _, m := range e.mr.items {
			ls = append(ls, m.Label)
		}
		e.mr.mu.Unlock()
		e.t.Fatalf("no menu item %q in %q", label, ls)
	}
	return it
}

// pick runs a menu item the way the shell's menu does (on the UI
// goroutine, between frames).
func (e *kbEnv) pick(label string) {
	e.t.Helper()
	e.menu(label).Do()
	e.frames(3)
}

func (e *kbEnv) clearMenu() { e.mr.mu.Lock(); e.mr.items = nil; e.mr.mu.Unlock() }

// cellCenter is a cell's centre in the view.
func (e *kbEnv) cellCenter(rec, col string) image.Point {
	g := &e.v.grid
	ni, ci, ok := g.pos(cellRef{rec, col})
	if !ok {
		e.t.Fatalf("no cell %s/%s", rec, col)
	}
	ln := g.lines[g.nav[ni]]
	gc := g.cols[ci]
	x := g.x0 + gc.x + gc.w/2
	if ci > 0 {
		x -= g.scrollX
	}
	return e.v.gridAt.Add(image.Pt(x, ln.y-g.scrollY+ln.h/2))
}

func (e *kbEnv) click(p image.Point) {
	pt := f32.Pt(float32(p.X), float32(p.Y))
	e.r.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pt},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pt})
	e.frames(3)
}

func (e *kbEnv) semantics() []input.SemanticNode {
	var flat []input.SemanticNode
	var walk func(ns []input.SemanticNode)
	walk = func(ns []input.SemanticNode) {
		for _, n := range ns {
			flat = append(flat, n)
			walk(n.Children)
		}
	}
	walk(e.r.AppendSemantics(nil))
	return flat
}

func (e *kbEnv) clipboard() string {
	_, b, ok := e.r.WriteClipboard()
	if !ok {
		return ""
	}
	return string(b)
}

const (
	rec1 = "Rec0ITEM001" // Write the launch announcement
	rec3 = "Rec0ITEM003" // Record the demo video
	rec4 = "Rec0ITEM004" // Ship the beta
	rec5 = "Rec0ITEM005"
)

func eachMode(t *testing.T, f func(t *testing.T, mode string)) {
	for _, m := range kbModes {
		t.Run(m, func(t *testing.T) { f(t, m) })
	}
}

// Focus arriving at the grid lands on the first cell (8dY2 aO); its ring
// is the semantic tree's selected node; Escape leaves the cell and focus
// coming back restores it; a click makes the clicked cell active.
func TestListKeysFocusRing(t *testing.T) {
	eachMode(t, func(t *testing.T, mode string) {
		e := newKB(t, mode, "", false)
		e.focus()
		e.wantAt("focus", 0, 0)
		found := false
		for _, n := range e.semantics() {
			if n.Desc.Selected && n.Desc.Label == "Write the launch announcement" {
				found = true
			}
		}
		if !found {
			t.Fatal("the active cell isn't the selected semantic node")
		}
		e.press(key.NameRightArrow, 0)
		e.press(key.NameEscape, 0)
		if e.v.grid.active.ok() {
			t.Fatal("Escape kept the active cell")
		}
		e.r.Source().Execute(key.FocusCmd{Tag: nil})
		e.frames(2)
		e.focus()
		e.wantAt("refocus", 0, 1)
		// a click on a checkbox: it toggles and becomes the active cell
		e.click(e.cellCenter(rec3, "Col0DONE001"))
		e.wantAt("click", e.row(rec3), e.col("Col0DONE001"))
		if !e.v.grid.focused {
			t.Fatal("a click didn't give the grid the focus")
		}
		e.until("the click's toggle", func() bool { f := e.stored(rec3, "Col0DONE001"); return f != nil && f.Checkbox != nil && *f.Checkbox })
	})
}

// ←/→/↑/↓ move one cell and stop at the edges (useKeyCommands eQ eZ eJ eX).
func TestListKeysArrows(t *testing.T) {
	eachMode(t, func(t *testing.T, mode string) {
		e := newKB(t, mode, "", false)
		e.focus()
		e.press(key.NameLeftArrow, 0)
		e.wantAt("← at the edge", 0, 0)
		e.press(key.NameUpArrow, 0)
		e.wantAt("↑ at the edge", 0, 0)
		e.press(key.NameRightArrow, 0)
		e.wantAt("→", 0, 1)
		e.press(key.NameDownArrow, 0)
		e.wantAt("↓", 1, 1)
		e.press(key.NameUpArrow, 0)
		e.wantAt("↑", 0, 1)
		e.press(key.NameLeftArrow, 0)
		e.wantAt("←", 0, 0)
		last := len(e.v.grid.nav) - 1
		e.goTo(e.v.grid.cell(last, 0).rec, "name")
		e.press(key.NameDownArrow, 0)
		e.wantAt("↓ at the edge", last, 0)
	})
}

// ↑/↓ cross a group boundary only with tabbing navigation (Data entry
// mode): eJ/eX's `J === em ± 1 || ex`.
func TestListKeysGroupEdges(t *testing.T) {
	eachMode(t, func(t *testing.T, mode string) {
		e := newKB(t, mode, "View0GRP001", false)
		e.focus()
		g := &e.v.grid
		edge := -1
		for i := 0; i+1 < len(g.nav); i++ {
			if !g.contiguous(i, i+1) {
				edge = i
				break
			}
		}
		if edge < 0 {
			t.Fatal("the grouped view has one group")
		}
		e.goTo(g.cell(edge, 0).rec, "name")
		e.press(key.NameDownArrow, 0)
		want := edge
		if mode == "tab_nav" {
			want = edge + 1
		}
		e.wantAt("↓ at the group's end", want, 0)
		e.goTo(g.cell(edge+1, 0).rec, "name")
		e.press(key.NameUpArrow, 0)
		want = edge + 1
		if mode == "tab_nav" {
			want = edge
		}
		e.wantAt("↑ at the group's start", want, 0)
	})
}

// Home and End: the row's first and last cells (e7, e6).
func TestListKeysHomeEnd(t *testing.T) {
	eachMode(t, func(t *testing.T, mode string) {
		e := newKB(t, mode, "", false)
		e.focus()
		e.press(key.NameDownArrow, 0)
		e.press(key.NameEnd, 0)
		e.wantAt("End", 1, len(e.v.grid.cols)-1)
		if e.v.grid.scrollX == 0 {
			t.Fatal("End didn't scroll the last column into view")
		}
		e.press(key.NameHome, 0)
		e.wantAt("Home", 1, 0)
	})
}

// PageDown/PageUp: the last/first row in view, the next one when the
// active row is already it, and the list pages (e9, tm).
func TestListKeysPage(t *testing.T) {
	eachMode(t, func(t *testing.T, mode string) {
		e := newKB(t, mode, "", false)
		e.sz = image.Pt(1440, 420) // a few rows in view
		e.frames(2)
		e.focus()
		e.press(key.NamePageDown, 0)
		n1, _ := e.at()
		if n1 <= 0 {
			t.Fatalf("PageDown stayed on row %d", n1)
		}
		e.press(key.NamePageDown, 0)
		n2, _ := e.at()
		if n2 <= n1 {
			t.Fatalf("a second PageDown went from row %d to %d", n1, n2)
		}
		if e.v.grid.scrollY == 0 {
			t.Fatal("PageDown didn't page the list")
		}
		e.press(key.NamePageUp, 0)
		n3, _ := e.at()
		if n3 >= n2 {
			t.Fatalf("PageUp went from row %d to %d", n2, n3)
		}
	})
}

// Tab and Shift+Tab move between cells and wrap rows only when the pref
// says tab_nav (e3/e4); at the end, in Full keyboard mode or with the
// pref unset, the grid leaves Tab to Gio's focus move.
func TestListKeysTab(t *testing.T) {
	for _, mode := range []string{"tab_nav", "arrow_nav", ""} {
		name := mode
		if name == "" {
			name = "unset"
		}
		t.Run(name, func(t *testing.T) {
			e := newKB(t, mode, "", false)
			e.focus()
			last := len(e.v.grid.cols) - 1
			if mode != "tab_nav" {
				if e.tab(0) {
					t.Fatal("the grid took Tab")
				}
				return
			}
			if !e.tab(0) {
				t.Fatal("Tab wasn't the grid's")
			}
			e.wantAt("Tab", 0, 1)
			e.tab(key.ModShift)
			e.wantAt("Shift+Tab", 0, 0)
			e.press(key.NameEnd, 0)
			e.tab(0)
			e.wantAt("Tab at the row's end", 1, 0)
			e.tab(key.ModShift)
			e.wantAt("Shift+Tab at the row's start", 0, last)
			lastRow := len(e.v.grid.nav) - 1
			e.goTo(e.v.grid.cell(lastRow, last).rec, e.v.grid.cols[last].c.Key)
			if e.tab(0) {
				t.Fatal("Tab at the list's last cell stayed in the grid")
			}
		})
	}
}

// Enter edits a text cell; Enter in the editor saves and, in Data entry
// mode, moves down a row (nextRowOnSave); Full keyboard mode stays.
func TestListKeysEnterEdits(t *testing.T) {
	eachMode(t, func(t *testing.T, mode string) {
		e := newKB(t, mode, "", false)
		e.focus()
		e.goTo(rec3, "name")
		e.press(key.NameReturn, 0)
		if !e.v.edit.editing(rec3, e.v.grid.cols[0].c) || e.v.edit.ed.Text() != "Record the demo video" {
			t.Fatalf("Enter: editor %v %q", e.v.edit.active, e.v.edit.ed.Text())
		}
		e.v.edit.ed.SetText("Record the demo video again")
		e.press(key.NameReturn, 0)
		e.until("the name", func() bool { return e.stored(rec3, "name").PlainText() == "Record the demo video again" })
		want := e.row(rec3)
		if mode == "tab_nav" {
			want++
		}
		e.wantAt("after Enter", want, 0)
		if !e.v.grid.focused {
			t.Fatal("the grid didn't take the focus back")
		}
	})
}

// Escape in an editor cancels and keeps the cell (RecordTextInput eI).
func TestListKeysEscapeCancels(t *testing.T) {
	eachMode(t, func(t *testing.T, mode string) {
		e := newKB(t, mode, "", false)
		e.focus()
		e.goTo(rec3, "Col0EST0001")
		e.press(key.NameReturn, 0)
		if !e.v.edit.active {
			t.Fatal("Enter didn't edit the number")
		}
		e.v.edit.ed.SetText("99")
		n := len(e.fs.ListCellUpdates())
		e.press(key.NameEscape, 0)
		if e.v.edit.active || len(e.fs.ListCellUpdates()) != n {
			t.Fatalf("Escape: editor %v, updates %d→%d", e.v.edit.active, n, len(e.fs.ListCellUpdates()))
		}
		e.wantAt("Escape", e.row(rec3), e.col("Col0EST0001"))
	})
}

// Typing a printable character starts editing with it at the end.
func TestListKeysTyping(t *testing.T) {
	eachMode(t, func(t *testing.T, mode string) {
		e := newKB(t, mode, "", false)
		e.focus()
		e.goTo(rec3, "name")
		e.typeText("!")
		if !e.v.edit.active || e.v.edit.ed.Text() != "Record the demo video!" {
			t.Fatalf("typing: editor %v %q", e.v.edit.active, e.v.edit.ed.Text())
		}
		e.press(key.NameEscape, 0)
		// a number: the digit lands after the value
		e.goTo(rec3, "Col0EST0001")
		e.typeText("5")
		if e.v.edit.ed.Text() != "25" {
			t.Fatalf("typing in a number: %q", e.v.edit.ed.Text())
		}
		e.press(key.NameReturn, 0)
		e.until("25", func() bool {
			f := e.stored(rec3, "Col0EST0001")
			return f != nil && len(f.Number) == 1 && f.Number[0] == 25
		})
	})
}

// Enter on a single select opens its options; a pick saves and moves down
// in Data entry mode (nextRowOnSave); multi-selects and people stay.
func TestListKeysSelectAndPeople(t *testing.T) {
	eachMode(t, func(t *testing.T, mode string) {
		e := newKB(t, mode, "", false)
		e.focus()
		e.goTo(rec3, "Col0STATUS1")
		e.press(key.NameReturn, 0)
		if !e.v.grid.menuOpen {
			t.Fatal("Enter didn't open the select")
		}
		e.pick("Blocked")
		e.until("Blocked", func() bool {
			f := e.stored(rec3, "Col0STATUS1")
			return f != nil && len(f.Select) == 1 && f.Select[0] == "OptBLOK0001"
		})
		want := e.row(rec3)
		if mode == "tab_nav" {
			want++
		}
		e.wantAt("after the select", want, e.col("Col0STATUS1"))
		// multi-select: F2 opens it, a pick stays
		e.goTo(rec3, "Col0TAGS001")
		e.clearMenu()
		e.press(key.NameF2, 0)
		e.pick("Design")
		e.until("the tag", func() bool { f := e.stored(rec3, "Col0TAGS001"); return f != nil && len(f.Select) == 2 })
		e.wantAt("after the multi-select", e.row(rec3), e.col("Col0TAGS001"))
		// people
		e.goTo(rec3, "Col0OWNER01")
		e.clearMenu()
		e.press(key.NameSpace, 0)
		var pick *MenuItem
		e.mr.mu.Lock()
		for i := range e.mr.items {
			if e.mr.items[i].Icon == "user" && !strings.Contains(e.mr.items[i].Label, "(you)") {
				pick = &e.mr.items[i]
				break
			}
		}
		e.mr.mu.Unlock()
		if pick == nil {
			t.Fatal("Space didn't open the people menu")
		}
		pick.Do()
		e.frames(2)
		e.until("the owner", func() bool {
			f := e.stored(rec3, "Col0OWNER01")
			return f != nil && len(f.User) == 1 && f.User[0] != fakeslack.SelfID
		})
		e.wantAt("after the person", e.row(rec3), e.col("Col0OWNER01"))
	})
}

// A date: Enter edits, Enter saves and stays (DateSelectComponent stops
// with nextRow false).
func TestListKeysDate(t *testing.T) {
	eachMode(t, func(t *testing.T, mode string) {
		e := newKB(t, mode, "", false)
		e.focus()
		e.goTo(rec3, "Col0DUE0001")
		e.press(key.NameReturn, 0)
		e.v.edit.ed.SetText("2026-12-01")
		e.press(key.NameReturn, 0)
		e.until("the date", func() bool {
			f := e.stored(rec3, "Col0DUE0001")
			return f != nil && len(f.Date) == 1 && f.Date[0] == "2026-12-01"
		})
		e.wantAt("after the date", e.row(rec3), e.col("Col0DUE0001"))
	})
}

// A checkbox: Space toggles it (skipEditMode → onCustomClickCell), and so
// does a typed character (GridField sends printable keys there too).
func TestListKeysCheckbox(t *testing.T) {
	eachMode(t, func(t *testing.T, mode string) {
		e := newKB(t, mode, "", false)
		e.focus()
		e.goTo(rec3, "Col0DONE001")
		e.press(key.NameSpace, 0)
		e.typeText(" ") // the Space key's own text: eaten
		e.until("checked", func() bool { f := e.stored(rec3, "Col0DONE001"); return f != nil && f.Checkbox != nil && *f.Checkbox })
		n := len(e.fs.ListCellUpdates())
		if n != 1 {
			t.Fatalf("Space sent %d updates", n)
		}
		e.until("the doc", func() bool {
			f := e.v.Doc().Record(rec3).Field("Col0DONE001")
			return f != nil && f.Checkbox != nil && *f.Checkbox
		})
		e.typeText("x")
		e.until("unchecked", func() bool { f := e.stored(rec3, "Col0DONE001"); return f != nil && f.Checkbox != nil && !*f.Checkbox })
	})
}

// A vote: Enter adds the viewer's.
func TestListKeysVote(t *testing.T) {
	eachMode(t, func(t *testing.T, mode string) {
		e := newKB(t, mode, "", false)
		e.focus()
		e.goTo(rec3, "Col0VOTE001")
		e.press(key.NameReturn, 0)
		e.until("the vote", func() bool {
			f := e.stored(rec3, "Col0VOTE001")
			return f != nil && len(f.User) == 1 && f.User[0] == fakeslack.SelfID
		})
	})
}

// A link: Enter opens "Link options"; ⌘/Super+Enter opens the link; an
// empty link cell edits.
func TestListKeysLink(t *testing.T) {
	eachMode(t, func(t *testing.T, mode string) {
		e := newKB(t, mode, "", false)
		e.focus()
		e.goTo(rec1, "Col0LINK001")
		e.press(key.NameReturn, 0)
		for _, l := range []string{"Open link", "Edit link", "Copy link", "Display as URL", "Remove link"} {
			e.menu(l)
		}
		e.pick("Open link")
		e.press(key.NameReturn, metaMod())
		e.mu.Lock()
		urls := append([]string(nil), e.urls...)
		e.mu.Unlock()
		if len(urls) != 2 || urls[0] != "https://example.com/launch" || urls[1] != urls[0] {
			t.Fatalf("opened %q", urls)
		}
		e.goTo(rec3, "Col0LINK001")
		e.press(key.NameReturn, 0)
		if !e.v.edit.active {
			t.Fatal("Enter on an empty link didn't edit it")
		}
	})
}

// A rating: Enter opens the range editor; →/↑ +1, ←/↓ −1, Home, End,
// digits; Enter saves, Escape restores.
func TestListKeysRating(t *testing.T) {
	eachMode(t, func(t *testing.T, mode string) {
		e := newKB(t, mode, "", false)
		e.focus()
		e.goTo(rec3, "Col0PRIO001") // 3 stars
		e.press(key.NameReturn, 0)
		if !e.v.grid.rate.active || e.v.grid.rate.val != 3 {
			t.Fatalf("Enter: rating editor %+v", e.v.grid.rate)
		}
		e.press(key.NameRightArrow, 0)
		e.press(key.NameUpArrow, 0)
		e.press(key.NameLeftArrow, 0)
		if e.v.grid.rate.val != 4 {
			t.Fatalf("arrows: %d", e.v.grid.rate.val)
		}
		e.press(key.NameEnd, 0)
		e.press("2", 0)
		e.press(key.NameReturn, 0)
		e.until("2 stars", func() bool {
			f := e.stored(rec3, "Col0PRIO001")
			return f != nil && len(f.Rating) == 1 && f.Rating[0] == 2
		})
		e.wantAt("after the rating", e.row(rec3), e.col("Col0PRIO001"))
		n := len(e.fs.ListCellUpdates())
		e.press(key.NameReturn, 0)
		e.press(key.NameHome, 0)
		e.press(key.NameEscape, 0)
		if e.v.grid.rate.active || len(e.fs.ListCellUpdates()) != n {
			t.Fatal("Escape in the rating editor saved")
		}
	})
}

// Files and computed cells: Enter says why nothing opens (Clack's guard
// toast for files; Slack's "Not editable" announcement for computed).
func TestListKeysAttachmentsAndComputed(t *testing.T) {
	eachMode(t, func(t *testing.T, mode string) {
		e := newKB(t, mode, "", false)
		e.focus()
		e.goTo(rec1, "Col0FILE001")
		e.press(key.NameReturn, 0)
		if !strings.Contains(e.h.lastToast(), "it has files") {
			t.Fatalf("files: toast %q", e.h.lastToast())
		}
		e.goTo(rec1, "Col0CRBY001")
		e.press(key.NameReturn, 0)
		if a := e.v.grid.announced; len(a) == 0 || a[len(a)-1] != "Not editable" {
			t.Fatalf("computed: announced %q", a)
		}
		var desc string
		for _, n := range e.semantics() {
			if n.Desc.Selected {
				desc = n.Desc.Description
			}
		}
		if desc != "Read only, Not editable" {
			t.Fatalf("computed cell description %q", desc)
		}
	})
}

// Shift+arrows extend the selection; Mod+Shift+↓ stretches it to the last
// row; a plain arrow collapses it (eY).
func TestListKeysSelection(t *testing.T) {
	eachMode(t, func(t *testing.T, mode string) {
		e := newKB(t, mode, "", false)
		e.focus()
		e.press(key.NameRightArrow, key.ModShift)
		e.press(key.NameDownArrow, key.ModShift)
		g := &e.v.grid
		r0, r1, c0, c1, multi, _ := g.selection()
		if !multi || r0 != 0 || r1 != 1 || c0 != 0 || c1 != 1 {
			t.Fatalf("Shift+arrows: rows %d-%d cols %d-%d", r0, r1, c0, c1)
		}
		e.press(key.NameDownArrow, key.ModShift|key.ModCtrl)
		if _, r1, _, _, _, _ = g.selection(); r1 != len(g.nav)-1 {
			t.Fatalf("Ctrl+Shift+↓: last row %d", r1)
		}
		e.press(key.NameRightArrow, 0)
		if _, _, _, _, multi, _ := g.selection(); multi {
			t.Fatal("a plain arrow kept the selection")
		}
	})
}

// Copy puts the cells as tab-separated text (copyValue: labels, raw ids)
// with Slack's toast; paste writes from the active cell; Clack's own
// multi-row copy pastes as rows, like Slack's slack/texty flavour.
func TestListKeysCopyPaste(t *testing.T) {
	eachMode(t, func(t *testing.T, mode string) {
		e := newKB(t, mode, "", false)
		e.focus()
		e.goTo(rec1, "Col0STATUS1")
		e.press("C", key.ModShortcut)
		if got := e.clipboard(); got != "In progress" || e.h.lastToast() != "Cell copied" {
			t.Fatalf("copy: %q, toast %q", got, e.h.lastToast())
		}
		e.press(key.NameRightArrow, key.ModShift)
		e.press(key.NameDownArrow, key.ModShift)
		e.press("C", key.ModShortcut)
		want := "In progress\t" + fakeslack.SelfID + "\nBlocked\tU0FAKE00002"
		if got := e.clipboard(); got != want || e.h.lastToast() != "Cells copied" {
			t.Fatalf("copy: %q, toast %q", got, e.h.lastToast())
		}
		e.goTo(rec4, "Col0STATUS1")
		e.press("V", key.ModShortcut)
		if !e.r.ClipboardRequested() {
			t.Fatal("paste didn't read the clipboard")
		}
		e.r.Queue(transfer.DataEvent{Type: "application/text", Open: func() io.ReadCloser { return io.NopCloser(strings.NewReader(want)) }})
		e.frames(3)
		e.until("the pasted block", func() bool {
			s := e.stored(rec4, "Col0STATUS1")
			o := e.stored(rec5, "Col0OWNER01")
			return s != nil && len(s.Select) == 1 && s.Select[0] == "OptPROG0001" && o != nil && len(o.User) == 1 && o.User[0] == "U0FAKE00002"
		})
		// plain text from elsewhere: rows run together into one cell
		if got := parseClipboard("a\nb", false); len(got) != 1 || got[0][0] != "ab" {
			t.Fatalf("foreign rows %q", got)
		}
		if got := parseClipboard("a\tb", false); len(got) != 1 || len(got[0]) != 2 {
			t.Fatalf("foreign row %q", got)
		}
	})
}

// Backspace/Delete empty the selected cells; a selection across every
// column deletes the items (deleteRecords).
func TestListKeysDelete(t *testing.T) {
	eachMode(t, func(t *testing.T, mode string) {
		e := newKB(t, mode, "", false)
		e.focus()
		e.goTo(rec3, "Col0EST0001")
		e.press(key.NameDeleteBackward, 0)
		e.until("the estimate cleared", func() bool { f := e.stored(rec3, "Col0EST0001"); return f == nil || len(f.Number) == 0 })
		e.goTo(rec5, "name")
		for range e.v.grid.cols {
			e.press(key.NameRightArrow, key.ModShift)
		}
		if !e.v.grid.wholeRows() {
			t.Fatal("Shift+→ across the row didn't select it whole")
		}
		e.press(key.NameDeleteForward, 0)
		e.until("the item deleted", func() bool { return !e.exists(rec5) })
		e.until("the toast", func() bool { return e.h.lastToast() == "Item deleted" })
	})
}

// Shift+Enter adds an item (its name editor); Alt+Enter opens the item
// (onRowOpenKeyboardEvent); Enter on the last row's name adds one in
// Data entry mode.
func TestListKeysAddAndOpen(t *testing.T) {
	eachMode(t, func(t *testing.T, mode string) {
		e := newKB(t, mode, "", false)
		e.focus()
		e.goTo(rec3, "Col0STATUS1")
		e.press(key.NameReturn, key.ModAlt)
		e.mu.Lock()
		opened := append([]string(nil), e.opened...)
		e.mu.Unlock()
		if len(opened) != 1 || opened[0] != rec3 {
			t.Fatalf("Alt+Enter opened %q", opened)
		}
		e.press(key.NameReturn, key.ModShift)
		if !e.v.edit.active || e.v.edit.rec != "" {
			t.Fatal("Shift+Enter didn't open a new item")
		}
		e.press(key.NameEscape, 0)
		last := e.v.grid.cell(len(e.v.grid.nav)-1, 0)
		e.goTo(last.rec, "name")
		e.press(key.NameReturn, 0)
		e.v.edit.ed.SetText("Last row")
		e.press(key.NameReturn, 0)
		newItem := e.v.edit.active && e.v.edit.rec == ""
		if newItem != (mode == "tab_nav") {
			t.Fatalf("Enter on the last row's name: new item editor %v", newItem)
		}
	})
}

// The screen-reader labels Slack's cells carry (x8/8, 3VkJ nE).
func TestListKeysLabels(t *testing.T) {
	e := newKB(t, "", "", false)
	e.frames(2)
	want := map[string]string{"4 of 5, star": "", "+1, 3 votes": "Including you", "Yes": "", "No": ""}
	got := map[string]string{}
	readOnly := false
	read := func() {
		for _, n := range e.semantics() {
			if _, ok := want[n.Desc.Label]; ok {
				got[n.Desc.Label] = n.Desc.Description
			}
			if n.Desc.Description == "Read only" {
				readOnly = true
			}
		}
	}
	read()
	e.focus()
	e.goTo(rec1, "Col0VOTE001") // scrolls the columns past the first screen into view
	read()
	e.press(key.NameEnd, 0)
	read()
	for l, d := range want {
		if g, ok := got[l]; !ok || g != d {
			t.Errorf("label %q: description %q (found %v)", l, g, ok)
		}
	}
	if !readOnly {
		t.Error("no computed cell says Read only")
	}
	if l := cellLabel(nil, &lists.Column{Type: lists.Vote, Options: lists.Options{Emoji: ":+1:"}}, &lists.Field{User: []string{"U1"}}); l != "+1, 1 vote" {
		t.Errorf("one vote: %q", l)
	}
}

// The mode follows the pref and the experiment (3VkJ tg; useKeyCommands).
func TestListKeysMode(t *testing.T) {
	for _, c := range []struct {
		pref, exp string
		want      kbMode
	}{
		{"", "on", kbMode{tabbing: true}},
		{"tab_nav", "on", kbMode{tabbing: true, tabKeys: true}},
		{"arrow_nav", "on", kbMode{arrowNav: true}},
		{"tab_nav", "off", kbMode{}},
	} {
		e := newKB(t, c.pref, "", false)
		e.h.a.SetExperimentGroup(ExpListsTabbing, c.exp)
		if got := e.v.kbMode(); got != c.want {
			t.Errorf("pref %q exp %q: %+v, want %+v", c.pref, c.exp, got, c.want)
		}
	}
}

// Screenshots: the focus ring, a selection and the edit states, light and
// dark (LISTS_SHOTS_DIR).
func TestListKeysShots(t *testing.T) {
	dir := os.Getenv("LISTS_SHOTS_DIR")
	for _, dark := range []bool{false, true} {
		e := newKB(t, "tab_nav", "", dark)
		e.sz = image.Pt(1200, 560)
		if dir != "" {
			w, err := pixeltest.NewWindow(e.sz.X, e.sz.Y)
			if err != nil {
				t.Fatalf("headless window: %v", err)
			}
			e.win = w
			t.Cleanup(w.Release)
		}
		e.frames(2)
		e.focus()
		e.goTo(rec3, "Col0STATUS1")
		e.shot(dir, "lists-kb-ring-"+mode(dark))
		e.press(key.NameRightArrow, key.ModShift)
		e.press(key.NameDownArrow, key.ModShift)
		e.shot(dir, "lists-kb-selection-"+mode(dark))
		e.goTo(rec3, "name")
		e.typeText("!")
		e.shot(dir, "lists-kb-edit-"+mode(dark))
		e.press(key.NameEscape, 0)
		e.goTo(rec3, "Col0PRIO001")
		e.press(key.NameReturn, 0)
		e.press(key.NameRightArrow, 0)
		e.shot(dir, "lists-kb-rating-"+mode(dark))
		e.press(key.NameEscape, 0)
	}
}

func (e *kbEnv) shot(dir, name string) {
	e.t.Helper()
	for i := 0; i < 4; i++ { // images arrive asynchronously
		e.frame()
		time.Sleep(30 * time.Millisecond)
	}
	e.frame()
	if e.win == nil {
		return
	}
	if err := e.win.Frame(e.last); err != nil {
		e.t.Fatalf("frame: %v", err)
	}
	img := image.NewRGBA(image.Rectangle{Max: e.sz})
	if err := e.win.Screenshot(img); err != nil {
		e.t.Fatalf("screenshot: %v", err)
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		e.t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		e.t.Fatal(err)
	}
}
