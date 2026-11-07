package imgcache

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
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
