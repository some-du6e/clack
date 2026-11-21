package media

import (
	"sync"
	"sync/atomic"
	"time"
)

// Base is what every backend's player keeps the same way: its events,
// the state the UI reads (a snapshot the backend updates from its media
// thread, whose position runs on with the wall clock while it plays, as
// an HTMLMediaElement's currentTime does between the backend's reads),
// when the GPU last asked for a frame, and its counts. A backend embeds
// it and calls Init first.
type Base struct {
	events chan Event
	emitMu sync.Mutex // Emit and CloseEvents, from any goroutine
	closed bool

	mu      sync.Mutex
	st      State
	rate    float64
	at      time.Time     // when st.Pos was read
	changed chan struct{} // closed at the next Update (Changed)

	lastAsk atomic.Int64 // unix ns

	// the counts (Stats)
	NFrames, NTransfers, NTicks, NAsks, NDropped, NSkipped, NTimeouts atomic.Int64
}

// Init readies b: paused, volume 1, rate 1, asked now (the click that
// made the player is on screen).
func (b *Base) Init() {
	b.events = make(chan Event, 256)
	b.st = State{Paused: true, Volume: 1}
	b.rate = 1
	b.at = time.Now()
	b.lastAsk.Store(time.Now().UnixNano())
}

// Events implements Player.
func (b *Base) Events() <-chan Event { return b.events }

// State implements Player: the snapshot, its position run on while it
// plays.
func (b *Base) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.stateLocked()
}

func (b *Base) stateLocked() State {
	st := b.st
	if st.runs() {
		st.Pos += time.Duration(float64(time.Since(b.at)) * b.rate)
		if st.Duration > 0 && st.Pos > st.Duration {
			st.Pos = st.Duration
		}
	}
	return st
}

// Update changes the snapshot under its lock: f gets it with Pos as it
// ran on until now, and a Pos f sets is the position now.
func (b *Base) Update(f func(st *State)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.st.Pos = b.stateLocked().Pos
	b.at = time.Now()
	f(&b.st)
	if b.changed != nil {
		close(b.changed)
		b.changed = nil
	}
}

// Changed is closed at the snapshot's next Update: a waiter takes it
// before it reads the state, and wakes for any change after.
func (b *Base) Changed() <-chan struct{} {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.changed == nil {
		b.changed = make(chan struct{})
	}
	return b.changed
}

// runs is whether the position runs on with the clock (playing, with
// data).
func (st State) runs() bool {
	return !st.Paused && !st.Ended && !st.Waiting && st.Err == 0 && st.Ready >= HaveFutureData
}

// SetRate is the rate the position runs at.
func (b *Base) SetRate(r float64) {
	b.Update(func(*State) {})
	b.mu.Lock()
	b.rate = r
	b.mu.Unlock()
}

// Emit sends an event of kind k with the state now; a full channel drops
// it (NDropped), and none is sent after CloseEvents.
func (b *Base) Emit(k EventKind) {
	ev := Event{Kind: k, State: b.State()}
	b.emitMu.Lock()
	defer b.emitMu.Unlock()
	if b.closed {
		return
	}
	select {
	case b.events <- ev:
	default:
		b.NDropped.Add(1)
	}
}

// CloseEvents closes the events channel as the player ends; later Emits
// send nothing.
func (b *Base) CloseEvents() {
	b.emitMu.Lock()
	defer b.emitMu.Unlock()
	if !b.closed {
		b.closed = true
		close(b.events)
	}
}

// Asked records the GPU's ask now; it reports whether the one before was
// more than a second ago (the frames had stopped).
func (b *Base) Asked() (afterGap bool) {
	b.NAsks.Add(1)
	now := time.Now().UnixNano()
	return now-b.lastAsk.Swap(now) > int64(time.Second)
}

// AskedWithin is whether the GPU asked within d.
func (b *Base) AskedWithin(d time.Duration) bool {
	return time.Now().UnixNano()-b.lastAsk.Load() < int64(d)
}

// TouchAsk counts now as an ask without counting it (a play or a seek:
// the player is on screen).
func (b *Base) TouchAsk() { b.lastAsk.Store(time.Now().UnixNano()) }

// BaseStats are b's counts as Stats, with rs's.
func (b *Base) BaseStats(rs *RangeSource) Stats {
	st := Stats{Frames: b.NFrames.Load(), Transfers: b.NTransfers.Load(), Ticks: b.NTicks.Load(), Asks: b.NAsks.Load(),
		Dropped: b.NDropped.Load(), Skipped: b.NSkipped.Load(), Timeouts: b.NTimeouts.Load()}
	if rs != nil {
		r := rs.Stats()
		st.Requests, st.BytesRead = r.Requests, r.BytesRead
	}
	return st
}
