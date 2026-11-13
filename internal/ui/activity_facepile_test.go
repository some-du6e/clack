package ui

import (
	"encoding/json"
	"image"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

// An Activity item's conversation, opened beside the inbox, draws its
// header's member facepile with avatars (polish-1113c: they stayed the
// loading tiles). Only a Navigate to a conversation made the header's
// reads (backend.opened); the secondary column (OpenChannelSecondary,
// activity_inbox_remove_persistent_detail_panel on) and the persistent
// detail panel's pane (Panes.ChannelPane, the experiment off) draw it
// through layoutChannel without one, so its members were never read.
// The members the read returns aren't in the store beforehand; their
// avatars come from what the read stored.
func TestActivityItemFacepileLoads(t *testing.T) {
	for _, persistent := range []bool{false, true} {
		name := "secondary"
		if persistent {
			name = "persistent-panel"
		}
		t.Run(name, func(t *testing.T) {
			e := newActShell(t)
			if persistent {
				e.ca.SetExperimentGroup("activity_inbox_remove_persistent_detail_panel", "off")
			}
			ch := fakeslack.ViewsChannel
			delete(e.a.data.fetched, ch) // newActShell skips the header reads; this test wants them
			// ch's members are people the store has never seen
			fresh := []slack.User{
				{ID: "U0FACEPIL01", TeamID: fakeslack.TeamID, Name: "pat", Profile: slack.Profile{RealName: "Pat Example", Image72: "https://avatars.invalid/pat-72.png"}},
				{ID: "U0FACEPIL02", TeamID: fakeslack.TeamID, Name: "sam", Profile: slack.Profile{RealName: "Sam Example", Image72: "https://avatars.invalid/sam-72.png"}},
				{ID: "U0FACEPIL03", TeamID: fakeslack.TeamID, Name: "lee", Profile: slack.Profile{RealName: "Lee Example", Image72: "https://avatars.invalid/lee-72.png"}},
			}
			e.fs.SetEdgeHook(func(method string, body map[string]json.RawMessage) (any, bool) {
				var chans []string
				var filter string
				_ = json.Unmarshal(body["channels"], &chans)
				_ = json.Unmarshal(body["filter"], &filter)
				if method != "edge/users/list" || len(chans) != 1 || chans[0] != ch || filter == "apps" {
					return nil, false
				}
				return map[string]any{"ok": true, "results": fresh, "next_marker": ""}, true
			})
			known := map[string]bool{}
			for _, u := range fresh {
				if _, ok := e.ca.S.RawUser(u.ID); ok {
					known[u.ID] = true
				}
			}
			if c := e.a.model().conv(ch); c == nil || len(c.Members) > 0 {
				t.Fatalf("%s before the item opened: %+v", ch, c)
			}
			e.a.Navigate("activity")
			e.until("the activity view", func() bool { return e.activity() != nil })
			e.until("the activity feed", func() bool { return e.fs.CallCount("activity.feed") > 0 })
			e.frames(20)
			drawn := func() bool {
				at, ok := e.a.data.drawn[ch]
				return ok && time.Since(at) < 500*time.Millisecond
			}
			for y := 200; y < e.sz.Y-40 && !drawn(); y += 30 {
				e.click(image.Pt(e.sz.X/3, y))
			}
			if !drawn() {
				t.Fatalf("no row opened %s's conversation beside the inbox", ch)
			}
			if e.a.router.Current().Page() != "activity" {
				t.Fatalf("the item navigated away from Activity: %s", e.a.router.Current())
			}
			e.until("the opened item's facepile", func() bool {
				c := e.a.model().conv(ch)
				return c != nil && len(c.Members) > 0
			})
			uncached := 0
			for _, id := range e.a.model().conv(ch).Members {
				if !known[id] {
					uncached++
				}
				if e.a.userAvatar(id) == "" {
					t.Errorf("facepile member %s has no avatar", id)
				}
			}
			if uncached != len(fresh) {
				t.Errorf("facepile %v, want the %d members the read returned, none of them in the store before", e.a.model().conv(ch).Members, len(fresh))
			}
		})
	}
}
