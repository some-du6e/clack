package popups

// The viewer, Copy Image and the GIF picker under imgcache's decode memory:
// an original too big to decode is replaced by the file's largest Slack
// thumbnail (shown; copied, and toasted as a smaller copy) only for that
// reason, never for a slow download or encode; and the picker's GIFs decode
// in the small lane, their first gifAnimMaxFrames frames only, kept frames
// charged. Synthetic images; no network, no clipboard.

import (
	"bytes"
	"compress/lzw"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"runtime"
	"sync"
	"testing"
	"time"

	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// progressiveJPEG is a complete, valid progressive JPEG of w×h at 4:4:4:
// a quantization table, a DC Huffman table of one 1-bit code, one DC scan
// in which every block's difference is 0 (one 0 bit a block), and EOI (as
// imgcache's decfixtures_test.go builds it, which decodes it). At 6000×6000
// its decode holds the 108 MB image and 72 MB of int16 coefficients a
// component: past imgcache.MaxDecode at the second component, so it's
// ErrOverCap; with room, it would decode to a flat grey 6000×6000.
func progressiveJPEG(w, h int) []byte {
	seg := func(marker byte, payload []byte) []byte {
		n := len(payload) + 2
		return append([]byte{0xff, marker, byte(n >> 8), byte(n)}, payload...)
	}
	out := []byte{0xff, 0xd8}
	dqt := make([]byte, 65)
	for i := 1; i < 65; i++ {
		dqt[i] = 1
	}
	out = append(out, seg(0xdb, dqt)...)
	sof := []byte{8, byte(h >> 8), byte(h), byte(w >> 8), byte(w), 3}
	for i := 0; i < 3; i++ {
		sof = append(sof, byte(i+1), 0x11, 0)
	}
	out = append(out, seg(0xc2, sof)...)
	dht := make([]byte, 18)
	dht[1] = 1 // one code of length 1, for category 0
	out = append(out, seg(0xc4, dht)...)
	out = append(out, seg(0xda, []byte{3, 1, 0, 2, 0, 3, 0, 0, 0, 0})...)
	blocks := ((w + 7) / 8) * ((h + 7) / 8) * 3
	out = append(out, make([]byte, (blocks+7)/8)...)
	return append(out, 0xff, 0xd9)
}

func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

const (
	testOrig  = "https://files.slack.com/files-pri/T1-F1/photo.jpg"
	testThumb = "https://files.slack.com/files-tmb/T1-F1-abc/photo_1024.png"
)

// TestLightboxShowsThumbnailForOverCapOriginal: a valid photo whose
// original needs more decode memory than an image may have opens in the
// viewer as its largest Slack thumbnail, never "This image couldn't be
// loaded", and stays that way once the original has failed. The original
// is a complete progressive JPEG: without the cap it would decode, and the
// viewer would show it (2048 px) instead; without the thumbnail standing in,
// the viewer would show nothing.
func TestLightboxShowsThumbnailForOverCapOriginal(t *testing.T) {
	data := map[string][]byte{testOrig: progressiveJPEG(6000, 6000), testThumb: testPNG(t, 1024, 768)}
	imgs := imgcache.New(func(_ context.Context, u string) ([]byte, error) {
		if b, ok := data[u]; ok {
			return b, nil
		}
		return nil, errors.New("404")
	}, "", nil)
	file := &richtext.File{ID: "F1", Name: "photo.jpg", Mime: "image/jpeg", IsImage: true, URL: testOrig,
		Thumb: &richtext.Image{URL: testThumb, W: 1024, H: 768, Proxy: true}}
	it := lbItem{img: *file.Thumb, file: file}
	if it.displayURL() != testOrig {
		t.Fatalf("the viewer shows %s, want the original", it.displayURL())
	}
	for end := time.Now().Add(60 * time.Second); !imgs.Failed(testOrig, 0); time.Sleep(15 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatal("the over-cap original neither loaded nor failed in 60 s")
		}
		img, ok, msg := lbShown(imgs, it)
		if msg == "This image couldn't be loaded" {
			t.Fatal("the viewer reported the photo as not loadable; its thumbnail should stand in")
		}
		if ok && img.Size() != image.Pt(1024, 768) {
			t.Fatalf("the viewer shows a %v image, want the 1024×768 thumbnail (the original is past the decode memory)", img.Size())
		}
	}
	var shown paint.ImageOp
	ok, msg := false, ""
	for end := time.Now().Add(30 * time.Second); !ok && time.Now().Before(end); time.Sleep(15 * time.Millisecond) {
		shown, ok, msg = lbShown(imgs, it)
	}
	if !ok || shown.Size() != image.Pt(1024, 768) || msg != "" {
		t.Fatalf("once the original failed the viewer shows ok=%v %v %q, want the 1024×768 thumbnail", ok, shown.Size(), msg)
	}
}

// copyFake serves copyFetch from data (a delay per URL first, ctx
// permitting; failF's error instead, for a URL it has one for) and records
// what copyWrite is given (failing with failW for an image of the size it
// names).
type copyFake struct {
	data   map[string][]byte
	delay  map[string]time.Duration
	failF  map[string]error
	failW  map[image.Point]error
	mu     sync.Mutex
	copied []image.Rectangle
}

