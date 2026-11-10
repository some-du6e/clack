package core

import (
	"context"
	"testing"
)

// SetChannelNotifyPref is Slack's setNotificationsChannelOverride: one
// pref per call, {name, value, channel_id, global: false, sync: false};
// the refreshed prefs carry badge_all_unreads and desktop_push_enabled.
func TestSetChannelNotifyPref(t *testing.T) {
	a, f := testApp(t)
	f.reply["users.prefs.setNotifications"] = `{"ok":true}`
	f.reply["users.prefs.get"] = `{"ok":true,"prefs":{"all_notifications_prefs":"{\"channels\":{\"C1\":{\"badge_all_unreads\":true,\"desktop_push_enabled\":false,\"mobile\":\"nothing\"},\"C2\":{\"desktop\":\"everything\"}},\"global\":{}}"}}`
	if err := a.SetChannelNotifyPref(context.Background(), "C1", "badge_all_unreads", "true"); err != nil {
		t.Fatal(err)
	}
	p := f.last("users.prefs.setNotifications")
	for k, want := range map[string]string{"name": "badge_all_unreads", "value": "true", "channel_id": "C1", "global": "false", "sync": "false", "prefs": "", "channel_ids": ""} {
		if p.Get(k) != want {
			t.Errorf("%s = %q, want %q", k, p.Get(k), want)
		}
	}
	n := a.NotifyPrefs("C1")
	if !n.BadgeAllUnreads || n.DesktopPush != "false" || n.Mobile != "nothing" {
		t.Errorf("C1 prefs %+v", n)
	}
	if n := a.NotifyPrefs("C2"); n.DesktopPush != "" || n.BadgeAllUnreads {
		t.Errorf("C2 prefs %+v (unset desktop_push_enabled must stay \"\")", n)
	}
}
