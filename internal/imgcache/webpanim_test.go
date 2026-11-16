package imgcache

// Animated WebP (webpanim.go). Synthetic files only (webptest, and the
// x/image test picture in testdata/): nothing here comes from Slack.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/image/webp"

	"github.com/some-du6e/clack/internal/imgcache/webptest"
)

var (
	wRed    = color.NRGBA{255, 0, 0, 255}
	wBlueH  = color.NRGBA{0, 0, 255, 128}
	wGreenH = color.NRGBA{0, 255, 0, 128}
)

// synthWebP: an 8×8 canvas; frame 0 red over rows 0–5 only (50 ms),
// frame 1 a half-transparent blue 4×4 at (2,2) blended over it and
// disposed of (0 ms: 100 ms), frame 2 a half-transparent green 4×4 at
// (0,4) that replaces what's under it (200 ms). libwebp (Pillow 10.2)
// composes it to the pixels TestAnimWebPCompose wants.
func synthWebP(loop int) []byte {
	f1 := webptest.Solid(2, 2, 4, 4, 0, wBlueH)
	f1.Dispose = true
	f2 := webptest.Solid(0, 4, 4, 4, 200, wGreenH)
	f2.NoBlend = true
	return webptest.Anim(8, 8, loop, webptest.Solid(0, 0, 8, 6, 50, wRed), f1, f2)
}

func nearRGBA(a, b color.RGBA, tol int) bool {
	d := func(x, y uint8) bool { return int(x)-int(y) <= tol && int(y)-int(x) <= tol }
	return d(a.R, b.R) && d(a.G, b.G) && d(a.B, b.B) && d(a.A, b.A)
}

func TestAnimWebPIndex(t *testing.T) {
	data := synthWebP(0)
	if !isAnimWebP(data) || !isAnim(data) || isGIF(data) {
		t.Fatal("the synthetic file isn't seen as an animated WebP")
	}
	// the bug: x/image/webp reads only stills, and this used to be all
	// imgcache tried
	if _, err := Decode(data); err == nil {
		t.Log("x/image/webp decodes animated WebP now: webpanim.go may be simpler")
	}
	ix, err := testIndexAnim(data)
	if err != nil {
		t.Fatal(err)
	}
	if !ix.webp || ix.w != 8 || ix.h != 8 || len(ix.frames) != 3 || ix.loop != 0 || ix.prev {
		t.Fatalf("index %dx%d, %d frames, loop %d (webp %v, prev %v)", ix.w, ix.h, len(ix.frames), ix.loop, ix.webp, ix.prev)
	}
	for i, want := range []struct {
		r     image.Rectangle
		delay time.Duration
		blend bool
		disp  byte
	}{
		{image.Rect(0, 0, 8, 6), 50 * time.Millisecond, true, 0},
		{image.Rect(2, 2, 6, 6), 100 * time.Millisecond, true, gif.DisposalBackground},
		{image.Rect(0, 4, 4, 8), 200 * time.Millisecond, false, 0},
	} {
		f := ix.frames[i]
		if f.r != want.r || f.delay != want.delay || f.blend != want.blend || f.disp != want.disp {
			t.Errorf("frame %d: %v %v blend %v disp %d, want %+v", i, f.r, f.delay, f.blend, f.disp, want)
		}
		if f.n <= 0 || f.data+f.n > len(data) || string(data[f.data:f.data+4]) != "VP8L" {
			t.Errorf("frame %d's chunks at %d+%d", i, f.data, f.n)
		}
	}
	// WebP counts passes; gifIndex's loop is GIF's repetitions
	for passes, want := range map[int]int{0: 0, 1: -1, 2: 1, 5: 4} {
		if ix, err := testIndexWebP(synthWebP(passes)); err != nil || ix.loop != want {
			t.Errorf("loop count %d: indexed %v (%v), want %d", passes, ix, err, want)
		}
	}
	if ix, err := testIndexWebP(data[:len(data)-5]); err != nil || len(ix.frames) != 3 { // the last frame cut short
		t.Errorf("cut short: %v", err)
	}
	if _, err := testIndexWebP(webptest.Anim(8, 8, 0)); err == nil {
		t.Error("an animation of no frames indexed")
	}
	if _, err := testIndexWebP(webptest.Anim(20000, 8, 0, webptest.Solid(0, 0, 8, 8, 50, wRed))); !errors.Is(err, ErrTooBig) {
		t.Errorf("a 20000px canvas: %v, want ErrTooBig", err)
	}
}

