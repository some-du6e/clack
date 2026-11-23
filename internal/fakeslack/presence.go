package fakeslack

import (
	"sync"
	"time"
)

// The server's auto-away: Slack marks a member away when none of their
// clients has shown activity for a while (about 30 minutes; a client's
// tickle, sent at most every 19 s while the person uses it, is that
// activity), and back when one does. AutoAway plays it for the viewer in
// a test-sized window.

// autoAways is each server's AutoAway state.
var autoAways = newPerServer() // *Server -> *autoAway

type autoAway struct {
	mu      sync.Mutex
	window  time.Duration
	last    time.Time // the last tickle (or AutoAway's start)
	away    bool
	tickles []string // the reasons, oldest first
	stop    chan struct{}
}

// AutoAway marks the viewer away (presence_change away) once window
// passes without a tickle from a client, and active again at the next
// tickle; 0 turns it off. Tickles' reasons are kept (Tickles).
func (s *Server) AutoAway(window time.Duration) {
	if v, ok := autoAways.LoadAndDelete(s); ok {
		close(v.(*autoAway).stop)
	}
	if window <= 0 {
		return
	}
	aa := &autoAway{window: window, last: time.Now(), stop: make(chan struct{})}
	autoAways.Store(s, aa)
	go func() {
		t := time.NewTicker(window / 10)
		defer t.Stop()
		for {
			select {
			case <-aa.stop:
				return
			case <-t.C:
			}
			aa.mu.Lock()
			due := !aa.away && time.Since(aa.last) > aa.window
			if due {
				aa.away = true
			}
			aa.mu.Unlock()
			if due {
				s.Push(map[string]any{"type": "presence_change", "user": SelfID, "presence": "away"})
			}
		}
	}()
}

// Tickles is the reasons of the tickles clients sent since AutoAway.
func (s *Server) Tickles() []string {
	v, ok := autoAways.Load(s)
	if !ok {
		return nil
	}
	aa := v.(*autoAway)
	aa.mu.Lock()
	defer aa.mu.Unlock()
	return append([]string(nil), aa.tickles...)
}

// noteTickle is a client's tickle (the gateway's read loop): activity.
func (s *Server) noteTickle(reason string) {
	v, ok := autoAways.Load(s)
	if !ok {
		return
	}
	aa := v.(*autoAway)
	aa.mu.Lock()
	aa.last = time.Now()
	aa.tickles = append(aa.tickles, reason)
	back := aa.away
	aa.away = false
	aa.mu.Unlock()
	if back {
		s.Push(map[string]any{"type": "presence_change", "user": SelfID, "presence": "active"})
	}
}
