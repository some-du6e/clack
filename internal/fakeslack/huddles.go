package fakeslack

import (
	"fmt"
	"net/url"
	"strings"
	"sync"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// The huddle menus' API: rooms.getLink (Copy huddle link),
// calendar.user.status and calendar.getConnectedCalendars (the Schedule
// Huddle modal's calendar check), huddles.scheduled.new (Schedule), and
// conversations.open for a huddle with someone you have no DM with yet,
// rooms.inviteResponse (Decline / Be there soon on an incoming invite) and
// dnd.info (whether an invite rings). Nothing here starts a call:
// rooms.join stays the fake Chime's (internal/huddle.NewFake).

// HuddleLinkBase is where rooms.getLink's links point: + the channel id.
const HuddleLinkBase = "https://app.slack.com/huddle/" + TeamID + "/"

// ScheduledHuddle is one huddles.scheduled.new call.
type ScheduledHuddle struct {
	Form url.Values
}

type huddleState struct {
	mu        sync.Mutex
	noCal     bool // calendar.user.status answers "not connected"
	scheduled []ScheduledHuddle
	opened    []string // conversations.open users
	ims       map[string]string
	responses []url.Values // rooms.inviteResponse forms
	dnd       map[string]any
}

// InviteResponses returns the rooms.inviteResponse calls so far.
func (s *Server) InviteResponses() []url.Values {
	s.hud.mu.Lock()
	defer s.hud.mu.Unlock()
	return append([]url.Values(nil), s.hud.responses...)
}

// SetDND sets what dnd.info answers (nil: notifications on).
func (s *Server) SetDND(status map[string]any) {
	s.hud.mu.Lock()
	s.hud.dnd = status
	s.hud.mu.Unlock()
}

// SetCalendarConnected makes calendar.user.status answer with (true, the
// default) or without a connected, writable Google Calendar.
func (s *Server) SetCalendarConnected(on bool) {
	s.hud.mu.Lock()
	s.hud.noCal = !on
	s.hud.mu.Unlock()
}

// ScheduledHuddles returns the huddles.scheduled.new calls so far.
func (s *Server) ScheduledHuddles() []ScheduledHuddle {
	s.hud.mu.Lock()
	defer s.hud.mu.Unlock()
	return append([]ScheduledHuddle(nil), s.hud.scheduled...)
}

// OpenedDMs returns the users of each conversations.open so far.
func (s *Server) OpenedDMs() []string {
	s.hud.mu.Lock()
	defer s.hud.mu.Unlock()
	return append([]string(nil), s.hud.opened...)
}

func (s *Server) handleHuddles(method string, f url.Values) (any, bool) {
	h := &s.hud
	switch method {
	case "rooms.getLink":
		ch := f.Get("channel_id")
		if ch == "" {
			return map[string]any{"ok": false, "error": "invalid_channel_id"}, true
		}
		return ok("link", HuddleLinkBase+ch), true
	case "calendar.user.status":
		h.mu.Lock()
		st := "connected"
		if h.noCal {
			st = "not_connected"
		}
		h.mu.Unlock()
		return ok("status", []map[string]any{{"cal_app_type": "GCAL", "user_status": st, "is_calendar_app_installed": true}}), true
	case "calendar.getConnectedCalendars":
		h.mu.Lock()
		noCal := h.noCal
		h.mu.Unlock()
		if noCal || f.Get("cal_app_type") != "GCAL" {
			return ok("connected_calendars", []any{}), true
		}
		return ok("connected_calendars", []map[string]any{{"id": "primary", "name": "Fake calendar", "is_writable": true}}), true
	case "rooms.inviteResponse":
		h.mu.Lock()
		h.responses = append(h.responses, f)
		h.mu.Unlock()
		switch {
		case f.Get("channel_id") == "" || f.Get("room_id") == "":
			return map[string]any{"ok": false, "error": "invalid_arguments"}, true
		case f.Get("response") != "decline" && f.Get("response") != "joining_soon" && f.Get("response") != "accept":
			return map[string]any{"ok": false, "error": "invalid_response"}, true
		}
		return map[string]any{"ok": true}, true
	case "dnd.info":
		h.mu.Lock()
		out := map[string]any{"ok": true, "dnd_enabled": false, "snooze_enabled": false}
		for k, v := range h.dnd {
			out[k] = v
		}
		h.mu.Unlock()
		return out, true
	case "huddles.scheduled.new":
		h.mu.Lock()
		h.scheduled = append(h.scheduled, ScheduledHuddle{Form: f})
		n := len(h.scheduled)
		h.mu.Unlock()
		if f.Get("channel_id") == "" {
			return map[string]any{"ok": false, "error": "channel_not_found"}, true
		}
		return ok("icaluid", fmt.Sprintf("fake-%d@calendar", n)), true
	case "conversations.open":
		users := f.Get("users")
		if users == "" || strings.Contains(users, ",") {
			return nil, false // group DMs: not here
		}
		h.mu.Lock()
		h.opened = append(h.opened, users)
		if h.ims == nil {
			h.ims = map[string]string{}
		}
		id := h.ims[users]
		h.mu.Unlock()
		if id == "" {
			s.convMu.Lock()
			for _, c := range s.convs {
				if c.IsIM && c.User == users {
					id = c.ID
				}
			}
			s.convMu.Unlock()
		}
		if id == "" {
			h.mu.Lock()
			id = fmt.Sprintf("D0OPEN%05d", len(h.ims)+1)
			h.ims[users] = id
			h.mu.Unlock()
		}
		return ok("channel", slack.Conversation{ID: id, IsIM: true, User: users}), true
	}
	return nil, false
}
