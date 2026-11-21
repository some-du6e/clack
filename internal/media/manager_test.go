package media_test

import (
	"errors"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/media"
	"github.com/some-du6e/clack/internal/media/mediatest"
)

// Slack's MediaPlaybackCoordinator (bxCZ): a player's play event pauses
// every other player, so one plays at a time.
func TestManagerOnePlays(t *testing.T) {
	m := fakeManager()
	a, ra, _ := open(t, m, "FM1", mediatest.GreyGreyRed)
	b, rb, _ := open(t, m, "FM2", mediatest.GreyGreyRed)
	c, _, _ := open(t, m, "FM3", mediatest.Silent) // never played
	a.SetLoop(true)
	b.SetLoop(true)
	a.Play()
	ra.wait(t, media.Playing, 5*time.Second)
	b.Play()
	rb.wait(t, media.Playing, 5*time.Second)
	ra.wait(t, media.Pause, 2*time.Second)
	if pl := m.Playing(); len(pl) != 1 || pl[0] != b {
		t.Errorf("%d playing, want only the one played last", len(pl))
	}
	a.Play()
	ra.wait(t, media.Playing, 2*time.Second)
	rb.wait(t, media.Pause, 2*time.Second)
	if pl := m.Playing(); len(pl) != 1 || pl[0] != a {
		t.Errorf("%d playing, want only the first again", len(pl))
	}
	if st := c.State(); !st.Paused {
		t.Errorf("the player never played: %+v", st)
	}
	if m.Len() != 3 {
		t.Errorf("Len %d, want 3", m.Len())
	}
	m.CloseAll()
	if m.Len() != 0 {
		t.Errorf("Len %d after CloseAll", m.Len())
	}
}

// The manager's timeupdate comes every 250 ms while a player plays, and
// not while it's paused.
func TestManagerTimeUpdate(t *testing.T) {
	m := fakeManager()
	h, r, _ := open(t, m, "FM4", mediatest.GreyGreyRed)
	h.SetLoop(true)
	h.Play()
	r.wait(t, media.Playing, 5*time.Second)
	n0 := r.count(media.TimeUpdate)
	time.Sleep(1100 * time.Millisecond)
	if n := r.count(media.TimeUpdate) - n0; n < 3 || n > 6 {
		t.Errorf("%d timeupdates in 1.1 s of playing, want 4", n)
	}
	h.Pause()
	r.wait(t, media.Pause, time.Second)
	n1 := r.count(media.TimeUpdate)
	time.Sleep(700 * time.Millisecond)
	if n := r.count(media.TimeUpdate) - n1; n != 0 {
		t.Errorf("%d timeupdates while paused", n)
	}
}

// A manager with no backend makes no players (Linux, Windows arm64, …).
func TestManagerNoBackend(t *testing.T) {
	m := media.NewManager(nil)
	if err := m.Available(); !errors.Is(err, media.ErrNoBackend) {
		t.Fatalf("Available %v, want ErrNoBackend (this test binary registers none)", err)
	}
	src, _ := mediatest.Source("FM5", mediatest.GreyGreyRed, false)
	if _, err := m.Open(src, nil, nil); err == nil || m.Len() != 0 {
		t.Errorf("Open: %v, %d players", err, m.Len())
	}
	if media.Available() == nil {
		t.Error("media.Available with nothing registered")
	}
}
