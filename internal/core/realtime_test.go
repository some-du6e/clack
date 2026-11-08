package core

import (
	"context"
	"testing"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
)

// waitEvent reads evs until want arrives (or fails after d).
func waitEvent(t *testing.T, evs <-chan Event, d time.Duration, want func(Event) bool) {
	t.Helper()
	deadline := time.After(d)
	for {
		select {
		case ev := <-evs:
			if want(ev) {
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for event")
		}
	}
}

// The realtime socket is opt-in: Boot alone never asks for a gateway URL
// or dials it, and polling keeps counts fresh instead. SetRealtime(true)
// connects; SetRealtime(false) closes it again.
func TestRealtimeOptIn(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	a := New(slack.New(fs.Creds()), store.New(t.TempDir()))
	a.pollEvery = 30 * time.Millisecond
	defer a.Shutdown()
	evs, stop := a.Subscribe()
	defer stop()
	if err := a.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	counts := fs.CallCount("client.counts")
	for i := 0; i < 2; i++ {
		waitEvent(t, evs, 5*time.Second, func(ev Event) bool { return ev.Type == "poll" })
	}
	if n := fs.CallCount("ws") + fs.CallCount("client.getWebSocketURL"); n != 0 {
		t.Fatalf("realtime off, but the gateway was contacted %d times", n)
	}
	if fs.CallCount("client.counts") <= counts {
		t.Error("polling didn't refresh client.counts")
	}
	if a.Realtime() {
		t.Error("Realtime() true by default")
	}

	a.SetRealtime(true)
	waitEvent(t, evs, 5*time.Second, func(ev Event) bool { return ev.Type == "connection" && ev.Connected })
	if fs.CallCount("ws") != 1 {
		t.Errorf("ws dials = %d, want 1", fs.CallCount("ws"))
	}
	a.SetRealtime(true) // already on: no second socket
	a.SetRealtime(false)
	if a.RTM.Connected() {
		t.Error("socket still up after SetRealtime(false)")
	}
	if fs.CallCount("ws") != 1 {
		t.Errorf("ws dials = %d after off, want 1", fs.CallCount("ws"))
	}
}

// SetRealtime(true) before Boot waits for Boot; Shutdown closes the socket.
func TestRealtimeBeforeBootAndShutdown(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	a := New(slack.New(fs.Creds()), store.New(t.TempDir()))
	evs, stop := a.Subscribe()
	defer stop()
	a.SetRealtime(true)
	if fs.CallCount("ws") != 0 {
		t.Fatal("dialled before Boot")
	}
	if err := a.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitEvent(t, evs, 5*time.Second, func(ev Event) bool { return ev.Type == "connection" && ev.Connected })
	a.Shutdown()
	deadline := time.Now().Add(5 * time.Second)
	for a.RTM.Connected() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if a.RTM.Connected() {
		t.Error("socket still up after Shutdown")
	}
}
