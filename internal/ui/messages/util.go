package messages

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/openurl"
	"github.com/some-du6e/clack/internal/ui/uipref"
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

// clock formats a message time like Slack: "3:04 PM" ("15:04" with the
// time24 pref).
func clock(t time.Time) string { return uipref.Clock(t) }

// shortClock is the hover timestamp in grouped rows: "3:04".
func shortClock(t time.Time) string { return uipref.ShortClock(t) }

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// dayLabel is the day divider text: Today, Yesterday, "Monday, November
// 3rd", or "Wednesday, December 31st, 2025" for other years.
func dayLabel(t, now time.Time) string {
	if l := i18n.Locale(); l != i18n.EnUS {
		return i18n.DayDivider(l, t, now, false) // formatDateDayDivider
	}
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

// threadTime is a thread pane message's time, Slack's THREAD_DETAIL_VIEW
// format (getCustomTimestampFormat → module QvGr): "Just now" under a
// minute, then toTimeAgo's "1 minute ago" (under 90 s), "N minutes ago"
// (rounded, under 45 min), "1 hour ago"; after an hour "Today at 3:04 PM",
// "Yesterday at 3:04 PM", "Monday at 3:04 PM" within a week, else
// "Nov 3rd at 3:04 PM" ("Nov 3rd, 2025 at …" in another year).
func threadTime(t, now time.Time) string {
	s := now.Sub(t).Seconds()
	if s < 0 {
		s = -s
	}
	m := s / 60
	if l := i18n.Locale(); l != i18n.EnUS {
		return threadTimeIn(l, t, now, s, m)
	}
	switch {
	case s < 60:
		return "Just now"
	case m < 60:
		switch {
		case s < 90:
			return "1 minute ago"
		case m < 45:
			return strconv.Itoa(int(m+0.5)) + " minutes ago"
		}
		return "1 hour ago"
	case sameDay(t, now):
		return "Today at " + clock(t)
	case sameDay(t, now.AddDate(0, 0, -1)):
		return "Yesterday at " + clock(t)
	case m/60/24 < 7:
		return t.Format("Monday") + " at " + clock(t)
	}
	d := t.Format("Jan 2") + ordinal(t.Day())
	if t.Year() != now.Year() {
		d += t.Format(", 2006")
	}
	return d + " at " + clock(t)
}

// msgNS is Slack's "message" I18n (the timestamp helpers, the reply bar).
var msgNS = i18n.NS("message")

// threadTimeIn is threadTime's Slack strings in a locale.
func threadTimeIn(l string, t, now time.Time, s, m float64) string {
	tm := clock(t)
	switch {
	case s < 60:
		return msgNS.T("Just now")
	case m < 60:
		return i18n.TimeAgo(l, t, now, false, true)
	case sameDay(t, now):
		return msgNS.T("Today at {time}", "time", tm)
	case sameDay(t, now.AddDate(0, 0, -1)):
		return msgNS.T("Yesterday at {time}", "time", tm)
	case m/60/24 < 7:
		return msgNS.T("{dayOfTheWeek} at {time}", "dayOfTheWeek", i18n.Weekday(l, t, false), "time", tm)
	}
	d := i18n.CalendarDate(l, t, i18n.CalendarOpts{ShortenMonth: true, ExcludeYear: t.Year() == now.Year()})
	return msgNS.T("{calendarDate} at {time}", "calendarDate", d, "time", tm)
}

// threadsNS is Slack's "threads" I18n (BZhr's reply bar strings).
var threadsNS = i18n.NS("threads")

// offlineNS is LargeOfflineIndicator's (nMFb) I18n.
var offlineNS = i18n.NS("offline_mode")

// lastReply is the reply bar's getThreadLatestReplyText (client-boot-shared
// BZhr): today, "Last reply today at 3:23 PM" ("Today at 3:23 PM" with
// one reply); else "Last reply" and toTimeAgo ("Last reply 16 hours ago",
// "Last reply 3 days ago"; the time ago alone with one reply).
func lastReply(t, now time.Time, count int) string {
	tm := clock(t)
	if sameDay(t, now) {
		if count == 1 {
			return threadsNS.T("Today at {time}", "time", tm)
		}
		return threadsNS.T("Last reply today at {time}", "time", tm)
	}
	ago := i18n.TimeAgo(i18n.Locale(), t, now, false, false)
	if count == 1 {
		return ago
	}
	return threadsNS.T("Last reply {time_ago}", "time_ago", ago)
}

// repliesLabel is the reply count, Slack's threads string
// "{count, plural, =1 {# reply} other {# replies}}".
func repliesLabel(n int) string {
	if i18n.Locale() == i18n.EnUS {
		if n == 1 {
			return "1 reply"
		}
		return strconv.Itoa(n) + " replies"
	}
	return i18n.T("threads", "{count, plural, =1 {# reply} other {# replies}}", "count", n)
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
// https and mailto only; see openurl.Check). A var so tests can catch it.
var openURL = func(u string) { _ = openurl.Open(u) }

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
