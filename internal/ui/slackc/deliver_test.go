package slackc

import (
	"sync/atomic"
	"testing"
)

// JS committing a root again with the same output (React re-rendering it
// for nothing: a portal under it mounted or unmounted, a context update)
// is not a new tree: no rebuilt boxes (hover states and scroll offsets
// would start over), no frame. A message list with plugins did this all
// the time once rows were idle (Karim, alpha.24: "stuff is still spazzing
// out").
func TestDeliverSameTreeIsNothing(t *testing.T) {
	var frames atomic.Int64
	prev := CurrentEnv()
	e := prev
	e.Invalidate = func() { frames.Add(1) }
	Init(e)
	t.Cleanup(func() { Init(prev) })
	const root = 1 << 40
	in := &inst{root: root, name: "MessageWrapper", view: NewView(root), slots: map[string]int64{}}
	rootsMu.Lock()
	roots[root] = in
	rootsMu.Unlock()
	t.Cleanup(func() { rootsMu.Lock(); delete(roots, root); rootsMu.Unlock() })

	tree := []byte(`[{"t":"div","k":1,"p":{"className":"x-wrap"},"c":[{"t":"@","n":"MessageWrapper","k":2}]}]`)
	if err := Deliver(root, tree); err != nil {
		t.Fatal(err)
	}
	first := in.tree.Load()
	if first == nil || frames.Load() != 1 {
		t.Fatalf("first delivery: tree %v, %d frames asked", first, frames.Load())
	}
	for i := 0; i < 5; i++ {
		_ = Deliver(root, append([]byte(nil), tree...))
	}
	if in.tree.Load() != first || frames.Load() != 1 {
		t.Errorf("the same tree again: replaced %v, %d frames asked (want the first kept, 1 frame)", in.tree.Load() != first, frames.Load())
	}
	changed := []byte(`[{"t":"div","k":1,"p":{"className":"x-wrap x-new"},"c":[{"t":"@","n":"MessageWrapper","k":2}]}]`)
	_ = Deliver(root, changed)
	if in.tree.Load() == first || frames.Load() != 2 {
		t.Errorf("a changed tree: replaced %v, %d frames asked", in.tree.Load() != first, frames.Load())
	}
}
