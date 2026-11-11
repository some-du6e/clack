package listview

// The list dialogs (picker.go) against internal/fakeslack: "Add to list"
// and the channel tab bar's "Add a list". Synthetic fixtures only.

import (
	"context"
	"encoding/json"
	"image"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// pickHost is testHost with the rest of popups.Host.
type pickHost struct {
	*testHost
	omu      sync.Mutex
	overlays []popups.Overlay
}

func (h *pickHost) ShowOverlay(o popups.Overlay) {
	h.omu.Lock()
	h.overlays = append(h.overlays, o)
	h.omu.Unlock()
}
func (h *pickHost) OpenThread(string, string)                 {}
func (h *pickHost) CloseSecondary()                           {}
func (h *pickHost) OpenEmojiPicker(image.Point, func(string)) {}
func (h *pickHost) OpenMessageMenu(image.Point, core.Message) {}
func (h *pickHost) OpenLightbox(richtext.Image, core.Message) {}

type pickEnv struct {
	t  *testing.T
	h  *pickHost
	fs *fakeslack.Server
	r  *input.Router
	sz image.Point

	mu    sync.Mutex
	queue []func()
	// UI goroutine (the test's) only
	opened []string // OpenItem: "<list>/<record>"
	made   []string // OpenList
	shown  []string // ShowTab
	menus  [][]MenuItem
	tabs   map[string]bool // HasTab's answers
}

func newPickEnv(t *testing.T, dark bool) *pickEnv {
	h, fs := newEnv(t, dark)
	return &pickEnv{t: t, h: &pickHost{testHost: h}, fs: fs, r: new(input.Router), sz: image.Pt(1440, 900), tabs: map[string]bool{}}
}

func (e *pickEnv) opts(tab bool) AddOpts {
	o := AddOpts{
		Post: func(f func()) {
			e.mu.Lock()
			e.queue = append(e.queue, f)
			e.mu.Unlock()
		},
		OpenItem: func(l, r string) { e.opened = append(e.opened, l+"/"+r) },
		OpenList: func(id string) { e.made = append(e.made, id) },
		Menu:     func(items []MenuItem) { e.menus = append(e.menus, items) },
	}
	if tab {
		o.HasTab = func(id string) bool { return e.tabs[id] }
		o.ShowTab = func(id string) { e.shown = append(e.shown, id) }
	}
	return o
}

// frame runs the posted callbacks, then lays m out over the window.
func (e *pickEnv) frame(m *popups.Modal) {
	e.mu.Lock()
	q := e.queue
	e.queue = nil
	e.mu.Unlock()
	for _, f := range q {
		f()
	}
	ops := new(op.Ops)
	gtx := layout.Context{Ops: ops, Now: time.Now(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(e.sz), Source: e.r.Source()}
	theme.Fill(gtx, image.Rectangle{Max: e.sz}, e.h.th.C.Bg)
	m.Layout(gtx)
	e.r.Frame(ops)
}

func (e *pickEnv) until(m *popups.Modal, what string, cond func() bool) {
	e.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			e.t.Fatalf("timed out waiting for %s", what)
		}
		e.frame(m)
		time.Sleep(10 * time.Millisecond)
	}
}

func (e *pickEnv) click(m *popups.Modal, p image.Point) {
	pt := f32.Pt(float32(p.X), float32(p.Y))
	e.r.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pt},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pt})
	for i := 0; i < 3; i++ {
		e.frame(m)
	}
}

// histTS is the newest message of ch's fake history.
func histTS(t *testing.T, a *core.App, ch string) string {
	t.Helper()
	var r struct {
		Messages []struct {
			TS string `json:"ts"`
		} `json:"messages"`
	}
	if err := a.C.Call(context.Background(), "conversations.history", url.Values{"channel": {ch}, "limit": {"1"}}, &r); err != nil || len(r.Messages) == 0 {
		t.Fatalf("history of %s: %v", ch, err)
	}
	return r.Messages[0].TS
}

func rowIndex(rows []core.ListResult, id string) int {
	for i, r := range rows {
		if r.ID == id {
			return i
		}
	}
	return -1
}

// The dialog's geometry at 1x: 520 wide (650 for the picker), a 70px
// header, the body (1 + 10 + 36 + 16 + 400 of results) and the 85px
// footer, centred in the 1440×900 window.
func dialogOrigin(w, bodyH int) image.Point {
	return image.Pt((1440-w)/2, (900-(70+bodyH+85))/2)
}

