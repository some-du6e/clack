package ui

import (
	"os"
	"strings"
	"testing"
	"time"

	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/idleprobe"
)

// TestIdleTrafficFrames leaves the window alone under a busy workspace's
// socket about nothing on screen (idleTraffic: a status change every 50
// ms, renames, presence, messages in a channel that is already unread,
// unchanged badge counts, a channel the viewer isn't in renamed, deletes,
// reactions, threads' read states, other people's DND, new emoji, typing
// elsewhere), through the fake's socket, for 5 s: in a channel with its
// composer focused, then on Activity. Slack draws nothing for any of it.
// So the window may draw the caret's blinks, and no frame an Invalidate
// asked for, and core builds no sidebar snapshot (Karim's Clack drew a
// frame for each, 10% of his GPU on Hack Club's grid, 2026-11-16).
// CLACK_IDLE_FRAMES=1: ci's linux-idle and windows-idle jobs.
func TestIdleTrafficFrames(t *testing.T) {
	if os.Getenv("CLACK_IDLE_FRAMES") == "" {
		t.Skip("CLACK_IDLE_FRAMES not set")
	}
	p := idleprobe.New()
	defer p.Close()
	invalidateHook = func(a *App) { idleTrafficHook(a, p) }
	defer func() { invalidateHook = nil }()

	sz := fakeslack.Small
	sz.Unread = 0
	sz.ThreadHeader = true
	sz.GroupDMs = true
	e := newIdleEnv(t, sz, -1)
	idleTrafficApp.Store(e.a)
	ch := fakeslack.ThreadPublicID
	e.openChannel(ch)
	e.until("history", 20*time.Second, func() bool { return e.loadedMessages(ch) > 0 })
	e.focusComposer(ch, "")
	tr := newIdleTraffic(e, func(raw map[string]any) { e.fs.Push(raw) }) // through the fake's socket
	tr.prepare()
	if tr.ch == "" || tr.dm.ID == "" || tr.mpim.ID == "" {
		t.Fatalf("traffic without its channel (%q), DM partner (%q) or group DM member (%q)", tr.ch, tr.dm.ID, tr.mpim.ID)
	}
	t.Logf("traffic: messages in %s from %s, status changes of DM partner %s and group DM member %s, renames of %s", tr.ch, tr.sender, tr.dm.ID, tr.mpim.ID, tr.other)
	// the warm-up round's effects (the first unread message, the first
	// copies of each profile, the counts refresh after the thread events)
	// land before the measuring
	e.settle(3 * time.Second)
	if c := e.a.model().conv(tr.ch); c == nil || !c.Unread {
		t.Fatalf("%s isn't unread after its first message: %+v", tr.ch, c)
	}

	run := func(name string, caret bool) {
		t.Helper()
		e.settle(time.Second)
		b0 := e.a.data.core.SnapshotBuilds()
		tr.start()
		const d = 5 * time.Second
		res := p.Run(name, d, &e.r, e.idleFrame)
		events := tr.stop()
		builds := e.a.data.core.SnapshotBuilds() - b0
		res.Log(t)
		blinks := res.CountMatching("Editor).Layout")
		var asked []string
		frames := 0
		for _, f := range res.Frames {
			inv := false
			for _, c := range f.Causes {
				if strings.HasPrefix(c, "Invalidate ") {
					inv = true
					asked = append(asked, "\t"+c)
				}
			}
			if inv {
				frames++
			}
		}
		t.Logf("idle-traffic %s: %d events in %v: %d frames (%.1f a second), %d the caret's, %d asked for by an Invalidate; %d snapshot builds",
			name, events, d, res.Count(), float64(res.Count())/d.Seconds(), blinks, frames, builds)
		if frames > 0 {
			if len(asked) > 12 {
				asked = asked[:12]
			}
			t.Errorf("%s: %d frames in %v of traffic about nothing on screen were asked for by an Invalidate, want 0:\n%s", name, frames, d, strings.Join(asked, "\n"))
		}
		if builds > 0 {
			t.Errorf("%s: core built %d sidebar snapshots for traffic that changes no sidebar, want 0", name, builds)
		}
		maxBlinks := 0
		if blink := widget.CaretBlinkInterval(); caret && blink > 0 {
			maxBlinks = int(d/blink) + 1
		}
		if blinks > maxBlinks {
			t.Errorf("%s: %d caret frames in %v, want at most %d", name, blinks, d, maxBlinks)
		}
		if other := res.Count() - blinks - frames; other > 2 { // a clock-minute tick or two (relative times)
			t.Errorf("%s: %d timer frames besides the caret's, want at most 2", name, other)
		}
	}
	run("channel, composer focused", true)
	e.a.Navigate("activity")
	e.frame()
	e.settle(3 * time.Second)
	run("activity", false)
}
