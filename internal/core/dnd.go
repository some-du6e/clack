package core

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// The viewer's Do Not Disturb, as Slack's getCurrentMemberDndStatus reads
// it (9wzb): paused notifications (snooze) or inside the DND schedule.
// dnd.info seeds it once; dnd_updated keeps it current.

// DNDStatus is Slack's dnd_status (dnd.info's answer, dnd_updated's
// dnd_status).
type DNDStatus struct {
	DNDEnabled    bool  `json:"dnd_enabled"`
	NextStart     int64 `json:"next_dnd_start_ts"`
	NextEnd       int64 `json:"next_dnd_end_ts"`
	SnoozeEnabled bool  `json:"snooze_enabled"`
	SnoozeEnd     int64 `json:"snooze_endtime"`
}

// Active reports whether notifications are paused at now.
func (d DNDStatus) Active(now time.Time) bool {
	t := now.Unix()
	if d.SnoozeEnabled && (d.SnoozeEnd == 0 || t < d.SnoozeEnd) {
		return true
	}
	return d.DNDEnabled && d.NextStart > 0 && d.NextStart <= t && (d.NextEnd == 0 || t < d.NextEnd)
}

type dndState struct {
	mu     sync.Mutex
	known  bool
	status DNDStatus
}

// SetDNDStatus replaces the viewer's DND status (tests, the demo).
func (a *App) SetDNDStatus(d DNDStatus) {
	a.dnd.mu.Lock()
	a.dnd.known, a.dnd.status = true, d
	a.dnd.mu.Unlock()
}

// DNDActive reports whether the viewer's notifications are paused now,
// asking dnd.info the first time. Off the UI goroutine.
func (a *App) DNDActive(ctx context.Context) bool {
	a.dnd.mu.Lock()
	known, st := a.dnd.known, a.dnd.status
	a.dnd.mu.Unlock()
	if !known {
		raw, err := a.DND(ctx)
		if err != nil {
			return false
		}
		if json.Unmarshal(raw, &st) != nil {
			return false
		}
		a.SetDNDStatus(st)
	}
	return st.Active(time.Now())
}

// onDNDUpdated applies dnd_updated (the viewer's own; dnd_updated_user is
// someone else's).
func (a *App) onDNDUpdated(raw json.RawMessage) {
	var ev struct {
		User   string     `json:"user"`
		Status *DNDStatus `json:"dnd_status"`
	}
	if json.Unmarshal(raw, &ev) != nil || ev.Status == nil {
		return
	}
	if ev.User != "" && ev.User != a.S.SelfID() {
		return
	}
	a.SetDNDStatus(*ev.Status)
}
