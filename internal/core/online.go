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

// clientOnlineProbe reports whether the machine has a network connection
// (tests replace it).
var clientOnlineProbe = probeInterfaces

// networkFingerprint names the machine's network addresses; a change
// means the network path changed (tests replace it).
var networkFingerprint = interfaceFingerprint

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
		o.on = clientOnlineProbe()
		go a.watchOnline()
	})
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.on
}

func (a *App) watchOnline() {
	probe, poll, throttle, fingerprint := clientOnlineProbe, clientOnlinePoll, clientOfflineThrottle, networkFingerprint
	t := time.NewTicker(poll)
	defer t.Stop()
	last, addrs := slack.ReadClock(), fingerprint()
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
		if f := fingerprint(); f != addrs {
			log.Printf("core: network addresses changed (%d → %d)", countAddrs(addrs), countAddrs(f))
			addrs = f
			why = append(why, "network changed")
		}
		if len(why) > 0 {
			a.networkChanged(strings.Join(why, ", "))
		}
		on := probe()
		if !on && a.ClientOnline() {
			// the offline event's throttle: a blip doesn't count
			select {
			case <-a.bg.Done():
				return
			case <-time.After(throttle):
			}
			on = probe()
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

// probeInterfaces reports an interface that is up and running, not
// loopback, with an address that isn't link-local.
func probeInterfaces() bool {
	ifs, err := net.Interfaces()
	if err != nil {
		return true // nothing to go on: don't claim offline
	}
	for _, ifc := range ifs {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagRunning == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, ad := range addrs {
			var ip net.IP
			switch v := ad.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip != nil && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsUnspecified() {
				return true
			}
		}
	}
	return false
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

// interfaceFingerprint lists the addresses probeInterfaces counts (up,
// running, not loopback, not link-local), sorted. Link-local addresses
// are left out: macOS's AWDL interfaces come and go with nothing but
// those, and no Slack connection uses them.
func interfaceFingerprint() string {
	ifs, err := net.Interfaces()
	if err != nil {
		return ""
	}
	var out []string
	for _, ifc := range ifs {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagRunning == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, ad := range addrs {
			var ip net.IP
			switch v := ad.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip != nil && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsUnspecified() {
				out = append(out, ifc.Name+"="+ip.String())
			}
		}
	}
	sort.Strings(out)
	return strings.Join(out, " ")
}

func countAddrs(fp string) int { return len(strings.Fields(fp)) }
