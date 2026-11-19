//go:build linux

package ui

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/widget"
)

// The caret's blink on Linux: half GTK's gtk-cursor-blink-time, none when
// gtk-cursor-blink is off, GTK's 1200 ms when nothing sets it (Chromium's
// OsSettingsProviderGtk::CaretBlinkInterval). The desktop's settings
// (XSETTINGS or the portal) over GTK 3's settings.ini files, which load in
// GTK's order (/usr/share, /etc, XDG_CONFIG_DIRS, XDG_CONFIG_HOME, a later
// one's value over an earlier one's) as GLib's key file reads them. The
// system's directories are the test's own, not the machine's.
func TestGtkCaretBlink(t *testing.T) {
	home, sys1, sys2, data, etc := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("XDG_CONFIG_DIRS", sys1+":"+sys2)
	defer func(d, e string) { gtkDataDir, gtkSysconfDir = d, e }(gtkDataDir, gtkSysconfDir)
	gtkDataDir, gtkSysconfDir = data, etc
	write := func(dir, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(dir, "gtk-3.0"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "gtk-3.0", "settings.ini"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	check := func(what string, desk gtkBlink, want time.Duration) {
		t.Helper()
		if got := desk.over(gtkSettingsIni()).interval(); got != want {
			t.Errorf("%s: %v, want %v", what, got, want)
		}
	}
	none := gtkBlink{}
	check("nothing set", none, 600*time.Millisecond)
	// GTK 4's files aren't GTK 3's
	if err := os.MkdirAll(filepath.Join(home, "gtk-4.0"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "gtk-4.0", "settings.ini"), []byte("[Settings]\ngtk-cursor-blink-time=300\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	check("GTK 4's file only", none, 600*time.Millisecond)
	write(data, "[Settings]\ngtk-cursor-blink-time=1400\n")
	check("/usr/share's 1400 ms", none, 700*time.Millisecond)
	write(etc, "[Settings]\ngtk-cursor-blink-time=1000\n")
	check("/etc's 1000 ms over /usr/share's", none, 500*time.Millisecond)
	write(sys1, "[Settings]\ngtk-cursor-blink-time=1600\n")
	write(sys2, "# Xubuntu's\n[Settings]\ngtk-cursor-blink-time=1800\n")
	check("XDG_CONFIG_DIRS' second over its first (GTK loads them in order)", none, 900*time.Millisecond)
	write(home, "[Other]\ngtk-cursor-blink-time=200\n[Settings]\ngtk-theme-name=Adwaita\ngtk-cursor-blink-time = 1100\n")
	check("the user's 1100 ms, the [Settings] group's", none, 550*time.Millisecond)
	write(home, "[Settings]\ngtk-cursor-blink-time=1100\ngtk-cursor-blink-time=1300\n")
	check("a key's last line", none, 650*time.Millisecond)
	write(home, "[Settings]\ngtk-cursor-blink-time=40\n")
	check("40 ms, under GTK's 100 ms minimum: refused, the system's stays", none, 900*time.Millisecond)
	write(home, "[Settings]\ngtk-cursor-blink-time=1000\ngtk-cursor-blink-time=40\n")
	check("a refused last line refuses the key", none, 900*time.Millisecond)
	write(home, "[Settings]\ngtk-cursor-blink-time=1201\n")
	check("1201 ms: half in Chromium's integer milliseconds", none, 600*time.Millisecond)
	write(home, "[Settings]\ngtk-cursor-blink-time=1000 ms\n")
	check("1000 then a space (strtol's)", none, 500*time.Millisecond)
	write(home, "[Settings]\ngtk-cursor-blink-time=1000ms\n")
	check("1000ms: not an integer", none, 900*time.Millisecond)
	write(home, "[Settings]\ngtk-cursor-blink-time=1000\nthis line is no pair\n")
	check("a line that is no pair: the file doesn't load", none, 900*time.Millisecond)
	write(home, "gtk-cursor-blink-time=1000\n[Settings]\n")
	check("a pair before any group: the file doesn't load", none, 900*time.Millisecond)
	write(home, "[Settings]\ngtk-cursor-blink=yes\n")
	check("blink=yes: not a key file boolean", none, 900*time.Millisecond)
	write(home, "[Settings]\ngtk-cursor-blink=False\n")
	check("blink=False: not a key file boolean (case matters)", none, 900*time.Millisecond)
	write(home, "[Settings]\ngtk-cursor-blink=0\n")
	check("blink=0: off", none, 0)
	write(home, "[Settings]\ngtk-cursor-blink=false\n")
	check("blink=false: off", none, 0)
	// the desktop's over the files; its time raised to GTK's minimum
	check("the desktop's blink on and 1000 ms", gtkBlink{blink: true, okBlink: true, period: time.Second, okPeriod: true}, 500*time.Millisecond)
	write(home, "[Settings]\ngtk-cursor-blink-time=1000\n")
	check("the desktop's blink off", gtkBlink{blink: false, okBlink: true}, 0)
	check("the desktop's 2000 ms over the user's 1000", gtkBlink{period: 2 * time.Second, okPeriod: true}, time.Second)
	check("the desktop's 30 ms, raised to 100", xsettingsBlink(xsettingsProp(binary.LittleEndian, 1, 30)), 50*time.Millisecond)
}

// xsettingsProp is an XSETTINGS property (the spec's format) in byte order
// bo: a string and a colour around Net/CursorBlink (blink) and
// Net/CursorBlinkTime (ms; negative: unset).
func xsettingsProp(bo binary.ByteOrder, blink, ms int32) []byte {
	var b []byte
	u16 := func(v uint16) {
		var t [2]byte
		bo.PutUint16(t[:], v)
		b = append(b, t[:]...)
	}
	u32 := func(v uint32) {
		var t [4]byte
		bo.PutUint32(t[:], v)
		b = append(b, t[:]...)
	}
	padTo4 := func() {
		for len(b)%4 != 0 {
			b = append(b, 0)
		}
	}
	name := func(typ byte, n string) {
		b = append(b, typ, 0)
		u16(uint16(len(n)))
		b = append(b, n...)
		padTo4()
		u32(7) // last-change serial
	}
	order := byte(0)
	if bo == binary.ByteOrder(binary.BigEndian) {
		order = 1
	}
	b = append(b, order, 0, 0, 0)
	u32(42) // serial
	n := uint32(4)
	if ms < 0 {
		n = 3
	}
	u32(n)
	name(1, "Net/ThemeName")
	u32(5)
	b = append(b, "Adwai"...)
	padTo4()
	name(0, "Net/CursorBlink")
	u32(uint32(blink))
	name(2, "Gtk/ColorAccent")
	u16(1)
	u16(2)
	u16(3)
	u16(0xffff)
	if ms >= 0 {
		name(0, "Net/CursorBlinkTime")
		u32(uint32(ms))
	}
	return b
}

// XSETTINGS as GDK's X11 backend maps them to GTK's settings, in either
// byte order; a truncated or foreign property says nothing.
func TestXSettingsBlink(t *testing.T) {
	for _, bo := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		s := xsettingsBlink(xsettingsProp(bo, 1, 800))
		if !s.okBlink || !s.blink || !s.okPeriod || s.period != 800*time.Millisecond || s.interval() != 400*time.Millisecond {
			t.Errorf("%v: blink on, 800 ms: %+v (interval %v), want on, 800 ms (400 ms)", bo, s, s.interval())
		}
		if s := xsettingsBlink(xsettingsProp(bo, 0, 800)); !s.okBlink || s.blink || s.interval() != 0 {
			t.Errorf("%v: blink off: %+v, want off (no blinking)", bo, s)
		}
		if s := xsettingsBlink(xsettingsProp(bo, 1, -1)); !s.okBlink || s.okPeriod {
			t.Errorf("%v: no Net/CursorBlinkTime: %+v, want the blink only", bo, s)
		}
		p := xsettingsProp(bo, 1, 800)
		for _, cut := range []int{0, 5, 11, 20, len(p) - 3} {
			if s := xsettingsBlink(p[:cut]); s.okBlink || s.okPeriod {
				t.Errorf("%v: cut at %d of %d bytes: %+v, want nothing", bo, cut, len(p), s)
			}
		}
	}
	if s := xsettingsBlink(append([]byte{7}, xsettingsProp(binary.LittleEndian, 1, 800)[1:]...)); s.okBlink || s.okPeriod {
		t.Errorf("byte order 7: %+v, want nothing", s)
	}
	if s := xsettingsBlink(nil); s.okBlink || s.okPeriod {
		t.Errorf("no manager: %+v, want nothing", s)
	}
}

// The watcher on X11 applies the settings manager's XSETTINGS over GTK's
// files, read once (as Chromium reads the interval).
func TestCaretBlinkWatcherX11(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_DIRS", t.TempDir())
	defer func(d, e string) { gtkDataDir, gtkSysconfDir = d, e }(gtkDataDir, gtkSysconfDir)
	gtkDataDir, gtkSysconfDir = t.TempDir(), t.TempDir()
	defer func(f func() []byte) { readXSettings = f }(readXSettings)
	readXSettings = func() []byte { return xsettingsProp(binary.LittleEndian, 1, 800) }
	widget.SetCaretBlinkInterval(-1)
	t.Cleanup(func() { widget.SetCaretBlinkInterval(-1) })
	startCaretBlinkWatcher()
	for dl := time.Now().Add(10 * time.Second); widget.CaretBlinkInterval() != 400*time.Millisecond; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(dl) {
			t.Fatalf("the caret blinks every %v, want the manager's 800 ms halved", widget.CaretBlinkInterval())
		}
	}
}
