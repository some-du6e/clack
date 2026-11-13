package imgcache

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDiskOK(t *testing.T) {
	for u, want := range map[string]bool{
		"https://avatars.slack-edge.com/2024/x_72.jpg":                                         true,
		"https://ca.slack-edge.com/T0266FRGM-U1-abc-72":                                        true,
		"https://emoji.slack-edge.com/T0266FRGM/party/abc.gif":                                 true,
		"https://a.slack-edge.com/production-standard-emoji-assets/14.0/apple-large/1f600.png": true,
		"https://files.slack.com/files-pri/T-F/image.png":                                      false,
		"https://slack-files.com/T-F-x":                                                        false,
		"https://example.com/unfurl.png":                                                       false,
		"http://avatars.slack-edge.com/x.jpg":                                                  false,
		"https://slack-imgs.com/?c=1&url=https%3A%2F%2Fexample.com%2Fa.png":                    false,
		"https://avatars.slack-edge.com.evil.com/a.png":                                        false,
		"https://files-edge.slack.com/files-tmb/T-F/a_360.png":                                 false,
		"https://a.slack-edge.com/bv1-13/slack_logo.png":                                       false,
		"https://media.giphy.com/media/x/giphy.gif":                                            false,
	} {
		if got := DiskOK(u); got != want {
			t.Errorf("DiskOK(%q) = %v, want %v", u, got, want)
		}
	}
}

func pngBytes(w, h int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	img.Set(0, 0, color.NRGBA{255, 0, 0, 255})
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

func TestGetAsyncScalesAndCaches(t *testing.T) {
	var calls atomic.Int32
	done := make(chan struct{}, 4)
	dir := t.TempDir()
	c := New(func(ctx context.Context, url string) ([]byte, error) {
		calls.Add(1)
		return pngBytes(200, 100), nil
	}, dir, func() { done <- struct{}{} })
	avatar := "https://avatars.slack-edge.com/a.png"
	file := "https://files.slack.com/files-pri/x.png"
	if _, ok := c.Get(avatar, 50); ok {
		t.Fatal("ready before fetch")
	}
	c.Get(file, 0)
	for i := 0; i < 2; i++ {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("timeout")
		}
	}
	if sz, ok := c.Size(avatar, 50); !ok || sz != image.Pt(50, 25) {
		t.Fatalf("avatar size = %v %v", sz, ok)
	}
	if _, ok := c.Get(file, 0); !ok {
		t.Fatal("file not cached in memory")
	}
	if n := countFiles(dir); n != 1 {
		t.Fatalf("disk files = %d, want 1 (avatar only; message files stay in memory)", n)
	}
	if calls.Load() != 2 {
		t.Fatalf("fetch calls = %d", calls.Load())
	}
}

func countFiles(dir string) int {
	n := 0
	filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			n++
		}
		return nil
	})
	return n
}

func TestSweepDropsIdleEntries(t *testing.T) {
	c := New(nil, "", nil)
	c.mu.Lock()
	for i, used := range []int64{100, 200} {
		e := &entry{k: key{url: string(rune('a' + i))}, n: 10, used: used}
		c.entries[e.k] = c.lru.PushFront(e)
		c.bytes += e.n
	}
	c.mu.Unlock()
	c.sweep(150)
	if len(c.entries) != 1 || c.bytes != 10 {
		t.Fatalf("after sweep: %d entries, %d bytes", len(c.entries), c.bytes)
	}
	if _, ok := c.entries[key{url: "b"}]; !ok {
		t.Fatal("the recently used entry was dropped")
	}
}

// A standard emoji drawn bigger than its URL's CDN size loads the size
// that covers it (Slack's @2x) and is scaled down to the pixels asked for;
// when that variant fails, the URL as given still loads.
func TestStandardEmojiLoadsCoveringSize(t *testing.T) {
	const b = "https://a.slack-edge.com/production-standard-emoji-assets/16.0/"
	var mu sync.Mutex
	var got []string
	done := make(chan struct{}, 8)
	c := New(func(ctx context.Context, u string) ([]byte, error) {
		mu.Lock()
		got = append(got, u)
		mu.Unlock()
		switch u {
		case b + "google-medium/1f601.png":
			return pngBytes(22, 22), nil
		case b + "google-medium/1f601@2x.png":
			return pngBytes(44, 44), nil
		case b + "google-small/1f44d.png":
			return pngBytes(16, 16), nil
		}
		return nil, errors.New("404")
	}, "", func() { done <- struct{}{} })
	wait := func() {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("timeout")
		}
	}
	med := b + "google-medium/1f601.png"
	c.Get(med, 28) // 22px at 125%
	wait()
	if sz, ok := c.Size(med, 28); !ok || sz != image.Pt(28, 28) {
		t.Fatalf("size = %v %v, want 28×28 (44px scaled down, not 22px stretched)", sz, ok)
	}
	c.Get(med, 22)
	wait()
	if sz, _ := c.Size(med, 22); sz != image.Pt(22, 22) {
		t.Fatalf("size at 100%% = %v", sz)
	}
	thumbs := b + "google-small/1f44d.png" // no @2x served: falls back
	c.Get(thumbs, 30)
	wait()
	mu.Lock()
	defer mu.Unlock()
	want := []string{b + "google-medium/1f601@2x.png", med, b + "google-small/1f44d@2x.png", thumbs}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("fetched %v, want %v", got, want)
	}
}

// TestDeriveSmallerSizeAtOnce: a size of a URL another size of which is
// decoded is drawn by the first Get (scaled down from it, no fetch): a
// reaction pill showing the emoji the picker loaded. A larger size isn't
// made by upscaling; it loads.
func TestDeriveSmallerSizeAtOnce(t *testing.T) {
	var mu sync.Mutex
	fetches := 0
	fetch := func(context.Context, string) ([]byte, error) {
		mu.Lock()
		fetches++
		mu.Unlock()
		img := image.NewRGBA(image.Rect(0, 0, 64, 64))
		var b bytes.Buffer
		_ = png.Encode(&b, img)
		return b.Bytes(), nil
	}
	c := New(fetch, "", nil)
	const u = "https://emoji.slack-edge.com/T0/verity/0a1b2c.png"
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, ok := c.Get(u, 44); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the 44px decode never arrived")
		}
		time.Sleep(5 * time.Millisecond)
	}
	op, ok := c.Get(u, 16)
	if !ok || op.Size().X > 16 || op.Size().X == 0 {
		t.Fatalf("16px at once: ok %v, size %v", ok, op.Size())
	}
	if sz, ok := c.Size(u, 16); !ok || sz.X != 16 {
		t.Errorf("16px entry %v %v", sz, ok)
	}
	if _, ok := c.Get(u, 96); ok {
		t.Error("96px drawn from a 44px decode (upscaled)")
	}
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if fetches != 2 { // 44px, then 96px; 16px fetched nothing
		t.Errorf("%d fetches, want 2", fetches)
	}
}
