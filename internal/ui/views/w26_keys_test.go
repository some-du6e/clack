package views

// Work package W26 (docs/GAPS-2026-11-12.md gap 15): Threads' Enter
// handing focus to the channel's list, the focus after "Show N more
// replies", typed text (shifted symbols, any layout) reaching the reply
// box, hasFocus showing a To-dos or Saved row's actions, a tombstoned
// to-do's Enter opening LaterRemoveModal and "View to-dos" selecting its
// row. All on internal/fakeslack (synthetic data only); Slack's cites are
// in the views' comments (build 2cd9270e786c).

import (
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
	"github.com/some-du6e/clack/internal/ui/composer"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// w26Host is kbHost with the ui root's MessageFocuser and secondary
// column.
type w26Host struct {
	*kbHost
	focused          []string // FocusMessage calls: channel/ts
	secCh, secThread string   // what the secondary column shows
}

func (h *w26Host) FocusMessage(ch, ts string) {
	h.mu.Lock()
	h.focused = append(h.focused, ch+"/"+ts)
	h.mu.Unlock()
}

func (h *w26Host) OpenChannelSecondary(ch string) { h.secCh, h.secThread = ch, "" }
func (h *w26Host) SecondaryShows(ch, ts string) bool {
	return ch != "" && ch == h.secCh && ts == h.secThread
}
func (h *w26Host) OpenDetailSecondary(string, layout.Widget) {}
func (h *w26Host) SecondaryShowsDetail(string) bool          { return false }

func newW26Host(app *core.App, dark bool) *w26Host { return &w26Host{kbHost: newKBHost(app, dark)} }

// w26Draft is the draft a new reply box for the thread opens on.
func w26Draft(h composer.Host, ch, ts string) string {
	c := composer.New(h, ch, ts)
	defer c.Close()
	return c.DocText()
}

// kbType is a key press as the OS sends it: the key.Event, the text it
// typed (key.EditEvent; WM_CHAR, XLookupString, insertText), the release.
func kbType(e *vaEnv, n key.Name, m key.Modifiers, text string) {
	e.r.Queue(key.Event{Name: n, Modifiers: m, State: key.Press}, key.EditEvent{Text: text}, key.Event{Name: n, Modifiers: m, State: key.Release})
	e.frame()
	e.frame()
}

func w26Modes(t *testing.T, f func(t *testing.T, dark bool)) {
	for _, dark := range []bool{false, true} {
		t.Run(map[bool]string{false: "light", true: "dark"}[dark], func(t *testing.T) { f(t, dark) })
	}
}

// Typing on a Threads row (onKeyDown, isPrintableKeyEvent): the reply
// box gets exactly the text the OS typed: Shift+1's "!", a letter once
// (not again from the key's name), and text without a key Clack names
// (another layout's "é").
func TestThreadsTypedTextLikeSlack(t *testing.T) {
	w26Modes(t, func(t *testing.T, dark bool) {
		app, _ := vaFake(t)
		h := newW26Host(app, dark)
		v := NewThreads(h)
		e := &vaEnv{size: image.Pt(1000, 1100)}
		e.draw = func(gtx layout.Context) { v.Layout(gtx) }
		e.until(t, "the followed threads", func() bool { p, ok, _ := v.data.get(); return ok && len(p.items) == 2 })
		e.frame()
		e.frame()
		p, _, _ := v.data.get()
		k0 := p.items[0].key
		root := "root-" + k0
		// the reply box opens on any draft the composer kept for this
		// thread (drafts outlive a composer, and earlier tests type here)
		draft := w26Draft(h, p.items[0].Channel, p.items[0].Root.TS)
		kbClickRow(t, e, v.kb, root, 49)
		kbType(e, "1", key.ModShift, "!")
		c := v.cards[k0]
		if c == nil || c.comp == nil {
			t.Fatalf("Shift+1 on a row didn't open the reply box (active %q)", v.kb.active)
		}
		if got := c.comp.DocText(); got != draft+"!" {
			t.Fatalf("Shift+1: reply box %q, want %q (the draft, then one !)", got, draft+"!")
		}
		before := c.comp.DocText()
		kbClickRow(t, e, v.kb, root, 49)
		if !v.kb.focused {
			t.Fatal("a press on the root didn't give the list the focus back")
		}
		kbType(e, "A", 0, "a")
		if got := c.comp.DocText(); got != before+"a" {
			t.Fatalf("A: reply box %q, want %q (the letter once)", got, before+"a")
		}
		kbClickRow(t, e, v.kb, root, 49)
		e.r.Queue(key.EditEvent{Text: "é"})
		e.frame()
		e.frame()
		if got := c.comp.DocText(); got != before+"aé" {
			t.Fatalf("é: reply box %q, want %q", got, before+"aé")
		}
		if v.kb.focused {
			t.Error("the list kept the focus after typing")
		}
		vaShot(t, e, "threads-typed-symbol", dark)
		c.comp.Close()
	})
}

// Enter on a root or reply: jumpToMessage, then transitionFocusTo
// MESSAGE_PANE_MESSAGE_LIST at the root (the host's FocusMessage).
func TestThreadsEnterFocusesTheRootLikeSlack(t *testing.T) {
	app, _ := vaFake(t)
	h := newW26Host(app, false)
	v := NewThreads(h)
	e := &vaEnv{size: image.Pt(1000, 1100)}
	e.draw = func(gtx layout.Context) { v.Layout(gtx) }
	e.until(t, "the followed threads", func() bool { p, ok, _ := v.data.get(); return ok && len(p.items) == 2 })
	e.frame()
	e.frame()
	p, _, _ := v.data.get()
	it := p.items[0]
	kbClickRow(t, e, v.kb, "root-"+it.key, 49)
	kbPress(e, key.NameDownArrow, 0) // the first reply: Enter still goes to the root
	kbPress(e, key.NameReturn, 0)
	want := it.Channel + "/" + it.Root.TS
	h.mu.Lock()
	got := append([]string(nil), h.focused...)
	h.mu.Unlock()
	if nav, _ := h.last(); nav != "c/"+it.Channel+"/m/"+it.Root.TS {
		t.Errorf("Enter went to %q", nav)
	}
	if len(got) != 1 || got[0] != want {
		t.Errorf("FocusMessage calls %q, want [%s]", got, want)
	}
	// Enter on the heading does neither
	kbPress(e, key.NameHome, 0)
	kbPress(e, key.NameReturn, 0)
	h.mu.Lock()
	n := len(h.focused)
	h.mu.Unlock()
	if n != 1 {
		t.Errorf("Enter on the heading focused a message (%d calls)", n)
	}
}

// "Show N more replies": once the older replies are in, focusOnKey moves
// to the reply that was the oldest shown before (minimumVisibleTs), with
// the ring after Enter and without it after a click.
func TestThreadsShowMoreFocusLikeSlack(t *testing.T) {
	w26Modes(t, func(t *testing.T, dark bool) {
		app, fs := vaFake(t)
		ch, root := fakeslack.ViewsChannel, "1762700000.000100"
		msg := func(ts, user, text string) slack.Message {
			return slack.Message{Type: "message", Channel: ch, TS: ts, ThreadTS: root, User: user, Text: text}
		}
		r := msg(root, "U0FAKE00001", "a synthetic long thread")
		r.ReplyCount, r.LatestReply = 7, "1762700700.000100"
		var mu sync.Mutex
		asks := 0
		fs.SetHook(func(method string, f url.Values) (any, bool) {
			switch method {
			case "subscriptions.thread.getView":
				th := map[string]any{"root_msg": r, "unread_replies": []slack.Message{},
					"latest_replies": []slack.Message{msg("1762700600.000100", "U0FAKE00002", "reply 6"), msg("1762700700.000100", "U0FAKE00001", "reply 7")}}
				return map[string]any{"ok": true, "threads": []any{th}, "has_more": false, "total_unread_replies": 0}, true
			case "conversations.replies":
				mu.Lock()
				asks++
				n := asks
				mu.Unlock()
				if n == 1 {
					return map[string]any{"ok": true, "has_more": true, "messages": []slack.Message{r,
						msg("1762700300.000100", "U0FAKE00001", "reply 3"), msg("1762700400.000100", "U0FAKE00002", "reply 4"),
						msg("1762700500.000100", "U0FAKE00001", "reply 5"), msg("1762700600.000100", "U0FAKE00002", "reply 6")}}, true
				}
				return map[string]any{"ok": true, "has_more": false, "messages": []slack.Message{r,
					msg("1762700100.000100", "U0FAKE00001", "reply 1"), msg("1762700200.000100", "U0FAKE00002", "reply 2"),
					msg("1762700300.000100", "U0FAKE00001", "reply 3")}}, true
			}
			return nil, false
		})
		h := newW26Host(app, dark)
		v := NewThreads(h)
		e := &vaEnv{size: image.Pt(1000, 1400)}
		e.draw = func(gtx layout.Context) { v.Layout(gtx) }
		e.until(t, "the long thread", func() bool { p, ok, _ := v.data.get(); return ok && len(p.items) == 1 })
		e.frame()
		e.frame()
		p, _, _ := v.data.get()
		it := &p.items[0]
		c := v.cards[it.key]
		kbClickRow(t, e, v.kb, "root-"+it.key, 49)
		kbPress(e, key.NameDownArrow, 0)
		if v.kb.active != "load_older-"+it.key {
			t.Fatalf("↓ from the root: %q, want the load_older row", v.kb.active)
		}
		kbPress(e, key.NameReturn, 0)
		e.until(t, "the older replies", func() bool { e.frame(); return !c.olderBusy && len(c.older) == 3 })
		e.frame()
		if want := it.key + "-1762700600.000100"; v.kb.active != want || !v.kb.ring {
			t.Fatalf("after Enter's load: active %q ring %v, want %q (reply 6, the oldest shown before) with the ring", v.kb.active, v.kb.ring, want)
		}
		vaShot(t, e, "threads-show-more-focus", dark)
		kbClickRow(t, e, v.kb, "load_older-"+it.key, 49)
		e.until(t, "the thread's start", func() bool { e.frame(); return !c.olderBusy && len(c.older) == 5 })
		e.frame()
		if want := it.key + "-1762700300.000100"; v.kb.active != want || v.kb.ring {
			t.Fatalf("after a click's load: active %q ring %v, want %q without the ring", v.kb.active, v.kb.ring, want)
		}
	})
}

// w26LiveTS is a message messages.list has: fakeslack answers it with
// the Activity fixture's messages ("a synthetic keyword match", a
// top-level message in ViewsChannel). The other items' ts are in no
// fixture, so messages.list answers without them: deleted, Gone.
const w26LiveTS = "1762700400.000100"

// laterItems serves saved.list: n to-dos on unknown timestamps (gone
// from the history), plus one on a message the fixtures have.
func laterItems(t *testing.T, todo string, n int, live bool) (*core.App, *callLog) {
	t.Helper()
	now := time.Now().Unix()
	var items []map[string]any
	for i := range n {
		items = append(items, map[string]any{"item_id": fakeslack.ViewsChannel, "item_type": "message",
			"ts": fmt.Sprintf("%d.000100", 1762601000+i), "todo_state": todo, "date_created": now - 6000 + int64(i)})
	}
	if live {
		items = append(items, map[string]any{"item_id": fakeslack.ViewsChannel, "item_type": "message",
			"ts": w26LiveTS, "todo_state": todo, "date_created": now - 100})
	}
	log := &callLog{}
	var mu sync.Mutex
	app, _ := vkFake(t, func(method string, f url.Values) (any, bool) {
		log.rec(method, f)
		if method == "saved.list" {
			mu.Lock()
			defer mu.Unlock()
			return map[string]any{"ok": true, "saved_items": append([]map[string]any(nil), items...)}, true
		}
		return nil, false
	})
	return app, log
}

// hasFocus: a focused To-dos row shows its actions as on hover
// (p-saved_item__actions--active), a press and the arrows both; rows
// without the focus or the pointer don't.
func TestTodosFocusedRowActionsLikeSlack(t *testing.T) {
	w26Modes(t, func(t *testing.T, dark bool) {
		app, _ := laterItems(t, "to_do", 4, false)
		h := newW26Host(app, dark)
		l := NewLater(h)
		e := &vaEnv{size: image.Pt(1000, 900)}
		e.draw = func(gtx layout.Context) { l.Layout(gtx) }
		e.until(t, "the to-dos", func() bool { return len(l.view) == 4 })
		e.frame()
		e.frame()
		keys := []string{laterKey(&l.view[0]), laterKey(&l.view[1]), laterKey(&l.view[2]), laterKey(&l.view[3])}
		for _, k := range keys {
			if l.rowState(k).barOn {
				t.Fatalf("%s shows its actions before anything has the focus", k)
			}
		}
		kbClickRow(t, e, l.kb, keys[0], e.size.Y-l.kb.viewH)
		kbPress(e, key.NameDownArrow, 0)
		kbPress(e, key.NameDownArrow, 0)
		if l.kb.active != keys[2] {
			t.Fatalf("↓↓: active %q", l.kb.active)
		}
		if !l.rowState(keys[2]).barOn {
			t.Error("the keyboard-focused row doesn't show its actions")
		}
		if l.rowState(keys[1]).barOn || l.rowState(keys[3]).barOn {
			t.Error("a row without the focus or the pointer shows its actions")
		}
		vaShot(t, e, "todos-focus-actions", dark)
	})
}

// The Saved page's rows pass hasFocus || hasFocusWithin too (eQN2 eM).
func TestSavedFocusedRowActionsLikeSlack(t *testing.T) {
	w26Modes(t, func(t *testing.T, dark bool) {
		app, _ := laterItems(t, "saved", 3, false)
		h := newW26Host(app, dark)
		s := NewSaved(h)
		e := &vaEnv{size: image.Pt(1000, 900)}
		e.draw = func(gtx layout.Context) { s.Layout(gtx) }
		e.until(t, "the saved items", func() bool { return len(s.feed.items) == 3 })
		e.frame()
		e.frame()
		k0, k1, k2 := laterKey(&s.feed.items[0]), laterKey(&s.feed.items[1]), laterKey(&s.feed.items[2])
		kbClickRow(t, e, s.kb, k0, e.size.Y-s.kb.viewH)
		kbPress(e, key.NameDownArrow, 0)
		if s.kb.active != k1 {
			t.Fatalf("↓: active %q, want %q", s.kb.active, k1)
		}
		if !s.rowState(k1).barOn || s.rowState(k2).barOn {
			t.Errorf("actions: focused row %v, the next %v", s.rowState(k1).barOn, s.rowState(k2).barOn)
		}
		vaShot(t, e, "saved-focus-actions", dark)
	})
}

// A tombstoned to-do's Enter dispatches openLaterRemoveModal (Kivd):
// LaterRemoveModal, whose Remove unsaves it; a to-do whose message is
// there opens it as before.
func TestTodosTombstoneEnterLikeSlack(t *testing.T) {
	w26Modes(t, func(t *testing.T, dark bool) {
		app, log := laterItems(t, "to_do", 1, true)
		app.SetExperimentGroup("later_todos", "on")
		h := newW26Host(app, dark)
		l := NewLater(h)
		e := &vaEnv{size: image.Pt(1000, 900)}
		e.draw = func(gtx layout.Context) {
			l.Layout(gtx)
			for _, o := range h.open() {
				o.Layout(gtx)
			}
		}
		e.until(t, "the to-dos", func() bool { return len(l.view) == 2 })
		e.frame()
		e.frame()
		var gone, live string
		for i := range l.view {
			it := &l.view[i]
			switch {
			case it.TS == w26LiveTS:
				live = laterKey(it)
				if it.Msg == nil || laterTombstoned(it) {
					t.Fatalf("the fixture message wasn't hydrated: %+v", it)
				}
			default:
				gone = laterKey(it)
				if !it.Gone || !laterTombstoned(it) {
					t.Fatalf("a to-do on a missing message isn't a tombstone: %+v", it)
				}
			}
		}
		listY := e.size.Y - l.kb.viewH
		kbClickRow(t, e, l.kb, live, listY)
		navs := len(h.navs)
		kbPress(e, key.NameReturn, 0)
		if nav, _ := h.last(); len(h.navs) == navs || nav != "c/"+fakeslack.ViewsChannel {
			t.Errorf("Enter on a live to-do went to %q", nav)
		}
		if len(h.open()) != 0 {
			t.Fatal("Enter on a live to-do opened a dialog")
		}
		kbClickRow(t, e, l.kb, gone, listY)
		navs = len(h.navs)
		kbPress(e, key.NameReturn, 0)
		m := actLastModal(h.vaHost)
		if m == nil {
			t.Fatal("Enter on a tombstoned to-do opened no LaterRemoveModal")
		}
		if len(h.navs) != navs {
			t.Errorf("Enter on a tombstoned to-do also navigated: %q", h.navs[len(h.navs)-1])
		}
		vaShot(t, e, "todos-remove-modal", dark)
		m.PressDefault()
		e.until(t, "saved.delete", func() bool { e.frame(); return log.count("saved.delete", nil) == 1 })
	})
}

// LaterRemoveModal's copy (6Fvj over w6zd) for a deleted message.
func TestLaterRemoveCopyLikeSlack(t *testing.T) {
	todo := &core.SavedItem{ItemType: "message", TodoState: "to_do"}
	saved := &core.SavedItem{ItemType: "message", TodoState: "saved"}
	for _, c := range []struct {
		it          *core.SavedItem
		todos       bool
		title, body string
	}{
		{todo, true, "Remove to-do", "A message you added as a to-do was deleted. Remove it from To-dos?"},
		{saved, true, "Remove saved item", "A message you saved was deleted. Remove it from Saved?"},
		{todo, false, "Remove saved item", "A message you saved was deleted. Remove it from Later?"},
	} {
		if title, body := laterRemoveCopy(c.it, c.todos); title != c.title || body != c.body {
			t.Errorf("%s, later_todos %v: %q / %q, want %q / %q", c.it.TodoState, c.todos, title, body, c.title, c.body)
		}
	}
	if laterTombstoned(&core.SavedItem{ItemType: "message", Channel: "C1", TS: "1.2"}) {
		t.Error("a message that couldn't be looked up (not Gone) counts as deleted")
	}
	if !laterTombstoned(&core.SavedItem{ItemType: "message", Channel: "C1", TS: "1.2", Msg: &core.Message{Tombstone: true}}) {
		t.Error("a tombstone message isn't tombstoned")
	}
}

// "View to-dos" (wRj5): the to-do whose message the secondary column
// shows is rowSelected (surf-hgl-3); a collapsed section holding it
// opens; closing the column clears it.
func TestTodosViewSelectsLikeSlack(t *testing.T) {
	w26Modes(t, func(t *testing.T, dark bool) {
		app, _ := laterItems(t, "to_do", 3, false)
		app.SetExperimentGroup("later_todos", "on")
		h := newW26Host(app, dark)
		l := NewLater(h)
		e := &vaEnv{size: image.Pt(1000, 900)}
		e.draw = func(gtx layout.Context) { l.Layout(gtx) }
		e.until(t, "the to-dos", func() bool { return len(l.view) == 3 })
		e.frame()
		it := l.view[1]
		l.secCollapsed[todoSecInProgress] = true
		e.frame()
		h.OpenChannelSecondary(it.Channel)
		l.SelectTodo(it.Channel, it.TS, "")
		e.frame()
		e.frame()
		e.frame()
		if l.secCollapsed[todoSecInProgress] {
			t.Fatal("the selected to-do's section stayed collapsed")
		}
		rows := l.todoRows(todoNow())
		if got := l.todoSelection(rows); got != laterKey(&it) {
			t.Fatalf("selected %q, want %q", got, laterKey(&it))
		}
		vaShot(t, e, "todos-view-selected", dark)
		// collapsing it again by hand keeps it collapsed (once per selection)
		l.secCollapsed[todoSecInProgress] = true
		e.frame()
		e.frame()
		if !l.secCollapsed[todoSecInProgress] {
			t.Error("the section reopened after the user collapsed it")
		}
		l.secCollapsed[todoSecInProgress] = false
		h.secCh = ""
		e.frame()
		if got := l.todoSelection(l.todoRows(todoNow())); got != "" {
			t.Errorf("with the column closed the row is still selected (%q)", got)
		}
	})
}

var _ popups.Host = (*vaHost)(nil)
