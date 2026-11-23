package ui

import (
	"encoding/json"
	"image"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/pluginlib"
	"github.com/some-du6e/clack/internal/slack"
)

// TestPluginGapOnlineCount: Slick's Online Count from the Library, in the
// whole client, over #plugin-bench with six people on its roster. The
// plugin lists them through Slack's flannel query export (edge
// users/list) and subscribes to their presence; the bench client has no
// socket, so the test hands core what the gateway answers presence_sub
// with (presence_change: four online, two away). The channel header then
// draws the count beside the avatar stack, as Slack's AutoClog hook lays
// it out: the stack's component is the stack, the count right of it, as
// tall as the stack and ending where the stack alone ended. A
// presence_change moves the count.
func TestPluginGapOnlineCount(t *testing.T) {
	var people []string
	for i := 1; i <= 6; i++ {
		people = append(people, fakeslack.SynthMemberID(93000+i))
	}
	e := gapStartWith(t, pluginlib.Slick, "Online Count", nil, func(fs *fakeslack.Server) {
		fs.SetRoster(fakeslack.BenchChannelID, people...)
	})
	c := e.a.data.core
	var placed image.Rectangle
	var stack image.Point
	hdrStackTap = func(ch string, p image.Rectangle, s image.Point) {
		if ch == fakeslack.BenchChannelID {
			placed, stack = p, s
		}
	}
	t.Cleanup(func() { hdrStackTap = nil })

	// the header's draw reads the channel's counts (users/counts: the
	// roster's six; the channel's fixture says 1240, which the plugin
	// would sample instead of counting)
	e.until("#plugin-bench's member count", 20*time.Second, func() bool {
		n, ok := c.MemberCountCached(fakeslack.BenchChannelID)
		return ok && n == len(people)
	})
	// it found the query export, listed the channel's people and
	// subscribed them (the presence manager's own counts)
	e.until("the plugin subscribing the channel's people", 30*time.Second, func() bool {
		subs := c.PresenceSubscriptions()
		for _, id := range people {
			if subs[id] < 1 {
				return false
			}
		}
		return true
	})
	e.until("the stack drawn alone first", 10*time.Second, func() bool { return placed.Dx() > 0 && placed.Dx() == stack.X })
	alone := placed
	presence := func(p string, ids ...string) {
		raw, _ := json.Marshal(map[string]any{"type": "presence_change", "presence": p, "users": ids})
		c.Inject(slack.Event{Type: "presence_change", Raw: raw})
	}
	presence("active", people[:4]...)
	presence("away", people[4:]...)
	e.until("the count of the 4 online in the header", 15*time.Second, func() bool {
		return e.treeWith("slick-online-count", `"tip":"4 online, 2 away"`) != "" && placed.Dx() > stack.X
	})
	// the count drawn, nothing keeps asking: settling, and the live
	// update below, send no more edge users/list (one listing of the six)
	lists := e.fs.CallCount("edge/users/list")
	e.settle(800 * time.Millisecond)
	if n := e.fs.CallCount("edge/users/list"); n != lists {
		t.Errorf("%d more edge users/list while the count stood", n-lists)
	}
	t.Logf("the stack alone %v; with the count %v (the stack %v of it)", alone, placed, stack)
	// the count right of the stack: the 6px gap, its 8px dot, the dot's
	// 5px gap and its 4px right padding at least
	if w := placed.Dx() - stack.X; w < 6+8+5+4 {
		t.Errorf("the component is %dpx wider than the stack: no room for the count", w)
	}
	if placed.Max.X != alone.Max.X {
		t.Errorf("the stack with its count ends at x=%d, the stack alone at %d: the header moved its right edge", placed.Max.X, alone.Max.X)
	}
	if placed.Dy() != stack.Y {
		t.Errorf("the stack with its count is %dpx tall, the stack %dpx: the count isn't centred beside it", placed.Dy(), stack.Y)
	}

	// one of the away people comes online
	presence("active", people[4])
	e.until("the count after a presence_change", 15*time.Second, func() bool {
		return e.treeWith("slick-online-count", `"tip":"5 online, 1 away"`) != ""
	})
	if n := e.fs.CallCount("edge/users/list"); n != lists {
		t.Errorf("a presence_change made %d more edge users/list", n-lists)
	}
}
