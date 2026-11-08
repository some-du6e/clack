//go:build darwin && cgo

package ui

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit
void clack_nav_install(void);
*/
import "C"

import (
	"sync/atomic"
	"time"
)

// macOS: Gio's scrollWheel: drops NSEvent's phase and momentumPhase, and
// its otherMouseDown: maps only buttons 0-2. An NSEvent local monitor
// (nav_darwin.m) reports both before Gio sees the event; it never consumes
// anything.

var navApp atomic.Pointer[App]

func init() {
	startNavGlue = func(a *App) {
		navApp.Store(a)
		C.clack_nav_install()
	}
}

// NSEventPhase
const (
	nsPhaseBegan      = 1 << 0
	nsPhaseStationary = 1 << 1
	nsPhaseChanged    = 1 << 2
	nsPhaseEnded      = 1 << 3
	nsPhaseCancelled  = 1 << 4
)

//export clackNavScroll
func clackNavScroll(phase C.int, sdx, sdy, ts C.double, inverted, enabled C.int) {
	a := navApp.Load()
	if a == nil {
		return
	}
	var p swipePhase
	switch int(phase) {
	case nsPhaseBegan:
		p = swipeBegan
	case nsPhaseChanged, nsPhaseStationary:
		p = swipeChanged
	case nsPhaseEnded:
		p = swipeEnded
	case nsPhaseCancelled:
		p = swipeCancelled
	default: // may-begin
		return
	}
	// finger direction: with natural scrolling the deltas follow the fingers
	dx, dy := float32(sdx), float32(sdy)
	if inverted == 0 {
		dx, dy = -dx, -dy
	}
	a.swipe.raw(swipeSample{
		t:     time.Duration(float64(ts)*float64(time.Second) + .5), // as Gio's pointer.Event.Time
		phase: p, dx: dx, dy: dy, inverted: inverted != 0, enabled: enabled != 0,
	}, time.Now())
	if p == swipeEnded || p == swipeCancelled {
		a.Invalidate() // evaluate it
	}
}

//export clackNavButton
func clackNavButton(dir C.int) {
	if a := navApp.Load(); a != nil {
		d := int(dir)
		a.Post(func() { a.navigate(d) })
	}
}
