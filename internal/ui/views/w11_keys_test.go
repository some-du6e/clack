package views

// Work package W11 (docs/GAPS-2026-11-11.md gap 11): the To-dos sections'
// own keyboard lists with the dividers as Tab stops, the Sent tab's and
// the Saved page's keyboard, Unreads' unfocused scrolling keys and the DMs
// list's empty state. All on internal/fakeslack (synthetic data only);
// Slack's cites are in the views' comments (build d3de3a487b1c).

import (
	"fmt"
	"image"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

// To-dos with later_todos: "In progress" and "Completed" are separate
// lists. End stops at the last in-progress row, ↑/↓ don't cross into the
// other list, Tab goes divider → list → divider → list (each list at its
// own initial row) and Shift+Tab back; a click on a divider focuses it
// without the ring; a cleared row hands focus to the row now at its index.
func TestTodosSectionListsLikeSlack(t *testing.T) {
	for _, dark := range []bool{false, true} {
		t.Run(map[bool]string{false: "light", true: "dark"}[dark], func(t *testing.T) { todosSectionLists(t, dark) })
	}
}

func todosSectionLists(t *testing.T, dark bool) {
	now := time.Now().Unix()
	var mu sync.Mutex
	var items []map[string]any
	for i := range 3 {
		items = append(items, map[string]any{"item_id": fakeslack.ViewsChannel, "item_type": "message",
			"ts": fmt.Sprintf("%d.000100", 1762601000+i), "todo_state": "to_do", "date_created": now - 6000 + int64(i)})
	}
	for i := range 2 {
		items = append(items, map[string]any{"item_id": fakeslack.ViewsChannel, "item_type": "message",
			"ts": fmt.Sprintf("%d.000100", 1762602000+i), "todo_state": "completed", "date_created": now - 9000 + int64(i), "date_completed": now - 100 + int64(i)})
	}
	log := &callLog{}
	app, _ := vkFake(t, func(method string, f url.Values) (any, bool) {
		log.rec(method, f)
		if method == "saved.list" {
			mu.Lock()
			defer mu.Unlock()
			return map[string]any{"ok": true, "saved_items": append([]map[string]any(nil), items...),
				"counts": map[string]any{"todo_count": 3, "completed_count": 2}}, true
		}
		return nil, false
	})
	app.SetExperimentGroup("later_todos", "on")
	h := newKBHost(app, dark)
	l := NewLater(h)
	l.f[todoFInProg], l.f[todoFDone] = true, true
	e := &vaEnv{size: image.Pt(1000, 900)}
	e.draw = func(gtx layout.Context) { l.Layout(gtx) }
	e.until(t, "the to-dos", func() bool { return len(l.view) == 5 })
	e.frame()
	e.frame()
	in, done := l.sk.keys[todoSecInProgress], l.sk.keys[todoSecCompleted]
	if len(in) != 3 || len(done) != 2 {
		t.Fatalf("section lists: in progress %q, completed %q", in, done)
	}
	if got := l.todoStops(); fmt.Sprint(got) != "[{true 1} {false 1} {true 2} {false 2}]" {
		t.Fatalf("tab stops %v, want In progress's divider and list, then Completed's", got)
	}
	listY := e.size.Y - l.kb.viewH
	src := e.r.Source()
	focused := func(tag any) bool { return src.Focused(tag) }

	kbClickRow(t, e, l.kb, in[0], listY)
	if l.kb.active != in[0] || l.kb.ring {
		t.Fatalf("after a press: active %q ring %v", l.kb.active, l.kb.ring)
	}
	kbPress(e, key.NameEnd, 0)
	if l.kb.active != in[2] {
		t.Fatalf("End: %q, want the last in-progress to-do %q (not a completed one)", l.kb.active, in[2])
	}
	kbPress(e, key.NameDownArrow, 0)
	if l.kb.active != in[2] {
		t.Fatalf("↓ at the end of In progress left its list: %q", l.kb.active)
	}

	kbPress(e, key.NameTab, 0)
	if !focused(&l.secClick[todoSecCompleted]) || !l.sk.ring[todoSecCompleted] {
		t.Fatalf("Tab from In progress's list: completed divider focused %v ring %v", focused(&l.secClick[todoSecCompleted]), l.sk.ring[todoSecCompleted])
	}
	vaShot(t, e, "todos-divider-focus", dark)
	kbPress(e, key.NameTab, 0)
	if !focused(l.kb) || l.kb.active != done[0] || !l.kb.ring {
		t.Fatalf("Tab from the divider: list focused %v, active %q ring %v, want %q", focused(l.kb), l.kb.active, l.kb.ring, done[0])
	}
	kbPress(e, key.NameUpArrow, 0)
	if l.kb.active != done[0] {
		t.Fatalf("↑ at the top of Completed left its list: %q", l.kb.active)
	}
	kbPress(e, key.NameDownArrow, 0)
	if l.kb.active != done[1] {
		t.Fatalf("↓ in Completed: %q, want %q", l.kb.active, done[1])
	}
	vaShot(t, e, "todos-completed-list-focus", dark)
	kbPress(e, key.NameTab, key.ModShift)
	if !focused(&l.secClick[todoSecCompleted]) {
		t.Fatal("Shift+Tab from Completed's list didn't reach its divider")
	}
	kbPress(e, key.NameTab, key.ModShift)
	if !focused(l.kb) || l.kb.active != in[2] {
		t.Fatalf("Shift+Tab to In progress's list: focused %v active %q, want its initial row %q", focused(l.kb), l.kb.active, in[2])
	}
	kbPress(e, key.NameTab, key.ModShift)
	if !focused(&l.secClick[todoSecInProgress]) {
		t.Fatal("Shift+Tab from In progress's list didn't reach its divider")
	}
	// Enter on a divider collapses its section; the list's stop goes
	kbPress(e, key.NameReturn, 0)
	if !l.secCollapsed[todoSecInProgress] {
		t.Fatal("Enter on the In progress divider didn't collapse it")
	}
	if got := l.todoStops(); fmt.Sprint(got) != "[{true 1} {true 2} {false 2}]" {
		t.Fatalf("collapsed In progress: stops %v", got)
	}
	kbPress(e, key.NameReturn, 0)
	if l.secCollapsed[todoSecInProgress] {
		t.Fatal("a second Enter didn't expand In progress")
	}

	// a click on a divider focuses it without the ring
	kbClickRow(t, e, l.kb, todoDivKey(todoSecCompleted), listY)
	if !focused(&l.secClick[todoSecCompleted]) || l.sk.ring[todoSecCompleted] || !l.secCollapsed[todoSecCompleted] {
		t.Fatalf("divider click: focused %v ring %v collapsed %v", focused(&l.secClick[todoSecCompleted]), l.sk.ring[todoSecCompleted], l.secCollapsed[todoSecCompleted])
	}
	kbClickRow(t, e, l.kb, todoDivKey(todoSecCompleted), listY)
	if l.secCollapsed[todoSecCompleted] {
		t.Fatal("a second click didn't expand Completed")
	}

	// clearing the focused row: the row at its index in the same list
	todoUndoFor = 50 * time.Millisecond
	t.Cleanup(func() { todoUndoFor = 6 * time.Second })
	kbClickRow(t, e, l.kb, in[2], listY)
	kbPress(e, key.NameDeleteForward, 0)
	e.frame()
	if want := l.sk.keys[todoSecInProgress]; len(want) != 2 || l.kb.active != want[1] || !focused(l.kb) {
		t.Fatalf("after Delete: active %q (focused %v), want the in-progress row now at index 2 → %q", l.kb.active, focused(l.kb), want)
	}
	e.until(t, "saved.delete once the toast is gone", func() bool { return log.count("saved.delete", nil) == 1 })
}

// Drafts & sent's Sent tab: SentPage's list keyboard. The education card
// and the messages are focusable, the day dividers aren't; Enter opens the
// message (its thread for a reply).
func TestSentKeysLikeSlack(t *testing.T) {
	for _, dark := range []bool{false, true} {
		t.Run(map[bool]string{false: "light", true: "dark"}[dark], func(t *testing.T) { sentKeys(t, dark) })
	}
}

func sentKeys(t *testing.T, dark bool) {
	now := time.Now()
	ts := []string{
		fmt.Sprintf("%d.000100", now.Add(-time.Minute).Unix()),
		fmt.Sprintf("%d.000200", now.Add(-2*time.Minute).Unix()),
		fmt.Sprintf("%d.000300", now.AddDate(0, 0, -3).Unix()),
	}
	app, _ := vkFake(t, func(method string, f url.Values) (any, bool) {
		if method == "messages.sent.list" {
			var its []map[string]any
			for _, x := range ts {
				its = append(its, map[string]any{"channel_id": fakeslack.ViewsChannel, "ts": x})
			}
			return map[string]any{"ok": true, "items": its, "pagination": map[string]any{"page": 1, "total_count": len(ts)}}, true
		}
		return nil, false
	})
	self := app.S.SelfID()
	for i, x := range ts {
		thread := ""
		if i == 1 {
			thread = ts[2] // a reply
		}
		app.S.Upsert(fakeslack.ViewsChannel, slack.Message{Type: "message", User: self, TS: x, ThreadTS: thread, Text: fmt.Sprintf("synthetic sent message %d", i)})
	}
	h := newKBHost(app, dark)
	d := NewDrafts(h)
	d.tabs.Sel = draftsTabSent
	e := &vaEnv{size: image.Pt(1000, 900)}
	e.draw = func(gtx layout.Context) {
		d.Layout(gtx)
		for _, o := range h.open() {
			o.Layout(gtx)
		}
	}
	e.until(t, "the sent messages", func() bool { return len(d.sentMsgs) == 3 && d.viewTab == draftsTabSent })
	e.frame()
	e.frame()
	kb := d.kb[draftsTabSent]
	var msgs []string
	card := false
	for _, it := range kb.items {
		switch {
		case strings.HasPrefix(it.key, "s:"):
			if !it.focusable {
				t.Errorf("message %s isn't focusable", it.key)
			}
			msgs = append(msgs, it.key)
		case it.key == "sent-page-education-card":
			card = it.focusable
		case it.focusable:
			t.Errorf("%s is focusable (only the messages and the education card are)", it.key)
		}
	}
	if len(msgs) != 3 {
		t.Fatalf("sent rows %q", msgs)
	}
	listY := e.size.Y - kb.viewH
	kbClickRow(t, e, kb, msgs[0], listY)
	if kb.active != msgs[0] || kb.ring {
		t.Fatalf("after a press: active %q ring %v", kb.active, kb.ring)
	}
	h.mu.Lock()
	h.navs, h.threads = nil, nil
	h.mu.Unlock()
	kbPress(e, key.NameDownArrow, 0)
	kbPress(e, key.NameDownArrow, 0) // over the day divider
	if kb.active != msgs[2] || !kb.ring {
		t.Fatalf("↓↓: %q ring %v, want %q past the divider", kb.active, kb.ring, msgs[2])
	}
	vaShot(t, e, "sent-keyboard-focus", dark)
	kbPress(e, key.NameReturn, 0)
	if nav, _ := h.last(); nav != "c/"+fakeslack.ViewsChannel {
		t.Errorf("Enter on a message went to %q", nav)
	}
	kbPress(e, key.NameUpArrow, 0)
	kbPress(e, key.NameReturn, 0)
	if _, th := h.last(); th != fakeslack.ViewsChannel+"/"+ts[2] {
		t.Errorf("Enter on a reply opened thread %q", th)
	}
	if card {
		kbPress(e, key.NameHome, 0)
		if kb.active != "sent-page-education-card" {
			t.Errorf("Home: %q, want the education card", kb.active)
		}
		vaShot(t, e, "sent-education-focus", dark)
	}
}

// The Saved page: BookmarksPage's list keyboard (every row, Enter opens).
func TestSavedKeysLikeSlack(t *testing.T) {
	for _, dark := range []bool{false, true} {
		t.Run(map[bool]string{false: "light", true: "dark"}[dark], func(t *testing.T) { savedKeys(t, dark) })
	}
}

func savedKeys(t *testing.T, dark bool) {
	var items []map[string]any
	for i := range 3 {
		items = append(items, map[string]any{"item_id": fakeslack.ViewsChannel, "item_type": "message",
			"ts": fmt.Sprintf("%d.000100", 1762603000+i), "date_created": 1762603000 + i})
	}
	app, _ := vkFake(t, func(method string, f url.Values) (any, bool) {
		if method == "saved.list" {
			return map[string]any{"ok": true, "saved_items": items}, true
		}
		return nil, false
	})
	h := newKBHost(app, dark)
	s := NewSaved(h)
	e := &vaEnv{size: image.Pt(1000, 900)}
	e.draw = func(gtx layout.Context) { s.Layout(gtx) }
	e.until(t, "the saved items", func() bool { return len(s.feed.items) == 3 && len(s.kb.items) == 3 })
	e.frame()
	e.frame()
	first, second := s.kb.items[0].key, s.kb.items[1].key
	kbClickRow(t, e, s.kb, first, e.size.Y-s.kb.viewH)
	if s.kb.active != first || s.kb.ring {
		t.Fatalf("after a press: active %q ring %v", s.kb.active, s.kb.ring)
	}
	h.mu.Lock()
	h.navs = nil
	h.mu.Unlock()
	kbPress(e, key.NameDownArrow, 0)
	if s.kb.active != second || !s.kb.ring {
		t.Fatalf("↓: %q ring %v, want %q", s.kb.active, s.kb.ring, second)
	}
	vaShot(t, e, "saved-keyboard-focus", dark)
	kbPress(e, key.NameReturn, 0)
	if nav, _ := h.last(); nav != "c/"+fakeslack.ViewsChannel {
		t.Errorf("Enter on a saved message went to %q", nav)
	}
	kbPress(e, key.NameEnd, 0)
	if s.kb.active != s.kb.items[2].key {
		t.Errorf("End: %q", s.kb.active)
	}
}

// Unreads with nothing focused: the list's global keys scroll it (↑/↓
// 40px, PageUp/PageDown a page, Home/End) and make no row active.
func TestUnreadsGlobalScrollLikeSlack(t *testing.T) {
	v, _, e, _ := unrEnv(t, false, nil)
	e.frame()
	if !e.r.Source().Focused(nil) {
		t.Fatal("something holds key focus before the test")
	}
	active := v.active
	kbPress(e, key.NameDownArrow, 0)
	if p := v.list.Position; p.First == 0 && p.Offset != 40 {
		t.Fatalf("↓ unfocused: position %+v, want 40px down", p)
	}
	kbPress(e, key.NameUpArrow, 0)
	if p := v.list.Position; p.First != 0 || p.Offset != 0 {
		t.Fatalf("↑ unfocused: position %+v, want the top", p)
	}
	kbPress(e, key.NamePageDown, 0)
	if p := v.list.Position; p.First == 0 && p.Offset == 0 {
		t.Fatal("PageDown unfocused didn't scroll")
	}
	kbPress(e, key.NameEnd, 0)
	if p := v.list.Position; p.First+p.Count < len(v.rows) || p.BeforeEnd {
		t.Fatalf("End unfocused: position %+v of %d rows, want the end", p, len(v.rows))
	}
	kbPress(e, key.NameHome, 0)
	if p := v.list.Position; p.First != 0 || p.Offset != 0 {
		t.Fatalf("Home unfocused: position %+v", p)
	}
	if v.active != active || v.kbMode {
		t.Fatalf("the global keys made row %d active (keyboard mode %v)", v.active, v.kbMode)
	}
	// focused, the keys move the active row instead
	e.click(image.Pt(500, 300))
	kbPress(e, key.NameDownArrow, 0)
	if v.active == active || !v.kbMode {
		t.Fatalf("↓ focused: row %d keyboard mode %v", v.active, v.kbMode)
	}
}

// DMs with Unreads on and nothing unread: DmsList's empty state, one of
// getUnreadsEmptyPageData's five, with "Show all DMs", which turns Unreads
// off and focuses the list (no ring after a click).
func TestDMsUnreadsEmptyLikeSlack(t *testing.T) {
	for _, dark := range []bool{false, true} {
		t.Run(map[bool]string{false: "light", true: "dark"}[dark], func(t *testing.T) { dmsUnreadsEmpty(t, dark) })
	}
}

func dmsUnreadsEmpty(t *testing.T, dark bool) {
	const dm = "D0FAKEW1101"
	ts := fmt.Sprintf("%d.000100", time.Now().Add(-time.Hour).Unix())
	app, _ := vkFake(t, func(method string, f url.Values) (any, bool) {
		if method == "client.dms" {
			return map[string]any{"ok": true, "ims": []map[string]any{{
				"id":      dm,
				"message": map[string]any{"type": "message", "user": "U0FAKE00001", "ts": ts, "text": "a synthetic read DM"},
				"channel": map[string]any{"id": dm, "is_im": true, "user": "U0FAKE00001"},
			}}, "mpims": []any{}}, true
		}
		return nil, false
	})
	h := newKBHost(app, dark)
	d := NewDMs(h)
	d.unreads.Value = true
	e := &vaEnv{size: image.Pt(1000, 800)}
	e.draw = func(gtx layout.Context) { d.Layout(gtx) }
	e.until(t, "the DMs", func() bool { return len(d.items) == 1 })
	e.frame()
	e.frame()
	if len(d.view) != 0 {
		t.Fatalf("Unreads on shows %d rows for a read DM", len(d.view))
	}
	st := actUnreadsEmpty[d.emptyPick%len(actUnreadsEmpty)]
	titles := map[string]bool{}
	for _, s := range actUnreadsEmpty {
		titles[s.title] = true
	}
	if !titles[st.title] {
		t.Fatalf("empty state %q isn't one of getUnreadsEmptyPageData's", st.title)
	}
	for i := range len(actUnreadsEmpty) {
		d.emptyPick = i
		vaShot(t, e, fmt.Sprintf("dms-unreads-empty-%d", i), dark)
	}
	picks := map[int]bool{}
	for range 200 {
		p := dmsEmptyPick()
		if p < 0 || p >= len(actUnreadsEmpty) {
			t.Fatalf("pick %d out of range", p)
		}
		picks[p] = true
	}
	if len(picks) < 2 {
		t.Fatalf("200 picks gave only %v", picks)
	}
	if got := d.skinTone("raised_hands"); got != "raised_hands" {
		t.Errorf("no skin tone pref: %q", got)
	}

	d.showAll.Click()
	e.frame()
	e.frame()
	e.frame()
	if d.unreads.Value {
		t.Fatal("Show all DMs left Unreads on")
	}
	if len(d.view) != 1 || !d.kb.focused || d.kb.active != dm || d.kb.ring {
		t.Fatalf("after Show all DMs: %d rows, list focused %v, active %q, ring %v", len(d.view), d.kb.focused, d.kb.active, d.kb.ring)
	}
}
