//go:build linux && cgo && !android && !nox11

package globalkey

/*
#cgo pkg-config: x11
#include <stdlib.h>
#include <X11/Xlib.h>
#include <X11/keysym.h>

static int grabFailed;

static int onXError(Display *d, XErrorEvent *e) {
	if (e->error_code == BadAccess) grabFailed = 1;
	return 0;
}

// clack_grab grabs Ctrl+Shift+Space on the root window with every
// combination of Caps Lock (LockMask) and Num Lock (Mod2Mask): 0 ok, 1
// another client holds it.
static int clack_grab(Display *d) {
	Window root = DefaultRootWindow(d);
	KeyCode kc = XKeysymToKeycode(d, XK_space);
	unsigned int locks[4] = {0, LockMask, Mod2Mask, LockMask | Mod2Mask};
	grabFailed = 0;
	XErrorHandler old = XSetErrorHandler(onXError);
	for (int i = 0; i < 4; i++) {
		XGrabKey(d, kc, ControlMask | ShiftMask | locks[i], root, False, GrabModeAsync, GrabModeAsync);
	}
	XSync(d, False);
	XSetErrorHandler(old);
	return grabFailed;
}

static void clack_ungrab(Display *d) {
	Window root = DefaultRootWindow(d);
	KeyCode kc = XKeysymToKeycode(d, XK_space);
	unsigned int locks[4] = {0, LockMask, Mod2Mask, LockMask | Mod2Mask};
	for (int i = 0; i < 4; i++) {
		XUngrabKey(d, kc, ControlMask | ShiftMask | locks[i], root);
	}
	XSync(d, False);
}

// clack_presses drains the queue and counts KeyPress events.
static int clack_presses(Display *d) {
	int n = 0;
	while (XPending(d) > 0) {
		XEvent ev;
		XNextEvent(d, &ev);
		if (ev.type == KeyPress) n++;
	}
	return n;
}
*/
import "C"

import (
	"os"
	"runtime"
	"sync"

	"golang.org/x/sys/unix"
)

// X11: a display connection of its own, on one locked thread, polled with
// a pipe that stop writes to.
func register(onPress func()) (func(), error) {
	if os.Getenv("WAYLAND_DISPLAY") != "" && os.Getenv("CLACK_X11") == "" {
		return nil, ErrWayland
	}
	if os.Getenv("DISPLAY") == "" {
		return nil, ErrUnsupported
	}
	var p [2]int
	if err := unix.Pipe2(p[:], unix.O_CLOEXEC|unix.O_NONBLOCK); err != nil {
		return nil, err
	}
	ready := make(chan error, 1)
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		defer unix.Close(p[0])
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		d := C.XOpenDisplay(nil)
		if d == nil {
			ready <- ErrUnsupported
			return
		}
		defer C.XCloseDisplay(d)
		if C.clack_grab(d) != 0 {
			C.clack_ungrab(d)
			ready <- ErrTaken
			return
		}
		defer C.clack_ungrab(d)
		ready <- nil
		fds := []unix.PollFd{{Fd: int32(C.XConnectionNumber(d)), Events: unix.POLLIN}, {Fd: int32(p[0]), Events: unix.POLLIN}}
		for {
			for n := C.clack_presses(d); n > 0; n-- {
				go onPress()
			}
			fds[0].Revents, fds[1].Revents = 0, 0
			if _, err := unix.Poll(fds, -1); err != nil && err != unix.EINTR {
				return
			}
			if fds[1].Revents != 0 || fds[0].Revents&(unix.POLLHUP|unix.POLLERR) != 0 {
				return
			}
		}
	}()
	if err := <-ready; err != nil {
		<-exited
		unix.Close(p[1])
		return nil, err
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			unix.Write(p[1], []byte{0})
			<-exited
			unix.Close(p[1])
		})
	}, nil
}
