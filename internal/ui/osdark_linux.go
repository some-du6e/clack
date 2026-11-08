//go:build linux

package ui

import (
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

var osDarkBus struct {
	once sync.Once
	conn *dbus.Conn
}

// osPrefersDark reads the desktop's colour scheme from the XDG settings
// portal (org.freedesktop.appearance color-scheme: 1 dark, 2 light, 0 no
// preference). ok is false without a portal.
func osPrefersDark() (dark, ok bool) {
	osDarkBus.once.Do(func() {
		if c, err := dbus.ConnectSessionBus(); err == nil {
			osDarkBus.conn = c
		}
	})
	c := osDarkBus.conn
	if c == nil {
		return false, false
	}
	obj := c.Object("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop")
	ch := make(chan *dbus.Call, 1)
	call := obj.Go("org.freedesktop.portal.Settings.Read", 0, ch, "org.freedesktop.appearance", "color-scheme")
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
	for i := 0; i < 2; i++ { // Read wraps the value in a second variant
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
