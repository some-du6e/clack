//go:build perfharness && !linux && !windows

package ui

import "io"

// perfOSMem has no OS counters outside Linux and Windows (perfmem_*.go);
// the driver's Go numbers still come out.
func perfOSMem(w io.Writer) {}
