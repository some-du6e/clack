package media_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/media"
	"github.com/some-du6e/clack/internal/media/mediatest"
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

// A request Pace holds while the player is paused costs no wake-ups: it
// waits on the state, not a timer. Round 3's review, a MINOR: AVPlayer
// keeps its to-end request open while paused, and the request held there
// woke 10 times a second for as long as the tile stayed on screen. Once it
// plays, the limit moving with the position, Pace looks again every 100 ms,
// and lets the request go once the limit passes it.
func TestPaceWaitsWhilePaused(t *testing.T) {
	src, _ := mediatest.Source("FPACE", mediatest.GreyGreyRed, false)
	rs := source(src, quiet, nil)
	defer rs.Close()
	var b media.Base
	b.Init() // paused
	b.Update(func(st *media.State) { st.Duration = 10 * time.Minute })
	var wakes atomic.Int64
	undo := media.SetPaceWake(func() { wakes.Add(1) })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan bool, 1)
	const cur = 9 << 20 // past 30 s of a 10-minute file's bitrate and the 8 MB slack: held
	go func() { done <- b.Pace(ctx, rs, cur) }()
	defer func() {
		cancel()
		<-done
		undo()
	}()
	time.Sleep(600 * time.Millisecond)
	if n := wakes.Load(); n != 1 {
		t.Errorf("paused for 600 ms, the held request woke %d times, want once (its wait)", n)
	}
	select {
	case <-done:
		t.Fatal("Pace let the request go while paused")
	default:
	}
	b.Update(func(st *media.State) { st.Paused, st.Ready = false, media.HaveEnoughData })
	time.Sleep(450 * time.Millisecond)
	if n := wakes.Load(); n < 3 {
		t.Errorf("playing for 450 ms: %d waits in all, want it looking again every 100 ms", n)
	}
	b.Update(func(st *media.State) { st.Pos = 9*time.Minute + 40*time.Second }) // the rest is within the lead
	select {
	case ok := <-done:
		if !ok {
			t.Error("Pace ended as if cancelled")
		}
		done <- ok // for the deferred wait
	case <-time.After(2 * time.Second):
		t.Fatal("the limit passed the request and Pace held it on")
	}
}
