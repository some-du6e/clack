package core

import (
	"testing"
	"time"
)

// The memory-only download cache lets go of what nothing asked for in
// fileIdle (sweep, run by the eviction loop): an idle client keeps what it
// showed lately, as the image cache does.
func TestFileCacheSweepsIdle(t *testing.T) {
	c := newFileCache(1 << 20)
	c.put("old", fileEntry{data: make([]byte, 1000)})
	c.put("new", fileEntry{data: make([]byte, 2000)})
	c.items["old"].Value.(*fileItem).used = time.Now().Add(-2 * fileIdle)
	c.sweep(time.Now().Add(-fileIdle))
	if _, ok := c.get("old"); ok {
		t.Error("an entry idle for twice fileIdle stayed")
	}
	if _, ok := c.get("new"); !ok {
		t.Error("a fresh entry went")
	}
	if c.size != 2000 {
		t.Errorf("size %d after the sweep, want 2000", c.size)
	}
}
