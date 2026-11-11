package core

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/store"
)

// kickOffTempMuteTimer waits for the next expiration, but no less than
// 15 s and no more than 5 minutes.
func TestTempMuteTimerClamp(t *testing.T) {
	for _, c := range []struct {
		next, now int64
		want      time.Duration
	}{
		{1000 + 60, 1000, 60 * time.Second},
		{1000 + 3, 1000, 15 * time.Second},
		{1000 + 3600, 1000, 300 * time.Second},
	} {
		if got := tempMuteWait(c.next, c.now); got != c.want {
			t.Errorf("tempMuteWait(+%d) = %v, want %v", c.next-c.now, got, c.want)
		}
	}
}

func tempMutePrefs(exp int64) map[string]json.RawMessage {
	anp, _ := json.Marshal(map[string]any{"channels": map[string]any{
		"C1": map[string]any{"temp_mute_expiration_ts": strconv.FormatInt(exp, 10)},
		"C9": map[string]any{"muted": true},
	}})
	s, _ := json.Marshal(string(anp))
	return map[string]json.RawMessage{"all_notifications_prefs": s}
}

// updateMutedChannelsFromPref: under temp_mute a channel whose
// temp_mute_expiration_ts is ahead is muted; without it, or once it has
// passed, it isn't. Changing temp_mute's group re-reads them.
func TestTempMuteMutes(t *testing.T) {
	a := rulesApp(t, store.Prefs{})
	a.setExperiments(map[string]string{"temp_mute": "on"})
	a.installPrefs(tempMutePrefs(time.Now().Unix() + 3600))
	muted := func(id string) bool {
		a.S.RLock()
		defer a.S.RUnlock()
		return a.S.Muted[id]
	}
	if !muted("C1") || !muted("C9") {
		t.Fatalf("temp_mute on: C1 %v C9 %v", muted("C1"), muted("C9"))
	}
	a.mu.Lock()
	armed := a.tempMuteTimer != nil
	a.mu.Unlock()
	if !armed {
		t.Error("no timer for the expiration")
	}
	a.SetExperimentGroup("temp_mute", "off")
	if muted("C1") || !muted("C9") {
		t.Errorf("temp_mute off: C1 %v C9 %v", muted("C1"), muted("C9"))
	}
	a.ClearExperimentGroup("temp_mute")
	if !muted("C1") {
		t.Error("temp_mute back on: C1 isn't muted")
	}
	a.installPrefs(tempMutePrefs(time.Now().Unix() - 10)) // ran out
	if muted("C1") {
		t.Error("an expired temporary mute still mutes")
	}
}

// getDayOfWeekDndEndTime: a custom schedule's partial day ends at its
// dnd_before_<day>, every other day at dnd_end_hour.
func TestDNDEndHour(t *testing.T) {
	a := rulesApp(t, store.Prefs{})
	q := func(s string) json.RawMessage { b, _ := json.Marshal(s); return b }
	a.installPrefs(map[string]json.RawMessage{"dnd_end_hour": q("08:30"), "dnd_days": q("custom"),
		"dnd_enabled_tuesday": q("partial"), "dnd_before_tuesday": q("10:15"), "dnd_enabled_monday": q("full")})
	if got := a.DNDEndHour(time.Tuesday); got != "10:15" {
		t.Errorf("Tuesday %q", got)
	}
	if got := a.DNDEndHour(time.Monday); got != "08:30" {
		t.Errorf("Monday %q", got)
	}
}