// TestAnimWebPCompose: each frame is composed as libwebp composes it
// (blend, no-blend, dispose to transparent), and the second pass starts
// from an empty canvas.
func TestAnimWebPCompose(t *testing.T) {
	data := synthWebP(0)
	ix, err := testIndexWebP(data)
	if err != nil {
		t.Fatal(err)
	}
	red := color.RGBA{255, 0, 0, 255}
	mix := color.RGBA{127, 0, 128, 255} // blue at 50% over red (libwebp: 126, 0, 127)
	green := color.RGBA{0, 128, 0, 128} // premultiplied
	none := color.RGBA{}
	want := []map[image.Point]color.RGBA{
		{{0, 0}: red, {7, 5}: red, {0, 7}: none, {7, 7}: none, {3, 3}: red, {2, 4}: red},
		{{0, 0}: red, {1, 1}: red, {3, 3}: mix, {5, 5}: mix, {2, 5}: mix, {6, 6}: none, {7, 3}: red, {1, 5}: red},
		{{0, 0}: red, {3, 3}: none, {5, 5}: none, {5, 3}: none, {2, 5}: green, {1, 7}: green, {3, 7}: green, {2, 4}: green, {7, 3}: red, {6, 7}: none},
	}
	d := testDecoder(t, ix, data)
	for pass := 0; pass < 2; pass++ {
		for i, px := range want {
			if got := d.next1(); got != i || d.werr != nil {
				t.Fatalf("pass %d: composed frame %d (%v), want %d", pass, got, d.werr, i)
			}
			for p, w := range px {
				if g := d.canvas.RGBAAt(p.X, p.Y); !nearRGBA(g, w, 2) {
					t.Errorf("pass %d frame %d: pixel %v %v, want %v", pass, i, p, g, w)
				}
			}
		}
	}
}

// TestAnimWebPLossyAlpha: a lossy (VP8) frame with an ALPH chunk, the
// kind :explodes: is made of, decodes through x/image as a still with
// that alpha, at its offset; a VP8 frame without ALPH is opaque and,
// not blended, replaces its rectangle only.
func TestAnimWebPLossyAlpha(t *testing.T) {
	still, err := os.ReadFile("testdata/blue-purple-pink.lossy.webp")
	if err != nil {
		t.Fatal(err)
	}
	if string(still[12:16]) != "VP8 " {
		t.Fatalf("testdata isn't a simple lossy WebP: %q", still[12:16])
	}
	vp8 := still[12:] // the VP8 chunk, header and all
	m, err := webp.Decode(bytes.NewReader(still))
	if err != nil {
		t.Fatal(err)
	}
	ycc := m.(*image.YCbCr)
	w, h := ycc.Rect.Dx(), ycc.Rect.Dy() // 150×100
	plane := make([]byte, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			plane[y*w+x] = byte(x * 255 / (w - 1)) // transparent at the left edge, opaque at the right
		}
	}
	alph := webptest.Chunk("ALPH", append([]byte{0}, plane...)) // uncompressed, unfiltered
	data := webptest.Anim(w+10, h+10, 0,
		webptest.Frame{X: 10, Y: 4, W: w, H: h, DurMS: 70, Chunks: [][]byte{alph, vp8}},
		webptest.Frame{X: 0, Y: 0, W: w, H: h, DurMS: 30, NoBlend: true, Chunks: [][]byte{vp8}},
	)
	ix, err := testIndexWebP(data)
	if err != nil {
		t.Fatal(err)
	}
	ref := image.NewRGBA(image.Rect(0, 0, w, h)) // the frame alone, premultiplied
	draw.Draw(ref, ref.Rect, &image.NYCbCrA{YCbCr: *ycc, A: plane, AStride: w}, image.Point{}, draw.Src)
	opaque := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(opaque, opaque.Rect, ycc, image.Point{}, draw.Src)

	d := testDecoder(t, ix, data)
	d.next1()
	if d.werr != nil {
		t.Fatal(d.werr)
	}
	for _, p := range []image.Point{{0, 0}, {1, 50}, {75, 50}, {149, 99}, {100, 3}} {
		if g, want := d.canvas.RGBAAt(10+p.X, 4+p.Y), ref.RGBAAt(p.X, p.Y); !nearRGBA(g, want, 1) {
			t.Errorf("frame 0 at %v: %v, want %v", p, g, want)
		}
	}
	if g := d.canvas.RGBAAt(5, 50); g != (color.RGBA{}) {
		t.Errorf("left of frame 0: %v, want transparent", g)
	}
	if g := d.canvas.RGBAAt(10+75, 4+50); g.A == 0 || g.A == 255 {
		t.Errorf("frame 0's middle has alpha %d, want the plane's ~128", g.A)
	}
	d.next1()
	if d.werr != nil {
		t.Fatal(d.werr)
	}
	for _, p := range []image.Point{{0, 0}, {75, 50}, {149, 99}} {
		if g, want := d.canvas.RGBAAt(p.X, p.Y), opaque.RGBAAt(p.X, p.Y); !nearRGBA(g, want, 1) {
			t.Errorf("frame 1 at %v: %v, want %v", p, g, want)
		}
	}
	if g, want := d.canvas.RGBAAt(155, 60), ref.RGBAAt(145, 56); !nearRGBA(g, want, 1) { // past frame 1: frame 0 stays
		t.Errorf("frame 0 under frame 1's right: %v, want %v", g, want)
	}
}

