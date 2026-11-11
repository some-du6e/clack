package imgcache

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// waitInval waits for one repaint request.
func waitInval(t *testing.T, ch <-chan struct{}, what string, d time.Duration) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(d):
		t.Fatalf("no repaint for %s within %v", what, d)
	}
}

// A visible avatar whose first fetch fails (a dropped connection) loads by
// itself: the failure schedules a repaint when its retry is due, and the
// surface's next Get fetches again. Before, the failure was only retried
// once something else happened to redraw the window 30 s later, so the
// channel header's facepile could stay grey tiles (Karim, 2026-11-10).
func TestFailedFetchRetriesAndRepaints(t *testing.T) {
	var calls atomic.Int32
	inval := make(chan struct{}, 16)
	c := New(func(ctx context.Context, url string) ([]byte, error) {
		if calls.Add(1) == 1 {
			return nil, errors.New("read tcp: connection reset by peer")
		}
		return pngBytes(48, 48), nil
	}, "", func() { inval <- struct{}{} })
	u := "https://ca.slack-edge.com/T1-U1-abc-72"
	if _, ok := c.Get(u, 40); ok {
		t.Fatal("ready before any fetch")
	}
	waitInval(t, inval, "the failure", 5*time.Second)
	if !c.Failed(u, 40) {
		t.Fatal("the failed fetch isn't marked failed")
	}
	if _, ok := c.Get(u, 40); ok {
		t.Fatal("ready after a failed fetch")
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("fetched %d times before the retry was due, want 1", n)
	}
	t0 := time.Now()
	waitInval(t, inval, "the retry", RetryMin+4*time.Second) // nothing else redraws
	if d := time.Since(t0); d < RetryMin/2 {
		t.Fatalf("retry repaint after %v, want about %v", d, RetryMin)
	}
	c.Get(u, 40) // the surface draws again and asks
	waitInval(t, inval, "the retried image", 5*time.Second)
	if _, ok := c.Get(u, 40); !ok {
		t.Fatal("not ready after the retry")
	}
	if c.Failed(u, 40) {
		t.Fatal("still marked failed after loading")
	}
	if n := calls.Load(); n != 2 {
		t.Fatalf("fetched %d times, want 2", n)
	}
}

func TestRetryBackoff(t *testing.T) {
	net := errors.New("context deadline exceeded")
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 32 * time.Second, time.Minute, time.Minute}
	for i, w := range want {
		if got := retryAfter(i+1, net); got != w {
			t.Errorf("retry %d: %v, want %v", i+1, got, w)
		}
	}
	for _, err := range []error{ErrTooBig, fmt.Errorf("decode: %w", ErrBlockedAddr), errors.New("slack: refusing to fetch \"x\"")} {
		if got := retryAfter(1, err); got != RetryPermanent {
			t.Errorf("%v: %v, want %v", err, got, RetryPermanent)
		}
	}
}

// Avatars and emoji have their own fetch lane: eight message images stuck
// on a slow host don't hold an avatar back.
func TestAvatarsDontQueueBehindFiles(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	inval := make(chan struct{}, 32)
	c := New(func(ctx context.Context, url string) ([]byte, error) {
		if DiskOK(url) {
			return pngBytes(24, 24), nil
		}
		select { // a message image on a slow connection
		case <-release:
		case <-ctx.Done():
		}
		return nil, errors.New("slow")
	}, "", func() { inval <- struct{}{} })
	for i := 0; i < 12; i++ {
		c.Get(fmt.Sprintf("https://files.slack.com/files-pri/T1-F%d/big.png", i), 0)
	}
	av := "https://avatars.slack-edge.com/2026-01-01/1_abc_72.png"
	c.Get(av, 40)
	deadline := time.After(5 * time.Second)
	for {
		if _, ok := c.Get(av, 40); ok {
			return
		}
		select {
		case <-inval:
		case <-deadline:
			t.Fatal("the avatar waited behind the message images")
		}
	}
}
