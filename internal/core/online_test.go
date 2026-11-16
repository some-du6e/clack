package core

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// isClientOnline (online.go): the first read probes, the watcher follows
// the interfaces, an offline reading counts only once it lasts the
// offline event's throttle, and each change is one "client_online" event.
func TestClientOnline(t *testing.T) {
	if on, fp := scanInterfaces(); !on || fp == "" {
		t.Errorf("scanInterfaces: online %v, addresses %q on a runner with a network", on, fp)
	}
	var up atomic.Bool
	up.Store(true)
	var probes atomic.Int32
	oldScan, oldPoll, oldThrottle := netScan, clientOnlinePoll, clientOfflineThrottle
	netScan = func() (bool, string) { probes.Add(1); return up.Load(), "" }
	clientOnlinePoll, clientOfflineThrottle = 20*time.Millisecond, 60*time.Millisecond
	t.Cleanup(func() { netScan, clientOnlinePoll, clientOfflineThrottle = oldScan, oldPoll, oldThrottle })

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

// closeCounting wraps the API transport and counts pool flushes.
type closeCounting struct {
	base   http.RoundTripper
	closes *atomic.Int32
}

func (c closeCounting) RoundTrip(r *http.Request) (*http.Response, error) { return c.base.RoundTrip(r) }

func (c closeCounting) CloseIdleConnections() {
	c.closes.Add(1)
	if ci, ok := c.base.(interface{ CloseIdleConnections() }); ok {
		ci.CloseIdleConnections()
	}
}

// clock2 is a two-clock source (slack.ClockReading) the test moves:
// sleep moves only the wall clock, as on a Mac waking up.
type clock2 struct {
	mu   sync.Mutex
	wall time.Time
	mono time.Duration
}

func (c *clock2) read() slack.ClockReading {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slack.ClockReading{Wall: c.wall, Mono: c.mono}
}

func (c *clock2) tick(d time.Duration) {
	c.mu.Lock()
	c.wall, c.mono = c.wall.Add(d), c.mono+d
	c.mu.Unlock()
}

func (c *clock2) sleep(d time.Duration) {
	c.mu.Lock()
	c.wall = c.wall.Add(d)
	c.mu.Unlock()
}

// A change in the machine's network addresses (a VPN tunnel, a Wi-Fi
// switch) and a wake from sleep each flush the API pool and reconnect the
// realtime socket at once, on a fresh client.getWebSocketURL rather than
// the reconnect_url the old path handed out. A poll that sees neither does
// nothing.
func TestNetworkChangeReconnects(t *testing.T) {
	var addrs atomic.Value
	addrs.Store("en0=192.168.100.166")
	clk := &clock2{wall: time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)}
	t.Cleanup(slack.SetClock(clk.read))
	oldScan, oldPoll := netScan, clientOnlinePoll
	clientOnlinePoll = 20 * time.Millisecond
	netScan = func() (bool, string) { clk.tick(20 * time.Millisecond); return true, addrs.Load().(string) }
	t.Cleanup(func() { netScan, clientOnlinePoll = oldScan, oldPoll })

	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	c := slack.New(fs.Creds())
	var closes atomic.Int32
	c.HTTP.Transport = closeCounting{c.HTTP.Transport, &closes}
	a := New(c, store.New(t.TempDir()))
	defer a.Shutdown()
	evs, stop := a.Subscribe()
	defer stop()
	if err := a.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	a.SetRealtime(true)
	connected := func(want bool) {
		t.Helper()
		waitEvent(t, evs, 5*time.Second, func(ev Event) bool { return ev.Type == "connection" && ev.Connected == want })
	}
	connected(true)
	for fs.Sockets() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	ws := "ws" + strings.TrimPrefix(fs.URL, "http") + "ws?fast=1"
	reconnectURL := func() {
		fs.Push(map[string]any{"type": "reconnect_url", "url": ws})
		time.Sleep(100 * time.Millisecond) // handled before anything changes
	}
	reconnectURL()

	time.Sleep(150 * time.Millisecond) // several quiet polls
	if n := closes.Load(); n != 0 {
		t.Fatalf("%d pool flushes with nothing changed", n)
	}
	if n := fs.CallCount("ws"); n != 1 {
		t.Fatalf("ws dials = %d with nothing changed, want 1", n)
	}

	urls := fs.CallCount("client.getWebSocketURL")
	addrs.Store("en0=192.168.100.166 utun4=172.16.0.2") // WARP on
	connected(false)
	connected(true)
	if n := closes.Load(); n < 1 {
		t.Error("network change didn't flush the API pool")
	}
	if n := fs.CallCount("ws"); n != 2 {
		t.Errorf("ws dials = %d after the network change, want 2", n)
	}
	if fs.CallCount("client.getWebSocketURL") != urls+1 || a.RTM.LastConnectFast() {
		t.Error("reconnected on the old path's reconnect_url, not a fresh URL")
	}

	for fs.Sockets() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	reconnectURL()
	before := closes.Load()
	clk.sleep(25 * time.Minute) // the Mac slept
	connected(false)
	connected(true)
	if closes.Load() <= before {
		t.Error("wake didn't flush the API pool")
	}
	if n := fs.CallCount("ws"); n != 3 {
		t.Errorf("ws dials = %d after the wake, want 3", n)
	}
	if a.RTM.LastConnectFast() {
		t.Error("reconnected on a reconnect_url after waking")
	}
}

// The watcher walks the interfaces once a poll: whether the machine is
// online and its addresses come from the same walk (it walked them twice
// every 2 s, and on Windows each walk was a GetAdaptersAddresses of
// every adapter per interface).
func TestOnlineWatcherWalksOncePerPoll(t *testing.T) {
	var n atomic.Int64
	oldScan, oldPoll := netScan, clientOnlinePoll
	netScan = func() (bool, string) { n.Add(1); return scanInterfaces() }
	clientOnlinePoll = 25 * time.Millisecond
	t.Cleanup(func() { netScan, clientOnlinePoll = oldScan, oldPoll })
	a := New(slack.New(slack.Creds{}), store.New(""))
	t.Cleanup(a.Shutdown)
	a.ClientOnline() // the first read walks, and starts the watcher
	time.Sleep(20 * clientOnlinePoll)
	a.Shutdown()
	time.Sleep(2 * clientOnlinePoll)
	walks := n.Load()
	polls := int64(20)
	// the first read, the watcher's first look, then one a tick (a
	// loaded runner may skip a tick or two)
	if walks > polls+2 {
		t.Errorf("%d interface walks in %d polls, want one a poll", walks, polls)
	}
	if walks < polls/3 {
		t.Errorf("%d interface walks in %d polls: the watcher isn't polling", walks, polls)
	}
}
