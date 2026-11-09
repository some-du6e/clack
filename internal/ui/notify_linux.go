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

	sigOnce sync.Once
	on      map[uint32]func(string) // notification id -> its buttons' handler
}

func newNotifier() notifier { return &dbusNotifier{} }

func (n *dbusNotifier) Notify(title, body string) error {
	return n.NotifyActions(title, body, nil, nil)
}

const notifyIface = "org.freedesktop.Notifications"

func (n *dbusNotifier) NotifyActions(title, body string, actions []string, on func(string)) error {
	n.once.Do(func() { n.conn, n.err = dbus.ConnectSessionBus() })
	if n.err != nil {
		return n.err
	}
	if len(actions) > 0 && on != nil {
		n.listen()
	}
	if actions == nil {
		actions = []string{}
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	obj := n.conn.Object(notifyIface, "/org/freedesktop/Notifications")
	call := obj.Call(notifyIface+".Notify", 0,
		AppName, uint32(0), "", title, body, actions, map[string]dbus.Variant{
			"desktop-entry": dbus.MakeVariant("clack"),
		}, int32(6000))
	if call.Err != nil {
		return call.Err
	}
	_ = call.Store(&n.last)
	if len(actions) > 0 && on != nil {
		if n.on == nil {
			n.on = map[uint32]func(string){}
		}
		n.on[n.last] = on
	}
	return nil
}

// listen dispatches ActionInvoked to the notification's handler (and
// forgets it on NotificationClosed).
func (n *dbusNotifier) listen() {
	n.sigOnce.Do(func() {
		for _, m := range []string{"ActionInvoked", "NotificationClosed"} {
			if err := n.conn.AddMatchSignal(dbus.WithMatchInterface(notifyIface), dbus.WithMatchMember(m)); err != nil {
				return
			}
		}
		ch := make(chan *dbus.Signal, 16)
		n.conn.Signal(ch)
		go func() {
			for s := range ch {
				if len(s.Body) < 2 {
					continue
				}
				id, ok := s.Body[0].(uint32)
				if !ok {
					continue
				}
				n.mu.Lock()
				f := n.on[id]
				if s.Name == notifyIface+".NotificationClosed" {
					delete(n.on, id)
					f = nil
				}
				n.mu.Unlock()
				if key, ok := s.Body[1].(string); ok && f != nil {
					f(key)
				}
			}
		}()
	})
}
