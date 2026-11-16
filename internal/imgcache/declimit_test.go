package imgcache

// Tests for the process-wide weighted decode limiter (declimit.go) and the
// decode entry points reserving from it. Synthetic images only (webptest,
// pngBytes); nothing here comes from Slack.

import (
	"context"
	"errors"
	"image/color"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/imgcache/webptest"
)

func (l *decLimiter) usedNow() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.used
}

// blocks reports whether f does not return within d (it's waiting).
func blocks(d time.Duration, f func() error) (chan error, bool) {
	done := make(chan error, 1)
	go func() { done <- f() }()
	select {
	case err := <-done:
		done <- err // put it back for the caller
		return done, false
	case <-time.After(d):
		return done, true
	}
}

// A reservation larger than the whole cap can never be admitted, so it must
// fail at once (ErrDecodeBudget) rather than block on a context that may
// never cancel — the "doesn't wait forever" requirement.
func TestDecLimiterOverCapFailsFast(t *testing.T) {
	l := &decLimiter{cap: 100}
	done, blocked := blocks(2*time.Second, func() error { return l.acquire(context.Background(), 101) })
	if blocked {
		t.Fatal("acquire of more than the whole cap blocked instead of failing")
	}
	if err := <-done; !errors.Is(err, ErrDecodeBudget) {
		t.Errorf("acquire(101) under a 100 cap: %v, want ErrDecodeBudget", err)
	}
	if u := l.usedNow(); u != 0 {
		t.Errorf("a failed over-cap acquire reserved %d bytes", u)
	}
}

// The sum of live reservations never exceeds the cap: a second reservation
// that would overflow it waits for the first to release, so the two are
// never both held at once.
func TestDecLimiterBoundsSum(t *testing.T) {
	l := &decLimiter{cap: 100}
	if err := l.acquire(context.Background(), 70); err != nil {
		t.Fatalf("acquire 70: %v", err)
	}
	done, blocked := blocks(150*time.Millisecond, func() error { return l.acquire(context.Background(), 70) })
	if !blocked {
		t.Fatalf("a second 70-byte reservation didn't wait (70+70 > 100 cap): %v", <-done)
	}
	l.release(70) // the first finishes
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("the waiting reservation after room freed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the waiting reservation never woke after release")
	}
	l.release(70)
	if u := l.usedNow(); u != 0 {
		t.Errorf("used = %d after both released, want 0", u)
	}
}

// A blocked acquire returns its context's error when cancelled and reserves
// nothing, so a cancelled decode doesn't hold the cap.
func TestDecLimiterHonoursCancel(t *testing.T) {
	l := &decLimiter{cap: 100}
	if err := l.acquire(context.Background(), 100); err != nil {
		t.Fatalf("fill the cap: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done, blocked := blocks(100*time.Millisecond, func() error { return l.acquire(ctx, 50) })
	if !blocked {
		t.Fatalf("acquire with the cap full didn't wait: %v", <-done)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("cancelled acquire: %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled acquire never returned")
	}
	if u := l.usedNow(); u != 100 {
		t.Errorf("used = %d after a cancelled waiter, want 100 (it reserved nothing)", u)
	}
	l.release(100)
}

// Decode reserves its budget from the process cap before it allocates: with
// the cap full the decode waits, and proceeds once room is freed.
func TestDecodeWaitsForCap(t *testing.T) {
	data := webptest.Simple(webptest.Chunk("VP8L", webptest.VP8LSolid(8, 8, color.NRGBA{1, 2, 3, 255})))
	if err := decMem.acquire(context.Background(), decCap); err != nil {
		t.Fatalf("filling the cap: %v", err)
	}
	released := false
	defer func() {
		if !released {
			decMem.release(decCap)
		}
	}()
	done := make(chan error, 1)
	go func() { _, err := Decode(data); done <- err }()
	select {
	case err := <-done:
		t.Fatalf("decode finished with the cap full (%v): it didn't reserve from the cap", err)
	case <-time.After(150 * time.Millisecond):
	}
	decMem.release(decCap)
	released = true
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("decode after the cap was freed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("decode never finished after the cap was freed")
	}
	if u := decMem.usedNow(); u != 0 {
		t.Errorf("decMem.used = %d after the decode, want 0", u)
	}
}

// Copy Image (DecodeClipboard) decodes a real large photo that the cache's
// own Decode refuses — wider than MaxSide, or over MaxPixels — charging its
// natural size to the cap, while a genuine bomb is still refused within the
// cap (not an OOM).
func TestCopyImageDecodeRestoresLargePhotos(t *testing.T) {
	ctx := context.Background()
	wide := pngBytes(17000, 8) // 17000 > MaxSide (16384)
	if _, err := Decode(wide); !errors.Is(err, ErrTooBig) {
		t.Errorf("cache Decode of a 17000px-wide PNG: %v, want ErrTooBig", err)
	}
	m, err := DecodeClipboard(ctx, wide)
	if err != nil {
		t.Fatalf("DecodeClipboard of a 17000px-wide PNG: %v (Copy Image must still copy it)", err)
	}
	if b := m.Bounds(); b.Dx() != 17000 || b.Dy() != 8 {
		t.Errorf("decoded %v, want 17000x8", b)
	}
	// a tiny PNG header claiming 100000×100000 is a bomb: over photoMaxPixels,
	// refused before any large allocation.
	if _, err := DecodeClipboard(ctx, pngHeader(100000, 100000)); !errors.Is(err, ErrTooBig) {
		t.Errorf("DecodeClipboard of a 100000² PNG bomb: %v, want ErrTooBig", err)
	}
}
