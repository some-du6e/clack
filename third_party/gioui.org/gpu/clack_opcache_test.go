// SPDX-License-Identifier: Unlicense OR MIT

package gpu

import "testing"

// A path that leaves the screen for a frame and comes back must be found
// in the cache again from the frame after it was built. put hands freed
// slots out again, and a freed slot used to keep its old key, so the old
// slot deleted the key's live entry at every frame's end: the path was
// built and uploaded again every frame it stayed on screen.
func TestClackOpCacheReturningPathHits(t *testing.T) {
	c := newOpCache()
	k, l := opKey{strokeWidth: 1}, opKey{strokeWidth: 2}
	c.put(k, opCacheValue{})
	c.put(l, opCacheValue{})
	c.frame() // both drawn this frame
	c.frame() // neither drawn: both slots freed
	if _, ok := c.get(k); ok {
		t.Fatal("a path no frame drew is still cached")
	}
	c.put(k, opCacheValue{}) // k comes back alone, into l's old slot
	for f := 1; f <= 3; f++ {
		c.frame()
		if _, ok := c.get(k); !ok {
			t.Fatalf("frame %d after it came back: the path drawn every frame missed the cache", f)
		}
	}
	if _, ok := c.get(l); ok {
		t.Error("a path that never came back is cached")
	}
	// every slot is either live or free, never both
	live := map[int]bool{}
	for _, i := range c.index {
		live[i-1] = true
	}
	for _, i := range c.freelist {
		if live[i] {
			t.Errorf("slot %d is in the index and on the freelist", i)
		}
	}
}
