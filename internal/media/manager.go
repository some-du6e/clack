package media

import (
	"fmt"
	"sync"
	"time"
)

// TimeUpdateEvery is Chromium's periodic timeupdate while a media element
// plays (html_media_element.cc: kMaxTimeupdateEventFrequency, 250 ms),
// which Slack's scrubber and clock follow.
const TimeUpdateEvery = 250 * time.Millisecond

// Manager keeps a client's players: Slack's MediaPlaybackCoordinator
// (bxCZ: a player's play event pauses every other one, the media viewer's
// included, so one plays at a time) and the periodic timeupdate.
type Manager struct {
	backend Backend
	err     error

	mu      sync.Mutex
	players map[*Handle]struct{}
}

// NewManager is a manager making players with b; nil is the registered
// backend available here (Default), if any.
func NewManager(b Backend) *Manager {
	m := &Manager{backend: b, players: map[*Handle]struct{}{}}
	if b == nil {
		m.backend, m.err = Default()
	} else if err := b.Available(); err != nil {
		m.err = fmt.Errorf("media: %s: %w", b.Name(), err)
	}
	return m
}

// Available is nil when the manager can make players.
func (m *Manager) Available() error {
	if m.backend == nil && m.err == nil {
		return ErrNoBackend
	}
	return m.err
}

// Takes reports whether the manager can make a player of src: its
// backend takes that kind of source (Taker), or takes any.
func (m *Manager) Takes(src Source) bool {
	if m.Available() != nil {
		return false
	}
	if t, ok := m.backend.(Taker); ok {
		return t.Takes(src)
	}
	return true
}

// Backend is the backend's name, "" for none.
func (m *Manager) Backend() string {
	if m.backend == nil {
		return ""
	}
	return m.backend.Name()
}

// Open makes a player of src, paused (the caller plays it: Slack's
// autoplay once the source is decided). frameReady is the App's repaint
// for a new frame; onEvent (may be nil) gets the player's events and the
// periodic timeupdate, on the handle's goroutine, in order: it must not
// block (it posts to the UI's loop), and it is the only way to the events
// (Handle.Events is nil). Backends fire only HTML's non-periodic
// timeupdates; the periodic ones are the manager's.
func (m *Manager) Open(src Source, frameReady func(), onEvent func(Event)) (*Handle, error) {
	if err := m.Available(); err != nil {
		return nil, err
	}
	if frameReady == nil {
		frameReady = func() {}
	}
	p, err := m.backend.New(src, frameReady)
	if err != nil {
		return nil, err
	}
	h := &Handle{Player: p, m: m, onEvent: onEvent, stop: make(chan struct{}), done: make(chan struct{})}
	m.mu.Lock()
	m.players[h] = struct{}{}
	m.mu.Unlock()
	go h.loop()
	return h, nil
}

// Len is the players open now.
func (m *Manager) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.players)
}

// Playing is the handles playing now.
func (m *Manager) Playing() []*Handle {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*Handle
	for h := range m.players {
		if playing(h.State()) {
			out = append(out, h)
		}
	}
	return out
}

// playing is whether st is a player playing: not paused, ended, waiting
// or failed (HTML leaves paused false after an error).
func playing(st State) bool { return !st.Paused && !st.Ended && !st.Waiting && st.Err == 0 }

// CloseAll closes every player (sign-out, the window going).
func (m *Manager) CloseAll() {
	m.mu.Lock()
	hs := make([]*Handle, 0, len(m.players))
	for h := range m.players {
		hs = append(hs, h)
	}
	m.mu.Unlock()
	for _, h := range hs {
		h.Close()
	}
}

// playbackStarted pauses every player but h (bxCZ.playbackStarted).
func (m *Manager) playbackStarted(h *Handle) {
	m.mu.Lock()
	var others []*Handle
	for o := range m.players {
		if o != h {
			others = append(others, o)
		}
	}
	m.mu.Unlock()
	for _, o := range others {
		if st := o.State(); !st.Paused {
			o.Pause()
		}
	}
}

// Handle is a managed player.
type Handle struct {
	Player
	m       *Manager
	onEvent func(Event)
	stop    chan struct{}
	done    chan struct{}
	once    sync.Once
}

// Close closes the player and forgets it. It returns at once.
func (h *Handle) Close() {
	h.once.Do(func() {
		h.m.mu.Lock()
		delete(h.m.players, h)
		h.m.mu.Unlock()
		close(h.stop)
		h.Player.Close()
	})
}

// Done is closed once the handle's goroutine is gone (after Close).
func (h *Handle) Done() <-chan struct{} { return h.done }

// Events is nil: a managed player's events go to Open's onEvent (the
// handle's own loop reads the player's, and one read elsewhere wouldn't
// reach the coordinator).
func (h *Handle) Events() <-chan Event { return nil }

// loop hands on the player's events, pauses the others when it plays, and
// adds a timeupdate every TimeUpdateEvery while it plays.
func (h *Handle) loop() {
	defer close(h.done)
	var tick *time.Ticker
	defer func() {
		if tick != nil {
			tick.Stop()
		}
	}()
	events := h.Player.Events()
	last := time.Duration(-1)
	// a play without the data to play yet (an autoplay still loading):
	// the others pause at its playing, as Chromium fires <video
	// autoplay>'s play once it has enough data (fIJO's tI calls
	// bxCZ.playbackStarted from onPlay)
	waitStart := false
	for {
		var tc <-chan time.Time
		if tick != nil {
			tc = tick.C
		}
		select {
		case ev, ok := <-events:
			if !ok {
				return
			}
			select {
			case <-h.stop:
				return // closed: what's queued is for no one
			default:
			}
			switch {
			case ev.Kind == Play && ev.State.Err == 0:
				if ev.State.Waiting || ev.State.Ready < HaveFutureData {
					waitStart = true
				} else {
					h.m.playbackStarted(h)
				}
			case ev.Kind == Playing && waitStart:
				waitStart = false
				h.m.playbackStarted(h)
			case ev.Kind == Pause || ev.Kind == Error || ev.Kind == Ended:
				waitStart = false
			}
			on := playing(ev.State)
			switch {
			case on && tick == nil:
				tick = time.NewTicker(TimeUpdateEvery)
			case !on && tick != nil:
				tick.Stop()
				tick = nil
			}
			h.emit(ev)
		case <-tc:
			st := h.Player.State()
			if !playing(st) {
				tick.Stop()
				tick = nil
				continue
			}
			if st.Pos == last {
				continue // Chromium fires it only when the media time moved
			}
			last = st.Pos
			h.emit(Event{Kind: TimeUpdate, State: st})
		case <-h.stop:
			return
		}
	}
}

func (h *Handle) emit(ev Event) {
	if h.onEvent != nil {
		h.onEvent(ev)
	}
}
