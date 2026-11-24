package popups

import (
	"context"
	"image"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/richview"
)

// Synthetic fixtures only.

// Yes, No and Maybe are calendar.event.rsvp with the event, the answer
// and the message; the answer the card last sent sends nothing again; a
// failed call is the toast; an auth_url the calendar connect modal.
func TestEventRSVP(t *testing.T) {
	eventAnswers.Range(func(k, _ any) bool { eventAnswers.Delete(k); return true }) // -count > 1
	h, _, rec := emBooted(t)
	ev := &richtext.Event{ID: "E1", AppType: "gcal"}
	key := richview.EventKey("C0EV", "1.0", "E1")
	EventAction(h, "rsvp:accepted", ev, "C0EV", "1.0", image.Rectangle{})
	rec.wait(t, "calendar.event.rsvp", map[string]string{"event_id": "E1", "rsvp": "accepted", "message_ts": "1.0", "channel": "C0EV"}, 10*time.Second)
	maUntil(t, "Yes settled", idleWrite(key))
	EventAction(h, "rsvp:accepted", ev, "C0EV", "1.0", image.Rectangle{}) // the answer it last sent: nothing
	EventAction(h, "rsvp:declined", ev, "C0EV", "1.0", image.Rectangle{})
	rec.wait(t, "calendar.event.rsvp", map[string]string{"event_id": "E1", "rsvp": "declined"}, 10*time.Second)
	maUntil(t, "No settled", idleWrite(key))
	if n := callCount(rec, "calendar.event.rsvp"); n != 2 {
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
	// a failed answer can be sent again
	maUntil(t, "the failed answer settled", idleWrite(richview.EventKey("C0EV", "2.0", fakeslack.EventRSVPFails)))
	EventAction(h, "rsvp:tentative", &richtext.Event{ID: fakeslack.EventRSVPFails}, "C0EV", "2.0", image.Rectangle{})
	maUntil(t, "the answer sent again", func() bool {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		n := 0
		for _, f := range rec.calls["calendar.event.rsvp"] {
			if f.Get("event_id") == fakeslack.EventRSVPFails {
				n++
			}
		}
		return n == 2
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
	EventAction(h, "copy", &richtext.Event{ID: "E1", AppType: "gcal"}, "C0EV", "1.0", image.Rectangle{})
	rec.wait(t, "calendar.event.copy", map[string]string{"event_id": "E1", "message_ts": "1.0", "channel": "C0EV"}, 10*time.Second)
	EventAction(h, "copy", &richtext.Event{ID: fakeslack.EventNeedsAuth, AppType: "gcal"}, "C0EV", "2.0", image.Rectangle{})
	maUntil(t, "Connect calendar", func() bool { m := h.topModal(); return m != nil && m.o.Title == "Connect calendar" })
	h.mu.Lock()
	n := 0
	for _, o := range h.shown {
		if m, ok := o.(*Modal); ok && m.o.Title == "Connect calendar" {
			n++
		}
	}
	h.mu.Unlock()
	if n != 1 {
		t.Errorf("%d Connect calendar modals, want 1 (E1's plain ok opens none)", n)
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

// idleWrite reports, for maUntil, that key's write settled.
func idleWrite(key string) func() bool {
	return func() bool {
		_, busy := richview.Busy(key)
		return !busy
	}
}

func callCount(rec *emCalls, method string) int {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return len(rec.calls[method])
}

// evBooted is a booted host whose fake holds each calendar call until the
// channel hold names (when set) is closed. Every call is recorded.
func evBooted(t *testing.T, hold *atomic.Pointer[chan struct{}]) (*tzHost, *emCalls) {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	rec := &emCalls{calls: map[string][]url.Values{}}
	fs.SetHook(func(method string, form url.Values) (any, bool) {
		rec.mu.Lock()
		rec.calls[method] = append(rec.calls[method], form)
		rec.mu.Unlock()
		if strings.HasPrefix(method, "calendar.event.") {
			if c := hold.Load(); c != nil {
				<-*c
			}
		}
		return nil, false
	})
	h := hcEnvOn(t, fs)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- h.a.Boot(ctx) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("boot: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("boot timed out")
	}
	return h, rec
}

// A card's write holds its buttons while its call is out, as Slack's do
// (BaseEventCopyToCalendar's loading Button, BaseEventRSVP's disabled
// options): a double click on Copy to Your Calendar sends one copy, and No
// clicked while Yes is out sends nothing until Yes settles.
func TestEventWriteGuard(t *testing.T) {
	eventAnswers.Range(func(k, _ any) bool { eventAnswers.Delete(k); return true })
	var hold atomic.Pointer[chan struct{}]
	c1 := make(chan struct{})
	hold.Store(&c1)
	h, rec := evBooted(t, &hold)
	ev := &richtext.Event{ID: "E9", AppType: "gcal"}
	key := richview.EventKey("C0EV", "9.0", "E9")
	EventAction(h, "copy", ev, "C0EV", "9.0", image.Rectangle{})
	maUntil(t, "the first copy", func() bool { return callCount(rec, "calendar.event.copy") == 1 })
	if act, busy := richview.Busy(key); !busy || act != "copy" {
		t.Errorf("busy %v act %q while the copy is out, want copy", busy, act)
	}
	EventAction(h, "copy", ev, "C0EV", "9.0", image.Rectangle{}) // the double click's second
	close(c1)
	maUntil(t, "the copy settled", idleWrite(key))
	if n := callCount(rec, "calendar.event.copy"); n != 1 {
		t.Fatalf("%d calendar.event.copy calls, want 1", n)
	}

	c2 := make(chan struct{})
	hold.Store(&c2)
	ev2 := &richtext.Event{ID: "E10", AppType: "gcal"}
	key2 := richview.EventKey("C0EV", "10.0", "E10")
	EventAction(h, "rsvp:accepted", ev2, "C0EV", "10.0", image.Rectangle{})
	maUntil(t, "Yes", func() bool { return callCount(rec, "calendar.event.rsvp") == 1 })
	EventAction(h, "rsvp:declined", ev2, "C0EV", "10.0", image.Rectangle{}) // while Yes is out
	close(c2)
	maUntil(t, "Yes settled", idleWrite(key2))
	if n := callCount(rec, "calendar.event.rsvp"); n != 1 {
		t.Fatalf("%d calendar.event.rsvp calls, want 1 (No while Yes was out sends nothing)", n)
	}
	hold.Store(nil)
	EventAction(h, "rsvp:declined", ev2, "C0EV", "10.0", image.Rectangle{})
	maUntil(t, "No, once Yes settled", func() bool { return callCount(rec, "calendar.event.rsvp") == 2 })
}

// A card drawn without its message (no channel or ts: a surface that
// didn't pass them) sends nothing: RSVP, Copy and Share event act on it,
// Share event reading the message (conversations.history) for its dialog.
func TestEventIdentity(t *testing.T) {
	h, _, rec := emBooted(t)
	ev := &richtext.Event{ID: "E11", AppType: "gcal"}
	shown := h.overlaysShown()
	for _, id := range [][2]string{{"", "11.0"}, {"C0EV", ""}} {
		for _, act := range []string{"rsvp:accepted", "copy", "share"} {
			EventAction(h, act, ev, id[0], id[1], image.Rectangle{})
		}
	}
	EventAction(h, "copy", ev, "C0EV", "11.0", image.Rectangle{}) // then a whole one
	rec.wait(t, "calendar.event.copy", map[string]string{"channel": "C0EV", "message_ts": "11.0"}, 10*time.Second)
	maUntil(t, "the actions' calls settled", asyncIdle)
	if n, m := callCount(rec, "calendar.event.rsvp"), callCount(rec, "calendar.event.copy"); n != 0 || m != 1 {
		t.Errorf("%d rsvp and %d copy calls, want 0 and 1 (none without the message)", n, m)
	}
	rec.mu.Lock()
	for _, f := range rec.calls["conversations.history"] {
		if f.Get("channel") == "C0EV" || f.Get("latest") == "11.0" {
			t.Errorf("Share event without the message read history (channel %q, latest %q)", f.Get("channel"), f.Get("latest"))
		}
	}
	rec.mu.Unlock()
	if h.overlaysShown() != shown {
		t.Errorf("Share event without the message opened something")
	}
}

// asyncIdle reports no popups work in flight (run's) and none queued.
func asyncIdle() bool {
	asyncMu.Lock()
	defer asyncMu.Unlock()
	return inflight == 0 && len(asyncQueue) == 0
}

// overlaysShown counts the overlays shown but run's async pump, which the
// first call in flight shows (invisible).
func (h *tzHost) overlaysShown() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, o := range h.shown {
		if _, ok := o.(*pump); !ok {
			n++
		}
	}
	return n
}
