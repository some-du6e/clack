package listview

// lists_sub_tasks (subtasks.go) against fakeslack: subtasks made with
// lists.records.create {parent_record_id}; on, the grid's summary and new
// ThreadIndicator and the hover Open button, the board card's summary,
// the record pane's Subtasks section (Add Subtask writes a subtask and
// opens its name), a subtask's "Subtask of"; off, none of them; "Open
// subtasks" in the record menu in both groups. Light and dark shots
// (list-subtasks-grid-*, list-subtasks-thread-*, list-subtasks-pane-*,
// list-subtask-pane-*).

import (
	"context"
	"image"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/lists"
	"github.com/some-du6e/clack/internal/slack"
)

const (
	todoParent = "Rec0TODO002" // Order stickers
	todoPlain  = "Rec0TODO001"
)

// seedSubtasks gives the to-do list's "Order stickers" two subtasks (one
// done, one assigned with a due date) and the kitchen's first item three,
// and the kitchen item's comments root a followed, unread thread.
func seedSubtasks(t *testing.T, a *core.App) (done, open string) {
	t.Helper()
	ctx := context.Background()
	d, err := a.List(ctx, fakeslack.ListTodos)
	if err != nil {
		t.Fatal(err)
	}
	name, tc, who, due := d.Meta.Column("name"), d.Meta.Column(lists.TodoCompleted), d.Meta.Column("todo_assignee"), d.Meta.Column("todo_due_date")
	r1, err := a.CreateListRecord(ctx, fakeslack.ListTodos, "", todoParent, lists.Edit{Column: name, Text: "Pick a sticker shop"}, lists.Edit{Column: tc, Check: true})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := a.CreateListRecord(ctx, fakeslack.ListTodos, "", todoParent, lists.Edit{Column: name, Text: "Approve the proof"},
		lists.Edit{Column: who, Values: []string{"U0FAKE00001"}}, lists.Edit{Column: due, Values: []string{"2026-11-20"}})
	if err != nil {
		t.Fatal(err)
	}
	k, err := a.List(ctx, fakeslack.ListKitchen)
	if err != nil {
		t.Fatal(err)
	}
	kp := k.Meta.Primary()
	for _, n := range []string{"Draft", "Review", "Publish"} {
		if _, err := a.CreateListRecord(ctx, fakeslack.ListKitchen, "", rec1, lists.Edit{Column: kp, Text: n}); err != nil {
			t.Fatal(err)
		}
	}
	root := "1762500000.000300"
	a.S.SetLatest(core.ListCommentsChannel(fakeslack.ListKitchen), []slack.Message{{Type: "message", Subtype: "list_record_comment", TS: root, ThreadTS: root,
		ReplyCount: 2, LatestReply: "1762500200.000100", Subscribed: true, LastRead: "1762500100.000100"}}, false)
	return r1.ID, r2.ID
}

type subEnv struct {
	t      *testing.T
	h      *testHost
	fs     *fakeslack.Server
	r      *input.Router
	sz     image.Point
	mu     sync.Mutex
	opened []string
	menus  [][]MenuItem
}

func (e *subEnv) act() Actions {
	return Actions{
		OpenItem: func(_, rec string) { e.mu.Lock(); e.opened = append(e.opened, rec); e.mu.Unlock() },
		Copy:     func(string) {},
		Menu:     func(m []MenuItem) { e.mu.Lock(); e.menus = append(e.menus, m); e.mu.Unlock() },
	}
}

func (e *subEnv) frame(w func(gtx layout.Context)) {
	ops := new(op.Ops)
	gtx := layout.Context{Ops: ops, Now: time.Now(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(e.sz), Source: e.r.Source()}
	w(gtx)
	e.r.Frame(ops)
}

func (e *subEnv) until(what string, w func(gtx layout.Context), cond func() bool) {
	e.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			e.t.Fatalf("timed out waiting for %s", what)
		}
		e.frame(w)
		time.Sleep(10 * time.Millisecond)
	}
}

func (e *subEnv) click(w func(gtx layout.Context), p image.Point) {
	pt := f32.Pt(float32(p.X), float32(p.Y))
	e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pt},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pt},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pt})
	e.frame(w)
	e.frame(w)
}

