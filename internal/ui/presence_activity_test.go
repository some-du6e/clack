package ui

// notify-relatives round 2: the viewer's activity told to the realtime
// socket as Slack's client and desktop app tell it (presence_activity.go,
// core/tickle.go; docs/NOTIFY-RULES.md §10), against fakeslack's auto-away
// (the server marks the viewer away after a window without a tickle).
// Synthetic people and text only.

import (
	"context"
	"encoding/json"
	"image"
	"slices"
	"strings"
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/logtag"
)

// A viewer who reads, clicks, types and comes back to the window for
// twice the server's auto-away window stays active (a tickle for each
// reason: mousedown, keydown, focus), and @here in a channel at Mentions
// still toasts and badges for them; a burst of presses within the
// throttle sends one tickle; with nothing done in Clack's window the
// desktop poll keeps them active while the system has input, and lets
// them go away once the screen is locked, when @here stops reaching them
// as Slack's rules have it. Clack sent no tickle: the server's auto-away
// marked the viewer away while they read, and @here stopped toasting.
func TestPresenceActivityLikeSlack(t *testing.T) {
	presenceTimers(t, 250*time.Millisecond, 150*time.Millisecond)
	setIdle := systemIdle(t, 60) // the system idle: no poll tickles yet
	setLocked := screenLocked(t, false)
	t.Setenv("CLACK_HOME", t.TempDir())
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	ch := fs.ChannelIDs()[2]
	prefs, _ := json.Marshal(map[string]any{"global": map[string]any{"global_desktop": "mentions_dms", "global_mpdm_desktop": "mentions_dms"}})
	fs.SetPrefs(map[string]any{"all_notifications_prefs": string(prefs)})
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	c := e.a.data.core
	c.SetRealtime(true)
	e.until("the socket", 20*time.Second, func() bool { return c.RTM != nil && c.RTM.Connected() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := c.RefreshPrefs(ctx); err != nil {
		t.Fatal(err)
	}
	rec := &timedNotes{}
	e.a.notify = rec
	logs := captureLog(t)
	away := func() bool {
		c.S.RLock()
		defer c.S.RUnlock()
		return c.S.Presence[fakeslack.SelfID] == "away"
	}
	const window = 1500 * time.Millisecond
	fs.AutoAway(window)
	pt := image.Pt(e.size.X/2, e.size.Y/2)
	// reading and clicking around for twice the window
	for i, start := 0, time.Now(); time.Since(start) < 2*window; i++ {
		switch i % 3 {
		case 0:
			e.click(pt)
		case 1:
			e.key(key.NameShift, 0)
		case 2:
			e.a.setFocused(false)
			e.a.setFocused(true)
		}
		e.frame()
		time.Sleep(300 * time.Millisecond)
	}
	reasons := fs.Tickles()
	for _, want := range []string{"mousedown", "keydown", "focus"} {
		if !slices.Contains(reasons, want) {
			t.Errorf("no tickle for %s (the gateway heard %q)", want, reasons)
		}
	}
	if away() {
		t.Fatalf("the viewer read and clicked in Clack for twice the server's auto-away window and went away (the gateway heard tickles %q)", reasons)
	}
	// @here in a channel at Mentions reaches them: a toast and a badge
	e.a.focused = false
	line := func(ts string) string {
		for _, l := range logs.matching("notify: ") {
			if strings.HasSuffix(l, " "+logtag.ID(ch)+"/"+logtag.ID(ts)) {
				return l
			}
		}
		return ""
	}
	before := c.Count(ch).Mentions
	ts, ev := fs.PostQuiet(ch, fs.UserIDs()[3], "<!here> a synthetic heads-up")
	fs.Push(ev)
	e.until("the @here's decision line", 10*time.Second, func() bool { return line(ts) != "" })
	if l := line(ts); !strings.HasPrefix(l, "notify: show ") {
		t.Errorf("@here for a viewer active in Clack: %q, want it shown", l)
	}
	if n := c.Count(ch).Mentions; n != before+1 {
		t.Errorf("@here for a viewer active in Clack: the channel's badge went from %d to %d, want one more", before, n)
	}
	// a burst of presses within the throttle: one tickle
	time.Sleep(300 * time.Millisecond)
	n := len(fs.Tickles())
	for range 5 {
		e.click(pt)
	}
	time.Sleep(150 * time.Millisecond)
	if got := len(fs.Tickles()) - n; got != 1 {
		t.Errorf("%d tickles for five presses within the throttle, want one", got)
	}
	// nothing done in Clack's window, the system in use: the desktop
	// poll's tickles keep the viewer active
	setIdle(0)
	time.Sleep(2 * window)
	if !slices.Contains(fs.Tickles(), "polling") {
		t.Errorf("no polling tickle while the system had input (the gateway heard %q)", fs.Tickles())
	}
	if away() {
		t.Error("the viewer went away while the system had input and the desktop poll ran")
	}
	// the screen locked: no tickle, and the server's auto-away comes
	setLocked(true)
	e.until("away while the screen is locked", 10*time.Second, away)
	ts2, ev2 := fs.PostQuiet(ch, fs.UserIDs()[3], "<!here> a synthetic heads-up while away")
	fs.Push(ev2)
	e.until("the away @here's decision line", 10*time.Second, func() bool { return line(ts2) != "" })
	if l := line(ts2); !strings.HasPrefix(l, "notify: skip 1 ") {
		t.Errorf("@here for a viewer the server marked away: %q, want skip 1 (NotifyMention: not while away)", l)
	}
}
