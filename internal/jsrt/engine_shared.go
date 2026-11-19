package jsrt

// What the engine (engine.go, wasmmem.go) shares with the rest of the
// package and with its browser stand-in (engine_js.go).

import "sync/atomic"

const excBit = 0x80000000

const (
	isArray   = 1
	isPromise = 2
	isError   = 3
)

func unpack(pl uint64) (uint32, uint32) { return uint32(pl >> 32), uint32(pl) }

type wasmMemStats struct {
	size     atomic.Uint64 // the memory's length in bytes
	reserved atomic.Uint64 // address space reserved for it; 0 = it's a Go slice
}
