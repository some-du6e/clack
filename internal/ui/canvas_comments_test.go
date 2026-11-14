package ui

import (
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
	"github.com/some-du6e/clack/internal/ui/views"
)

// commentShot saves the secondary pane (CANVAS_SHOTS_DIR, with a GPU).
func (e *perfEnv) commentShot(t *testing.T, name string) {
	t.Helper()
	dir := os.Getenv("CANVAS_SHOTS_DIR")
	if dir == "" || e.gpu == nil {
		return
	}
	img := image.NewRGBA(image.Rectangle{Max: e.size})
	if err := e.gpu.Screenshot(img); err != nil {
		t.Fatalf("screenshot: %v", err)
	}
	r := image.Rectangle{Min: e.a.secondaryOrigin, Max: e.size}
	if w := e.a.secondary.drawnW; w > 0 {
		r.Max.X = min(e.size.X, r.Min.X+w)
	}
	out := image.NewRGBA(image.Rectangle{Max: r.Size()})
	draw.Draw(out, out.Bounds(), img, r.Min, draw.Src)
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, out); err != nil {
		t.Fatal(err)
	}
}

func (e *perfEnv) commentStream(t *testing.T) *views.CanvasComments {
	t.Helper()
	var s *views.CanvasComments
	e.until("the comment stream", 10*time.Second, func() bool {
		v, ok := e.a.secondary.view.(*views.CanvasComments)
		s = v
		return ok && v.Loaded()
	})
	return s
}

