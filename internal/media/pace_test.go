package media_test

import (
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/media"
)

// A stream is held to 30 s of the file's average bitrate past the
// position, and 8 MB more; nothing holds it before the metadata or near
// the end.
func TestPaceLimit(t *testing.T) {
	const total = 600 << 20 // a 10-minute video of 600 MB: 1 MB a second
	for _, tc := range []struct {
		pos, dur time.Duration
		want     int64
	}{
		{0, 0, -1},                            // no metadata yet
		{0, 10 * time.Minute, 30<<20 + 8<<20}, // 30 s of it
		{2 * time.Minute, 10 * time.Minute, 150<<20 + 8<<20},
		{9*time.Minute + 40*time.Second, 10 * time.Minute, -1}, // the rest
		{0, 3 * time.Second, -1},                               // a clip shorter than the lead
	} {
		if got := media.PaceLimit(total, tc.pos, tc.dur); got != tc.want {
			t.Errorf("at %v of %v: %d, want %d", tc.pos, tc.dur, got, tc.want)
		}
	}
	if got := media.PaceLimit(-1, 0, time.Minute); got != -1 {
		t.Errorf("an unknown size: %d, want no limit", got)
	}
}
