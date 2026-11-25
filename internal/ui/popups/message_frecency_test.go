package popups

import (
	"image"
	"testing"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
)

// A row picked in a message menu records its frecencyId in the frecency
// store (MAMenu dispatchMessageAction's recordMessageActionFrecencyEvent),
// which then goes out with users.prefs.set: Connect to apps' order is
// built from it. Opening the menu writes nothing.
func TestMessageMenuPickRecordsFrecency(t *testing.T) {
	h, calls := mkHostCounting(t)
	prev := MessageShortcuts
	ran := 0
	MessageShortcuts = func(Host, core.Message) []MenuItem {
		it := MenuItem{Action: "X5", Label: "Translate", Right: "Synthetic Notes", Do: func() { ran++ }}
		setFrecency(&it, "MAX5")
		return []MenuItem{it}
	}
	t.Cleanup(func() { MessageShortcuts = prev })
	index := func(items []MenuItem, action string) int {
		for i, it := range items {
			if it.Action == action {
				return i
			}
		}
		t.Fatalf("no %s row", action)
		return -1
	}
	menu := NewMessageMenuOpts(h, image.Pt(10, 10), mkMsg(false), MessageMenuOpts{})
	time.Sleep(300 * time.Millisecond)
	if n := calls("users.prefs.set"); n != 0 {
		t.Fatalf("opening the menu wrote the prefs %d times", n)
	}
	if h.a.FrecencyCache().Item("MAX5") != nil {
		t.Fatal("the fixture already has MAX5")
	}
	i := index(menu.items, "connect_to_apps_actions_menu")
	menu.openSub(i, true)
	if menu.sub == nil {
		t.Fatal("Connect to apps opened no submenu")
	}
	menu.sub.activate(layout.Context{}, index(menu.sub.items, "X5"))
	if ran != 1 {
		t.Fatalf("the shortcut ran %d times", ran)
	}
	deadline := time.Now().Add(5 * time.Second)
	for h.a.FrecencyCache().Item("MAX5") == nil || calls("users.prefs.set") == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("after the pick: MAX5 %v, users.prefs.set %d", h.a.FrecencyCache().Item("MAX5"), calls("users.prefs.set"))
		}
		time.Sleep(50 * time.Millisecond)
	}

	// a built-in row records its own id: Mark unread's MAmark_unread
	menu = NewMessageMenuOpts(h, image.Pt(10, 10), mkMsg(false), MessageMenuOpts{})
	menu.activate(layout.Context{}, index(menu.items, "mark_unread"))
	deadline = time.Now().Add(5 * time.Second)
	for h.a.FrecencyCache().Item("MAmark_unread") == nil {
		if time.Now().After(deadline) {
			t.Fatal("Mark unread recorded no MAmark_unread")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
