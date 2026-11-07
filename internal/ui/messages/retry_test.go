package messages

import (
	"testing"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// historyCalls opens a channel whose history fails with code, draws frames
// for d (like a window repainting on every invalidate), and counts the
// conversations.history requests.
func historyCalls(t *testing.T, code string, d time.Duration, during func(v *ChannelView)) (int, *ChannelView) {
	n, v, _ := historyCallsAPI(t, code, d, during)
	return n, v
}

func historyCallsAPI(t *testing.T, code string, d time.Duration, during func(v *ChannelView)) (int, *ChannelView, *fakeSlack) {
	app, api := newTestApp(t)
	api.mu.Lock()
	api.fail["conversations.history"] = code
	api.mu.Unlock()
	v := NewChannelView(coreHost{app: app, th: theme.New(false)}, tEmpty)
	v.Preview = true
	f := frameFn(v)
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		f()
		if during != nil {
			during(v)
		}
		time.Sleep(16 * time.Millisecond)
	}
	return len(api.called("conversations.history")), v, api
}

// A failing load backs off (1 s, 2 s, 4 s …) instead of refetching every
// frame: at most 3 requests in 3.5 s (0 s, 1 s, 3 s), not ~3 a second.
func TestHistoryFailureBacksOff(t *testing.T) {
	n, v := historyCalls(t, "fatal_error", 3500*time.Millisecond, nil)
	t.Logf("conversations.history: %d calls in 3.5 s", n)
	if n < 2 || n > 3 {
		t.Errorf("%d history calls in 3.5 s, want 2–3 (exponential backoff)", n)
	}
	if v.s.snap.err == nil || v.s.snap.loaded {
		t.Errorf("no error state: err=%v loaded=%v", v.s.snap.err, v.s.snap.loaded)
	}
	if backoff(1) != time.Second || backoff(3) != 4*time.Second || backoff(20) != maxBackoff {
		t.Errorf("backoff: %v %v %v", backoff(1), backoff(3), backoff(20))
	}
}

// An auth failure stops loading: one request, then none.
func TestHistoryAuthFailureStops(t *testing.T) {
	n, v, api := historyCallsAPI(t, "invalid_auth", 2500*time.Millisecond, nil)
	if n != 1 {
		t.Errorf("%d history calls after invalid_auth, want 1", n)
	}
	if !v.s.hb.authFailed.Load() {
		t.Error("invalid_auth didn't stop the hub")
	}
	// core stops all Slack traffic once the session ended: Retry can't
	// load until the user signs in again (no request goes out), and the
	// list keeps showing "Couldn't load messages · Retry".
	if !v.s.hb.app.SignedOut() {
		t.Error("core didn't record the ended session")
	}
	v.s.hb.retry(v.s.m)
	f := frameFn(v)
	for i := 0; i < 60; i++ {
		f()
		time.Sleep(16 * time.Millisecond)
	}
	if got := len(api.called("conversations.history")); got != 1 {
		t.Errorf("%d history calls after Retry on an ended session, want still 1", got)
	}
	if v.s.snap.err == nil || v.s.snap.loaded {
		t.Errorf("no error state after Retry: err=%v loaded=%v", v.s.snap.err, v.s.snap.loaded)
	}
	// the Retry link is the sign-in prompt now
	sh := &signInHost{coreHost: coreHost{app: v.s.hb.app, th: theme.New(false)}}
	v.s.retryBtn.Click()
	v2 := &ChannelView{h: sh, ch: tEmpty, s: v.s}
	frameFn(v2)()
	if sh.shown != 1 {
		t.Errorf("Retry on an ended session showed sign-in %d times, want 1", sh.shown)
	}
	if got := len(api.called("conversations.history")); got != 1 {
		t.Errorf("%d history calls after the sign-in Retry, want 1", got)
	}
}

// core's auth_failed event stops every load, even ones that would retry.
func TestAuthFailedEventStops(t *testing.T) {
	sent := false
	n, v := historyCalls(t, "fatal_error", 3500*time.Millisecond, func(v *ChannelView) {
		if !sent {
			sent = true
			v.s.hb.apply(core.Event{Type: "auth_failed"})
		}
	})
	if n > 1 {
		t.Errorf("%d history calls after auth_failed, want at most the one in flight", n)
	}
	// the connection coming back resumes
	v.s.hb.apply(core.Event{Type: "connection", Connected: true})
	if v.s.hb.authFailed.Load() {
		t.Error("still stopped after reconnecting")
	}
}

type signInHost struct {
	coreHost
	shown int
}

func (h *signInHost) ShowSignIn() { h.shown++ }
