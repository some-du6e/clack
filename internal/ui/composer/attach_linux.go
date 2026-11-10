//go:build linux && !android

package composer

import (
	"context"
	"os"
	"os/exec"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/some-du6e/clack/internal/ui/uiguard"
)

// attExplorerUsable reports whether the xdg-desktop-portal FileChooser is
// likely to answer. The Gio explorer talks to the portal over D-Bus and
// blocks on a Response signal that never arrives when no portal backend is
// registered (headless sessions, bare Xvfb), so we probe first and skip
// the explorer when the portal name has no owner. The probe is bounded so
// a wedged bus can't hang the picker.
func attExplorerUsable() bool {
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return false
	}
	type res struct {
		owned bool
	}
	ch := make(chan res, 1)
	go func() {
		conn, err := dbus.SessionBus()
		if err != nil {
			ch <- res{}
			return
		}
		var has bool
		err = conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0,
			"org.freedesktop.portal.Desktop").Store(&has)
		ch <- res{owned: err == nil && has}
	}()
	select {
	case r := <-ch:
		return r.owned
	case <-time.After(400 * time.Millisecond):
		return false
	}
}

// Linux clipboard images and file pickers, via command-line tools when
// installed: wl-paste (wl-clipboard) on Wayland, xclip on X11/XWayland;
// zenity or kdialog for the chooser.

func attClipboardSupported() bool { return true }

// attClipboardImage reads an image from the system clipboard. It returns
// errAttNoClipImage when the clipboard holds none and errAttClipTools when
// no clipboard tool is installed.
func attClipboardImage(ctx context.Context, explicit bool) ([]byte, string, error) {
	wayland := os.Getenv("WAYLAND_DISPLAY") != ""
	x11 := os.Getenv("DISPLAY") != ""
	switch {
	case wayland && attHave("wl-paste"):
		return attClipVia(ctx, explicit,
			[]string{"wl-paste", "--list-types"},
			func(t string) []string { return []string{"wl-paste", "--no-newline", "--type", t} })
	case x11 && attHave("xclip"):
		return attClipVia(ctx, explicit,
			[]string{"xclip", "-selection", "clipboard", "-t", "TARGETS", "-o"},
			func(t string) []string { return []string{"xclip", "-selection", "clipboard", "-t", t, "-o"} })
	}
	return nil, "", errAttClipTools
}

func attClipVia(ctx context.Context, explicit bool, list []string, get func(typ string) []string) ([]byte, string, error) {
	out, err := attRun(ctx, 1<<20, list[0], list[1:]...)
	if err != nil {
		// Both tools exit non-zero when the clipboard is empty.
		return nil, "", errAttNoClipImage
	}
	typ := attPickImageType(attParseTargets(string(out)), explicit)
	if typ == "" {
		return nil, "", errAttNoClipImage
	}
	args := get(typ)
	data, err := attRun(ctx, attMaxFileSize, args[0], args[1:]...)
	if err == errAttTooBig {
		return nil, "", err
	}
	if err != nil || len(data) == 0 {
		return nil, "", errAttNoClipImage
	}
	return data, typ, nil
}

// attNativePick runs zenity or kdialog. ok is false when neither is
// installed (or there's no display); paths is nil when the user cancelled.
func attNativePick(ctx context.Context) (paths []string, ok bool, err error) {
	uiguard.OffUI("exec zenity/kdialog")
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return nil, false, nil
	}
	home, _ := os.UserHomeDir()
	switch {
	case attHave("zenity"):
		args := []string{"--file-selection", "--multiple", "--separator=\n", "--title=Upload files"}
		if home != "" {
			args = append(args, "--filename="+home+"/")
		}
		c := exec.CommandContext(ctx, "zenity", args...)
		c.WaitDelay = attWaitDelay
		out, err := c.Output()
		return attPickResult(out, err)
	case attHave("kdialog"):
		if home == "" {
			home = "/"
		}
		c := exec.CommandContext(ctx, "kdialog", "--title", "Upload files",
			"--getopenfilename", home, "--multiple", "--separate-output")
		c.WaitDelay = attWaitDelay
		out, err := c.Output()
		return attPickResult(out, err)
	}
	return nil, false, nil
}
