package messages

import (
	"testing"
	"time"
)

// pageLimiter against HistoryRateLimiter (boot-shared WWGY, build
// e277bbacc0bc) step by step: a key twice in 10 s, then held to the end
// of the window (inclusive: !(time < now - duration)); a window that
// reached the limit makes the next one twice as long, up to 160 s, and
// one that didn't brings it back to 10 s; other keys are their own.
func TestPageLimiterLikeSlack(t *testing.T) {
	t0 := time.Unix(1_800_000_000, 0)
	at := func(d time.Duration) time.Time { return t0.Add(d) }
	const k, other = "undefined_1.000100", "undefined_2.000100"
	var l pageLimiter
	step := func(what string, d time.Duration, want bool) {
		t.Helper()
		if got := l.prevent(k, at(d)); got != want {
			t.Errorf("%s (+%v): prevent %v, want %v", what, d, got, want)
		}
	}
	step("never asked", 0, false)
	l.log(k, at(0))
	step("once", time.Second, false)
	l.log(k, at(time.Second))
	step("twice in the window", 2*time.Second, true)
	if l.prevent(other, at(2*time.Second)) {
		t.Error("another key is held by this one's requests")
	}
	step("the window's last instant", 10*time.Second, true)
	step("past the window", 10*time.Second+time.Millisecond, false)
	// the next window reached the limit before: 20 s
	l.log(k, at(11*time.Second))
	l.log(k, at(12*time.Second))
	step("twice in the second window", 30*time.Second, true)
	step("past the second window (20 s)", 31*time.Second+time.Millisecond, false)
	// windows that keep reaching the limit double to 160 s and stay there
	start := 32 * time.Second
	for _, want := range []time.Duration{40 * time.Second, 80 * time.Second, 160 * time.Second, 160 * time.Second} {
		l.log(k, at(start))
		l.log(k, at(start))
		if got := l.reqs[k].duration; got != want {
			t.Fatalf("window at +%v lasts %v, want %v", start, got, want)
		}
		step("held in its window", start+want, true)
		step("past it", start+want+time.Millisecond, false)
		start += want + time.Second
	}
	// a window with one request brings the next one back to 10 s
	l.log(k, at(start))
	if got := l.reqs[k].duration; got != 160*time.Second {
		t.Fatalf("the window after a full one lasts %v, want 160 s", got)
	}
	start += 160*time.Second + time.Second
	l.log(k, at(start))
	if got := l.reqs[k].duration; got != 10*time.Second {
		t.Errorf("after a window of one request the next lasts %v, want 10 s", got)
	}
	l.reset()
	if l.prevent(k, at(start)) {
		t.Error("reset kept the key's window")
	}
}
