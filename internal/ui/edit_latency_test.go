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
// Replies to an open thread: thread_reply_latency_test.go.

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
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/richview"
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

// latDraw is one change (an edit, a reply) and when it was first drawn.
type latDraw struct {
	text    string
	pushed  time.Time // the server sent it over the socket
	pushedN int64     // frames drawn before it was sent
	core    time.Time // core announced it (its "update" or "message" event)
	drawn   time.Time // the first frame drawing it
	frames  int64     // frames from its sending to that one
}

// latStats logs a phase's changes and fails those drawn more than two
// frames after the server sent them (the socket's and the hub's goroutine
// hops can miss the frame in progress), or never; it returns their
// median latency. Counted from the socket, not from core's announcement:
// core holding an event back (a lookup on the event pump) is lateness
// too.
func latStats(t *testing.T, mode, name string, ds []latDraw) time.Duration {
	t.Helper()
	var lat []time.Duration
	late, missed := 0, 0
	var all []string
	for i := range ds {
		d := &ds[i]
		switch {
		case d.drawn.IsZero():
			missed++
			all = append(all, "missed")
		default:
			lat = append(lat, d.drawn.Sub(d.pushed))
			if d.frames > 2 {
				late++
			}
			s := fmt.Sprintf("%dms/%df", d.drawn.Sub(d.pushed).Milliseconds(), d.frames)
			if !d.core.IsZero() {
				s += fmt.Sprintf("(core +%dms)", d.core.Sub(d.pushed).Milliseconds())
			}
			all = append(all, s)
		}
	}
	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	q := func(p float64) time.Duration {
		if len(lat) == 0 {
			return 0
		}
		return lat[min(len(lat)-1, int(p*float64(len(lat))))]
	}
	t.Logf("%s, %s: %d changes, drawn %d (late %d, never %d), socket to frame median %v p90 %v max %v; each: %s",
		mode, name, len(ds), len(lat), late, missed, q(0.5).Round(time.Millisecond), q(0.9).Round(time.Millisecond), q(1).Round(time.Millisecond), strings.Join(all, " "))
	if missed > 0 || late > 0 {
		t.Errorf("%s, %s: %d of %d changes drawn more than two frames after the socket brought them, %d never: Slack draws each in the render after it", mode, name, late, len(ds), missed)
	}
	return q(0.5)
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
	fs.PostReplyQuiet(ch, ts, fakeslack.SelfID, "on it, checking 10router") // Karim replied in its thread: he follows it
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

	// what each frame draws: labels (theme.DrawHook: the plan card's title
	// is one) and message bodies (richview.DrawnHook: a reply's text), and
	// whether the app asked for a frame since the last one
	var dmu sync.Mutex
	var labels, bodies []string
	theme.DrawHook = func(s string) {
		dmu.Lock()
		labels = append(labels, strings.TrimSpace(s))
		dmu.Unlock()
	}
	richview.DrawnHook = func(d richtext.Doc) {
		s := richtext.PlainText(d)
		dmu.Lock()
		bodies = append(bodies, s)
		dmu.Unlock()
	}
	t.Cleanup(func() { theme.DrawHook, richview.DrawnHook = nil, nil })
	var asked atomic.Bool
	prevHook := invalidateHook
	invalidateHook = func(*App) { asked.Store(true) }
	t.Cleanup(func() { invalidateHook = prevHook })

	var frames atomic.Int64
	var lastFrame time.Time
	const vsync = time.Second / 60
	frame := func() (ls, bs []string) {
		if w := time.Until(lastFrame.Add(vsync)); w > 0 {
			time.Sleep(w)
		}
		lastFrame = time.Now()
		dmu.Lock()
		labels, bodies = labels[:0], bodies[:0]
		dmu.Unlock()
		e.frame()
		frames.Add(1)
		dmu.Lock()
		ls, bs = append([]string(nil), labels...), append([]string(nil), bodies...)
		dmu.Unlock()
		return ls, bs
	}
	has := func(strs []string, s string) bool {
		for _, x := range strs {
			if strings.Contains(x, s) {
				return true
			}
		}
		return false
	}
	// setup: frames (every frame) until cond holds on what one drew
	until := func(what string, cond func(ls, bs []string) bool) {
		t.Helper()
		for dl := time.Now().Add(20 * time.Second); ; time.Sleep(time.Millisecond) {
			if ls, bs := frame(); cond(ls, bs) {
				return
			}
			if time.Now().After(dl) {
				t.Fatalf("timed out waiting for %s", what)
			}
		}
	}

	const prefix = "10router blipped · "
	title := func(k int) string { return fmt.Sprintf(prefix+"%dm %ds", k/60, k%60) }
	n := 0 // the edits so far
	// newestTitle is whether a frame's labels draw edit k's title and no
	// older one: every surface showing the message is at k
	newestTitle := func(ls []string, k int) bool {
		at := false
		for _, l := range ls {
			if !strings.HasPrefix(l, prefix) {
				continue
			}
			if l != title(k) {
				return false // a surface still draws another (older) title
			}
			at = true
		}
		return at
	}

	// core's announcements: an edit's "update" by its plan's title
	evs, stop := c.Subscribe()
	defer stop()
	var emu sync.Mutex
	announced := map[string]time.Time{}
	note := func(k string) {
		emu.Lock()
		if _, ok := announced[k]; !ok {
			announced[k] = time.Now()
		}
		emu.Unlock()
	}
	go func() {
		for ev := range evs {
			if ev.Type == "update" && ev.Msg != nil && ev.Msg.TS == ts {
				var bs []struct {
					Title string `json:"title"`
				}
				if json.Unmarshal(ev.Msg.Blocks, &bs) == nil && len(bs) > 0 {
					note(bs[0].Title)
				}
			}
		}
	}()

	// watch runs frames as the window asks for them (an Invalidate, the
	// router's wake-up) for d, plus up to 3 s for the last change, making
	// a change every every; drawn reports whether a frame drew change i
	watch := func(d, every time.Duration, change func(i int) (text, key string), drawn func(ls, bs []string, i int) bool) []latDraw {
		t.Helper()
		var ds []latDraw
		var keys []string
		next := time.Now()
		end := next.Add(d)
		for {
			now := time.Now()
			if !now.Before(end) && (len(ds) == 0 || !ds[len(ds)-1].drawn.IsZero() || now.Sub(ds[len(ds)-1].pushed) > 3*time.Second) {
				break
			}
			if now.Before(end) && !now.Before(next) {
				pn := frames.Load()
				text, key := change(len(ds))
				ds = append(ds, latDraw{text: text, pushed: now, pushedN: pn})
				keys = append(keys, key)
				next = next.Add(every)
			}
			wake, ok := e.r.WakeupTime()
			if !asked.Swap(false) && !(ok && !wake.After(now)) {
				time.Sleep(time.Millisecond)
				continue
			}
			ls, bs := frame()
			emu.Lock()
			for i := range ds {
				dd := &ds[i]
				if dd.core.IsZero() {
					if at, ok := announced[keys[i]]; ok {
						dd.core = at
					}
				}
				if dd.drawn.IsZero() && drawn(ls, bs, i) {
					dd.drawn, dd.frames = lastFrame, frames.Load()-dd.pushedN
				}
			}
			emu.Unlock()
		}
		return ds
	}
	// edits every 500 ms for 10 s: each drawn when a frame draws its title
	// and no older one
	edits := func(name string) {
		t.Helper()
		first := n + 1
		ds := watch(10*time.Second, 500*time.Millisecond, func(int) (string, string) {
			n++
			if !fs.EditUptime(ts, title(n)) {
				t.Fatal("the fake lost the message")
			}
			return title(n), title(n)
		}, func(ls, _ []string, i int) bool { return newestTitle(ls, first+i) })
		latStats(t, mode, name, ds)
	}
	// the window watched, not used: no caret blinking in a focused
	// composer draws frames the changes didn't ask for
	settleBlurred := func() {
		e.settle(500 * time.Millisecond)
		e.blur()
		asked.Store(false)
	}

	// the channel alone
	e.openChannel(ch)
	until("the channel", func(ls, _ []string) bool { return has(ls, title(0)) })
	settleBlurred()
	edits("channel")

	// the thread pane alone: its root, the primary on another channel
	e.unblur()
	e.openChannel(other)
	e.a.OpenThread(ch, ts)
	until("the thread pane's root", func(ls, _ []string) bool { return newestTitle(ls, n) })
	settleBlurred()
	edits("thread root")

	// both: the channel with its thread open beside it (Karim's)
	e.unblur()
	e.openChannel(ch)
	e.a.OpenThread(ch, ts)
	until("the channel and the thread pane", func(ls, _ []string) bool {
		tv := e.threadView(ch, ts)
		if tv == nil || !newestTitle(ls, n) {
			return false
		}
		if _, _, ok := tv.RowText(ts); !ok {
			return false
		}
		l, ok := e.a.convoFor(ch, "").list.(channelList)
		if !ok {
			return false
		}
		for _, row := range l.VisibleRows() {
			if row.Key == ts {
				return true
			}
		}
		return false
	})
	settleBlurred()
	edits("channel and thread root")
}