// "Add to list": a row click picks the list, Add closes the dialog and
// sends lists.addToList, the pref keeps the list and the new item opens.
func TestAddToListDialog(t *testing.T) {
	e := newPickEnv(t, false)
	ch := fakeslack.ListTabChan
	ts := histTS(t, e.h.a, ch)
	p := newAddToList(e.h, ch, ts, e.opts(false))
	e.until(p.modal, "the lists", func() bool { return !p.loading && rowIndex(p.rowsShown(), fakeslack.ListKitchen) >= 0 })
	if p.sel != "" {
		t.Fatalf("preselected %q without a default list", p.sel)
	}
	if p.banner() != "" {
		t.Errorf("banner %q with nothing picked", p.banner())
	}
	o := dialogOrigin(520, 463)
	i := rowIndex(p.rowsShown(), fakeslack.ListKitchen)
	e.click(p.modal, image.Pt(o.X+120, o.Y+70+63+i*65+32))
	if p.sel != fakeslack.ListKitchen {
		t.Fatalf("the click picked %q", p.sel)
	}
	e.until(p.modal, "the list's header", func() bool { return p.hdrs[fakeslack.ListKitchen] != nil })
	if p.banner() != "" {
		t.Errorf("banner %q for a list with a message column", p.banner())
	}
	// Add: the footer's right button (80px)
	e.click(p.modal, image.Pt(o.X+520-28-40, o.Y+70+463+1+24+18))
	if !p.modal.Dismissed() {
		t.Fatal("Add didn't close the dialog")
	}
	e.until(p.modal, "the new item", func() bool { return len(e.opened) == 1 && len(e.fs.OpenRecordCalls()) == 1 })
	calls := e.fs.AddToListCalls()
	if len(calls) != 1 || calls[0].Get("list_id") != fakeslack.ListKitchen || calls[0].Get("message_ts") != ts ||
		calls[0].Get("channel_id") != ch || calls[0].Get("reason") != "add-to-list" {
		t.Fatalf("lists.addToList %v", calls)
	}
	if !strings.HasPrefix(e.opened[0], fakeslack.ListKitchen+"/Rec") {
		t.Errorf("opened %v", e.opened)
	}
	e.until(p.modal, "the default list pref", func() bool {
		return e.h.a.PrefString(core.PrefAddToListDefault) == fakeslack.ListKitchen
	})
}

// The pref's list is picked and shown first; one without a message
// column gets Slack's note; a deleted one is dropped and the pref cleared.
func TestAddToListDefault(t *testing.T) {
	e := newPickEnv(t, false)
	ctx := context.Background()
	ch := fakeslack.ListTabChan
	ts := histTS(t, e.h.a, ch)
	if err := e.h.a.SetPref(ctx, core.PrefAddToListDefault, fakeslack.ListTodos); err != nil {
		t.Fatal(err)
	}
	p := newAddToList(e.h, ch, ts, e.opts(false))
	if p.sel != fakeslack.ListTodos {
		t.Fatalf("preselected %q", p.sel)
	}
	e.until(p.modal, "the lists and the default's header", func() bool { return !p.loading && p.hdrs[fakeslack.ListTodos] != nil })
	if rows := p.rowsShown(); len(rows) == 0 || rows[0].ID != fakeslack.ListTodos {
		t.Errorf("first row %+v", rows)
	}
	if !strings.Contains(p.banner(), "doesn’t have a message field. Continuing will automatically create one.") {
		t.Errorf("banner %q", p.banner())
	}

	// "Create New List": the add_to_new_list list, the message in it, the list opens
	p.createAndAdd()
	e.until(p.modal, "the new list", func() bool { return len(e.made) == 1 })
	if !p.modal.Dismissed() {
		t.Error("the dialog stayed open")
	}
	if l, ok := e.fs.List(e.made[0]); !ok || len(l.Records) != 1 {
		t.Errorf("new list %+v %v", l, ok)
	}

	gone := "F0LISTGONE1"
	if err := e.h.a.SetPref(ctx, core.PrefAddToListDefault, gone); err != nil {
		t.Fatal(err)
	}
	p2 := newAddToList(e.h, ch, ts, e.opts(false))
	e.until(p2.modal, "the deleted list's header", func() bool { return p2.hdrs[gone] != nil })
	if p2.sel != "" || p2.def != "" {
		t.Errorf("a deleted default stayed: sel %q def %q", p2.sel, p2.def)
	}
	e.until(p2.modal, "the pref cleared", func() bool { return e.h.a.PrefString(core.PrefAddToListDefault) == "" })
}

