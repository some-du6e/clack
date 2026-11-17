package ui

import (
	"encoding/json"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache/webptest"
	"github.com/some-du6e/clack/internal/slack"
)

// An animated custom emoji on screen (:explodes:, as webptest.Explodes
// replicates it) asks for a frame every 30–70 ms. Such a frame used to lay
// the whole window out (17–23 ms on Karim's Mac with his plugins): every
// row, the sidebar, the composer, for a few hundred pixels of emoji. Now a
// frame only an animation asked for is quiet (quiet.go): the last frame's
// ops again, the emoji's frame drawn as it is now (live ImageOps), no
// layout at all, so no row is laid out again. Anything else that asks for
// a frame (a message arriving, input) still lays the window out.
// Synthetic data only.
func TestAnimationTickIsQuiet(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
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
	var asks atomic.Int32
	fs.SetImages(func(p string) ([]byte, string, bool) {
		if p == path {
			asks.Add(1)
			return data, "image/png", true // a WebP behind a .png URL, as Slack serves :explodes:
		}
		return nil, "", false
	})
	// who asks for frames while the ticks run (Invalidate's callers, the
	// animation repaints included), for the log
	var record atomic.Bool
	var srcMu sync.Mutex
	sources := map[string]int{}
	invalidateHook = func(*App) {
		if !record.Load() {
			return
		}
		pcs := make([]uintptr, 16)
		fr := runtime.CallersFrames(pcs[:runtime.Callers(3, pcs)]) // past Invalidate or animReady
		var names []string
		for len(names) < 4 {
			f, more := fr.Next()
			names = append(names, strings.TrimPrefix(f.Function, "github.com/some-du6e/clack/internal/"))
			if !more {
				break
			}
		}
		srcMu.Lock()
		sources[strings.Join(names, " < ")]++
		srcMu.Unlock()
	}
	t.Cleanup(func() { invalidateHook = nil })
	home := t.TempDir()
	t.Setenv("CLACK_HOME", home)
	e := startEnv(t, fs, home)
	e.waitReady()
	wake := make(chan struct{}, 1)
	onAnimRepaint(e, func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	})
	ch := fs.ChannelIDs()[0]
	e.openChannel(ch)
	e.settle(500 * time.Millisecond)
	self := e.a.Core().Self().ID
	// a message in the fake's history (a history fetch keeps it), and to
	// the client as the socket would bring it
	post := func(text string) string {
		ts, ev := fs.PostQuiet(ch, self, text)
		b, _ := json.Marshal(ev)
		chJSON, _ := json.Marshal(ch)
		e.a.data.core.Inject(slack.Event{Type: "message", Channel: chJSON, Raw: b})
		return ts
	}
	toBottom := func() {
		for i := 0; i < 40; i++ {
			e.scroll(e.listPoint(), 4000)
			e.frame()
		}
	}
	inTimeline := func(ts string) bool {
		if tl := e.a.data.core.S.Timeline(ch); tl != nil {
			for _, m := range tl.Msgs {
				if m.TS == ts {
					return true
				}
			}
		}
		return false
	}
	first := post("boom :" + name + ": boom")
	e.until("the message", 10*time.Second, func() bool { return inTimeline(first) })
	toBottom()
	e.until("the emoji playing", 20*time.Second, func() bool { return e.a.images.Stats().Players > 0 })
	t.Logf("the emoji's bytes asked for %d times; %+v", asks.Load(), e.a.images.Stats())
	e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(-100, -100)}) // off the window
	e.until("the images loaded", 10*time.Second, func() bool { return e.a.images.Pending() == 0 })
	e.focusComposer(ch, "") // the caret in the composer, as when the channel opens
	focus := e.r.Source().FocusTag()
	if focus == nil {
		t.Fatal("nothing focused after focusing the composer")
	}
	e.settle(800 * time.Millisecond)
	e.frame()
	askedAt, asked := e.r.WakeupTime()

	// the ticks, for 1.5 s: a frame whenever something asks for one, as the
	// window's loop draws them: a repaint (the image cache's animation
	// repaint or an Invalidate), or a wake-up the window's frames asked
	// for falling due; none when nothing asks
	laid0, adv0 := e.a.FrameSeq(), e.a.images.Stats().Advances
	record.Store(true)
	quiet, full, lost := 0, 0, 0
	why := map[string]int{} // the laid-out frames, by why they weren't quiet
	for dl := time.Now().Add(1500 * time.Millisecond); time.Now().Before(dl); {
		var due <-chan time.Time
		if asked {
			due = time.After(max(time.Until(askedAt), 0))
		}
		select {
		case <-wake:
		case <-due:
		case <-time.After(time.Until(dl)):
			continue // the end; nothing asked
		}
		if e.quietFrame(asked, askedAt) {
			quiet++
		} else {
			full++
			why[frameWhy(e)]++
		}
		if e.r.Source().FocusTag() != focus {
			lost++
		}
		askedAt, asked = e.r.WakeupTime()
	}
	record.Store(false)
	laid, adv := e.a.FrameSeq()-laid0, e.a.images.Stats().Advances-adv0
	t.Logf("1.5 s of ticks: %d quiet, %d laid out (%d layouts; why: %v), %d animation frames shown", quiet, full, laid, why, adv)
	srcMu.Lock()
	for src, n := range sources {
		t.Logf("  %3d× %s", n, src)
	}
	srcMu.Unlock()
	if quiet < 15 {
		t.Errorf("%d ticks were quiet and %d laid the window out (%d layouts): an animation tick should lay nothing out", quiet, full, laid)
	}
	// a frame is laid out only because something besides an animation
	// asked (an Invalidate, posted work, a due wake-up of the window's,
	// or nothing at all: a repaint whose reasons the frame before took);
	// never for one of the conditions that mean the last frame can't be
	// shown again, none of which holds here
	for _, r := range []string{whyNotShowable, whyResized, whyOtherWindow, whySignInInspect} {
		if n := why[r]; n > 0 {
			t.Errorf("%d frames laid out because %q: none should be here", n, r)
		}
	}
	if lost > 0 {
		t.Errorf("the composer wasn't focused after %d of %d frames: a frame drawn again dropped the focus", lost, quiet+full)
	}
	if int(laid) > 2*full {
		t.Errorf("%d layouts in %d full frames: a quiet one laid out", laid, full)
	}
	if int(adv) < quiet/2 {
		t.Errorf("%d animation frames shown in %d quiet frames: the emoji didn't play in them", adv, quiet)
	}

	// a message arriving lays the window out (and the new row draws)
	laid0 = e.a.FrameSeq()
	ts := post("and a message after it")
	for dl := time.Now().Add(5 * time.Second); !inTimeline(ts) && time.Now().Before(dl); {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond) // the list's model hears of it
	select {
	case <-wake: // and an animation asks too
	case <-time.After(time.Second):
	}
	if e.quietFrame(asked, askedAt) {
		t.Error("the frame after a message arrived was quiet")
	}
	if e.a.FrameSeq() == laid0 {
		t.Error("the frame after a message arrived wasn't laid out")
	}
	askedAt, asked = e.r.WakeupTime()

	// so does input
	select {
	case <-wake:
	case <-time.After(time.Second):
		t.Fatal("no animation repaint for a second")
	}
	p := e.listPoint()
	e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(p.X), float32(p.Y))}) // the pointer comes in
	askedAt, asked = e.r.WakeupTime()
	if e.quietFrame(asked, askedAt) {
		t.Error("the frame after the pointer moved in was quiet")
	}
}
