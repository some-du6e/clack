package imgcache

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// A blob: URL (a plugin's object URL: HMojis' emoji images) loads from
// the blob source the UI sets, the plugins' runtime: drawn at its size,
// nothing asked of the fetcher, nothing written to the disk tier. A URL
// the source doesn't hold (revoked), or any with no source, fails.
func TestBlobURLImages(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 3; x++ {
			src.SetNRGBA(x, y, color.NRGBA{G: 255, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	var asked atomic.Int32
	fetch := func(ctx context.Context, raw string) ([]byte, error) {
		asked.Add(1)
		return nil, errors.New("no network in this test")
	}
	dir := t.TempDir()
	c := New(fetch, dir, nil)
	defer c.Close()
	t.Cleanup(func() { setBlobSource(nil) })
	setBlobSource(func(ctx context.Context, u string, max int) ([]byte, int, bool) {
		if u == "blob:clack/held" {
			return buf.Bytes(), buf.Len(), true
		}
		return nil, 0, false
	})
	wait := func(u string, want bool) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for {
			if _, ok := c.Get(u, 0); ok {
				if !want {
					t.Fatalf("%s was drawn", u)
				}
				return
			}
			if c.Failed(u, 0) {
				if want {
					t.Fatalf("%s failed to load", u)
				}
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s neither drew nor failed in 10s", u)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	wait("blob:clack/held", true)
	if sz, _ := c.Size("blob:clack/held", 0); sz != image.Pt(3, 2) {
		t.Errorf("drawn at %v, want its 3×2", sz)
	}
	wait("blob:clack/revoked", false)
	setBlobSource(nil)
	wait("blob:clack/no-source", false)
	if n := asked.Load(); n != 0 {
		t.Errorf("the fetcher was asked %d times; a blob: URL never reaches the network", n)
	}
	files := 0
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files++
		}
		return nil
	})
	if files != 0 {
		t.Errorf("%d files in the disk tier, want none", files)
	}
}

// A blob: URL is held to a download's limits: past MaxFetchBytes it's
// refused without its bytes (the source is asked with the cap), the wait
// for the plugins' runtime is bounded (blobWait: blob loads share a lane
// with message images), and a URL the runtime doesn't hold yet (plugins
// start after the first frames) is tried again soon, not after
// RetryPermanent's 10 minutes.
func TestBlobURLLimits(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	c := New(func(ctx context.Context, raw string) ([]byte, error) {
		return nil, errors.New("no network in this test")
	}, t.TempDir(), nil)
	defer c.Close()
	t.Cleanup(func() { setBlobSource(nil) })
	var mu sync.Mutex
	maxes := map[string]int{}
	deadlines := map[string]time.Duration{}
	var late atomic.Bool
	setBlobSource(func(ctx context.Context, u string, max int) ([]byte, int, bool) {
		dl, has := ctx.Deadline()
		mu.Lock()
		maxes[u] = max
		if has {
			deadlines[u] = time.Until(dl)
		} else {
			deadlines[u] = -1
		}
		mu.Unlock()
		switch u {
		case "blob:clack/huge":
			return nil, MaxFetchBytes + 1, true
		case "blob:clack/slow":
			select {
			case <-ctx.Done():
			case <-time.After(5 * time.Second):
			}
			return nil, 0, false
		case "blob:clack/late":
			if late.Load() {
				return buf.Bytes(), buf.Len(), true
			}
		}
		return nil, 0, false
	})
	settle := func(u string, within time.Duration) (drawn bool) {
		t.Helper()
		for deadline := time.Now().Add(within); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			if _, ok := c.Get(u, 0); ok {
				return true
			}
			if c.Failed(u, 0) {
				return false
			}
		}
		t.Fatalf("%s neither drew nor failed in %v", u, within)
		return false
	}
	if settle("blob:clack/huge", 10*time.Second) {
		t.Error("a blob past MaxFetchBytes was drawn")
	}
	t0 := time.Now()
	if settle("blob:clack/slow", 10*time.Second) {
		t.Error("the slow blob was drawn")
	}
	if d := time.Since(t0); d > 3*time.Second {
		t.Errorf("a load waited %v for a busy runtime; want about blobWait (1 s)", d.Round(time.Millisecond))
	}
	mu.Lock()
	if maxes["blob:clack/huge"] != MaxFetchBytes {
		t.Errorf("the source was asked with max %d; want MaxFetchBytes, so a bigger blob's bytes stay unread", maxes["blob:clack/huge"])
	}
	if d := deadlines["blob:clack/slow"]; d <= 0 || d > 2*time.Second {
		t.Errorf("the source's context had deadline %v ahead; want about a second", d)
	}
	mu.Unlock()
	// not held yet, then held: drawn on a retry, a few seconds on
	if settle("blob:clack/late", 10*time.Second) {
		t.Fatal("the late blob drew before the runtime held it")
	}
	late.Store(true)
	for deadline := time.Now().Add(RetryMin + 6*time.Second); ; time.Sleep(20 * time.Millisecond) {
		if _, ok := c.Get("blob:clack/late", 0); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("a blob: URL the runtime came to hold still wasn't drawn after %v: its first failure counted as permanent", RetryMin+6*time.Second)
		}
	}
}
