package messages

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/openurl"
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

// ago is the "Last reply …" text, as Slack writes it (measured live):
// "today at 3:23 PM", "yesterday at 9:05 AM", then "N days ago",
// "N months ago", "N years ago".
func ago(t, now time.Time) string {
	if sameDay(t, now) || t.After(now) {
		return "today at " + clock(t)
	}
	if sameDay(t, now.AddDate(0, 0, -1)) {
		return "yesterday at " + clock(t)
	}
	days := int(now.Sub(t) / (24 * time.Hour))
	if days < 2 {
		days = 2
	}
	plural := func(n int, unit string) string {
		if n == 1 {
			return "1 " + unit + " ago"
		}
		return strconv.Itoa(n) + " " + unit + "s ago"
	}
	switch {
	case days < 30:
		return plural(days, "day")
	case days < 365:
		return plural(days/30, "month")
	}
	return plural(days/365, "year")
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

// openURL opens a link in the system browser without blocking (http,
// https and mailto only; see openurl.Check).
func openURL(u string) { _ = openurl.Open(u) }

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
