package views

// Karim's Activity breakage, 2026-11-13: the All tab failed with "json:
// cannot unmarshal string into Go struct field .items.item.invite of
// type core.actInvite" with the Unreads filter on and off (Slack's feed
// sends item.invite as an id string), and a ping didn't show up live.
// Synthetic content only.

import (
	"encoding/json"
	"image"
	"testing"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func actStringInviteView(t *testing.T) (*Activity, *vaEnv) {
	t.Helper()
	app, fs := vaFake(t)
	fs.SetActivityInviteIDs(true)
	h := &vaHost{slickHost: &slickHost{app: app, th: theme.New(true)}}
	v := NewActivity(h)
	e := &vaEnv{size: image.Pt(1200, 1400)}
	e.draw = func(gtx layout.Context) { v.Layout(gtx) }
	return v, e
}

// The All tab loads with string invites, Unreads off and on, and the
// external invites' rows have their invite (connectInvites.list).
func TestActivityStringInviteLoads(t *testing.T) {
	v, e := actStringInviteView(t)
	for _, unread := range []bool{false, true} {
		if unread {
			v.unread.Value = true
			v.load(actFresh)
		}
		e.until(t, "the feed", func() bool {
			_, loaded, err := v.data.get()
			return loaded && !v.data.busy() || err != nil
		})
		p, _, err := v.data.get()
		if err != nil {
			t.Fatalf("unread %v: the page failed: %v", unread, err)
		}
		if len(p.Items) < 5 {
			t.Fatalf("unread %v: %d items", unread, len(p.Items))
		}
		found := 0
		for _, it := range p.Items {
			if it.Type == "external_channel_invite" || it.Type == "external_dm_invite" {
				found++
				if it.Invite == nil || it.Invite.Sig == "" {
					t.Errorf("unread %v: %s has no invite", unread, it.Key)
				}
			}
			if unread && !it.Unread {
				t.Errorf("%s read under Unreads", it.Key)
			}
		}
		if found != 2 {
			t.Errorf("unread %v: %d external invites, want 2", unread, found)
		}
		e.frame()
	}
}

// A realtime at_user ping shows in Activity at once, at the top, also
// when the entry carries a field of an odd shape (a string invite).
func TestActivityRealtimeMentionAppearsAtOnce(t *testing.T) {
	v, _, e, _ := vaFakeActivity(t, false)
	app := v.h.Core()
	n := len(actPage(v).Items)
	for _, odd := range []bool{false, true} {
		key, ts := "fa-ping", "1762702300.000100"
		item := map[string]any{"type": "at_user", "message": map[string]any{"ts": "1762700100.000100", "channel": fakeslack.ViewsChannel, "author_user_id": "U0FAKE00001"}}
		if odd {
			key, ts = "fa-ping-odd", "1762702400.000100"
			item["invite"] = "I0ODD"
		}
		raw, _ := json.Marshal(map[string]any{"type": "activity", "subtype": "activity_updated", "key": key,
			"entry": map[string]any{"key": key, "feed_ts": ts, "is_unread": true, "item": item}})
		app.Inject(slack.Event{Type: "activity", Subtype: "activity_updated", Raw: raw})
		n++
		want := n
		e.until(t, key+" in the list", func() bool { return len(actPage(v).Items) == want })
		if k := actPage(v).Items[0].Key; k != key {
			t.Errorf("first row %s, want %s", k, key)
		}
	}
}
