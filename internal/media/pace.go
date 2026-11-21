package media

import (
	"context"
	"time"
)

// PaceAhead is how much playback a stream may be ahead of the position by
// (PaceLimit), and PaceSlack the bytes on top.
const (
	PaceAhead = 30 * time.Second
	PaceSlack = 8 << 20
)

// PaceLimit is how far into a file of total bytes and dur long a stream
// may hand the decoder bytes while it plays at pos: PaceAhead of the
// average bitrate past the position, and PaceSlack more (AVFoundation's
// requests to the end of the resource would otherwise pull the whole file
// into its memory as fast as the network goes; a paused player stops
// there). -1 (no limit) while the size or the duration isn't known: the
// metadata, wherever it is in the file, comes first.
func PaceLimit(total int64, pos, dur time.Duration) int64 {
	if total <= 0 || dur <= 0 {
		return -1
	}
	f := float64(pos+PaceAhead) / float64(dur)
	if f >= 1 {
		return -1
	}
	return int64(float64(total)*f) + PaceSlack
}

// paceEvery is how often Pace looks again while the position runs.
const paceEvery = 100 * time.Millisecond

// paceWake, when set, runs each time Pace waits (tests).
var paceWake func()

// Pace holds a stream's request at byte cur of rs's resource until
// PaceLimit passes it (true), or ctx ends (false). While the position
// runs the limit moves with it, so Pace looks again every paceEvery;
// while it doesn't (paused, waiting for data, ended), Pace waits for the
// state to change (Changed): a request a paused player holds costs it no
// wake-ups.
func (b *Base) Pace(ctx context.Context, rs *RangeSource, cur int64) bool {
	for {
		info, err := rs.Info(ctx)
		if err != nil {
			return ctx.Err() == nil
		}
		changed := b.Changed()
		st := b.State()
		if lim := PaceLimit(info.Total, st.Pos, st.Duration); lim < 0 || cur < lim || st.Err != 0 {
			return true
		}
		var tick <-chan time.Time
		if st.runs() {
			tick = time.After(paceEvery)
		}
		if paceWake != nil {
			paceWake()
		}
		select {
		case <-ctx.Done():
			return false
		case <-changed:
		case <-tick:
		}
	}
}
