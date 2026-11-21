package ui

// Activity as an edit surface: an edit of an Activity entry's message
// draws in the entry's row in the frame after the socket brings it, as
// Slack's Activity row reads the live message from the store
// (messageChanged replaces it there, and the row's selector re-renders).
// Here someone's mention of the viewer is edited every 500 ms over the
// socket with the Activity page open, frames drawn only when the app asks
// for one (Invalidate, the router's wake-up) and the window blurred, so an
// edit that changes Activity's copy but asks for no frame shows as late as
// the next unrelated one. Natively, under a plugin patching the messages
// slice and under one patching the components an Activity row draws
// through (ActivityItem, ActivityThread, the row contents: the row's text
// then comes from a JS tree).
// Synthetic data only. Channel and thread surfaces: edit_latency_test.go.

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// actPassComponents patches what an Activity row draws through (the
// views' lmPatches: ActivityItem, ActivityThread and the row contents) and
// passes it through: the row's text then comes from a JS tree, which can
// still be of the revision before an edit.
const actPassComponents = `(() => {
const TautPlugin = globalThis.TautPlugin
class ActPassComponents extends TautPlugin {
  static id = 'ActPassComponents'
  static pluginName = 'Activity pass components'
  static description = 'Patches the Activity row components, changing nothing'
  static category = 'other'
  static authors = ['test']
  static defaultConfig = { enabled: false }
  start() {
    for (const name of ['ActivityItem', 'ActivityThread', 'ActivityRowContentMultiLine', 'ActivityRowContentDetailed']) {
      this.api.patchComponent(name, (Original) => (props) => React.createElement(Original, props))
    }
  }
  stop() {}
}
return ActPassComponents
})()`

func TestActivityEditLatency(t *testing.T) {
	if os.Getenv("CLACK_LATENCY_TESTS") == "" {
		t.Skip("CLACK_LATENCY_TESTS not set: flake-check's activity-edits job runs this")
	}
	for _, mode := range []string{"native", "slice", "components"} {
		t.Run(mode, func(t *testing.T) { activityEditLatency(t, mode) })
	}
}

