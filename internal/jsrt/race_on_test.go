//go:build race

package jsrt

// raceOn: the race detector instruments allocations, so allocation counts
// are only checked without it.
const raceOn = true
