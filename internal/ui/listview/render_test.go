package listview

// Synthetic fixtures only (scraping policy): the lists come from
// internal/fakeslack, made by hand.

import (
	"context"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/lists"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

type testHost struct {
	a      *core.App
	th     *theme.Theme
	img    *imgcache.Cache
	mu     sync.Mutex
	routes []string
	toasts []string
}

func (h *testHost) Core() *core.App                               { return h.a }
func (h *testHost) Theme() *theme.Theme                           { return h.th }
func (h *testHost) Images() *imgcache.Cache                       { return h.img }
func (h *testHost) Invalidate()                                   {}
func (h *testHost) OpenProfile(anchor image.Point, userID string) {}
func (h *testHost) Navigate(r string)                             { h.mu.Lock(); h.routes = append(h.routes, r); h.mu.Unlock() }
func (h *testHost) Toast(s string)                                { h.mu.Lock(); h.toasts = append(h.toasts, s); h.mu.Unlock() }

func (h *testHost) lastToast() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.toasts) == 0 {
		return ""
	}
	return h.toasts[len(h.toasts)-1]
}

func newEnv(t *testing.T, dark bool) (*testHost, *fakeslack.Server) {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	a := core.New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(a.Shutdown)
	if err := a.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	h := &testHost{a: a, th: theme.New(dark)}
	h.img = imgcache.New(func(ctx context.Context, u string) ([]byte, error) {
		b, _, err := a.Fetch(ctx, u)
		return b, err
	}, "", nil)
	h.th.Images = h.img
	return h, fs
}

