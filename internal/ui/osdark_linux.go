//go:build linux

package ui

import (
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/uiguard"
)

// Linux: the XDG settings portal's org.freedesktop.appearance color-scheme
// (1 dark, 2 light, 0 no preference), read and watched (SettingChanged) on
// the watcher goroutine. D-Bus calls can wait on the bus, never on the UI
// goroutine.

const (
	portalDest  = "org.freedesktop.portal.Desktop"
	portalPath  = "/org/freedesktop/portal/desktop"
	portalIface = "org.freedesktop.portal.Settings"
)

// osPrefersDark reads the portal once (up to a second). ok is false
// without a portal or a preference. Watcher goroutine only.
func osPrefersDark() (dark, ok bool) {
	uiguard.OffUI("osPrefersDark")
	c := osDarkConn()
	if c == nil {
		return false, false
	}
	obj := c.Object(portalDest, portalPath)
	ch := make(chan *dbus.Call, 1)
	call := obj.Go(portalIface+".Read", 0, ch, "org.freedesktop.appearance", "color-scheme")
	select {
	case <-call.Done:
	case <-time.After(time.Second):
		return false, false
	}
	if call.Err != nil {
		return false, false
	}
	var v dbus.Variant
	if call.Store(&v) != nil {
		return false, false
	}
	return portalScheme(v)
}

// portalScheme decodes a color-scheme value (Read wraps it in a second
// variant; SettingChanged doesn't).
func portalScheme(v dbus.Variant) (dark, ok bool) {
	for i := 0; i < 2; i++ {
		if in, isV := v.Value().(dbus.Variant); isV {
			v = in
		}
	}
	n, isU := v.Value().(uint32)
	if !isU || n == 0 {
		return false, false
	}
	return n == 1, true
}

var osDarkBus *dbus.Conn

// osDarkConn is the watcher's own session-bus connection (watcher
// goroutine only).
func osDarkConn() *dbus.Conn {
	if osDarkBus == nil {
		if c, err := dbus.ConnectSessionBus(); err == nil {
			osDarkBus = c
		}
	}
	return osDarkBus
}

func startOSSchemeWatcher() {
	go func() {
		if c := osDarkConn(); c != nil {
			if c.AddMatchSignal(dbus.WithMatchInterface(portalIface), dbus.WithMatchMember("SettingChanged"),
				dbus.WithMatchObjectPath(portalPath)) == nil {
				sig := make(chan *dbus.Signal, 8)
				c.Signal(sig)
				go func() {
					for s := range sig {
						if s.Name != portalIface+".SettingChanged" || len(s.Body) < 3 {
							continue
						}
						ns, _ := s.Body[0].(string)
						key, _ := s.Body[1].(string)
						v, isV := s.Body[2].(dbus.Variant)
						if ns != "org.freedesktop.appearance" || key != "color-scheme" || !isV {
							continue
						}
						if dark, ok := portalScheme(v); ok {
							setOSScheme(dark)
						}
					}
				}()
			}
		}
		for {
			if dark, ok := osPrefersDark(); ok {
				setOSScheme(dark)
			}
			time.Sleep(osSchemePoll)
		}
	}()
}
