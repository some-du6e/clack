package views

// The views and navigation experiments (core/views_experiments.go,
// docs/EXPERIMENTS.md), each group Slack's code tells apart, on
// internal/fakeslack's views fixtures.

import (
	"fmt"
	"image"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/shortcut"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// ThreadsViewHeader (client-threads-view auBp): with nested_secondary_view
// the header always has its inset line; without it there is none until,
// with view_header_overflow_borders, the list has scrolled more than 15px
// under it. Without either the line never shows.
func TestThreadsHeaderGroups(t *testing.T) {
	app, _ := vaFake(t)
	v := NewThreads(&slickHost{app: app, th: theme.New(false)})
	e := &vaEnv{size: image.Pt(1000, 900)}
	e.draw = func(gtx layout.Context) { v.Layout(gtx) }
	e.until(t, "the followed threads", func() bool { p, ok, _ := v.data.get(); return ok && len(p.items) == 2 })
	gtx := layout.Context{}
	gtx.Metric.PxPerDp = 1
	cases := []struct {
		nested, overflow string
		first, offset    int
		want             int
	}{
		{"on", "on", 0, 0, thrBorderInset},
		{"on", "", 1, 0, thrBorderInset},
		{"", "on", 0, 0, thrBorderNone},
		{"", "on", 0, 15, thrBorderNone}, // isScrolled is scrollTop > 15
		{"", "on", 0, 16, thrBorderBelow},
		{"", "on", 1, 0, thrBorderBelow},
		{"", "", 2, 40, thrBorderNone}, // maybeSetIsScrolled does nothing without the experiment
		{"control", "off", 1, 0, thrBorderNone},
	}
	for _, c := range cases {
		app.SetExperimentGroup("nested_secondary_view", c.nested)
		app.SetExperimentGroup("view_header_overflow_borders", c.overflow)
		v.list.Position = layout.Position{First: c.first, Offset: c.offset}
		if got := v.headBorder(gtx); got != c.want {
			t.Errorf("nested %q, overflow %q, scrolled %d/%d: border %d, want %d", c.nested, c.overflow, c.first, c.offset, got, c.want)
		}
	}
	// agentic_ia turns the nested view off (kl02 W7)
	app.SetExperimentGroup("nested_secondary_view", "on")
	app.SetExperimentGroup("agentic_ia", "on")
	v.list.Position = layout.Position{}
	if got := v.headBorder(gtx); got != thrBorderNone {
		t.Errorf("under agentic_ia: border %d, want none", got)
	}
	app.ClearExperimentGroup("agentic_ia")
	// and both groups draw
	for _, g := range []string{"on", ""} {
		app.SetExperimentGroup("nested_secondary_view", g)
		e.frame()
		vaShot(t, e, "threads-header-nested-"+map[string]string{"on": "on", "": "off"}[g], false)
	}
}

// The Keyboard Shortcuts pane: with keyboard_shortcuts_search its search
// field filters the rows by title and a search with none shows one of
// Slack's three empty states; without it there is no field and the list
// stays whole whatever was typed.
func TestKeyboardShortcutsSearchGroups(t *testing.T) {
	app, _ := vaFake(t)
	opts := func() shortcut.ListOpts {
		return shortcut.ListOpts{GOOS: "darwin", KeyboardLocale: "us", ActivityLabel: "Activity", LaterTodos: true}
	}
	for _, g := range []string{"on", ""} {
		app.SetExperimentGroup("keyboard_shortcuts_search", g)
		v := NewKeyboardShortcuts(&slickHost{app: app, th: theme.New(false)}, opts)
		e := &vaEnv{size: image.Pt(420, 900)}
		e.draw = func(gtx layout.Context) { v.Layout(gtx) }
		e.frame()
		e.frame() // the open's FocusCmd lands
		if focused := e.r.Source().Focused(&v.search.ed); focused != (g == "on") {
			t.Errorf("%q: search field focused %v on open", g, focused)
		}
		all := v.Rows()
		if all[0] != "(subtitle)" || all[1] != "## Basics" {
			t.Fatalf("%q: rows start %v", g, all[:2])
		}
		vaShot(t, e, "keyboard-shortcuts-search-"+map[string]string{"on": "on", "": "off"}[g], false)
		v.SetQuery("thread")
		e.frame()
		got := v.Rows()
		if g == "" {
			if len(got) != len(all) {
				t.Errorf("without the experiment the query filtered: %d rows of %d", len(got), len(all))
			}
			continue
		}
		if len(got) >= len(all) || len(got) < 3 {
			t.Fatalf("filtered rows %v", got)
		}
		for _, r := range got {
			if strings.HasPrefix(r, "(") || strings.HasPrefix(r, "## ") {
				continue
			}
			if !strings.Contains(strings.ToLower(r[:strings.Index(r, " = ")]), "thread") {
				t.Errorf("%q kept for thread", r)
			}
		}
		vaShot(t, e, "keyboard-shortcuts-search-thread", false)
		v.SetQuery("zzzz")
		e.frame()
		if got := v.Rows(); len(got) != 1 || !strings.HasPrefix(got[0], "(empty ") {
			t.Errorf("no match: %v", got)
		}
		vaShot(t, e, "keyboard-shortcuts-search-empty", false)
	}
}

// todoRowsText renders the To-dos rows: dividers with their counts, the
// to-dos by ts.
func todoRowsText(l *Later, rows []todoRow) string {
	var out []string
	for _, r := range rows {
		switch r.kind {
		case tdItem:
			out = append(out, l.view[r.item].TS)
		case tdOverdue:
			out = append(out, fmt.Sprintf("[Overdue %d]", r.count))
		case tdInProgress:
			out = append(out, fmt.Sprintf("[In progress %d]", r.count))
		case tdCompleted:
			out = append(out, fmt.Sprintf("[Completed %d]", r.count))
		case tdNoInProgress:
			out = append(out, "(caught up)")
		case tdSectionEnd:
			out = append(out, "-")
		}
	}
	return strings.Join(out, " ")
}

// The To-dos sections (TodosPage): with later_todos, "In progress" over
// the open to-dos and "Completed" over the completed ones, with
// saved.list's counts; with activity_in_home_prototype too, the overdue
// to-dos under "Overdue" first; without later_todos one plain list. A
// divider collapses its section; with only completed to-dos shown,
// "In progress" stands over "You’re all caught up 🎉".
func TestTodosSectionsGroups(t *testing.T) {
	now := time.Now().Unix()
	var mu sync.Mutex
	items := []map[string]any{
		// overdue: a reminder an hour ago
		{"item_id": fakeslack.ViewsChannel, "item_type": "message", "ts": "1762601000.000100", "todo_state": "to_do",
			"date_created": now - 7200, "date_due": now - 3600},
		// due tomorrow
		{"item_id": fakeslack.ViewsChannel, "item_type": "message", "ts": "1762601001.000100", "todo_state": "to_do",
			"date_created": now - 7000, "date_due": now + 86400},
		// no reminder (date_due = date_created)
		{"item_id": fakeslack.ViewsChannel, "item_type": "message", "ts": "1762601002.000100", "todo_state": "to_do",
			"date_created": now - 6000, "date_due": now - 6000},
		{"item_id": fakeslack.ViewsChannel, "item_type": "message", "ts": "1762601003.000100", "todo_state": "completed",
			"date_created": now - 9000, "date_completed": now - 100},
	}
	app, _ := vkFake(t, func(method string, f url.Values) (any, bool) {
		if method == "saved.list" {
			mu.Lock()
			defer mu.Unlock()
			return map[string]any{"ok": true, "saved_items": append([]map[string]any(nil), items...),
				"counts": map[string]any{"todo_count": 5, "completed_count": 4}}, true
		}
		return nil, false
	})
	app.SetExperimentGroup("later_todos", "on")
	h := newKBHost(app, false)
	l := NewLater(h)
	l.f[todoFInProg], l.f[todoFDone] = true, true
	e := &vaEnv{size: image.Pt(1000, 900)}
	e.draw = func(gtx layout.Context) { l.Layout(gtx) }
	e.until(t, "the to-dos", func() bool { return len(l.view) == 4 })
	e.frame()
	got := todoRowsText(l, l.todoRows(now))
	// the page's due-date sort: the no-reminder to-do (due at its creation) first
	want := "[In progress 5] 1762601002.000100 1762601000.000100 1762601001.000100 - [Completed 4] 1762601003.000100"
	if got != want {
		t.Errorf("later_todos:\n%s\nwant\n%s", got, want)
	}
	vaShot(t, e, "todos-sections", false)

	app.SetExperimentGroup("activity_in_home_prototype", "on")
	e.frame()
	got = todoRowsText(l, l.todoRows(now))
	want = "[Overdue 1] 1762601000.000100 - [In progress 4] 1762601002.000100 1762601001.000100 - [Completed 4] 1762601003.000100"
	if got != want {
		t.Errorf("activity_in_home_prototype:\n%s\nwant\n%s", got, want)
	}
	vaShot(t, e, "todos-sections-overdue", false)
	app.ClearExperimentGroup("activity_in_home_prototype")

	// collapse "In progress": its to-dos go, the divider stays
	l.secCollapsed[todoSecInProgress] = true
	got = todoRowsText(l, l.todoRows(now))
	if want := "[In progress 5] - [Completed 4] 1762601003.000100"; got != want {
		t.Errorf("collapsed: %s", got)
	}
	l.secCollapsed[todoSecInProgress] = false

	// only completed to-dos: In progress over the caught-up row
	mu.Lock()
	items = items[3:]
	mu.Unlock()
	l.reload(&l.feed)
	e.until(t, "the completed to-do alone", func() bool { return len(l.view) == 1 })
	got = todoRowsText(l, l.todoRows(now))
	if want := "[In progress 5] (caught up) - [Completed 4] 1762601003.000100"; got != want {
		t.Errorf("none in progress: %s", got)
	}
	vaShot(t, e, "todos-sections-caught-up", false)

	// without later_todos: one list
	app.SetExperimentGroup("later_todos", "")
	if got := todoRowsText(l, l.todoRows(now)); got != "1762601003.000100" {
		t.Errorf("without later_todos: %s", got)
	}
}

// Threads' "Show N more replies" (LoadOlderMessage, loadOlderReplies):
// N is reply_count less the replies shown (at most 100); a click asks
// conversations.replies for the 100 up to the oldest shown (latest,
// inclusive, limit 100) and shows them in the card above the others,
// without marking the thread read or opening it; at the thread's start
// the row goes.
func TestThreadsShowMoreReplies(t *testing.T) {
	app, fs := vaFake(t)
	ch, root := fakeslack.ViewsChannel, "1762700000.000100"
	msg := func(ts, user, text string) slack.Message {
		return slack.Message{Type: "message", Channel: ch, TS: ts, ThreadTS: root, User: user, Text: text}
	}
	r := msg(root, "U0FAKE00001", "a synthetic long thread")
	r.ReplyCount, r.LatestReply = 7, "1762700700.000100"
	var mu sync.Mutex
	var asks []url.Values
	fs.SetHook(func(method string, f url.Values) (any, bool) {
		switch method {
		case "subscriptions.thread.getView":
			th := map[string]any{"root_msg": r, "unread_replies": []slack.Message{},
				"latest_replies": []slack.Message{msg("1762700600.000100", "U0FAKE00002", "reply 6"), msg("1762700700.000100", "U0FAKE00001", "reply 7")}}
			return map[string]any{"ok": true, "threads": []any{th}, "has_more": false, "total_unread_replies": 0}, true
		case "conversations.replies":
			mu.Lock()
			asks = append(asks, f)
			n := len(asks)
			mu.Unlock()
			if n == 1 { // up to reply 6: replies 3-6 (6 again: inclusive) and more before
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
	h := newKBHost(app, false)
	v := NewThreads(h)
	e := &vaEnv{size: image.Pt(1000, 1400)}
	e.draw = func(gtx layout.Context) { v.Layout(gtx) }
	e.until(t, "the long thread", func() bool { p, ok, _ := v.data.get(); return ok && len(p.items) == 1 })
	e.frame()
	p, _, _ := v.data.get()
	it := &p.items[0]
	c := v.cards[it.key]
	if c == nil || c.moreText != "Show 5 more replies" {
		t.Fatalf("row %q", c.moreText)
	}
	vaShot(t, e, "threads-show-more", false)
	v.loadOlder(it, c)
	e.until(t, "the older replies", func() bool { return !c.olderBusy && len(c.older) == 3 })
	e.frame()
	mu.Lock()
	a := asks[0]
	mu.Unlock()
	if a.Get("channel") != ch || a.Get("ts") != root || a.Get("latest") != "1762700600.000100" || a.Get("limit") != "100" || a.Get("inclusive") != "true" {
		t.Errorf("conversations.replies %v", a)
	}
	if got := thrShown(it, c); len(got) != 5 || got[0].TS != "1762700300.000100" || got[4].TS != "1762700700.000100" {
		t.Errorf("shown %v", got)
	}
	if c.moreText != "Show 2 more replies" {
		t.Errorf("after the first load: %q", c.moreText)
	}
	found := false
	for _, k := range v.kbItems(p.items) {
		found = found || k.key == it.key+"-1762700300.000100"
	}
	if !found {
		t.Error("the loaded reply isn't a keyboard row")
	}
	vaShot(t, e, "threads-show-more-loaded", false)
	v.loadOlder(it, c)
	e.until(t, "the thread's start", func() bool { return !c.olderBusy && len(c.older) == 5 })
	if c.moreText != "" || !c.olderStart {
		t.Errorf("at the start: %q (start %v)", c.moreText, c.olderStart)
	}
	if len(fs.ThreadMarks()) != 0 {
		t.Errorf("loading replies marked the thread: %v", fs.ThreadMarks())
	}
	if _, th := h.last(); th != "" {
		t.Errorf("loading replies opened %s", th)
	}
}
