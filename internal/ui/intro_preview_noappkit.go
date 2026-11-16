//go:build !darwin || !cgo

package ui

// appKitReport: there is no AppKit here.
func appKitReport() *appKitState { return nil }
