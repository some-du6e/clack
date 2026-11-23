package fakeslack

import (
	"encoding/json"
	"fmt"
	"time"
)

// The event block (Slack's Event): a calendar event's card for a guest, an
// organizer and anyone else, on now, ahead, ended and cancelled. Synthetic:
// the times are relative to when the fake starts, so "now" and "ended"
// hold whenever the gallery is drawn.

func init() { RegisterGallery("20-event", galleryEvent) }

type evFx struct {
	id, title, app, provider, url, status string
	start, end                            time.Duration // from now
	organizer                             string
	attendees                             [][2]string // user, rsvp
	count                                 int
	copies                                []string
}

func (f evFx) block() string {
	now := time.Now()
	atts := []map[string]any{}
	for _, a := range f.attendees {
		atts = append(atts, map[string]any{"user_id": a[0], "rsvp": a[1]})
	}
	ev := map[string]any{"title": f.title, "start": map[string]any{"date_time": now.Add(f.start).Unix()},
		"end": map[string]any{"date_time": now.Add(f.end).Unix()}, "organizer": map[string]any{"user_id": f.organizer},
		"attendees": atts, "attendee_count": f.count, "app_type": f.app, "meeting_provider": f.provider, "meeting_url": f.url,
		"web_link": "https://calendar.test/event/" + f.id, "status": f.status, "copies": f.copies}
	b, _ := json.Marshal(map[string]any{"type": "event", "block_id": "ev-" + f.id, "event_id": f.id, "event": ev})
	return string(b)
}

func galleryEvent(e GalleryEnv) []GalleryMessage {
	u := func(i int) string {
		if i < len(e.Users) {
			return e.Users[i]
		}
		return fmt.Sprintf("U0EVGUEST%02d", i)
	}
	team := [][2]string{{SelfID, "accepted"}, {u(1), "accepted"}, {u(2), "tentative"}, {u(3), "declined"}, {u(4), "needs_action"}}
	ten := [][2]string{}
	for i := range 10 {
		ten = append(ten, [2]string{fmt.Sprintf("U0EVGUEST%02d", i), "accepted"})
	}
	fx := []struct {
		name, text string
		ev         evFx
	}{
		{"event/on now, a guest", "Standup is on", evFx{id: "EV01", title: "Team standup", app: "gcal", provider: "zoom", url: "https://zoom.test/j/1",
			start: -15 * time.Minute, end: 45 * time.Minute, organizer: u(1), attendees: team, count: 5}},
		{"event/tomorrow, no answer yet", "Planning tomorrow", evFx{id: EventNeedsAuth, title: "Quarterly planning", app: "ocal", provider: "msft_teams", url: "https://teams.test/m/2",
			start: 26 * time.Hour, end: 27 * time.Hour, organizer: u(1), attendees: [][2]string{{SelfID, "needs_action"}, {u(1), "accepted"}}, count: 2}},
		{"event/not a guest", "Office hours (Copy to Your Calendar)", evFx{id: "EV03", title: "Design office hours", app: "gcal", provider: "google_meet", url: "https://meet.test/abc",
			start: 50 * time.Hour, end: 51 * time.Hour, organizer: u(2), attendees: [][2]string{{u(2), "accepted"}}, count: 1, copies: []string{}}},
		{"event/copied", "Office hours, copied", evFx{id: "EV04", title: "Design office hours", app: "gcal", provider: "google_meet", url: "https://meet.test/abc",
			start: 50 * time.Hour, end: 51 * time.Hour, organizer: u(2), attendees: [][2]string{{u(2), "accepted"}}, count: 1, copies: []string{SelfID}}},
		{"event/organizer, no meeting link", "My 1:1", evFx{id: "EV05", title: "1:1", app: "gcal", start: 3 * time.Hour, end: 4 * time.Hour,
			organizer: SelfID, attendees: [][2]string{{u(1), "needs_action"}}, count: 2}},
		{"event/ended", "Yesterday's retro", evFx{id: "EV06", title: "Sprint retro", app: "gcal", provider: "slack", url: "https://slack.test/huddle",
			start: -26 * time.Hour, end: -25 * time.Hour, organizer: u(1), attendees: team, count: 5}},
		{"event/cancelled", "Cancelled", evFx{id: "EV07", title: "Offsite dinner", app: "ocal", status: "cancelled",
			start: 30 * time.Hour, end: 32 * time.Hour, organizer: u(1), attendees: team, count: 5}},
		{"event/ten guests, no title, Outlook GCC High", "Ten guests", evFx{id: "EV08", app: "ocal_gcc", provider: "webex", url: "https://webex.test/9",
			start: 70 * time.Hour, end: 71 * time.Hour, organizer: u(1), attendees: ten, count: 12}},
	}
	out := make([]GalleryMessage, 0, len(fx))
	for _, f := range fx {
		out = append(out, GalleryMessage{Name: f.name, Text: f.text, Blocks: `[` + f.ev.block() + `]`})
	}
	return out
}
