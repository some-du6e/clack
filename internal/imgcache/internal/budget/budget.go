// Package budget is the memory image decoding may allocate. Clack's own,
// not vendored (CLACK_PATCHES.md).
//
// Every decoder imgcache runs (the vendored image/jpeg, image/png,
// image/gif, x/image bmp, webp and vp8l in this directory), the scaler
// (catmullrom), the animation canvases and Copy Image's clipboard copies
// charge a Budget before each allocation whose size the bytes decide. A
// root Budget draws every charge from a process-wide Pool at once: what
// all the budgets hold together never passes the pool's limit. A charge
// past the Budget's own limit fails with ErrBudget (the same bytes always
// need the same memory); one the pool can't cover fails with ErrBusy
// (other decodes hold the memory: try again shortly). Release gives
// everything back once the decode, and the scale or conversion that
// follows it, is done; Free gives back a buffer the decoder no longer
// references (the garbage collector reclaims it), so a decode holds what's
// live, not everything it ever allocated.
//
// Small decodes (a budget holding at most the pool's Small bytes: emoji,
// avatars, thumbnails) never wait: a charge either fits now or fails with
// ErrBusy, and the pool's last Reserve bytes are kept for them. A decode
// that grows past Small takes the pool's one big slot first, waiting for
// the big decodes before it, in order (no small decode ever waits for
// it): so at most one big decode competes for the pool at a time, beside
// small decodes and animation players. Holding the slot, a charge the pool
// can't cover now waits for memory to be given back, up to the pool's
// BigWait, before it fails with ErrBusy — but only when what the other
// decodes hold could cover it: memory players hold (NewPlayer: animation
// players, the GIF picker's kept frames) isn't given back while they
// play, so a shortfall only they could cover fails at once. Players never
// take the slot or wait, and nor do small-lane decodes (NewSmallContext:
// many at once, the GIF picker's previews), whose memory, unlike players',
// counts as coming back. A budget made with NewContext stops waiting (for
// the slot or for memory), and charges nothing more, once its context is
// done.
package budget

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// ErrBudget is a decode that needs more than its Budget's limit. The
// decoder returns it before the allocation that doesn't fit; the same bytes
// would fail the same way again.
var ErrBudget = errors.New("decode needs more memory than its budget")

// ErrBusy is a charge the Pool can't cover now: other decodes in flight
// hold the memory. It isn't the bytes' fault; the same decode succeeds once
// they're done.
var ErrBusy = errors.New("decode memory is in use by other decodes")

// DefaultBigWait is how long a big decode's charge waits for memory to be
// given back before it fails with ErrBusy.
const DefaultBigWait = 2 * time.Second

// Pool is the memory every root Budget drawing from it holds together, at
// most Limit bytes. Its last Reserve bytes are kept for small budgets (those
// holding at most Small bytes), so the big decode in flight can't make the
// small ones fail; a budget past Small takes the pool's big slot first.
type Pool struct {
	limit, reserve, small int64
	used, peak            atomic.Int64
	// players is what player budgets hold of used: memory that isn't
	// given back while they play
	players atomic.Int64

	// big is the big slot: a decode budget holding more than small has it
	// (one at a time; the next waits on the send, in order).
	big     chan struct{}
	bigWait time.Duration

	// waiters for memory to be given back (the slot holder's charges):
	// freed is closed, and replaced, when some is
	nwait atomic.Int32
	mu    sync.Mutex
	freed chan struct{}
}

// NewPool returns a pool of limit bytes whose last reserve bytes only
// budgets holding at most small bytes may take.
func NewPool(limit, reserve, small int64) *Pool {
	reserve = min(max(reserve, 0), limit)
	return &Pool{limit: limit, reserve: reserve, small: small, big: make(chan struct{}, 1), bigWait: DefaultBigWait}
}

// SetBigWait sets how long the big decode's charges wait for memory (tests).
func (p *Pool) SetBigWait(d time.Duration) { p.bigWait = d }

// Limit is the most the pool's budgets hold together.
func (p *Pool) Limit() int64 { return p.limit }

