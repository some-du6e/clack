package messages

import (
	"net/url"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// tsTime parses a Slack ts ("1700000000.123456").
func tsTime(ts string) time.Time {
	sec, frac, _ := strings.Cut(ts, ".")
	s, err := strconv.ParseInt(sec, 10, 64)
	if err != nil {
		return time.Time{}
	}
	var ns int64
	if len(frac) == 6 {
		us, _ := strconv.ParseInt(frac, 10, 64)
		ns = us * 1000
	}
	return time.Unix(s, ns).Local()
}

// tsLess compares Slack timestamps numerically.
func tsLess(a, b string) bool {
	as, af, _ := strings.Cut(a, ".")
	bs, bf, _ := strings.Cut(b, ".")
	if len(as) != len(bs) {
		return len(as) < len(bs)
	}
	if as != bs {
		return as < bs
	}
	return af < bf
}

func ordinal(d int) string {
	if d >= 11 && d <= 13 {
		return "th"
	}
	switch d % 10 {
	case 1:
		return "st"
	case 2:
		return "nd"
	case 3:
		return "rd"
	}
	return "th"
}

// clock formats a message time like Slack: "3:04 PM".
func clock(t time.Time) string { return t.Format("3:04 PM") }

// shortClock is the hover timestamp in grouped rows: "3:04".
func shortClock(t time.Time) string { return t.Format("3:04") }

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// dayLabel is the day divider text: Today, Yesterday, "Monday, November
// 3rd", or "Wednesday, December 31st, 2025" for other years.
func dayLabel(t, now time.Time) string {
	if sameDay(t, now) {
		return "Today"
	}
	if sameDay(t, now.AddDate(0, 0, -1)) {
		return "Yesterday"
	}
	s := t.Format("Monday, January 2") + ordinal(t.Day())
	if t.Year() != now.Year() {
		s += t.Format(", 2006")
	}
	return s
}

// ago is "Last reply …" text: "just now", "5 minutes ago", "today at
// 3:04 PM", "yesterday at …", "Nov 3rd at …".
func ago(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		n := int(d / time.Minute)
		if n == 1 {
			return "1 minute ago"
		}
		return strconv.Itoa(n) + " minutes ago"
	case sameDay(t, now):
		n := int(d / time.Hour)
		if n == 1 {
			return "1 hour ago"
		}
		return strconv.Itoa(n) + " hours ago"
	case sameDay(t, now.AddDate(0, 0, -1)):
		return "yesterday at " + clock(t)
	}
	n := int(d / (24 * time.Hour))
	if n < 7 {
		return strconv.Itoa(n) + " days ago"
	}
	s := t.Format("Jan 2") + ordinal(t.Day())
	if t.Year() != now.Year() {
		s += t.Format(", 2006")
	}
	return s
}

// slackLink recognizes links to Slack messages and channels
// (https://x.slack.com/archives/C123/p1700000000123456?thread_ts=…).
func slackLink(raw string) (ch, ts string, ok bool) {
	u, err := url.Parse(raw)
	if err != nil || !strings.HasSuffix(u.Host, ".slack.com") {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || parts[0] != "archives" {
		return "", "", false
	}
	ch = parts[1]
	if len(parts) >= 3 && strings.HasPrefix(parts[2], "p") && len(parts[2]) > 7 {
		p := parts[2][1:]
		ts = p[:len(p)-6] + "." + p[len(p)-6:]
		if tt := u.Query().Get("thread_ts"); tt != "" {
			ts = tt
		}
	}
	return ch, ts, true
}

// permalink builds a message link for copy/forward.
func permalink(domain, ch, ts string) string {
	if domain == "" {
		domain = "app"
	}
	return "https://" + domain + ".slack.com/archives/" + ch + "/p" + strings.ReplaceAll(ts, ".", "")
}

// openURL opens a link in the system browser without blocking.
func openURL(u string) {
	if u == "" {
		return
	}
	if pu, err := url.Parse(u); err != nil || (pu.Scheme != "http" && pu.Scheme != "https" && pu.Scheme != "mailto") {
		return
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	go func() { _ = cmd.Run() }()
}

// avatarRadius is Slack's clamp(4px, min(22.222%, 12px), 12px).
func avatarRadius(px int) int {
	r := int(float32(px)*0.22222 + 0.5)
	if r < 4 {
		r = 4
	}
	if r > 12 {
		r = 12
	}
	return r
}
