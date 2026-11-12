package popups

import (
	"context"
	"errors"
	"fmt"
	"image"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
)

// The to-do action (Yoyn) and the reminder row (ih0g) by the message's
// saved metadata, from Slack's CDN build d3de3a487b1c: a saved bookmark or
// a completed to-do still reads "Add to-do" and "Add a reminder"; a to-do
// reads "Complete to-do" (check in a menu, check-circle-filled in the
// hover toolbar) and "Edit reminder". Fake Slack only.
func TestTodoAndReminderLabelsLikeSlack(t *testing.T) {
	h := hcEnv(t)
	a := h.Core()
	m := mkMsg(false)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	check := func(stage, todo, menuIcon, hoverIcon, remind string) {
		t.Helper()
		ta := TodoActionFor(a, &m)
		if ta.Label != todo || ta.MenuIcon != menuIcon || ta.HoverIcon != hoverIcon || ta.Complete != (todo == "Complete to-do") {
			t.Errorf("%s: to-do action %+v, want %q %s/%s", stage, ta, todo, menuIcon, hoverIcon)
		}
		if got := ReminderLabel(a, &m); got != remind {
			t.Errorf("%s: reminder row %q, want %q", stage, got, remind)
		}
		// the right-click menu's first group ends with the to-do row
		var row MenuItem
		for _, it := range NewMessageContextMenu(h, image.Pt(100, 100), m, InChannel, MessageMenuOpts{}).Items() {
			if it.Separator {
				break
			}
			row = it
		}
		if row.Label != todo || row.Icon != menuIcon {
			t.Errorf("%s: context menu row %q (%s), want %q (%s)", stage, row.Label, row.Icon, todo, menuIcon)
		}
		// the ⋯ menu's reminder row
		found := false
		for _, it := range actMessageItems(h, m, MessageMenuOpts{}) {
			if it.Icon == "remind" {
				found = true
				if it.Label != remind {
					t.Errorf("%s: ⋯ menu reminder row %q, want %q", stage, it.Label, remind)
				}
			}
		}
		if !found {
			t.Errorf("%s: no reminder row in the ⋯ menu", stage)
		}
	}
	check("not saved", "Add to-do", "todo-add", "todo-add", "Add a reminder")
	if err := a.Save(ctx, m.Channel, m.TS, 0); err != nil {
		t.Fatal(err)
	}
	check("a saved bookmark", "Add to-do", "todo-add", "todo-add", "Add a reminder")
	if err := a.MoveToTodos(ctx, m.Channel, m.TS, 0); err != nil {
		t.Fatal(err)
	}
	check("a to-do", "Complete to-do", "check", "check-circle-filled", "Edit reminder")
	if err := a.CompleteTodo(ctx, m.Channel, m.TS); err != nil {
		t.Fatal(err)
	}
	check("completed", "Add to-do", "todo-add", "todo-add", "Add a reminder")
}

// A in the right-click menu runs Oyqq through core ToggleTodo and toasts
// 43Gs's text: "To-do added", then "To-do marked complete".
func TestTodoActionClickToasts(t *testing.T) {
	h := hcEnv(t)
	a := h.Core()
	m := mkMsg(false)
	for i, want := range []string{"To-do added", "To-do marked complete"} {
		e := newMkEnv(t, NewMessageContextMenu(h, image.Pt(200, 200), m, InChannel, MessageMenuOpts{}))
		e.press("A", 0)
		end := time.Now().Add(10 * time.Second)
		for len(h.toastList()) <= i && time.Now().Before(end) {
			e.frame()
			time.Sleep(10 * time.Millisecond)
		}
		ts := h.toastList()
		if len(ts) <= i || ts[i] != want {
			t.Fatalf("toasts %q, want %q at %d", ts, want, i)
		}
	}
	if meta, ok := a.SavedMeta(m.Channel, m.TS); !ok || !meta.Completed() {
		t.Errorf("after both clicks: %+v %v, want a completed to-do", meta, ok)
	}
}