func (e *subEnv) move(w func(gtx layout.Context), p image.Point) {
	e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(p.X), float32(p.Y))})
	e.frame(w)
	e.frame(w)
}

func (e *subEnv) lastOpened() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.opened) == 0 {
		return ""
	}
	return e.opened[len(e.opened)-1]
}

// rowMid is a record row's middle in the grid, 80px into its first cell.
func rowMid(v *View, rec string) (image.Point, bool) {
	g := &v.grid
	for _, ln := range g.lines {
		if ln.kind == lineRow && ln.row.Rec != nil && ln.row.Rec.ID == rec {
			return image.Pt(g.x0+80, ln.y-g.scrollY+ln.h/2), true
		}
	}
	return image.Point{}, false
}

func actOf(v *View, rec string, kind int) (image.Rectangle, bool) {
	for _, a := range v.grid.acts {
		if a.rec == rec && a.kind == kind {
			return a.r, true
		}
	}
	return image.Rectangle{}, false
}

func TestSubtasksSummary(t *testing.T) {
	for _, c := range []struct {
		done, total int
		todo        bool
		text, icon  string
	}{
		{0, 2, true, "0/2", "subtask"}, {1, 2, true, "1/2", "subtask-progress"}, {2, 2, true, "2/2", "subtask-complete"},
		{1, 3, false, "3", "subtask-progress"}, {100, 120, true, "99+/99+", "subtask-progress"}, {5, 120, true, "5/99+", "subtask-progress"},
	} {
		if got := summaryText(c.done, c.total, c.todo); got != c.text {
			t.Errorf("summaryText(%d, %d, %v) = %q, want %q", c.done, c.total, c.todo, got, c.text)
		}
		if got := summaryIcon(c.done, c.total); got != c.icon {
			t.Errorf("summaryIcon(%d, %d) = %q, want %q", c.done, c.total, got, c.icon)
		}
	}
	if threadCount(9) != "9" || threadCount(10) != "9+" {
		t.Errorf("thread counts %s %s", threadCount(9), threadCount(10))
	}
}

