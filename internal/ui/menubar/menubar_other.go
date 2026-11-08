//go:build !darwin || !cgo

package menubar

// Other platforms keep Gio's window: Clack draws no menu bar there.
func install([]Menu) {}

// SetMainView does nothing outside macOS.
func SetMainView(uintptr) {}