func (f *copyFake) install(t *testing.T) {
	prevF, prevW := copyFetch, copyWrite
	t.Cleanup(func() { copyFetch, copyWrite = prevF, prevW })
	copyFetch = func(ctx context.Context, _ Host, u string, _ bool) ([]byte, error) {
		select {
		case <-time.After(f.delay[u]):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if err := f.failF[u]; err != nil {
			return nil, err
		}
		if b, ok := f.data[u]; ok {
			return b, nil
		}
		return nil, errors.New("http 404")
	}
	copyWrite = func(ctx context.Context, img image.Image, charge func(int64) error) error {
		if err := f.failW[img.Bounds().Size()]; err != nil {
			return err
		}
		if err := charge(4 * int64(img.Bounds().Dx()) * int64(img.Bounds().Dy())); err != nil {
			return err
		}
		f.mu.Lock()
		f.copied = append(f.copied, img.Bounds())
		f.mu.Unlock()
		return nil
	}
}

// TestCopyImageOverCapCopiesThumbnail: Copy image on a photo whose
// original Clack can't decode within the decode memory copies the file's
// largest thumbnail in its place and says so (smaller); with no thumbnail
// the original's reason (over the cap) is what's reported.
func TestCopyImageOverCapCopiesThumbnail(t *testing.T) {
	f := &copyFake{data: map[string][]byte{testOrig: progressiveJPEG(6000, 6000), testThumb: testPNG(t, 1024, 768)}}
	f.install(t)
	smaller, err := copyImageWork(context.Background(), nil, testOrig, testThumb, true)
	if err != nil || !smaller {
		t.Fatalf("Copy image of an over-cap original: smaller=%v %v, want the thumbnail copied, as a smaller copy", smaller, err)
	}
	if len(f.copied) != 1 || f.copied[0] != image.Rect(0, 0, 1024, 768) {
		t.Errorf("copied %v, want only the 1024×768 thumbnail", f.copied)
	}
	f.copied = nil
	smaller, err = copyImageWork(context.Background(), nil, testOrig, "", true)
	if !errors.Is(err, imgcache.ErrTooBig) || smaller || len(f.copied) != 0 {
		t.Errorf("the original alone: smaller=%v copied %v, %v; want ErrTooBig and nothing copied", smaller, f.copied, err)
	}
}

// TestCopyImageSlowOriginalIsCopied: an original that fits but takes
// longer than 8 s to download is still the one copied, with the whole of
// the copy's time (run's 15 s), never its thumbnail. With a cut-off for
// the original, the thumbnail would be copied and toasted as the image.
func TestCopyImageSlowOriginalIsCopied(t *testing.T) {
	if testing.Short() {
		t.Skip("waits 9 s")
	}
	f := &copyFake{
		data:  map[string][]byte{testOrig: testPNG(t, 3000, 2000), testThumb: testPNG(t, 1024, 683)},
		delay: map[string]time.Duration{testOrig: 9 * time.Second},
	}
	f.install(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second) // as run gives it
	defer cancel()
	smaller, err := copyImageWork(ctx, nil, testOrig, testThumb, true)
	if err != nil || smaller {
		t.Fatalf("a 9 s original: smaller=%v %v, want the original copied", smaller, err)
	}
	if len(f.copied) != 1 || f.copied[0] != image.Rect(0, 0, 3000, 2000) {
		t.Errorf("copied %v, want only the 3000×2000 original", f.copied)
	}
}

// TestCopyImageNoStandInForOtherFailures: a copy whose original fails for
// a reason that isn't the original itself (its download fails, its
// clipboard helper fails, it times out) reports that failure and copies
// nothing in its place, though the thumbnail would copy: the thumbnail
// stands in only for an original Clack can't decode or download at all.
func TestCopyImageNoStandInForOtherFailures(t *testing.T) {
	orig, thumb := testPNG(t, 300, 200), testPNG(t, 100, 66)
	for _, c := range []struct {
		name string
		fake *copyFake
		ctx  time.Duration
	}{
		{name: "download fails", fake: &copyFake{failF: map[string]error{testOrig: errors.New("core: Fetch: http 500")}}},
		{name: "clipboard helper fails", fake: &copyFake{failW: map[image.Point]error{{300, 200}: errors.New("xclip: exit status 1")}}},
		{name: "times out", fake: &copyFake{delay: map[string]time.Duration{testOrig: time.Hour}}, ctx: 300 * time.Millisecond},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := c.fake
			f.data = map[string][]byte{testOrig: orig, testThumb: thumb}
			f.install(t)
			ctx := context.Background()
			if c.ctx > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, c.ctx)
				defer cancel()
			}
			smaller, err := copyImageWork(ctx, nil, testOrig, testThumb, true)
			if err == nil || smaller || len(f.copied) != 0 {
				t.Errorf("smaller=%v copied %v, %v; want the original's failure and nothing copied", smaller, f.copied, err)
			}
		})
	}
}