// The tab bar's "Add a list": a list with several views asks which one
// the tab opens on; Insert asks to share it, and Share adds the tab.
func TestListPickerTab(t *testing.T) {
	e := newPickEnv(t, false)
	ch := fakeslack.ListTabChan
	p := newListPicker(e.h, ch, e.opts(true))
	e.until(p.modal, "the lists", func() bool { return !p.loading && rowIndex(p.rowsShown(), fakeslack.ListKitchen) >= 0 })
	o := dialogOrigin(650, 463)
	i := rowIndex(p.rowsShown(), fakeslack.ListKitchen)
	e.click(p.modal, image.Pt(o.X+120, o.Y+70+63+i*65+32))
	e.until(p.modal, "the speed bump", func() bool { return p.bump })
	if p.bumpList != fakeslack.ListKitchen || p.bumpView != "View0ALL001" {
		t.Fatalf("speed bump for %q on %q", p.bumpList, p.bumpView)
	}
	// the View select offers the tab views, all items first
	e.click(p.modal, image.Pt(o.X+325, o.Y+618-20-36-24-18)) // the select, above the buttons
	if len(e.menus) != 1 || len(e.menus[0]) != 4 || e.menus[0][0].Label != "All items" {
		t.Fatalf("view menu %+v", e.menus)
	}
	e.menus[0][1].Do() // the board
	if p.bumpView != "View0BRD001" {
		t.Errorf("view %q after picking the board", p.bumpView)
	}
	// Insert (the sheet's right button)
	e.click(p.modal, image.Pt(o.X+650-24-40, o.Y+618-20-18))
	if !p.modal.Dismissed() || p.shared == nil {
		t.Fatalf("Insert: dismissed %v, share dialog %v", p.modal.Dismissed(), p.shared != nil)
	}
	if p.shared.access != "write" {
		t.Errorf("share access %q", p.shared.access)
	}
	sm := p.shared.modal
	for i := 0; i < 3; i++ {
		e.frame(sm)
	}
	// Share has the focus (autoFocus): Enter presses it
	e.r.Queue(key.Event{Name: key.NameReturn, State: key.Press}, key.Event{Name: key.NameReturn, State: key.Release})
	e.until(sm, "the tab", func() bool { return len(e.shown) == 1 })
	if e.shown[0] != fakeslack.ListKitchen || !sm.Dismissed() {
		t.Errorf("shown %v, share dialog dismissed %v", e.shown, sm.Dismissed())
	}
	calls := e.fs.AddTabCalls()
	if len(calls) != 1 {
		t.Fatalf("conversations.addTab %v", calls)
	}
	var data map[string]string
	_ = json.Unmarshal([]byte(calls[0].Get("data")), &data)
	if data["file_id"] != fakeslack.ListKitchen || data["access_level"] != "write" || data["lists_channel_tab_default_refine_view_id"] != "View0BRD001" {
		t.Errorf("tab data %v", data)
	}

	// a list already in the tab bar just opens its tab
	e.tabs[fakeslack.ListTodos] = true
	p2 := newListPicker(e.h, ch, e.opts(true))
	e.until(p2.modal, "the lists", func() bool { return !p2.loading })
	p2.insertTab(fakeslack.ListTodos, "")
	if len(e.shown) != 2 || e.shown[1] != fakeslack.ListTodos || len(e.fs.AddTabCalls()) != 1 {
		t.Errorf("an existing tab: shown %v, addTab calls %d", e.shown, len(e.fs.AddTabCalls()))
	}

	// Create New List: a blank list for the tab
	p2.newTabList()
	e.until(p2.modal, "the new list's tab", func() bool { return len(e.shown) == 3 })
	nc := e.fs.AddTabCalls()
	if len(nc) != 2 {
		t.Fatalf("conversations.addTab %v", nc)
	}
	var nd map[string]string
	_ = json.Unmarshal([]byte(nc[1].Get("data")), &nd)
	if nd["file_id"] != e.shown[2] || nd["access_level"] != "" || len(nd) != 1 {
		t.Errorf("new list tab data %v (shown %v)", nd, e.shown)
	}
	if _, ok := e.fs.List(e.shown[2]); !ok {
		t.Errorf("no list %s", e.shown[2])
	}
}

// Screenshots (lists-shots): the dialogs in light and dark.
func TestRenderListDialogs(t *testing.T) {
	for _, dark := range []bool{false, true} {
		e := newPickEnv(t, dark)
		s := newShooter(t, e.sz)
		ch := fakeslack.ListTabChan
		ts := histTS(t, e.h.a, ch)
		draw := func(m *popups.Modal) func(gtx layout.Context) {
			return func(gtx layout.Context) {
				e.mu.Lock()
				q := e.queue
				e.queue = nil
				e.mu.Unlock()
				for _, f := range q {
					f()
				}
				theme.Fill(gtx, image.Rectangle{Max: e.sz}, e.h.th.C.Bg)
				m.Layout(gtx)
			}
		}
		p := newAddToList(e.h, ch, ts, e.opts(false))
		settle(t, s, draw(p.modal), "the lists", func() bool { return !p.loading && len(p.results) > 0 })
		p.pick(fakeslack.ListTodos)
		settle(t, s, draw(p.modal), "the header", func() bool { return p.hdrs[fakeslack.ListTodos] != nil })
		s.shot("lists-add-to-list-"+mode(dark), draw(p.modal))

		lp := newListPicker(e.h, ch, e.opts(true))
		settle(t, s, draw(lp.modal), "the lists", func() bool { return !lp.loading && len(lp.results) > 0 })
		s.shot("lists-add-tab-"+mode(dark), draw(lp.modal))
		lp.pick(fakeslack.ListKitchen)
		settle(t, s, draw(lp.modal), "the speed bump", func() bool { return lp.bump })
		s.shot("lists-add-tab-view-"+mode(dark), draw(lp.modal))
		lp.bump = false
		lp.insertTab(fakeslack.ListKitchen, "View0ALL001")
		if lp.shared == nil {
			t.Fatal("no share dialog")
		}
		s.shot("lists-share-tab-"+mode(dark), draw(lp.shared.modal))
	}
}
