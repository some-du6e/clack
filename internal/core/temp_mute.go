package core

import (
	"maps"
	"strings"
	"time"

	"github.com/some-du6e/clack/internal/store"
)

// Temporary mutes, as Slack's client keeps them under temp_mute
// (client-boot O2By VJ; boot-render zuQH, build 4d0c78085232):
//
//   - initializeMutedChannelsFromPref / updateMutedChannelsFromPref: a
//     channel is muted when its all_notifications_prefs override has
//     muted, or (with temp_mute on) a temp_mute_expiration_ts still ahead;
//   - kickOffTempMuteTimer: a timer at the next expiry, no sooner than 15 s
//     and no later than 5 minutes away, recomputes the muted channels and
//     arms itself again;
//   - without temp_mute the expiration is ignored: the channel isn't muted
//     by it.
//
// "Temporarily mute…" itself (the bell's More options) is
// popups/notify_menu.go.

// ExpTempMute is the experiment's name (TestExperimentReadsListed).
const ExpTempMute = "temp_mute"

// TempMuteOn is O2By VJ.
func (a *App) TempMuteOn() bool { return a.experiment(ExpTempMute) == "on" }

// addTempMutes marks the channels whose temporary mute hasn't run out and
// returns the soonest expiration ahead (0: none).
func addTempMutes(p store.Prefs, muted map[string]bool, now int64) (next int64) {
	for id, n := range p.ChannelNotify {
		if n.TempMuteExp > now {
			muted[id] = true
			if next == 0 || n.TempMuteExp < next {
				next = n.TempMuteExp
			}
		}
	}
	return next
}

// tempMuteWait is kickOffTempMuteTimer's delay for the expiration next.
func tempMuteWait(next, now int64) time.Duration {
	t := next
	if t-now < 15 {
		t = now + 15
	}
	if t-now > 300 {
		t = now + 300
	}
	return time.Duration(t-now) * time.Second
}

// withTempMutes adds the temporary mutes to muted (under temp_mute) and
// arms the timer for the next expiration.
func (a *App) withTempMutes(p store.Prefs, muted map[string]bool) map[string]bool {
	a.mu.Lock()
	if a.tempMuteTimer != nil {
		a.tempMuteTimer.Stop()
		a.tempMuteTimer = nil
	}
	a.mu.Unlock()
	if !a.TempMuteOn() {
		return muted
	}
	now := time.Now().Unix()
	next := addTempMutes(p, muted, now)
	if next == 0 {
		return muted
	}
	d := tempMuteWait(next, now)
	a.mu.Lock()
	a.tempMuteTimer = time.AfterFunc(d, a.refreshTempMutes)
	a.mu.Unlock()
	return muted
}

// refreshTempMutes recomputes the muted channels from the prefs Clack
// holds (the timer firing, temp_mute changing group) and tells the UI when
// they changed.
func (a *App) refreshTempMutes() {
	a.S.RLock()
	raw := a.S.RawPrefs
	before := maps.Clone(a.S.Muted)
	a.S.RUnlock()
	if raw == nil {
		return
	}
	p, muted := store.ParsePrefs(raw)
	muted = a.withTempMutes(p, muted)
	if maps.Equal(before, muted) {
		return
	}
	a.S.SetPrefs(p, muted)
	pp := a.Prefs()
	a.emit(Event{Type: "prefs", Prefs: &pp})
}

// DNDEndHour is getDayOfWeekDndEndTime (boot-shared BTdg g): the day's
// "dnd_before_<day>" under custom days with that day partial, else
// dnd_end_hour; "" when unset. Slack's "Until tomorrow" ends then
// (09:00 when unset).
func (a *App) DNDEndHour(day time.Weekday) string {
	if a.PrefString("dnd_days") == "custom" {
		name := strings.ToLower(day.String())
		if a.PrefString("dnd_enabled_"+name) == "partial" {
			if t := a.PrefString("dnd_before_" + name); t != "" {
				return t
			}
		}
	}
	return a.PrefString("dnd_end_hour")
}
