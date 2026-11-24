package richview

import (
	"image"
	"strconv"
	"strings"
	"testing"
	"time"

	"gioui.org/io/pointer"

	rt "github.com/some-du6e/clack/internal/richtext"
)

// Synthetic fixtures only.

// evHandler records event card actions.
type evHandler struct {
	recHandler
	acts []string
}

func (h *evHandler) EventAction(act string, ev *rt.Event, ch, ts string, _ image.Rectangle, _ bool) {
	h.acts = append(h.acts, act+":"+ev.ID)
}

// evClock pins the card's clock at 2026-11-23 15:00 local.
func evClock(t *testing.T) time.Time {
	now := time.Date(2026, 11, 23, 15, 0, 0, 0, time.Local)
	prev := eventNow
	eventNow = func() time.Time { return now }
	t.Cleanup(func() { eventNow = prev })
	return now
}

func eventDoc(ev rt.Event) rt.Doc {
	return doc(rt.Block{Kind: rt.EventBlock, Kit: true, BlockID: "ev", Event: &ev})
}

// standup is an event from start to end minutes from now, the viewer an
// attendee who accepted.
func standup(now time.Time, start, end time.Duration) rt.Event {
	return rt.Event{ID: "E1", Title: "Standup", Start: now.Add(start).Unix(), End: now.Add(end).Unix(), AttendeeCount: 5,
		Accepted: []rt.EventGuest{{UserID: "USELF"}, {UserID: "U1"}}, Maybe: 1, No: 1, NoReply: 1, RSVP: "accepted", Attendee: true,
		AppType: "gcal", MeetingProvider: "zoom", MeetingURL: "https://zoom.test/j", WebLink: "https://cal.test/e", CopiesOK: true}
}

// oneEvent is the view's one event card.
func oneEvent(t *testing.T, v *View) EventRect {
	t.Helper()
	rs := v.EventRects()
	if len(rs) != 1 {
		t.Fatalf("%d event cards, want 1", len(rs))
	}
	return rs[0]
}

func evWords(v *View) string {
	var b strings.Builder
	for _, f := range textFrags(&v.d) {
		b.WriteString(f.sh.str + " ")
	}
	return b.String()
}

// Event's card: 440px at most; the header's title over the date line
// with NOW while it's on; the guests line; Join Zoom Meeting for a guest
// while it's on; Going? with the guest's answer picked.
func TestEventLayout(t *testing.T) {
	now := evClock(t)
	st := newTestStyle()
	v := layoutAt(st, eventDoc(standup(now, -10*time.Minute, 50*time.Minute)), Options{}, 600)
	rs := v.EventRects()
	if len(rs) != 1 || rs[0].Card.Dx() != 440 {
		t.Fatalf("cards %+v: want one 440px card", rs)
	}
	w := evWords(v)
	for _, s := range []string{"Standup", "NOW", "Mon", "at", "5", "guests:", "2", "yes,", "maybe,", "no,", "hasn’t", "Join", "Zoom", "Going?", "Yes", "No", "Maybe"} {
		if !strings.Contains(" "+w, " "+s+" ") {
			t.Errorf("no %q in %q", s, w)
		}
	}
	if _, ok := rs[0].Buttons["copy"]; ok {
		t.Errorf("a guest's card has Copy to Your Calendar")
	}
	yes, no := rs[0].Buttons["rsvp:accepted"], rs[0].Buttons["rsvp:declined"]
	maybe := rs[0].Buttons["rsvp:tentative"]
	if yes.Empty() || no.Empty() || maybe.Empty() || !(yes.Max.X <= no.Min.X && no.Max.X <= maybe.Min.X) || maybe.Max.X != rs[0].Card.Max.X-16 {
		t.Errorf("Yes %v No %v Maybe %v: want them in order, Maybe 16px in from the card's right %d", yes, no, maybe, rs[0].Card.Max.X)
	}
	picked := false
	for _, b := range v.d.boxes {
		if b.r == yes && b.fill == evPicked {
			picked = true
		}
	}
	if !picked {
		t.Errorf("the guest's answer (Yes) isn't picked")
	}
	// ahead: no NOW, "Standup Meeting" not Join
	v = layoutAt(st, eventDoc(standup(now, 24*time.Hour, 25*time.Hour)), Options{}, 600)
	if w := evWords(v); strings.Contains(w, "NOW") || strings.Contains(w, "Join") || !strings.Contains(w, "Zoom Meeting") {
		t.Errorf("ahead: %q, want no NOW and Zoom Meeting", w)
	}
}

