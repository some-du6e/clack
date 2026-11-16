package budget

import (
	"errors"
	"math/rand/v2"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
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
	p := NewPool(100, 0, 0)
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
				b := New(p, limit)
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
	if err := New(p, 100).Charge(11); !errors.Is(err, ErrBusy) {
		t.Errorf("an 11-byte charge with only the reserve left: %v, want ErrBusy", err)
	}
	small := New(p, 100)
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
