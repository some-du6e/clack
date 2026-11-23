package slick

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
)

// TestOnlineCountLikeSlack: Slick's OnlineCount, built from Slick's
// source, over fakeslack's workspace with the realtime socket up. Beside
// a channel's avatar stack (BaseAvatarStack) it shows how many of the
// channel's people are online, the way it does in Slack:
//
//   - it finds Slack's flannel query export (named query, its source
//     saying "Unsupported flannel method") and lists the channel's people
//     with it (edge users/list, people only, 100 a page);
//   - it subscribes to their presence (addMemberToPresenceSubscription),
//     so presence_sub carries every one of them, after sizing its own
//     subscriptions by getPresenceSubscriptionByTeamId, whose map counts
//     them too;
//   - once the gateway answered with everyone's presence it draws the
//     count, "4", with "4 online, 2 away" as its tip, and a presence_change
//     later moves it;
//   - stopped, it unsubscribes: presence_sub goes without them.
func TestOnlineCountLikeSlack(t *testing.T) {
	dir := pluginsDir(t)
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	const ch = fakeslack.ViewsChannel
	var people []string
	for i := 1; i <= 6; i++ {
		people = append(people, fakeslack.SynthMemberID(91000+i))
	}
	fs.SetRoster(ch, people...)
	for i, id := range people {
		p := "active"
		if i >= 4 {
			p = "away"
		}
		fs.SetPresence(id, p)
	}
	h := newBootedHarness(t, fs)
	// the channel's member count (users/counts), as its header's draw reads it
	if _, err := h.app.ChannelInfo(h.ctx(), ch); err != nil {
		t.Fatal(err)
	}
	if n, ok := h.app.MemberCountCached(ch); !ok || n != len(people) {
		t.Fatalf("%s's member count: %d (%v), want %d", ch, n, ok, len(people))
	}
	h.importFile(filepath.Join(dir, "OnlineCount.js"))
	if err := h.s.SetEnabled(h.ctx(), "OnlineCount", true); err != nil {
		t.Fatal(err)
	}
	if info := h.plugin("OnlineCount"); !info.Running || info.StartError != "" {
		t.Fatalf("OnlineCount didn't start: %+v", info)
	}
	h.render(1, "BaseAvatarStack", map[string]any{"channelId": ch, "memberCount": len(people)})
	shows := func(want string) func([]any) bool {
		return func(nodes []any) bool { text, _ := onlineCountOf(nodes); return text == want }
	}
	_, tip := onlineCountOf(h.waitTree(t, 1, "the count of the 4 online", shows("4")))
	if tip != "4 online, 2 away" {
		t.Errorf("its tip %q, want %q", tip, "4 online, 2 away")
	}

	// users/list asked for the channel's people the way the plugin does
	if fs.UserListFilters() == nil || !slices.Contains(fs.UserListFilters(), "everyone AND NOT bots AND NOT apps") {
		t.Errorf("no edge users/list for the channel's people: filters %q", fs.UserListFilters())
	}
	// presence_sub carries every one of the channel's people
	sub := lastPresenceSub(fs)
	for _, id := range people {
		if !slices.Contains(sub, id) {
			t.Errorf("presence_sub doesn't carry %s: %v", id, sub)
		}
	}
	// the presence manager's map counts them, and the viewer
	var counted map[string]int
	h.jsInto(`const g = T.getExport((e) => typeof e === 'function' && e.name === 'getPresenceSubscriptionByTeamId')
		return g(T.getRawState().selfTeamIds.teamId) ?? null`, &counted)
	for _, id := range append([]string{fakeslack.SelfID}, people...) {
		if counted[id] < 1 {
			t.Errorf("getPresenceSubscriptionByTeamId has no count for %s: %v", id, counted)
		}
	}
	for id := range counted {
		if !slices.Contains(sub, id) && !slices.Contains(lastPresenceSub(fs), id) {
			t.Errorf("getPresenceSubscriptionByTeamId counts %s, which presence_sub doesn't carry", id)
		}
	}

	// one of the away people comes online: the count follows
	fs.SetPresence(people[4], "active")
	_, tip = onlineCountOf(h.waitTree(t, 1, "the count after a presence_change", shows("5")))
	if tip != "5 online, 1 away" {
		t.Errorf("its tip after the change %q, want %q", tip, "5 online, 1 away")
	}

	// stopped, it lets their presence go
	if err := h.s.SetEnabled(h.ctx(), "OnlineCount", false); err != nil {
		t.Fatal(err)
	}
	eventually(t, 5*time.Second, "presence_sub without the channel's people", func() bool {
		s := lastPresenceSub(fs)
		return s != nil && !slices.ContainsFunc(people, func(id string) bool { return slices.Contains(s, id) })
	})
}

// onlineCountOf is what OnlineCount drew in a tree: its
// .slick-online-count's text, and the tip of the Tooltip round it.
func onlineCountOf(nodes []any) (text, tip string) {
	var walk func(ns []any, tipOf string) bool
	walk = func(ns []any, tipOf string) bool {
		for _, x := range ns {
			m, ok := x.(map[string]any)
			if !ok {
				continue
			}
			p, _ := m["p"].(map[string]any)
			in := tipOf
			if m["t"] == "@" && m["n"] == "Tooltip" {
				in, _ = p["tip"].(string)
			}
			kids, _ := m["c"].([]any)
			if c, _ := p["className"].(string); strings.Contains(" "+c+" ", " slick-online-count ") {
				text, tip = treeText(kids), in
				return true
			}
			if walk(kids, in) {
				return true
			}
		}
		return false
	}
	walk(nodes, "")
	return text, tip
}

// lastPresenceSub is the ids of the last presence_sub the client sent.
func lastPresenceSub(fs *fakeslack.Server) []string {
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
