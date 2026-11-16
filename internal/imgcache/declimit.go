package imgcache

// A process-wide limit on the memory image decodes hold at once, so a few
// crafted uploads can't drive the process past cmd/clack's soft memoryLimit
// (768 MiB). Each decode reserves, from its header, what it will really
// allocate (decweight.go) before it allocates, held for the whole lifetime
// of that memory — through the scale and the clipboard copies, not just the
// decode call — and releases at the end.
//
// Two pools keep a big or slow decode from stalling the small ones. Avatars
// and emoji (a few MB) reserve from decSmall, which a message image, unfurl
// or Copy Image of a photo (tens to hundreds of MB, from decBig) can never
// fill. Within a pool a reservation that fits is admitted at once, even
// past an earlier larger waiter (no head-of-line blocking), but once that
// waiter has been kept waiting decStarve it is no longer passed, so it gets
// its turn within a bounded time.

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Pool sizes. A reservation of at most decSmallMax goes to the small pool,
// anything larger (up to decBigCap) to the big pool; the two caps together
// (320 MiB) leave the session's own heap room under the 768 MiB soft limit
// even when both pools are full. A reservation larger than the big pool can
// never be admitted and fails at once.
const (
	decSmallCap = 64 << 20
	decSmallMax = 16 << 20
	decBigCap   = 256 << 20
	// decMaxDecode is the largest a single decode may reserve (the big
	// pool's cap); a WebP budget and any other weight is clamped to it.
	decMaxDecode = decBigCap
)

// decStarve is how long a waiter may be passed by later fitting
// reservations before newcomers must queue behind it instead (the
// starvation guard). A var so tests can exercise the barred path.
var decStarve = 2 * time.Second

// ErrDecodeBudget is a decode whose memory weight is larger than the whole
// big pool. It can never be admitted whatever else stops, so reserving it
// returns this at once instead of blocking forever.
var ErrDecodeBudget = errors.New("imgcache: decode needs more memory than the decode cap")

var (
	decSmall = &decPool{cap: decSmallCap}
	decBig   = &decPool{cap: decBigCap}
)

// decReserve reserves n bytes for a decode from the pool its size picks,
// waiting (honouring ctx) until they fit, and returns a release to call
// when the decode's memory is freed. n ≤ 0 reserves nothing. A weight over
// the big pool fails at once with ErrDecodeBudget.
func decReserve(ctx context.Context, n int64) (release func(), err error) {
	if n <= 0 {
		return func() {}, nil
	}
	pool := decBig
	if n <= decSmallMax {
		pool = decSmall
	}
	if err := pool.acquire(ctx, n); err != nil {
		return nil, err
	}
	var once sync.Once
	return func() { once.Do(func() { pool.release(n) }) }, nil
}

// decPool is a weighted semaphore (one of the two pools). A reservation
// that fits is admitted immediately, barging past waiters, unless the front
// waiter has starved (waited decStarve), in which case newcomers queue
// behind it. On release the front waiters are granted while they fit.
type decPool struct {
	mu      sync.Mutex
	cap     int64
	used    int64
	waiters list.List // of *decWaiter, front = longest-waiting
}

type decWaiter struct {
	n     int64
	ready chan struct{}
	since time.Time
}

func (p *decPool) acquire(ctx context.Context, n int64) error {
	done := ctx.Done()
	p.mu.Lock()
	if n > p.cap {
		p.mu.Unlock()
		return fmt.Errorf("%w: %d bytes, pool %d", ErrDecodeBudget, n, p.cap)
	}
	select {
	case <-done:
		p.mu.Unlock()
		return ctx.Err()
	default:
	}
	if p.used+n <= p.cap && !p.barredLocked() {
		p.used += n
		p.mu.Unlock()
		return nil
	}
	w := &decWaiter{n: n, ready: make(chan struct{}), since: time.Now()}
	el := p.waiters.PushBack(w)
	p.mu.Unlock()

	select {
	case <-done:
		p.mu.Lock()
		select {
		case <-w.ready:
			// granted as ctx went done: hand it back as if never granted
			p.used -= n
			p.notifyLocked()
		default:
			isFront := p.waiters.Front() == el
			p.waiters.Remove(el)
			if isFront {
				p.notifyLocked() // a newly-front waiter may now fit
			}
		}
		p.mu.Unlock()
		return ctx.Err()
	case <-w.ready:
		select {
		case <-done:
			p.release(n)
			return ctx.Err()
		default:
		}
		return nil
	}
}

// barredLocked reports whether the front waiter has waited long enough that
// newcomers must stop barging past it (the starvation guard).
func (p *decPool) barredLocked() bool {
	el := p.waiters.Front()
	if el == nil {
		return false
	}
	return time.Since(el.Value.(*decWaiter).since) >= decStarve
}

func (p *decPool) release(n int64) {
	if n <= 0 {
		return
	}
	p.mu.Lock()
	p.used -= n
	if p.used < 0 {
		p.mu.Unlock()
		panic("imgcache: decode pool released more than acquired")
	}
	p.notifyLocked()
	p.mu.Unlock()
}

// notifyLocked grants the front waiters while they fit. It stops at the
// first that doesn't, so a large front reservation is served before the
// smaller ones behind it (which barging let in ahead of it earlier).
func (p *decPool) notifyLocked() {
	for {
		el := p.waiters.Front()
		if el == nil {
			return
		}
		w := el.Value.(*decWaiter)
		if p.used+w.n > p.cap {
			return
		}
		p.used += w.n
		p.waiters.Remove(el)
		close(w.ready)
	}
}

// decAcquireErr maps a reservation failure to the error a decoder returns:
// a weight over the pool is a too-big image (ErrTooBig, so callers treat it
// like any oversize image); a cancelled context passes through unchanged.
func decAcquireErr(err error) error {
	if errors.Is(err, ErrDecodeBudget) {
		return fmt.Errorf("%w: %w", ErrTooBig, err)
	}
	return err
}
