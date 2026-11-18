//go:build linux

package ui

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"

	"gioui.org/app"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/uiguard"
)

// The caret's blink on Linux is Chromium's (OsSettingsProviderGtk::
// CaretBlinkInterval, ui/gtk/os_settings_provider_gtk.cc, main
// 2026-11-17): half of GTK's gtk-cursor-blink-time (a whole on-off
// cycle; 1200 ms when nothing sets it), none when gtk-cursor-blink is
// off. GTK's timeout (gtk-cursor-blink-timeout) is GTK's own widgets':
// Blink's caret (FrameCaret) blinks on, and so does Slack's.
//
// GTK 3 (Chromium's default GTK; gtk/gtksettings.c, gtk-3-24) takes the
// two settings from the display's backend first: on X11 the settings
// manager's XSETTINGS (Net/CursorBlink, Net/CursorBlinkTime: GNOME's
// gsd-xsettings, Xfce's xfsettingsd, ...), on Wayland the XDG settings
// portal's org.gnome.desktop.interface (cursor-blink, cursor-blink-time);
// a time from there under GTK's minimum of 100 ms is raised to it
// (g_param_value_validate). What the backend doesn't set comes from
// settings.ini, read once as GTK reads them at start: GTK 3's
// /usr/share/gtk-3.0, /etc/gtk-3.0, each of XDG_CONFIG_DIRS (/etc/xdg) and
// then XDG_CONFIG_HOME (~/.config), a later file's value over an earlier
// one's; a value GTK's key file doesn't parse (booleans true, false, 1, 0)
// or outside the setting's range (a time under 100 ms) is skipped. Read on
// a goroutine of its own (X11 and D-Bus wait): XSETTINGS again every
// xsettingsPoll (GTK hears the manager's changes at once), the portal
// again when it says they changed. Clack's window is on Wayland when
// WAYLAND_DISPLAY is set (Gio tries Wayland first), else on X11.

const (
	// gtkBlinkTime is GTK's default gtk-cursor-blink-time; gtkBlinkMin its
	// minimum.
	gtkBlinkTime = 1200 * time.Millisecond
	gtkBlinkMin  = 100 * time.Millisecond
	// xsettingsPoll is how often XSETTINGS are read again.
	xsettingsPoll = 5 * time.Second
)

// gtkDataDir and gtkSysconfDir are GTK's data prefix's share and its
// sysconfdir (variables: tests).
var gtkDataDir, gtkSysconfDir = "/usr/share", "/etc"

// gtkBlink is what one of GTK's sources sets of the caret.
type gtkBlink struct {
	blink, okBlink bool
	period         time.Duration
	okPeriod       bool
}

// over is s with what s doesn't set taken from under.
func (s gtkBlink) over(under gtkBlink) gtkBlink {
	if !s.okBlink {
		s.blink, s.okBlink = under.blink, under.okBlink
	}
	if !s.okPeriod {
		s.period, s.okPeriod = under.period, under.okPeriod
	}
	return s
}

// interval is the caret's blink interval GTK's settings give Chromium.
func (s gtkBlink) interval() time.Duration {
	s = s.over(gtkBlink{blink: true, okBlink: true, period: gtkBlinkTime, okPeriod: true})
	if !s.blink {
		return 0
	}
	return s.period / 2
}

func startCaretBlinkWatcher() {
	go func() {
		ini := gtkSettingsIni()
		if os.Getenv("WAYLAND_DISPLAY") == "" {
			last := time.Duration(-1)
			for {
				if d := xsettingsBlink(app.ClackXSettings()).over(ini).interval(); d != last {
					widget.SetCaretBlinkInterval(d)
					last = d
				}
				time.Sleep(xsettingsPoll)
			}
		}
		c := caretBus()
		apply := func() { widget.SetCaretBlinkInterval(portalBlink(c).over(ini).interval()) }
		apply()
		if c == nil {
			return
		}
		if c.AddMatchSignal(dbus.WithMatchInterface(portalIface), dbus.WithMatchMember("SettingChanged"),
			dbus.WithMatchObjectPath(portalPath)) != nil {
			return
		}
		sig := make(chan *dbus.Signal, 8)
		c.Signal(sig)
		for s := range sig {
			if s.Name != portalIface+".SettingChanged" || len(s.Body) < 2 {
				continue
			}
			ns, _ := s.Body[0].(string)
			key, _ := s.Body[1].(string)
			if ns == "org.gnome.desktop.interface" && (key == "cursor-blink" || key == "cursor-blink-time") {
				apply()
			}
		}
	}()
}

