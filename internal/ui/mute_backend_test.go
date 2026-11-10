package ui

import (
	"encoding/json"
	"net/url"
	"sync"
	"testing"
	"time"
)

// The shell's mute (the DM header bell's menu) is Slack's
// muteChannelById / unmuteChannelById: the row flips at once, the
// "muted" channel pref goes to users.prefs.setNotifications (global=false,
// one channel), core reads it back, and a refused write puts the row back
// without a toast.
func TestBackendSetMutedWritesSlackPref(t *testing.T) {
	e, fs := previewEnv(t)
	dm := ""
	for id, c := range e.a.model().Convs {
		if c.Kind == "im" && !c.Muted {
			dm = id
			break
		}
	}
	if dm == "" {
		t.Fatal("fakeslack has no unmuted DM")
	}
	type write struct{ ch, prefs, global string }
	var mu sync.Mutex
	var writes []write
	refuse := false
	fs.SetHook(func(method string, form url.Values) (any, bool) {
		if method != "users.prefs.setNotifications" {
			return nil, false
		}
		mu.Lock()
		defer mu.Unlock()
		writes = append(writes, write{form.Get("channel_ids"), form.Get("prefs"), form.Get("global")})
		if refuse {
			return map[string]any{"ok": false, "error": "restricted_action"}, true
		}
		return nil, false // fakeslack stores it
	})
	muted := func() bool { c := e.a.model().conv(dm); return c != nil && c.Muted }
	coreMuted := func() bool { c, _ := e.a.Core().Conv(dm); return c.Muted }
	e.a.toast = toastState{}

	e.a.data.setMuted(dm, true)
	if !muted() {
		t.Fatal("the row didn't go muted at once")
	}
	e.until("core sees the mute", 10*time.Second, coreMuted)
	mu.Lock()
	if len(writes) != 1 {
		t.Fatalf("%d setNotifications writes, want 1", len(writes))
	}
	w := writes[0]
	mu.Unlock()
	var ps []struct{ Name, Value string }
	if err := json.Unmarshal([]byte(w.prefs), &ps); err != nil || len(ps) != 1 || ps[0].Name != "muted" || ps[0].Value != "true" {
		t.Errorf("prefs = %s, want [{muted true}]", w.prefs)
	}
	if w.ch != dm || w.global != "false" {
		t.Errorf("wrote channel_ids=%q global=%q, want %q false", w.ch, w.global, dm)
	}

	// already muted: no second write
	e.a.data.setMuted(dm, true)
	e.settle(100 * time.Millisecond)
	mu.Lock()
	if len(writes) != 1 {
		t.Errorf("muting a muted DM wrote again (%d writes)", len(writes))
	}
	refuse = true
	mu.Unlock()

	e.a.data.setMuted(dm, false)
	if muted() {
		t.Fatal("the row didn't unmute at once")
	}
	e.until("the refused unmute reverts", 10*time.Second, muted)
	if !coreMuted() {
		t.Error("core lost the mute after a refused unmute")
	}

	mu.Lock()
	refuse = false
	mu.Unlock()
	e.a.data.setMuted(dm, false)
	e.until("the unmute write", 10*time.Second, func() bool { mu.Lock(); defer mu.Unlock(); return len(writes) == 3 })
	mu.Lock()
	t.Logf("writes %+v", writes)
	mu.Unlock()
	dl := time.Now().Add(10 * time.Second)
	for coreMuted() && time.Now().Before(dl) {
		e.settle(20 * time.Millisecond)
	}
	if coreMuted() {
		t.Fatalf("core still has %s muted after the unmute (notify prefs %+v, prefs muted %v)", dm, e.a.Core().NotifyPrefs(dm), e.a.Core().Prefs().Muted)
	}
	if muted() {
		t.Error("the row is still muted after unmuting")
	}
	if e.a.toast.text != "" {
		t.Errorf("mute toasted %q; Slack's doesn't", e.a.toast.text)
	}
}
