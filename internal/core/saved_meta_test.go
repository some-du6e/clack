package core

import (
	"context"
	"strconv"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// Each saved message's metadata (saved_meta.go) against Slack's CDN build
// d3de3a487b1c: p6HR's isSaved / isTodo / isSavedAndCompleted, qb0V's due
// date helpers, and Oyqq runAddToLaterTodos's four branches with the
// saved.update / saved.add each sends. Synthetic only.

func TestSavedMetaHelpersLikeSlack(t *testing.T) {
	const now = 1_800_000_000
	cases := []struct {
		name                string
		m                   SavedMeta
		todo, done, overdue bool
		real                int64
		shows               bool
	}{
		{"bookmark", SavedMeta{TodoState: "saved", DateCreated: now - 50}, false, false, false, 0, false},
		// Add to-do: date_due is date_created (bs), so no reminder
		{"to-do, no reminder", SavedMeta{TodoState: "to_do", DateCreated: now - 50, DateDue: now - 50}, true, false, false, 0, false},
		{"to-do due later", SavedMeta{TodoState: "to_do", DateCreated: now - 50, DateDue: now + 3600}, true, false, false, now + 3600, true},
		{"to-do overdue", SavedMeta{TodoState: "to_do", DateCreated: now - 9000, DateDue: now - 60}, true, false, true, now - 60, true},
		{"due exactly now", SavedMeta{TodoState: "to_do", DateCreated: now - 9000, DateDue: now}, true, false, true, now, true},
		{"completed", SavedMeta{TodoState: "completed", DateCreated: now - 50, DateCompleted: now - 10, DateDue: now + 60}, false, true, false, now + 60, false},
		{"archived", SavedMeta{TodoState: "to_do", DateCreated: now - 50, DateDue: now + 60, Archived: true}, true, false, false, now + 60, false},
		{"date_due 1", SavedMeta{TodoState: "to_do", DateCreated: now - 50, DateDue: 1}, true, false, false, 0, true},
	}
	for _, c := range cases {
		m := c.m
		if m.IsTodo() != c.todo || m.Completed() != c.done || m.Overdue(now) != c.overdue || m.RealDueDate() != c.real || m.ShowsDue() != c.shows {
			t.Errorf("%s: todo %v done %v overdue %v real %d shows %v, want %v %v %v %d %v", c.name,
				m.IsTodo(), m.Completed(), m.Overdue(now), m.RealDueDate(), m.ShowsDue(), c.todo, c.done, c.overdue, c.real, c.shows)
		}
	}
}

func TestSavedMetaFromListAndEvents(t *testing.T) {
	a, f := testApp(t)
	a.S.SetLatest("C1", []slack.Message{{TS: "1.000001", User: "U1"}, {TS: "2.000001", User: "U1"}}, false)
	// loadSaved reads both filters; each answers the same page here
	f.reply["saved.list"] = `{"ok":true,"saved_items":[` +
		`{"item_id":"C1","item_type":"message","ts":"1.000001","todo_state":"to_do","date_created":100,"date_due":5000},` +
		`{"item_id":"C1","item_type":"message","ts":"2.000001","todo_state":"saved","date_created":200,"is_archived":true}]}`
	if err := a.loadSaved(context.Background()); err != nil {
		t.Fatal(err)
	}
	if m, ok := a.SavedMeta("C1", "1.000001"); !ok || m != (SavedMeta{TodoState: "to_do", DateCreated: 100, DateDue: 5000}) {
		t.Errorf("to-do: %+v %v", m, ok)
	}
	if m, ok := a.SavedMeta("C1", "2.000001"); !ok || !m.Archived || m.TodoState != "saved" {
		t.Errorf("archived bookmark: %+v %v", m, ok)
	}
	if _, ok := a.SavedMeta("C1", "3.000001"); ok {
		t.Error("an unsaved message has metadata")
	}
	f.reply["messages.list"] = `{"ok":true,"messages":{}}`
	a.handle(rtm("saved_updated", map[string]any{"saved": map[string]any{"item_id": "C1", "item_type": "message", "ts": "1.000001",
		"todo_state": "completed", "date_created": 100, "date_completed": 6000}}))
	if m, _ := a.SavedMeta("C1", "1.000001"); !m.Completed() || m.TodoState != "completed" || m.DateDue != 0 {
		t.Errorf("after saved_updated: %+v", m)
	}
	a.handle(rtm("saved_deleted", map[string]any{"saved": map[string]any{"item_id": "C1", "item_type": "message", "ts": "1.000001"}}))
	if _, ok := a.SavedMeta("C1", "1.000001"); ok {
		t.Error("saved_deleted kept the metadata")
	}
}

// Oyqq runAddToLaterTodos, one branch each.
func TestToggleTodoLikeSlack(t *testing.T) {
	a, f := testApp(t)
	a.S.SetLatest("C1", []slack.Message{{TS: "1.000001", User: "U1"}}, false)
	ctx := context.Background()
	f.reply["saved.add"] = `{"ok":true}`
	f.reply["saved.update"] = `{"ok":true}`

	// nothing saved: VJ addTodoMessage, saved.add {todo_state to_do, date_due now}
	act, err := a.ToggleTodo(ctx, "C1", "1.000001")
	p := f.last("saved.add")
	if err != nil || act != TodoAdded || p.Get("todo_state") != "to_do" || p.Get("item_type") != "message" || p.Get("item_id") != "C1" || p.Get("ts") != "1.000001" {
		t.Fatalf("add: %v %v %v", act, err, p)
	}
	m, ok := a.SavedMeta("C1", "1.000001")
	if !ok || !m.IsTodo() || m.DateDue != m.DateCreated || strconv.FormatInt(m.DateDue, 10) != p.Get("date_due") {
		t.Errorf("after add: %+v (date_due %s)", m, p.Get("date_due"))
	}
	created := m.DateCreated

	// a to-do: e_ {mark completed}: date_due 0, todo_state completed
	act, err = a.ToggleTodo(ctx, "C1", "1.000001")
	p = f.last("saved.update")
	if err != nil || act != TodoCompleted || p.Get("mark") != "completed" || p.Get("todo_state") != "completed" || p.Get("date_due") != "0" {
		t.Fatalf("complete: %v %v %v", act, err, p)
	}
	if m, _ := a.SavedMeta("C1", "1.000001"); !m.Completed() || m.IsTodo() {
		t.Errorf("after complete: %+v", m)
	}

	// completed: e_ {mark uncompleted}: todo_state to_do, date_due its date_created
	act, err = a.ToggleTodo(ctx, "C1", "1.000001")
	p = f.last("saved.update")
	if err != nil || act != TodoAdded || p.Get("mark") != "uncompleted" || p.Get("todo_state") != "to_do" || p.Get("date_due") != strconv.FormatInt(created, 10) {
		t.Fatalf("uncomplete: %v %v %v", act, err, p)
	}
	if m, _ := a.SavedMeta("C1", "1.000001"); m.Completed() || !m.IsTodo() || m.DateDue != created {
		t.Errorf("after uncomplete: %+v", m)
	}

	// an archived bookmark: tOLz runMoveTodoState to to_do, mark unarchived
	a.setSavedMeta("C1", "1.000001", SavedMeta{TodoState: "saved", DateCreated: 300, Archived: true})
	act, err = a.ToggleTodo(ctx, "C1", "1.000001")
	p = f.last("saved.update")
	if err != nil || act != TodoMovedToDos || p.Get("todo_state") != "to_do" || p.Get("date_due") != "300" || p.Get("mark") != "unarchived" {
		t.Fatalf("move: %v %v %v", act, err, p)
	}
	if m, _ := a.SavedMeta("C1", "1.000001"); !m.IsTodo() || m.Archived || m.DateDue != 300 {
		t.Errorf("after move: %+v", m)
	}

	// a failed write puts the metadata back
	f.reply["saved.update"] = `{"ok":false,"error":"internal_error"}`
	before, _ := a.SavedMeta("C1", "1.000001")
	if _, err := a.ToggleTodo(ctx, "C1", "1.000001"); err == nil {
		t.Fatal("no error")
	}
	if m, _ := a.SavedMeta("C1", "1.000001"); m != before {
		t.Errorf("after a failed complete: %+v, want %+v", m, before)
	}
	f.reply["saved.add"] = `{"ok":false,"error":"internal_error"}`
	a.S.SetLatest("C1", []slack.Message{{TS: "1.000001", User: "U1"}, {TS: "4.000001", User: "U1"}}, false)
	if _, err := a.ToggleTodo(ctx, "C1", "4.000001"); err == nil {
		t.Fatal("no error")
	}
	if _, ok := a.SavedMeta("C1", "4.000001"); ok {
		t.Error("a failed add left the message saved")
	}
}

// Save, Unsave, CompleteTodo and MoveToSaved keep the metadata too.
func TestSavedMetaFollowsWrites(t *testing.T) {
	a, f := testApp(t)
	a.S.SetLatest("C1", []slack.Message{{TS: "1.000001", User: "U1"}}, false)
	ctx := context.Background()
	for _, m := range []string{"saved.add", "saved.update", "saved.delete"} {
		f.reply[m] = `{"ok":true}`
	}
	if err := a.Save(ctx, "C1", "1.000001", 0); err != nil {
		t.Fatal(err)
	}
	if m, ok := a.SavedMeta("C1", "1.000001"); !ok || m.TodoState != "saved" || m.DateDue != 0 {
		t.Errorf("Save: %+v %v", m, ok)
	}
	if err := a.MoveToTodos(ctx, "C1", "1.000001", 9000); err != nil {
		t.Fatal(err)
	}
	if m, _ := a.SavedMeta("C1", "1.000001"); !m.IsTodo() || m.DateDue != 9000 {
		t.Errorf("MoveToTodos: %+v", m)
	}
	if err := a.CompleteTodo(ctx, "C1", "1.000001"); err != nil {
		t.Fatal(err)
	}
	if m, _ := a.SavedMeta("C1", "1.000001"); !m.Completed() || m.DateDue != 0 {
		t.Errorf("CompleteTodo: %+v", m)
	}
	if err := a.MoveToSaved(ctx, "C1", "1.000001"); err != nil {
		t.Fatal(err)
	}
	if m, _ := a.SavedMeta("C1", "1.000001"); m.TodoState != "saved" || m.Completed() {
		t.Errorf("MoveToSaved: %+v", m)
	}
	if err := a.Unsave(ctx, "C1", "1.000001"); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.SavedMeta("C1", "1.000001"); ok {
		t.Error("Unsave kept the metadata")
	}
}