// caretBus is a session-bus connection of the watcher's own, or nil: only
// to a bus that's running (never dbus-launch's autolaunch, which would
// start a daemon). Watcher goroutine only.
func caretBus() *dbus.Conn {
	uiguard.OffUI("caretBus")
	c, err := dbus.SessionBusPrivateNoAutoStartup()
	if err != nil {
		return nil
	}
	if c.Auth(nil) != nil || c.Hello() != nil {
		c.Close()
		return nil
	}
	return c
}

// portalBlink is what the XDG settings portal says (c: the session bus, or
// nil), as GDK's Wayland backend reads it.
func portalBlink(c *dbus.Conn) gtkBlink {
	uiguard.OffUI("portalBlink")
	var s gtkBlink
	if c == nil {
		return s
	}
	if v, ok := portalRead(c, "org.gnome.desktop.interface", "cursor-blink"); ok {
		if b, ok := v.(bool); ok {
			s.blink, s.okBlink = b, true
		}
	}
	if v, ok := portalRead(c, "org.gnome.desktop.interface", "cursor-blink-time"); ok {
		if n, ok := v.(int32); ok {
			s.period, s.okPeriod = max(time.Duration(n)*time.Millisecond, gtkBlinkMin), true
		}
	}
	return s
}

// xsettingsBlink is what an XSETTINGS property (app.ClackXSettings) says:
// Net/CursorBlink, Net/CursorBlinkTime, as GDK's X11 backend maps them to
// GTK's settings (an integer each).
func xsettingsBlink(prop []byte) gtkBlink {
	var s gtkBlink
	ints, ok := xsettingInts(prop)
	if !ok {
		return s
	}
	if v, ok := ints["Net/CursorBlink"]; ok {
		s.blink, s.okBlink = v != 0, true
	}
	if v, ok := ints["Net/CursorBlinkTime"]; ok {
		s.period, s.okPeriod = max(time.Duration(v)*time.Millisecond, gtkBlinkMin), true
	}
	return s
}

// xsettingInts are the integer settings of an XSETTINGS property (the
// XSETTINGS spec: a byte order, a serial and the settings' count, then each
// setting's type, name, last-change serial and value, padded to 4 bytes);
// ok is false when the bytes aren't one.
func xsettingInts(b []byte) (map[string]int32, bool) {
	if len(b) < 12 {
		return nil, false
	}
	var bo binary.ByteOrder
	switch b[0] {
	case 0: // LSBFirst
		bo = binary.LittleEndian
	case 1: // MSBFirst
		bo = binary.BigEndian
	default:
		return nil, false
	}
	pad := func(n int) int { return (n + 3) &^ 3 }
	n := bo.Uint32(b[8:12])
	p := 12
	ints := map[string]int32{}
	for i := uint32(0); i < n; i++ {
		if p+4 > len(b) {
			return nil, false
		}
		typ, nameLen := b[p], int(bo.Uint16(b[p+2:p+4]))
		p += 4
		if p+pad(nameLen)+4 > len(b) {
			return nil, false
		}
		name := string(b[p : p+nameLen])
		p += pad(nameLen) + 4 // the name, the last-change serial
		switch typ {
		case 0: // integer
			if p+4 > len(b) {
				return nil, false
			}
			ints[name] = int32(bo.Uint32(b[p : p+4]))
			p += 4
		case 1: // string
			if p+4 > len(b) {
				return nil, false
			}
			l := bo.Uint32(b[p : p+4])
			if uint64(l) > uint64(len(b)) {
				return nil, false
			}
			p += 4 + pad(int(l))
		case 2: // color
			p += 8
		default:
			return nil, false
		}
		if p > len(b) {
			return nil, false
		}
	}
	return ints, true
}

