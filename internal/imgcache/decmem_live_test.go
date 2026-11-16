package imgcache

// Capacity and liveness of the decode memory for normal images: a big
// progressive photo decodes (and is never shown as failed), an image under
// memory pressure loads once the pressure ends and never spins for good,
// and a retry decodes the bytes it already fetched. Synthetic images only.

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// countingCache is a cache (no disk tier) whose fetcher serves data and
// counts the downloads.
func countingCache(data []byte) (*Cache, *atomic.Int64) {
	var n atomic.Int64
	c := New(func(context.Context, string) ([]byte, error) {
		n.Add(1)
		return data, nil
	}, "", nil)
	return c, &n
}

// drawHow is how drawUntil treats Failed: fail the test, return, or keep on.
type drawHow int

const (
	mustNotFail drawHow = iota
	untilFailed
	throughFailed
)

// drawUntil draws (Get) k every few ms, as a window's frames would, until
// it's loaded or d passes, and as how says when Failed is true. It returns
// whether it loaded, and whether Failed was seen.
func drawUntil(t *testing.T, c *Cache, k key, d time.Duration, how drawHow) (loaded, failed bool) {
	t.Helper()
	for end := time.Now().Add(d); time.Now().Before(end); time.Sleep(15 * time.Millisecond) {
		if _, ok := c.Get(k.url, k.size); ok {
			return true, failed
		}
		if c.Failed(k.url, k.size) {
			failed = true
			switch how {
			case mustNotFail:
				t.Fatalf("%s showed as failed while it should be loading: %+v", k.url, c.failed[k])
			case untilFailed:
				return false, true
			}
		}
	}
	return false, failed
}

// TestProgressive444PhotoShows: a 16 MP progressive JPEG at 4:4:4 (the
// kind Photoshop and GIMP write at high quality) opens in the lightbox
// (Get at size 0) as its original, and is never Failed on the way. Its
// decode holds a 48 MB image and, until the image is built, 96 MB of
// coefficients: with them kept as int32 (192 MB) it would pass MaxDecode
// and fail as ErrOverCap.
func TestProgressive444PhotoShows(t *testing.T) {
	data := progressiveJPEG(4608, 3456, 3)
	c, fetches := countingCache(data)
	k := key{url: "https://files.slack.com/files-pri/T1-F1/photo.jpg", size: 0}
	loaded, _ := drawUntil(t, c, k, 60*time.Second, mustNotFail)
	if !loaded {
		t.Fatalf("a 16 MP progressive 4:4:4 JPEG didn't load in 60 s (failed: %+v)", c.failed[k])
	}
	if sz, ok := c.Size(k.url, k.size); !ok || sz.X != 2048 {
		t.Errorf("loaded at %v, want the 2048 px lightbox size", sz)
	}
	if n := fetches.Load(); n != 1 {
		t.Errorf("fetched %d times, want 1", n)
	}
}

// TestPressureEndsInALoad: an image whose decode the pool can't cover
// (other holders keep it nearly full) is still loading (never Failed)
// while the pressure is short, and loads once it ends; the busy tries
// decode the bytes the first try fetched. Make ErrBusy final (or drop the
// held bytes) and it never loads (or downloads again each try).
func TestPressureEndsInALoad(t *testing.T) {
	data := pngSolid(1024, 1024, pngRGBA8, false, nil) // 4 MB to decode
	withTestPool(16<<20, func() {
		c, fetches := countingCache(data)
		k := key{url: "https://files.slack.com/files-pri/T1-F2/shot.png", size: 0}
		hold := fill(t, decodePool.Limit()-1<<20) // leaves 1 MB
		go func() {
			time.Sleep(700 * time.Millisecond)
			hold.Release()
		}()
		loaded, _ := drawUntil(t, c, k, 20*time.Second, mustNotFail)
		if !loaded {
			t.Fatalf("the image didn't load once the pressure ended (failed: %+v)", c.failed[k])
		}
		if n := fetches.Load(); n != 1 {
			t.Errorf("fetched %d times across its busy tries, want 1", n)
		}
	})
}

// TestSustainedPressureNoEndlessSpinner: under pressure that doesn't end,
// an image stops being "still loading" after busyTries busy tries (Failed:
// the lightbox shows its thumbnail, a message its placeholder) instead of
// spinning for good, and loads once the pressure ends, with the bytes it
// fetched first.
func TestSustainedPressureNoEndlessSpinner(t *testing.T) {
	data := pngSolid(1024, 1024, pngRGBA8, false, nil)
	withTestPool(16<<20, func() {
		c, fetches := countingCache(data)
		k := key{url: "https://files.slack.com/files-pri/T1-F3/shot.png", size: 0}
		hold := fill(t, decodePool.Limit()-1<<20)
		loaded, failed := drawUntil(t, c, k, 15*time.Second, untilFailed)
		if loaded {
			t.Fatal("the image loaded while the pool stayed full")
		}
		if !failed {
			t.Fatalf("under pressure that doesn't end the image was still loading after 15 s: %+v", c.failed[k])
		}
		if f := c.failed[k]; f.busy || f.final {
			t.Errorf("past its busy tries: %+v, want failed (not busy) and not final", f)
		}
		hold.Release()
		if loaded, _ := drawUntil(t, c, k, 20*time.Second, throughFailed); !loaded {
			t.Fatalf("the image didn't load once the pressure ended: %+v", c.failed[k])
		}
		if n := fetches.Load(); n != 1 {
			t.Errorf("fetched %d times, want 1 (busy tries keep the bytes)", n)
		}
	})
}

