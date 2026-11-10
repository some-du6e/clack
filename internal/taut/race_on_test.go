//go:build race

package taut

// raceOn: the race detector slows the JS engine several times over, so
// tests that bound wall-clock time give it room.
const raceOn = true
