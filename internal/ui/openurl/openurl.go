// Package openurl opens links in the system browser or mail client. It is
// the only place Clack hands a URL to the Windows shell (ShellExecuteEx,
// rundll32 as its retry), xdg-open or open, and it only passes http,
// https and plain mailto links.
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
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/some-du6e/clack/internal/ui/uiguard"
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
	if raw == "" || len(raw) > 8192 || !utf8.ValidString(raw) || strings.ContainsAny(raw, " \t\r\n\\\"`<>") {
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

// errNoShell: this OS has no shell hand-off (shellOpen); the URL goes to
// command's tool.
var errNoShell = errors.New("openurl: no shell hand-off here")

// rundllFirst is Slack's isOpenExternalShellForced turned off: after
// one way failed and the other opened the link, later links go the way
// that worked first (openExternal's catch: `if (await li(i, …), a !== i)
// return ag({isOpenExternalShellForced: !t})`), the other as their retry.
// Process-wide, as Slack's setting is per app.
var rundllFirst atomic.Bool

// launch hands an already-checked URL to the OS and returns once it took
// it. Off the UI goroutine. On Windows that is shellOpen, Slack's
// shell.openExternal, with rundll32 as Slack's retry after it fails
// (openExternal's catch runs the other method), and whichever worked on a
// retry goes first after it; on macOS shellOpen (NSWorkspace), retried
// once as Slack does; elsewhere command's tool.
func launch(u string) {
	uiguard.OffUI("open a link")
	launchWith(func() error { return shellOpen(u) }, func() error { return runTool(u) }, retryWithTool)
}

// launchWith is launch with its two ways (tests); withTool: a failed
// shell hand-off is retried with the tool (Windows), else with the shell
// again (macOS: Slack retries the same shell.openExternal).
func launchWith(shell, tool func() error, withTool bool) {
	if !withTool {
		if err := shell(); err != nil {
			log.Printf("openurl: %v; trying again", err)
			if err := shell(); err != nil {
				log.Printf("openurl: retry: %v", err)
			}
		}
		return
	}
	rf := rundllFirst.Load() // the order this launch uses, as Slack's ln(e, t) reads t once
	first, retry := shell, tool
	if rf {
		first, retry = tool, shell
	}
	err := first()
	switch {
	case err == nil:
		return
	case errors.Is(err, errNoShell): // macOS and Linux: the tool only
		if err := tool(); err != nil {
			log.Printf("openurl: %v", err)
		}
		return
	}
	log.Printf("openurl: %v; trying the other way", err)
	if err := retry(); err != nil {
		log.Printf("openurl: retry: %v", err)
		return
	}
	// Slack's ag({isOpenExternalShellForced: !t}): the inverse of what
	// this launch started from, so two overlapping launches don't undo
	// each other's flip
	rundllFirst.CompareAndSwap(rf, !rf)
}

// runTool runs command's tool for u and waits for it: a start error or
// a non-zero exit is a failure (Slack's spawned RunDll32 rejects on any
// other exit code). A mailto: link whose xdg-email isn't installed (an
// xdg-open replacement without xdg-utils) goes to xdg-open, as before.
func runTool(u string) error {
	uiguard.OffUI("exec (open a link)") // exec.Command looks the tool up in PATH
	cmd := command(u)
	err := cmd.Start()
	if err != nil && errors.Is(err, exec.ErrNotFound) && len(cmd.Args) > 0 && cmd.Args[0] == "xdg-email" {
		cmd = exec.Command("xdg-open", u)
		cmd.WaitDelay = waitDelay
		err = cmd.Start()
	}
	if err != nil {
		return err
	}
	return cmd.Wait()
}

// externalHandlerCharmap is Chromium's kExternalHandlerCharmap
// (base/strings/escape.cc): everything but alphanumerics, the reserved
// characters ;/?:@&=+$, and !'()*-._~#[] is escaped: controls, space,
// " % < > \ ^ ` { | } DEL and every non-ASCII byte.
var externalHandlerCharmap = [8]uint32{0xffffffff, 0x50000025, 0x50000000, 0xb8000001, 0xffffffff, 0xffffffff, 0xffffffff, 0xffffffff}

// escapeExternalHandlerValue is Chromium's EscapeExternalHandlerValue,
// which Electron's OpenExternal applies before quoting the URL for the
// shell: Escape(text, kExternalHandlerCharmap, use_plus false,
// keep_escaped true), so a %XX already there stays and any other % is
// escaped.
func escapeExternalHandlerValue(s string) string {
	return chromiumEscape(s, &externalHandlerCharmap)
}

// nsURLCharmap is Chromium's kNSURLCharmap (base/strings/escape.cc,
// Apple only): controls, space, " # % < > [ \ ] ^ ` { | }, DEL and every
// non-ASCII byte.
var nsURLCharmap = [8]uint32{0xffffffff, 0x5000002d, 0x78000000, 0xb8000001, 0xffffffff, 0xffffffff, 0xffffffff, 0xffffffff}

// nsURLPrecursor is what Chromium's NSURLWithGURL (net/base/apple/
// url_conversions.mm) hands NSURL: the path, the query and the ref each
// run through EscapeNSURLPrecursor (kNSURLCharmap, %XX kept), the scheme
// and authority as they are. NSURL before macOS 14 refuses `|{}^[]`, a
// bare % and raw non-ASCII there, and Electron's shell.openExternal
// opens such links (a Grafana ?var-x={a,b}, ?q=café).
func nsURLPrecursor(s string) string {
	i := strings.IndexByte(s, ':') + 1
	if strings.HasPrefix(s[i:], "//") {
		j := strings.IndexAny(s[i+2:], "/?#")
		if j < 0 {
			return s
		}
		i += 2 + j
	}
	rest, out := s[i:], s[:i]
	ref, hasRef := "", false
	if k := strings.IndexByte(rest, '#'); k >= 0 {
		rest, ref, hasRef = rest[:k], rest[k+1:], true
	}
	q, hasQ := "", false
	if k := strings.IndexByte(rest, '?'); k >= 0 {
		rest, q, hasQ = rest[:k], rest[k+1:], true
	}
	out += chromiumEscape(rest, &nsURLCharmap)
	if hasQ {
		out += "?" + chromiumEscape(q, &nsURLCharmap)
	}
	if hasRef {
		out += "#" + chromiumEscape(ref, &nsURLCharmap)
	}
	return out
}

// chromiumEscape is Chromium's Escape(text, charmap, use_plus false,
// keep_escaped true): every byte in charmap as %XX, a %XX already there
// kept.
func chromiumEscape(s string, charmap *[8]uint32) string {
	const hex = "0123456789ABCDEF"
	isHex := func(c byte) bool { return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F' }
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]):
			b.WriteByte('%')
		case charmap[c>>5]&(1<<(c&31)) != 0:
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&15])
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
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
		// Electron's platform_util_linux.cc OpenExternal: a mailto: link to
		// xdg-email, anything else to xdg-open
		if strings.HasPrefix(u, "mailto:") {
			c = exec.Command("xdg-email", u)
		} else {
			c = exec.Command("xdg-open", u)
		}
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
	go launch(u)
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

// Open checks raw and opens it without blocking: the hand-off (launch)
// runs on another goroutine at once (ShellExecuteEx, or exec looking the
// tool up in PATH and forking, which the UI goroutine must never wait
// for). Only Check's error is returned.
func Open(raw string) error {
	u, err := Check(raw)
	if err != nil {
		return err
	}
	go launch(u)
	return nil
}
