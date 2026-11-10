//go:build linux

package screenlock

import (
	"bufio"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// privateBus starts a dbus-daemon of the test's own (never the user's
// session or system bus) and returns its address.
func privateBus(t *testing.T) string {
	t.Helper()
	bin, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("no dbus-daemon")
	}
	cmd := exec.Command(bin, "--session", "--nofork", "--print-address=1")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Skip("dbus-daemon:", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(line)
}

func dial(t *testing.T, addr string) *dbus.Conn {
	t.Helper()
	c, err := dbus.Connect(addr)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// fakeLogind answers GetSession/GetSessionByPID with one session path.
type fakeLogind struct{ path dbus.ObjectPath }

func (f fakeLogind) GetSession(string) (dbus.ObjectPath, *dbus.Error)      { return f.path, nil }
func (f fakeLogind) GetSessionByPID(uint32) (dbus.ObjectPath, *dbus.Error) { return f.path, nil }

func want(t *testing.T, got chan bool, v bool, what string) {
	t.Helper()
	select {
	case l := <-got:
		if l != v {
			t.Fatalf("%s: got locked=%v, want %v", what, l, v)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("%s: no callback", what)
	}
}

// logind's Lock/Unlock and LockedHint on this process's session, and a
// screen saver's ActiveChanged, all reach the callback (deduplicated),
// on a private bus standing in for the system and session buses.
func TestWatchDBus(t *testing.T) {
	addr := privateBus(t)
	svc := dial(t, addr)
	defer svc.Close()
	if r, err := svc.RequestName(logindDest, dbus.NameFlagDoNotQueue); err != nil || r != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("request name: %v %v", r, err)
	}
	sess := dbus.ObjectPath("/org/freedesktop/login1/session/_31")
	if err := svc.Export(fakeLogind{sess}, logindPath, logindManager); err != nil {
		t.Fatal(err)
	}

	got := make(chan bool, 8)
	stop, ok := watchConnsDedup(dial(t, addr), dial(t, addr), func(l bool) { got <- l })
	if !ok {
		t.Fatal("no source")
	}
	defer stop()
	time.Sleep(200 * time.Millisecond) // the match rules reach the daemon

	emit := func(path dbus.ObjectPath, name string, body ...any) {
		t.Helper()
		if err := svc.Emit(path, name, body...); err != nil {
			t.Fatal(err)
		}
	}
	emit(sess, logindSession+".Lock")
	want(t, got, true, "logind Lock")
	emit(sess, propsIface+".PropertiesChanged", logindSession, map[string]dbus.Variant{"LockedHint": dbus.MakeVariant(true)}, []string{})
	emit(sess, logindSession+".Unlock")
	want(t, got, false, "logind Unlock (a repeated LockedHint=true is dropped)")
	emit(sess, propsIface+".PropertiesChanged", logindSession, map[string]dbus.Variant{"LockedHint": dbus.MakeVariant(true)}, []string{})
	want(t, got, true, "LockedHint")
	emit("/org/freedesktop/login1/session/other", logindSession+".Unlock")
	emit("/org/gnome/ScreenSaver", "org.gnome.ScreenSaver.ActiveChanged", false)
	want(t, got, false, "GNOME screen saver off (another session's Unlock is ignored)")
	emit("/org/freedesktop/ScreenSaver", "org.freedesktop.ScreenSaver.ActiveChanged", true)
	want(t, got, true, "freedesktop screen saver on")
}

// watchConnsDedup is Watch over given connections.
func watchConnsDedup(sys, sess *dbus.Conn, f func(bool)) (func(), bool) {
	var stop func()
	var ok bool
	old := watchFn
	watchFn = func(send func(bool)) (func(), error) {
		stop, ok = watchConns(sys, sess, send)
		if !ok {
			return nil, ErrUnsupported
		}
		return stop, nil
	}
	defer func() { watchFn = old }()
	s, err := Watch(f)
	if err != nil {
		return func() {}, false
	}
	return s, true
}
