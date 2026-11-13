package ui

// The thread pane's header in the whole window against internal/
// fakeslack's thread header fixtures (synthetic; nothing talks to Slack).
// A thread opened from Activity (what the Activity item's "Open" does)
// names its conversation after "Thread", the channel resolved through
// core when the viewer's conversations don't hold it; clicking the name
// goes to the channel at the root (c/<id>/m/<root>), the thread pane
// staying open on it and the name hiding. With THREAD_HEADER_SHOTS_DIR
// set (the thread-header workflow) each conversation kind is saved,
// window and pane, light and dark, plus the group DM's tooltip.

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

func threadHeaderEnv(t *testing.T, dark bool) *perfEnv {
	t.Helper()
	sz := fakeslack.Small
	sz.ThreadHeader = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	if os.Getenv("THREAD_HEADER_SHOTS_DIR") != "" {
		if e.gpu != nil {
			e.gpu.Release()
		}
		w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	e.a.Core().SetExperimentGroup("mpdm_naming", "on") // the named group DM's name (on for the account)
	if dark {
		e.a.setDark(true)
	}
	return e
}

// threadView is the open thread pane's list.
func (e *perfEnv) threadView(ch, ts string) *messages.ThreadView {
	if c := e.a.convos[ch+"/"+ts]; c != nil {
		if l, ok := c.list.(threadList); ok {
			return l.ThreadView
		}
	}
	return nil
}

// waitThreadHeader runs frames until the open thread's header names want
// as a link, failing on a frame that draws a name without text.
func (e *perfEnv) waitThreadHeader(want fakeslack.ThreadHeaderCase) image.Rectangle {
	t := e.tb
	t.Helper()
	var got messages.HeaderSubtitle
	var shown bool
	var r image.Rectangle
	dl := time.Now().Add(20 * time.Second)
	for {
		e.frame()
		if tv := e.threadView(want.Channel, fakeslack.ThreadRootTS); tv != nil {
			got, r, shown = tv.HeaderName()
			if shown && (got.Text == "" || got.Text == "#") {
				t.Fatalf("%s: the header drew %+v (a placeholder)", want.Name, got)
			}
			if shown && got.Link && got.Text == want.Text && got.Icon == want.Icon && got.Tip == want.Tip {
				return r
			}
		}
		if time.Now().After(dl) {
			t.Fatalf("%s: the header shows %+v (shown %v), want %q icon %q tip %q", want.Name, got, shown, want.Text, want.Icon, want.Tip)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// threadHeaderShot saves the window and the thread pane alone.
func (e *perfEnv) threadHeaderShot(name string) {
	dir := os.Getenv("THREAD_HEADER_SHOTS_DIR")
	if dir == "" || e.gpu == nil {
		return
	}
	e.settle(600 * time.Millisecond)
	img := image.NewRGBA(image.Rectangle{Max: e.size})
	if err := e.gpu.Screenshot(img); err != nil {
		e.tb.Fatalf("screenshot: %v", err)
	}
	save := func(name string, img image.Image) {
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			e.tb.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			e.tb.Fatal(err)
		}
	}
	save(name, img)
	if e.a.secondary.kind == secThread {
		w := e.a.secondary.drawnW // the auto width is 0
		save(name+"-pane", img.SubImage(image.Rect(e.size.X-w, 0, e.size.X, e.size.Y)))
	}
}

func TestThreadHeaderFromActivity(t *testing.T) {
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		t.Run(mode, func(t *testing.T) {
			e := threadHeaderEnv(t, dark)
			other := e.fs.ChannelIDs()[0]
			for i, c := range e.fs.ThreadHeaderCases() {
				// the Activity page, or another channel, in the primary view
				if i%2 == 0 {
					e.a.Navigate("activity")
				} else {
					e.openChannel(other)
				}
				e.a.OpenThread(c.Channel, fakeslack.ThreadRootTS)
				r := e.waitThreadHeader(c)
				e.threadHeaderShot("thread-header-" + c.Name + "-" + mode)
				if c.Tip != "" { // the group DM's members, on hover
					p := r.Min.Add(r.Size().Div(2))
					e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(p.X), float32(p.Y))})
					e.settle(900 * time.Millisecond)
					e.threadHeaderShot("thread-header-" + c.Name + "-tip-" + mode)
				}
				e.clickAt(r.Min.Add(r.Size().Div(2)), time.Duration(i+1)*time.Second)
				want := Route("c/" + c.Channel + "/m/" + fakeslack.ThreadRootTS)
				e.until(c.Name+": the click navigates", 5*time.Second, func() bool { return e.a.router.Current() == want })
				if e.a.secondary.kind != secThread || e.a.secondary.channel != c.Channel || e.a.secondary.ts != fakeslack.ThreadRootTS {
					t.Errorf("%s: the thread pane didn't stay open (secondary %v %s/%s)", c.Name, e.a.secondary.kind,
						e.a.secondary.channel, e.a.secondary.ts)
				}
				e.settle(300 * time.Millisecond)
				if tv := e.threadView(c.Channel, fakeslack.ThreadRootTS); tv != nil {
					if sub, _, ok := tv.HeaderName(); ok {
						t.Errorf("%s: the header still names %+v in its own channel", c.Name, sub)
					}
				}
				if i == 0 {
					e.threadHeaderShot("thread-header-after-click-" + mode)
				}
			}
		})
	}
}

