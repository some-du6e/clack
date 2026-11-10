//go:build linux && cgo && !android && !nox11

package globalkey

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

// Under an X server (CI: xvfb-run), Ctrl+Shift+Space typed with XTest
// (xdotool) reaches the grab while no Clack window has focus; a second
// client can't take it; once released it's free again.
func TestRegisterX11(t *testing.T) {
	if os.Getenv("DISPLAY") == "" {
		t.Skip("no X display")
	}
	xdo, err := exec.LookPath("xdotool")
	if err != nil {
		t.Skip("no xdotool")
	}
	t.Setenv("WAYLAND_DISPLAY", "")
	pressed := make(chan struct{}, 4)
	stop, err := Register(func() { pressed <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if out, err := exec.Command(xdo, "key", "--clearmodifiers", "ctrl+shift+space").CombinedOutput(); err != nil {
		t.Fatalf("xdotool: %v %s", err, out)
	}
	select {
	case <-pressed:
	case <-time.After(5 * time.Second):
		t.Fatal("the grab got no press")
	}
	// with Num Lock on too
	_ = exec.Command(xdo, "key", "Num_Lock").Run()
	_ = exec.Command(xdo, "key", "ctrl+shift+space").Run()
	_ = exec.Command(xdo, "key", "Num_Lock").Run()
	select {
	case <-pressed:
	case <-time.After(5 * time.Second):
		t.Fatal("no press with Num Lock on")
	}
	stop()
	stop2, err := Register(func() {})
	if err != nil {
		t.Fatalf("register after stop: %v", err)
	}
	stop2()
}

// A Wayland session has no global grabs.
func TestRegisterWayland(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	t.Setenv("CLACK_X11", "")
	if _, err := Register(func() {}); err != ErrWayland {
		t.Fatalf("err = %v, want ErrWayland", err)
	}
}
