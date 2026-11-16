package core

// Slack's isClientOnline (client-boot mG45 `Pb`: the connectivity store's
// clientOnline, set by cbxg setClientOnline). The web client sets it from
// the window's offline and online events (boot-render yYRM
// upsertClientOfflineStatus, throttled 500 ms, and
// upsertClientOnlineStatus, at once), so in the desktop app it is
// Chromium's navigator.onLine: false only while the machine has no network
// connection at all, not when Slack's servers are slow (serviceOnline) or
// the socket is down. The omniswitcher's footer reads it (lj3A Footer's
// offline notice).
//
// Clack has no browser to ask. It reads the interfaces the way Chromium's
// network change notifier does: online while an interface that isn't
// loopback is up and running with an address that isn't link-local. The
// watcher starts with the first ClientOnline call, polls every
// clientOnlinePoll while the session lasts, waits clientOfflineThrottle
// before it believes an offline reading (the offline event's throttle) and
// emits "client_online" when the answer changes.
//
// The same poll watches for the two things that kill every open
// connection at once: the machine waking from sleep (the wall clock ran
// ahead of the monotonic one, slack.ClockReading) and a change in its
// network addresses (a VPN tunnel up or down, a Wi-Fi switch). Either way
// the pooled API connections and the realtime socket belong to a path that
// is gone, so core flushes the pool and reconnects the socket on a fresh
// URL, as Chromium's NetworkChangeNotifier (OnIPAddressChanged flushing
// the socket pools with ERR_NETWORK_CHANGED) and power monitor make Slack's
// client do. Nothing else would notice: idle sockets on a dead path get no
// RST until they are written to.

import (
	"log"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// The watcher's poll and the offline throttle (vars for tests).
var (
	clientOnlinePoll      = 2 * time.Second
	clientOfflineThrottle = 500 * time.Millisecond
)

// netScan reads the machine's network in one walk of its interfaces:
// whether it has a connection (online) and a name for its addresses
// (fingerprint), whose change means the network path changed (tests
// replace it). The watcher reads both every clientOnlinePoll, so one walk
// serves both: on Windows each interface's addresses cost another
// GetAdaptersAddresses of every adapter (Go's net), where scanInterfaces
// makes one call.
var netScan = scanInterfaces

type onlineState struct {
	once sync.Once
	mu   sync.Mutex
	on   bool
}

// ClientOnline is isClientOnline: whether the machine has a network
// connection.
func (a *App) ClientOnline() bool {
	o := &a.online
	o.once.Do(func() {
		o.on, _ = netScan()
		go a.watchOnline()
	})
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.on
}

func (a *App) watchOnline() {
	scan, poll, throttle := netScan, clientOnlinePoll, clientOfflineThrottle
	t := time.NewTicker(poll)
	defer t.Stop()
	_, addrs := scan()
	last := slack.ReadClock()
	for {
		select {
		case <-a.bg.Done():
			return
		case <-t.C:
		}
		now := slack.ReadClock()
		var why []string
		if slept := now.Slept(last); slept > slack.WakeDrift {
			log.Printf("core: woke after %s", slept.Round(time.Second))
			why = append(why, "woke from sleep")
		}
		last = now
		on, f := scan()
		if f != addrs {
			log.Printf("core: network addresses changed (%d → %d)", countAddrs(addrs), countAddrs(f))
			addrs = f
			why = append(why, "network changed")
		}
		if len(why) > 0 {
			a.networkChanged(strings.Join(why, ", "))
		}
		if !on && a.ClientOnline() {
			// the offline event's throttle: a blip doesn't count
			select {
			case <-a.bg.Done():
				return
			case <-time.After(throttle):
			}
			on, _ = scan()
		}
		a.setClientOnline(on)
	}
}

// setClientOnline is setClientOnline: a change emits "client_online".
func (a *App) setClientOnline(on bool) {
	o := &a.online
	o.once.Do(func() {}) // a test's value isn't overwritten by a first probe
	o.mu.Lock()
	was := o.on
	o.on = on
	o.mu.Unlock()
	if was != on {
		a.emit(Event{Type: "client_online", Connected: on})
	}
}

// networkChanged drops everything that rode the old network path: the
// idle API connections and the realtime socket, which reconnects at once
// with a fresh gateway URL.
func (a *App) networkChanged(why string) {
	a.C.ResetConns()
	if a.RTM != nil {
		a.RTM.Reconnect(why)
	}
}

func countAddrs(fp string) int { return len(strings.Fields(fp)) }

// countsOnline reports an address scanInterfaces counts, on an interface
// that is up, running and not loopback: not loopback, link-local or
// unspecified. Link-local addresses are left out of the fingerprint too:
// macOS's AWDL interfaces come and go with nothing but those, and no
// Slack connection uses them.
func countsOnline(ip net.IP) bool {
	return ip != nil && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsUnspecified()
}

// fingerprintOf is the fingerprint of an interface walk's addresses
// ("<interface>=<ip>"): sorted, space-separated.
func fingerprintOf(addrs []string) string {
	sort.Strings(addrs)
	return strings.Join(addrs, " ")
}