// TestBusyRetryDoesntRefetch: a load that fails busy keeps the bytes it
// fetched, and its next try decodes them without downloading again.
// Remove the held bytes and the second load fetches.
func TestBusyRetryDoesntRefetch(t *testing.T) {
	data := pngSolid(512, 512, pngRGBA8, false, nil)
	withTestPool(8<<20, func() {
		c, fetches := countingCache(data)
		k := key{url: "https://files.slack.com/files-pri/T1-F4/a.png", size: 0}
		hold := fill(t, decodePool.Limit())
		c.load(k)
		if f := c.failed[k]; !f.busy {
			t.Fatalf("with the pool held full the load recorded %+v, want busy", f)
		}
		if _, ok := c.held[k]; !ok {
			t.Fatal("a busy load didn't keep its bytes")
		}
		hold.Release()
		c.load(k)
		if _, ok := c.entries[k]; !ok {
			t.Fatalf("the retry didn't load: %+v", c.failed[k])
		}
		if n := fetches.Load(); n != 1 {
			t.Errorf("downloaded %d times for one busy failure and its retry, want 1", n)
		}
		if _, ok := c.held[k]; ok {
			t.Error("the loaded image's bytes are still held")
		}
	})
}

// TestOverCapIsFailedNotBusy: a decode needing more than one decode may
// hold fails at once as ErrOverCap (an ErrTooBig, which the lightbox
// shows the file's thumbnail for), not busy, and is not tried again
// shortly.
func TestOverCapIsFailedNotBusy(t *testing.T) {
	// a progressive 4:4:4 header of 6000×6000: 108 MB of image and 216 MB
	// of int16 coefficients, past MaxDecode
	data := jpegHeader(6000, 6000, 3, true)
	c, _ := countingCache(data)
	k := key{url: "https://files.slack.com/files-pri/T1-F5/huge.jpg", size: 0}
	c.load(k)
	f, ok := c.failed[k]
	if !ok || f.busy {
		t.Fatalf("an over-cap decode recorded %+v, want a failure that isn't busy", f)
	}
	if !c.Failed(k.url, k.size) {
		t.Error("Failed is false for an over-cap original: the lightbox wouldn't show the thumbnail")
	}
	b := NewBudget()
	defer b.Release()
	if _, err := decodeImage(data, MaxPixels, MaxSide, b); !errors.Is(err, ErrOverCap) || !errors.Is(err, ErrTooBig) {
		t.Errorf("decoding it: %v, want ErrOverCap (an ErrTooBig)", err)
	}
	if d := time.Until(f.retry); d < RetryPermanent/2 {
		t.Errorf("an over-cap original is tried again in %v; the same bytes need the same memory", d)
	}
}

// TestHeldBytesInCacheBudget: a busy load's kept bytes count in the cache's
// byte budget (as its images do), at most 1/heldShare of it, and go when
// the image loads; a file bigger than that share isn't kept (its next try
// downloads it again). So kept bytes can't stack on a full decode pool
// beyond what the cache may hold anyway.
func TestHeldBytesInCacheBudget(t *testing.T) {
	data := pngSolid(512, 512, pngRGBA8, false, nil)
	withTestPool(8<<20, func() {
		c, _ := countingCache(data)
		k := key{url: "https://files.slack.com/files-pri/T1-F6/a.png", size: 0}
		hold := fill(t, decodePool.Limit())
		c.load(k)
		if c.heldBytes != len(data) || c.bytes != len(data) {
			t.Errorf("a busy load's %d kept bytes: held %d, cache bytes %d, want both %d", len(data), c.heldBytes, c.bytes, len(data))
		}
		hold.Release()
		c.load(k)
		e := c.entryLocked(k)
		if e == nil {
			t.Fatalf("the retry didn't load: %+v", c.failed[k])
		}
		if c.heldBytes != 0 || c.bytes != e.n {
			t.Errorf("once loaded: held %d, cache bytes %d, want 0 and the image's %d", c.heldBytes, c.bytes, e.n)
		}
		c.SetLimit(heldShare*len(data) - 1) // its share is now under the file
		hold = fill(t, decodePool.Limit())
		k2 := key{url: "https://files.slack.com/files-pri/T1-F7/b.png", size: 0}
		c.load(k2)
		if _, ok := c.held[k2]; ok || c.heldBytes != 0 {
			t.Errorf("a file past the held share was kept (%d bytes held)", c.heldBytes)
		}
		hold.Release()
	})
}