// TestStandInReasons: the reasons a thumbnail may stand in for an original,
// and none else.
func TestStandInReasons(t *testing.T) {
	for _, c := range []struct {
		err  error
		want bool
	}{
		{imgcache.ErrOverCap, true},
		{imgcache.ErrTooBig, true},
		{fmt.Errorf("decode: %w", image.ErrFormat), true},
		{fmt.Errorf("core: Fetch: file over 64 MB: %w", slack.ErrTooLarge), true},
		{context.DeadlineExceeded, false},
		{context.Canceled, false},
		{fmt.Errorf("%w (after %w)", imgcache.ErrTooBig, context.DeadlineExceeded), false},
		{imgcache.ErrBusy, false},
		{imgcache.ErrUndecodable, false},
		{errors.New("core: Fetch: http 500"), false},
	} {
		if got := standIn(c.err); got != c.want {
			t.Errorf("standIn(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

// manyFrameGIF is a GIF of frames frames, each the whole w×h screen of
// colour 0, its LZW data a few KB a frame: what a 12 MB GIF of thousands
// of big frames is made of.
func manyFrameGIF(w, h, frames int) []byte {
	var lz bytes.Buffer
	lw := lzw.NewWriter(&lz, lzw.LSB, 2)
	lw.Write(make([]byte, w*h))
	lw.Close()
	var sub []byte
	for p := lz.Bytes(); len(p) > 0; {
		n := min(255, len(p))
		sub = append(sub, byte(n))
		sub = append(sub, p[:n]...)
		p = p[n:]
	}
	sub = append(sub, 0)
	out := []byte("GIF89a")
	out = append(out, byte(w), byte(w>>8), byte(h), byte(h>>8), 0x80, 0, 0) // a 2-colour global table
	out = append(out, 0, 0, 0, 255, 255, 255)
	for i := 0; i < frames; i++ {
		out = append(out, 0x21, 0xf9, 4, 0, 5, 0, 0, 0) // 50 ms
		out = append(out, 0x2c, 0, 0, 0, 0, byte(w), byte(w>>8), byte(h), byte(h>>8), 0)
		out = append(out, 2)
		out = append(out, sub...)
	}
	return append(out, 0x3b)
}

func allocBytes(f func()) uint64 {
	var a, b runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&a)
	f()
	runtime.GC()
	runtime.ReadMemStats(&b)
	return b.TotalAlloc - a.TotalAlloc
}

func serveGIF(t *testing.T, data []byte) {
	gifAnims.mu.Lock()
	prev := gifAnims.fetch
	gifAnims.fetch = func(context.Context, string) ([]byte, error) { return data, nil }
	gifAnims.mu.Unlock()
	t.Cleanup(func() { gifAnims.mu.Lock(); gifAnims.fetch = prev; gifAnims.mu.Unlock() })
}

// TestGIFPickerDecodeBudgeted: the picker decodes a GIF of 400 1024×1024
// frames (420 MB of frames as stdlib's DecodeAll decoded them) through the
// budgeted decoder, its first gifAnimMaxFrames only, and the frames it
// keeps stay charged to the decode memory until it lets them go.
func TestGIFPickerDecodeBudgeted(t *testing.T) {
	serveGIF(t, manyFrameGIF(1024, 1024, 400))
	var d gifDecoded
	var err error
	alloc := allocBytes(func() { d, err = gifAnimDecode("https://media.giphy.com/media/x/200w.gif", 64) })
	if err != nil {
		t.Fatalf("decoding a 400-frame GIF: %v", err)
	}
	defer d.keep.Release()
	if len(d.frames) != gifAnimMaxFrames {
		t.Errorf("%d frames, want the first %d", len(d.frames), gifAnimMaxFrames)
	}
	if alloc > 220<<20 {
		t.Errorf("the picker allocated %d bytes for a GIF it shows %d frames of (every frame is ~420 MB)", alloc, gifAnimMaxFrames)
	}
	if want := int64(4 * 64 * 64 * len(d.frames)); d.keep.Used() != want || d.bytes != int(want) {
		t.Errorf("the kept frames are charged %d (%d counted), want %d", d.keep.Used(), d.bytes, want)
	}
}

// TestGIFPickerSmallLane: a preview whose decode and frames pass the small
// threshold (8 MiB) doesn't wait for the big-decode slot: a big decode
// holding it (a lightbox original, a Copy image) doesn't hold the picker up.
func TestGIFPickerSmallLane(t *testing.T) {
	serveGIF(t, manyFrameGIF(200, 200, 60)) // 60 frames of 198 px: 9.4 MB composed
	big := imgcache.NewBudget()
	defer big.Release()
	if err := big.Charge(16 << 20); err != nil { // past the small threshold: it holds the slot
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		d, err := gifAnimDecode("https://media.giphy.com/media/y/200w.gif", 198)
		d.keep.Release()
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("the preview with the big slot held: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the GIF picker's preview waited for the big-decode slot")
	}
}
