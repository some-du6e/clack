package slack

import (
	"net/http"
	"regexp"
	"runtime"
	"strings"
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
func (c *Client) userAgent() string {
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
