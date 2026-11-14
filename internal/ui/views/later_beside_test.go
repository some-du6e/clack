package views

// Work package W42 (docs/GAPS-2026-11-14.md gap 11): a To-dos or Saved
// row's click opens its message beside the page in the secondary column
// (Slack's jumpToSavedMessage, where L1Ms puts it), keeps the list and
// selects the row; a reply or a thread's root opens its thread at the
// message; a deleted message's click opens LaterRemoveModal. On
// internal/fakeslack with made-up messages only; Slack's cites are in
// later_beside.go.

import (
	"encoding/json"
	"image"
	"maps"
	"net/url"
	"testing"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
)

// The made-up saved messages, in ViewsChannel.
const (
	w42TopTS   = "1762800100.000100" // outside any thread
	w42RootTS  = "1762800200.000100" // a thread's root (its thread_ts its own ts)
	w42ReplyTS = "1762800300.000100" // its reply
	w42GoneTS  = "1762800900.000100" // a message messages.list doesn't have
)

// w42Items serves saved.list with four items in todo state on those
// messages and messages.list with the three that exist (the fourth comes
// back Gone, deleted).
func w42Items(t *testing.T, todo string) *core.App {
	t.Helper()
	now := time.Now().Unix()
	item := func(ts string, age int64) map[string]any {
		return map[string]any{"item_id": fakeslack.ViewsChannel, "item_type": "message", "ts": ts, "todo_state": todo, "date_created": now - age}
	}
	items := []map[string]any{item(w42TopTS, 100), item(w42ReplyTS, 200), item(w42RootTS, 300), item(w42GoneTS, 400)}
	msgs := []map[string]any{
		{"type": "message", "ts": w42TopTS, "user": "U0FAKE00001", "text": "a synthetic plan to look over"},
		{"type": "message", "ts": w42RootTS, "thread_ts": w42RootTS, "user": "U0FAKE00002", "text": "a synthetic question",
			"reply_count": 1, "latest_reply": w42ReplyTS, "reply_users": []string{"U0FAKE00001"}},
		{"type": "message", "ts": w42ReplyTS, "thread_ts": w42RootTS, "user": "U0FAKE00001", "text": "a synthetic answer"},
	}
	app, _ := vkFake(t, func(method string, f url.Values) (any, bool) {
		switch method {
		case "saved.list":
			return map[string]any{"ok": true, "saved_items": items}, true
		case "messages.list":
			var want []struct {
				Channel    string   `json:"channel"`
				Timestamps []string `json:"timestamps"`
			}
			_ = json.Unmarshal([]byte(f.Get("message_ids")), &want)
			out := map[string][]map[string]any{}
			for _, w := range want {
				for _, ts := range w.Timestamps {
					for _, m := range msgs {
						if w.Channel == fakeslack.ViewsChannel && m["ts"] == ts {
							out[w.Channel] = append(out[w.Channel], m)
						}
					}
				}
			}
			return map[string]any{"ok": true, "messages": out}, true
		}
		return nil, false
	})
	return app
}

func w42Key(ts string) string { return fakeslack.ViewsChannel + "/" + ts }

// w42SavedPage sets the has_starred_item pref (6owI hq, with later_todos)
// among the prefs the core has.
func w42SavedPage(app *core.App, on bool) {
	app.S.RLock()
	raw := maps.Clone(app.S.RawPrefs)
	app.S.RUnlock()
	if raw == nil {
		raw = map[string]json.RawMessage{}
	}
	delete(raw, "has_starred_item")
	if on {
		raw["has_starred_item"] = json.RawMessage("true")
	}
	app.S.SetRawPrefs(raw)
}

