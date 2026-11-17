//go:build linux

package ui

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"

	"gioui.org/widget"
)

// The caret's blink on Linux is Chromium's (OsSettingsProviderGtk::
// CaretBlinkInterval, ui/gtk/os_settings_provider_gtk.cc, main
// 2026-11-17): half of GTK's gtk-cursor-blink-time (a whole on-off
// cycle; 1200 ms when nothing sets it), none when gtk-cursor-blink is
// off. GTK's timeout (gtk-cursor-blink-timeout) is GTK's own widgets':
// Blink's caret (FrameCaret) blinks on, and so does Slack's. GTK takes
// them from the desktop: GNOME's org.gnome.desktop.interface (cursor-blink,
// cursor-blink-time), which it reads through XSETTINGS or the XDG settings
// portal, else its settings.ini. Read on a goroutine of its own (D-Bus
// waits), and again when the portal says they changed.

// gtkBlinkTime is GTK's default gtk-cursor-blink-time.
const gtkBlinkTime = 1200 * time.Millisecond

func startCaretBlinkWatcher() {
	go func() {
		c, err := dbus.ConnectSessionBus()
		if err != nil {
			c = nil
		}
		apply := func() { widget.SetCaretBlinkInterval(gtkCaretBlink(c)) }
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

// gtkCaretBlink is the caret's blink interval from GTK's settings as the
// desktop has them (c: the session bus, or nil).
func gtkCaretBlink(c *dbus.Conn) time.Duration {
	blink, period := true, gtkBlinkTime
	haveBlink, havePeriod := false, false
	if c != nil {
		if v, ok := portalRead(c, "org.gnome.desktop.interface", "cursor-blink"); ok {
			if b, ok := v.(bool); ok {
				blink, haveBlink = b, true
			}
		}
		if v, ok := portalRead(c, "org.gnome.desktop.interface", "cursor-blink-time"); ok {
			if n, ok := v.(int32); ok && n > 0 {
				period, havePeriod = time.Duration(n)*time.Millisecond, true
			}
		}
	}
	if !haveBlink || !havePeriod {
		ib, ip, okb, okp := gtkSettingsIni()
		if !haveBlink && okb {
			blink = ib
		}
		if !havePeriod && okp {
			period = ip
		}
	}
	if !blink {
		return 0
	}
	return period / 2
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

// gtkSettingsIni is gtk-cursor-blink and gtk-cursor-blink-time from GTK's
// settings.ini files, the user's first (XDG_CONFIG_HOME, then
// XDG_CONFIG_DIRS, then /etc), GTK 3's then 4's.
func gtkSettingsIni() (blink bool, period time.Duration, okBlink, okPeriod bool) {
	var dirs []string
	home := os.Getenv("XDG_CONFIG_HOME")
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = filepath.Join(h, ".config")
		}
	}
	if home != "" {
		dirs = append(dirs, home)
	}
	xdg := os.Getenv("XDG_CONFIG_DIRS")
	if xdg == "" {
		xdg = "/etc/xdg"
	}
	dirs = append(dirs, filepath.SplitList(xdg)...)
	dirs = append(dirs, "/etc")
	for _, d := range dirs {
		for _, v := range []string{"gtk-3.0", "gtk-4.0"} {
			b, p, ob, op := readGtkIni(filepath.Join(d, v, "settings.ini"))
			if ob && !okBlink {
				blink, okBlink = b, true
			}
			if op && !okPeriod {
				period, okPeriod = p, true
			}
			if okBlink && okPeriod {
				return
			}
		}
	}
	return
}

func readGtkIni(path string) (blink bool, period time.Duration, okBlink, okPeriod bool) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	inSettings := false
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			inSettings = line == "[Settings]"
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !inSettings || !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "gtk-cursor-blink":
			switch strings.ToLower(v) {
			case "true", "1", "yes":
				blink, okBlink = true, true
			case "false", "0", "no":
				blink, okBlink = false, true
			}
		case "gtk-cursor-blink-time":
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				period, okPeriod = time.Duration(n)*time.Millisecond, true
			}
		}
	}
	return
}