// The whole client on the fake Slack: the canvas's "Show all threads"
// opens Slack's comment stream in the secondary pane (Most recent: open
// threads by their latest reply, the unread one first), the filter
// switches it, a row opens the comment thread with the stream as its back
// stack, whose header resolves and re-opens it (quip.thread.archive /
// unarchive) and whose root is the highlighted text; "Hide all threads"
// closes the stream. Light and dark shots.
func TestCanvasComments(t *testing.T) {
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		t.Run(mode, func(t *testing.T) {
			fs, err := fakeslack.Start(fakeslack.Small)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { fs.Close() })
			e := startEnv(t, fs, t.TempDir())
			if os.Getenv("CANVAS_SHOTS_DIR") != "" && e.gpu == nil {
				w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
				if err != nil {
					t.Fatalf("headless window: %v", err)
				}
				e.gpu = w
			}
			e.waitReady()
			e.setDark(dark)
			e.a.OpenCanvasFull(fakeslack.CanvasChannel)
			e.settle(500 * time.Millisecond)
			if e.a.CanvasCommentsOpen(fakeslack.CanvasChannel) {
				t.Fatal("the stream is open before Show all threads")
			}
			e.a.ToggleCanvasComments(fakeslack.CanvasChannel) // More actions: Show all threads
			s := e.commentStream(t)
			if !e.a.CanvasCommentsOpen(fakeslack.CanvasChannel) || s.Title() != "All threads" {
				t.Fatalf("open %v, title %q", e.a.CanvasCommentsOpen(fakeslack.CanvasChannel), s.Title())
			}
			want := []string{fakeslack.CommentUnread, fakeslack.CommentOpen, fakeslack.CommentReacted}
			var got []string
			for _, r := range s.Roots() {
				got = append(got, r.TS)
			}
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Fatalf("roots %v, want %v", got, want)
			}
			e.settle(400 * time.Millisecond) // avatars and names
			for _, k := range append(want, "load-more-"+fakeslack.CommentOpen, "1762510200.000300") {
				if s.RowRect(k).Empty() {
					t.Errorf("row %s isn't drawn", k)
				}
			}
			if !s.RowRect("load-more-" + fakeslack.CommentReacted).Empty() {
				t.Error("Show more replies on a thread with one reply")
			}
			e.commentShot(t, "canvas-comments-stream-"+mode)

			// the filter menu: Unread
			e.click(rectMid(s.FilterRect()))
			e.settle(200 * time.Millisecond)
			e.commentShot(t, "canvas-comments-filter-"+mode)
			e.key("Escape", 0)
			e.frame()
			s.SetFilter(core.CommentsUnread)
			s = e.commentStream(t)
			if len(s.Roots()) != 1 || s.Roots()[0].TS != fakeslack.CommentUnread || s.Title() != "Unread comments" {
				t.Fatalf("Unread: %d roots, title %q", len(s.Roots()), s.Title())
			}
			s.SetFilter(core.CommentsLatest)
			s = e.commentStream(t)

			// a row opens the thread over the stream
			e.settle(200 * time.Millisecond)
			e.click(rectMid(s.RowRect(fakeslack.CommentOpen)))
			e.until("the comment thread", 10*time.Second, func() bool {
				return e.a.secondary.kind == secThread && e.a.secondary.ts == fakeslack.CommentOpen && e.a.secondary.channel == fakeslack.FileChanLaunch
			})
			var tvShown bool
			e.until("Resolve thread", 15*time.Second, func() bool {
				tv := e.threadView(fakeslack.FileChanLaunch, fakeslack.CommentOpen)
				tvShown = tv != nil && tv.ResolveShown()
				if tvShown {
					r, _ := tv.ResolveRect()
					return !r.Empty()
				}
				return false
			})
			e.settle(400 * time.Millisecond)
			e.commentShot(t, "canvas-comments-thread-"+mode)
			tv := e.threadView(fakeslack.FileChanLaunch, fakeslack.CommentOpen)
			r, reopen := tv.ResolveRect()
			if reopen {
				t.Fatal("an open thread offers Re-open thread")
			}
			e.click(rectMid(r))
			e.until("quip.thread.archive", 10*time.Second, func() bool { return len(fs.CommentCalls()) == 1 })
			if c := fs.CommentCalls()[0]; c.Get("method") != "quip.thread.archive" || c.Get("channel_id") != fakeslack.FileChanLaunch || c.Get("thread_ts") != fakeslack.CommentOpen {
				t.Fatalf("resolve sent %v", c)
			}
			e.until("Re-open thread", 10*time.Second, func() bool { _, ro := tv.ResolveRect(); return ro })
			e.commentShot(t, "canvas-comments-resolved-"+mode)
			r, _ = tv.ResolveRect()
			e.click(rectMid(r))
			e.until("quip.thread.unarchive", 10*time.Second, func() bool { return len(fs.CommentCalls()) == 2 })
			if c := fs.CommentCalls()[1]; c.Get("method") != "quip.thread.unarchive" {
				t.Fatalf("re-open sent %v", c)
			}

			// Back returns to the stream; Hide all threads closes it
			e.a.navSrc = srcSecondary // as while the pane lays out its header
			if !e.a.PaneBack() {
				t.Fatal("the comment thread has no back stack (FlexpaneBackButton)")
			}
			e.a.GoPaneBack()
			e.a.navSrc = srcPrimary
			e.frame()
			if _, ok := e.a.secondary.view.(*views.CanvasComments); !ok {
				t.Fatalf("back from the thread shows %T", e.a.secondary.view)
			}
			e.a.ToggleCanvasComments(fakeslack.CanvasChannel) // Hide all threads
			e.frame()
			if e.a.CanvasCommentsOpen(fakeslack.CanvasChannel) {
				t.Error("Hide all threads left the stream open")
			}
		})
	}
}

// The comment stream's empty states, per filter.
func TestCanvasCommentsEmpty(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	e.a.OpenCanvasFull(fakeslack.CanvasShared) // no file channel: no threads
	e.settle(300 * time.Millisecond)
	e.a.ToggleCanvasComments(fakeslack.CanvasShared)
	s := e.commentStream(t)
	if len(s.Roots()) != 0 {
		t.Fatalf("%d roots on a canvas without comments", len(s.Roots()))
	}
}