func gtxFor(ops *op.Ops, sz image.Point) layout.Context {
	return layout.Context{Ops: ops, Now: time.Now(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(sz)}
}

type shooter struct {
	t   *testing.T
	dir string
	win *headless.Window
	sz  image.Point
}

func newShooter(t *testing.T, sz image.Point) *shooter {
	s := &shooter{t: t, dir: os.Getenv("LISTS_SHOTS_DIR"), sz: sz}
	if s.dir != "" {
		w, err := pixeltest.NewWindow(sz.X, sz.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		s.win = w
		t.Cleanup(w.Release)
	}
	return s
}

func (s *shooter) frame(w func(gtx layout.Context)) *op.Ops {
	ops := new(op.Ops)
	w(gtxFor(ops, s.sz))
	return ops
}

func (s *shooter) shot(name string, w func(gtx layout.Context)) {
	// a few frames first: images arrive asynchronously
	for i := 0; i < 6; i++ {
		s.frame(w)
		time.Sleep(30 * time.Millisecond)
	}
	ops := s.frame(w)
	if s.win == nil {
		return
	}
	if err := s.win.Frame(ops); err != nil {
		s.t.Fatalf("frame: %v", err)
	}
	img := image.NewRGBA(image.Rectangle{Max: s.sz})
	if err := s.win.Screenshot(img); err != nil {
		s.t.Fatalf("screenshot: %v", err)
	}
	f, err := os.Create(filepath.Join(s.dir, name+".png"))
	if err != nil {
		s.t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		s.t.Fatal(err)
	}
}

func settle(t *testing.T, s *shooter, w func(gtx layout.Context), what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		s.frame(w)
		time.Sleep(10 * time.Millisecond)
	}
}

func mode(dark bool) string {
	if dark {
		return "dark"
	}
	return "light"
}

type menuRec struct {
	mu    sync.Mutex
	items []MenuItem
}

func (m *menuRec) act() Actions {
	return Actions{Menu: func(items []MenuItem) { m.mu.Lock(); m.items = items; m.mu.Unlock() }}
}

func (m *menuRec) find(label string) *MenuItem {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.items {
		if strings.HasPrefix(m.items[i].Label, label) {
			return &m.items[i]
		}
	}
	return nil
}

// Every view of the kitchen-sink list draws in every mode, light and dark;
// every column type renders a value.
func TestRenderList(t *testing.T) {
	for _, dark := range []bool{false, true} {
		h, _ := newEnv(t, dark)
		s := newShooter(t, image.Pt(1440, 820))
		v := New(h, fakeslack.ListKitchen, Full, Actions{})
		draw := func(gtx layout.Context) { v.Layout(gtx) }
		settle(t, s, draw, "the list", func() bool { return v.Doc() != nil })
		d := v.Doc()
		if got := len(v.rowsFor(d, v.current(d))); got != 6 {
			t.Errorf("All items rows = %d", got)
		}
		// each column type renders text for its fixture value
		a := h.a
		r := d.Record("Rec0ITEM001")
		for i := range d.Meta.Schema {
			c := &d.Meta.Schema[i]
			switch c.Type {
			case lists.Attachment, lists.Canvas:
				if f := r.Field(c.Key); f == nil || len(f.Attachment) == 0 {
					t.Errorf("%s: fixture has no files", c.Name)
				}
				continue
			case lists.Checkbox:
				continue // unchecked: no text
			}
			if CellText(a, c, r.Field(c.Key)) == "" {
				t.Errorf("column %s (%s): no text", c.Name, c.Type)
			}
		}
		s.shot("lists-table-full-"+mode(dark), draw)

		v.SelectView("View0BRD001")
		s.shot("lists-board-full-"+mode(dark), draw)
		v.SelectView("View0MINE01")
		s.frame(draw)
		if got := v.rowsFor(v.Doc(), v.current(v.Doc())); len(got) != 2 {
			t.Errorf("My open items rows = %d", len(got))
		}
		s.shot("lists-filtered-full-"+mode(dark), draw)

		// a channel's list tab: the to-do list with its title header
		tab := New(h, fakeslack.ListTodos, Tab, Actions{})
		ts := newShooter(t, image.Pt(1100, 600))
		tdraw := func(gtx layout.Context) { tab.Layout(gtx) }
		settle(t, ts, tdraw, "the to-do list", func() bool { return tab.Doc() != nil })
		ts.shot("lists-todo-tab-"+mode(dark), tdraw)

		// the side panel
		side := New(h, fakeslack.ListKitchen, Side, Actions{Close: func() {}, FullView: func() {}})
		ss := newShooter(t, image.Pt(560, 760))
		sdraw := func(gtx layout.Context) { side.Layout(gtx) }
		settle(t, ss, sdraw, "the side panel", func() bool { return side.Doc() != nil })
		ss.shot("lists-side-"+mode(dark), sdraw)

		// an item's pane with its comments
		it := NewItem(h, fakeslack.ListKitchen, "Rec0ITEM001", Actions{Close: func() {}})
		is := newShooter(t, image.Pt(480, 900))
		idraw := func(gtx layout.Context) { it.Layout(gtx) }
		settle(t, is, idraw, "the item's comments", func() bool {
			it.mu.Lock()
			defer it.mu.Unlock()
			return it.doc != nil && it.cload && len(it.comments) == 3
		})
		is.shot("lists-item-"+mode(dark), idraw)
	}
}

// Someone else's edit and new item show up while the list is open.
func TestLiveUpdate(t *testing.T) {
	h, fs := newEnv(t, false)
	evs, stop := h.a.Subscribe()
	defer stop()
	h.a.SetRealtime(true)
	deadline := time.After(10 * time.Second)
	for connected := false; !connected; {
		select {
		case ev := <-evs:
			connected = ev.Type == "connection" && ev.Connected
		case <-deadline:
			t.Fatal("no realtime connection")
		}
	}
	s := newShooter(t, image.Pt(1440, 820))
	v := New(h, fakeslack.ListKitchen, Full, Actions{})
	draw := func(gtx layout.Context) { v.Layout(gtx) }
	settle(t, s, draw, "the list", func() bool { return v.Doc() != nil && h.a.ListOpen(fakeslack.ListKitchen) })
	fs.EditListAsOther(fakeslack.ListKitchen, "Rec0ITEM003", lists.Field{Key: "Col0STATUS1", ColumnID: "Col0STATUS1", Select: []string{"OptDONE0001"}})
	id := fs.AddListRecordAsOther(fakeslack.ListKitchen, "Added by someone else")
	settle(t, s, draw, "the live edit", func() bool {
		d := v.Doc()
		if d == nil || d.Record(id) == nil {
			return false
		}
		f := d.Record("Rec0ITEM003").Field("Col0STATUS1")
		return f != nil && len(f.Select) == 1 && f.Select[0] == "OptDONE0001"
	})
	s.shot("lists-live-light", draw)
}

// Editing from the grid and the item pane reaches the fake as Slack's
// lists.* calls; lossy edits are refused before anything is sent.
func TestEditRoundTrip(t *testing.T) {
	h, fs := newEnv(t, false)
	var mr menuRec
	s := newShooter(t, image.Pt(1440, 820))
	v := New(h, fakeslack.ListKitchen, Full, mr.act())
	draw := func(gtx layout.Context) { v.Layout(gtx) }
	settle(t, s, draw, "the list", func() bool { return v.Doc() != nil })
	d := v.Doc()
	gtx := gtxFor(new(op.Ops), image.Pt(100, 100))
	rec := d.Record("Rec0ITEM003")
	stored := func(id, key string) *lists.Field {
		l, _ := fs.List(fakeslack.ListKitchen)
		for i := range l.Records {
			if l.Records[i].ID == id {
				return l.Records[i].Field(key)
			}
		}
		return nil
	}
	wait := func(what string, cond func() bool) {
		t.Helper()
		settle(t, s, draw, what, cond)
	}
	// a checkbox toggles
	v.cellClick(d, rec, d.Meta.Column("Col0DONE001"), 0, gtx)
	wait("the checkbox", func() bool {
		f := stored("Rec0ITEM003", "Col0DONE001")
		return f != nil && f.Checkbox != nil && *f.Checkbox
	})
	// a select picks from its options
	v.cellClick(d, rec, d.Meta.Column("Col0STATUS1"), 0, gtx)
	if it := mr.find("Blocked"); it == nil {
		t.Fatal("no Blocked option")
	} else {
		it.Do()
	}
	wait("the status", func() bool {
		f := stored("Rec0ITEM003", "Col0STATUS1")
		return f != nil && len(f.Select) == 1 && f.Select[0] == "OptBLOK0001"
	})
	// a person: assign someone
	v.cellClick(d, rec, d.Meta.Column("Col0OWNER01"), 0, gtx)
	var pick *MenuItem
	mr.mu.Lock()
	for i := range mr.items {
		if !strings.Contains(mr.items[i].Label, "(you)") && mr.items[i].Icon == "user" {
			pick = &mr.items[i]
			break
		}
	}
	mr.mu.Unlock()
	if pick == nil {
		t.Fatal("no one else to assign")
	}
	pick.Do()
	wait("the owner", func() bool {
		f := stored("Rec0ITEM003", "Col0OWNER01")
		return f != nil && len(f.User) == 1 && f.User[0] != fakeslack.SelfID
	})
	// the inline editor: name and number
	v.cellClick(d, rec, d.Meta.Column("name"), 0, gtx)
	if !v.edit.active {
		t.Fatal("the name didn't open the editor")
	}
	v.edit.ed.SetText("Record the demo video, take two")
	v.commit(d)
	wait("the name", func() bool { return stored("Rec0ITEM003", "name").PlainText() == "Record the demo video, take two" })
	v.cellClick(d, rec, d.Meta.Column("Col0EST0001"), 0, gtx)
	v.edit.ed.SetText("4.5")
	v.commit(d)
	wait("the estimate", func() bool {
		f := stored("Rec0ITEM003", "Col0EST0001")
		return f != nil && len(f.Number) == 1 && f.Number[0] == 4.5
	})
	// a rating: the third star
	v.cellClick(d, rec, d.Meta.Column("Col0PRIO001"), gtx.Dp(26)*2+2, gtx)
	wait("the rating", func() bool {
		f := stored("Rec0ITEM003", "Col0PRIO001")
		return f != nil && len(f.Rating) == 1 && f.Rating[0] == 3
	})
	// a vote: mine added
	v.cellClick(d, rec, d.Meta.Column("Col0VOTE001"), 0, gtx)
	wait("the vote", func() bool {
		f := stored("Rec0ITEM003", "Col0VOTE001")
		return f != nil && len(f.User) == 1 && f.User[0] == fakeslack.SelfID
	})
	// the guard: formatted text, a computed field, files
	n := len(fs.ListCellUpdates())
	v.cellClick(d, d.Record("Rec0ITEM002"), d.Meta.Column("name"), 0, gtx)
	if v.edit.active || !strings.Contains(h.lastToast(), "can't be edited in Clack yet (it has formatting)") {
		t.Errorf("formatted text: editor %v, toast %q", v.edit.active, h.lastToast())
	}
	v.cellClick(d, rec, d.Meta.Column("Col0FILE001"), 0, gtx)
	if !strings.Contains(h.lastToast(), "it has files") {
		t.Errorf("files toast %q", h.lastToast())
	}
	v.cellClick(d, rec, d.Meta.Column("Col0CRBY001"), 0, gtx)
	if len(fs.ListCellUpdates()) != n {
		t.Error("a refused edit was sent")
	}
	// a new item from "Add item"
	v.startNew(d, nil, "")
	v.edit.ed.SetText("Plan the launch party")
	v.commit(d)
	wait("the new item", func() bool {
		cr := fs.ListRecordCreates()
		return len(cr) == 1 && strings.Contains(cr[0].Get("initial_fields"), "Plan the launch party")
	})
	var init []map[string]json.RawMessage
	_ = json.Unmarshal([]byte(fs.ListRecordCreates()[0].Get("initial_fields")), &init)
	if len(init) != 1 || string(init[0]["column_id"]) != `"Col00000001"` {
		t.Errorf("initial_fields %s", fs.ListRecordCreates()[0].Get("initial_fields"))
	}
	wait("the new row", func() bool {
		vd := v.Doc()
		return vd != nil && len(v.rowsFor(vd, v.current(vd))) == 7
	})
	s.shot("lists-edited-light", draw)

	// a comment from the item pane (an item without a thread yet)
	it := NewItem(h, fakeslack.ListKitchen, "Rec0ITEM004", Actions{})
	is := newShooter(t, image.Pt(480, 900))
	idraw := func(gtx layout.Context) { it.Layout(gtx) }
	settle(t, is, idraw, "the item", func() bool { it.mu.Lock(); defer it.mu.Unlock(); return it.cload })
	it.composer.SetText("Shipped it")
	it.send()
	settle(t, is, idraw, "the comment", func() bool {
		it.mu.Lock()
		ts := it.thread
		it.mu.Unlock()
		cm := fs.ListComments(fakeslack.ListKitchen, ts)
		return len(cm) == 2 && cm[1].Text == "Shipped it"
	})
	settle(t, is, idraw, "the comment shown", func() bool {
		it.mu.Lock()
		defer it.mu.Unlock()
		for _, m := range it.comments {
			if m.Text == "Shipped it" {
				return true
			}
		}
		return false
	})
	is.shot("lists-item-commented-light", idraw)
}
