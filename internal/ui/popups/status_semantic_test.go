package popups

import (
	"testing"
	"time"
)

// semantic_emoji_matching (D72a CustomStatusModal): typing a status picks
// its emoji by meaning, 250 ms after the last change, until an emoji is
// chosen by hand; with the experiment off (or unassigned) nothing does.
func TestStatusSemanticEmoji(t *testing.T) {
	h, _ := ccEnv(t)
	a := h.Core()

	a.SetExperimentGroup("semantic_emoji_matching", "on")
	s := actNewStatusState(h, false)
	ccFrame(s.modal)
	s.ed.SetText("In a meeting")
	ccUntil(t, s.modal, "the meeting emoji", func() bool { return s.emoji == "date" })
	s.ed.SetText("Out of office until Monday")
	ccUntil(t, s.modal, "no_entry", func() bool { return s.emoji == "no_entry" })
	s.ed.SetText("Nothing in particular")
	ccUntil(t, s.modal, "the emoji cleared", func() bool { return s.emoji == "" })

	// a preset is an emoji chosen by hand: typing leaves it
	s.pickPreset("bus", "Commuting", "Commuting", actDurCustom)
	ccFrame(s.modal)
	s.ed.SetText("lunch")
	for i := 0; i < 20; i++ {
		time.Sleep(20 * time.Millisecond)
		ccFrame(s.modal)
	}
	if s.emoji != "bus" {
		t.Fatalf("a chosen emoji was replaced: %q", s.emoji)
	}
	// Clear resets it
	s.emoji, s.emojiPicked = "", false
	s.ed.SetText("coffee break")
	ccUntil(t, s.modal, "coffee", func() bool { return s.emoji == "coffee" })

	for _, g := range []string{"off", ""} {
		a.SetExperimentGroup("semantic_emoji_matching", g)
		s := actNewStatusState(h, false)
		ccFrame(s.modal)
		s.ed.SetText("In a meeting")
		for i := 0; i < 20; i++ {
			time.Sleep(20 * time.Millisecond)
			ccFrame(s.modal)
		}
		if s.emoji != "" {
			t.Errorf("group %q picked %q", g, s.emoji)
		}
	}
}
