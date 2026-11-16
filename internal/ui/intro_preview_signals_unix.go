//go:build !windows

package ui

import (
	"os"
	"os/signal"
	"syscall"
)

// handleChildSignals readies the renderer for its window going away: a
// write to the closed pipe returns EPIPE instead of killing it with
// SIGPIPE (so its deferred cleanup runs), and SIGTERM (Linux's parent-death
// signal) removes its folder before it exits.
func handleChildSignals(dir string) {
	signal.Ignore(syscall.SIGPIPE)
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM)
	go func() {
		<-ch
		removePreviewDir(dir)
		os.Exit(1)
	}()
}
