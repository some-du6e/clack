//go:build linux

package ui

import (
	"sync"

	"github.com/godbus/dbus/v5"
)

// dbusNotifier talks to org.freedesktop.Notifications on the session bus.
type dbusNotifier struct {
	once sync.Once
	conn *dbus.Conn
	err  error
	mu   sync.Mutex
	last uint32
}

func newNotifier() notifier { return &dbusNotifier{} }

func (n *dbusNotifier) Notify(title, body string) error {
	n.once.Do(func() { n.conn, n.err = dbus.ConnectSessionBus() })
	if n.err != nil {
		return n.err
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	obj := n.conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications")
	call := obj.Call("org.freedesktop.Notifications.Notify", 0,
		AppName, uint32(0), "", title, body, []string{}, map[string]dbus.Variant{
			"desktop-entry": dbus.MakeVariant("clack"),
		}, int32(6000))
	if call.Err != nil {
		return call.Err
	}
	_ = call.Store(&n.last)
	return nil
}
