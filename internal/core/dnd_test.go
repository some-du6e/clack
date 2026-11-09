package core

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
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
		a.onDNDUpdated(raw)
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
