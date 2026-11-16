//go:build unix && !linux

package jsrt

// mapNoReserve: macOS and the BSDs charge an anonymous mapping nothing
// before its pages are touched.
const mapNoReserve = 0
