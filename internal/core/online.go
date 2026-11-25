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
	once   sync.Once
	mu     sync.Mutex
	on     bool
	pinned bool // PinNetwork: the host's network is none of this App's business
	// reading is the watcher's last reading (navigator.onLine): clientOnline
	// follows it only when it changes, as the window's offline and online
	// events fire only then; the API queue's waits set clientOnline too
	// (connectivity.go)
	reading bool
	// changed is the connectivity store's lastClientConnection: set at
	// start and at every setClientOnline, a value it had already included
	changed time.Time
}

// PinNetwork takes the machine as online on one network that never
// changes, from now on: the watcher (the poll that reconnects on a network
// change or a wake from sleep, and flips isClientOnline) never starts, or
// stops at its next tick. For tests over a loopback fake: the host's own
// network (a CI runner's interfaces going away for two seconds) must
// neither reconnect them nor mark them offline mid-measurement. Call it
// before boot.
func (a *App) PinNetwork() {
	o := &a.online
	o.once.Do(func() { o.changed = time.Now() }) // no first probe, no watcher
	o.mu.Lock()
	o.pinned, o.on, o.reading = true, true, true
	o.mu.Unlock()
}

// watcherReading is navigator.onLine: the interface watcher's last
// reading (online while PinNetwork pins it).
func (a *App) watcherReading() bool {
	o := &a.online
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.pinned || o.reading
}

// recentlyOnlineGrace is how long wasRecentlyOnline outlasts a lost
// connection (Vdrr TP: 1e4 ms).
const recentlyOnlineGrace = 10 * time.Second

// ClientOnline is isClientOnline: whether the machine has a network
// connection.
func (a *App) ClientOnline() bool {
	o := &a.online
	o.once.Do(func() {
		o.on, o.changed = clientOnlineProbe(), time.Now()
		o.reading = o.on
		go a.watchOnline()
		if !o.on {
			go a.awaitNetwork(false) // the queue's first tick: clientOnline is false
		}
	})
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.on
}

// RecentlyOnline is wasRecentlyOnline (client-boot Vdrr TP), which the
// composer's hint bar reads: online (isOnline: the client has a network
// and the service is up, Clack's nearest being a session that wasn't
// ended, membersOnline), or clientOnline changed under 10s ago
// (lastClientConnection, set at start too). until is when a lost
// connection stops counting as recent (zero while online): Slack forces
// a re-render then (refreshRecentlyOnline).
func (a *App) RecentlyOnline() (on bool, until time.Time) {
	if a.membersOnline() {
		return true, time.Time{}
	}
	o := &a.online
	o.mu.Lock()
	until = o.changed.Add(recentlyOnlineGrace)
	o.mu.Unlock()
	return time.Now().Before(until), until
}

// LastClientConnection is the connectivity store's lastClientConnection
// (C4fL): when clientOnline last changed (the start, at first). The
// OfflineChannelToast's "Last updated" reads it.
func (a *App) LastClientConnection() time.Time {
	o := &a.online
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.changed
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
		a.online.mu.Lock()
		pinned := a.online.pinned
		a.online.mu.Unlock()
		if pinned {
			return // PinNetwork
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
		a.online.mu.Lock()
		was := a.online.reading
		a.online.mu.Unlock()
		on := probe()
		if !on && was {
			// the offline event's throttle: a blip doesn't count
			select {
			case <-a.bg.Done():
				return
			case <-time.After(throttle):
			}
			on = probe()
		}
		if on == was {
			continue // no offline or online event
		}
		a.online.mu.Lock()
		a.online.reading = on
		a.online.mu.Unlock()
		a.setClientOnline(on)
		if !on {
			// the queue's next tick: clientOnline is false (it just set
			// it: no second setClientOffline, no second event)
			a.awaitNetwork(false)
		}
	}
}

// setClientOnline is the connectivity store's setClientOnline: every call
// stamps lastClientConnection, the same value or not (the reducer does),
// and emits "client_online", so the views re-read wasRecentlyOnline's
// grace from its new start.
func (a *App) setClientOnline(on bool) {
	o := &a.online
	o.once.Do(func() { o.changed = time.Now() }) // a test's value isn't overwritten by a first probe
	o.mu.Lock()
	was := o.on
	o.on, o.changed = on, time.Now()
	o.mu.Unlock()
	a.emit(Event{Type: "client_online", Connected: on})
	if on && !was {
		a.refetchOnline() // what failed while offline is asked for again
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
