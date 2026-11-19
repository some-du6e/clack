//go:build js

package ui

// handleChildSignals: the browser demo starts no renderer process and has
// no signals (intro_preview_signals_unix.go).
func handleChildSignals(string) {}