// MaxBudget is the most one budget can draw: the limit less the reserve.
func (p *Pool) MaxBudget() int64 { return p.limit - p.reserve }

// Small is how much a small budget holds at most.
func (p *Pool) Small() int64 { return p.small }

// Used is what the pool's budgets hold now.
func (p *Pool) Used() int64 { return p.used.Load() }

// Players is what player budgets hold of it.
func (p *Pool) Players() int64 { return p.players.Load() }

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

// give returns n bytes, waking a charge waiting for memory.
func (p *Pool) give(n int64) {
	p.used.Add(-n)
	if p.nwait.Load() > 0 {
		p.mu.Lock()
		if p.freed != nil {
			close(p.freed)
			p.freed = nil
		}
		p.mu.Unlock()
	}
}

// waitFreed is a channel closed the next time memory is given back; the
// caller calls doneWaiting after.
func (p *Pool) waitFreed() <-chan struct{} {
	p.nwait.Add(1)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.freed == nil {
		p.freed = make(chan struct{})
	}
	return p.freed
}

func (p *Pool) doneWaiting() { p.nwait.Add(-1) }

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
	player bool // never takes the big slot or waits; holds memory while it plays (Players)
	lane   bool // small lane: never takes the big slot or waits; gives its memory back
	slot   bool // holds the pool's big slot
	// ctx (NewContext) ends its waits and its charges once done
	ctx context.Context
}

// New returns a decode's root budget of at most limit bytes drawing from p
// (and no more than p.MaxBudget()). Past p.Small() it takes p's big slot.
func New(p *Pool, limit int64) *Budget {
	return &Budget{pool: p, limit: min(limit, p.MaxBudget())}
}

// NewContext is New for a decode ctx may cancel: once ctx is done, a wait
// for the big slot or for memory ends, and every charge fails, with ctx's
// error (so a cancelled decode doesn't decode on).
func NewContext(ctx context.Context, p *Pool, limit int64) *Budget {
	b := New(p, limit)
	b.ctx = ctx
	return b
}

// NewPlayer returns an animation player's root budget: as New, but it
// never takes the big slot nor waits for memory (it holds what it has for
// as long as it plays, so it mustn't hold up the big decodes). What it
// holds counts as the pool's Players.
func NewPlayer(p *Pool, limit int64) *Budget {
	return &Budget{pool: p, limit: min(limit, p.MaxBudget()), player: true}
}

// NewSmallContext is a small-lane decode's budget: as NewContext, but it
// never takes the big slot nor waits for memory (it fails busy at once),
// so many of them at once (the GIF picker's previews) never queue behind,
// or hold up, the big decodes. Its memory is a decode's, given back when
// it's done: a waiting big decode waits for it (not Players).
func NewSmallContext(ctx context.Context, p *Pool, limit int64) *Budget {
	b := NewContext(ctx, p, limit)
	b.lane = true
	return b
}

// done is b's context's Done channel (nil, never closed, without one).
func (b *Budget) done() <-chan struct{} {
	if b.ctx == nil {
		return nil
	}
	return b.ctx.Done()
}

