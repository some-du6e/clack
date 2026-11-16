package popups

// The viewer, Copy Image and the GIF picker under imgcache's decode memory:
// an original too big to decode is replaced by the file's largest Slack
// thumbnail (shown, copied) with no error, and the picker's GIFs decode
// budgeted, their first gifAnimMaxFrames frames only. Synthetic images;
// no network, no clipboard.

import (
	"bytes"
	"compress/lzw"
	"context"
	"errors"
	"image"
	"image/png"
	"runtime"
	"testing"
	"time"

	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/richtext"
)

// overCapJPEG is a progressive 4:4:4 JPEG header of w×h with no scan data.
// Its decode charges the image, then each component's coefficient blocks
// as it parses the scan header: for 6000×6000, 108 MB and then 72 MB a
// component, past imgcache.MaxDecode, so it fails as ErrOverCap at once.
func overCapJPEG(w, h int) []byte {
	seg := func(marker byte, payload []byte) []byte {
		n := len(payload) + 2
		return append([]byte{0xff, marker, byte(n >> 8), byte(n)}, payload...)
	}
	out := []byte{0xff, 0xd8}
	sof := []byte{8, byte(h >> 8), byte(h), byte(w >> 8), byte(w), 3}
	for i := 0; i < 3; i++ {
		sof = append(sof, byte(i+1), 0x11, 0)
	}
	out = append(out, seg(0xc2, sof)...)
	out = append(out, seg(0xda, []byte{3, 1, 0, 2, 0, 3, 0, 0, 0, 0})...)
	return out
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

// TestLightboxShowsThumbnailForOverCapOriginal: a photo whose original
// needs more decode memory than an image may have opens in the viewer as
// its largest Slack thumbnail, never "This image couldn't be loaded", and
// stays that way once the original has failed.
func TestLightboxShowsThumbnailForOverCapOriginal(t *testing.T) {
	data := map[string][]byte{testOrig: overCapJPEG(6000, 6000), testThumb: testPNG(t, 1024, 768)}
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
	shown := false
	for end := time.Now().Add(30 * time.Second); time.Now().Before(end); time.Sleep(15 * time.Millisecond) {
		img, ok, msg := lbShown(imgs, it)
		if msg == "This image couldn't be loaded" {
			t.Fatal("the viewer reported the photo as not loadable; its thumbnail should stand in")
		}
		if ok {
			if sz := img.Size(); sz != image.Pt(1024, 768) {
				t.Fatalf("the viewer shows a %v image, want the 1024×768 thumbnail", sz)
			}
			shown = true
			if imgs.Failed(testOrig, 0) {
				break
			}
		}
	}
	if !shown {
		t.Fatal("the viewer never showed the thumbnail")
	}
	if !imgs.Failed(testOrig, 0) {
		t.Fatal("the over-cap original never failed")
	}
	if img, ok, msg := lbShown(imgs, it); !ok || img.Size() != image.Pt(1024, 768) || msg != "" {
		t.Errorf("once the original failed the viewer shows ok=%v %v %q, want the thumbnail", ok, img.Size(), msg)
	}
}

// TestCopyImageFallsBackToThumbnail: Copy image on a photo whose original
// can't be decoded within the decode memory copies the file's largest
// thumbnail instead, with no error; with no thumbnail the original's
// error (over the cap) is what's reported.
func TestCopyImageFallsBackToThumbnail(t *testing.T) {
	data := map[string][]byte{testOrig: overCapJPEG(6000, 6000), testThumb: testPNG(t, 1024, 768)}
	prevF, prevW := copyFetch, copyWrite
	t.Cleanup(func() { copyFetch, copyWrite = prevF, prevW })
	copyFetch = func(_ context.Context, _ Host, u string, _ bool) ([]byte, error) {
		if b, ok := data[u]; ok {
			return b, nil
		}
		return nil, errors.New("404")
	}
	var copied image.Rectangle
	copyWrite = func(_ context.Context, img image.Image, charge func(int64) error) error {
		copied = img.Bounds()
		return charge(4 * int64(copied.Dx()) * int64(copied.Dy())) // as clipimg's copies would
	}
	if err := copyImageWork(context.Background(), nil, testOrig, testThumb, true); err != nil {
		t.Fatalf("Copy image with a thumbnail to stand in: %v", err)
	}
	if copied != image.Rect(0, 0, 1024, 768) {
		t.Errorf("copied %v, want the 1024×768 thumbnail", copied)
	}
	copied = image.Rectangle{}
	err := copyImageWork(context.Background(), nil, testOrig, "", true)
	if !errors.Is(err, imgcache.ErrTooBig) {
		t.Errorf("Copy image of the original alone: %v, want ErrTooBig (over the decode memory)", err)
	}
	if !copied.Empty() {
		t.Errorf("an over-cap original was copied (%v)", copied)
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

// TestGIFPickerDecodeBudgeted: the picker decodes a GIF of 400 1024×1024
// frames (400 MB of frames as stdlib's DecodeAll decoded them) through the
// budgeted decoder, its first gifAnimMaxFrames only: about a third of
// that. Decode every frame (stdlib gif.DecodeAll) and it allocates them
// all.
func TestGIFPickerDecodeBudgeted(t *testing.T) {
	data := manyFrameGIF(1024, 1024, 400)
	gifAnims.mu.Lock()
	prev := gifAnims.fetch
	gifAnims.fetch = func(context.Context, string) ([]byte, error) { return data, nil }
	gifAnims.mu.Unlock()
	t.Cleanup(func() { gifAnims.mu.Lock(); gifAnims.fetch = prev; gifAnims.mu.Unlock() })
	var n int
	var err error
	alloc := allocBytes(func() {
		var frames []paint.ImageOp
		frames, _, err = gifAnimDecode("https://media.giphy.com/media/x/200w.gif", 64)
		n = len(frames)
	})
	if err != nil {
		t.Fatalf("decoding a 400-frame GIF: %v", err)
	}
	if n != gifAnimMaxFrames {
		t.Errorf("%d frames, want the first %d", n, gifAnimMaxFrames)
	}
	if alloc > 220<<20 {
		t.Errorf("the picker allocated %d bytes for a GIF it shows %d frames of (every frame is ~420 MB)", alloc, gifAnimMaxFrames)
	}
}
