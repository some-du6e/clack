package ui

// A known gap, tracked: a row newly shown under a plugin patching the
// messages slice draws the plugins' view from its first frame, as Slack
// renders the patched slice in the render that first shows the message.
// Clack's row draws core's copy until JS's copy of it is ready
// (messages.applyView's viewWait: one JS round trip), so a channel opened,
// or a thread pane, under a text-marking plugin (Message Logger's kind)
// first paints the unmarked text for a few hundred milliseconds (the pane's
// reply: 218-266 ms, 12-15 frames, in runs 44591888895, 44596411773).
// Pushes run it once (CLACK_SLICE_TEXT, flake-check's edit-latency job):
// the pane opens on three threads, each after the app settled (boot's
// loads landed, the mirror idle), as does the channel before it, and
// each gap is held to sliceFirstDrawWall. The first pane of a session
// drew core's copy for ~650 ms (36-37 paints, runs 44828104385 and
// 44829338850), the next two ~55 ms: its thread's first load brought
// replies the plugins' copy of the conversation lacked and marked nothing
// (setAll marked only messages the model had), so their views waited for
// the mirror's sweep; a median hid it. Marking every new message held a
// channel's first window a JS round trip more, 124 ms settled against 17
// (runs 44844149401 and 44846768356): JS that never read a conversation reads
// it fresh (taut.MessagesAdded). CLACK_SLICE_GAPS (slice-gaps, on request)
// allows no unmarked frame. Synthetic data only.

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/richview"
)

// sliceFirstDrawWall bounds how long a newly shown row draws core's copy
// before the plugins' view on pushes: a JS round trip, the panes ~90 ms
// and the channel 16-113 ms settled. Past it: a thread pane's replies
// waiting for the mirror's sweep (~650 ms), a channel's first window
// marked whole when JS reads it fresh anyway (~290 ms).
const sliceFirstDrawWall = 200 * time.Millisecond

func TestSliceFirstDraw(t *testing.T) {
	strict := os.Getenv("CLACK_SLICE_GAPS") != ""
	if !strict && os.Getenv("CLACK_SLICE_TEXT") == "" {
		t.Skip("CLACK_SLICE_TEXT or CLACK_SLICE_GAPS not set: flake-check's edit-latency job runs it once, slice-gaps strictly")
	}
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	chs, ids := fs.ChannelIDs(), fs.UserIDs()
	ch := chs[2]
	const rootText = "a synthetic first-draw root"
	// three threads, a pane opened on each: the median of their gaps
	type thread struct{ root, reply string }
	var threads []thread
	for i := 0; i < 3; i++ {
		root, _ := fs.PostQuiet(ch, ids[2], fmt.Sprintf("%s %d", rootText, i))
		reply := fmt.Sprintf("a synthetic first-draw reply %d", i)
		fs.PostReplyQuiet(ch, root, ids[3], reply)
		threads = append(threads, thread{root, reply})
	}

	e := benchStart(t, fs).perfEnv
	e.waitReady()
	e.until("Taut", 60*time.Second, func() bool { return e.a.Taut() != nil })
	tt := e.a.Taut()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, err := tt.Import(ctx, []byte(reactSliceText), ""); err != nil {
		t.Fatal(err)
	}
	if err := tt.SetEnabled(ctx, "ReactSliceText", true); err != nil {
		t.Fatal(err)
	}
	e.until("the plugin's patch", 30*time.Second, func() bool { return tt.SlicePatched("messages") })

	// what the paints of a text were, over every frame drawn (toLatest's
	// too): the first unmarked one, the first marked one, how many paints
	// drew it unmarked
	type track struct {
		unmarked, marked time.Time
		plain            int
	}
	var mu sync.Mutex
	tracks := map[string]*track{}
	richview.DrawnHook = func(d richtext.Doc) {
		s := richtext.PlainText(d)
		mu.Lock()
		defer mu.Unlock()
		for text, tr := range tracks {
			switch {
			case strings.Contains(s, text+reactLogged):
				if tr.marked.IsZero() {
					tr.marked = time.Now()
				}
			case strings.Contains(s, text) && tr.marked.IsZero():
				if tr.unmarked.IsZero() {
					tr.unmarked = time.Now()
				}
				tr.plain++
			}
		}
	}
	t.Cleanup(func() { richview.DrawnHook = nil })
	follow := func(text string) {
		mu.Lock()
		tracks[text] = &track{}
		mu.Unlock()
	}
	// settle runs frames until text drew marked (or 20 s), then reports
	// the gap: how long core's copy drew before the plugins' view
	settle := func(what, text string) time.Duration {
		t.Helper()
		var tr track
		for dl := time.Now().Add(20 * time.Second); time.Now().Before(dl); time.Sleep(16 * time.Millisecond) {
			e.frame()
			mu.Lock()
			tr = *tracks[text]
			mu.Unlock()
			if !tr.marked.IsZero() {
				break
			}
		}
		lag := time.Duration(0)
		if !tr.unmarked.IsZero() && !tr.marked.IsZero() {
			lag = tr.marked.Sub(tr.unmarked)
		}
		t.Logf("first draw, %s: %d paints unmarked over %v before the first marked one (marked: %v)", what, tr.plain, lag.Round(time.Millisecond), !tr.marked.IsZero())
		switch {
		case tr.marked.IsZero():
			t.Errorf("%s: never drawn marked (%d paints drew it unmarked): the plugins' view never showed", what, tr.plain)
		case strict && tr.plain > 0:
			t.Errorf("%s: %d paints drew the message unmarked (core's copy, not the plugins' view) for %v before its first marked one", what, tr.plain, lag.Round(time.Millisecond))
		}
		return lag
	}
	bound := func(what string, lags []time.Duration) {
		t.Helper()
		for i, lag := range lags {
			if lag > sliceFirstDrawWall {
				t.Errorf("%s (%d of %d): drew core's copy for %v before the plugins' view, past %v", what, i+1, len(lags), lag.Round(time.Millisecond), sliceFirstDrawWall)
			}
		}
		sorted := append([]time.Duration(nil), lags...)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
		t.Logf("first draw, %s: median %v of %v", what, sorted[len(sorted)/2].Round(time.Millisecond), lags)
	}

	// the channel's newest message, once boot's loads landed and the
	// mirror is idle
	newest := fmt.Sprintf("%s %d", rootText, len(threads)-1)
	e.settle(2 * time.Second)
	follow(newest)
	e.openChannel(ch)
	e.until("the channel's history", 20*time.Second, func() bool { return e.loadedMessages(ch) > 0 })
	e.toLatest()
	bound("the channel opened", []time.Duration{settle("the channel opened", newest)})
	var lags []time.Duration
	for _, th := range threads {
		// boot's loads landed and the mirror idle, as a pane opens in use
		e.settle(2 * time.Second)
		follow(th.reply)
		e.a.OpenThread(ch, th.root)
		lags = append(lags, settle("the thread pane opened", th.reply))
	}
	bound("the thread pane opened", lags)
}