func TestSubtasks(t *testing.T) {
	h, fs := newEnv(t, false)
	a := h.a
	a.SetExperimentGroup("lists_in_later", "off")
	done, open := seedSubtasks(t, a)
	e := &subEnv{t: t, h: h, fs: fs, r: new(input.Router), sz: image.Pt(1200, 700)}
	v := New(h, fakeslack.ListTodos, Full, e.act())
	w := func(gtx layout.Context) { v.Layout(gtx) }
	e.until("the to-do list", w, func() bool { d := v.Doc(); return d != nil && len(subtasksOf(d, todoParent)) == 2 })
	d := v.Doc()
	if got := subtasksOf(d, todoParent); got[0].ID != done || got[1].ID != open {
		t.Fatalf("subtasks in order %s, %s; want %s, %s", got[0].ID, got[1].ID, done, open)
	}
	if dn, tot := subtaskProgress(d, todoParent); dn != 1 || tot != 2 {
		t.Fatalf("progress %d/%d, want 1/2", dn, tot)
	}
	// subtasks are never rows
	e.frame(w)
	for _, ln := range v.grid.lines {
		if ln.kind == lineRow && ln.row.Rec.ParentRecordID != "" {
			t.Errorf("subtask %s is a grid row", ln.row.Rec.ID)
		}
	}
	// "Open subtasks" in the record menu, in both groups
	menu := func(rec string) string { return moreLabels(v.recordMenu(d, d.Record(rec), false)) }
	for _, g := range []string{"off", "on"} {
		a.SetExperimentGroup("lists_sub_tasks", g)
		if got := menu(todoParent); !strings.HasPrefix(got, "Open item, Open subtasks, Add comment") {
			t.Errorf("%s: the parent's menu %s", g, got)
		}
		if got := menu(todoPlain); strings.Contains(got, "Open subtasks") {
			t.Errorf("%s: an item without subtasks: %s", g, got)
		}
	}

	// off: no summary, no buttons
	a.SetExperimentGroup("lists_sub_tasks", "off")
	mid, ok := rowMid(v, todoParent)
	if !ok {
		t.Fatal("no parent row")
	}
	mid = mid.Add(v.gridAt) // the router's coordinates
	e.move(w, mid)
	if len(v.grid.acts) != 0 {
		t.Errorf("off: primary-cell buttons %v", v.grid.acts)
	}
	if v.cardSummary(d, d.Record(todoParent)) {
		t.Error("off: the board card shows the summary")
	}

	// on: the summary at rest, Open item on hover; a click on the summary
	// opens the item
	a.SetExperimentGroup("lists_sub_tasks", "on")
	e.move(w, image.Pt(5, 5))
	if _, ok := actOf(v, todoParent, actSummary); !ok {
		t.Fatal("on: no summary on the parent's row")
	}
	if _, ok := actOf(v, todoParent, actOpen); ok {
		t.Error("on: Open item shown without hover")
	}
	if _, ok := actOf(v, todoPlain, actSummary); ok {
		t.Error("on: a summary on an item without subtasks")
	}
	e.move(w, mid)
	or, ok := actOf(v, todoParent, actOpen)
	if !ok {
		t.Fatal("on: no Open item button on hover")
	}
	if right := v.grid.x0 + v.grid.cols[0].w; or.Max.X != right-10 || or.Dx() != 32 || or.Dy() != 28 {
		t.Errorf("Open item at %v (cell right %d)", or, right)
	}
	sr, _ := actOf(v, todoParent, actSummary)
	if sr.Max.X != v.grid.x0+v.grid.cols[0].w-8-34 {
		t.Errorf("summary ends at %d, want the cell's right less 42", sr.Max.X)
	}
	e.click(w, sr.Min.Add(image.Pt(10, sr.Dy()/2)).Add(v.gridAt))
	if got := e.lastOpened(); got != todoParent {
		t.Errorf("summary click opened %q", got)
	}
	if !v.cardSummary(d, d.Record(todoParent)) && v.gridTodos(d) {
		t.Error("on: the board card has no summary")
	}

	// the kitchen list (not a to-do list): the total and the unread thread
	kv := New(h, fakeslack.ListKitchen, Full, e.act())
	kw := func(gtx layout.Context) { kv.Layout(gtx) }
	e.until("the kitchen list", kw, func() bool { d := kv.Doc(); return d != nil && len(subtasksOf(d, rec1)) == 3 })
	e.frame(kw)
	ir, ok := actOf(kv, rec1, actThread)
	if !ok {
		t.Fatal("on: no thread indicator on the commented item")
	}
	if !kv.threadUnread(kv.Doc(), kv.Doc().Record(rec1)) {
		t.Error("the followed thread with a newer reply isn't unread")
	}
	kr, _ := actOf(kv, rec1, actSummary)
	if kr.Max.X != ir.Min.X-2 || ir.Max.X != kv.grid.x0+kv.grid.cols[0].w-8-20 {
		t.Errorf("summary %v, indicator %v (cell right %d)", kr, ir, kv.grid.x0+kv.grid.cols[0].w)
	}

	// the pane: the section, Add Subtask
	it := NewItem(h, fakeslack.ListTodos, todoParent, e.act())
	iw := func(gtx layout.Context) { it.Layout(gtx) }
	e.sz = image.Pt(480, 900)
	e.until("the item", iw, func() bool { return it.doc != nil && it.doc.Record(todoParent) != nil })
	e.frame(iw)
	count := func(act string) (n int, r image.Rectangle) {
		for _, h := range it.hits {
			if h.act == act {
				n++
				r = h.r
			}
		}
		return n, r
	}
	if n, _ := count("subtask-row"); n != 2 {
		t.Errorf("pane rows %d, want 2", n)
	}
	n, add := count("subtask-add")
	if n != 1 {
		t.Fatal("no Add Subtask")
	}
	before := len(fs.ListRecordCreates())
	e.click(iw, add.Min.Add(image.Pt(4, 4)).Sub(image.Pt(0, it.scroll)).Add(image.Pt(0, 49)))
	e.until("the new subtask", iw, func() bool { return len(fs.ListRecordCreates()) > before })
	cr := fs.ListRecordCreates()[len(fs.ListRecordCreates())-1]
	if cr.Get("parent_record_id") != todoParent || cr.Get("list_id") != fakeslack.ListTodos {
		t.Errorf("records.create %v", cr)
	}
	e.until("the new subtask's name editor", iw, func() bool {
		it.mu.Lock()
		d := it.doc
		it.mu.Unlock()
		subs := subtasksOf(d, todoParent)
		return len(subs) == 3 && it.edit.active && it.edit.rec == subs[2].ID
	})
	// the subtask's menu
	sm := moreLabels(it.subtaskMenu(it.doc, it.doc.Record(open)))
	if sm != "Open subtask, Add comment, Copy link to subtask, ─, Delete subtask" {
		t.Errorf("subtask menu %s", sm)
	}
	// off: no section
	a.SetExperimentGroup("lists_sub_tasks", "off")
	e.frame(iw)
	if n, _ := count("subtask-row"); n != 0 {
		t.Errorf("off: %d subtask rows in the pane", n)
	}
	a.SetExperimentGroup("lists_sub_tasks", "on")

	// a subtask's pane: Subtask of, the link opens the parent
	sp := NewItem(h, fakeslack.ListTodos, done, e.act())
	sw := func(gtx layout.Context) { sp.Layout(gtx) }
	e.until("the subtask", sw, func() bool { return sp.doc != nil && sp.doc.Record(done) != nil })
	e.frame(sw)
	var pr image.Rectangle
	for _, h := range sp.hits {
		if h.act == "subtask-parent" {
			pr = h.r
		}
	}
	if pr.Empty() {
		t.Fatal("no Subtask of link")
	}
	e.click(sw, pr.Min.Add(image.Pt(4, 4)).Add(image.Pt(0, 49)))
	if got := e.lastOpened(); got != todoParent {
		t.Errorf("Subtask of opened %q", got)
	}
}

