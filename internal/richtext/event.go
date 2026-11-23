package richtext

// An event block: a calendar event's card (Slack's Event, boot-shared
// 0lYF; CDN build 9f450b8ad3db). Its props go through toCamelCaseProps
// (ijf8 camelCaseKeysDeep, ten levels deep), so every key of the event
// takes either spelling. Event draws nothing without an event; the keys
// its connected props read first (start, end, organizer, attendees)
// throw when they're missing, which is the block's error line.

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Event is an event block's event as the card reads it.
type Event struct {
	// ID is the block's event_id, which RSVP and Copy to Your Calendar
	// send.
	ID    string
	Title string
	// Start and End are start.dateTime and end.dateTime, unix seconds.
	Start, End int64
	// AttendeeCount is attendeeCount ("{n} guests:").
	AttendeeCount int
	// Accepted are the accepted attendees, the viewer first
	// (BaseEventRSVPStatus); Maybe, No and NoReply count the tentative,
	// declined and needs_action ones.
	Accepted           []EventGuest
	Maybe, No, NoReply int
	// RSVP is the viewer's attendee entry's rsvp; Attendee whether there
	// is one. Organizer: the viewer organizes it.
	RSVP                string
	Attendee, Organizer bool
	// Cancelled is status "cancelled".
	Cancelled bool
	// AppType is the calendar (gcal, ocal, ocal_gcc), MeetingURL and
	// MeetingProvider the call, WebLink the event on the calendar.
	AppType, MeetingURL, MeetingProvider, WebLink string
	// Copied is whether copies holds the viewer ("Copied to your primary
	// calendar!").
	Copied bool
	// NeedsRefetch is needsRefetch: a non-attendee, non-organizer viewer
	// fetches the message again for the next set of attendees.
	NeedsRefetch bool
}

// EventGuest is an accepted attendee: the member and their picture.
type EventGuest struct{ UserID, Avatar string }

// bkEvent is an event block's keys, raw (toCamelCaseProps: either
// spelling).
type bkEvent struct {
	Event   json.RawMessage `json:"event"`
	EventID json.RawMessage `json:"event_id"`
	IDCamel json.RawMessage `json:"eventId"`
	BlockID json.RawMessage `json:"block_id"`
}

// camelKey is 2v8Q's camelCase for the keys Slack sends: snake_case to
// camelCase.
func camelKey(k string) string {
	if !strings.Contains(k, "_") {
		return k
	}
	parts := strings.Split(k, "_")
	var b strings.Builder
	b.WriteString(parts[0])
	for _, p := range parts[1:] {
		if p != "" {
			b.WriteString(strings.ToUpper(p[:1]) + p[1:])
		}
	}
	return b.String()
}

// camelDeep is camelCaseKeyDeepPrivate: v with its object keys camel-cased
// to depth max (arrays count as a level).
func camelDeep(v any, depth, max int) any {
	if depth > max {
		return v
	}
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			out[camelKey(k)] = camelDeep(e, depth+1, max)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = camelDeep(e, depth+1, max)
		}
		return out
	}
	return v
}

// unixOf is moment.unix's argument: a number, or a numeric string.
func unixOf(v any) (int64, bool) {
	switch t := v.(type) {
	case float64:
		return int64(t), true
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(t), 64); err == nil {
			return int64(f), true
		}
	}
	return 0, false
}

// eventBlock builds an event block.
func (c *builder) eventBlock(out []Block, b *bk) []Block {
	v := b.event
	if v == nil {
		v = &bkEvent{}
	}
	raw, ok := rawValue(v.Event)
	if !ok || !jsTruthy(raw) {
		return out // event_event: no event, nothing
	}
	// processProps camel-cases the block's props: the event sits one level
	// under them
	m, _ := camelDeep(raw, 1, 10).(map[string]any)
	if m == nil {
		return append(out, unsupported()) // e.start.dateTime of a primitive's undefined
	}
	start, sok := m["start"].(map[string]any)
	end, eok := m["end"].(map[string]any)
	org, ook := m["organizer"].(map[string]any)
	atts, aok := m["attendees"].([]any)
	if !sok || !eok || !ook || !aok {
		return append(out, unsupported())
	}
	ev := &Event{ID: rawString(firstRaw(v.EventID, v.IDCamel)), MeetingURL: stringKey(m, "meetingUrl"),
		MeetingProvider: stringKey(m, "meetingProvider"), WebLink: stringKey(m, "webLink"), AppType: stringKey(m, "appType")}
	if t, ok := m["title"].(string); ok {
		ev.Title = t
	}
	ev.Start, _ = unixOf(start["dateTime"])
	ev.End, _ = unixOf(end["dateTime"])
	if n, ok := m["attendeeCount"].(float64); ok {
		ev.AttendeeCount = int(n)
	}
	ev.Cancelled = m["status"] == "cancelled"
	ev.NeedsRefetch = jsTruthy(m["needsRefetch"])
	self := ""
	if c.r != nil {
		self = c.r.SelfID()
	}
	ev.Organizer = self != "" && org["userId"] == self
	for _, a := range atts {
		at, _ := a.(map[string]any)
		if at == nil {
			continue
		}
		uid, _ := at["userId"].(string)
		rsvp, _ := at["rsvp"].(string)
		if uid != "" && uid == self && !ev.Attendee {
			ev.Attendee, ev.RSVP = true, rsvp
		}
		switch rsvp {
		case "accepted":
			g := EventGuest{UserID: uid}
			if mn, ok := c.r.(MemberNamer); ok && uid != "" {
				g.Avatar = SafeImage(mn.MemberAvatar(uid))
			}
			if uid == self && self != "" {
				ev.Accepted = append([]EventGuest{g}, ev.Accepted...)
			} else {
				ev.Accepted = append(ev.Accepted, g)
			}
		case "tentative":
			ev.Maybe++
		case "declined":
			ev.No++
		case "needs_action":
			ev.NoReply++
		}
	}
	if cs, ok := m["copies"].([]any); ok && self != "" {
		for _, u := range cs {
			if u == self {
				ev.Copied = true
			}
		}
	}
	return append(out, Block{Kind: EventBlock, BlockID: b.BlockID, Event: ev})
}
