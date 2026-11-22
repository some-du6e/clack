package popups

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The desktop shell's image and link menus (Slack 4.53.276
// main.bundle.cjs, the same in the macOS, Windows and Linux builds) end
// with addToggleFullScreen: a separator and "Exit Full Screen" while the
// window is full screen, nothing otherwise.
func TestShellMenusExitFullScreen(t *testing.T) {
	h := &tzHost{th: theme.New(false)}
	at := image.Pt(10, 10)
	full, exited := false, 0
	setShellFullScreen(func() bool { return full }, func() { exited++ })
	t.Cleanup(func() { setShellFullScreen(nil, nil) })
	if got := ctxLabels(NewImageMenu(h, at, "https://example.com/a.png", false)); got != "Copy Image|Copy Image URL" {
		t.Errorf("windowed image menu %q", got)
	}
	full = true
	m := NewImageMenu(h, at, "https://example.com/a.png", false)
	if got := ctxLabels(m); got != "Copy Image|Copy Image URL|-|Exit Full Screen" {
		t.Fatalf("full screen image menu %q", got)
	}
	m.Items()[3].Do()
	if exited != 1 {
		t.Errorf("Exit Full Screen left full screen %d times", exited)
	}
	if got := ctxLabels(NewLinkMenu(h, at, LinkMenu{URL: "https://example.com/", Image: "https://example.com/a.png"})); got != "Copy Link|Open Link|-|Copy Image|Copy Image URL|-|Exit Full Screen" {
		t.Errorf("full screen link menu %q", got)
	}
}

// lbEnv lays out a Lightbox on a real router over an image cache that
// serves every image as a 900x600 PNG.
type lbEnv struct {
	t   *testing.T
	h   *tzHost
	lb  *Lightbox
	r   input.Router
	ops op.Ops
	now time.Time
}

func newLBEnv(t *testing.T, img richtext.Image, m core.Message) *lbEnv {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 900, 600))); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	serve := func(context.Context, string) ([]byte, error) { return data, nil }
	e := &lbEnv{t: t, h: &tzHost{th: theme.New(false), im: imgcache.New(serve, "", nil)}, now: time.Now()}
	e.lb = NewLightbox(e.h, img, m)
	deadline := time.Now().Add(10 * time.Second)
	for {
		e.frame()
		if _, ok, _ := lbShown(e.h.im, e.lb.cur()); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the viewer's image never loaded")
		}
		time.Sleep(10 * time.Millisecond)
	}
	e.frame()
	return e
}

func (e *lbEnv) frame() {
	e.ops.Reset()
	e.now = e.now.Add(20 * time.Millisecond)
	gtx := layout.Context{Ops: &e.ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(1200, 900)),
		Now: e.now, Source: e.r.Source()}
	e.lb.Layout(gtx)
	e.r.Frame(&e.ops)
}

// rightClickMiddle right-clicks the window's middle (the image's).
func (e *lbEnv) rightClickMiddle() {
	p := f32.Pt(600, 450)
	e.r.Queue(
		pointer.Event{Kind: pointer.Move, Position: p, Source: pointer.Mouse, Time: time.Second},
		pointer.Event{Kind: pointer.Press, Position: p, Source: pointer.Mouse, Buttons: pointer.ButtonSecondary, Time: time.Second + 10*time.Millisecond},
		pointer.Event{Kind: pointer.Release, Position: p, Source: pointer.Mouse, Time: time.Second + 60*time.Millisecond},
	)
	for k := 0; k < 3; k++ {
		e.frame()
	}
}

func (e *lbEnv) menus() []*Menu {
	e.h.mu.Lock()
	defer e.h.mu.Unlock()
	var out []*Menu
	for _, o := range e.h.shown {
		if m, ok := o.(*Menu); ok {
			out = append(out, m)
		}
	}
	return out
}

