package ui

// Edits draw in the frame after they land (edit-latency). Karim's uptime
// monitor (2026-11-18, α.108 on Windows, plugins on) posts one plan block
// whose title is the outage and its duration, "10router blipped · 2m 1s",
// and edits it with chat.update as the duration ticks: "this shit updates
// really slowly like every 33 seconds", "it should be like slack". Slack
// draws each message_changed in the render after it (messageChanged
// replaces the message in its store, and every surface reads it from
// there). Here the monitor edits its message every 500 ms over the
// socket, and each surface showing it (the channel's list, the root of
// its open thread pane, both at once) must draw each edit within a frame
// of core taking it in. Frames are drawn as a window draws them: when the
// app asks for one (Invalidate, the router's wake-up), so an edit that
// changes the models but asks for no frame shows as late as the next
// unrelated one. Natively, and under pass-through plugins: one patching
// the messages slice (rows draw the plugins' view of the message), one
// patching the components a message draws through (Blocks, MessageText,
// MessageWrapper: their trees come from JS). Synthetic data only.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// editPassSlice patches the messages slice and passes it through.
const editPassSlice = `(() => {
const TautPlugin = globalThis.TautPlugin
class EditPassSlice extends TautPlugin {
  static id = 'EditPassSlice'
  static pluginName = 'Edit pass slice'
  static description = 'Patches the messages slice, changing nothing'
  static category = 'other'
  static authors = ['test']
  static defaultConfig = { enabled: false }
  start() { this.api.redux.patchSlice('messages', (ch, bucket) => bucket) }
  stop() {}
}
return EditPassSlice
})()`

// editPassComponents patches what a message draws through and passes it
// through.
const editPassComponents = `(() => {
const TautPlugin = globalThis.TautPlugin
class EditPassComponents extends TautPlugin {
  static id = 'EditPassComponents'
  static pluginName = 'Edit pass components'
  static description = 'Patches Blocks, MessageText and MessageWrapper, changing nothing'
  static category = 'other'
  static authors = ['test']
  static defaultConfig = { enabled: false }
  start() {
    for (const name of ['Blocks', 'MessageText', 'MessageWrapper']) {
      this.api.patchComponent(name, (Original) => (props) => React.createElement(Original, props))
    }
  }
  stop() {}
}
return EditPassComponents
})()`

// editDraw is one edit and when each surface first drew it.
type editDraw struct {
	title  string
	pushed time.Time
	core   time.Time // core announced it (its "update" event)
	coreN  int64     // frames drawn before core announced it
	drawn  time.Time // the first frame drawing it as often as wanted
	frames int64     // frames from core's announcement to that one
}

func TestEditLatency(t *testing.T) {
	if os.Getenv("CLACK_LATENCY_TESTS") == "" {
		t.Skip("CLACK_LATENCY_TESTS not set: flake-check's edit-latency job runs this")
	}
	for _, mode := range []string{"native", "slice", "components"} {
		t.Run(mode, func(t *testing.T) { editLatency(t, mode) })
	}
}

