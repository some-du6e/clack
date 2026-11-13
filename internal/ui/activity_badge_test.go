package ui

import (
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// Karim, Windows, 2026-11-12: "no activity even do i do have". Slack's
// rail shows a "1" on Activity; Clack's showed nothing. Under
// activity_inbox Slack's tab reads 3x6A getActivityBadgeCount, the sum
// of client.counts' activity_v2 over the badged entry types without
// message_reaction (H), and badge_counts_updated replaces it (37DU
// handleBadgeCountsUpdatedRTMEvent). Clack's rail summed the channels'
// mention_count instead, so thread replies (thread_v2) never showed.

func badgeEnv(t *testing.T, counts map[string]int) *perfEnv {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	fs.SetActivityBadges(counts)
	e := startEnv(t, fs, t.TempDir())
	if os.Getenv("ACTIVITY_SHOTS_DIR") != "" && e.gpu == nil {
		if w, err := pixeltest.NewWindow(e.size.X, e.size.Y); err == nil {
			e.gpu = w
		} else {
			t.Logf("no GPU renderer (%v): no shots", err)
		}
	}
	e.waitReady()
	// the account's (fakeslack's experiments.getByUser doesn't list it)
	e.a.Core().SetExperimentGroup("activity_inbox", "on")
	e.until("the inbox rail", 5*time.Second, func() bool { return e.a.model().Rail.ActivityInbox })
	return e
}

func activityBadge(e *perfEnv) int {
	n, dot := e.a.railBadge("activity")
	if dot {
		e.tb.Errorf("the Activity tab draws a dot (Slack's rail has a count there: InjD useGetMentions)")
	}
	return n
}

// injectBadges is a badge_counts_updated with the fake's counts (the
// server sends its own; the fake keeps them for the next client.counts).
func injectBadges(e *perfEnv, counts map[string]int) {
	e.fs.SetActivityBadges(counts)
	raw, _ := json.Marshal(map[string]any{"type": "badge_counts_updated", "activity_v2": e.fs.ActivityBadges()})
	e.a.Core().Inject(slack.Event{Type: "badge_counts_updated", Raw: raw})
}

func TestActivityRailBadgeLikeSlack(t *testing.T) {
	// the real account's shape at the time of the report: one @mention,
	// one @channel, four thread replies (counts only)
	e := badgeEnv(t, map[string]int{"at_user": 1, "at_channel": 1, "thread_v2": 4, "message_reaction": 3})
	e.until("the boot's activity_v2", 5*time.Second, func() bool { return activityBadge(e) == 6 })
	if n := activityBadge(e); n != 6 {
		t.Fatalf("rail Activity badge %d, want 6 (at_user + at_channel + thread_v2, no reactions)", n)
	}

	// badge_counts_updated moves it with no client.counts answer
	injectBadges(e, map[string]int{"thread_v2": 1})
	e.until("badge_counts_updated", 5*time.Second, func() bool { return activityBadge(e) == 1 })

	// a thread reply only: activity_v2 moves while no conversation's
	// count does (a client.counts answer after thread_marked, the same
	// call as the 20 s poll without the socket)
	calls := e.fs.CallCount("client.counts")
	e.fs.SetActivityBadges(map[string]int{"thread_v2": 2, "dm": 1})
	raw, _ := json.Marshal(map[string]any{"type": "thread_marked"})
	e.a.Core().Inject(slack.Event{Type: "thread_marked", Raw: raw})
	e.until("client.counts' activity_v2", 10*time.Second, func() bool {
		return e.fs.CallCount("client.counts") > calls && activityBadge(e) == 3
	})

	// marking an unread item read takes it off at once
	// (decrementBadgeCounts), and off the server's counts
	it := core.ActivityItem{Key: "thread-key", Type: "thread_v2", FeedTS: "1762500000.000100", Unread: true}
	if err := e.a.Core().MarkActivityRead(e.a.data.ctx, it); err != nil {
		t.Fatal(err)
	}
	e.until("the optimistic decrement", 5*time.Second, func() bool { return activityBadge(e) == 2 })
	if got := e.fs.ActivityBadges()["thread_v2"]; got != 1 {
		t.Errorf("fake thread_v2 %d after activity.markRead, want 1", got)
	}
	// a read item doesn't count twice
	it.Unread = false
	if err := e.a.Core().MarkActivityRead(e.a.data.ctx, it); err != nil {
		t.Fatal(err)
	}
	e.frame()
	if n := activityBadge(e); n != 2 {
		t.Errorf("badge %d after marking a read item read, want 2", n)
	}

	// all read: no badge, no dot
	injectBadges(e, map[string]int{})
	e.until("all read", 5*time.Second, func() bool { return activityBadge(e) == 0 })

	// the Badge's maxCount 99 (S1+c Ex)
	for n, want := range map[int]string{1: "1", 9: "9", 10: "10", 99: "99", 100: "99+", 250: "99+"} {
		if got := railCountText(n); got != want {
			t.Errorf("railCountText(%d) = %q, want %q", n, got, want)
		}
	}
}

// The badge on the rail in light and dark with 1, 9, 10 and 100 (99+):
// each draws in both modes (the pixels by the Activity tile differ from
// the rail with none), and ACTIVITY_SHOTS_DIR gets the rail's crops.
func TestActivityRailBadgeShots(t *testing.T) {
	if os.Getenv("ACTIVITY_SHOTS_DIR") == "" {
		t.Skip("ACTIVITY_SHOTS_DIR is unset (the activity-shots workflow sets it)")
	}
	dir := os.Getenv("ACTIVITY_SHOTS_DIR")
	e := badgeEnv(t, map[string]int{})
	if e.gpu == nil {
		t.Skip("no GPU renderer")
	}
	e.settle(500 * time.Millisecond)
	rail := image.Rect(0, 0, 120, 420)
	grab := func() *image.RGBA {
		e.frame()
		e.frame()
		img := image.NewRGBA(image.Rectangle{Max: e.size})
		if err := e.gpu.Screenshot(img); err != nil {
			t.Fatal(err)
		}
		return img
	}
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		e.setDark(dark)
		injectBadges(e, map[string]int{})
		e.until("no badge", 5*time.Second, func() bool { return activityBadge(e) == 0 })
		none := grab()
		for _, n := range []int{1, 9, 10, 100} {
			injectBadges(e, map[string]int{"thread_v2": n})
			e.until(fmt.Sprintf("badge %d", n), 5*time.Second, func() bool { return activityBadge(e) == n })
			img := grab()
			if d := pxDiff(none, img, rail); d < 40 {
				t.Errorf("%s, %d: %d pixels of the rail moved with the badge: not drawn", mode, n, d)
			}
			saveCrop(t, img, rail, filepath.Join(dir, fmt.Sprintf("rail-activity-badge-%d-%s.png", n, mode)))
		}
	}
	e.setDark(false)
}

// pxDiff counts the pixels of r that differ between a and b.
func pxDiff(a, b *image.RGBA, r image.Rectangle) int {
	n := 0
	r = r.Intersect(a.Bounds()).Intersect(b.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			i := a.PixOffset(x, y)
			if a.Pix[i] != b.Pix[i] || a.Pix[i+1] != b.Pix[i+1] || a.Pix[i+2] != b.Pix[i+2] {
				n++
			}
		}
	}
	return n
}

func saveCrop(t *testing.T, img *image.RGBA, r image.Rectangle, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img.SubImage(r)); err != nil {
		t.Fatal(err)
	}
}
