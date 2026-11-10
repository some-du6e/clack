package views

// The rail list views' keyboard (vlist_keys.go, Slack's virtual list and
// the To-dos InteractiveList) and live updates, on internal/fakeslack's
// views fixtures (synthetic data only). Realtime events go over the fake's
// socket to a booted core, as Slack's RTM sends them.

import (
	"context"
	"fmt"
	"image"
	"net/url"
	"sync"
	"testing"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// kbHost records the views' navigation and Undo toasts.
type kbHost struct {
	*vaHost
	mu      sync.Mutex
	navs    []string
	threads []string
	undos   []string
	undo    func()
}

func (h *kbHost) Navigate(r string) {
	h.mu.Lock()
	h.navs = append(h.navs, r)
	h.mu.Unlock()
}

func (h *kbHost) OpenThread(ch, ts string) {
	h.mu.Lock()
	h.threads = append(h.threads, ch+"/"+ts)
	h.mu.Unlock()
}

func (h *kbHost) ToastUndo(text string, undo func()) {
	h.mu.Lock()
	h.undos = append(h.undos, text)
	h.undo = undo
	h.mu.Unlock()
}

func (h *kbHost) last() (nav, thread string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if n := len(h.navs); n > 0 {
		nav = h.navs[n-1]
	}
	if n := len(h.threads); n > 0 {
		thread = h.threads[n-1]
	}
	return
}

func newKBHost(app *core.App, dark bool) *kbHost {
	return &kbHost{vaHost: &vaHost{slickHost: &slickHost{app: app, th: theme.New(dark)}}}
}

// kbPress presses and releases a key, then runs two frames.
func kbPress(e *vaEnv, n key.Name, m key.Modifiers) {
	e.r.Queue(key.Event{Name: n, Modifiers: m, State: key.Press}, key.Event{Name: n, Modifiers: m, State: key.Release})
	e.frame()
	e.frame()
}

// kbClickRow clicks the middle of a list row (top from the list's last
// layout, listY the list's top in the view).
func kbClickRow(t *testing.T, e *vaEnv, kb *vkList, k string, listY int) {
	t.Helper()
	y, ok, _ := kb.top(k)
	if !ok {
		t.Fatalf("row %s isn't laid out", k)
	}
	e.click(image.Pt(e.size.X/2, listY+y+kb.height(k)/2))
}

// vkFake boots a core on the views fixtures with realtime on.
func vkFake(t *testing.T, hook func(method string, f url.Values) (any, bool)) (*core.App, *fakeslack.Server) {
	t.Helper()
	sz := fakeslack.Small
	sz.Views = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	if hook != nil {
		fs.SetHook(hook)
	}
	app := core.New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(app.Shutdown)
	evs, stop := app.Subscribe()
	defer stop()
	if err := app.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	app.S.PutUsers([]slack.User{
		{ID: "U0FAKE00001", Name: "ada", RealName: "Ada Example"},
		{ID: "U0FAKE00002", Name: "grace", RealName: "Grace Example"},
	})
	app.S.PutConv(slack.Conversation{ID: fakeslack.ViewsChannel, Name: "views-fixtures", IsChannel: true, IsMember: true, NumMembers: 3})
	app.SetRealtime(true)
	deadline := time.After(10 * time.Second)
	for {
		select {
		case ev := <-evs:
			if ev.Type == "connection" && ev.Connected {
				return app, fs
			}
		case <-deadline:
			t.Fatal("realtime didn't connect")
		}
	}
}

// Threads: Slack's rows (heading, root, replies, footer) under the
// virtual list's keys. A press makes a row active without the ring; ↓/↑
// move through every row with the ring; → opens the active message's
// thread; Enter on a root or reply jumps to the root in the channel;
// Home goes back to the first row.
func TestThreadsKeysLikeSlack(t *testing.T) {
	for _, dark := range []bool{false, true} {
		app, _ := vaFake(t)
		h := newKBHost(app, dark)
		v := NewThreads(h)
		e := &vaEnv{size: image.Pt(1000, 1100)}
		e.draw = func(gtx layout.Context) { v.Layout(gtx) }
		e.until(t, "the followed threads", func() bool { p, ok, _ := v.data.get(); return ok && len(p.items) == 2 })
		e.frame()
		e.frame()
		p, _, _ := v.data.get()
		k0 := p.items[0].key
		root := "root-" + k0
		kbClickRow(t, e, v.kb, root, 49)
		if v.kb.active != root || v.kb.ring || !v.kb.focused {
			t.Fatalf("after a press: active %q ring %v focused %v", v.kb.active, v.kb.ring, v.kb.focused)
		}
		kbPress(e, key.NameDownArrow, 0)
		if want := k0 + "-" + p.items[0].Replies[0].TS; v.kb.active != want || !v.kb.ring {
			t.Fatalf("↓: active %q (ring %v), want %q", v.kb.active, v.kb.ring, want)
		}
		vaShot(t, e, "threads-keyboard-focus", dark)
		kbPress(e, key.NameUpArrow, 0)
		kbPress(e, key.NameUpArrow, 0)
		if v.kb.active != "heading-"+k0 {
			t.Fatalf("↑↑: active %q, want the heading", v.kb.active)
		}
		kbPress(e, key.NameUpArrow, 0)
		if v.kb.active != "heading-"+k0 {
			t.Fatalf("↑ at the top moved to %q", v.kb.active)
		}
		kbPress(e, key.NameDownArrow, 0)
		kbPress(e, key.NameRightArrow, 0)
		if _, th := h.last(); th != fakeslack.ViewsChannel+"/"+fakeslack.ViewsThreadUnread {
			t.Errorf("→ on the root opened %q", th)
		}
		kbPress(e, key.NameReturn, 0)
		if nav, _ := h.last(); nav != "c/"+fakeslack.ViewsChannel+"/m/"+fakeslack.ViewsThreadUnread {
			t.Errorf("Enter on the root went to %q", nav)
		}
		// ↓ onto the footer: its reply box takes the focus (onItemFocusEnter)
		for i := 0; i < 4 && (v.cards[k0] == nil || v.cards[k0].comp == nil); i++ {
			kbPress(e, key.NameDownArrow, 0)
		}
		c := v.cards[k0]
		if c == nil || c.comp == nil {
			t.Fatalf("↓ to the footer didn't open the reply box (active %q)", v.kb.active)
		}
		e.frame()
		if v.kb.focused {
			t.Error("the list kept the focus after the footer opened its reply box")
		}
		// ↑ in the empty reply box: focusItemAboveInput
		c.comp.OnFocusList()
		e.frame()
		e.frame()
		last := p.items[0].Replies[len(p.items[0].Replies)-1].TS
		if v.kb.active != k0+"-"+last || !v.kb.focused || !v.kb.ring {
			t.Fatalf("↑ in the reply box: active %q focused %v", v.kb.active, v.kb.focused)
		}
		kbPress(e, key.NameHome, 0)
		if v.kb.active != "heading-"+k0 {
			t.Errorf("Home: active %q", v.kb.active)
		}
		// a printable key on a row: the reply box takes it (after any
		// draft the composer restored for this thread)
		before := c.comp.DocText()
		kbPress(e, "A", 0)
		if got := c.comp.DocText(); got != before+"a" {
			t.Errorf("typing on a row: reply box %q, was %q", got, before)
		}
		c.comp.Close()
	}
}

// Threads live: thread_marked patches the thread's last read in place
// (no getView), thread_subscribed brings the newly followed thread in
// (a refresh), as Slack's RTM-THREADS handlers do.
func TestThreadsRealtimeLikeSlack(t *testing.T) {
	app, fs := vkFake(t, nil)
	v := NewThreads(newKBHost(app, false))
	e := &vaEnv{size: image.Pt(1000, 1100)}
	e.draw = func(gtx layout.Context) { v.Layout(gtx) }
	e.until(t, "the followed threads", func() bool { p, ok, _ := v.data.get(); return ok && len(p.items) == 2 })
	p, _, _ := v.data.get()
	if v.unread(&p.items[0]) != 1 {
		t.Fatalf("unread %d, want 1", v.unread(&p.items[0]))
	}
	views := fs.CallCount("subscriptions.thread.getView")
	fs.Push(map[string]any{"type": "thread_marked", "subscription": map[string]any{
		"type": "thread", "channel": fakeslack.ViewsChannel, "thread_ts": fakeslack.ViewsThreadUnread,
		"last_read": "1762600200.000100", "active": true}, "event_ts": "1762600300.000100"})
	e.until(t, "the thread marked read in place", func() bool {
		p, _, _ := v.data.get()
		return v.unread(&p.items[0]) == 0
	})
	for i := 0; i < 20; i++ {
		e.frame()
		time.Sleep(10 * time.Millisecond)
	}
	if n := fs.CallCount("subscriptions.thread.getView"); n != views {
		t.Errorf("thread_marked reloaded the view (%d getView calls, was %d)", n, views)
	}
	fs.Push(map[string]any{"type": "thread_subscribed", "subscription": map[string]any{
		"type": "thread", "channel": fakeslack.ViewsChannel, "thread_ts": fakeslack.ViewsThreadRead,
		"last_read": "1762500100.000100", "active": true}, "event_ts": "1762600400.000100"})
	e.until(t, "the refresh after thread_subscribed", func() bool {
		return fs.CallCount("subscriptions.thread.getView") > views
	})
}

// Drafts & sent: the DraftList's keys (every row focusable, Enter is the
// row's click: a draft opens in its conversation) and the list taking a
// draft written on another device (draft_create).
func TestDraftsKeysAndRealtimeLikeSlack(t *testing.T) {
	app, fs := vkFake(t, nil)
	h := newKBHost(app, false)
	d := NewDrafts(h)
	e := &vaEnv{size: image.Pt(1000, 900)}
	e.draw = func(gtx layout.Context) {
		d.Layout(gtx)
		for _, o := range h.open() {
			o.Layout(gtx)
		}
	}
	e.until(t, "drafts", func() bool { return len(d.drafts) == 1 && len(d.scheduled) == 2 })
	e.frame()
	e.frame()
	kb := d.kb[draftsTabDrafts]
	k := "d:" + d.drafts[0].ID
	listY := e.size.Y - kb.viewH
	kbClickRow(t, e, kb, k, listY)
	if kb.active != k || kb.ring {
		t.Fatalf("after a press: active %q ring %v", kb.active, kb.ring)
	}
	h.mu.Lock()
	h.navs = nil
	h.mu.Unlock()
	kbPress(e, key.NameUpArrow, 0)
	if kb.active == k && d.view[0].kind == draftsEntryCard {
		t.Errorf("↑ didn't reach the education card")
	}
	kbPress(e, key.NameEnd, 0)
	if kb.active != k || !kb.ring {
		t.Fatalf("End: active %q ring %v, want the draft with the ring", kb.active, kb.ring)
	}
	vaShot(t, e, "drafts-keyboard-focus", false)
	kbPress(e, key.NameReturn, 0)
	if nav, _ := h.last(); nav != "c/"+fakeslack.ViewsChannel {
		t.Errorf("Enter on the draft went to %q", nav)
	}

	id := fs.DraftElsewhere("a synthetic draft from another device")
	e.until(t, "the other device's draft", func() bool {
		for _, x := range d.drafts {
			if x.ID == id {
				return true
			}
		}
		return false
	})
}

// To-dos: InteractiveList (arrows step, PageDown jumps a page of 10,
// Home/End), E completes the active to-do, Delete clears it behind an
// undoable "To-do cleared" toast, and a saved_added event reloads.
func TestTodosKeysAndRealtimeLikeSlack(t *testing.T) {
	var mu sync.Mutex
	items := []map[string]any{}
	for i := 0; i < 12; i++ {
		items = append(items, map[string]any{"item_id": fakeslack.ViewsChannel, "item_type": "message",
			"ts": fmt.Sprintf("%d.000100", 1762601000+i), "todo_state": "to_do", "date_created": 1762600000 + i})
	}
	log := &callLog{}
	app, fs := vkFake(t, func(method string, f url.Values) (any, bool) {
		log.rec(method, f)
		if method == "saved.list" {
			mu.Lock()
			defer mu.Unlock()
			return map[string]any{"ok": true, "saved_items": append([]map[string]any(nil), items...)}, true
		}
		return nil, false
	})
	h := newKBHost(app, false)
	l := NewLater(h)
	e := &vaEnv{size: image.Pt(1000, 900)}
	e.draw = func(gtx layout.Context) { l.Layout(gtx) }
	e.until(t, "the to-dos", func() bool { return len(l.view) == 12 })
	e.frame()
	e.frame()
	first := laterKey(&l.view[0])
	kbClickRow(t, e, l.kb, first, e.size.Y-l.kb.viewH)
	if l.kb.active != first || l.kb.ring {
		t.Fatalf("after a press: active %q ring %v", l.kb.active, l.kb.ring)
	}
	kbPress(e, key.NameDownArrow, 0)
	if want := laterKey(&l.view[1]); l.kb.active != want || !l.kb.ring {
		t.Fatalf("↓: %q ring %v, want %q", l.kb.active, l.kb.ring, want)
	}
	kbPress(e, key.NameRightArrow, 0)
	if want := laterKey(&l.view[2]); l.kb.active != want {
		t.Fatalf("→: %q, want %q", l.kb.active, want)
	}
	vaShot(t, e, "todos-keyboard-focus", false)
	kbPress(e, key.NamePageDown, 0)
	if want := laterKey(&l.view[10]); l.kb.active != want {
		t.Fatalf("PageDown from row 2: %q, want row 10", l.kb.active)
	}
	kbPress(e, key.NameHome, 0)
	if l.kb.active != first {
		t.Fatalf("Home: %q", l.kb.active)
	}
	kbPress(e, "E", 0)
	e.until(t, "saved.update mark=completed", func() bool {
		return log.count("saved.update", func(f url.Values) bool { return f.Get("mark") == "completed" || f.Get("todo_state") == "completed" }) > 0
	})
	todoUndoFor = 50 * time.Millisecond
	t.Cleanup(func() { todoUndoFor = 6 * time.Second })
	gone := laterKey(&l.view[1])
	kbPress(e, key.NameHome, 0)
	kbPress(e, key.NameDownArrow, 0)
	kbPress(e, key.NameDeleteForward, 0)
	h.mu.Lock()
	undos := append([]string(nil), h.undos...)
	h.mu.Unlock()
	if len(undos) != 1 || undos[0] != "To-do cleared" {
		t.Fatalf("Delete: toasts %q", undos)
	}
	for _, it := range l.view {
		if laterKey(&it) == gone {
			t.Fatal("Delete left the to-do in the list")
		}
	}
	e.until(t, "saved.delete once the toast is gone", func() bool { return log.count("saved.delete", nil) == 1 })

	lists := log.count("saved.list", nil)
	mu.Lock()
	items = append(items, map[string]any{"item_id": fakeslack.ViewsChannel, "item_type": "message", "ts": "1762700000.000100", "todo_state": "to_do", "date_created": 1762700000})
	mu.Unlock()
	fs.Push(map[string]any{"type": "saved_added", "saved": map[string]any{"item_id": fakeslack.ViewsChannel, "item_type": "message",
		"ts": "1762700000.000100", "todo_state": "to_do", "date_created": 1762700000}, "event_ts": "1762700000.000200"})
	e.until(t, "the reload after saved_added", func() bool { return log.count("saved.list", nil) > lists })
}

// Unreads: the virtual list's own Home/End under UnreadsList's commands,
// and the ring only in keyboard mode.
func TestUnreadsHomeEndLikeSlack(t *testing.T) {
	v, _, e, _ := unrEnv(t, false, nil)
	e.click(image.Pt(500, 300))
	if !v.focused || v.kbMode {
		t.Fatalf("after a press: focused %v keyboard mode %v", v.focused, v.kbMode)
	}
	kbPress(e, key.NameEnd, 0)
	if v.active < 0 || v.rows[v.active].kind != unrFooter || !v.kbMode {
		t.Fatalf("End: row %d (keyboard mode %v), want the footer", v.active, v.kbMode)
	}
	kbPress(e, key.NameHome, 0)
	if v.active < 0 || v.rows[v.active].kind != unrHeader {
		t.Fatalf("Home: row %d, want the first header", v.active)
	}
}