// A guest's card past its end: ENDED, the meeting link and no Going?; a
// cancelled one "Event cancelled"; the organizer's: no Going? and no
// Copy; anyone else's Copy to Your Calendar, or the sentence once copied;
// no meeting link: "No meeting link available".
func TestEventRoles(t *testing.T) {
	now := evClock(t)
	st := newTestStyle()
	past := standup(now, -2*time.Hour, -time.Hour)
	if v := layoutAt(st, eventDoc(past), Options{}, 600); !strings.Contains(evWords(v), "ENDED") || strings.Contains(evWords(v), "Going?") {
		t.Errorf("ended: %q", evWords(v))
	}
	cancelled := standup(now, time.Hour, 2*time.Hour)
	cancelled.Cancelled = true
	if v := layoutAt(st, eventDoc(cancelled), Options{}, 600); !strings.Contains(evWords(v), "Event cancelled") && !strings.Contains(evWords(v), "cancelled") ||
		strings.Contains(evWords(v), "Going?") {
		t.Errorf("cancelled: %q", evWords(v))
	}
	org := standup(now, time.Hour, 2*time.Hour)
	org.Attendee, org.RSVP, org.Organizer, org.MeetingURL = false, "", true, ""
	v := layoutAt(st, eventDoc(org), Options{}, 600)
	if w := evWords(v); strings.Contains(w, "Going?") || !strings.Contains(w, "No meeting link available") {
		t.Errorf("organizer: %q", w)
	}
	if b := oneEvent(t, v).Buttons; len(b) != 0 {
		t.Errorf("organizer's buttons %v, want none", b)
	}
	other := standup(now, time.Hour, 2*time.Hour)
	other.Attendee, other.RSVP = false, ""
	v = layoutAt(st, eventDoc(other), Options{}, 600)
	if w := evWords(v); strings.Contains(w, "Going?") || strings.Contains(w, "Zoom") || !strings.Contains(w, "Copy") {
		t.Errorf("not a guest: %q, want Copy to Your Calendar and no join row", w)
	}
	if _, ok := oneEvent(t, v).Buttons["copy"]; !ok {
		t.Errorf("no Copy button")
	}
	other.Copied = true
	v = layoutAt(st, eventDoc(other), Options{}, 600)
	if _, ok := oneEvent(t, v).Buttons["copy"]; ok || !strings.Contains(evWords(v), "primary") {
		t.Errorf("copied: %q, want the sentence and no button", evWords(v))
	}
}

// Yes, No, Maybe and Copy reach the handler with their action; the hover
// bar's Share event and More actions too.
func TestEventClicks(t *testing.T) {
	now := evClock(t)
	h := &evHandler{}
	hs := newHarness(eventDoc(standup(now, time.Hour, 2*time.Hour)), h)
	hs.w = 600
	hs.frame()
	r := oneEvent(t, hs.v)
	hs.clickRect(r.Buttons["rsvp:declined"], 0)
	hs.clickRect(r.Buttons["rsvp:tentative"], time.Second)
	hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: center(findFrag(t, &hs.v.d, "Standup"))})
	r = oneEvent(t, hs.v)
	hs.clickRect(r.Actions["share"], 2*time.Second)
	hs.clickRect(r.Actions["more"], 3*time.Second)
	if got := strings.Join(h.acts, ","); got != "rsvp:declined:E1,rsvp:tentative:E1,share:E1,more:E1" {
		t.Errorf("actions %q", got)
	}
	other := standup(now, time.Hour, 2*time.Hour)
	other.Attendee, other.RSVP = false, ""
	h = &evHandler{}
	hs = newHarness(eventDoc(other), h)
	hs.w = 600
	hs.frame()
	hs.clickRect(oneEvent(t, hs.v).Buttons["copy"], 0)
	if got := strings.Join(h.acts, ","); got != "copy:E1" {
		t.Errorf("actions %q, want [copy:E1]", got)
	}
}

// The menu's label: "View on Google calendar", "View on Outlook
// Calendar GCC High" for GCC High.
func TestEventCalendarLabel(t *testing.T) {
	if got := EventCalendarLabel("gcal"); got != "View on Google calendar" {
		t.Errorf("gcal: %q", got)
	}
	if got := EventCalendarLabel("ocal_gcc"); got != "View on Outlook Calendar GCC High" {
		t.Errorf("ocal_gcc: %q", got)
	}
}

