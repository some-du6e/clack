package ui

// List records outside the grid (listview/records.go, views/later_records.go)
// in the whole client on the fake Slack: a list row's right-click menu
// (BaseRecordContextMenu) with lists_in_later's Remind me about this ▸,
// the item pane's actions row, and Later's ListRecord rows on the To-dos
// and Saved pages; with the experiment off, no reminder row and an
// untranslated "List item". Light and dark shots (list-record-*.png) with
// LISTS_SHOTS_DIR.

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/listview"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

func lrShot(e *perfEnv, name string) {
	dir := os.Getenv("LISTS_SHOTS_DIR")
	if dir == "" || e.gpu == nil {
		return
	}
	for _, dark := range []bool{false, true} {
		e.setDark(dark)
		e.settle(300 * time.Millisecond)
		e.frame()
		e.frame()
		img := image.NewRGBA(image.Rectangle{Max: e.size})
		if err := e.gpu.Screenshot(img); err != nil {
			e.tb.Fatalf("screenshot: %v", err)
		}
		f, err := os.Create(filepath.Join(dir, name+"-"+map[bool]string{false: "light", true: "dark"}[dark]+".png"))
		if err != nil {
			e.tb.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			e.tb.Fatal(err)
		}
		f.Close()
	}
	e.setDark(false)
	e.settle(200 * time.Millisecond)
}

func lrEnv(t *testing.T) (*perfEnv, *fakeslack.Server) {
	sz := fakeslack.Small
	sz.ListRecordsInLater = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	e.size = image.Pt(1280, 820)
	if os.Getenv("LISTS_SHOTS_DIR") != "" {
		w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	go e.a.Core().SetRealtime(true)
	e.until("the socket", 15*time.Second, func() bool { return e.a.data.m.Connected })
	return e, fs
}

func TestListRecordMenusShots(t *testing.T) {
	e, _ := lrEnv(t)
	if g := e.a.Core().Experiment("lists_in_later"); g != "on" {
		t.Fatalf("lists_in_later is %q in fakeslack, want the account's on", g)
	}
	e.a.Navigate("lists/" + fakeslack.ListKitchen)
	var v *listview.View
	e.until("the list", 15*time.Second, func() bool {
		v, _ = e.a.views[Route("lists/"+fakeslack.ListKitchen)].(*listview.View)
		return v != nil && v.Doc() != nil
	})
	e.move(f32.Pt(float32(e.size.X/2), float32(e.size.Y/2)))
	var p image.Point
	e.until("the row", 10*time.Second, func() bool {
		var ok bool
		p, ok = v.RowPoint(fakeslack.SavedRecordSaved)
		return ok
	})
	e.move(f32.Pt(float32(p.X), float32(p.Y)))
	e.rightClickAt(p, time.Duration(time.Now().UnixNano()))
	e.until("the record menu", 5*time.Second, func() bool {
		m := e.topMenu()
		return m != nil && slices.Contains(m.Labels(), "Open item")
	})
	m := e.topMenu()
	want := []string{"Open item", "Add comment", "Copy link to item", "Duplicate item", "Remind me about this", "Archive item", "Delete item"}
	got := []string{}
	for _, l := range m.Labels() {
		if l != "---" {
			got = append(got, l)
		}
	}
	if !slices.Equal(got, want) {
		t.Errorf("record menu %q, want %q", got, want)
	}
	lrShot(e, "list-record-menu")
	if !m.OpenSubmenu("Remind me about this") {
		t.Fatal("no Remind me about this submenu")
	}
	e.settle(300 * time.Millisecond)
	lrShot(e, "list-record-remind")
	e.key(key.NameEscape, 0)
	e.key(key.NameEscape, 0)
	e.settle(200 * time.Millisecond)

	// the item pane: Add Comment and More actions under the title
	e.a.OpenListItem(fakeslack.ListKitchen, fakeslack.SavedRecordTodo)
	e.until("the item pane", 10*time.Second, func() bool {
		it, ok := e.a.secondary.view.(*listview.Item)
		return ok && it.RecordID() == fakeslack.SavedRecordTodo
	})
	e.settle(800 * time.Millisecond)
	lrShot(e, "list-record-pane")
}

func TestLaterListRecordRows(t *testing.T) {
	e, _ := lrEnv(t)
	for _, c := range []struct{ route, rec, shot string }{
		{"later", fakeslack.SavedRecordTodo, "list-record-later-todos"},
		{"saved", fakeslack.SavedRecordSaved, "list-record-later-saved"},
	} {
		e.a.Navigate(c.route)
		e.until("the list record's row on "+c.route, 15*time.Second, func() bool {
			e.frame()
			info, ok := e.a.Core().SavedListRecordInfo(fakeslack.ListKitchen, c.rec)
			return ok && info.ListTitle == "Launch tracker" && info.Name != ""
		})
		e.settle(800 * time.Millisecond)
		lrShot(e, c.shot)
	}
	info, _ := e.a.Core().SavedListRecordInfo(fakeslack.ListKitchen, fakeslack.SavedRecordTodo)
	if info.Name != "Write the launch announcement" || info.Deleted {
		t.Errorf("the to-do's row: %+v", info)
	}
}
