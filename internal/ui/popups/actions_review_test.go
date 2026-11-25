package popups

// parity-actions-fix's review blockers, each against Slack's code (build
// 83aff18a1847b8e9): the save action's slot with later_todos off, an
// admin's Delete on their own system message, and Organize ▸ Un-pin on a
// pinned huddle message under the system-message redesign (with P, which
// binds by canPin rather than by the row). Synthetic data on fakeslack.

import (
	"image"
	"testing"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

func arGroup(items []MenuItem) []MenuItem {
	var g []MenuItem
	for _, it := range items {
		if it.Separator {
			break
		}
		g = append(g, it)
	}
	return g
}

func arFind(items []MenuItem, action string) (MenuItem, bool) {
	for _, it := range items {
		if !it.Separator && it.Action == action {
			return it, true
		}
	}
	return MenuItem{}, false
}

// With later_todos off, Slack's to-do slot is aHFc's later: "Save for
// later" (key A) in the toolbar and the right-click menu's first group,
// and the ⋮ menu has no save row (eY). The branch had dropped the save
// row and kept Add to-do in the slot, so nothing saved.
func TestSaveSlotWithLaterTodosOffLikeSlack(t *testing.T) {
	h, _, rec := emBooted(t)
	h.a.SetExperimentGroup("later_todos", "off")
	at := image.Pt(100, 100)
	m := core.Message{Channel: emCh, TS: "1700000000.000100", User: fakeslack.ProfileID, Text: "a synthetic message"}
	first := arGroup(NewMessageContextMenu(h, at, m, InChannel, MessageMenuOpts{}).Items())
	later, ok := arFind(first, "later")
	if !ok || later.Label != "Save for later" || len(later.Keys) != 1 || later.Keys[0] != "a" {
		t.Fatalf("the right-click menu's first group has no Save for later on A: %+v (%v)", later, ok)
	}
	if it, ok := arFind(first, "add_to_later_todos"); ok {
		t.Errorf("Add to-do is still in the slot: %+v", it)
	}
	if it, ok := arFind(NewMessageMenuOpts(h, at, m, MessageMenuOpts{}).Items(), "later"); ok {
		t.Errorf("the ⋮ menu has a save row with later_todos off: %+v", it)
	}
	run, ok := MessageKeyAction(h, at, m, InChannel, MessageMenuOpts{}, "a")
	if !ok {
		t.Fatal("A takes no action with later_todos off")
	}
	run(layout.Context{})
	maUntil(t, "Save for later saving", func() bool {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		return len(rec.calls["saved.add"]) > 0
	})
}

// canDeleteMessageHelper ends `isIm || S4 || canDeleteMyMessageHelper`:
// an admin who can act in the channel (JM7a S4) deletes their own system
// message, which the helper's own rule refuses; under the redesign n+vA
// keeps Delete on an automated message for the channel's manager or such
// an admin.
func TestAdminDeletesOwnSystemMessageLikeSlack(t *testing.T) {
	h, _, _ := emBooted(t)
	self := fakeslack.SelfID
	m := core.Message{Channel: emCh, TS: "1700000000.000300", User: self, Subtype: "channel_join", Text: "joined"}
	setAdmin := func(on bool) {
		u, _ := h.a.S.User(self)
		cp := slack.User{ID: self, Name: "self"}
		if u != nil {
			cp = *u
		}
		cp.IsAdmin = on
		h.a.S.PutUsers([]slack.User{cp})
	}
	for _, redesign := range []string{"off", "on"} {
		h.a.SetExperimentGroup("system_messages_redesign_m1", redesign)
		setAdmin(false)
		if DeleteShown(h.a, &m, self) {
			t.Errorf("redesign %s: a member deletes their own channel_join", redesign)
		}
		setAdmin(true)
		if !DeleteShown(h.a, &m, self) {
			t.Errorf("redesign %s: an admin can't delete their own channel_join (S4)", redesign)
		}
	}
	h.a.ClearExperimentGroup("system_messages_redesign_m1")
}

// Under system_messages_redesign_m1 a pinned huddle message keeps Organize
// ▸ Un-pin (tk; canUnpinMessage), and an automated message, which has no
// Organize there, still pins with P (getActionsForKeyboardShortcuts binds
// pin by canPin).
func TestRedesignPinRowsLikeSlack(t *testing.T) {
	h, _, rec := emBooted(t)
	h.a.SetExperimentGroup("system_messages_redesign_m1", "on")
	t.Cleanup(func() { h.a.ClearExperimentGroup("system_messages_redesign_m1") })
	at := image.Pt(100, 100)
	huddle := core.Message{Channel: emCh, TS: "1700000000.000400", User: fakeslack.ProfileID, Subtype: "huddle_thread", Pinned: true}
	org, ok := arFind(NewMessageMenuOpts(h, at, huddle, MessageMenuOpts{}).Items(), "organize_actions_menu")
	if !ok {
		t.Fatal("a pinned huddle message has no Organize under the redesign")
	}
	if pin, ok := arFind(org.Sub, "pin"); !ok || pin.Label != "Un-pin from channel" {
		t.Errorf("Organize ▸ %+v (%v), want Un-pin from channel", pin, ok)
	}

	topic := core.Message{Channel: emCh, TS: "1700000000.000500", User: fakeslack.ProfileID, Subtype: "channel_topic", Text: "set the topic"}
	if it, ok := arFind(NewMessageMenuOpts(h, at, topic, MessageMenuOpts{}).Items(), "organize_actions_menu"); ok {
		t.Errorf("an automated message has Organize under the redesign: %+v", it)
	}
	run, ok := MessageKeyAction(h, at, topic, InChannel, MessageMenuOpts{}, "p")
	if !ok {
		t.Fatal("P pins nothing on an automated message under the redesign")
	}
	run(layout.Context{})
	deadline := time.Now().Add(10 * time.Second)
	for {
		drain()
		rec.mu.Lock()
		n := len(rec.calls["pins.add"])
		rec.mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("P didn't pin (no pins.add)")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
