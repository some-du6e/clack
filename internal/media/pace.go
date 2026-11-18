package media

import "time"

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
