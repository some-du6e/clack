package listview

// A record's menus (records.go): the grid row's right-click
// BaseRecordContextMenu and the record pane's More actions, with
// lists_in_later's Remind me about this on and off, against fakeslack.

import (
	"image"
	"strconv"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func (e *kbEnv) rightClick(p image.Point) {
	pt := f32.Pt(float32(p.X), float32(p.Y))
	e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pt},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonSecondary, Position: pt},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pt})
	e.frames(3)
}

func (e *kbEnv) menuLabels() string {
	e.mr.mu.Lock()
	defer e.mr.mu.Unlock()
	return moreLabels(e.mr.items)
}

func (e *kbEnv) savedRecord(rec string) map[string]any {
	return e.fs.SavedListRecord(fakeslack.ListKitchen, rec)
}

const remindSub = "In 30 mins, In 1 hour, In 3 hours, Tomorrow at 9:00 AM, Monday at 9:00 AM, Custom…"

func TestRecordContextMenu(t *testing.T) {
	e := newKB(t, "", "", false)
	e.h.a.SetExperimentGroup("lists_in_later", "on")
	e.rightClick(e.cellCenter(rec3, "Col0STATUS1"))
	want := "Open item, Add comment, Copy link to item, ─, Duplicate item, ─, Remind me about this ▸ (" + remindSub + "), ─, Archive item, Delete item"
	if got := e.menuLabels(); got != want {
		t.Fatalf("menu:\n got %s\nwant %s", got, want)
	}
	if e.menu("Delete item").Danger != true {
		t.Error("Delete item isn't a danger row")
	}

	// Open item opens the pane
	e.pick("Open item")
	e.mu.Lock()
	opened := strings.Join(e.opened, ",")
	e.mu.Unlock()
	if opened != rec3 {
		t.Errorf("opened %q", opened)
	}

	// Remind me about this ▸ In 1 hour: saved.add {item_type list_record, item_id, ts, date_due}
	e.rightClick(e.cellCenter(rec3, "Col0STATUS1"))
	before := time.Now()
	for _, s := range e.menu("Remind me about this").Sub {
		if s.Label == "In 1 hour" {
			s.Do()
		}
	}
	e.until("the saved record", func() bool { return e.savedRecord(rec3) != nil })
	w := e.fs.ListRecordSaveWrites()
	if len(w) != 1 || w[0].Get("item_type") != "list_record" || w[0].Get("item_id") != fakeslack.ListKitchen || w[0].Get("ts") != rec3 {
		t.Fatalf("saved.add %v", w)
	}
	if due := w[0].Get("date_due"); due < strconv.FormatInt(before.Add(time.Hour).Unix()-5, 10) || due > strconv.FormatInt(time.Now().Add(time.Hour).Unix()+5, 10) {
		t.Errorf("date_due %s", due)
	}
	e.until("the saved state", func() bool { _, ok := e.h.a.ListRecordSaved(fakeslack.ListKitchen, rec3); return ok })

	// now Edit reminder, with its description and Clear reminder
	e.rightClick(e.cellCenter(rec3, "Col0STATUS1"))
	it := e.menu("Edit reminder")
	if !strings.HasPrefix(it.Subtitle, "Due in ") {
		t.Errorf("description %q", it.Subtitle)
	}
	if got := moreLabels(it.Sub); got != remindSub+", ─, Clear reminder" {
		t.Errorf("Edit reminder ▸ %s", got)
	}
	for _, s := range it.Sub {
		if s.Label == "Clear reminder" {
			s.Do()
		}
	}
	// a to-do (a reminder makes one): updateSavedListRecord {dateDue: dateCreated}
	e.until("the clear", func() bool { return len(e.fs.ListRecordSaveWrites()) == 2 })
	if w := e.fs.ListRecordSaveWrites()[1]; w.Get("item_type") != "list_record" || w.Get("ts") != rec3 || !w.Has("date_due") {
		t.Errorf("clear: %v", w)
	}

	// lists_in_later off: no reminder row
	e.h.a.SetExperimentGroup("lists_in_later", "off")
	e.rightClick(e.cellCenter(rec3, "Col0STATUS1"))
	if got := e.menuLabels(); got != "Open item, Add comment, Copy link to item, ─, Duplicate item, ─, Archive item, Delete item" {
		t.Errorf("off: %s", got)
	}

	// Duplicate item: lists.records.create {duplicated_record_id}
	e.pick("Duplicate item")
	e.until("the copy", func() bool {
		for _, c := range e.fs.ListRecordCreates() {
			if c.Get("duplicated_record_id") == rec3 {
				return true
			}
		}
		return false
	})

	// Archive item: lists.records.archive {list_id, ids}, and the row goes
	e.rightClick(e.cellCenter(rec4, "Col0STATUS1"))
	e.pick("Archive item")
	e.until("the archive", func() bool { return len(e.fs.ListArchives()) == 1 })
	if a := e.fs.ListArchives()[0]; a.Get("list_id") != fakeslack.ListKitchen || a.Get("ids") != `["`+rec4+`"]` {
		t.Errorf("archive %v", a)
	}
	e.until("the archived row to go", func() bool { return !e.exists(rec4) })
}

// The record pane's More actions (the ⋮ beside Add Comment).
func TestRecordPaneMenu(t *testing.T) {
	h, _ := newEnv(t, false)
	h.a.SetExperimentGroup("lists_in_later", "on")
	mr := &menuRec{}
	act := mr.act()
	act.Copy = func(string) {}
	it := NewItem(h, fakeslack.ListKitchen, rec1, act)
	sz := image.Pt(520, 820)
	router := new(input.Router)
	frame := func() {
		ops := new(op.Ops)
		gtx := layout.Context{Ops: ops, Now: time.Now(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(sz), Source: router.Source()}
		it.Layout(gtx)
		router.Frame(ops)
	}
	deadline := time.Now().Add(20 * time.Second)
	for it.moreAt.Empty() {
		if time.Now().After(deadline) {
			t.Fatal("the pane never drew its actions")
		}
		frame()
		time.Sleep(10 * time.Millisecond)
	}
	frame()
	c := it.moreAt.Min.Add(it.moreAt.Size().Div(2)).Add(image.Pt(0, int(theme.HeaderH)-it.scroll)) // below the pane's bar
	pt := f32.Pt(float32(c.X), float32(c.Y))
	router.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pt},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pt})
	frame()
	frame()
	mr.mu.Lock()
	got := moreLabels(mr.items)
	mr.mu.Unlock()
	if want := "Copy link to item, ─, Remind me about this ▸ (" + remindSub + "), ─, Archive item, Delete item"; got != want {
		t.Errorf("pane menu:\n got %s\nwant %s", got, want)
	}
}
