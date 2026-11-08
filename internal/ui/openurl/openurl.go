// Package openurl opens links in the system browser or mail client. It is
// the only place Clack hands a URL to xdg-open, open or rundll32, and it
// only passes http, https and plain mailto links.
package openurl

import (
	"errors"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/uiguard"
)

// ErrRefused is returned for links Clack won't hand to the OS.
var ErrRefused = errors.New("can't open this link")

// mailtoParams are the mailto: header fields passed through. Others (attach=,
// attachment=, …) are dropped: some mail clients attach local files from them.
var mailtoParams = map[string]bool{"subject": true, "body": true, "cc": true, "bcc": true, "to": true}

// Check returns the URL to hand to the OS, or ErrRefused. Only absolute
// http(s) URLs with a host and mailto: links are allowed; file:, javascript:,
// data:, slack: and other custom schemes, and anything with control
// characters or spaces, are refused. The URL is re-serialized, so it always
// starts with its scheme (never "-", which xdg-open/open would read as a flag).
func Check(raw string) (string, error) {
	if raw == "" || len(raw) > 8192 || strings.ContainsAny(raw, " \t\r\n\\\"`<>") {
		return "", ErrRefused
	}
	for _, r := range raw {
		if r < 0x20 || r == 0x7f {
			return "", ErrRefused
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", ErrRefused
	}
	switch u.Scheme {
	case "http", "https":
		if u.Host == "" || u.Opaque != "" || strings.HasPrefix(u.Host, "-") {
			return "", ErrRefused
		}
	case "mailto":
		if u.Opaque == "" || strings.HasPrefix(u.Opaque, "-") {
			return "", ErrRefused
		}
		q := u.Query()
		for k := range q {
			if !mailtoParams[strings.ToLower(k)] {
				q.Del(k)
			}
		}
		u.RawQuery = strings.ReplaceAll(q.Encode(), "+", "%20") // mailto wants %20
	default:
		return "", ErrRefused
	}
	s := u.String()
	if !strings.HasPrefix(s, u.Scheme+":") {
		return "", ErrRefused
	}
	return s, nil
}

// command is the OS command for an already-checked URL.
func command(u string) *exec.Cmd {
	uiguard.OffUI("exec (open a link)") // exec.Command looks the tool up in PATH
	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		c = exec.Command("open", u)
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		c = exec.Command("xdg-open", u)
	}
	c.WaitDelay = waitDelay
	return c
}

// waitDelay bounds Wait after the tool exits (its children may keep the
// inherited stdio open).
const waitDelay = 2 * time.Second

// settingsPages are the system settings pages Clack opens: the privacy
// panes where the user lets it use the microphone and camera (huddles).
var settingsPages = map[string]bool{
	"x-apple.systempreferences:com.apple.preference.security?Privacy_Microphone": true,
	"x-apple.systempreferences:com.apple.preference.security?Privacy_Camera":     true,
	"ms-settings:privacy-microphone":                                             true,
	"ms-settings:privacy-webcam":                                                 true,
}

// OpenSettings opens one of the fixed system settings pages above, without
// blocking (like Open). Anything else is ErrRefused.
func OpenSettings(u string) error {
	if !settingsPages[u] {
		return ErrRefused
	}
	go func() {
		cmd := command(u)
		if err := cmd.Start(); err != nil {
			log.Printf("openurl: %v", err)
			return
		}
		_ = cmd.Wait()
	}()
	return nil
}

// OpenFolder shows an existing directory in the file manager (Finder,
// Explorer, xdg-open), without blocking. dir must be absolute.
func OpenFolder(dir string) error {
	if !filepath.IsAbs(dir) || strings.ContainsAny(dir, "\x00\r\n") {
		return ErrRefused
	}
	dir = filepath.Clean(dir)
	go func() {
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			return
		}
		uiguard.OffUI("exec (open a folder)")
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", dir)
		case "windows":
			cmd = exec.Command("explorer", dir)
		default:
			cmd = exec.Command("xdg-open", dir)
		}
		cmd.WaitDelay = waitDelay
		if err := cmd.Start(); err != nil {
			log.Printf("openurl: %v", err)
			return
		}
		_ = cmd.Wait()
	}()
	return nil
}

// Open checks raw and opens it without blocking: the browser starts on
// another goroutine (exec looks the tool up in PATH and forks, which the
// UI goroutine must never wait for). Only Check's error is returned.
func Open(raw string) error {
	u, err := Check(raw)
	if err != nil {
		return err
	}
	go func() {
		cmd := command(u)
		if err := cmd.Start(); err != nil {
			log.Printf("openurl: %v", err)
			return
		}
		_ = cmd.Wait()
	}()
	return nil
}