// Karim (karim-1113c): "clicking # clack up there doesnt bring u to the
// correct msg". The thread header's name is the root's permalink: the
// channel (or DM) opens scrolled to the root and flashes it, the thread
// pane staying open. A conversation already mounted in the background,
// last left at its newest message and with messages that arrived since,
// rebuilt on the jump and its scroll anchoring put it back at the old
// spot, nowhere near the root.
func TestThreadHeaderJumpsToRoot(t *testing.T) {
	e := threadHeaderEnv(t, false)
	other := e.fs.ChannelIDs()[0]
	users := e.fs.UserIDs()
	for i, c := range e.fs.ThreadHeaderCases() {
		if c.Channel == other {
			continue
		}
		// plenty of newer messages, so the root is off screen at the newest
		for n := 0; n < 40; n++ {
			e.fs.PostQuiet(c.Channel, users[n%len(users)], "a synthetic newer message")
		}
		e.openChannel(c.Channel)
		e.until(c.Name+": the conversation loads", 10*time.Second, func() bool {
			l, ok := e.a.convoFor(c.Channel, "").list.(channelList)
			return ok && len(l.VisibleRows()) > 0
		})
		e.toLatest()
		visible := func() bool {
			l, _ := e.a.convoFor(c.Channel, "").list.(channelList)
			for _, r := range l.VisibleRows() {
				if r.Key == fakeslack.ThreadRootTS {
					return true
				}
			}
			return false
		}
		if visible() {
			t.Fatalf("%s: the root is on screen at the newest message; the test needs it off screen", c.Name)
		}
		// away, while more messages arrive
		e.openChannel(other)
		for n := 0; n < 3; n++ {
			_, ev := e.fs.PostQuiet(c.Channel, users[n%len(users)], "a synthetic message while away")
			e.fs.Push(ev)
		}
		e.settle(300 * time.Millisecond)
		e.a.OpenThread(c.Channel, fakeslack.ThreadRootTS)
		r := e.waitThreadHeader(c)
		e.clickAt(r.Min.Add(r.Size().Div(2)), time.Duration(i+1)*time.Second)
		want := Route("c/" + c.Channel + "/m/" + fakeslack.ThreadRootTS)
		e.until(c.Name+": the click navigates", 5*time.Second, func() bool { return e.a.router.Current() == want })
		e.until(c.Name+": the root on screen", 10*time.Second, visible)
		e.settle(500 * time.Millisecond)
		if !visible() {
			t.Errorf("%s: the root scrolled away after the jump", c.Name)
		}
		if s := e.a.secondary; s.kind != secThread || s.channel != c.Channel || s.ts != fakeslack.ThreadRootTS {
			t.Errorf("%s: the thread pane didn't stay open (secondary %v %s/%s)", c.Name, s.kind, s.channel, s.ts)
		}
	}
}
