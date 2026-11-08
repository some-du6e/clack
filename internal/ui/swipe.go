package ui

import (
	"math"
	"sync"
	"time"
)

// Back/forward by a two-finger horizontal swipe, the way Slack's Chromium
// does it on macOS ("Swipe between pages"): a deliberate horizontal
// over-scroll that the view under the pointer didn't take, with no
// vertical intent, navigates when the fingers lift.
//
// Gio delivers trackpad scrolling as pointer.Scroll with no gesture
// phase, so the macOS glue (swipe_darwin.go) reports each scroll's phase
// and raw deltas from an NSEvent monitor: raw. Gio's routing then gives
// the window-wide handler (navGestures) only the horizontal scroll no
// inner scrollable consumed: leftover. A swipe needs
//   - a trackpad gesture (phase began … ended; momentum is ignored),
//   - its first swipeLockDist pt mostly horizontal (|x| ≥ 2|y|) and the
//     whole gesture too,
//   - nearly all of it left over (≥ swipeFreeFrac): an inner view that can
//     still scroll sideways takes the gesture instead, like Chromium,
//   - at least swipeDist pt of net finger travel; fingers moving right go
//     back, left go forward, whatever the scroll direction setting.
// The arrow overlay (layoutSwipe) shows the progress meanwhile.

const (
	swipeLockDist = 8   // pt of travel before the axis is decided
	swipeDist     = 120 // pt of net horizontal finger travel to navigate
	swipeFreeFrac = 0.8 // share of the horizontal scroll that must be left over
	swipeSettle   = 40 * time.Millisecond
)

type swipePhase uint8

const (
	swipeBegan swipePhase = iota + 1
	swipeChanged
	swipeEnded
	swipeCancelled
)

// swipeSample is one trackpad scroll from the platform glue. dx/dy are in
// points, finger direction (positive = fingers moving right/down).
type swipeSample struct {
	t        time.Duration // NSEvent timestamp, Gio's pointer.Event.Time
	phase    swipePhase
	dx, dy   float32
	inverted bool // natural scrolling: content follows the fingers
	enabled  bool // System Settings: swipe between pages with two fingers
}

type swipeGesture struct {
	start, end time.Duration
	ended      bool
	cancelled  bool
	endWall    time.Time
	inverted   bool
	enabled    bool
	absX, absY float32 // raw travel
	netX       float32 // raw net finger travel
	lock       int8    // 0 undecided, 1 horizontal, -1 not a swipe
	free       float32 // left-over horizontal travel (from Gio)
	used       bool
	done       bool // evaluated
}

// swiper recognises swipes. raw comes from the platform glue's thread,
// leftover and poll from the UI goroutine.
type swiper struct {
	mu  sync.Mutex
	gs  [4]swipeGesture // ring of recent gestures, gs[cur] the newest
	cur int
	any bool
}

func (s *swiper) raw(e swipeSample, wall time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.phase == swipeBegan {
		s.cur = (s.cur + 1) % len(s.gs)
		s.gs[s.cur] = swipeGesture{start: e.t, inverted: e.inverted, enabled: e.enabled, used: true}
		s.any = true
	}
	if !s.any {
		return
	}
	g := &s.gs[s.cur]
	if g.ended || g.cancelled {
		return
	}
	g.absX += abs32(e.dx)
	g.absY += abs32(e.dy)
	g.netX += e.dx
	if g.lock == 0 && g.absX+g.absY >= swipeLockDist {
		g.lock = -1
		if g.absX >= 2*g.absY {
			g.lock = 1
		}
	}
	switch e.phase {
	case swipeEnded, swipeCancelled:
		g.ended, g.cancelled = true, e.phase == swipeCancelled
		g.end, g.endWall = e.t, wall
	}
}

// leftover records horizontal scroll (Gio px, content direction) that no
// view under the pointer consumed.
func (s *swiper) leftover(t time.Duration, gioDX, pxPerPt float32) {
	if pxPerPt <= 0 {
		pxPerPt = 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.any {
		return
	}
	for i := range s.gs {
		g := &s.gs[i]
		if !g.used || g.done || t < g.start || (g.ended && t > g.end) {
			continue
		}
		g.free += abs32(gioDX) / pxPerPt
		return
	}
}

// swipeState is what poll tells the UI.
type swipeState struct {
	nav      int       // -1 back, +1 forward, 0 nothing (once per gesture)
	progress float32   // 0..1+ of the overlay while a swipe is under way
	dir      int       // the overlay's direction (-1 back, +1 forward)
	wake     time.Time // run poll again then (a gesture is settling)
}

func (s *swiper) poll(now time.Time) swipeState {
	s.mu.Lock()
	defer s.mu.Unlock()
	var st swipeState
	if !s.any {
		return st
	}
	for i := range s.gs {
		g := &s.gs[i]
		if !g.used || g.done {
			continue
		}
		if !g.ended {
			if g.candidate() && i == s.cur {
				st.dir = navDir(g.netX)
				st.progress = abs32(g.netX) / swipeDist
			}
			continue
		}
		if now.Sub(g.endWall) < swipeSettle { // Gio's events for it may still be queued
			if w := g.endWall.Add(swipeSettle); st.wake.IsZero() || w.Before(st.wake) {
				st.wake = w
			}
			if g.candidate() && i == s.cur {
				st.dir = navDir(g.netX)
				st.progress = abs32(g.netX) / swipeDist
			}
			continue
		}
		g.done = true
		if !g.cancelled && g.candidate() && abs32(g.netX) >= swipeDist {
			st.nav = navDir(g.netX)
		}
	}
	return st
}

// candidate: so far a deliberate, unconsumed horizontal gesture.
func (g *swipeGesture) candidate() bool {
	return g.enabled && g.lock == 1 && g.absY*2 <= g.absX && g.free >= swipeFreeFrac*g.absX
}

// navDir maps finger travel to history: fingers right go back.
func navDir(netX float32) int {
	if netX > 0 {
		return -1
	}
	return 1
}

func abs32(v float32) float32 { return float32(math.Abs(float64(v))) }
