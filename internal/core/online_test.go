package core

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// isClientOnline (online.go): the first read probes, the watcher follows
// the interfaces, an offline reading counts only once it lasts the
// offline event's throttle, and each change is one "client_online" event.
func TestClientOnline(t *testing.T) {
	if !probeInterfaces() {
		t.Error("probeInterfaces: offline on a runner with a network")
	}
	var up atomic.Bool
	up.Store(true)
	var probes atomic.Int32
	oldProbe, oldPoll, oldThrottle := clientOnlineProbe, clientOnlinePoll, clientOfflineThrottle
	clientOnlineProbe = func() bool { probes.Add(1); return up.Load() }
	clientOnlinePoll, clientOfflineThrottle = 20*time.Millisecond, 60*time.Millisecond
	t.Cleanup(func() { clientOnlineProbe, clientOnlinePoll, clientOfflineThrottle = oldProbe, oldPoll, oldThrottle })

	a := New(slack.New(slack.Creds{}), store.New(""))
	t.Cleanup(a.Shutdown)
	evs, stop := a.Subscribe()
	defer stop()
	if !a.ClientOnline() {
		t.Fatal("ClientOnline false with the network up")
	}

	wait := func(want bool) {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case ev := <-evs:
				if ev.Type != "client_online" {
					continue
				}
				if ev.Connected != want {
					t.Fatalf("client_online %v, want %v", ev.Connected, want)
				}
				if a.ClientOnline() != want {
					t.Fatalf("ClientOnline %v after the event", !want)
				}
				return
			case <-deadline:
				t.Fatalf("no client_online %v", want)
			}
		}
	}
	up.Store(false)
	wait(false)
	up.Store(true)
	wait(true)

	// a blip shorter than the throttle doesn't count
	n := probes.Load()
	up.Store(false)
	for probes.Load() < n+1 {
		time.Sleep(5 * time.Millisecond)
	}
	up.Store(true)
	time.Sleep(150 * time.Millisecond)
	for {
		select {
		case ev := <-evs:
			if ev.Type == "client_online" {
				t.Fatalf("a blip emitted client_online %v", ev.Connected)
			}
			continue
		default:
		}
		break
	}
	if !a.ClientOnline() {
		t.Error("offline after a blip")
	}
}
