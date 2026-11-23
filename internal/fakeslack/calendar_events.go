package fakeslack

import "net/url"

// An event block's calls: calendar.event.rsvp (Yes, No, Maybe) and
// calendar.event.copy (Copy to Your Calendar). Synthetic.

// Event ids the calendar calls answer differently: EventNeedsAuth with an
// auth_url (the viewer's calendar isn't connected), EventRSVPFails with
// an error.
const (
	EventNeedsAuth  = "EV0AUTH"
	EventRSVPFails  = "EV0FAIL"
	EventConnectURL = "https://calendar.test/connect"
)

func (s *Server) handleCalendar(method string, f url.Values) (any, bool) {
	switch method {
	case "calendar.event.rsvp":
		switch {
		case f.Get("event_id") == "" || f.Get("rsvp") == "":
			return map[string]any{"ok": false, "error": "invalid_arguments"}, true
		case f.Get("event_id") == EventRSVPFails:
			return map[string]any{"ok": false, "error": "fatal_error"}, true
		}
	case "calendar.event.copy":
		if f.Get("event_id") == "" {
			return map[string]any{"ok": false, "error": "invalid_arguments"}, true
		}
	default:
		return nil, false
	}
	if f.Get("event_id") == EventNeedsAuth {
		return ok("auth_url", EventConnectURL), true
	}
	return ok(), true
}
