package popups

import (
	"image"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
)

// Synthetic fixtures only.

// Yes, No and Maybe are calendar.event.rsvp with the event, the answer
// and the message; the answer the card last sent sends nothing again; a
// failed call is the toast; an auth_url the calendar connect modal.
func TestEventRSVP(t *testing.T) {
	eventAnswers.Range(func(k, _ any) bool { eventAnswers.Delete(k); return true }) // -count > 1
	h, _, rec := emBooted(t)
	ev := &richtext.Event{ID: "E1", AppType: "gcal"}
	EventAction(h, "rsvp:accepted", ev, "C0EV", "1.0", image.Rectangle{})
	rec.wait(t, "calendar.event.rsvp", map[string]string{"event_id": "E1", "rsvp": "accepted", "message_ts": "1.0", "channel": "C0EV"}, 10*time.Second)
	EventAction(h, "rsvp:accepted", ev, "C0EV", "1.0", image.Rectangle{})
	EventAction(h, "rsvp:declined", ev, "C0EV", "1.0", image.Rectangle{})
	rec.wait(t, "calendar.event.rsvp", map[string]string{"event_id": "E1", "rsvp": "declined"}, 10*time.Second)
	rec.mu.Lock()
	n := len(rec.calls["calendar.event.rsvp"])
	rec.mu.Unlock()
	if n != 2 {
		t.Errorf("%d calls, want 2 (the same answer twice sends it once)", n)
	}

	EventAction(h, "rsvp:tentative", &richtext.Event{ID: fakeslack.EventRSVPFails}, "C0EV", "2.0", image.Rectangle{})
	maUntil(t, "the toast", func() bool {
		for _, s := range h.toastList() {
			if s == "Something’s gone wrong. Mind trying again in a moment?" {
				return true
			}
		}
		return false
	})

	EventAction(h, "rsvp:accepted", &richtext.Event{ID: fakeslack.EventNeedsAuth, AppType: "ocal"}, "C0EV", "3.0", image.Rectangle{})
	maUntil(t, "Connect calendar", func() bool { m := h.topModal(); return m != nil && m.o.Title == "Connect calendar" })
	m := h.topModal()
	if len(m.o.Buttons) != 2 || m.o.Buttons[0].Label != "Close" || m.o.Buttons[1].Label != "Connect a Calendar" || m.o.Buttons[1].Kind != BtnPrimary {
		t.Errorf("buttons %+v", m.o.Buttons)
	}
	if got := calendarConnectText("ocal"); got != "Looks like Outlook Calendar is installed but you don’t have any connected calendars. Connect a calendar to start creating events!" {
		t.Errorf("sentence %q", got)
	}
	if got := calendarConnectText("ocal_gcc"); got != "Looks like Outlook Calendar GCC High is installed but you don’t have any connected calendars. Connect a calendar to start creating events!" {
		t.Errorf("GCC High sentence %q", got)
	}
}

// Copy to Your Calendar is calendar.event.copy; its auth_url opens the
// connect modal, a plain ok nothing.
func TestEventCopy(t *testing.T) {
	h, _, rec := emBooted(t)
	shown := h.shownCount()
	EventAction(h, "copy", &richtext.Event{ID: "E1", AppType: "gcal"}, "C0EV", "1.0", image.Rectangle{})
	rec.wait(t, "calendar.event.copy", map[string]string{"event_id": "E1", "message_ts": "1.0", "channel": "C0EV"}, 10*time.Second)
	EventAction(h, "copy", &richtext.Event{ID: fakeslack.EventNeedsAuth, AppType: "gcal"}, "C0EV", "2.0", image.Rectangle{})
	maUntil(t, "Connect calendar", func() bool { m := h.topModal(); return m != nil && m.o.Title == "Connect calendar" })
	if n := h.shownCount(); n != shown+1 {
		t.Errorf("%d overlays shown, want one (the modal)", n-shown)
	}
}

// More actions' menu: one item, View on {calApp} calendar.
func TestEventMenu(t *testing.T) {
	h, _, _ := emBooted(t)
	mn := NewEventMenu(h, &richtext.Event{ID: "E1", AppType: "gcal", WebLink: "https://cal.test/e"}, image.Rect(10, 10, 42, 42))
	if len(mn.items) != 1 || mn.items[0].Label != "View on Google calendar" {
		t.Errorf("items %+v", mn.items)
	}
}
