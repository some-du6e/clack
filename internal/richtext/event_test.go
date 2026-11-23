package richtext

import (
	"strings"
	"testing"
)

// Synthetic fixtures only.

// eventRes is the test resolver (the viewer is USELF) with pictures.
type eventRes struct{ fakeRes }

func (eventRes) MemberName(id string) (string, bool) { return "", false }
func (eventRes) MemberAvatar(id string) string {
	if id == "U1" || id == "USELF" {
		return "https://avatars.test/" + id + ".png"
	}
	return ""
}

func eventOf(t *testing.T, block string) []Block {
	t.Helper()
	return FromMessage(eventRes{}, msg(t, `{"ts":"1.0","text":"fallback text","blocks":[`+block+`]}`)).Blocks
}

// An event's keys in either spelling (toCamelCaseProps, nested keys too):
// the viewer's answer and role, the accepted guests with the viewer first
// and their pictures, the other answers counted, cancelled and copied.
func TestEventDecode(t *testing.T) {
	for _, b := range []string{
		`{"type":"event","block_id":"ev","event_id":"E1","event":{"title":"Standup","start":{"date_time":1700000000},"end":{"date_time":"1700003600"},` +
			`"organizer":{"user_id":"U2"},"attendees":[{"user_id":"U1","rsvp":"accepted"},{"user_id":"USELF","rsvp":"accepted"},{"user_id":"U3","rsvp":"tentative"},` +
			`{"user_id":"U4","rsvp":"declined"},{"user_id":"U5","rsvp":"needs_action"},{"user_id":"U6","rsvp":"needs_action"}],"attendee_count":6,` +
			`"app_type":"gcal","meeting_provider":"zoom","meeting_url":"https://zoom.test/j","web_link":"https://cal.test/e","copies":["USELF"]}}`,
		`{"type":"event","blockId":"ev","eventId":"E1","event":{"title":"Standup","start":{"dateTime":1700000000},"end":{"dateTime":"1700003600"},` +
			`"organizer":{"userId":"U2"},"attendees":[{"userId":"U1","rsvp":"accepted"},{"userId":"USELF","rsvp":"accepted"},{"userId":"U3","rsvp":"tentative"},` +
			`{"userId":"U4","rsvp":"declined"},{"userId":"U5","rsvp":"needs_action"},{"userId":"U6","rsvp":"needs_action"}],"attendeeCount":6,` +
			`"appType":"gcal","meetingProvider":"zoom","meetingUrl":"https://zoom.test/j","webLink":"https://cal.test/e","copies":["USELF"]}}`,
	} {
		bs := eventOf(t, b)
		if len(bs) != 1 || bs[0].Kind != EventBlock || bs[0].Event == nil {
			t.Fatalf("%s: %s", b, dump(bs))
		}
		ev := bs[0].Event
		if ev.ID != "E1" || ev.Title != "Standup" || ev.Start != 1700000000 || ev.End != 1700003600 || ev.AttendeeCount != 6 ||
			ev.AppType != "gcal" || ev.MeetingProvider != "zoom" || ev.MeetingURL != "https://zoom.test/j" || ev.WebLink != "https://cal.test/e" {
			t.Errorf("event %+v", ev)
		}
		if !ev.Attendee || ev.RSVP != "accepted" || ev.Organizer || ev.Cancelled || !ev.Copied {
			t.Errorf("viewer: attendee %v rsvp %q organizer %v cancelled %v copied %v", ev.Attendee, ev.RSVP, ev.Organizer, ev.Cancelled, ev.Copied)
		}
		if len(ev.Accepted) != 2 || ev.Accepted[0].UserID != "USELF" || ev.Accepted[1].UserID != "U1" || ev.Accepted[1].Avatar != "https://avatars.test/U1.png" {
			t.Errorf("accepted %+v: want the viewer first, then U1 with its picture", ev.Accepted)
		}
		if ev.Maybe != 1 || ev.No != 1 || ev.NoReply != 2 {
			t.Errorf("counts maybe %d no %d no reply %d", ev.Maybe, ev.No, ev.NoReply)
		}
	}
	org := eventOf(t, `{"type":"event","event_id":"E2","event":{"title":"1:1","status":"cancelled","start":{"date_time":1},"end":{"date_time":2},"organizer":{"user_id":"USELF"},"attendees":[]}}`)
	if ev := org[0].Event; ev == nil || !ev.Organizer || ev.Attendee || !ev.Cancelled || ev.Copied {
		t.Errorf("organizer's cancelled event: %s", dump(org))
	}
}

// Event draws nothing without an event; the keys its props read first
// (start, end, organizer, attendees) are the block's error line when
// they're missing.
func TestEventDecodeErrors(t *testing.T) {
	if bs := eventOf(t, `{"type":"event","event_id":"E1"}`); len(bs) != 0 {
		t.Errorf("no event: %s, want nothing", dump(bs))
	}
	if bs := eventOf(t, `{"type":"event","event":null}`); len(bs) != 0 {
		t.Errorf("a null event: %s, want nothing", dump(bs))
	}
	full := `"start":{"date_time":1},"end":{"date_time":2},"organizer":{"user_id":"U2"},"attendees":[]`
	for _, drop := range []string{`"start":{"date_time":1},`, `"end":{"date_time":2},`, `"organizer":{"user_id":"U2"},`, `,"attendees":[]`} {
		ev := strings.Replace(full, drop, "", 1)
		bs := eventOf(t, `{"type":"event","event":{`+ev+`}}`)
		if len(bs) != 1 || bs[0].Kind != Unsupported {
			t.Errorf("without %s: %s, want the error line", drop, dump(bs))
		}
	}
	if bs := eventOf(t, `{"type":"event","event":{`+full+`,"attendees":"x"}}`); len(bs) != 1 || bs[0].Kind != Unsupported {
		t.Errorf("attendees a string: %s, want the error line", dump(bs))
	}
}
