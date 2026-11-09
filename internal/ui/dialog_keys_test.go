package ui

import (
	"os"
	"strings"
	"testing"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
)

// press sends a key and runs the frames that handle it (a FocusCmd defers
// the rest of its frame's events).
func (e *perfEnv) press(name key.Name, mods key.Modifiers) {
	e.key(name, mods)
	for range 4 {
		e.frame()
	}
}

func dkChannel(t *testing.T, a *App) string {
	t.Helper()
	s := sectionOfType(a, "channels")
	if s == nil || len(s.Rows) == 0 {
		t.Fatal("no channel in the sidebar")
	}
	return s.Rows[0]
}

// openEditTopic opens channel details over ch and its "Edit topic" dialog.
func openEditTopic(t *testing.T, e *perfEnv, ch string) detailsOverlay {
	t.Helper()
	a := e.a
	a.OpenDetails(ch)
	var o detailsOverlay
	e.until("channel details", 10*time.Second, func() bool {
		var ok bool
		o, ok = topOverlay[detailsOverlay](a)
		if !ok {
			return false
		}
		o.d.EditTopic()
		return o.d.Editing()
	})
	e.settle(400 * time.Millisecond)
	return o
}

// "Edit topic" (ChannelTopicDialog over channel details): the input has
// the focus; Tab cycles close, input, Cancel, Save and wraps; Enter in
// the input adds a line instead of saving (Texty's onEnter returns true);
// Escape closes this dialog only, a second Escape the details.
func TestEditTopicKeys(t *testing.T) {
	e, _ := sectionEnv(t)
	a := e.a
	ch := dkChannel(t, a)
	a.Navigate("c/" + ch)
	e.settle(300 * time.Millisecond)
	o := openEditTopic(t, e, ch)
	focus := func(want string, ring bool) {
		t.Helper()
		got, r := o.d.EditFocus(layout.Context{Source: e.r.Source()})
		if got != want || r != ring {
			t.Fatalf("focus %q (ring %v), want %q (ring %v)", got, r, want, ring)
		}
	}
	focus("input", false)
	for _, w := range []string{"cancel", "save", "close", "input"} {
		e.press(key.NameTab, 0)
		focus(w, true)
	}
	e.press(key.NameTab, key.ModShift)
	focus("close", true)
	e.press(key.NameTab, 0)
	focus("input", true)

	o.d.SetEditText("synthetic topic")
	e.press(key.NameEnd, 0)
	e.press(key.NameReturn, 0)
	if !o.d.Editing() {
		t.Fatal("Enter in the topic input saved and closed the dialog")
	}
	if got := o.d.EditText(); !strings.Contains(got, "\n") {
		t.Fatalf("Enter didn't add a line: %q", got)
	}

	e.press(key.NameEscape, 0)
	if o.d.Editing() {
		t.Fatal("Escape left the edit dialog open")
	}
	if d, ok := topOverlay[detailsOverlay](a); !ok || d.d != o.d || o.d.Closed() {
		t.Fatal("Escape in the edit dialog closed channel details too")
	}
	e.press(key.NameEscape, 0)
	if !o.d.Closed() {
		t.Fatal("a second Escape didn't close channel details")
	}
}

// With DIALOG_KEYS_SHOTS_DIR set (the dialog-keys workflow, under xvfb),
// the dialogs are saved with Slack's focus ring where the keyboard put
// it, light and dark: Delete message as it opens (Delete focused), after
// Tab (×) and Tab again (Cancel, an outline button losing its border),
// Leave channel, Delete this section, and Edit topic with Save reached by
// Tab.
func TestDialogKeysShots(t *testing.T) {
	dir := os.Getenv("DIALOG_KEYS_SHOTS_DIR")
	if dir == "" {
		t.Skip("DIALOG_KEYS_SHOTS_DIR not set")
	}
	t.Setenv("CLACK_BENCH_GPU", "1")
	e, _ := sectionEnv(t)
	a := e.a
	ch := dkChannel(t, a)
	a.Navigate("c/" + ch)
	e.settle(800 * time.Millisecond)
	closeAll := func() {
		for _, o := range a.overlays {
			if d, ok := o.(*popups.Modal); ok {
				d.Dismiss()
			}
		}
		a.overlays = nil
		e.frame()
	}
	own := core.Message{Channel: ch, TS: "1700000000.000100", User: fakeslack.SelfID,
		Text: "a synthetic message of mine, long enough to wrap over three lines in the preview card of the delete dialog, so that its line pitch, padding and the gap above it can be set against Slack's"}
	for _, dark := range []bool{false, true} {
		a.th.SetDark(dark)
		a.Invalidate()
		e.settle(6 * time.Second) // llvmpipe frames are slow; tokens restyle over several
		mode := "light"
		if dark {
			mode = "dark"
		}
		shot := func(name string) {
			for range 3 {
				e.frame()
			}
			e.shotTo(t, dir, name+"-"+mode)
		}
		closeAll()
		popups.ConfirmDelete(a, own)
		e.settle(500 * time.Millisecond)
		shot("1-delete-message-open")
		e.press(key.NameTab, 0)
		shot("2-delete-message-tab-close")
		e.press(key.NameTab, 0)
		shot("3-delete-message-tab-cancel")
		closeAll()
		popups.Confirm(a, "Leave plans", "When you leave a private channel, you’ll no longer be able to see any of its messages. To rejoin this channel later, you’ll need to be invited.",
			"Leave Channel", true, nil, nil)
		e.settle(300 * time.Millisecond)
		shot("4-leave-channel-open")
		closeAll()
		a.ShowOverlay(popups.NewDeleteSectionModal(a, "Synthetic", func() {}))
		e.settle(300 * time.Millisecond)
		shot("5-delete-section-open")
		closeAll()
		o := openEditTopic(t, e, ch)
		e.press(key.NameTab, 0)
		e.press(key.NameTab, 0)
		shot("6-edit-topic-tab-save")
		o.d.Escape()
		o.d.Escape()
		closeAll()
	}
}