func TestTodoToastText(t *testing.T) {
	fail := errors.New("network down")
	code := func(c string) error { return fmt.Errorf("saved.update: %w", &slack.Error{Code: c}) }
	for _, c := range []struct {
		act  core.TodoAction
		err  error
		want string
	}{
		{core.TodoAdded, nil, "To-do added"},
		{core.TodoCompleted, nil, "To-do marked complete"},
		{core.TodoMovedToDos, nil, "Moved to to-dos"},
		{core.TodoAdded, fail, "Couldn’t add to your to-dos. Try again?"},
		{core.TodoCompleted, fail, "Something went wrong. Please try again."},
		{core.TodoCompleted, code("message_not_found"), "Something went wrong. Please try again."},
		{core.TodoMovedToDos, fail, "Couldn’t move to your to-dos. Try again?"},
		{core.TodoAdded, code("permission_denied"), "You don’t have permission to do that."},
		{core.TodoAdded, code("message_not_found"), "Couldn’t add to your to-dos. The item couldn’t be found."},
		{core.TodoMovedToDos, code("channel_not_found"), "Couldn’t move to your to-dos. The item couldn’t be found."},
		{core.TodoAdded, code("already_saved"), "This is already in your to-dos."},
	} {
		if got := TodoToast(c.act, c.err); got != c.want {
			t.Errorf("TodoToast(%v, %v) = %q, want %q", c.act, c.err, got, c.want)
		}
	}
}

// The ⋯ menu's save row (oMi4) by the saved metadata, and its clicks'
// toasts (aSBj): Add to saved → "Added to Saved", then Remove from Saved
// → "Saved item cleared"; a to-do reads "Move to Saved" → "To-do moved to
// Saved"; a completed item "Remove from Saved", or with the saved page
// (has_starred_item) "Move to Saved".
func TestSaveRowLikeSlack(t *testing.T) {
	h := hcEnv(t)
	a := h.Core()
	m := mkMsg(false)
	row := func() MenuItem {
		for _, it := range actMessageItems(h, m, MessageMenuOpts{}) {
			if len(it.Keys) == 1 && it.Keys[0] == KeySaveTodos[0] {
				return it
			}
		}
		t.Fatal("no save row")
		return MenuItem{}
	}
	click := func(stage, want string) {
		t.Helper()
		n := len(h.toastList())
		e := newMkEnv(t, NewMessageMenu(h, image.Pt(200, 200), m))
		e.frame()
		row().Do()
		end := time.Now().Add(10 * time.Second)
		for len(h.toastList()) <= n && time.Now().Before(end) {
			e.frame()
			time.Sleep(10 * time.Millisecond)
		}
		if ts := h.toastList(); len(ts) <= n || ts[n] != want {
			t.Fatalf("%s: toasts %q, want %q", stage, ts, want)
		}
	}
	expect := func(stage, label, icon string) {
		t.Helper()
		if r := row(); r.Label != label || r.Icon != icon {
			t.Errorf("%s: save row %q (%s), want %q (%s)", stage, r.Label, r.Icon, label, icon)
		}
	}
	expect("not saved", "Add to saved", "bookmark")
	click("add", "Added to Saved")
	expect("a bookmark", "Remove from Saved", "bookmark-filled")
	click("remove", "Saved item cleared")
	if _, ok := a.SavedMeta(m.Channel, m.TS); ok {
		t.Error("still saved after Remove from Saved")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := a.AddTodo(ctx, m.Channel, m.TS, 0); err != nil {
		t.Fatal(err)
	}
	expect("a to-do", "Move to Saved", "bookmark")
	click("move", "To-do moved to Saved")
	if meta, _ := a.SavedMeta(m.Channel, m.TS); meta.TodoState != "saved" {
		t.Errorf("after Move to Saved: %+v", meta)
	}
	if err := a.MoveToTodos(ctx, m.Channel, m.TS, 0); err != nil {
		t.Fatal(err)
	}
	if err := a.CompleteTodo(ctx, m.Channel, m.TS); err != nil {
		t.Fatal(err)
	}
	expect("completed", "Remove from Saved", "bookmark-filled")
	a.S.Lock()
	a.S.Prefs.HasStarredItem = true
	a.S.Unlock()
	expect("completed, saved page", "Move to Saved", "bookmark")
}