// The image viewer's image is ImageViewer's <img> (p-image_viewer__image),
// with no context menu of its own: a right-click is the desktop shell's
// image menu. Copy Image URL copies the <img>'s src: a file's image, or
// for an image that isn't a file its image.src through Slack's image
// proxy.
func TestLightboxImageMenu(t *testing.T) {
	const shot = "https://bucket.s3.example.com/shots/a.png"
	unfurl := core.Message{Channel: "C1", TS: "1762700000.000200", Doc: richtext.Doc{Blocks: []richtext.Block{
		{Kind: richtext.AttachmentBlock, Att: &richtext.Attachment{ID: "1", Legacy: true, FromURL: shot, OriginalURL: shot,
			Image: &richtext.Image{URL: shot, W: 900, H: 600}}},
	}}}
	e := newLBEnv(t, richtext.Image{URL: shot, W: 900, H: 600}, unfurl)
	e.rightClickMiddle()
	ms := e.menus()
	if len(ms) != 1 {
		t.Fatalf("a right-click on the viewer's image opened %d menus, want 1", len(ms))
	}
	if got := ctxLabels(ms[0]); got != "Copy Image|Copy Image URL" {
		t.Fatalf("viewer image menu %q", got)
	}
	want := "https://slack-imgs.com/?c=1&o1=ro&url=" + url.QueryEscape(shot)
	if got := emClip(ms[0].Items()[1]); got != want {
		t.Errorf("Copy Image URL copied %q, want image.src %q", got, want)
	}
	if e.lb.dragging {
		t.Error("the right-click started a drag")
	}

	file := &richtext.File{ID: "F0TEST0001", Name: "photo.png", Title: "photo.png", Mime: "image/png", IsImage: true,
		URL: "https://files.slack.com/files-pri/T1-F0TEST0001/photo.png", Permalink: "https://fake.slack.com/files/U1/F0TEST0001/photo.png",
		Thumb: &richtext.Image{URL: "https://files.slack.com/files-tmb/T1-F0TEST0001-abc/photo_360.png", W: 360, H: 240, Proxy: true}}
	fm := core.Message{Channel: "C1", TS: "1762700000.000300", Doc: richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.FileBlock, File: file}}}}
	e = newLBEnv(t, *file.Thumb, fm)
	e.rightClickMiddle()
	ms = e.menus()
	if len(ms) != 1 {
		t.Fatalf("a right-click on a file's image opened %d menus, want 1", len(ms))
	}
	if got := emClip(ms[0].Items()[1]); got != file.URL {
		t.Errorf("file: Copy Image URL copied %q, want the file's image %q", got, file.URL)
	}
}

// Copy Image puts the image itself on the clipboard: a fakeslack image,
// downloaded through core and decoded, is what the clipboard write gets.
func TestCopyImageFakeslack(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	a := core.New(slack.New(fs.Creds()), store.New(""))
	h := &tzHost{a: a, th: theme.New(false)}
	src := fs.URL + "img/64x48/" + fakeslack.TeamID + "/party/00000000000000ab.png"
	var mu sync.Mutex
	var got image.Image
	prev := copyWrite
	t.Cleanup(func() { copyWrite = prev })
	copyWrite = func(_ context.Context, img image.Image, _ func(int64) error) error {
		mu.Lock()
		got = img
		mu.Unlock()
		return nil
	}
	m := NewImageMenu(h, image.Pt(10, 10), src, true)
	m.Items()[0].Do()
	deadline := time.Now().Add(10 * time.Second)
	for {
		Pump()
		mu.Lock()
		g := got
		mu.Unlock()
		if g != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Copy Image wrote nothing (toasts %q)", h.toastList())
		}
		time.Sleep(10 * time.Millisecond)
	}
	want := fetchPNG(t, src)
	if got.Bounds() != want.Bounds() {
		t.Fatalf("copied a %v image, want %v", got.Bounds(), want.Bounds())
	}
	for _, p := range []image.Point{{0, 0}, {31, 20}, {63, 47}} {
		r1, g1, b1, a1 := got.At(p.X, p.Y).RGBA()
		r2, g2, b2, a2 := want.At(p.X, p.Y).RGBA()
		if r1 != r2 || g1 != g2 || b1 != b2 || a1 != a2 {
			t.Errorf("pixel %v copied %v, want %v", p, got.At(p.X, p.Y), want.At(p.X, p.Y))
		}
	}
	if got := emClip(m.Items()[1]); got != src {
		t.Errorf("Copy Image URL copied %q, want %q", got, src)
	}
}

// fetchPNG downloads and decodes the PNG at u.
func fetchPNG(t *testing.T, u string) image.Image {
	t.Helper()
	resp, err := http.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(http.DetectContentType(b), "image/png") {
		t.Fatal(errors.New("not a PNG: " + http.DetectContentType(b)))
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return img
}
