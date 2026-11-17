//go:build linux || windows || (darwin && cgo)

package ui

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/app"
	"gioui.org/io/event"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/imgcache/webptest"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// TestRealWindowQuietFrames is quiet frames (quiet.go) on Clack's real
// window: App.Run's own app.Window and driver, the X11 one under Xvfb on
// Linux (CLACK_REAL_WINDOW_TESTS with a DISPLAY, ci's linux-gio job), Metal
// on macOS (CLACK_MACOS_WINDOW_TESTS, ci's macos job; TestMain runs
// app.Main) and Direct3D on Windows (CLACK_REAL_WINDOW_TESTS, ci's
// windows-idle job). Signed in to internal/fakeslack, the open channel
// shows an animated custom emoji (webptest.Explodes, :explodes:'s shape: 13
// frames of 30 to 70 ms) whose frames reach the window as they do on
// Karim's machines: the image cache's animation repaint, the invalidator,
// Window.Invalidate, the driver, a FrameEvent. Gio used to say the window
// had asked for every frame an Invalidate brought (FrameEvent.Asked, Gio
// patch 35), so App.Run laid out every one of them: no quiet frame ever
// happened in a real window, only in tests that model it.
//
// For 6 s it counts the frames the window drew, the layouts (FrameSeq), the
// quiet frames (QuietFrames), the emoji's frames and why each laid-out
// frame wasn't quiet (quietWhy): most frames must be quiet, the emoji must
// play in them, and none may be laid out for a reason that says the last
// frame can't be shown again. Then a message arriving must lay the window
// out. The log has each frame's cost, quiet and laid out (frameStats).
// Synthetic data only.
func TestRealWindowQuietFrames(t *testing.T) {
	if on, why := realWindowGate(); !on {
		t.Skip(why)
	}
	sz := fakeslack.Small
	sz.Unread = 0 // the channel opens at its newest message
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	name, u := fs.FixtureEmoji()
	if name == "" || !strings.HasPrefix(u, fs.URL+"img/") {
		t.Fatalf("the fake's custom emoji %q is at %q", name, u)
	}
	path := strings.TrimPrefix(u, fs.URL+"img/")
	data := webptest.Explodes()
	fs.SetImages(func(p string) ([]byte, string, bool) {
		if p == path {
			return data, "image/png", true // a WebP behind a .png URL, as Slack serves :explodes:
		}
		return nil, "", false
	})
	home := t.TempDir()
	t.Setenv("CLACK_HOME", home)
	cache := filepath.Join(home, "cache")

	// what the window drew, from App.Run's own goroutine (OnWindowEvent
	// runs there, before each event is handled): every FrameEvent, those
	// the window's frames asked for, and why the frame before each one
	// wasn't quiet
	var mu sync.Mutex
	var measuring bool
	var frames, asked int
	whys := map[string]int{}
	var a *App
	images := imgcache.New(fs.Fetch, "", nil)
	a = New(Options{ConfigDir: home, NoRealtime: true, Started: time.Now(), OnWindowEvent: func(e event.Event) {
		realWindowEvent(e)
		fe, ok := e.(app.FrameEvent)
		if !ok {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if !measuring {
			return
		}
		whys[a.quietWhy]++ // the frame before this one ("" when it was quiet)
		frames++
		if fe.Asked && !fe.AskedAt.After(fe.Now) {
			asked++
		}
	}}, images)
	// a small window: Mesa's llvmpipe draws Clack's 1440×900 in about
	// 400 ms, too few frames to count in a few seconds
	a.prefs.MainWindow = &mainWindowSettings{Size: &winSize{Width: 800, Height: 600}, Bounds: &winRect{X: 60, Y: 60, Width: 800, Height: 600}}
	a.Attach(core.New(slack.New(fs.Creds()), store.New(cache)), cache)
	done := make(chan error, 1)
	go func() { done <- a.Run() }()
	t.Cleanup(func() {
		a.Quit()
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			t.Error("Quit didn't end Run")
		}
	})
	until := func(what string, d time.Duration, ok func() bool) {
		t.Helper()
		for dl := time.Now().Add(d); !ok(); time.Sleep(50 * time.Millisecond) {
			select {
			case err := <-done:
				t.Fatalf("%s: Run returned (%v)", what, err)
			default:
			}
			if time.Now().After(dl) {
				t.Fatalf("timed out waiting for %s", what)
			}
		}
	}
	onUI := func(f func()) {
		t.Helper()
		ran := make(chan struct{})
		a.Post(func() { f(); close(ran) })
		select {
		case <-ran:
		case <-time.After(30 * time.Second):
			t.Fatal("work posted to the UI goroutine didn't run")
		}
	}
	until("core ready", 90*time.Second, func() bool {
		booted := false
		onUI(func() { booted = a.data != nil && !a.data.booted.IsZero() })
		return booted
	})
	realWindowShow(t)
	ch := fs.ChannelIDs()[0]
	onUI(func() { a.Navigate("c/" + ch) })
	inTimeline := func(ts string) func() bool {
		return func() bool {
			found := false
			onUI(func() {
				if tl := a.data.core.S.Timeline(ch); tl != nil {
					for _, m := range tl.Msgs {
						found = found || ts == "" || m.TS == ts
					}
				}
			})
			return found
		}
	}
	until("the channel's history", 60*time.Second, inTimeline(""))
	self := a.Core().Self().ID
	post := func(text string) string {
		ts, ev := fs.PostQuiet(ch, self, text)
		b, _ := json.Marshal(ev)
		chJSON, _ := json.Marshal(ch)
		a.data.core.Inject(slack.Event{Type: "message", Channel: chJSON, Raw: b})
		return ts
	}
	ts := post("boom :" + name + ": boom")
	until("the message", 30*time.Second, inTimeline(ts))
	toLatest := func() { // the channel's newest message on screen
		onUI(func() {
			if v, ok := a.convoFor(ch, "").list.(interface{ ScrollToBottom() }); ok {
				v.ScrollToBottom()
			}
		})
	}
	toLatest()
	time.Sleep(time.Second)
	toLatest() // again once the row is laid out (its height known)
	for dl := time.Now().Add(60 * time.Second); images.Stats().Players == 0; time.Sleep(100 * time.Millisecond) {
		if time.Now().After(dl) {
			t.Fatalf("the emoji never played: its image loaded %v, an animation at 22/32/44 px %v/%v/%v; %d layouts so far; %+v",
				images.Loaded(u), images.IsAnimation(u, 22), images.IsAnimation(u, 32), images.IsAnimation(u, 44), a.FrameSeq(), images.Stats())
		}
	}
	time.Sleep(3 * time.Second) // loads, the deferred boot work, opening animations

	var st0, st1 frameStats
	onUI(func() { st0 = a.frameStats })
	mu.Lock()
	measuring, frames, asked = true, 0, 0
	clear(whys)
	mu.Unlock()
	quiet0, laid0, adv0 := a.QuietFrames(), a.FrameSeq(), images.Stats().Advances
	time.Sleep(6 * time.Second)
	quiet, laid, adv := int(a.QuietFrames()-quiet0), int(a.FrameSeq()-laid0), int(images.Stats().Advances-adv0)
	onUI(func() { st1 = a.frameStats })
	mu.Lock()
	measuring = false
	n, nAsked := frames, asked
	reasons := make([]string, 0, len(whys))
	for r, k := range whys {
		if r == "" {
			r = "(quiet)"
		}
		reasons = append(reasons, r+": "+strconv.Itoa(k))
	}
	why := map[string]int{}
	for r, k := range whys {
		why[r] = k
	}
	mu.Unlock()
	sort.Strings(reasons)
	t.Logf("6 s with the emoji playing: %d frames drawn (%d asked for by the window's frames), %d quiet, %d layouts, %d animation frames shown; the frames before them: %s",
		n, nAsked, quiet, laid, adv, strings.Join(reasons, ", "))
	if st1.n >= st0.n && st1.quiet >= st0.quiet { // frameStats didn't start over (CLACK_FRAMESTATS logs every 5 s)
		nq, nf := st1.quiet-st0.quiet, (st1.n-st1.quiet)-(st0.n-st0.quiet)
		tq, tf := st1.qtotal-st0.qtotal, (st1.total-st1.qtotal)-(st0.total-st0.qtotal)
		t.Logf("App.Run's time for them, GPU submit included: %d quiet frames, %v each; %d laid out, %v each (layout %v of it)",
			nq, (tq / time.Duration(max(nq, 1))).Round(10*time.Microsecond), nf,
			(tf / time.Duration(max(nf, 1))).Round(10*time.Microsecond), ((st1.layout - st0.layout) / time.Duration(max(nf, 1))).Round(10*time.Microsecond))
	}
	if n < 8 {
		t.Fatalf("the window drew %d frames in 6 s of an animation: it isn't drawing", n)
	}
	// against the frames the window didn't ask for itself (the caret's
	// blinks lay out at a fixed rate, so a slow software rasterizer would
	// otherwise fail the ratio with quiet frames working)
	if quiet < 5 || 2*quiet < n-nAsked {
		t.Errorf("%d of %d frames were quiet (%d laid out, %d asked for by the window's frames): a frame only the animation asked for is still laid out in a real window", quiet, n, laid, nAsked)
	}
	if 2*adv < quiet {
		t.Errorf("%d animation frames shown in %d quiet frames: the emoji doesn't play in them", adv, quiet)
	}
	for _, r := range []string{whyNotShowable, whyResized, whyOtherWindow, whySignInInspect} {
		if k := why[r]; k > 0 {
			t.Errorf("%d frames laid out because %q: none should be here", k, r)
		}
	}

	// a message arriving lays the window out, among the emoji's quiet frames
	laid0 = a.FrameSeq()
	ts = post("and a message after it")
	until("the second message", 30*time.Second, inTimeline(ts))
	until("a frame laid out for it", 10*time.Second, func() bool { return a.FrameSeq() > laid0 })
}