// A To-dos row (kmjU, ZrhC F): a message outside a thread opens its
// conversation beside the page at the message (jpad R9), the list stays
// and the row is selected; a reply opens its thread at the reply, a
// thread's root its thread at the root (toThread's replyTs); none of
// them navigates. A deleted message opens LaterRemoveModal and nothing
// else (Kivd).
func TestTodosRowClickLikeSlack(t *testing.T) {
	w26Modes(t, func(t *testing.T, dark bool) {
		app := w42Items(t, "to_do")
		app.SetExperimentGroup("later_todos", "on")
		h := newW26Host(app, dark)
		l := NewLater(h)
		// the page's width beside the column (45% of a 1440 window's panel)
		e := &vaEnv{size: image.Pt(620, 900)}
		e.draw = func(gtx layout.Context) {
			l.Layout(gtx)
			for _, o := range h.open() {
				o.Layout(gtx)
			}
		}
		e.until(t, "the to-dos", func() bool { return len(l.view) == 4 })
		e.frame()
		e.frame()
		for _, it := range l.view {
			if gone := it.TS == w42GoneTS; laterTombstoned(&it) != gone || gone == (it.Msg != nil) {
				t.Fatalf("the fixture's hydration: %+v", it)
			}
		}
		listY := e.size.Y - l.kb.viewH
		selected := func() string { return l.todoSelection(l.todoRows(todoNow())) }
		for _, c := range []struct {
			name, ts, thread string
		}{
			{"a message outside a thread", w42TopTS, ""},
			{"a reply", w42ReplyTS, w42RootTS},
			{"a thread's root", w42RootTS, w42RootTS},
		} {
			kbClickRow(t, e, l.kb, w42Key(c.ts), listY)
			if h.secCh != fakeslack.ViewsChannel || h.secThread != c.thread || h.secAt != c.ts {
				t.Fatalf("%s: the column shows %q thread %q at %q, want thread %q at %q", c.name, h.secCh, h.secThread, h.secAt, c.thread, c.ts)
			}
			if nav, th := h.last(); nav != "" || th != "" {
				t.Fatalf("%s: navigated to %q / opened the thread pane %q instead", c.name, nav, th)
			}
			if got := selected(); got != w42Key(c.ts) {
				t.Errorf("%s: selected row %q, want %q", c.name, got, w42Key(c.ts))
			}
			if ch, ts := l.Selected(); ch != fakeslack.ViewsChannel || ts != c.ts {
				t.Errorf("%s: Selected %q/%q", c.name, ch, ts)
			}
			if c.thread == "" {
				vaShot(t, e, "todos-beside-selected", dark)
			}
		}
		// the column closed: no row selected
		h.secCh, h.secThread, h.secAt = "", "", ""
		e.frame()
		if got := selected(); got != "" {
			t.Errorf("with the column closed %q is still selected", got)
		}
		kbClickRow(t, e, l.kb, w42Key(w42GoneTS), listY)
		if actLastModal(h.vaHost) == nil {
			t.Fatal("a deleted message's click opened no LaterRemoveModal")
		}
		if h.secCh != "" || len(h.navs) != 0 || len(h.threads) != 0 {
			t.Errorf("a deleted message's click also opened %q / navigated %q / threads %q", h.secCh, h.navs, h.threads)
		}
	})
}

// The Saved page's rows (BookmarksPage draws kmjU with
// openInSecondaryView): under 6owI hq (later_todos and the
// has_starred_item pref) a conversation opens beside the page and the row
// is selected; without it the conversation replaces the page (L1Ms keeps
// a Channel in the Primary), while a thread is always a secondary view
// (+zKh aK). Saved's Enter (eQN2 eT) is jumpToSavedMessage too.
func TestSavedRowClickLikeSlack(t *testing.T) {
	w26Modes(t, func(t *testing.T, dark bool) {
		app := w42Items(t, "saved")
		app.SetExperimentGroup("later_todos", "on")
		w42SavedPage(app, true)
		h := newW26Host(app, dark)
		s := NewSaved(h)
		e := &vaEnv{size: image.Pt(620, 900)}
		e.draw = func(gtx layout.Context) {
			s.Layout(gtx)
			for _, o := range h.open() {
				o.Layout(gtx)
			}
		}
		e.until(t, "the saved items", func() bool { return len(s.feed.items) == 4 })
		e.frame()
		e.frame()
		listY := e.size.Y - s.kb.viewH
		kbClickRow(t, e, s.kb, w42Key(w42TopTS), listY)
		if h.secCh != fakeslack.ViewsChannel || h.secThread != "" || h.secAt != w42TopTS || len(h.navs) != 0 {
			t.Fatalf("hq: the column shows %q/%q at %q, navigated %q", h.secCh, h.secThread, h.secAt, h.navs)
		}
		if ch, ts := s.Selected(); ch != fakeslack.ViewsChannel || ts != w42TopTS {
			t.Errorf("hq: Selected %q/%q", ch, ts)
		}
		vaShot(t, e, "saved-beside-selected", dark)
		kbClickRow(t, e, s.kb, w42Key(w42ReplyTS), listY)
		if h.secThread != w42RootTS || h.secAt != w42ReplyTS {
			t.Fatalf("a saved reply: the column shows thread %q at %q", h.secThread, h.secAt)
		}
		// without the saved page the conversation takes the primary, at
		// the message; a thread still opens beside the page
		w42SavedPage(app, false)
		h.secCh, h.secThread, h.secAt = "", "", ""
		e.frame()
		if ch, _ := s.Selected(); ch != "" {
			t.Errorf("with the column closed %q is still selected", ch)
		}
		kbClickRow(t, e, s.kb, w42Key(w42TopTS), listY)
		if nav, _ := h.last(); nav != "c/"+fakeslack.ViewsChannel+"/m/"+w42TopTS || h.secCh != "" {
			t.Errorf("without hq: went to %q, column %q", nav, h.secCh)
		}
		kbClickRow(t, e, s.kb, w42Key(w42RootTS), listY)
		if h.secThread != w42RootTS || h.secAt != w42RootTS {
			t.Errorf("without hq a thread's root: the column shows thread %q at %q", h.secThread, h.secAt)
		}
		// a deleted message's click opens LaterRemoveModal (Kivd), with
		// the saved item's copy
		n := len(h.navs)
		kbClickRow(t, e, s.kb, w42Key(w42GoneTS), listY)
		if actLastModal(h.vaHost) == nil {
			t.Fatal("a deleted saved message's click opened no LaterRemoveModal")
		}
		if len(h.navs) != n {
			t.Errorf("a deleted saved message's click navigated to %q", h.navs[n:])
		}
	})
}
