package core

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

func TestDNDUpdated(t *testing.T) {
	now := time.Now()
	for _, c := range []struct {
		d    DNDStatus
		want bool
	}{
		{DNDStatus{}, false},
		{DNDStatus{SnoozeEnabled: true, SnoozeEnd: now.Add(time.Hour).Unix()}, true},
		{DNDStatus{SnoozeEnabled: true, SnoozeEnd: now.Add(-time.Minute).Unix()}, false},
		{DNDStatus{DNDEnabled: true, NextStart: now.Add(-time.Hour).Unix(), NextEnd: now.Add(time.Hour).Unix()}, true},
		{DNDStatus{DNDEnabled: true, NextStart: now.Add(time.Hour).Unix(), NextEnd: now.Add(2 * time.Hour).Unix()}, false},
	} {
		if got := c.d.Active(now); got != c.want {
			t.Errorf("%+v: %v, want %v", c.d, got, c.want)
		}
	}
	st := store.New("")
	st.Self = slack.User{ID: "U0SELF"}
	a := New(slack.New(slack.Creds{}), st)
	ev := func(user string, snooze bool) {
		raw, _ := json.Marshal(map[string]any{"type": "dnd_updated", "user": user,
			"dnd_status": map[string]any{"snooze_enabled": snooze, "snooze_endtime": now.Add(time.Hour).Unix()}})
		a.onDNDUpdated(nil, raw)
	}
	ev("U0SELF", true)
	if !a.DNDActive(t.Context()) {
		t.Fatal("snoozed, not active")
	}
	ev("U0OTHER", false) // someone else's
	if !a.DNDActive(t.Context()) {
		t.Fatal("another user's dnd_updated ended yours")
	}
	ev("U0SELF", false)
	if a.DNDActive(t.Context()) {
		t.Fatal("snooze ended, still active")
	}
}

// The control strip's status emoji (T6Tm): a preset status's own emoji;
// the headphones in a huddle; under availability_prefs_section, with no
// status emoji, dnd.info's dnd_profile_status (its emoji, the mantelpiece
// clock outside working hours, the telescope in focus mode); else the
// profile's emoji.
func TestSelfStatusEmoji(t *testing.T) {
	fs, a := bootFake(t)
	ctx := t.Context()
	now := time.Now()
	setSelf := func(emoji, canonical string, huddle bool) {
		u, ok := a.S.User(a.S.SelfID())
		if !ok {
			t.Fatal("no self user")
		}
		cp := *u
		cp.Profile.StatusEmoji, cp.Profile.StatusTextCanonical = emoji, canonical
		cp.Profile.HuddleState, cp.Profile.HuddleExpires = "", 0
		if huddle {
			cp.Profile.HuddleState, cp.Profile.HuddleExpires = "in_a_huddle", now.Add(time.Hour).Unix()
		}
		a.S.PutUsers([]slack.User{cp})
	}
	dnd := func(ps map[string]any) {
		st := map[string]any{"snooze_enabled": ps != nil}
		if ps != nil {
			st["dnd_profile_status"] = ps
		}
		fs.SetDND(st)
		if err := a.LoadDND(ctx); err != nil {
			t.Fatal(err)
		}
	}
	check := func(what, want string) {
		t.Helper()
		if got, _ := a.SelfStatusEmoji(now); got != want {
			t.Errorf("%s: %q, want %q", what, got, want)
		}
	}
	a.SetExperimentGroup("availability_prefs_section", "off")
	setSelf("", "", false)
	dnd(nil)
	check("nothing", "")
	dnd(map[string]any{"type": "focus_mode"})
	check("focus mode without availability_prefs_section", "")
	a.SetExperimentGroup("availability_prefs_section", "on")
	check("focus mode", "telescope")
	dnd(map[string]any{"type": "scheduled_dnd"})
	check("outside working hours", "mantelpiece_clock")
	dnd(map[string]any{"type": "focus_mode", "emoji": ":headphones:"})
	check("focus mode with a chosen emoji", "headphones")
	setSelf(":palm_tree:", "", false)
	check("a status emoji wins over the automatic one", "palm_tree")
	setSelf("", "", true)
	dnd(map[string]any{"type": "focus_mode"})
	check("in a huddle", "headphones")
	a.SetExperimentGroup("huddles_status_ultra", "on")
	check("huddles_status_ultra: no headphones", "telescope")
	setSelf(":calendar:", "In a meeting", true)
	check("a preset status", "calendar")
	if _, ok := a.DNDProfile(); !ok {
		t.Error("DNDProfile lost the profile status")
	}
	dnd(nil)
	if _, ok := a.DNDProfile(); ok {
		t.Error("DNDProfile kept a profile status dnd.info no longer has")
	}
}

// dnd_updated for the viewer asks dnd.info again for the profile status
// (Slack's dndUpdated marks the member stale).
func TestDNDUpdatedRefetches(t *testing.T) {
	fs, a := bootFake(t)
	fs.SetDND(map[string]any{"snooze_enabled": true, "snooze_endtime": time.Now().Add(time.Hour).Unix(),
		"dnd_profile_status": map[string]any{"type": "focus_mode"}})
	raw, _ := json.Marshal(map[string]any{"type": "dnd_updated", "user": a.S.SelfID(),
		"dnd_status": map[string]any{"snooze_enabled": true, "snooze_endtime": time.Now().Add(time.Hour).Unix()}})
	a.onDNDUpdated(t.Context(), raw)
	if ps, ok := a.DNDProfile(); !ok || ps.Type != "focus_mode" {
		t.Errorf("profile status after dnd_updated: %+v %v", ps, ok)
	}
	if !a.DNDActive(t.Context()) {
		t.Error("not snoozed")
	}
}
