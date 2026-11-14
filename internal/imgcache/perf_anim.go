//go:build perfharness

package imgcache

// PerfAnimStats is the animated GIFs as the memory workflow's driver
// reports them (internal/ui/perfdrive.go, through an interface so the
// driver still builds on a side without this file): players playing and
// their bytes, animated GIFs held (playing or not) and their encoded
// bytes. Not in perf.yml's SCALE_HARNESS: an older side has no players.
func (c *Cache) PerfAnimStats() (players, playerBytes, animated, encodedBytes int) {
	s := c.Stats()
	return s.Players, s.PlayerBytes, s.Animated, s.EncodedBytes
}
