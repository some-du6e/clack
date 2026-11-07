// Package openurl opens links in the system browser or mail client. It is
// the only place Clack hands a URL to xdg-open, open or rundll32, and it
// only passes http, https and plain mailto links.
package openurl

import (
	"errors"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
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
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", u)
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		return exec.Command("xdg-open", u)
	}
}

// Open checks raw and opens it without blocking.
func Open(raw string) error {
	u, err := Check(raw)
	if err != nil {
		return err
	}
	cmd := command(u)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
