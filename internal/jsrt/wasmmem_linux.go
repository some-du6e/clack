package jsrt

import "golang.org/x/sys/unix"

// mapNoReserve keeps a reservation out of the overcommit accounting until
// its pages are committed (under vm.overcommit_memory=2 the kernel charges
// what mprotect makes writable).
const mapNoReserve = unix.MAP_NORESERVE
