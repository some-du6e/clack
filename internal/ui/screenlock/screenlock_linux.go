//go:build linux

package screenlock

import (
	"os"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	logindDest    = "org.freedesktop.login1"
	logindPath    = "/org/freedesktop/login1"
	logindManager = "org.freedesktop.login1.Manager"
	logindSession = "org.freedesktop.login1.Session"
	propsIface    = "org.freedesktop.DBus.Properties"
)

// screenSavers are the session-bus screen savers whose ActiveChanged(bool)
// means the screen saver (and, on GNOME, the lock screen) started.
var screenSavers = []struct{ iface, path string }{
	{"org.freedesktop.ScreenSaver", "/org/freedesktop/ScreenSaver"},
	{"org.freedesktop.ScreenSaver", "/ScreenSaver"},
	{"org.gnome.ScreenSaver", "/org/gnome/ScreenSaver"},
}

func watch(send func(bool)) (func(), error) {
	sys, _ := dbus.ConnectSystemBus()
	sess, _ := dbus.ConnectSessionBus()
	if sys == nil && sess == nil {
		return nil, ErrUnsupported
	}
	stop, ok := watchConns(sys, sess, send)
	if !ok {
		stop()
		return nil, ErrUnsupported
	}
	return stop, nil
}

// watchConns listens on the given buses (either may be nil) and closes
// them on stop. ok is false when neither has a source to listen to.
func watchConns(sys, sess *dbus.Conn, send func(bool)) (stop func(), ok bool) {
	done := make(chan struct{})
	var conns []*dbus.Conn
	if sys != nil {
		if path := sessionPath(sys); path != "" {
			sigs := make(chan *dbus.Signal, 16)
			sys.Signal(sigs)
			okSig := sys.AddMatchSignal(dbus.WithMatchObjectPath(path), dbus.WithMatchInterface(logindSession)) == nil
			okProp := sys.AddMatchSignal(dbus.WithMatchObjectPath(path), dbus.WithMatchInterface(propsIface), dbus.WithMatchMember("PropertiesChanged")) == nil
			if okSig || okProp {
				ok = true
				conns = append(conns, sys)
				go logindLoop(sigs, path, send, done)
			}
		}
		if !ok {
			sys.Close()
		}
	}
	if sess != nil {
		sigs := make(chan *dbus.Signal, 16)
		sess.Signal(sigs)
		any := false
		for _, s := range screenSavers {
			if sess.AddMatchSignal(dbus.WithMatchInterface(s.iface), dbus.WithMatchMember("ActiveChanged")) == nil {
				any = true
			}
		}
		if any {
			ok = true
			conns = append(conns, sess)
			go saverLoop(sigs, send, done)
		} else {
			sess.Close()
		}
	}
	return func() {
		close(done)
		for _, c := range conns {
			c.Close()
		}
	}, ok
}

// sessionPath is this process's logind session object ("" without one).
func sessionPath(c *dbus.Conn) dbus.ObjectPath {
	m := c.Object(logindDest, logindPath)
	call := func(method string, arg any) dbus.ObjectPath {
		ch := make(chan *dbus.Call, 1)
		cl := m.Go(logindManager+"."+method, 0, ch, arg)
		select {
		case <-cl.Done:
		case <-time.After(2 * time.Second):
			return ""
		}
		var p dbus.ObjectPath
		if cl.Err != nil || cl.Store(&p) != nil {
			return ""
		}
		return p
	}
	if id := os.Getenv("XDG_SESSION_ID"); id != "" {
		if p := call("GetSession", id); p != "" {
			return p
		}
	}
	return call("GetSessionByPID", uint32(os.Getpid()))
}

func logindLoop(sigs chan *dbus.Signal, path dbus.ObjectPath, send func(bool), done chan struct{}) {
	for {
		select {
		case <-done:
			return
		case s, open := <-sigs:
			if !open {
				return
			}
			if s == nil || s.Path != path {
				continue
			}
			switch s.Name {
			case logindSession + ".Lock":
				send(true)
			case logindSession + ".Unlock":
				send(false)
			case propsIface + ".PropertiesChanged":
				if len(s.Body) < 2 {
					continue
				}
				if iface, _ := s.Body[0].(string); iface != logindSession {
					continue
				}
				if ch, ok := s.Body[1].(map[string]dbus.Variant); ok {
					if v, ok := ch["LockedHint"]; ok {
						if b, ok := v.Value().(bool); ok {
							send(b)
						}
					}
				}
			}
		}
	}
}

func saverLoop(sigs chan *dbus.Signal, send func(bool), done chan struct{}) {
	for {
		select {
		case <-done:
			return
		case s, open := <-sigs:
			if !open {
				return
			}
			if s == nil || len(s.Body) < 1 {
				continue
			}
			for _, ss := range screenSavers {
				if s.Name == ss.iface+".ActiveChanged" {
					if b, ok := s.Body[0].(bool); ok {
						send(b)
					}
					break
				}
			}
		}
	}
}