func editLatency(t *testing.T, mode string) {
	sz := fakeslack.Small
	sz.AppPlans = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	ch := fakeslack.AppPlanChannelID
	ts := fs.PostUptimeQuiet("10router blipped · 0m 0s")
	fs.PostReplyQuiet(ch, ts, fakeslack.SelfID, "on it") // Karim replied in its thread
	other := fs.ChannelIDs()[0]

	var e *perfEnv
	switch mode {
	case "native":
		e = startEnv(t, fs, t.TempDir())
	default:
		be := benchStart(t, fs)
		e = be.perfEnv
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
			code, id = editPassComponents, "EditPassComponents"
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
			return slackc.Patched("Blocks") && slackc.Patched("MessageWrapper")
		})
	}

	// what each frame draws (theme.DrawHook: the plan card's title is a
	// label), and whether the app asked for a frame since the last one
	var dmu sync.Mutex
	var drawn []string
	theme.DrawHook = func(s string) {
		dmu.Lock()
		drawn = append(drawn, s)
		dmu.Unlock()
	}
	t.Cleanup(func() { theme.DrawHook = nil })
	var asked atomic.Bool
	prevHook := invalidateHook
	invalidateHook = func(*App) { asked.Store(true) }
	t.Cleanup(func() { invalidateHook = prevHook })

	var frames atomic.Int64
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
		frames.Add(1)
		dmu.Lock()
		out := append([]string(nil), drawn...)
		dmu.Unlock()
		return out
	}
	// how many labels a frame drew that read s (the card's title is one
	// line)
	count := func(strs []string, s string) int {
		n := 0
		for _, x := range strs {
			if strings.TrimSpace(x) == s {
				n++
			}
		}
		return n
	}
	// frames (every frame: setup) until the drawn labels read s at least n
	// times, then a second's more; it returns how often a frame draws it
	// then (once per surface showing the message, or more if one draws it
	// twice)
	waitDrawn := func(what, s string, n int) int {
		t.Helper()
		for dl := time.Now().Add(20 * time.Second); ; {
			if count(frame(), s) >= n {
				break
			}
			if time.Now().After(dl) {
				t.Fatalf("%s: %q never drawn %d times", what, s, n)
			}
			time.Sleep(time.Millisecond)
		}
		got := 0
		for dl := time.Now().Add(time.Second); time.Now().Before(dl); time.Sleep(10 * time.Millisecond) {
			got = max(got, count(frame(), s))
		}
		return got
	}
	title := func(k int) string { return fmt.Sprintf("10router blipped · %dm %ds", k/60, k%60) }

	// core's "update" for each edit, by its plan's title
	evs, stop := c.Subscribe()
	defer stop()
	var emu sync.Mutex
	announced := map[string]time.Time{}
	announcedN := map[string]int64{}
	go func() {
		for ev := range evs {
			if ev.Type != "update" || ev.Msg == nil || ev.Msg.TS != ts {
				continue
			}
			var bs []struct {
				Title string `json:"title"`
			}
			if json.Unmarshal(ev.Msg.Blocks, &bs) != nil || len(bs) == 0 {
				continue
			}
			emu.Lock()
			if _, ok := announced[bs[0].Title]; !ok {
				announced[bs[0].Title] = time.Now()
				announcedN[bs[0].Title] = frames.Load()
			}
			emu.Unlock()
		}
	}()

	n := 0
	// phase: edits every 500 ms for 10 s, each wanted drawn want times a
	// frame (one per surface showing the message)
	phase := func(name string, want int) []editDraw {
		t.Helper()
		var eds []editDraw
		next := time.Now()
		end := next.Add(10 * time.Second)
		for time.Now().Before(end) || len(eds) > 0 && eds[len(eds)-1].drawn.IsZero() && time.Since(eds[len(eds)-1].pushed) < 3*time.Second {
			now := time.Now()
			if now.Before(end) && !now.Before(next) {
				n++
				if !fs.EditUptime(ts, title(n)) {
					t.Fatal("the fake lost the message")
				}
				eds = append(eds, editDraw{title: title(n), pushed: now})
				next = next.Add(500 * time.Millisecond)
			}
			wake, ok := e.r.WakeupTime()
			if !asked.Swap(false) && !(ok && !wake.After(now)) {
				time.Sleep(time.Millisecond)
				continue
			}
			strs := frame()
			emu.Lock()
			for i := range eds {
				d := &eds[i]
				if d.core.IsZero() {
					if at, ok := announced[d.title]; ok {
						d.core, d.coreN = at, announcedN[d.title]
					}
				}
				if d.drawn.IsZero() && !d.core.IsZero() && count(strs, d.title) >= want {
					d.drawn, d.frames = lastFrame, frames.Load()-d.coreN
				}
			}
			emu.Unlock()
		}
		// what each edit cost, and the budget: drawn, within two frames of
		// core taking it in (the hub's goroutine hop can miss the frame in
		// progress), as Slack draws it in the render after it
		var lat []time.Duration
		late, missed := 0, 0
		for i := range eds {
			d := &eds[i]
			switch {
			case d.core.IsZero():
				t.Errorf("%s, %s: core never announced edit %q", mode, name, d.title)
			case d.drawn.IsZero():
				missed++
			default:
				lat = append(lat, d.drawn.Sub(d.core))
				if d.frames > 2 {
					late++
				}
			}
		}
		sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
		q := func(p float64) time.Duration {
			if len(lat) == 0 {
				return 0
			}
			return lat[min(len(lat)-1, int(p*float64(len(lat))))]
		}
		var all []string
		for i := range eds {
			d := &eds[i]
			s := "missed"
			if !d.drawn.IsZero() {
				s = fmt.Sprintf("%dms/%df", d.drawn.Sub(d.core).Milliseconds(), d.frames)
			}
			all = append(all, s)
		}
		t.Logf("%s, %s: %d edits, drawn %d (late %d, never %d), event to frame median %v p90 %v max %v; each: %s",
			mode, name, len(eds), len(lat), late, missed, q(0.5).Round(time.Millisecond), q(0.9).Round(time.Millisecond), q(1).Round(time.Millisecond), strings.Join(all, " "))
		if missed > 0 || late > 0 {
			t.Errorf("%s, %s: %d of %d edits drawn more than two frames after core took them in, %d never: Slack draws each in the render after it", mode, name, late, len(eds), missed)
		}
		return eds
	}

	// the window watched, not used: no caret blinking in a focused
	// composer draws frames the edits didn't ask for
	settleBlurred := func() {
		e.settle(500 * time.Millisecond)
		e.blur()
		asked.Store(false)
	}

	// the channel alone
	e.openChannel(ch)
	inList := waitDrawn("the channel", title(0), 1)
	settleBlurred()
	phase("channel", inList)

	// the thread pane alone: its root, the primary on another channel
	e.unblur()
	e.openChannel(other)
	e.a.OpenThread(ch, ts)
	inThread := waitDrawn("the thread pane's root", title(n), 1)
	settleBlurred()
	phase("thread root", inThread)

	// both: the channel with its thread open beside it (Karim's)
	e.unblur()
	e.openChannel(ch)
	e.a.OpenThread(ch, ts)
	waitDrawn("the channel and the thread pane", title(n), inList+inThread)
	settleBlurred()
	phase("channel and thread root", inList+inThread)
}
