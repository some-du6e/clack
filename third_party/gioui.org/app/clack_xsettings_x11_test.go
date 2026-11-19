// SPDX-License-Identifier: Unlicense OR MIT

//go:build linux && !android && !nox11

package app

import (
	"bufio"
	"bytes"
	stdcontext "context"
	"encoding/hex"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Clack patch (CLACK_PATCHES.md §47) on a real X server (ci's linux-gio job,
// under Xvfb with CLACK_X11_DROP_TESTS set): python-xlib plays a settings
// manager (gsd-xsettings, xfsettingsd). It takes _XSETTINGS_S0, sets
// _XSETTINGS_SETTINGS (type _XSETTINGS_SETTINGS, format 8: Net/CursorBlink
// and Net/CursorBlinkTime, a string and a colour) and prints the bytes;
// on "blink0" it sets them again with the blink off; on "quit" it goes, and
// its window with it. ClackXSettings must read each version byte for byte
// and nil once the manager is gone.
const xsettingsManagerPy = `
import sys, struct
from Xlib import X, display
d = display.Display()
root = d.screen().root
sel = d.intern_atom("_XSETTINGS_S%d" % d.get_default_screen())
prop = d.intern_atom("_XSETTINGS_SETTINGS")
win = root.create_window(0, 0, 1, 1, 0, X.CopyFromParent)
def pad(b):
    return b + b"\0" * (-len(b) % 4)
def setting(typ, name, value):
    n = name.encode()
    return struct.pack("<BBH", typ, 0, len(n)) + pad(n) + struct.pack("<I", 7) + value
def settings(blink, ms):
    s = [setting(1, "Net/ThemeName", struct.pack("<I", 7) + pad(b"Adwaita")),
         setting(0, "Net/CursorBlink", struct.pack("<i", blink)),
         setting(2, "Gtk/ColorAccent", struct.pack("<HHHH", 1, 2, 3, 0xffff)),
         setting(0, "Net/CursorBlinkTime", struct.pack("<i", ms))]
    return struct.pack("<BxxxII", 0, 42, len(s)) + b"".join(s)
def publish(blink, ms):
    b = settings(blink, ms)
    win.change_property(prop, prop, 8, b)
    d.sync()
    print(b.hex(), flush=True)
win.set_selection_owner(sel, X.CurrentTime)
publish(1, 800)
for line in sys.stdin:
    if line.strip() == "blink0":
        publish(0, 800)
    else:
        break
win.destroy()
d.sync()
print("gone", flush=True)
`

func TestClackXSettingsX11(t *testing.T) {
	if os.Getenv("CLACK_X11_DROP_TESTS") == "" || os.Getenv("DISPLAY") == "" {
		t.Skip("CLACK_X11_DROP_TESTS or DISPLAY not set")
	}
	if b := ClackXSettings(); b != nil {
		t.Fatalf("the X server has a settings manager before the test's (%d bytes)", len(b))
	}
	ctx, stop := stdcontext.WithTimeout(stdcontext.Background(), time.Minute)
	defer stop()
	cmd := exec.CommandContext(ctx, "python3", "-I", "-c", xsettingsManagerPy)
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	outPipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Wait()
	defer in.Close()
	out := bufio.NewScanner(outPipe)
	next := func(what string) string {
		t.Helper()
		if !out.Scan() {
			t.Fatalf("the manager printed no %s: %v\n%s", what, out.Err(), stderr.String())
		}
		return strings.TrimSpace(out.Text())
	}
	check := func(what string) {
		t.Helper()
		want, err := hex.DecodeString(next(what))
		if err != nil {
			t.Fatal(err)
		}
		if got := ClackXSettings(); !bytes.Equal(got, want) {
			t.Errorf("%s: ClackXSettings read %d bytes %x, want the manager's %d %x", what, len(got), got, len(want), want)
		}
	}
	check("the settings, blink on, 800 ms")
	if _, err := in.Write([]byte("blink0\n")); err != nil {
		t.Fatal(err)
	}
	check("the settings again, blink off")
	if _, err := in.Write([]byte("quit\n")); err != nil {
		t.Fatal(err)
	}
	if s := next("goodbye"); s != "gone" {
		t.Fatalf("the manager said %q, want gone", s)
	}
	if b := ClackXSettings(); b != nil {
		t.Errorf("the manager gone: ClackXSettings read %d bytes, want none", len(b))
	}
}
