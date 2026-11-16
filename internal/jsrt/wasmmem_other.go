//go:build !unix && !windows

package jsrt

import "errors"

// No address-space reservation here (js/wasm, plan9, …): the wasm memory
// is a Go slice, as wazero's own.

func reserve(n uint64) ([]byte, error) { return nil, errors.New("not supported on this platform") }
func commit(b []byte) error            { return errors.New("not supported on this platform") }
func release(b []byte) error           { return errors.New("not supported on this platform") }
