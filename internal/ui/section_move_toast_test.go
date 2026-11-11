package ui

import (
	"slices"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// performBulkChannelMove's toast: Manage conversations' save says
// "Updated conversations in <section>." under manage_conversations_strings
// and "Moved N conversations to <section>." without it; one channel moved
// by the header's Move to… is "Moved #x to <section>."; Undo sends the
// reverse move.
func TestSectionMoveToast(t *testing.T) {
	e, fs := filterEnv(t, nil)
	a := e.a
	c := a.Core()
	const to, from = "L0FAKE02", "L0FAKE01" // Projects, Hack Club (the fake's custom sections)
	src := a.sectionByID(from)
	if src == nil || len(src.All) < 2 || a.sectionByID(to) == nil {
		t.Fatalf("fake sections: %+v", a.model().Sections)
	}
	moved := append([]string(nil), src.All[:2]...)
	in := func(sec, ch string) bool {
		for _, s := range fs.Sections() {
			if s.ID == sec {
				return slices.Contains(s.Channels, ch)
			}
		}
		return false
	}
	manage := func() {
		t.Helper()
		a.openCreateSection("", to)
		m, ok := topOverlay[*popups.CreateSectionModal](a)
		if !ok {
			t.Fatal("no Manage conversations modal")
		}
		for _, id := range moved {
			m.Toggle(id)
		}
		m.Submit()
		if m.Bumped() { // "already organized in other sections": Move Conversations
			m.Submit()
		}
	}

	a.toast = toastState{}
	manage()
	e.until("moved", 10*time.Second, func() bool { return in(to, moved[0]) && in(to, moved[1]) })
	e.until("toast", 5*time.Second, func() bool { return a.toast.text != "" })
	if got, want := a.toast.text, "Updated conversations in Projects."; got != want {
		t.Errorf("manage_conversations_strings on: %q, want %q", got, want)
	}
	if a.toast.undo == nil {
		t.Fatal("no Undo")
	}
	a.undoToast()
	e.until("undone", 10*time.Second, func() bool { return !in(to, moved[0]) && in(from, moved[0]) && in(from, moved[1]) })

	c.SetExperimentGroup("manage_conversations_strings", "off")
	e.settle(300 * time.Millisecond)
	a.toast = toastState{}
	manage()
	e.until("moved again", 10*time.Second, func() bool { return in(to, moved[0]) && in(to, moved[1]) })
	e.until("toast", 5*time.Second, func() bool { return a.toast.text != "" })
	if got, want := a.toast.text, "Moved 2 conversations to Projects."; got != want {
		t.Errorf("manage_conversations_strings off: %q, want %q", got, want)
	}
	c.ClearExperimentGroup("manage_conversations_strings")

	// one channel through the header's Move to…: its own strings
	ch := moved[0]
	e.openChannel(ch)
	e.settle(300 * time.Millisecond)
	dst := a.sectionByID("L0FAKE03") // Events
	if dst == nil {
		t.Fatal("no Events section")
	}
	a.toast = toastState{}
	a.moveChannelTo(ch, dst, a.channelSections(ch))
	e.until("Move to…", 10*time.Second, func() bool { return in("L0FAKE03", ch) })
	e.until("toast", 5*time.Second, func() bool { return a.toast.text != "" })
	if got, want := a.toast.text, "Moved #"+a.model().conv(ch).Name+" to Events."; got != want {
		t.Errorf("one channel: %q, want %q", got, want)
	}
	var mv core.SectionMove
	if a.bulkMoveText(mv) != "Moved 0 conversations." || (core.SectionMove{}).Count != 0 {
		t.Error("an empty move")
	}
}
