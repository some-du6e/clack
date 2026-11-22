// SPDX-License-Identifier: Unlicense OR MIT

//go:build linux && !android && !nox11

package app

import (
	"bufio"
	"errors"
	"fmt"
	"image"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// Clack patch (CLACK_PATCHES.md §59) on a real X server: ci's linux-gio
// job runs it with Xvfb installed (CLACK_X11_ASKED_TESTS). A Gio window
// whose X server goes away (here the private Xvfb it's on is killed, as
// the end of an X session or a crash of the server takes it) calls
// X11ConnectionLost before the process exits with status 1, as Xlib's
// default handler exits. Review of karim-1118: Xlib's default handler
// ended Clack with no exit line.
func TestX11ConnectionLost(t *testing.T) {
	if marker := os.Getenv("CLACK_X11_LOST_HELPER"); marker != "" {
		x11LostHelper(marker)
		return
	}
	if os.Getenv("CLACK_X11_ASKED_TESTS") == "" {
		t.Skip("CLACK_X11_ASKED_TESTS not set")
	}
	xvfb, err := exec.LookPath("Xvfb")
	if err != nil {
		t.Skip("no Xvfb")
	}
	n := 0
	for d := 87; d < 120; d++ {
		if _, err := os.Stat(fmt.Sprintf("/tmp/.X11-unix/X%d", d)); errors.Is(err, os.ErrNotExist) {
			if _, err := os.Stat(fmt.Sprintf("/tmp/.X%d-lock", d)); errors.Is(err, os.ErrNotExist) {
				n = d
				break
			}
		}
	}
	if n == 0 {
		t.Fatal("no free X display number")
	}
	disp := fmt.Sprintf(":%d", n)
	srv := exec.Command(xvfb, disp, "-nolisten", "tcp", "-screen", "0", "640x480x24")
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Process.Kill(); _ = srv.Wait() })
	for end := time.Now().Add(10 * time.Second); ; {
		if _, err := os.Stat(fmt.Sprintf("/tmp/.X11-unix/X%d", n)); err == nil {
			break
		}
		if time.Now().After(end) {
			t.Fatalf("Xvfb %s didn't come up", disp)
		}
		time.Sleep(50 * time.Millisecond)
	}
	marker := filepath.Join(t.TempDir(), "lost")
	helper := exec.Command(os.Args[0], "-test.run=^TestX11ConnectionLost$", "-test.count=1")
	env := []string{"DISPLAY=" + disp, "CLACK_X11_LOST_HELPER=" + marker}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "DISPLAY=") && !strings.HasPrefix(kv, "WAYLAND_DISPLAY=") {
			env = append(env, kv)
		}
	}
	helper.Env = env
	out, err := helper.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	helper.Stderr = os.Stderr
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	up := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			if line := sc.Text(); strings.HasPrefix(line, "x11 helper:") {
				t.Log(line)
				if strings.Contains(line, "framed") {
					up <- line
				}
			}
		}
	}()
	select {
	case <-up:
	case <-time.After(30 * time.Second):
		_ = helper.Process.Kill()
		t.Fatal("the helper's window drew no frame on the private Xvfb")
	}
	// the X server goes
	if err := srv.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- helper.Wait() }()
	var werr error
	select {
	case werr = <-done:
	case <-time.After(20 * time.Second):
		_ = helper.Process.Kill()
		t.Fatal("the helper didn't end after its X server went")
	}
	var ee *exec.ExitError
	if !errors.As(werr, &ee) || ee.ExitCode() != 1 {
		t.Errorf("the helper ended with %v, want exit status 1 (Xlib's IO error)", werr)
	}
	if b, err := os.ReadFile(marker); err != nil || string(b) != "lost" {
		t.Errorf("X11ConnectionLost wasn't called before the exit: %q %v", b, err)
	}
}

// x11LostHelper is the helper process: a window on DISPLAY that draws,
// X11ConnectionLost writing marker.
func x11LostHelper(marker string) {
	os.Unsetenv("WAYLAND_DISPLAY")
	X11ConnectionLost = func() { _ = os.WriteFile(marker, []byte("lost"), 0o600) }
	w := new(Window)
	w.Option(Title("Clack X11 lost test"), Size(200, 150))
	var ops op.Ops
	framed := false
	for {
		switch e := w.Event().(type) {
		case FrameEvent:
			ops.Reset()
			cl := clip.Rect(image.Rectangle{Max: e.Size}).Push(&ops)
			paint.ColorOp{Color: color.NRGBA{R: 0x40, G: 0x80, B: 0xc0, A: 0xff}}.Add(&ops)
			paint.PaintOp{}.Add(&ops)
			cl.Pop()
			e.Frame(&ops)
			if !framed {
				framed = true
				fmt.Println("x11 helper: framed")
			}
		case DestroyEvent:
			fmt.Printf("x11 helper: destroyed: %v\n", e.Err)
			os.Exit(3)
		}
	}
}
