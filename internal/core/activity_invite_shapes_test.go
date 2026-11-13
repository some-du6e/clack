package core

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
)

// Slack's feed sends item.invite as a string: the joiner's user id for
// team_joiner_invite, the connect invite's id for external invites
// (/29I, rZx3 senderId: t.invite, oC getConnectInviteById). Karim's
// Activity failed on it every time, with the Unreads filter on and off
// (2026-11-13): "json: cannot unmarshal string into Go struct field
// .items.item.invite of type core.actInvite". Both of the All tab's
// requests load, and the invites come from connectInvites.list as
// Slack's inbox reads them.
func TestActivityStringInviteAllAndUnreads(t *testing.T) {
	for _, unread := range []bool{false, true} {
		name := map[bool]string{false: "all", true: "all+unreads"}[unread]
		t.Run(name, func(t *testing.T) {
			a, fs := actFake(t)
			fs.SetActivityInviteIDs(true)
			ctx := context.Background()
			page, err := a.Activity(ctx, ActivityOpts{View: "all", Unread: unread})
			if err != nil {
				t.Fatalf("the All tab (unread %v) failed: %v", unread, err)
			}
			keys := map[string]*ActivityItem{}
			for i := range page.Items {
				keys[page.Items[i].Key] = &page.Items[i]
			}
			for _, k := range []string{"fa-ext-invite", "fa-ext-dm-invite"} {
				it := keys[k]
				if it == nil {
					t.Errorf("%s missing", k)
					continue
				}
				if it.Invite == nil || it.Invite.Sig == "" || it.Invite.TeamID != fakeslack.ExternalTeamID || it.Actor != "U0PARTNER1" {
					t.Errorf("%s: invite not read from connectInvites.list: %+v (actor %q)", k, it.Invite, it.Actor)
				}
			}
			if it := keys["fa-ext-invite"]; it != nil && (it.Title != "synthetic-partners" || it.Invite == nil || it.Invite.ID != fakeslack.ActivityInviteChannel) {
				t.Errorf("fa-ext-invite: title %q invite %+v", it.Title, it.Invite)
			}
			w := actWrite(t, fs, "connectInvites.list")
			if w.Form.Get("direction") != "incoming" || w.Form.Get("reason") != "slack-connect-fetch-invites-for-activity-feed" ||
				w.Form.Get("invite_types") != `["channel","im"]` {
				t.Errorf("connectInvites.list %v", w.Form)
			}
			if unread {
				for _, it := range page.Items {
					if !it.Unread {
						t.Errorf("%s is read under Unreads", it.Key)
					}
				}
			}
			// the joiner (team_joiner_invite is behind an experiment in All)
			j, err := a.Activity(ctx, ActivityOpts{Types: "team_joiner_invite", Unread: unread})
			if err != nil || len(j.Items) != 1 || j.Items[0].Actor != "U0FAKE00002" {
				t.Errorf("team_joiner_invite: %+v %v, want the joiner U0FAKE00002 from item.invite", j.Items, err)
			}
		})
	}
}

