package imgcache

// Tests for the two-pool decode limiter (declimit.go) and the decode entry
// points reserving from it. Synthetic images only (pngBytes, webptest);
// nothing here comes from Slack.

import (
	"context"
	"errors"
	"testing"
	"time"
)

func (p *decPool) usedNow() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.used
}

// blocks reports whether f hasn't returned within d; it returns a channel
// carrying f's eventual result either way.
func blocks(d time.Duration, f func() error) (chan error, bool) {
	done := make(chan error, 1)
	go func() { done <- f() }()
	select {
	case err := <-done:
		done <- err
		return done, false
	case <-time.After(d):
		return done, true
	}
}

// A reservation larger than the whole pool can never be admitted: it must
// fail at once (ErrDecodeBudget), not block on a context that may never
// cancel.
func TestDecPoolOverCapFailsFast(t *testing.T) {
	p := &decPool{cap: 100}
	done, blocked := blocks(2*time.Second, func() error { return p.acquire(context.Background(), 101) })
	if blocked {
		t.Fatal("acquire of more than the whole pool blocked instead of failing")
	}
	if err := <-done; !errors.Is(err, ErrDecodeBudget) {
		t.Errorf("acquire(101) under a 100 pool: %v, want ErrDecodeBudget", err)
	}
	if u := p.usedNow(); u != 0 {
		t.Errorf("a failed over-cap acquire reserved %d bytes", u)
	}
}

