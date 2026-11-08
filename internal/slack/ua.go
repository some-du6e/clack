package slack

import (
	"net/http"
	"os"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
)

// Slack ties a session to the browser that signed in. When the same d
// cookie turns up from what looks like a different device (another OS or
// browser in the User-Agent), Enterprise Grid's session anomaly checks can
// end that session everywhere, the browser included. So Clack sends the
// User-Agent of the browser the session was copied from (Creds.UserAgent,
// pasted at sign-in). Without one it falls back to Chrome on this OS, which
// still matches a Chrome session on the same computer.

// chromeMajor is the Chrome version of the fallback User-Agent (stable
// Chrome as of 2026-11, read from a real app.slack.com session).
const chromeMajor = "157"

// UserAgent is the fallback User-Agent: desktop Chrome on this OS.
var UserAgent = defaultUserAgent(runtime.GOOS)

func defaultUserAgent(goos string) string {
	plat := "X11; Linux x86_64"
	switch goos {
	case "darwin":
		plat = "Macintosh; Intel Mac OS X 10_15_7" // Chrome freezes the macOS version
	case "windows":
		plat = "Windows NT 10.0; Win64; x64" // Chrome freezes Windows too
	}
	return "Mozilla/5.0 (" + plat + ") AppleWebKit/537.36 (KHTML, like Gecko) Chrome/" + chromeMajor + ".0.0.0 Safari/537.36"
}

// CleanUserAgent returns ua if it looks like a real browser User-Agent
// (one line, printable, starts with Mozilla/5.0), else "".
func CleanUserAgent(ua string) string {
	ua = strings.TrimSpace(ua)
	ua = strings.TrimPrefix(ua, "User-Agent:")
	ua = strings.TrimSpace(strings.Trim(strings.TrimSpace(ua), `"'`))
	if len(ua) < 20 || len(ua) > 512 || !strings.HasPrefix(ua, "Mozilla/5.0 (") {
		return ""
	}
	for _, r := range ua {
		if r < 0x20 || r > 0x7e {
			return ""
		}
	}
	return ua
}

// userAgent is what this client sends: the session's own browser if known.
// A desktop session always sends Slack desktop's, the one it signed in with.
func (c *Client) userAgent() string {
	if c.Creds.Desktop() {
		return desktopUA()
	}
	if ua := CleanUserAgent(c.Creds.UserAgent); ua != "" {
		return ua
	}
	return UserAgent
}

var chromeVer = regexp.MustCompile(`Chrome/(\d+)\.`)

// browserHeaders sets the User-Agent and, for Chromium browsers, the
// low-entropy client hints Chrome adds to every https request
// (sec-ch-ua, -mobile, -platform), so API calls carry the same browser
// identity as the session's own tab. Not used on the websocket handshake:
// Chrome sends no client hints there (checked on app.slack.com).
func (c *Client) browserHeaders(h http.Header) {
	ua := c.userAgent()
	h.Set("User-Agent", ua)
	for k, v := range clientHints(ua) {
		h.Set(k, v)
	}
}

// clientHints derives Chrome's default client hints from a User-Agent.
// Firefox and Safari send none; Edge, Opera and Brave are detected only as
// far as the User-Agent tells (Brave hides itself as Chrome there too).
func clientHints(ua string) map[string]string {
	m := chromeVer.FindStringSubmatch(ua)
	if m == nil || strings.Contains(ua, "Electron/") || strings.Contains(ua, "Slack/") {
		return nil
	}
	v := m[1]
	brand := `"Google Chrome";v="` + v + `"`
	switch {
	case strings.Contains(ua, " Edg/"):
		brand = `"Microsoft Edge";v="` + v + `"`
	case strings.Contains(ua, " OPR/"):
		brand = "" // Opera's own version isn't in the UA in a usable form
	}
	parts := []string{`"Not-A.Brand";v="99"`}
	if brand != "" {
		parts = append(parts, brand)
	}
	parts = append(parts, `"Chromium";v="`+v+`"`)
	plat, mobile := "Linux", "?0"
	switch {
	case strings.Contains(ua, "Android"):
		plat, mobile = "Android", "?1"
	case strings.Contains(ua, "Windows"):
		plat = "Windows"
	case strings.Contains(ua, "Macintosh"):
		plat = "macOS"
	case strings.Contains(ua, "CrOS"):
		plat = "Chrome OS"
	}
	return map[string]string{
		"sec-ch-ua":          strings.Join(parts, ", "),
		"sec-ch-ua-mobile":   mobile,
		"sec-ch-ua-platform": `"` + plat + `"`,
	}
}

