package imgcache

// A process-wide weighted limit on the memory image decodes hold at once.
//
// Each decode's own budget is bounded (webpanim.go's webpBudget for WebP,
// the header's pixel size for PNG/JPEG/GIF), but nothing bounded the sum:
// up to laneWorkers×2 lane decodes, bigDecodes big-image decodes and the
// animation players could each hold a full budget — about 58 MB apiece at
// 1 MP, up to the ceiling — so a few dozen crafted uploads (each a few KB
// of lossless WebP that allocates tens of MB before it fails) could drive
// the process past cmd/clack's soft memoryLimit (768 MiB) all at once.
//
// decMem is a weighted semaphore every decode reserves its budget's weight
// from before it allocates and releases when it ends, so the running
// decodes together never reserve more than decCap. A reservation larger
// than the whole cap can never be admitted, so acquire fails it at once
// (ErrDecodeBudget) rather than waiting for memory that will never free;
// DecodeWithin clamps each budget to decCap so in practice this is the
// decode failing within the cap, not an OOM.

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"sync"
)

// decCap is the most memory all image decodes running at once may reserve:
// a fixed share of cmd/clack/trim.go's memoryLimit (768 MiB), chosen so
// the decoders together leave the session's own heap (messages, documents,
// plugin state) room under the limit even when every lane worker, big
// decode and animation player is busy.
const decCap = 256 << 20

// ErrDecodeBudget is a decode whose memory budget is larger than the whole
// decCap. It can never be admitted whatever else stops, so acquire returns
// it immediately instead of blocking forever.
var ErrDecodeBudget = errors.New("imgcache: decode needs more memory than the decode cap")

// decMem is the one process-wide limiter (every Cache shares it, as they
// share the heap).
var decMem = &decLimiter{cap: decCap}

// decLimiter is a weighted semaphore: acquire reserves bytes (waiting in
// FIFO order, honouring ctx), release returns them and wakes the waiters
// that now fit. The front waiter blocks the rest so a steady stream of
// small decodes can't starve a large one. Modelled on
// golang.org/x/sync/semaphore, except an over-cap request fails at once
// rather than waiting for a cancellation.
type decLimiter struct {
	mu      sync.Mutex
	cap     int64
	used    int64
	waiters list.List // of decWaiter, front = next to be granted
}

type decWaiter struct {
	n     int64
	ready chan struct{} // closed when granted
}

// acquire reserves n bytes, blocking until they fit or ctx is done. It
// fails at once with ErrDecodeBudget when n is larger than the whole cap
// (no amount of waiting could admit it). n ≤ 0 reserves nothing.
func (l *decLimiter) acquire(ctx context.Context, n int64) error {
	if n <= 0 {
		return nil
	}
	done := ctx.Done()
	l.mu.Lock()
	if n > l.cap {
		l.mu.Unlock()
		return fmt.Errorf("%w: %d bytes, cap %d", ErrDecodeBudget, n, l.cap)
	}
	select {
	case <-done:
		l.mu.Unlock()
		return ctx.Err()
	default:
	}
	if l.used+n <= l.cap && l.waiters.Len() == 0 {
		l.used += n
		l.mu.Unlock()
		return nil
	}
	w := decWaiter{n: n, ready: make(chan struct{})}
	elem := l.waiters.PushBack(w)
	l.mu.Unlock()

	select {
	case <-done:
		l.mu.Lock()
		select {
		case <-w.ready:
			// granted after ctx was done: hand it back as if never granted
			l.used -= n
			l.notifyLocked()
		default:
			isFront := l.waiters.Front() == elem
			l.waiters.Remove(elem)
			if isFront && l.used < l.cap {
				l.notifyLocked()
			}
		}
		l.mu.Unlock()
		return ctx.Err()
	case <-w.ready:
		select {
		case <-done:
			l.release(n) // acquired as ctx went done: give it straight back
			return ctx.Err()
		default:
		}
		return nil
	}
}

// decAcquireErr maps an acquire failure to the error a decoder returns. A
// reservation larger than the whole cap is a too-big image (ErrTooBig, so
// callers treat it like any oversize image and never retry it forever); a
// cancelled or deadlined context passes through unchanged.
func decAcquireErr(err error) error {
	if errors.Is(err, ErrDecodeBudget) {
		return fmt.Errorf("%w: %w", ErrTooBig, err)
	}
	return err
}

// release returns n bytes and wakes the waiters that now fit.
func (l *decLimiter) release(n int64) {
	if n <= 0 {
		return
	}
	l.mu.Lock()
	l.used -= n
	if l.used < 0 {
		l.mu.Unlock()
		panic("imgcache: decode limiter released more than acquired")
	}
	l.notifyLocked()
	l.mu.Unlock()
}

// notifyLocked grants the front waiters while they fit. The front one
// blocks the rest: once it doesn't fit, nothing behind it is granted, so a
// large reservation is never starved by a run of small ones.
func (l *decLimiter) notifyLocked() {
	for {
		el := l.waiters.Front()
		if el == nil {
			return
		}
		w := el.Value.(decWaiter)
		if l.used+w.n > l.cap {
			return
		}
		l.used += w.n
		l.waiters.Remove(el)
		close(w.ready)
	}
}
