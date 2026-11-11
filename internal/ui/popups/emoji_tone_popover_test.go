package popups

import (
	"image"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

// getChoices: eG's order with no tone set, else the other five and the
// active one last.
func TestEmojiToneChoices(t *testing.T) {
	if got := epToneChoices(0); got != [6]int{1, 2, 3, 4, 5, 6} {
		t.Errorf("unset: %v", got)
	}
	if got := epToneChoices(3); got != [6]int{1, 2, 4, 5, 6, 3} {
		t.Errorf("tone 3: %v", got)
	}
	if got := epToneChoices(1); got != [6]int{2, 3, 4, 5, 6, 1} {
		t.Errorf("tone 1: %v", got)
	}
}

// epToneFrames drives a picker through an input router.
type epToneFrames struct {
	p   *EmojiPicker
	r   input.Router
	ops op.Ops
}

func (f *epToneFrames) frame() {
	f.ops.Reset()
	gtx := layout.Context{Ops: &f.ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(1440, 900)), Source: f.r.Source(), Now: time.Now()}
	f.p.Layout(gtx)
	f.r.Frame(&f.ops)
}

func (f *epToneFrames) press(n key.Name) {
	f.r.Queue(key.Event{Name: n, State: key.Press}, key.Event{Name: n, State: key.Release})
	f.frame()
	f.frame()
}

// The skin tone popover in both groups of emoji_skin_tone_picker_improvement:
// it opens on the toggle with the active tone focused, the arrows wrap
// through getChoices, Enter picks (the focused tone without the
// experiment, the selected one with it), Escape closes only the popover.
func TestEmojiTonePopover(t *testing.T) {
	h, _ := pfEnv(t)
	if err := h.a.SetPref(t.Context(), "preferred_skin_tone", "3"); err != nil {
		t.Fatal(err)
	}
	for _, group := range []string{"", "on"} {
		t.Run("group="+group, func(t *testing.T) {
			h.a.SetExperimentGroup("emoji_skin_tone_picker_improvement", group)
			p := NewEmojiPicker(h, func(string) {})
			if p.toneImproved() != (group == "on") {
				t.Fatalf("improved %v", p.toneImproved())
			}
			f := &epToneFrames{p: p}
			f.frame()
			f.frame()
			if epActiveTone(h) != 3 {
				t.Fatalf("active tone %d", epActiveTone(h))
			}
			p.toneBtn.Click()
			f.frame()
			f.frame()
			u := &p.toneUI
			if !u.open || u.focused != 3 || u.sel != 3 || u.hovered != 3 {
				t.Fatalf("opened: %+v", *u)
			}
			// Escape closes the popover, not the picker
			f.press(key.NameEscape)
			if u.open || p.Dismissed() {
				t.Fatalf("Escape: open %v picker dismissed %v", u.open, p.Dismissed())
			}
			p.toneBtn.Click()
			f.frame()
			f.frame()
			// choices [1 2 4 5 6 3]: Right from 3 wraps to 1, Left back to 3
			// and on to 6
			f.press(key.NameRightArrow)
			if u.focused != 1 || u.sel != 1 || !u.kbd {
				t.Fatalf("Right: %+v", *u)
			}
			f.press(key.NameLeftArrow)
			f.press(key.NameLeftArrow)
			if u.sel != 6 {
				t.Fatalf("Left twice: %+v", *u)
			}
			f.press(key.NameTab)
			if !u.open {
				t.Fatal("Tab left the popover")
			}
			f.press(key.NameReturn)
			if u.open || p.tone != 6 || epActiveTone(h) != 6 {
				t.Fatalf("Enter: open %v tone %d active %d", u.open, p.tone, epActiveTone(h))
			}
			pfUntil(t, "preferred_skin_tone 6", func() bool { return h.a.Prefs().SkinTone == "6" })
			pfUntil(t, "the write to land", func() bool {
				epToneMu.Lock()
				defer epToneMu.Unlock()
				return epToneSet == 0
			})
			if err := h.a.SetPref(t.Context(), "preferred_skin_tone", "3"); err != nil {
				t.Fatal(err)
			}
		})
	}
}
