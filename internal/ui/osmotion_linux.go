//go:build linux

package ui

import (
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/some-du6e/clack/internal/ui/uiguard"
)

// Linux: the XDG settings portal. org.freedesktop.appearance reduced-motion
// (1 reduce, 0 no preference) where the portal has it, else GNOME's
// org.gnome.desktop.interface enable-animations (false: reduce), which
// GNOME's portal backend exposes too. Read once, then watched through
// SettingChanged, on a session-bus connection of its own (the colour
// scheme watcher's is single-goroutine).

func startOSMotionWatcher() {
	go func() {
		c, err := dbus.ConnectSessionBus()
		if err != nil {
			return
		}
		if reduce, ok := osReadReduceMotion(c); ok {
			setOSReduceMotion(reduce)
		}
		if c.AddMatchSignal(dbus.WithMatchInterface(portalIface), dbus.WithMatchMember("SettingChanged"),
			dbus.WithMatchObjectPath(portalPath)) != nil {
			return
		}
		sig := make(chan *dbus.Signal, 8)
		c.Signal(sig)
		for s := range sig {
			if s.Name != portalIface+".SettingChanged" || len(s.Body) < 3 {
				continue
			}
			ns, _ := s.Body[0].(string)
			key, _ := s.Body[1].(string)
			v, isV := s.Body[2].(dbus.Variant)
			if !isV {
				continue
			}
			if reduce, ok := portalMotion(ns, key, v); ok {
				setOSReduceMotion(reduce)
			}
		}
	}()
}

// osReadReduceMotion asks the portal (up to a second per key). Watcher
// goroutine only.
func osReadReduceMotion(c *dbus.Conn) (reduce, ok bool) {
	uiguard.OffUI("osReadReduceMotion")
	obj := c.Object(portalDest, portalPath)
	for _, k := range [][2]string{
		{"org.freedesktop.appearance", "reduced-motion"},
		{"org.gnome.desktop.interface", "enable-animations"},
	} {
		ch := make(chan *dbus.Call, 1)
		call := obj.Go(portalIface+".Read", 0, ch, k[0], k[1])
		select {
		case <-call.Done:
		case <-time.After(time.Second):
			return false, false
		}
		if call.Err != nil {
			continue // no such key here: try the next
		}
		var v dbus.Variant
		if call.Store(&v) != nil {
			continue
		}
		if reduce, ok := portalMotion(k[0], k[1], v); ok {
			return reduce, true
		}
	}
	return false, false
}

// portalMotion decodes either key (Read wraps the value in a second
// variant; SettingChanged doesn't).
func portalMotion(ns, key string, v dbus.Variant) (reduce, ok bool) {
	for i := 0; i < 2; i++ {
		if in, isV := v.Value().(dbus.Variant); isV {
			v = in
		}
	}
	switch {
	case ns == "org.freedesktop.appearance" && key == "reduced-motion":
		n, isU := v.Value().(uint32)
		return n == 1, isU
	case ns == "org.gnome.desktop.interface" && key == "enable-animations":
		on, isB := v.Value().(bool)
		return !on, isB
	}
	return false, false
}
