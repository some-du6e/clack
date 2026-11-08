package canvasview

// Synthetic fixtures only (scraping policy): the canvases come from
// internal/fakeslack, made by hand.

import (
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/canvas"
	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
	"github.com/some-du6e/yoloslackgoclient/internal/imgcache"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

type testHost struct {
	a      *core.App
	th     *theme.Theme
	img    *imgcache.Cache
	mu     sync.Mutex
	routes []string
	toasts []string
}

func (h *testHost) Core() *core.App                               { return h.a }
func (h *testHost) Theme() *theme.Theme                           { return h.th }
func (h *testHost) Images() *imgcache.Cache                       { return h.img }
func (h *testHost) Invalidate()                                   {}
func (h *testHost) OpenProfile(anchor image.Point, userID string) {}
func (h *testHost) OpenLightbox(img richtext.Image, m core.Message) {
}
func (h *testHost) Navigate(r string) { h.mu.Lock(); h.routes = append(h.routes, r); h.mu.Unlock() }
func (h *testHost) Toast(s string)    { h.mu.Lock(); h.toasts = append(h.toasts, s); h.mu.Unlock() }

func newEnv(t *testing.T, dark bool) (*testHost, *fakeslack.Server) {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	a := core.New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(a.Shutdown)
	if err := a.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	h := &testHost{a: a, th: theme.New(dark)}
	h.img = imgcache.New(func(ctx context.Context, u string) ([]byte, error) {
		b, _, err := a.Fetch(ctx, u)
		return b, err
	}, "", nil)
	return h, fs
}

func gtxFor(ops *op.Ops, sz image.Point) layout.Context {
	return layout.Context{Ops: ops, Now: time.Now(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(sz)}
}

type shooter struct {
	t   *testing.T
	dir string
	win *headless.Window
	sz  image.Point
}

func newShooter(t *testing.T, sz image.Point) *shooter {
	s := &shooter{t: t, dir: os.Getenv("CANVAS_SHOTS_DIR"), sz: sz}
	if s.dir != "" {
		w, err := headless.NewWindow(sz.X, sz.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		s.win = w
		t.Cleanup(w.Release)
	}
	return s
}

func (s *shooter) frame(w func(gtx layout.Context)) *op.Ops {
	ops := new(op.Ops)
	w(gtxFor(ops, s.sz))
	return ops
}

func (s *shooter) shot(name string, ops *op.Ops) {
	if s.win == nil {
		return
	}
	if err := s.win.Frame(ops); err != nil {
		s.t.Fatalf("frame: %v", err)
	}
	img := image.NewRGBA(image.Rectangle{Max: s.sz})
	if err := s.win.Screenshot(img); err != nil {
		s.t.Fatalf("screenshot: %v", err)
	}
	f, err := os.Create(filepath.Join(s.dir, name+".png"))
	if err != nil {
		s.t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		s.t.Fatal(err)
	}
}

// settle draws frames until cond holds.
func settle(t *testing.T, s *shooter, v *View, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		s.frame(func(gtx layout.Context) { v.Layout(gtx) })
		time.Sleep(10 * time.Millisecond)
	}
}

// Every block type of the kitchen-sink canvas lays out, headings are
// sized like Slack's, and the three modes draw.
func TestRenderKitchenSink(t *testing.T) {
	for _, dark := range []bool{false, true} {
		h, _ := newEnv(t, dark)
		suffix := "light"
		if dark {
			suffix = "dark"
		}
		for _, m := range []struct {
			mode Mode
			name string
			sz   image.Point
		}{{Full, "full", image.Pt(1100, 900)}, {Side, "side", image.Pt(560, 900)}, {Tab, "tab", image.Pt(900, 700)}} {
			var canvasOpened string
			act := Actions{Canvas: func(id string) { canvasOpened = id }, FullView: func() {}, SideView: func() {}, Close: func() {}}
			v := New(h, fakeslack.CanvasChannel, m.mode, act)
			s := newShooter(t, m.sz)
			settle(t, s, v, "the canvas", func() bool { return v.Doc() != nil })
			d := v.Doc()
			if d.Info.Title != "Launch plan" {
				t.Fatalf("title %q", d.Info.Title)
			}
			// a few frames for images and emoji to decode
			for i := 0; i < 40; i++ {
				s.frame(func(gtx layout.Context) { v.Layout(gtx) })
				time.Sleep(15 * time.Millisecond)
			}
			if len(v.blocks) != len(d.Doc.Blocks) {
				t.Fatalf("%d blocks laid out for %d", len(v.blocks), len(d.Doc.Blocks))
			}
			s.shot("canvas-"+m.name+"-"+suffix, s.frame(func(gtx layout.Context) { v.Layout(gtx) }))

			// every block draws with a height; headings by size
			heights := map[canvas.Kind]int{}
			ops := new(op.Ops)
			gtx := gtxFor(ops, image.Pt(760, 4000))
			for i, b := range v.blocks {
				g := gtx
				g.Constraints = layout.Constraints{Min: image.Pt(700, 0), Max: image.Pt(700, 4000)}
				dim := v.layoutBlock(g, b, i == 0)
				if dim.Size.Y <= 0 && b.b.Kind != canvas.Paragraph {
					t.Errorf("block %d (kind %d) has no height", i, b.b.Kind)
				}
				if _, ok := heights[b.b.Kind]; !ok {
					heights[b.b.Kind] = dim.Size.Y - space(g, b.b.Kind, i == 0)
				}
			}
			if !(heights[canvas.Heading1] > heights[canvas.Heading2] && heights[canvas.Heading2] > heights[canvas.Heading3]) {
				t.Errorf("heading heights %v", heights)
			}
			if heights[canvas.Table] < 3*bodyLine || heights[canvas.Code] < 3*18 {
				t.Errorf("table %d code %d", heights[canvas.Table], heights[canvas.Code])
			}
			// links: another canvas opens natively, messages route
			v.handler.OpenURL("https://fake.slack.com/docs/T0FAKE0001/F0CANVAS002")
			if canvasOpened != "F0CANVAS002" {
				t.Errorf("canvas link opened %q", canvasOpened)
			}
			v.handler.OpenURL("https://fake.slack.com/archives/C0FAKE00003/p1762500000000100")
			h.mu.Lock()
			if len(h.routes) == 0 || h.routes[len(h.routes)-1] != "c/C0FAKE00003/t/1762500000.000100" {
				t.Errorf("routes %v", h.routes)
			}
			h.mu.Unlock()
		}
	}
}

// The whole document in one tall shot (CI artifact for review).
func TestRenderTallShot(t *testing.T) {
	if os.Getenv("CANVAS_SHOTS_DIR") == "" {
		t.Skip("CANVAS_SHOTS_DIR not set")
	}
	for _, dark := range []bool{false, true} {
		h, _ := newEnv(t, dark)
		v := New(h, fakeslack.CanvasChannel, Full, Actions{})
		s := newShooter(t, image.Pt(900, 2600))
		settle(t, s, v, "the canvas", func() bool { return v.Doc() != nil })
		for i := 0; i < 60; i++ {
			s.frame(func(gtx layout.Context) { v.Layout(gtx) })
			time.Sleep(15 * time.Millisecond)
		}
		name := "canvas-tall-light"
		if dark {
			name = "canvas-tall-dark"
		}
		s.shot(name, s.frame(func(gtx layout.Context) { v.Layout(gtx) }))
	}
}

// Live: another user's edit (document_update over the socket) shows up
// without a reload; files.open was sent for the viewer.
func TestLiveUpdate(t *testing.T) {
	h, fs := newEnv(t, false)
	evs, stop := h.a.Subscribe()
	defer stop()
	h.a.SetRealtime(true)
	deadline := time.After(5 * time.Second)
	for up := false; !up; {
		select {
		case ev := <-evs:
			up = ev.Type == "connection" && ev.Connected
		case <-deadline:
			t.Fatal("no socket")
		}
	}
	v := New(h, fakeslack.CanvasChannel, Tab, Actions{})
	s := newShooter(t, image.Pt(900, 700))
	settle(t, s, v, "the canvas", func() bool { return v.Doc() != nil })
	settle(t, s, v, "files.open", func() bool { fc, _ := fs.Canvas(fakeslack.CanvasChannel); return fc.Opened == 1 })
	fs.EditCanvasAsOther(fakeslack.CanvasChannel, "## Added live")
	settle(t, s, v, "the live edit", func() bool {
		d := v.Doc()
		return d != nil && len(d.Doc.Blocks) > 0 && d.Doc.Blocks[len(d.Doc.Blocks)-1].Text() == "Added live"
	})
	settle(t, s, v, "the rebuilt blocks", func() bool {
		return len(v.blocks) > 0 && v.blocks[len(v.blocks)-1].b.Kind == canvas.Heading2
	})
	// someone else opens it: their face joins the header
	fs.ViewCanvasAsOther(fakeslack.CanvasChannel, "U0FAKE00002", true)
	settle(t, s, v, "the viewer facepile", func() bool { return len(h.a.CanvasViewers(fakeslack.CanvasChannel)) == 1 })
	for i := 0; i < 20; i++ {
		s.frame(func(gtx layout.Context) { v.Layout(gtx) })
		time.Sleep(15 * time.Millisecond)
	}
	s.shot("canvas-live-update", s.frame(func(gtx layout.Context) { v.Layout(gtx) }))
	fs.ViewCanvasAsOther(fakeslack.CanvasChannel, "U0FAKE00002", false)
	settle(t, s, v, "the viewer leaving", func() bool { return len(h.a.CanvasViewers(fakeslack.CanvasChannel)) == 0 })
}

func TestCanvasLinkID(t *testing.T) {
	for in, want := range map[string]string{
		"https://hackclub.slack.com/docs/T0266FRGM/F0ABCDEF12":     "F0ABCDEF12",
		"https://app.slack.com/docs/T0266FRGM/F0ABCDEF12?x=1":      "F0ABCDEF12",
		"https://hackclub.slack.com/canvas/F0ABCDEF12":             "F0ABCDEF12",
		"https://hackclub.slack.com/archives/C1/p1700000000000100": "",
		"https://evil.example/docs/T1/F0ABCDEF12":                  "",
		"http://hackclub.slack.com/docs/T1/F0ABCDEF12":             "",
		"https://hackclub.slack.com/docs/T1/../F0ABCDEF12":         "",
	} {
		if got := CanvasLinkID(in); got != want {
			t.Errorf("CanvasLinkID(%s) = %q, want %q", in, got, want)
		}
	}
	if ordinal(2, 0) != "2" || ordinal(2, 1) != "b" || ordinal(4, 2) != "iv" || ordinal(27, 1) != "aa" {
		t.Error("ordinal")
	}
	if !strings.Contains(ago(time.Now().Add(-3*time.Hour)), "3 hours") {
		t.Error("ago")
	}
}
