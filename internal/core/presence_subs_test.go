package core

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
)

// TestPluginSubscriptionsKeepClacksOwnPresence: the members a plugin
// subscribes to (addMemberToPresenceSubscription: OnlineCount's batch of
// 520 from a big channel) sit beside Clack's own presence set in
// presence_sub, as Slack's presence manager (client-boot 4hlf) keeps
// them beside its own: they don't count against the 500 people Clack keeps
// for what its views show. So the people a view showed before the batch
// stay subscribed, and a profile card opened during it subscribes its
// member, who gets presence (presence_change), instead of being added and
// dropped in the same call.
func TestPluginSubscriptionsKeepClacksOwnPresence(t *testing.T) {
	a, fs, _ := rtApp(t)
	lastSub := func() []string {
		var out []string
		for _, f := range fs.SocketFrames() {
			var m struct {
				Type string   `json:"type"`
				IDs  []string `json:"ids"`
			}
			if json.Unmarshal([]byte(f), &m) == nil && m.Type == "presence_sub" {
				out = m.IDs
			}
		}
		return out
	}
	carries := func(what string, ids ...[]string) {
		t.Helper()
		var sub []string
		dl := time.Now().Add(5 * time.Second)
		for {
			sub = lastSub()
			missing := ""
			for _, set := range ids {
				for _, id := range set {
					if !slices.Contains(sub, id) {
						missing = id
						break
					}
				}
			}
			if missing == "" {
				return
			}
			if time.Now().After(dl) {
				t.Fatalf("presence_sub doesn't carry %s (%s; %d ids)", missing, what, len(sub))
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	var shown []string // people Clack's views showed
	for i := 0; i < 20; i++ {
		shown = append(shown, fakeslack.SynthMemberID(94001+i))
	}
	a.SubscribePresence(shown)
	carries("the people a view showed", shown)

	var batch []string // a plugin's subscriptions
	for i := 0; i < 520; i++ {
		id := fakeslack.SynthMemberID(95000 + i)
		batch = append(batch, id)
		a.AddPresenceSubscription(id)
	}
	carries("the plugin's batch", batch)

	card := fakeslack.SynthMemberID(96001)
	fs.SetPresence(card, "active")
	a.SubscribePresence([]string{card})
	carries("a card opened during the batch, the views' people and the batch", []string{card, a.C.Creds.UserID}, shown, batch)
	dl := time.Now().Add(5 * time.Second)
	for {
		a.S.RLock()
		p := a.S.Presence[card]
		a.S.RUnlock()
		if p == "active" {
			break
		}
		if time.Now().After(dl) {
			t.Fatalf("the card's member got no presence (%q)", p)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if n, want := len(lastSub()), len(a.presenceIDs(nil)); n != want {
		t.Errorf("presence_sub carries %d ids, the set is %d", n, want)
	}
	if got := a.PresenceSubscriptionOf(a.C.Creds.Team); len(got) < 1+len(shown)+1+len(batch) {
		t.Errorf("the presence manager's map has %d members, want the viewer, %d shown, the card and %d subscribed", len(got), len(shown), len(batch))
	}
}