func TestSubtasksShots(t *testing.T) {
	for _, dark := range []bool{false, true} {
		md := mode(dark)
		h, _ := newEnv(t, dark)
		h.a.SetExperimentGroup("lists_sub_tasks", "on")
		h.a.SetExperimentGroup("lists_in_later", "off")
		done, _ := seedSubtasks(t, h.a)
		act := Actions{Copy: func(string) {}, Menu: func([]MenuItem) {}}
		v := New(h, fakeslack.ListTodos, Full, act)
		s := newShooter(t, image.Pt(900, 260))
		w := func(gtx layout.Context) { v.Layout(gtx) }
		settle(t, s, w, "the to-do list", func() bool { d := v.Doc(); return d != nil && len(subtasksOf(d, todoParent)) == 2 })
		s.frame(w)
		if p, ok := rowMid(v, todoParent); ok {
			v.grid.hover, v.grid.hoverX, v.grid.hoverY = v.lineAt(p.Y), p.X, p.Y
		}
		s.shot("list-subtasks-grid-"+md, w)

		kv := New(h, fakeslack.ListKitchen, Full, act)
		kw := func(gtx layout.Context) { kv.Layout(gtx) }
		settle(t, s, kw, "the kitchen list", func() bool { d := kv.Doc(); return d != nil && len(subtasksOf(d, rec1)) == 3 })
		s.shot("list-subtasks-thread-"+md, kw)

		ps := newShooter(t, image.Pt(480, 760))
		it := NewItem(h, fakeslack.ListTodos, todoParent, act)
		iw := func(gtx layout.Context) { it.Layout(gtx) }
		settle(t, ps, iw, "the item", func() bool { return it.doc != nil && it.doc.Record(todoParent) != nil })
		ps.frame(iw)
		for _, hh := range it.hits { // hover the second subtask: its handle and Open subtask
			if hh.act == "subtask-row" && hh.rec != done {
				it.hover = hh.r.Min.Add(image.Pt(60, 20)).Sub(image.Pt(0, it.scroll))
			}
		}
		ps.shot("list-subtasks-pane-"+md, iw)

		sp := NewItem(h, fakeslack.ListTodos, done, act)
		sw := func(gtx layout.Context) { sp.Layout(gtx) }
		settle(t, ps, sw, "the subtask", func() bool { return sp.doc != nil && sp.doc.Record(done) != nil })
		ps.shot("list-subtask-pane-"+md, sw)
	}
}