// No head-of-line blocking: a reservation that fits is admitted at once
// even while a larger one waits ahead of it, so a small decode never stalls
// behind a big one.
func TestDecPoolNoHeadOfLine(t *testing.T) {
	old := decStarve
	decStarve = time.Hour // never bar: pure barging
	defer func() { decStarve = old }()

	p := &decPool{cap: 100}
	if err := p.acquire(context.Background(), 60); err != nil {
		t.Fatalf("acquire 60: %v", err)
	}
	big, blocked := blocks(150*time.Millisecond, func() error { return p.acquire(context.Background(), 50) })
	if !blocked {
		t.Fatalf("a 50 reservation didn't wait with 60 of 100 held: %v", <-big)
	}
	// a 30 that fits the remaining 40 barges past the waiting 50
	small, smallBlocked := blocks(150*time.Millisecond, func() error { return p.acquire(context.Background(), 30) })
	if smallBlocked {
		t.Fatal("a fitting 30 reservation waited behind the larger 50 (head-of-line block)")
	}
	if err := <-small; err != nil {
		t.Fatalf("the small reservation: %v", err)
	}
	p.release(60)
	p.release(30)
	select {
	case err := <-big:
		if err != nil {
			t.Errorf("the big waiter after room freed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the big waiter never woke")
	}
	p.release(50)
}

// The starvation guard: once the front waiter has waited long enough
// (decStarve, forced to 0 here), newcomers queue behind it instead of
// barging, so it isn't passed forever; on release it is served first.
func TestDecPoolStarvationGuard(t *testing.T) {
	old := decStarve
	decStarve = 0 // the front waiter is barred at once
	defer func() { decStarve = old }()

	p := &decPool{cap: 100}
	if err := p.acquire(context.Background(), 60); err != nil {
		t.Fatalf("acquire 60: %v", err)
	}
	a, _ := blocks(50*time.Millisecond, func() error { return p.acquire(context.Background(), 50) }) // front waiter
	// a 30 fits the free 40, but the barred front waiter blocks it
	b, bBlocked := blocks(150*time.Millisecond, func() error { return p.acquire(context.Background(), 30) })
	if !bBlocked {
		t.Fatal("a newcomer barged past a starved front waiter (starvation guard missing)")
	}
	p.release(60) // frees 60: the front 50 is served, then the 30
	for _, w := range []struct {
		name string
		ch   chan error
	}{{"front 50", a}, {"the 30", b}} {
		select {
		case err := <-w.ch:
			if err != nil {
				t.Errorf("%s: %v", w.name, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s never woke", w.name)
		}
	}
	p.release(50)
	p.release(30)
	if u := p.usedNow(); u != 0 {
		t.Errorf("used %d after all released, want 0", u)
	}
}

// Cancelling the front waiter wakes a waiter behind it that now fits,
// without needing an unrelated release.
func TestDecPoolCancelFrontWakes(t *testing.T) {
	old := decStarve
	decStarve = 0 // force both to queue (no barging)
	defer func() { decStarve = old }()

	p := &decPool{cap: 100}
	if err := p.acquire(context.Background(), 50); err != nil {
		t.Fatalf("acquire 50: %v", err)
	}
	ctxA, cancelA := context.WithCancel(context.Background())
	aErr := make(chan error, 1)
	go func() { aErr <- p.acquire(ctxA, 60) }() // front: 50+60 > 100, waits
	time.Sleep(50 * time.Millisecond)
	b, bBlocked := blocks(100*time.Millisecond, func() error { return p.acquire(context.Background(), 30) }) // 50+30 ≤ 100 but barred
	if !bBlocked {
		t.Fatal("the 30 barged past the barred front waiter")
	}
	cancelA() // front leaves; the 30 now fits and must be woken
	if err := <-aErr; !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled front waiter: %v, want context.Canceled", err)
	}
	select {
	case err := <-b:
		if err != nil {
			t.Errorf("the 30 after the front was cancelled: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelling the front waiter didn't wake the fitting waiter behind it")
	}
	p.release(50)
	p.release(30)
}

// The two pools are independent: a full big pool never blocks a small
// reservation, so avatars and emoji load while a big image decodes.
func TestDecPoolsIndependent(t *testing.T) {
	relBig, err := decReserve(context.Background(), decBigCap) // fill the big pool
	if err != nil {
		t.Fatalf("reserve the whole big pool: %v", err)
	}
	defer relBig()
	done, blocked := blocks(200*time.Millisecond, func() error {
		r, err := decReserve(context.Background(), 1<<20) // a small decode
		if r != nil {
			r()
		}
		return err
	})
	if blocked {
		t.Fatal("a small reservation stalled behind a full big pool")
	}
	if err := <-done; err != nil {
		t.Errorf("small reservation with the big pool full: %v", err)
	}
}

// A still PNG decode reserves from the pool before it allocates: with the
// small pool full a small PNG's decode waits, and fails to reserve under a
// cancelled context.
func TestDecodePNGReserves(t *testing.T) {
	// fill the small pool directly (decReserve routes decSmallCap, which is
	// larger than decSmallMax, to the big pool)
	if err := decSmall.acquire(context.Background(), decSmallCap); err != nil {
		t.Fatalf("fill the small pool: %v", err)
	}
	defer decSmall.release(decSmallCap)
	ctx, cancel := context.WithCancel(context.Background())
	data := pngBytes(8, 8) // tiny → small pool, blocks while it's full
	done, blocked := blocks(150*time.Millisecond, func() error {
		_, err := DecodeCtx(ctx, data)
		return err
	})
	if !blocked {
		t.Fatal("a PNG decode didn't reserve from the pool (completed while it was full)")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("PNG decode cancelled while reserving: %v, want context.Canceled", err)
	}
}

// Copy Image (DecodeClipboard) still copies a real large photo the cache's
// own Decode refuses — wider than MaxSide, or over MaxPixels — and holds a
// reservation across the clipboard conversion; a genuine bomb is refused.
func TestCopyImageDecodeRestoresLargePhotos(t *testing.T) {
	ctx := context.Background()
	wide := pngBytes(17000, 8) // 17000 > MaxSide (16384)
	if _, err := Decode(wide); !errors.Is(err, ErrTooBig) {
		t.Errorf("cache Decode of a 17000px-wide PNG: %v, want ErrTooBig", err)
	}
	m, release, err := DecodeClipboard(ctx, wide)
	if err != nil {
		t.Fatalf("DecodeClipboard of a 17000px-wide PNG: %v (Copy Image must still copy it)", err)
	}
	if b := m.Bounds(); b.Dx() != 17000 || b.Dy() != 8 {
		t.Errorf("decoded %v, want 17000x8", b)
	}
	release()
	// a tiny PNG header claiming 100000×100000 is a bomb: over photoMaxPixels.
	if _, _, err := DecodeClipboard(ctx, pngHeader(100000, 100000)); !errors.Is(err, ErrTooBig) {
		t.Errorf("DecodeClipboard of a 100000² PNG bomb: %v, want ErrTooBig", err)
	}
}
