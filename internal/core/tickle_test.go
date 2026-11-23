package core

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
)

// notify-relatives round 2: maybeTickleMS (tickle.go). Synthetic ids only.

// tickleReasons is the reasons of the tickles the fake's gateway heard.
func tickleReasons(fs *fakeslack.Server) []string {
	var out []string
	for _, f := range fs.SocketFrames() {
		var v struct {
			Type   string `json:"type"`
			Reason string `json:"reason"`
			ID     int64  `json:"id"`
		}
		if json.Unmarshal([]byte(f), &v) == nil && v.Type == "tickle" && v.ID > 0 {
			out = append(out, v.Reason)
		}
	}
	return out
}

// A tickle goes out with its reason while the socket is up; another within
// the throttle doesn't, and a typing indicator counts as the activity the
// throttle reads (lastSocketActivity); none while the viewer set
// themselves away (manual_presence_change), and again once they're back.
// Clack sent none.
func TestTickleLikeSlack(t *testing.T) {
	const every = 400 * time.Millisecond
	tickleThrottle(t, every)
	a, fs, _ := rtApp(t)
	ch := fs.ChannelIDs()[2]
	sent := func(n int) []string {
		t.Helper()
		time.Sleep(150 * time.Millisecond) // a send runs aside
		got := tickleReasons(fs)
		if len(got) != n {
			t.Fatalf("the gateway heard %d tickles %q, want %d", len(got), got, n)
		}
		return got
	}
	tickle(a, "focus")
	until(t, "the first tickle", func() bool { return len(tickleReasons(fs)) == 1 })
	if got := sent(1); got[0] != "focus" {
		t.Errorf("the tickle's reason is %q, want focus", got[0])
	}
	tickle(a, "mousedown")
	sent(1) // within the throttle
	time.Sleep(every)
	if err := a.RTM.Typing(ch, ""); err != nil {
		t.Fatal(err)
	}
	tickle(a, "keydown")
	sent(1) // the typing indicator moved the throttle's clock
	time.Sleep(every)
	tickle(a, "keydown")
	if got := sent(2); got[1] != "keydown" {
		t.Errorf("the second tickle's reason is %q, want keydown", got[1])
	}
	fs.Push(map[string]any{"type": "manual_presence_change", "presence": "away"})
	until(t, "the viewer manually away", func() bool { return manualPresence(a) == "away" })
	time.Sleep(every)
	tickle(a, "polling")
	sent(2) // manually away: none
	fs.Push(map[string]any{"type": "manual_presence_change", "presence": "active"})
	until(t, "the viewer back", func() bool { return manualPresence(a) == "active" })
	tickle(a, "polling")
	if got := sent(3); got[2] != "polling" {
		t.Errorf("the third tickle's reason is %q, want polling", got[2])
	}
	a.S.RLock()
	p := a.S.Presence[fakeslack.SelfID]
	a.S.RUnlock()
	if p != "active" {
		t.Errorf("after manual_presence_change active the viewer's presence is %q", p)
	}
}
