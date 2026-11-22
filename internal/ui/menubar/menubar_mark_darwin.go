//go:build darwin && cgo

package menubar

import "C"

import "time"

// clackMenuInstalled is menubar_darwin.m's word that an install finished
// on AppKit's main thread, with what it took in microseconds (the startup
// timeline's menu-bar point). It only hands the number on.
//
//export clackMenuInstalled
func clackMenuInstalled(us C.double) {
	if f := installedHook.Load(); f != nil {
		(*f)(time.Duration(float64(us) * float64(time.Microsecond)))
	}
}