// portalRead is a value from the XDG settings portal (up to a second).
func portalRead(c *dbus.Conn, ns, key string) (any, bool) {
	ch := make(chan *dbus.Call, 1)
	call := c.Object(portalDest, portalPath).Go(portalIface+".Read", 0, ch, ns, key)
	select {
	case <-call.Done:
	case <-time.After(time.Second):
		return nil, false
	}
	if call.Err != nil {
		return nil, false
	}
	var v dbus.Variant
	if call.Store(&v) != nil {
		return nil, false
	}
	for i := 0; i < 2; i++ { // Read wraps the value in a second variant
		if in, isV := v.Value().(dbus.Variant); isV {
			v = in
		}
	}
	return v.Value(), true
}

// gtkSettingsIni is gtk-cursor-blink and gtk-cursor-blink-time from GTK 3's
// settings.ini files, in the order GTK loads them, a later file's over an
// earlier one's.
func gtkSettingsIni() gtkBlink {
	var dirs []string
	dirs = append(dirs, gtkDataDir, gtkSysconfDir)
	xdg := os.Getenv("XDG_CONFIG_DIRS")
	if xdg == "" {
		xdg = "/etc/xdg"
	}
	for _, d := range filepath.SplitList(xdg) {
		if d != "" {
			dirs = append(dirs, d)
		}
	}
	home := os.Getenv("XDG_CONFIG_HOME")
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = filepath.Join(h, ".config")
		}
	}
	if home != "" {
		dirs = append(dirs, home)
	}
	var s gtkBlink
	for _, d := range dirs {
		s = readGtkIni(filepath.Join(d, "gtk-3.0", "settings.ini")).over(s)
	}
	return s
}

// readGtkIni is what a settings.ini's [Settings] group sets, as GTK's key
// file reads it (GLib's gkeyfile.c): a file with a line that is no group,
// key=value pair or comment, or a pair before any group, doesn't load at
// all; a key's last line is its value; g_key_file_get_boolean takes true,
// false, 1 and 0, g_key_file_get_integer a decimal integer (strtol's, then
// nothing or a space); a time under GTK's minimum is refused
// (g_param_value_convert's strict validation) and leaves the earlier
// files' value.
func readGtkIni(path string) gtkBlink {
	var s gtkBlink
	data, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	var blinkV, timeV string
	var haveBlink, haveTime bool
	group, inGroup := "", false
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimLeft(line, " \t\r\v\f")
		switch {
		case line == "" || line[0] == '#':
			continue
		case line[0] == '[':
			end := strings.IndexByte(line, ']')
			if end < 0 || strings.Trim(line[end+1:], " \t\r") != "" {
				return gtkBlink{} // not a group: the file doesn't load
			}
			group, inGroup = line[1:end], true
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(k) == "" || !inGroup {
			return gtkBlink{} // not a pair, or one before any group
		}
		if group != "Settings" {
			continue
		}
		switch strings.TrimSpace(k) {
		case "gtk-cursor-blink":
			blinkV, haveBlink = strings.TrimSpace(v), true
		case "gtk-cursor-blink-time":
			timeV, haveTime = strings.TrimSpace(v), true
		}
	}
	if haveBlink {
		switch blinkV {
		case "true", "1":
			s.blink, s.okBlink = true, true
		case "false", "0":
			s.blink, s.okBlink = false, true
		}
	}
	if haveTime {
		if n, ok := keyFileInt(timeV); ok && time.Duration(n)*time.Millisecond >= gtkBlinkMin {
			s.period, s.okPeriod = time.Duration(n)*time.Millisecond, true
		}
	}
	return s
}

// keyFileInt parses v as g_key_file_get_integer does: strtol in base 10
// (a sign, digits), then the value's end or a space, within an int.
func keyFileInt(v string) (int32, bool) {
	i := 0
	if i < len(v) && (v[i] == '+' || v[i] == '-') {
		i++
	}
	j := i
	for j < len(v) && v[j] >= '0' && v[j] <= '9' {
		j++
	}
	if j == i || (j < len(v) && !strings.ContainsRune(" \t\n\r\v\f", rune(v[j]))) {
		return 0, false
	}
	n, err := strconv.ParseInt(v[:j], 10, 32)
	if err != nil {
		return 0, false
	}
	return int32(n), true
}
