package messages

import (
	"testing"
	"time"

	"gioui.org/io/pointer"
)

// TestLinkClickHandsOffAtOnce times a link's click in a message, the first
// part of the 3 s Karim waited on Windows (alpha.157, a news bot's
// https link) before the browser came up: from the frame that delivers
// the release to the hand-off (openURL, which then runs the shell on
// its own goroutine: openurl.Open). Nothing on the way waits: the
// richview click, MaybeWarnLink's check (a link whose text isn't a URL),
// the workflow, canvas, tab and list openers and FollowLink are all
// synchronous, so the link goes out in the release's own frame. No
// plugin listens for clicks here; with one (Slick's Quick Join), the
// click goes through the plugin's JS first (slackc.ClickLink, timed in
// internal/ui's TestPluginGapQuickJoinDoubleClick).
func TestLinkClickHandsOffAtOnce(t *testing.T) {
	e := newCtxEnv(t)
	var at []time.Time
	openURL = func(u string) { at = append(at, time.Now()) }
	p := e.find(tsCtxLink, "a link", onLink)
	var worst time.Duration
	for i := 0; i < 5; i++ {
		e.clock += time.Second
		e.r.Queue(
			pointer.Event{Kind: pointer.Move, Position: p, Source: pointer.Mouse, Time: e.clock},
			pointer.Event{Kind: pointer.Press, Position: p, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Time: e.clock + 10*time.Millisecond},
		)
		e.frame()
		e.r.Queue(pointer.Event{Kind: pointer.Release, Position: p, Source: pointer.Mouse, Time: e.clock + 60*time.Millisecond})
		t0 := time.Now()
		e.frame() // the release's frame
		if len(at) != i+1 {
			e.frame()
			t.Fatalf("click %d: the link wasn't handed off in its release's frame (%d hand-offs after one more)", i+1, len(at))
		}
		d := at[i].Sub(t0)
		worst = max(worst, d)
		t.Logf("click %d: release frame start → hand-off %v", i+1, d)
		for k := 0; k < 3; k++ {
			e.frame()
		}
	}
	if len(at) != 5 {
		t.Fatalf("%d hand-offs for 5 clicks", len(at))
	}
	// one frame's work, nowhere near the seconds Karim saw
	if worst > 100*time.Millisecond {
		t.Errorf("the slowest click took %v from its release's frame to the hand-off, want under 100ms", worst)
	}
}