// Slack desktop's identity. Desktop sessions (Creds.Session ==
// SessionDesktop) send this user agent on every request and the socket,
// because their sign-in said ssb=1 with it: switching to a browser user
// agent afterwards is the kind of mismatch Grid's anomaly checks look for.
// Values are from slack-desktop-4.53.276 (Electron 45.1.0, Chrome
// 156.0.8078.49; docs/DESKTOP-SIGNIN.md).
const (
	SlackDesktopVersion = "4.53.276"
	slackElectron       = "45.1.0"
	slackChrome         = "156.0.8078.49"
)

// DesktopUserAgent is Slack desktop's user agent on this OS (read once:
// os-release and the desktop session don't change while Clack runs, and
// the user agent mustn't either).
func DesktopUserAgent() string { return desktopUA() }

var desktopUA = sync.OnceValue(func() string { return desktopUserAgent(runtime.GOOS, linuxUAInfo()) })

// publicUA is the user agent of fetches without credentials (GetNoCreds).
var publicUA atomic.Value // string

// SetPublicUserAgent makes credential-free fetches (Slack's CDNs, its
// image proxy) send ua, so a desktop session presents one identity
// everywhere. "" restores the browser fallback (UserAgent).
func SetPublicUserAgent(ua string) { publicUA.Store(ua) }

func publicUserAgent() string {
	if ua, _ := publicUA.Load().(string); ua != "" {
		return ua
	}
	return UserAgent
}

// desktopUserAgent builds the user agent the way Slack desktop does (o3 in
// main.bundle.cjs, @808076): Electron's default
// "Mozilla/5.0 (<platform>) AppleWebKit/537.36 (KHTML, like Gecko)
// Slack/<ver> Chrome/<chrome> Electron/<electron> Safari/537.36", with
// "; <distro> <release>[; <desktop>]" added inside the parentheses on Linux
// (module 65726 "Mo"), then " Sonic Slack_SSB/<ver>". The Snap, Flatpak and
// GovEnvironment markers don't apply to Clack.
func desktopUserAgent(goos, linux string) string {
	plat := "X11; Linux x86_64" + linux
	switch goos {
	case "darwin":
		plat = "Macintosh; Intel Mac OS X 10_15_7"
	case "windows":
		plat = "Windows NT 10.0; Win64; x64"
	}
	return "Mozilla/5.0 (" + plat + ") AppleWebKit/537.36 (KHTML, like Gecko) Slack/" + SlackDesktopVersion +
		" Chrome/" + slackChrome + " Electron/" + slackElectron + " Safari/537.36 Sonic Slack_SSB/" + SlackDesktopVersion
}

// uaStrip is the character class Slack desktop removes from the distro
// parts of its user agent.
var uaStrip = regexp.MustCompile(`[()<>@,;:\\/\[\]?={}\t\n\r]`)

// linuxUAInfo is Slack desktop's Linux user agent addition: NAME (or ID)
// and VERSION_ID from os-release, and XDG_CURRENT_DESKTOP.
func linuxUAInfo() string {
	if runtime.GOOS != "linux" {
		return ""
	}
	var b []byte
	for _, p := range []string{"/etc/os-release", "/usr/lib/os-release"} {
		if d, err := os.ReadFile(p); err == nil {
			b = d
			break
		}
	}
	return linuxUAFrom(string(b), os.Getenv("XDG_CURRENT_DESKTOP"))
}

func linuxUAFrom(osRelease, desktop string) string {
	kv := map[string]string{}
	for _, l := range strings.Split(osRelease, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(l), "=")
		if !ok || k == "" || strings.HasPrefix(k, "#") {
			continue
		}
		if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
			v = v[1 : len(v)-1]
		}
		kv[k] = v
	}
	name := kv["NAME"]
	if name == "" {
		name = kv["ID"]
	}
	if name == "" {
		return ""
	}
	rel := kv["VERSION_ID"]
	if rel == "" {
		rel = "unversioned"
	}
	s := "; " + uaStrip.ReplaceAllString(name, "") + " " + uaStrip.ReplaceAllString(rel, "")
	if desktop = uaStrip.ReplaceAllString(desktop, ""); desktop != "" {
		s += "; " + desktop
	}
	return printableASCII(s)
}

func printableASCII(s string) string {
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x20 && s[i] <= 0x7e {
			b = append(b, s[i])
		}
	}
	return string(b)
}
