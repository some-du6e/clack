package fakeslack

import (
	"encoding/json"
	"net/url"

	"github.com/some-du6e/clack/internal/slack"
)

// An event block's calls: calendar.event.rsvp (Yes, No, Maybe) and
// calendar.event.copy (Copy to Your Calendar), each with the event and the
// message it's in, as Slack's server takes them: a call without the
// channel or the message_ts is invalid_arguments. A held message's event
// takes the answer (the viewer's attendee entry) or the copy (copies),
// and message_changed goes out, as the server tells the clients.
// Synthetic.

// Event ids the calendar calls answer differently: EventNeedsAuth with an
// auth_url (the viewer's calendar isn't connected), EventRSVPFails with
// an error.
const (
	EventNeedsAuth  = "EV0AUTH"
	EventRSVPFails  = "EV0FAIL"
	EventConnectURL = "https://calendar.test/connect"
)

func (s *Server) handleCalendar(method string, f url.Values) (any, bool) {
	bad := map[string]any{"ok": false, "error": "invalid_arguments"}
	id, ch, ts := f.Get("event_id"), f.Get("channel"), f.Get("message_ts")
	switch method {
	case "calendar.event.rsvp":
		switch {
		case id == "" || f.Get("rsvp") == "" || ch == "" || ts == "":
			return bad, true
		case id == EventRSVPFails:
			return map[string]any{"ok": false, "error": "fatal_error"}, true
		}
	case "calendar.event.copy":
		if id == "" || ch == "" || ts == "" {
			return bad, true
		}
	default:
		return nil, false
	}
	if id == EventNeedsAuth {
		return ok("auth_url", EventConnectURL), true
	}
	s.updateEvent(ch, ts, id, func(ev map[string]any) {
		if method == "calendar.event.copy" {
			cs, _ := ev["copies"].([]any)
			ev["copies"] = append(cs, SelfID)
			return
		}
		atts, _ := ev["attendees"].([]any)
		for _, a := range atts {
			if at, _ := a.(map[string]any); at != nil && at["user_id"] == SelfID {
				at["rsvp"] = f.Get("rsvp")
				return
			}
		}
		ev["attendees"] = append(atts, map[string]any{"user_id": SelfID, "rsvp": f.Get("rsvp")})
	})
	return ok(), true
}

// updateEvent changes the event (event_id id) of the held message ch/ts
// and sends message_changed; a message the fake doesn't hold (the
// gallery's, made on each read) is left alone.
func (s *Server) updateEvent(ch, ts, id string, change func(ev map[string]any)) {
	s.histMu.Lock()
	var prev, m slack.Message
	found := false
	h := s.hist[ch]
	for i := range h {
		if h[i].TS != ts {
			continue
		}
		var blocks []map[string]any
		if json.Unmarshal(h[i].Blocks, &blocks) != nil {
			break
		}
		for _, b := range blocks {
			if b["type"] == "event" && b["event_id"] == id {
				if ev, _ := b["event"].(map[string]any); ev != nil {
					change(ev)
					found = true
				}
			}
		}
		if found {
			prev = h[i]
			raw, _ := json.Marshal(blocks)
			h[i].Blocks = raw
			m = h[i]
		}
		break
	}
	s.histMu.Unlock()
	if !found {
		return
	}
	asMap := func(x slack.Message) map[string]any {
		raw, _ := json.Marshal(x)
		var out map[string]any
		_ = json.Unmarshal(raw, &out)
		out["channel"] = ch
		return out
	}
	ets := uptimeEditTS(ts)
	s.Push(map[string]any{"type": "message", "subtype": "message_changed", "channel": ch, "hidden": true,
		"ts": ets, "event_ts": ets, "message": asMap(m), "previous_message": asMap(prev)})
}