// TestAnimWebPEmojiPlays is :explodes:: a custom emoji that's an
// animated WebP behind a .png URL loads (it was a blank tile, fetched
// again every minute), its still is the first frame, it plays at the
// emoji's size and streams when it's large, and with animations off it
// stays the still.
func TestAnimWebPEmojiPlays(t *testing.T) {
	u := "https://emoji.slack-edge.com/T1/explodes/0123456789abcdef.png"
	var big []webptest.Frame // 400×400, 4 frames: 2.5 MB of frames, past animKeepBytes
	for i, c := range []color.NRGBA{wRed, {0, 0, 255, 255}, {0, 255, 0, 255}, {9, 9, 9, 255}} {
		f := webptest.Solid(0, 0, 400, 400, 60, c)
		f.NoBlend = i%2 == 1
		big = append(big, f)
	}
	ub := "https://emoji.slack-edge.com/T1/big/0123456789abcdef.png"
	c, _ := gifCache(t, map[string][]byte{u: synthWebP(0), ub: webptest.Anim(400, 400, 0, big...)})

	e := readyEntry(t, c, u, 44)
	if e.frames != 3 || !c.IsAnimation(u, 44) || c.Failed(u, 44) {
		t.Fatalf("entry: %d frames, animation %v, failed %v", e.frames, c.IsAnimation(u, 44), c.Failed(u, 44))
	}
	if g := e.px[0].RGBAAt(0, 0); g != (color.RGBA{255, 0, 0, 255}) { // 8×8 isn't scaled up
		t.Errorf("the still's top left is %v, want frame 0's red", g)
	}
	if g := e.px[0].RGBAAt(0, 7); g.A != 0 {
		t.Errorf("the still's bottom left is %v, want transparent (frame 0 stops at row 6)", g)
	}
	if seen := drawFor(c, u, 44, animSettle+700*time.Millisecond); len(seen) < 4 {
		t.Errorf("%d distinct frames drawn in 850 ms, want it to play", len(seen))
	}
	readyEntry(t, c, ub, 0)
	if seen := drawFor(c, ub, 0, animSettle+600*time.Millisecond); len(seen) < 4 {
		t.Errorf("the large one: %d distinct frames drawn in 750 ms, want it to stream", len(seen))
	}
	if p := playerOf(c, ub, 0); p == nil || p.keep {
		t.Errorf("the large one's player %+v, want a streaming one", p)
	}
	c.SetAnimate(false)
	if seen := drawFor(c, u, 44, 400*time.Millisecond); len(seen) != 1 || seen[0] != e.op {
		t.Errorf("animations off: %d frames drawn, want the still only", len(seen))
	}
}

