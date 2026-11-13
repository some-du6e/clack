//go:build perfharness

package imgcache

// PerfStats is the memory tier as the memory workflow's driver reports it
// (internal/ui/perfdrive.go; perfharness builds only): decoded entries,
// their bytes, and the budget.
func (c *Cache) PerfStats() (entries, bytes, limit int) {
	if c == nil {
		return 0, 0, 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries), c.bytes, c.maxBytes
}