func activityEditLatency(t *testing.T, mode string) {
	sz := fakeslack.Small
	sz.Views = true // Activity's feed, and messages.list for the entry's message
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	ch, author := fs.ChannelIDs()[1], fs.UserIDs()[2]
	// edit k's text, and the part of it the row draws whatever the
	// mention reads as
	text := func(k int) string {
		return fmt.Sprintf("<@%s> the synthetic standup moved to %d:%02d", fakeslack.SelfID, 9+k/60, k%60)
	}
	at := func(k int) string { return fmt.Sprintf("standup moved to %d:%02d", 9+k/60, k%60) }
	ts, _ := fs.PostQuiet(ch, author, text(0))

	var e *perfEnv
	switch mode {
	case "native":
		e = startEnv(t, fs, t.TempDir())
	default:
		e = benchStart(t, fs).perfEnv
	}
	e.waitReady()
	c := e.a.data.core
	c.SetRealtime(true)
	e.until("the socket", 20*time.Second, func() bool { return c.RTM != nil && c.RTM.Connected() })
	if mode != "native" {
		e.until("Taut", 60*time.Second, func() bool { return e.a.Taut() != nil })
		tt := e.a.Taut()
		code, id := editPassSlice, "EditPassSlice"
		if mode == "components" {
			code, id = actPassComponents, "ActPassComponents"
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if _, err := tt.Import(ctx, []byte(code), ""); err != nil {
			t.Fatal(err)
		}
		if err := tt.SetEnabled(ctx, id, true); err != nil {
			t.Fatal(err)
		}
		e.until("the plugin's patches", 30*time.Second, func() bool {
			if mode == "slice" {
				return tt.SlicePatched("messages")
			}
			return slackc.Patched("ActivityItem") && slackc.Patched("ActivityRowContentDetailed")
		})
	}

	// what each frame draws: labels and message bodies, and whether the
	// app asked for a frame since the last one
	var dmu sync.Mutex
	var drawn []string
	theme.DrawHook = func(s string) {
		dmu.Lock()
		drawn = append(drawn, s)
		dmu.Unlock()
	}
	richview.DrawnHook = func(d richtext.Doc) {
		s := richtext.PlainText(d)
		dmu.Lock()
		drawn = append(drawn, s)
		dmu.Unlock()
	}
	t.Cleanup(func() { theme.DrawHook, richview.DrawnHook = nil, nil })
	var asked atomic.Bool
	prevHook := invalidateHook
	invalidateHook = func(*App) { asked.Store(true) }
	t.Cleanup(func() { invalidateHook = prevHook })
	var frames int64
	var lastFrame time.Time
	const vsync = time.Second / 60
	frame := func() []string {
		if w := time.Until(lastFrame.Add(vsync)); w > 0 {
			time.Sleep(w)
		}
		lastFrame = time.Now()
		dmu.Lock()
		drawn = drawn[:0]
		dmu.Unlock()
		e.frame()
		frames++
		dmu.Lock()
		defer dmu.Unlock()
		return append([]string(nil), drawn...)
	}
	// newest reports whether a frame drew edit k's text and no older one
	newest := func(ss []string, k int) bool {
		got := false
		for _, s := range ss {
			for j := 0; j < k; j++ {
				if strings.Contains(s, at(j)) {
					return false
				}
			}
			got = got || strings.Contains(s, at(k))
		}
		return got
	}

	// Activity open, then the entry about the mention comes over the socket
	e.a.Navigate("activity")
	e.settle(2 * time.Second)
	fs.Push(map[string]any{"type": "activity", "subtype": "activity_updated", "entry": map[string]any{
		"key": "act-edit-latency", "feed_ts": ts, "is_unread": true,
		"item": map[string]any{"type": "at_user", "message": map[string]any{"ts": ts, "channel": ch, "author_user_id": author}}}})
	for dl := time.Now().Add(20 * time.Second); !newest(frame(), 0); time.Sleep(time.Millisecond) {
		if time.Now().After(dl) {
			t.Fatal("timed out waiting for the Activity row of the mention")
		}
	}
	e.settle(500 * time.Millisecond)
	// core's deferred boot loads (5 s after boot, an id-less "names") are
	// start-up, not edits
	if d := 8*time.Second - time.Since(e.a.data.booted); d > 0 {
		e.settle(d)
	}
	e.blur()
	asked.Store(false)

	// an edit every 500 ms for 10 s, each drawn when a frame draws its
	// text and no older one
	startFrames := frames
	var ds []latDraw
	n := 0
	next := time.Now()
	end := next.Add(10 * time.Second)
	for {
		now := time.Now()
		if !now.Before(end) && (len(ds) == 0 || !ds[len(ds)-1].drawn.IsZero() || now.Sub(ds[len(ds)-1].pushed) > 3*time.Second) {
			break
		}
		if now.Before(end) && !now.Before(next) {
			n++
			pn := frames
			if !fs.EditLive(ch, ts, text(n)) {
				t.Fatal("the fake lost the message")
			}
			ds = append(ds, latDraw{text: at(n), pushed: now, pushedN: pn})
			next = next.Add(500 * time.Millisecond)
		}
		wake, ok := e.r.WakeupTime()
		if !asked.Swap(false) && !(ok && !wake.After(now)) {
			time.Sleep(time.Millisecond)
			continue
		}
		ss := frame()
		for i := range ds {
			if d := &ds[i]; d.drawn.IsZero() && newest(ss, i+1) {
				d.drawn, d.frames = lastFrame, frames-d.pushedN
			}
		}
	}
	latStats(t, mode, "an Activity row", ds)
	// and with frames drawn only when asked, the first one after an edit is
	// the one it asked for: Slack's row reads the live message, so the edit
	// draws in it, past the odd one a frame already under way missed
	// (natively the first edit, 2 frames). A JS tree of the revision before
	// (components) drew every edit a frame late, within latStats's two
	slow := 0
	for _, d := range ds {
		if !d.drawn.IsZero() && d.frames > 1 {
			slow++
		}
	}
	if slow > len(ds)/4 {
		t.Errorf("%s: %d of %d edits drawn after the first frame following them: the row drew the revision before first", mode, slow, len(ds))
	}
	// the bounds hold only while the page asks for frames for the edits
	// alone: an edit asks for its frame (natively 21 for 20 edits), under a
	// plugin the views turning ready for two more (slice 61, components 60),
	// where a page redrawing on its own draws every edit in time; a few
	// frames over for a stray ask
	t.Logf("%s: %d frames for %d edits", mode, frames-startFrames, len(ds))
	edits := int64(len(ds))
	most := 2 * edits
	if mode != "native" {
		most = 3*edits + 5
	}
	if edits > 0 && frames-startFrames > most {
		t.Errorf("%s: %d frames for %d edits, past %d: the Activity page asks for frames of its own", mode, frames-startFrames, edits, most)
	}
}