// Bytes that arrive whole and don't decode are a final failure: no retry
// is scheduled (nothing repaints for it), Get never fetches them again,
// and the idle sweep keeps the failure. A network error still retries.
func TestUndecodableIsFinal(t *testing.T) {
	brokenAnim := synthWebP(0)
	if i := bytes.Index(brokenAnim, []byte("VP8L")); i > 0 {
		brokenAnim = append([]byte(nil), brokenAnim...)
		brokenAnim[i+8] = 0 // the first frame's VP8L signature
	}
	stillJunk := []byte("RIFF\x1a\x00\x00\x00WEBPVP8 \x0e\x00\x00\x00junkjunkjunkjk")
	files := map[string][]byte{
		"https://emoji.slack-edge.com/T1/a/0.png": brokenAnim,
		"https://emoji.slack-edge.com/T1/b/0.png": pngBytes(48, 48)[:60], // cut off after its header
		"https://emoji.slack-edge.com/T1/c/0.png": stillJunk,
	}
	var calls atomic.Int32
	inval := make(chan struct{}, 16)
	c := New(func(_ context.Context, u string) ([]byte, error) {
		calls.Add(1)
		if b, ok := files[u]; ok {
			return b, nil
		}
		return nil, errors.New("read tcp: connection reset by peer")
	}, "", func() { inval <- struct{}{} })
	for u := range files {
		c.Get(u, 44)
		waitInval(t, inval, u, 5*time.Second)
		k := key{u, 44}
		c.mu.Lock()
		f, ok := c.failed[k]
		c.mu.Unlock()
		if !ok || !f.final || !c.Failed(u, 44) {
			t.Fatalf("%s: failure %+v (%v), want a final one", u, f, ok)
		}
		c.mu.Lock()
		f.retry = time.Now().Add(-time.Hour) // long past any backoff
		c.failed[k] = f
		c.mu.Unlock()
		n := calls.Load()
		for i := 0; i < 3; i++ {
			if _, ok := c.Get(u, 44); ok {
				t.Fatalf("%s: ready", u)
			}
		}
		if c.Pending() != 0 || calls.Load() != n {
			t.Errorf("%s fetched again after its final failure", u)
		}
	}
	c.mu.Lock()
	wake := c.wakeAt
	c.mu.Unlock()
	if !wake.IsZero() {
		t.Errorf("a repaint is scheduled at %v for failures that never retry", wake)
	}
	c.sweep(time.Now().Add(time.Hour).UnixMilli())
	c.mu.Lock()
	kept := len(c.failed)
	c.mu.Unlock()
	if kept != len(files) {
		t.Errorf("the sweep dropped final failures: %d left of %d", kept, len(files))
	}
	// a network error isn't final: it retries
	net := "https://emoji.slack-edge.com/T1/net/0.png"
	c.Get(net, 44)
	waitInval(t, inval, "the network error", 5*time.Second)
	c.mu.Lock()
	f := c.failed[key{net, 44}]
	c.mu.Unlock()
	if f.final || f.retry.IsZero() {
		t.Errorf("a network error's failure %+v, want a retry", f)
	}

	if got := retryAfter(1, undecodable(errors.New("png: invalid format"))); got != RetryPermanent {
		t.Errorf("an undecodable image retries after %v", got)
	}
	for _, err := range []error{image.ErrFormat, ErrTooBig, fmt.Errorf("x: %w", ErrTooBig)} {
		if errors.Is(undecodable(err), ErrUndecodable) {
			t.Errorf("%v made final", err)
		}
	}
}

// An image drawn again while it waits (it's on screen) loads before the
// rest of its lane's queue (what the picker or the last channel left
// there), unless it hasn't been drawn for animGrace by its turn; every
// queued image still loads, once.
func TestDrawnLoadsJumpTheQueue(t *testing.T) {
	release := make(chan struct{})
	started := make(chan string, 1000)
	c := New(func(_ context.Context, u string) ([]byte, error) {
		started <- u
		<-release
		return pngBytes(8, 8), nil
	}, "", nil)
	url := func(i int) string {
		return fmt.Sprintf("https://emoji.slack-edge.com/T1/e%04d/0123456789abcdef.png", i)
	}
	next := func(what string) string {
		t.Helper()
		select {
		case u := <-started:
			return u
		case <-time.After(30 * time.Second):
			t.Fatalf("%s: no load started", what)
			return ""
		}
	}
	const n = 200
	for i := 0; i < n; i++ {
		c.Get(url(i), 16) // asked once each: a fast scroll
	}
	for k := 0; k < laneWorkers; k++ {
		next("the first loads")
	}
	stale, on := url(100), url(500)
	c.Get(stale, 16) // drawn again, then scrolled away
	c.Get(on, 16)    // queued at the back...
	c.Get(on, 16)    // ...and drawn again: on screen
	c.mu.Lock()
	w := c.waiting[key{stale, 16}]
	w.asked -= 2 * animGrace.Milliseconds()
	c.waiting[key{stale, 16}] = w
	c.mu.Unlock()

	release <- struct{}{}
	if u := next("the freed worker's load"); u != on {
		t.Fatalf("the freed worker loaded %s, want the one on screen (%s)", u, on)
	}
	release <- struct{}{}
	if u := next("the next load"); u != url(laneWorkers) {
		t.Fatalf("then %s, want the queue's head %s (the stale one waits its turn)", u, url(laneWorkers))
	}
	close(release)
	seen := map[string]int{}
	for k := laneWorkers + 2; k < n+1; k++ {
		seen[next("the rest")]++
	}
	for u, k := range seen {
		if k != 1 {
			t.Errorf("%s loaded %d times", u, k)
		}
	}
	if seen[stale] != 1 {
		t.Errorf("the stale one loaded %d times, want once at its turn", seen[stale])
	}
	for dl := time.Now().Add(5 * time.Second); c.Pending() > 0 && time.Now().Before(dl); time.Sleep(10 * time.Millisecond) {
	}
	c.mu.Lock()
	left := len(c.waiting)
	c.mu.Unlock()
	if c.Pending() != 0 || left != 0 {
		t.Errorf("%d pending, %d waiting after the queue drained", c.Pending(), left)
	}
}
