package budget

import (
	"context"
	"errors"
	"math/rand/v2"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestChargeLimit: a budget takes exactly what fits, an over-charge takes
// nothing (so a smaller one can still succeed), and a nil budget allows
// nothing.
func TestChargeLimit(t *testing.T) {
	if err := (*Budget)(nil).Charge(1); !errors.Is(err, ErrBudget) {
		t.Errorf("a nil budget's charge: %v, want ErrBudget", err)
	}
	b := NewLocal(100)
	if err := b.Charge(60); err != nil {
		t.Fatal(err)
	}
	if err := b.Charge(50); !errors.Is(err, ErrBudget) || errors.Is(err, ErrBusy) {
		t.Errorf("50 more of 100 with 60 used: %v, want ErrBudget", err)
	}
	if b.Used() != 60 {
		t.Errorf("a refused charge left Used %d, want 60", b.Used())
	}
	if err := b.Charge(40); err != nil {
		t.Errorf("the last 40: %v", err)
	}
	if err := b.Charge(-1); err == nil {
		t.Error("a negative charge was allowed")
	}
}

// TestBusyIsNotOver: a charge the pool can't cover because another budget
// holds the memory is ErrBusy, not ErrBudget, and succeeds once that budget
// is released; one past the budget's own limit is ErrBudget whatever the
// pool holds. Make take always succeed and the first check fails; make it
// report over-limit and the second does.
func TestBusyIsNotOver(t *testing.T) {
	p := NewPool(100, 0, 1000) // every budget here is small: none takes the big slot
	a := New(p, 100)
	if err := a.Charge(80); err != nil {
		t.Fatal(err)
	}
	b := New(p, 100)
	err := b.Charge(30)
	if !errors.Is(err, ErrBusy) || errors.Is(err, ErrBudget) {
		t.Fatalf("30 of a pool with 20 left: %v, want ErrBusy only", err)
	}
	if b.Used() != 0 || p.Used() != 80 {
		t.Errorf("a busy charge took something: budget %d, pool %d", b.Used(), p.Used())
	}
	a.Release()
	if err := b.Charge(30); err != nil {
		t.Errorf("30 once the other budget let go: %v", err)
	}
	if err := New(p, 50).Charge(60); !errors.Is(err, ErrBudget) || errors.Is(err, ErrBusy) {
		t.Errorf("60 on a 50-byte budget: %v, want ErrBudget", err)
	}
	b.Release()
	if p.Used() != 0 {
		t.Errorf("the pool holds %d after every budget let go", p.Used())
	}
}

// TestPoolNeverPassesLimit: budgets charging from many goroutines at once
// never hold more than the pool's limit together (Peak), however the
// charges interleave, and the pool is empty once they're all released.
// Each goroutine wants up to a quarter of the pool and holds it (yielding
// so the goroutines overlap) before releasing, so they genuinely contend;
// the pool's Used and Peak must never pass its limit, and it empties once
// they all release. Remove take's limit check and the overlap pushes the
// peak past the pool.
func TestPoolNeverPassesLimit(t *testing.T) {
	const limit = 16 << 10
	p := NewPool(limit, 0, 0)
	var wg sync.WaitGroup
	var busy atomic.Int64
	for g := 0; g < 64; g++ {
		wg.Add(1)
		go func(seed uint64) {
			defer wg.Done()
			r := rand.New(rand.NewPCG(seed, 1))
			for round := 0; round < 300; round++ {
				b := NewPlayer(p, limit) // never takes the big slot: they all contend

				if err := b.Charge(int64(1 + r.IntN(limit/4))); errors.Is(err, ErrBusy) {
					busy.Add(1)
				} else if err != nil {
					t.Errorf("charge: %v", err)
				}
				if u := p.Used(); u > limit {
					t.Errorf("the pool holds %d of %d", u, limit)
				}
				runtime.Gosched() // hold, so the goroutines overlap
				b.Release()
			}
		}(uint64(g))
	}
	wg.Wait()
	if pk := p.Peak(); pk > limit {
		t.Errorf("the pool's peak %d passed its limit %d", pk, limit)
	}
	if u := p.Used(); u != 0 {
		t.Errorf("the pool holds %d with every budget released", u)
	}
	if n := p.Players(); n != 0 {
		t.Errorf("players hold %d with every budget released", n)
	}
	t.Logf("%d charges were busy (the pool contended)", busy.Load())
}

// TestReserveForSmall: with big budgets holding all but the reserve, a
// small budget still gets its memory from the reserve, and a big one
// doesn't.
func TestReserveForSmall(t *testing.T) {
	p := NewPool(100, 20, 10)
	big := New(p, 100)
	if big.Limit() != 80 {
		t.Errorf("a budget's limit %d, want the pool's 100 less its 20 reserve", big.Limit())
	}
	if err := big.Charge(80); err != nil {
		t.Fatal(err)
	}
	if err := NewPlayer(p, 100).Charge(11); !errors.Is(err, ErrBusy) {
		t.Errorf("an 11-byte charge with only the reserve left: %v, want ErrBusy", err)
	}
	small := NewPlayer(p, 100)
	if err := small.Charge(10); err != nil {
		t.Errorf("a small budget's 10 bytes from the reserve: %v", err)
	}
	if err := small.Charge(1); !errors.Is(err, ErrBusy) {
		t.Errorf("a budget past small taking more of the reserve: %v, want ErrBusy", err)
	}
	big.Release()
	small.Release()
	if p.Used() != 0 {
		t.Errorf("the pool holds %d", p.Used())
	}
}

// TestSubBudgets: a child charges its parent and the pool, under its own
// limit too; releasing it gives back only its own; releasing the parent
// first takes the child's with it, and the child's release after does
// nothing more.
func TestSubBudgets(t *testing.T) {
	p := NewPool(1000, 0, 0)
	parent := New(p, 1000)
	if err := parent.Charge(100); err != nil {
		t.Fatal(err)
	}
	child := parent.Sub(50)
	if err := child.Charge(60); !errors.Is(err, ErrBudget) {
		t.Errorf("60 on a 50-byte child: %v, want ErrBudget", err)
	}
	if err := child.Charge(40); err != nil {
		t.Fatal(err)
	}
	if parent.Used() != 140 || p.Used() != 140 {
		t.Errorf("after the child's 40: parent %d, pool %d, want 140 each", parent.Used(), p.Used())
	}
	child.Release()
	if parent.Used() != 100 || p.Used() != 100 {
		t.Errorf("after the child's release: parent %d, pool %d, want 100 each", parent.Used(), p.Used())
	}
	late := parent.Sub(500)
	if err := late.Charge(200); err != nil {
		t.Fatal(err)
	}
	parent.Release()
	late.Release()
	if p.Used() != 0 {
		t.Errorf("the pool holds %d after the parent, then its child, let go", p.Used())
	}
	if err := late.Charge(1); err == nil {
		t.Error("a charge after release succeeded")
	}
	if err := parent.Charge(1); err == nil {
		t.Error("a charge on a released budget succeeded")
	}
}

// TestLocalDrawsNothing: a local budget's charges never touch a pool.
func TestLocalDrawsNothing(t *testing.T) {
	b := NewLocal(1 << 30)
	if err := b.Charge(1 << 29); err != nil {
		t.Fatal(err)
	}
	b.Release()
	if err := Unlimited().Charge(1 << 40); err != nil {
		t.Errorf("an unlimited budget refused a charge: %v", err)
	}
}

// TestBigSlotOneAtATime: a decode budget growing past Small takes the
// pool's one big slot, so a second big decode waits for the first to be
// released, while a small decode's charges never wait for it. Make the
// slot not exclusive and the second big charge succeeds at once.
func TestBigSlotOneAtATime(t *testing.T) {
	p := NewPool(1<<20, 0, 100)
	first := New(p, 1<<20)
	if err := first.Charge(200); err != nil { // past Small: the slot
		t.Fatal(err)
	}
	if !first.Big() {
		t.Fatal("a budget past Small doesn't hold the big slot")
	}
	small := New(p, 1<<20)
	done := make(chan error, 1)
	go func() { done <- small.Charge(50) }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("a small charge while the slot is held: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a small decode waited for the big slot")
	}
	second := New(p, 1<<20)
	got := make(chan error, 1)
	go func() { got <- second.Charge(300) }()
	select {
	case err := <-got:
		t.Fatalf("a second big decode charged (%v) while the first held the slot", err)
	case <-time.After(100 * time.Millisecond):
	}
	first.Release()
	select {
	case err := <-got:
		if err != nil {
			t.Errorf("the second big decode once the first let go: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the second big decode didn't get the slot once the first was released")
	}
	second.Release()
	small.Release()
	if p.Used() != 0 {
		t.Errorf("the pool holds %d", p.Used())
	}
}

// TestBigWaitsForRoom: the big decode's charge the pool can't cover waits
// for memory the other decodes give back instead of failing at once, and
// fails with ErrBusy only once it has waited BigWait. A small decode's (or
// a player's) charge never waits. Make the slot holder not wait and the
// first check fails.
func TestBigWaitsForRoom(t *testing.T) {
	p := NewPool(1000, 0, 100)
	p.SetBigWait(5 * time.Second)
	var smalls []*Budget // nine small decodes holding 900: they finish
	for i := 0; i < 9; i++ {
		s := New(p, 1000)
		if err := s.Charge(100); err != nil {
			t.Fatal(err)
		}
		smalls = append(smalls, s)
	}
	if err := NewPlayer(p, 1000).Charge(200); !errors.Is(err, ErrBusy) {
		t.Errorf("a player's charge with no room: %v, want ErrBusy at once", err)
	}
	big := New(p, 1000)
	got := make(chan error, 1)
	go func() { got <- big.Charge(500) }() // past Small: the slot, then a wait
	select {
	case err := <-got:
		t.Fatalf("the big charge returned (%v) before any memory was given back", err)
	case <-time.After(100 * time.Millisecond):
	}
	for _, s := range smalls[:5] { // five small decodes done
		s.Release()
	}
	select {
	case err := <-got:
		if err != nil {
			t.Errorf("the big charge once room was given back: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the big charge didn't wake when room was given back")
	}
	big.Release()

	p.SetBigWait(50 * time.Millisecond)
	start := time.Now()
	if err := New(p, 1000).Charge(700); !errors.Is(err, ErrBusy) { // the 400 held stay
		t.Errorf("a big charge the small decodes never make room for: %v, want ErrBusy", err)
	}
	if d := time.Since(start); d < 40*time.Millisecond {
		t.Errorf("the big charge failed after %v, before its BigWait", d)
	}
	for _, s := range smalls[5:] {
		s.Release()
	}
}

// TestBigFailsFastOnPlayers: memory players hold isn't given back while
// they play, so a big charge only they could make room for fails at once
// instead of holding the big slot (and the big decodes behind it) for
// BigWait. Remove the check and it waits the whole BigWait.
func TestBigFailsFastOnPlayers(t *testing.T) {
	p := NewPool(1000, 0, 100)
	p.SetBigWait(10 * time.Second)
	hold := NewPlayer(p, 1000)
	if err := hold.Charge(900); err != nil {
		t.Fatal(err)
	}
	if p.Players() != 900 {
		t.Errorf("players hold %d, want 900", p.Players())
	}
	start := time.Now()
	big := New(p, 1000)
	if err := big.Charge(500); !errors.Is(err, ErrBusy) {
		t.Errorf("a big charge only players could make room for: %v, want ErrBusy", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("the big charge waited %v for memory players hold", d)
	}
	big.Release()
	hold.Free(400)
	if p.Players() != 500 {
		t.Errorf("after the player's Free(400) players hold %d, want 500", p.Players())
	}
	hold.Release()
	if p.Players() != 0 || p.Used() != 0 {
		t.Errorf("players %d, pool %d, want 0 each", p.Players(), p.Used())
	}
}

// TestContextEndsWaits: a budget made with NewContext stops waiting for the
// big slot, and for memory, once its context is done, and charges nothing
// more: a cancelled copy doesn't decode anyway.
func TestContextEndsWaits(t *testing.T) {
	p := NewPool(1000, 0, 100)
	p.SetBigWait(time.Minute)
	first := New(p, 1000)
	if err := first.Charge(200); err != nil { // holds the slot
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	queued := NewContext(ctx, p, 1000)
	got := make(chan error, 1)
	go func() { got <- queued.Charge(300) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-got:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("a cancelled wait for the slot: %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a cancelled budget kept waiting for the big slot")
	}
	if queued.Big() || queued.Used() != 0 {
		t.Errorf("the cancelled budget holds the slot (%v) or %d bytes", queued.Big(), queued.Used())
	}
	first.Release()

	// waiting for memory, holding the slot: small decodes hold the rest
	small := New(p, 1000)
	if err := small.Charge(100); err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	waiting := NewContext(ctx, p, 1000)
	if err := waiting.Charge(850); err != nil {
		t.Fatal(err)
	}
	go func() { got <- waiting.Charge(100) }() // 1050 > 1000: waits for the small one
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-got:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("a cancelled wait for memory: %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a cancelled budget kept waiting for memory")
	}
	if err := waiting.Charge(1); !errors.Is(err, context.Canceled) {
		t.Errorf("a charge after its context ended: %v, want context.Canceled", err)
	}
	waiting.Release()
	small.Release()
	if p.Used() != 0 {
		t.Errorf("the pool holds %d", p.Used())
	}
}

// TestFreeGivesBack: Free returns bytes to the pool (a child's through its
// parent) and keeps the budget usable.
func TestFreeGivesBack(t *testing.T) {
	p := NewPool(1000, 0, 2000)
	b := New(p, 1000)
	if err := b.Charge(600); err != nil {
		t.Fatal(err)
	}
	b.Free(400)
	if b.Used() != 200 || p.Used() != 200 {
		t.Errorf("after Free(400): budget %d, pool %d, want 200 each", b.Used(), p.Used())
	}
	c := b.Sub(500)
	if err := c.Charge(300); err != nil {
		t.Fatal(err)
	}
	c.Free(300)
	if c.Used() != 0 || b.Used() != 200 || p.Used() != 200 {
		t.Errorf("after the child's Free: child %d, parent %d, pool %d", c.Used(), b.Used(), p.Used())
	}
	b.Free(1 << 20) // at most what it holds
	if b.Used() != 0 || p.Used() != 0 {
		t.Errorf("Free past what's held: budget %d, pool %d", b.Used(), p.Used())
	}
	if err := b.Charge(10); err != nil {
		t.Errorf("a charge after Free: %v", err)
	}
	b.Release()
}

// TestSmallLaneComesBack: a small-lane decode (NewSmallContext: the GIF
// picker's previews) never takes the big slot or waits, but its memory is
// a decode's, given back when it's done: a big decode waits for it, where
// it fails at once on players'. Count the small lane as players and the
// big charge fails at once.
func TestSmallLaneComesBack(t *testing.T) {
	p := NewPool(1000, 0, 100)
	p.SetBigWait(5 * time.Second)
	first := New(p, 1000)
	if err := first.Charge(200); err != nil { // holds the big slot
		t.Fatal(err)
	}
	lane := NewSmallContext(context.Background(), p, 1000)
	done := make(chan error, 1)
	go func() { done <- lane.Charge(700) }() // past Small: doesn't wait for the slot
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("a small-lane charge past Small with the slot held: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a small-lane decode waited for the big slot")
	}
	if lane.Big() || p.Players() != 0 {
		t.Errorf("the small-lane budget took the slot (%v) or counts as players (%d)", lane.Big(), p.Players())
	}
	first.Release()
	big := New(p, 1000)
	got := make(chan error, 1)
	go func() { got <- big.Charge(500) }() // 700 + 500 > 1000: waits for the lane's
	select {
	case err := <-got:
		t.Fatalf("the big charge returned (%v) instead of waiting for the small-lane decode", err)
	case <-time.After(100 * time.Millisecond):
	}
	lane.Release()
	select {
	case err := <-got:
		if err != nil {
			t.Errorf("the big charge once the small-lane decode finished: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the big charge didn't wake when the small-lane decode gave its memory back")
	}
	big.Release()
}
