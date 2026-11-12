package fakeslack

import (
	"fmt"
	"strconv"
	"time"
)

// Focus mode on the server. Slack's client turns focus mode on and off
// with one write, users.prefs.set focus_mode_expiration_ts (activate /
// deactivateFocusMode, client-boot Z1fa; nothing calls dnd.setSnooze).
// The server then pauses notifications and sets the automatic "In focus
// mode" status, and tells the client with dnd_updated, after which the
// client asks dnd.info again (dndUpdated marks the member stale). The fake
// does the same: a future expiration snoozes until then (9999999999, Don't
// turn off, has no end) with dnd_profile_status {type: focus_mode}; 0 or
// a past time ends it. Tests that SetDND by hand get what they set until
// the next focus mode write.

// focusModeWritten applies a focus_mode_expiration_ts write.
func (s *Server) focusModeWritten(value string) {
	exp, _ := strconv.ParseInt(value, 10, 64)
	now := time.Now().Unix()
	status := map[string]any{"dnd_enabled": false, "next_dnd_start_ts": 1, "next_dnd_end_ts": 1}
	if exp > now {
		status["snooze_enabled"] = true
		status["snooze_endtime"] = exp
		status["snooze_remaining"] = exp - now
		status["dnd_profile_status"] = map[string]any{"type": "focus_mode", "text": "", "emoji": ""}
	} else {
		status["snooze_enabled"] = false
	}
	s.SetDND(status)
	ev := map[string]any{"type": "dnd_updated", "user": SelfID, "dnd_status": status,
		"event_ts": fmt.Sprintf("%d.000300", now)}
	go s.Push(ev) // after the reply, as the server's socket would
}

// FocusModeDND reports the fake's current dnd.info answer (tests).
func (s *Server) FocusModeDND() map[string]any {
	s.hud.mu.Lock()
	defer s.hud.mu.Unlock()
	out := map[string]any{}
	for k, v := range s.hud.dnd {
		out[k] = v
	}
	return out
}
