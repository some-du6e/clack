package slick

import (
	"testing"
	"time"
)

// The channel's header tab reaches Slack's view state, where ChatTabs
// reads it to restore each channel's last tab: SetTabView's view goes
// through setRoute to T.setView, and the primary view becomes the tab's
// (toChannelTab's {id, viewType: Channel, params: {tabId, parentTab}}). A
// plain navigation's view is the channel's own again.
func TestSetTabViewReachesTheStore(t *testing.T) {
	h := newHarness(t)
	h.s.SetRoute(teamID, chGen, "")
	primary := func() map[string]any {
		var v map[string]any
		h.jsInto(`return globalThis.__taut.getState().view?.['v2::main::home::primary'] ?? null`, &v)
		return v
	}
	tab := func(v map[string]any) string {
		p, _ := v["params"].(map[string]any)
		id, _ := p["tabId"].(string)
		if parent, _ := p["parentTab"].(string); id != "" && parent != chGen {
			return "?"
		}
		return id
	}
	eventually(t, 5*time.Second, "the channel's own view", func() bool {
		v := primary()
		return v["id"] == chGen && v["viewType"] == "Channel" && tab(v) == ""
	})
	h.s.SetTabView(chGen, `{"id":"`+chGen+`","viewType":"Channel","params":{"tabId":"files","parentTab":"`+chGen+`"}}`)
	eventually(t, 5*time.Second, "the Files tab's view", func() bool {
		v := primary()
		return v["id"] == chGen && tab(v) == "files"
	})
	h.s.SetTabView(chGen, "")
	eventually(t, 5*time.Second, "the channel's own view again", func() bool {
		v := primary()
		return v["id"] == chGen && tab(v) == ""
	})
}