// Where the Copy row would draw from a copies with no includes, Slack's
// card throws: the block's error line instead of the card.
func TestEventCopyErrorLine(t *testing.T) {
	now := evClock(t)
	other := standup(now, time.Hour, 2*time.Hour)
	other.Attendee, other.RSVP, other.CopiesOK = false, "", false
	v := layoutAt(newTestStyle(), eventDoc(other), Options{}, 600)
	if len(v.EventRects()) != 0 || !strings.Contains(evWords(v), "could not be displayed") {
		t.Errorf("%q, want the error line and no card", evWords(v))
	}
	// a guest's card reads no copies: it draws
	if v := layoutAt(newTestStyle(), eventDoc(func() rt.Event { e := standup(now, time.Hour, 2*time.Hour); e.CopiesOK = false; return e }()), Options{}, 600); len(v.EventRects()) != 1 {
		t.Errorf("a guest's card without copies: no card")
	}
}

// An invalid end is moment's invalid date: "Invalid date" for the end
// time, and never past (Going? stays).
func TestEventNoEnd(t *testing.T) {
	now := evClock(t)
	ev := standup(now, -2*time.Hour, -time.Hour)
	ev.NoEnd = true
	v := layoutAt(newTestStyle(), eventDoc(ev), Options{}, 600)
	if w := evWords(v); !strings.Contains(w, "Invalid date") || !strings.Contains(w, "Going?") || strings.Contains(w, "ENDED") {
		t.Errorf("%q, want Invalid date, Going? and no ENDED", w)
	}
}

// The eighth face's overlay: "+N", "9+" past nine (BaseAvatar's
// renderAdditionalOverlay).
func TestEventOverlayCap(t *testing.T) {
	now := evClock(t)
	for _, c := range []struct {
		n    int
		want string
	}{{12, "+4"}, {17, "+9"}, {18, "9+"}, {30, "9+"}} {
		ev := standup(now, time.Hour, 2*time.Hour)
		ev.Accepted = nil
		for i := range c.n {
			ev.Accepted = append(ev.Accepted, rt.EventGuest{UserID: "U" + strconv.Itoa(i)})
		}
		v := layoutAt(newTestStyle(), eventDoc(ev), Options{}, 600)
		if !hasWord(v, c.want) {
			t.Errorf("%d accepted: no %q in %q", c.n, c.want, evWords(v))
		}
	}
}

// shouldExcludeYear (Nmg4): no year this year, or last year within the 31
// days before now; a later year keeps it.
func TestShouldExcludeYear(t *testing.T) {
	now := time.Date(2027, 1, 5, 12, 0, 0, 0, time.Local)
	for _, c := range []struct {
		t    time.Time
		want bool
	}{
		{time.Date(2027, 3, 1, 9, 0, 0, 0, time.Local), true},
		{time.Date(2026, 12, 28, 9, 0, 0, 0, time.Local), true},
		{time.Date(2026, 11, 1, 9, 0, 0, 0, time.Local), false},
		{time.Date(2028, 1, 2, 9, 0, 0, 0, time.Local), false},
	} {
		if got := shouldExcludeYear(c.t, now); got != c.want {
			t.Errorf("%v: %v, want %v", c.t, got, c.want)
		}
	}
}

// The card lays out again when its state changes (it starts: NOW), with
// one frame asked for then, not a frame loop.
func TestEventRefresh(t *testing.T) {
	now := evClock(t)
	ev := standup(now, time.Minute, time.Hour)
	hs := newHarness(eventDoc(ev), &evHandler{})
	hs.w = 600
	hs.frame()
	if hasWord(hs.v, "NOW") {
		t.Fatalf("NOW before it starts")
	}
	if at := hs.v.d.refreshAt; !at.Equal(time.Unix(ev.Start, 0)) {
		t.Errorf("refresh at %v, want the start %v", at, time.Unix(ev.Start, 0))
	}
	later := now.Add(2 * time.Minute)
	eventNow = func() time.Time { return later }
	hs.frame()
	hs.frame()
	if !hasWord(hs.v, "NOW") {
		t.Errorf("no NOW once it started: %q", evWords(hs.v))
	}
}

// Over an event card the pointer is Slack's: the hand on Yes, the text
// cursor on the title, the default arrow off them.
func TestEventCursors(t *testing.T) {
	now := evClock(t)
	hs := newHarness(eventDoc(standup(now, time.Hour, 2*time.Hour)), &evHandler{})
	hs.w = 600
	hs.frame()
	r := oneEvent(t, hs.v)
	wantCursor(t, hs, "Yes", rectMid(r.Buttons["rsvp:accepted"]), pointer.CursorPointer)
	wantCursor(t, hs, "the title", fragMid(findFrag(t, &hs.v.d, "Standup")), pointer.CursorText)
	// the guests' row right of its two faces (the hover bar sits higher)
	wantCursor(t, hs, "the guests' row's empty right", image.Pt(r.Card.Max.X-20, r.Card.Min.Y+70), pointer.CursorDefault)
}
