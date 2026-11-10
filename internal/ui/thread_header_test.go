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
		w := int(e.a.secondary.width)
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