// ctxErr is b's context's error as a charge returns it, nil while it isn't
// done (or b has none).
func (b *Budget) ctxErr() error {
	if b.ctx == nil || b.ctx.Err() == nil {
		return nil
	}
	return fmt.Errorf("budget: %w", context.Cause(b.ctx))
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
// them (for the big decode: once it has waited BigWait for memory).
func (b *Budget) Charge(n int64) error {
	if b == nil {
		return fmt.Errorf("%w: no budget", ErrBudget)
	}
	if b.parent != nil {
		b.mu.Lock()
		defer b.mu.Unlock()
		if err := b.checkLocked(n); err != nil {
			return err
		}
		if err := b.parent.Charge(n); err != nil {
			return err
		}
		b.used += n
		return nil
	}
	b.mu.Lock()
	if err := b.checkLocked(n); err != nil {
		b.mu.Unlock()
		return err
	}
	p := b.pool
	if p == nil {
		b.used += n
		b.mu.Unlock()
		return nil
	}
	if !b.player && !b.lane && !b.slot && b.used+n > p.small {
		// a big decode now: after the big decodes before it, unless its
		// context ends first
		b.mu.Unlock()
		select {
		case p.big <- struct{}{}:
		case <-b.done():
			return b.ctxErr()
		}
		b.mu.Lock()
		if err := b.checkLocked(n); err != nil { // released, or cancelled, meanwhile
			<-p.big
			b.mu.Unlock()
			return err
		}
		b.slot = true
	}
	var deadline time.Time
	for {
		var freed <-chan struct{}
		if b.slot {
			freed = p.waitFreed() // before the take, so a give between them wakes it
		}
		if p.take(n, b.used+n) {
			if freed != nil {
				p.doneWaiting()
			}
			b.used += n
			if b.player {
				p.players.Add(n)
			}
			b.mu.Unlock()
			return nil
		}
		if freed == nil { // small, the small lane, or a player: never waits
			b.mu.Unlock()
			return fmt.Errorf("%w: %d bytes more, %d of %d held", ErrBusy, n, p.Used(), p.limit)
		}
		// Wait only for memory that can come back: what the other decodes
		// hold (they give it back when they're done), not players' (held
		// for as long as they play) nor this decode's own.
		short := n - (p.limit - p.reserve - p.Used())
		if back := p.Used() - p.Players() - b.used; back < short {
			p.doneWaiting()
			b.mu.Unlock()
			return fmt.Errorf("%w: %d bytes more, %d of %d held, %d of it by players", ErrBusy, n, p.Used(), p.limit, p.Players())
		}
		if deadline.IsZero() {
			deadline = time.Now().Add(p.bigWait)
		}
		left := time.Until(deadline)
		if left <= 0 {
			p.doneWaiting()
			b.mu.Unlock()
			return fmt.Errorf("%w: %d bytes more after %v, %d of %d held", ErrBusy, n, p.bigWait, p.Used(), p.limit)
		}
		b.mu.Unlock()
		t := time.NewTimer(left)
		select {
		case <-freed:
		case <-t.C:
		case <-b.done():
		}
		t.Stop()
		p.doneWaiting()
		b.mu.Lock()
		if err := b.checkLocked(n); err != nil {
			b.mu.Unlock()
			return err
		}
	}
}

// checkLocked is the budget's own limit check for a charge of n, and its
// context's.
func (b *Budget) checkLocked(n int64) error {
	if b.closed {
		return fmt.Errorf("%w: released", ErrBudget)
	}
	if err := b.ctxErr(); err != nil {
		return err
	}
	if n < 0 || n > b.limit-b.used {
		return fmt.Errorf("%w: %d bytes more, %d of %d used", ErrBudget, n, b.used, b.limit)
	}
	return nil
}

// Used is how many bytes b holds.
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

// Big reports whether b holds its pool's big slot.
func (b *Budget) Big() bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.slot
}

// Free gives back n of the bytes b holds (at most what it holds): a buffer
// the decoder made and no longer references, which the garbage collector
// reclaims, so the next charges (b's own, or another decode's) can have
// them. It keeps the big slot: the decode is still big.
func (b *Budget) Free(n int64) {
	if b == nil || n <= 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	n = min(n, b.used)
	b.used -= n
	switch {
	case b.parent != nil:
		b.parent.uncharge(n)
	case b.pool != nil:
		b.giveLocked(n)
	}
}

// giveLocked returns n bytes of a root budget's to its pool.
func (b *Budget) giveLocked(n int64) {
	if b.player {
		b.pool.players.Add(-n)
	}
	b.pool.give(n)
}

// Release gives back everything b holds (to its parent, or its pool) and
// the big slot, and closes it: a charge after it fails. Releasing twice, or
// a child after its parent, does nothing more.
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
		b.giveLocked(n)
	}
	if b.slot {
		b.slot = false
		<-b.pool.big
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
		b.giveLocked(n)
	}
}

// IsBusy reports whether err is a charge the pool couldn't cover now.
func IsBusy(err error) bool { return errors.Is(err, ErrBusy) }

// IsOver reports whether err is a decode past its budget's limit.
func IsOver(err error) bool { return errors.Is(err, ErrBudget) }
