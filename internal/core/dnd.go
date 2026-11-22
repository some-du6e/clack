package core

import (
	"context"
	"encoding/json"
	"strings"
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
	// ProfileStatus is dnd.info's dnd_profile_status: the automatic
	// status the server shows while focus mode or the working-hours
	// schedule pauses notifications (Slack keeps it in
	// dndV2.profileStatuses under availability_prefs_section,
	// updateDndForCurrentMember). nil when there is none.
	ProfileStatus *DNDProfileStatus `json:"dnd_profile_status,omitempty"`
}

// DNDProfileStatus is one dnd_profile_status: its type ("scheduled_dnd"
// for outside working hours; anything else is focus mode), and the text
// and emoji the viewer chose for it (empty: Slack's defaults).
type DNDProfileStatus struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	Emoji string `json:"emoji"`
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
	at     time.Time // when status was set
	asked  time.Time // the last refetch DNDHeld started
	// events counts the viewer's dnd_updated events applied
	// (onDNDUpdated): a dnd.info read queued before the last one gives way
	// to it (loadDNDAfter)
	events uint64
}

// dndMaxAge is how long the viewer's DND status stays fresh: Slack keeps
// it in a conditional fetch manager (client-boot Z/ON, storeKey dndV2)
// whose maxAge is 600 s, and getDndStatusByMemberId's read refetches an
// older one in the background (maybeRefetch). A dnd_updated the socket
// missed (it was down) is caught that way.
const dndMaxAge = 10 * time.Minute

// dndRetry spaces DNDHeld's refetches while dnd.info fails.
const dndRetry = time.Minute

// SetDNDStatus replaces the viewer's DND status (dnd.info, dnd_updated;
// tests, the demo).
func (a *App) SetDNDStatus(d DNDStatus) {
	a.dnd.mu.Lock()
	a.dnd.known, a.dnd.status, a.dnd.at = true, d, time.Now()
	a.dnd.mu.Unlock()
}

// LoadDND asks dnd.info for the viewer (Slack's fetchUserDndInfo for the
// current member: at boot, and again after each dnd_updated marks it
// stale) and keeps the answer. Off the UI goroutine.
func (a *App) LoadDND(ctx context.Context) error {
	raw, err := a.DND(ctx)
	if err != nil {
		return err
	}
	var st DNDStatus
	if err := json.Unmarshal(raw, &st); err != nil {
		return err
	}
	a.SetDNDStatus(st)
	return nil
}

// DNDProfile is the viewer's dnd_profile_status as last read (ok false
// when dnd.info hasn't answered or there is none).
func (a *App) DNDProfile() (DNDProfileStatus, bool) {
	a.dnd.mu.Lock()
	defer a.dnd.mu.Unlock()
	if !a.dnd.known || a.dnd.status.ProfileStatus == nil {
		return DNDProfileStatus{}, false
	}
	return *a.dnd.status.ProfileStatus, true
}

// DNDHeld is getCurrentMemberDndStatus (client-boot d4YN ux) as the store
// holds it: whether the viewer's notifications are paused now, without
// waiting (false until dnd.info or dnd_updated brought a status, as
// Slack's store reads before its dnd.info answers). A status older than
// dndMaxAge, or none, is asked for again in the background (dndRetry
// apart), as Slack's read refetches one. Any goroutine: the notification
// decisions read it on the UI goroutine.
func (a *App) DNDHeld() bool {
	now := time.Now()
	a.dnd.mu.Lock()
	known, st := a.dnd.known, a.dnd.status
	refetch := (!known || now.Sub(a.dnd.at) > dndMaxAge) && now.Sub(a.dnd.asked) > dndRetry
	if refetch {
		a.dnd.asked = now
	}
	a.dnd.mu.Unlock()
	if refetch && a.C != nil {
		go func() { // maybeRefetch: this read takes the status as held
			ctx, cancel := context.WithTimeout(a.bg, 15*time.Second)
			defer cancel()
			_ = a.LoadDND(ctx)
		}()
	}
	return known && st.Active(now)
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
// someone else's). Slack's handler only marks the member stale
// (dndUpdated, then fetchAndUpsertDndForUsers asks dnd.info again); the
// event's dnd_status lands at once here and dnd.info then brings the
// dnd_profile_status the event doesn't carry (the rail's status tab).
func (a *App) onDNDUpdated(ctx context.Context, raw json.RawMessage) {
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
	a.dnd.mu.Lock()
	prev := a.dnd.status.ProfileStatus
	a.dnd.mu.Unlock()
	st := *ev.Status
	if st.ProfileStatus == nil {
		st.ProfileStatus = prev // until dnd.info answers
	}
	a.dnd.mu.Lock()
	a.dnd.known, a.dnd.status, a.dnd.at = true, st, time.Now()
	a.dnd.events++
	a.dnd.mu.Unlock()
	if ctx != nil {
		_ = a.LoadDND(ctx)
	}
}

// dndEvents is how many of the viewer's dnd_updated events onDNDUpdated has
// applied.
func (a *App) dndEvents() uint64 {
	a.dnd.mu.Lock()
	defer a.dnd.mu.Unlock()
	return a.dnd.events
}

// loadDNDAfter is LoadDND for the read a dnd_updated queued off the event
// pump (pump_later.go) when gen of them had been applied: its answer
// stands unless a newer dnd_updated came after the read was asked for,
// whose own read follows it. An older answer landing late put back a
// snooze the viewer had ended, and with the later read failing the toasts
// stayed held until DNDHeld's refetch (dndMaxAge).
func (a *App) loadDNDAfter(ctx context.Context, gen uint64) error {
	raw, err := a.DND(ctx)
	if err != nil {
		return err
	}
	var st DNDStatus
	if err := json.Unmarshal(raw, &st); err != nil {
		return err
	}
	a.dnd.mu.Lock()
	defer a.dnd.mu.Unlock()
	if a.dnd.events != gen {
		return nil
	}
	a.dnd.known, a.dnd.status, a.dnd.at = true, st, time.Now()
	return nil
}

// SelfStatusEmoji is the status emoji the control strip's user button
// shows over the avatar (client-boot T6Tm, the User's currentStatusEmoji;
// web client build fa938a086569): the profile's own status when it came
// from a preset (status_text_canonical); else the headphones while in a
// huddle (eYYm isCustomStatusAHuddlesStatus: huddle_state in_a_huddle
// until a future huddle_state_expiration_ts, no status_emoji, and
// huddles_status_ultra off); else, under availability_prefs_section with
// no status_emoji, the dnd_profile_status's (eYYm Xt / k8: its emoji, or
// mantelpiece_clock outside working hours, telescope in focus mode); else
// the profile's status emoji. name has no colons; url is set for the
// profile's own emoji (its resolved image), empty otherwise.
func (a *App) SelfStatusEmoji(now time.Time) (name, url string) {
	u := a.Self()
	profile := func() (string, string) { return strings.Trim(u.StatusEmoji, ":"), u.StatusEmojiURL }
	if u.StatusCanonical != "" {
		return profile()
	}
	if a.Experiment("huddles_status_ultra") != "on" && u.Huddle && u.HuddleExpires > now.Unix() && u.StatusEmoji == "" {
		return "headphones", ""
	}
	if a.Experiment("availability_prefs_section") == "on" && u.StatusEmoji == "" {
		if ps, ok := a.DNDProfile(); ok && ps.Type != "" {
			switch {
			case ps.Emoji != "":
				return strings.Trim(ps.Emoji, ":"), ""
			case ps.Type == "scheduled_dnd":
				return "mantelpiece_clock", ""
			}
			return "telescope", ""
		}
	}
	return profile()
}
