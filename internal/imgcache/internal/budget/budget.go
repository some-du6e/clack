// Package budget is the memory image decoding may allocate. Clack's own,
// not vendored (CLACK_PATCHES.md).
//
// Every decoder imgcache runs (the vendored image/jpeg, image/png,
// image/gif, x/image bmp, webp and vp8l in this directory), the scaler
// (catmullrom), the animation canvases and Copy Image's clipboard copies
// charge a Budget before each allocation whose size the bytes decide. A
// root Budget draws every charge from a process-wide Pool at once, with a
// non-blocking try-acquire: nothing ever waits for memory, so there is no
// queue, no head-of-line blocking and no deadlock. A charge the Pool can't
// cover right now fails with ErrBusy (other decodes hold the memory: try
// again shortly); one past the Budget's own limit fails with ErrBudget
// (the same bytes always need the same memory: final). Release gives
// everything back once the decode, and the scale or conversion that
// follows it, is done.
//
// Nothing is returned when a decoder frees memory mid-decode, so what a
// decode charges is at least everything it allocates over its life, not
// just its peak: a Pool's limit bounds what all the decodes in flight
// allocate together.
package budget

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
)

// ErrBudget is a decode that needs more than its Budget's limit. The
// decoder returns it before the allocation that doesn't fit; the same bytes
// would fail the same way again.
var ErrBudget = errors.New("decode needs more memory than its budget")

// ErrBusy is a charge the Pool can't cover now: other decodes in flight
// hold the memory. It isn't the bytes' fault; the same decode succeeds once
// they're done.
var ErrBusy = errors.New("decode memory is in use by other decodes")

// Pool is the memory every root Budget drawing from it holds together, at
// most Limit bytes. Its last Reserve bytes are kept for small budgets (those
// holding at most Small bytes, emoji and avatars), so a few big decodes in
// flight can't make every small one fail.
type Pool struct {
	limit, reserve, small int64
	used, peak            atomic.Int64
}

// NewPool returns a pool of limit bytes whose last reserve bytes only
// budgets holding at most small bytes may take.
func NewPool(limit, reserve, small int64) *Pool {
	reserve = min(max(reserve, 0), limit)
	return &Pool{limit: limit, reserve: reserve, small: small}
}

// Limit is the most the pool's budgets hold together.
func (p *Pool) Limit() int64 { return p.limit }

// MaxBudget is the most one budget can draw: the limit less the reserve.
func (p *Pool) MaxBudget() int64 { return p.limit - p.reserve }

// Used is what the pool's budgets hold now.
func (p *Pool) Used() int64 { return p.used.Load() }

// Peak is the most they held at once since ResetPeak.
func (p *Pool) Peak() int64 { return p.peak.Load() }

// ResetPeak starts Peak again from what's held now.
func (p *Pool) ResetPeak() { p.peak.Store(p.used.Load()) }

// take draws n bytes for a budget that will then hold total, or reports
// false, taking nothing, when they don't fit.
func (p *Pool) take(n, total int64) bool {
	ceiling := p.limit - p.reserve
	if total <= p.small {
		ceiling = p.limit
	}
	for {
		u := p.used.Load()
		if n > ceiling-u {
			return false
		}
		if p.used.CompareAndSwap(u, u+n) {
			for pk := p.peak.Load(); u+n > pk && !p.peak.CompareAndSwap(pk, u+n); pk = p.peak.Load() {
			}
			return true
		}
	}
}

func (p *Pool) give(n int64) { p.used.Add(-n) }

// Budget is what one decode (and the scale or conversion after it) may
// allocate. A root budget draws from a Pool; a child (Sub) charges its
// parent too, under a tighter limit of its own; a local one (NewLocal)
// draws from nothing, a bare allowance. A nil *Budget allows nothing.
// Its methods are safe for concurrent use.
type Budget struct {
	mu     sync.Mutex
	pool   *Pool
	parent *Budget
	limit  int64
	used   int64
	closed bool
}

// New returns a root budget of at most limit bytes drawing from p (and no
// more than p.MaxBudget()).
func New(p *Pool, limit int64) *Budget {
	return &Budget{pool: p, limit: min(limit, p.MaxBudget())}
}

// NewLocal returns a budget of limit bytes that draws from no pool: a
// per-decode allowance only (header reads, tests).
func NewLocal(limit int64) *Budget { return &Budget{limit: limit} }

// Unlimited is a local budget with no limit: for work that's bounded some
// other way (a small image on the UI goroutine).
func Unlimited() *Budget { return NewLocal(math.MaxInt64) }

// Sub returns a child of b with a limit of its own: what it charges counts
// against both, and draws from b's pool. Releasing the child gives back only
// what it charged; releasing b first takes the child's charges with it.
func (b *Budget) Sub(limit int64) *Budget {
	return &Budget{parent: b, limit: limit}
}

// Charge takes n bytes, before they're allocated. It fails, taking
// nothing, with an error wrapping ErrBudget when b (or a parent) has fewer
// than n left of its limit, or wrapping ErrBusy when its pool can't cover
// them now.
func (b *Budget) Charge(n int64) error {
	if b == nil {
		return fmt.Errorf("%w: no budget", ErrBudget)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return fmt.Errorf("%w: released", ErrBudget)
	}
	if n < 0 || n > b.limit-b.used {
		return fmt.Errorf("%w: %d bytes more, %d of %d used", ErrBudget, n, b.used, b.limit)
	}
	switch {
	case b.parent != nil:
		if err := b.parent.Charge(n); err != nil {
			return err
		}
	case b.pool != nil:
		if !b.pool.take(n, b.used+n) {
			return fmt.Errorf("%w: %d bytes more, %d of %d held", ErrBusy, n, b.pool.Used(), b.pool.limit)
		}
	}
	b.used += n
	return nil
}

// Used is how many bytes b holds: everything charged since it was made.
func (b *Budget) Used() int64 {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.used
}

// Limit is the most b may hold.
func (b *Budget) Limit() int64 {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.limit
}

// Left is how many more bytes b's own limit allows.
func (b *Budget) Left() int64 {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.limit - b.used
}

// Release gives back everything b holds (to its parent, or its pool) and
// closes it: a charge after it fails. Releasing twice, or a child after its
// parent, does nothing more.
func (b *Budget) Release() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	n := b.used
	b.used = 0
	switch {
	case n == 0:
	case b.parent != nil:
		b.parent.uncharge(n)
	case b.pool != nil:
		b.pool.give(n)
	}
}

// uncharge gives back n bytes a child charged through b.
func (b *Budget) uncharge(n int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed { // released already, the child's bytes with it
		return
	}
	b.used -= n
	switch {
	case b.parent != nil:
		b.parent.uncharge(n)
	case b.pool != nil:
		b.pool.give(n)
	}
}

// IsBusy reports whether err is a charge the pool couldn't cover now.
func IsBusy(err error) bool { return errors.Is(err, ErrBusy) }

// IsOver reports whether err is a decode past its budget's limit.
func IsOver(err error) bool { return errors.Is(err, ErrBudget) }
