package core

import (
	"encoding/json"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// noteMsgTS takes a socket message's ts into the notifications' maxTs
// (store/notifications_maxts.go), read on its own: a message the typed
// decode refuses, or one the event buffer had no room for, was still
// heard, and Slack's store took it, so the desktop_notification about it
// that follows isn't one for a message the client never had (19).
func noteMsgTS(s *store.Store, raw json.RawMessage) {
	var e struct {
		TS string `json:"ts"`
	}
	if json.Unmarshal(raw, &e) == nil && e.TS != "" {
		s.NoteAdded(e.TS)
	}
}

// onDroppedEvent is the socket's OnDrop: an event the buffer had no room
// for (slack/rtm.go). A new message's ts still goes into maxTs; its
// changes (an edit's event ts is the edit's time) don't.
func (a *App) onDroppedEvent(ev slack.Event) {
	if ev.Type != "message" {
		return
	}
	var e struct {
		Subtype string `json:"subtype"`
	}
	if json.Unmarshal(ev.Raw, &e) != nil {
		return
	}
	switch e.Subtype {
	case "message_changed", "message_deleted", "message_locked", "message_replied":
		return
	}
	noteMsgTS(a.S, ev.Raw)
}