// One odd entry never fails the page: a field of another type than
// Clack's decodes as missing, an entry that isn't an object is skipped,
// and the rest of the page is intact.
func TestActivityOddEntriesKeepThePage(t *testing.T) {
	body := `{"items":[
		{"key":"a","feed_ts":"1.1","is_unread":true,"item":{"type":"at_user","message":{"ts":"1.1","channel":"C1","author_user_id":"U1"}}},
		{"key":"b","feed_ts":"1.2","is_unread":"yes","item":{"type":"team_joiner_invite","invite":"U2","team_joiner_invite_payload":{"channel_id":"C1","is_reminder":"no"}}},
		"not an entry",
		null,
		{"key":"c","feed_ts":"1.3","is_unread":true,"item":{"type":"thread_v2","bundle_info":{"unread_count":"3","payload":{"thread_entry":{"channel_id":"C1","latest_ts":"1.3","thread_ts":"1.0","unread_msg_count":2}}}}},
		{"key":"d","feed_ts":"1.4","is_unread":true,"item":{"type":"external_channel_invite","invite":7}},
		{"key":"e","feed_ts":"1.5","is_unread":true,"item":{"type":"external_dm_invite","invite":{"id":"I5","date_invalid":"soon","inviting_user":{"id":"U9"}}}},
		{"key":"f","feed_ts":"1.6","is_unread":true,"item":{"type":"keyword","message":"C1/1.6"}}
	]}`
	var feed struct {
		Items []activityRaw `json:"items"`
	}
	if err := json.Unmarshal([]byte(body), &feed); err != nil {
		t.Fatalf("the page failed: %v", err)
	}
	got := map[string]ActivityItem{}
	for i := range feed.Items {
		if feed.Items[i].skip {
			continue
		}
		it, _ := activityItem(&feed.Items[i])
		got[it.Key] = it
	}
	if len(got) != 6 {
		t.Fatalf("%d entries, want 6 (the string and null skipped): %v", len(got), got)
	}
	if it := got["a"]; it.Channel != "C1" || it.TS != "1.1" || it.Author != "U1" {
		t.Errorf("a: %+v", it)
	}
	if it := got["b"]; it.Actor != "U2" || it.Channel != "C1" {
		t.Errorf("b: the joiner from item.invite: %+v", it)
	}
	if it := got["c"]; it.Channel != "C1" || it.ThreadTS != "1.0" || it.Count != 2 {
		t.Errorf("c: the thread kept the rest of its bundle: %+v", it)
	}
	if it := got["d"]; it.Invite != nil || it.InviteID != "" {
		t.Errorf("d: a numeric invite is no invite: %+v", it)
	}
	if it := got["e"]; it.Invite == nil || it.Invite.ID != "I5" || it.Actor != "U9" {
		t.Errorf("e: the invite object kept its other fields: %+v", it.Invite)
	}
	if it := got["f"]; it.Type != "keyword" {
		t.Errorf("f: %+v", it)
	}
}

// A realtime entry with an odd shape still reaches the views: it used to
// decode as an empty entry and be dropped as `activity_updated ""`
// (Karim: "i got a ping but it didnt show up in activity", 2026-11-13).
func TestActivityRealtimeOddShapeArrives(t *testing.T) {
	_, fs, log := rtApp(t)
	ch := fixtureCh
	push := func(key, ts string, item map[string]any) {
		fs.Push(map[string]any{"type": "activity", "subtype": "activity_updated", "key": key, "entry": map[string]any{
			"key": key, "feed_ts": ts, "is_unread": true, "item": item}})
	}
	push("ping-1", "1762709000.000100", map[string]any{"type": "at_user", "invite": "I0ODD",
		"message": map[string]any{"ts": "1762709000.000100", "channel": ch, "author_user_id": "U0FAKE00003"}})
	push("ping-2", "1762709001.000100", map[string]any{"type": "team_joiner_invite", "invite": "U0FAKE00003",
		"team_joiner_invite_payload": map[string]any{"channel_id": ch, "is_reminder": "sometimes"}})
	log.wait(t, "both realtime entries", func(evs []Event) bool {
		n := 0
		for _, ev := range evs {
			if ev.Type == "activity" && ev.Activity != nil && (ev.Activity.Key == "ping-1" || ev.Activity.Key == "ping-2") {
				n++
			}
		}
		return n == 2
	})
	for _, ev := range log.all() {
		if ev.Type != "activity" || ev.Activity == nil {
			continue
		}
		switch it := ev.Activity; it.Key {
		case "ping-1":
			if it.Type != "at_user" || it.Channel != ch || it.Author != "U0FAKE00003" || !it.Unread {
				t.Errorf("ping-1: %+v", it)
			}
		case "ping-2":
			if it.Actor != "U0FAKE00003" {
				t.Errorf("ping-2: joiner %q", it.Actor)
			}
		}
	}
}
